package muscle_ratio

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/csvutil"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/synchronizer"
)

// Params holds inputs for a batch muscle-ratio analysis run.
type Params struct {
	ManifestFile string
	DataFolder   string
	OutputDir    string
}

// SubjectResult records one subject's outcome in the batch.
//
// Success 與 Error 的語意：
//   - Success=true + Error="" → 兩個 CSV 都產出
//   - Success=true + Error 非空 → Output 1 產出、Output 2 跳過（warning），Error 解釋原因
//   - Success=false → Output 1 也沒產出（檔案不存在、解析失敗、缺通道等），Error 必填
type SubjectResult struct {
	Subject         string
	OutputAllPath   string
	OutputPhasePath string
	Success         bool
	Error           string
}

// AnalysisResult aggregates all subjects' results from a single Analyze call.
type AnalysisResult struct {
	Subjects []SubjectResult
}

// Analyzer orchestrates the batch pipeline. Concurrency model:
//   - PathValidator 與 EMGParser 都不掛在 struct 上（per-Analyze / per-subject 建立 instance），
//     避免 Wails 並行 RPC 場景下兩個 goroutine 共用 mutable instance → race condition。
//   - manifestParser 與 timeSynchronizer 是 stateless（無 mutable field write），可安全共用。
//
// EMGParser 的 race：`ParseFile` 寫 `p.frequency` 欄位，兩個並行 Analyze 呼叫共用 instance 會觸發
// write/write race（`-race` 偵測得到，CI 跑 `make test-race`）。
type Analyzer struct {
	manifestParser   *parsers.PhaseManifestParser
	timeSynchronizer *synchronizer.TimeSynchronizer
	logger           *logging.Logger
}

// NewAnalyzer creates a new muscle-ratio analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		manifestParser:   parsers.NewPhaseManifestParser(),
		timeSynchronizer: synchronizer.NewTimeSynchronizer(),
		logger:           logging.GetLogger("muscle_ratio_analyzer"),
	}
}

// Analyze runs the full batch pipeline:
//  1. Parse manifest → []PhaseManifest
//  2. Assert sanitized-subject uniqueness (no two manifests resolve to the same output file)
//  3. Build a request-scoped PathValidator rooted at DataFolder
//  4. Ensure OutputDir exists
//  5. For each subject: load EMG → channel map → compute ratios → write 2 CSVs
//
//nolint:err113 // dynamic errors for user-facing output
func (a *Analyzer) Analyze(params *Params) (*AnalysisResult, error) {
	if params == nil {
		return nil, fmt.Errorf("params 不能為 nil")
	}

	manifests, err := a.manifestParser.ParseFile(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	if len(manifests) == 0 {
		return nil, fmt.Errorf("分期總檔案沒有任何主題記錄")
	}

	if err := assertUniqueSanitizedSubjects(manifests); err != nil {
		return nil, err
	}

	baseFolder := params.DataFolder
	if resolved, evalErr := filepath.EvalSymlinks(baseFolder); evalErr == nil {
		baseFolder = resolved
	}

	if err := os.MkdirAll(params.OutputDir, fsperm.DirPerm); err != nil {
		return nil, fmt.Errorf("建立輸出目錄失敗: %w", err)
	}

	// 路徑解析走 security.ResolveLenientPath（不再用 PathValidator.GetSafePath，後者誤拒
	// 含 literal "%" 的標準 BTS EMG 匯出檔名 — 見該 helper 的 doc comment）。
	results := make([]SubjectResult, 0, len(manifests))
	for i := range manifests {
		m := &manifests[i]
		results = append(results, a.analyzeSubject(m, baseFolder, params.OutputDir))
	}

	a.logger.Info("肌肉比值批次分析完成", map[string]interface{}{
		"manifest": params.ManifestFile,
		"subjects": len(results),
	})

	return &AnalysisResult{Subjects: results}, nil
}

// analyzeSubject processes one subject. Errors are captured in SubjectResult.Error and never abort the batch.
func (a *Analyzer) analyzeSubject(
	m *models.PhaseManifest,
	dataFolder, outputDir string,
) SubjectResult {
	result := SubjectResult{Subject: m.Subject}

	// Subject 為空時擋下，否則 SanitizeFileName("") 會產生 "_muscle_ratio.csv"，
	// 多筆空 Subject 在 assertUniqueSanitizedSubjects 之後仍可能單筆通過寫到同一檔。
	if strings.TrimSpace(m.Subject) == "" {
		result.Error = "Subject 名稱為空"
		return result
	}

	emgPath, err := security.ResolveLenientPath(dataFolder, m.EMGFile)
	if err != nil {
		result.Error = fmt.Sprintf("EMG 檔案路徑驗證失敗: %v", err)
		return result
	}

	if _, statErr := os.Stat(emgPath); os.IsNotExist(statErr) {
		result.Error = fmt.Sprintf("EMG 檔案不存在: %s", emgPath)
		return result
	}

	// Per-subject EMGParser — see struct doc comment for race rationale.
	emgParser := parsers.NewEMGParser()

	emg, err := emgParser.ParseFile(emgPath)
	if err != nil {
		result.Error = fmt.Sprintf("解析 EMG 檔案失敗: %v", err)
		return result
	}

	if len(emg.Time) == 0 {
		result.Error = "EMG 資料為空"
		return result
	}

	channelMap, err := BuildRightSideChannelMap(emg.Headers)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	ratiosAll := ComputeAllRatios(emg, channelMap)

	safeSubject := calculator.SanitizeFileName(m.Subject)

	outAllPath := filepath.Join(outputDir, fmt.Sprintf("%s_muscle_ratio.csv", safeSubject))
	if err := writeOutputAll(outAllPath, emg.Time, ratiosAll); err != nil {
		result.Error = fmt.Sprintf("寫入 Output 1 失敗: %v", err)
		return result
	}

	result.OutputAllPath = outAllPath

	// Output 2 — phases + midpoints. 失敗或跳過時 Output 1 仍視為成功。
	points, warn := a.collectPhasePoints(m, emg)
	if warn != "" {
		result.Success = true
		result.Error = warn
		return result
	}

	outPhasePath := filepath.Join(outputDir, fmt.Sprintf("%s_muscle_ratio_phases.csv", safeSubject))
	if err := writeOutputPhases(outPhasePath, emg.Time, ratiosAll, points); err != nil {
		// Output 1 已寫入磁碟，依 SubjectResult 文件契約 Output 1 success 為 sticky：
		// 與 collectPhasePoints warn-path 對稱（Success=true + Error 解釋為何 Output 2 跳過）。
		result.Success = true
		result.Error = fmt.Sprintf("寫入 Output 2 失敗（Output 1 已產出）: %v", err)
		return result
	}

	result.OutputPhasePath = outPhasePath
	result.Success = true

	return result
}

// phasePoint represents one row in Output 2 — either an actual phase or a midpoint between two phases.
type phasePoint struct {
	name string
	time float64 // EMG-time-aligned
}

// collectPhasePoints converts the manifest's 10 raw phase values to EMG time, drops empties,
// validates bounds, sorts, generates N-1 midpoints, and returns 2N-1 rows sorted by time.
//
// 第二個回傳值是 warning message — 非空表示 Output 2 該跳過（Output 1 已成功）。
//
//nolint:err113 // strings, not errors, for the warning channel
func (a *Analyzer) collectPhasePoints(
	m *models.PhaseManifest, emg *models.PhaseSyncEMGData,
) ([]phasePoint, string) {
	phases := make([]phasePoint, 0, 10)

	for _, p := range models.AllPhases() {
		v, _, err := parsers.GetPhaseValue(&m.PhasePoints, p)
		if err != nil {
			continue
		}

		// v == 0 是 parser 的空值約定（"", "NA", "x", "-" → 0）。理論上 force-time 0 秒
		// 是合法的，但 manifest CSV 用 0 同時表達「空」與「真實 0 秒」是專案層級的契約 —
		// 與 cci/analyzer.go calculateGaitCycle 對稱。若 domain 改變需要區分兩者，要從
		// parser 層而非此處調整。
		if v == 0 {
			continue
		}

		var t float64
		if p.IsMotionIndex() {
			t = a.timeSynchronizer.MotionIndexToEMGTime(int(v), m.EMGMotionOffset)
		} else {
			t = a.timeSynchronizer.ForceTimeToEMGTime(v, m.EMGMotionOffset)
		}

		phases = append(phases, phasePoint{name: string(p), time: t})
	}

	if len(phases) < 2 {
		return nil, "有效分期點不足 2 個，跳過 Output 2"
	}

	// FindNearestTimeIndex 對 out-of-range target 會靜默 clamp 到首/末 sample
	// (見 synchronizer/time_sync.go:166-174)，這會產生「看似成功但數值錯位」的 Output 2。
	// 在呼叫前自行做 bounds check。
	emgStart, emgEnd := emg.Time[0], emg.Time[len(emg.Time)-1]
	for _, p := range phases {
		if p.time < emgStart || p.time > emgEnd {
			return nil, fmt.Sprintf(
				"phase %s 時間 %.4f 落在 EMG 範圍 [%.4f, %.4f] 外，跳過 Output 2",
				p.name, p.time, emgStart, emgEnd,
			)
		}
	}

	sort.Slice(phases, func(i, j int) bool { return phases[i].time < phases[j].time })

	points := make([]phasePoint, 0, 2*len(phases)-1)
	for i, p := range phases {
		points = append(points, p)

		if i < len(phases)-1 {
			points = append(points, phasePoint{
				name: fmt.Sprintf("mid_%s_%s", phases[i].name, phases[i+1].name),
				time: (phases[i].time + phases[i+1].time) / 2,
			})
		}
	}

	return points, ""
}

// assertUniqueSanitizedSubjects rejects batches whose subjects collapse to the same output filename
// after SanitizeFileName. Without this check, "A/B" + "A:B" both write to "A_B_muscle_ratio.csv" and
// the latter silently overwrites the former.
//
// 衝突 key 用 strings.ToLower：macOS 與 Windows 預設使用 case-insensitive filesystem，
// "SF8" 與 "sf8" 雖然 case-sensitive 不同，但寫入磁碟時實為同檔，後者 O_TRUNC 會覆寫前者。
//
//nolint:err113 // dynamic errors for user-facing output
func assertUniqueSanitizedSubjects(manifests []models.PhaseManifest) error {
	seen := make(map[string]string, len(manifests))

	for _, m := range manifests {
		safe := calculator.SanitizeFileName(m.Subject)
		key := strings.ToLower(safe)

		if prev, exists := seen[key]; exists {
			return fmt.Errorf(
				"輸出檔名衝突: subject %q 與 %q 經 SanitizeFileName 後同為 %q (case-insensitive)",
				prev, m.Subject, safe,
			)
		}

		seen[key] = m.Subject
	}

	return nil
}

// writeOutputAll writes the full time-series ratio CSV (Output 1).
//
// 寫入流程與 cci/analyzer.go writeCSVFile 對稱：fsperm.WriteFlags 含 O_NOFOLLOW、UTF-8 BOM、
// SanitizeHeaderRow 避免 CSV injection、NaN/Inf cell → 空字串。
//
//nolint:err113 // dynamic errors for user-facing output
func writeOutputAll(outputPath string, times []float64, ratiosAll [][]float64) (err error) {
	file, err := os.OpenFile(filepath.Clean(outputPath), fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("建立 CSV 檔案失敗: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	if err := csvutil.WriteBOM(file); err != nil {
		return fmt.Errorf("寫入 BOM 失敗: %w", err)
	}

	writer := csv.NewWriter(file)

	header := make([]string, 0, 1+len(DefaultRatios))
	header = append(header, "Time (s)")
	for _, r := range DefaultRatios {
		header = append(header, r.Name)
	}

	if err := writer.Write(csvutil.SanitizeHeaderRow(header)); err != nil {
		return fmt.Errorf("寫入標題列失敗: %w", err)
	}

	for i, t := range times {
		row := make([]string, 0, 1+len(ratiosAll))
		row = append(row, fmt.Sprintf("%.4f", t))

		for k := range ratiosAll {
			row = append(row, formatRatioCell(ratiosAll[k], i))
		}

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入資料列失敗: %w", err)
		}
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	return nil
}

// writeOutputPhases writes the 2N-1 phase+midpoint slice CSV (Output 2).
//
//nolint:err113 // dynamic errors for user-facing output
func writeOutputPhases(
	outputPath string, times []float64, ratiosAll [][]float64, points []phasePoint,
) (err error) {
	file, err := os.OpenFile(filepath.Clean(outputPath), fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("建立 CSV 檔案失敗: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	if err := csvutil.WriteBOM(file); err != nil {
		return fmt.Errorf("寫入 BOM 失敗: %w", err)
	}

	writer := csv.NewWriter(file)

	header := make([]string, 0, 2+len(DefaultRatios))
	header = append(header, "Phase", "Time (s)")
	for _, r := range DefaultRatios {
		header = append(header, r.Name)
	}

	if err := writer.Write(csvutil.SanitizeHeaderRow(header)); err != nil {
		return fmt.Errorf("寫入標題列失敗: %w", err)
	}

	for _, p := range points {
		idx := synchronizer.FindNearestTimeIndex(times, p.time)

		row := make([]string, 0, 2+len(ratiosAll))
		row = append(row, p.name, fmt.Sprintf("%.4f", times[idx]))

		for k := range ratiosAll {
			row = append(row, formatRatioCell(ratiosAll[k], idx))
		}

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入資料列失敗: %w", err)
		}
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	return nil
}

// formatRatioCell formats one ratio value as a CSV cell. NaN/Inf → empty string
// (missing-data convention, parallel to cci/analyzer.go writeCSVFile).
func formatRatioCell(values []float64, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}

	v := values[idx]
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}

	return fmt.Sprintf("%.6f", v)
}
