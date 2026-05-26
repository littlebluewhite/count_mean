package muscle_ratio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"count_mean/internal/calculator"
	"count_mean/internal/csvutil"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"count_mean/internal/manifest"
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
//
// DurationMs：per-subject 從 analyzeSubject 入口到結束的耗時（millisecond），用於
// partial-success batch 的 diagnostic（哪個 subject 拖慢整批、哪個 subject 提早 fail）。
type SubjectResult struct {
	Subject         string
	OutputAllPath   string
	OutputPhasePath string
	Success         bool
	Error           string
	DurationMs      int64
}

// Analyzer orchestrates the batch pipeline. Concurrency model:
//   - PathValidator 不掛在 struct 上（per-Analyze 建立 instance），避免 Wails 並行 RPC 場景下
//     兩個 goroutine 共用 mutable instance → race condition。
//   - timeSynchronizer 是 stateless（無 mutable field write），可安全共用。
//   - manifest loading 與 EMG file resolution 走 internal/manifest 套件（stateless package functions）。
type Analyzer struct {
	timeSynchronizer *synchronizer.TimeSynchronizer
	logger           *logging.Logger
}

// NewAnalyzer creates a new muscle-ratio analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
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
// ctx 帶 Wails Shutdown / 使用者中止訊號。subject 迴圈間檢查 ctx.Done(),
// 收到取消立即停止 — 已完成的 subject 結果保留,未開始的不處理。caller 可以
// 從 returned []SubjectResult 看到 partial 進度,但 returned error 為
// ctx.Err()(包 i18n 後),caller(gui handler)可區分「整批失敗」vs「部分取消」。
//
//nolint:err113 // dynamic errors for user-facing output
func (a *Analyzer) Analyze(ctx context.Context, params *Params) ([]SubjectResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx 不能為 nil")
	}
	if params == nil {
		return nil, fmt.Errorf("params 不能為 nil")
	}

	// Defense-in-depth：config 載入時已驗證 OutputDir，但 GUI file dialog 等繞過
	// config 的 caller 仍可能傳壞值。附 dummy child 後 ValidateExternalPath 擋
	// traversal / system-dir prefix，避免後續 os.MkdirAll 走到 /etc 等敏感目錄。
	checkPath := filepath.Join(params.OutputDir, "_validation_marker")
	if err := security.NewPathValidator(nil).ValidateExternalPath(checkPath); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorMuscleRatioOutputDirInvalid), err)
	}

	manifests, err := manifest.LoadManifests(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorMuscleRatioParseManifestFailed), err)
	}

	if len(manifests) == 0 {
		return nil, errors.New(i18n.T(i18n.KeyErrorMuscleRatioEmptyManifest))
	}

	if err := assertUniqueSanitizedSubjects(manifests); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(params.OutputDir, fsperm.DirPerm); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorMuscleRatioMkdirFailed), err)
	}

	// EMG 路徑解析走 manifest.ResolveEMGFile（內部走 security.ResolveLenientPath，
	// 允許含 literal "%" 的 BTS 匯出檔名 — 見 internal/manifest 套件 doc）。
	//
	// 每個 subject 處理完後檢查 ctx.Done() — Wails Shutdown 或使用者中止會
	// cancel ctx,迴圈立刻 break。返回 partial results + ctx.Err()(由 caller 包 i18n)。
	// 不在 analyzeSubject 內部頻繁 poll,subject-level granularity 對 batch UX 已夠 —
	// 每個 subject 平均 ~100ms-2s,使用者按 cancel 後最多等一個 subject 結束。
	results := make([]SubjectResult, 0, len(manifests))
	for i := range manifests {
		select {
		case <-ctx.Done():
			a.logger.Warn("肌肉比值批次被中止", map[string]any{
				"completed": len(results),
				"total":     len(manifests),
				"reason":    ctx.Err(),
			})
			return results, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorMuscleRatioCancelled), ctx.Err())
		default:
		}
		m := &manifests[i]
		results = append(results, a.analyzeSubject(m, params.DataFolder, params.OutputDir))
	}

	a.logger.Info("肌肉比值批次分析完成", map[string]any{
		"manifest": params.ManifestFile,
		"subjects": len(results),
	})

	return results, nil
}

// analyzeSubject processes one subject. Errors are captured in SubjectResult.Error and never abort the batch.
//
// 計時：用 named return + defer 在所有 return 路徑（含 error fast-path）統一寫入 DurationMs，
// 避免 11 個 return point 各自重複呼叫 time.Since。
func (a *Analyzer) analyzeSubject(
	m *models.PhaseManifest,
	dataFolder, outputDir string,
) (result SubjectResult) {
	start := time.Now()
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()

	result.Subject = m.Subject

	// Subject 為空時擋下，否則 SanitizeFileName("") 會產生 "_muscle_ratio.csv"，
	// 多筆空 Subject 在 assertUniqueSanitizedSubjects 之後仍可能單筆通過寫到同一檔。
	if strings.TrimSpace(m.Subject) == "" {
		result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectEmptyName)
		return result
	}

	emgPath, err := manifest.ResolveEMGFile(dataFolder, m.EMGFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	emg, _, err := parsers.NewEMGParser().ParseFile(emgPath)
	if err != nil {
		result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectParseEMGFailed, err)
		return result
	}

	if len(emg.Time) == 0 {
		result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectEmptyEMG)
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
		result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput1Failed, err)
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
		result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput2Failed, err)
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

// biomechanicalIntervalMidpoints defines extra "interval" midpoints whose endpoints
// span across the canonical phase chain — i.e. they skip over one intermediate phase
// to represent a biomechanical movement stage rather than an adjacent-pair sample.
//
//   - (S, D): squat phase, skips C (the squat-to-extension transition)
//   - (D, T): push-off phase, skips T0 (end of force decay before take-off)
//
// (T, O) and (O, L) are intentionally omitted: T→O and O→L are already adjacent
// pairs in the canonical phase order, so their midpoints are produced by the
// adjacent-pair loop in collectPhasePoints — duplicating them here would emit the
// same row twice.
//
//nolint:gochecknoglobals // canonical domain constants
var biomechanicalIntervalMidpoints = []struct {
	From, To models.PhasePoint
}{
	{models.PhaseS, models.PhaseD},
	{models.PhaseD, models.PhaseT},
}

// collectPhasePoints converts the manifest's 10 raw phase values to EMG time, drops empties,
// validates bounds, sorts, and emits two kinds of rows interleaved by time:
//   - actual phase points (up to 10)
//   - adjacent-pair midpoints between consecutive phases (up to N-1)
//   - biomechanical-interval midpoints from biomechanicalIntervalMidpoints (up to K, skipped if endpoints absent)
//
// Output length is up to 2N-1+K rows where K = len(biomechanicalIntervalMidpoints).
//
// 第二個回傳值是 warning message — 非空表示 Output 2 該跳過（Output 1 已成功）。
//
//nolint:err113 // strings, not errors, for the warning channel
func (a *Analyzer) collectPhasePoints(
	m *models.PhaseManifest, emg *models.PhaseSyncEMGData,
) ([]phasePoint, string) {
	phases := make([]phasePoint, 0, 10)

	for _, p := range models.AllPhases() {
		opt, _, err := parsers.GetPhaseValue(&m.PhasePoints, p)
		if err != nil {
			continue
		}

		// Batch T：用 OptFloat.Get() 判斷「該分期點是否已標定」。Set=false（NA / 空字串）
		// 跳過；Set=true 帶實際值（含 t=0 也是合法時間）。與 cci/analyzer.go
		// calculateGaitCycle 對稱。
		v, ok := opt.Get()
		if !ok {
			continue
		}

		var t float64
		if p.IsMotionIndex() {
			// `int(v)` 對 v ∉ [MinInt32, MaxInt32] 範圍時行為 unsafe
			// (float64 → int 對 32-bit platform 是 implementation-defined,且 1e15
			// 之類異常大值會 wrap-around 或 truncate)。Manifest parseInt 已對
			// motion-index 欄位 (D/O) cap 在 MaxReasonableMotionIndex (1e9),
			// 但 OptFloat 路徑來自 dynamic dispatch — 防呆檢查仍必要,避免未來
			// 引入新 motion-index phase 時漏防。
			//
			// 邊界:`MaxReasonableMotionIndex` (1e9) 與 negative motion index
			// (≤ 0 是 sentinel "未提供",理論上 OptFloat.Set=true 不該帶 0,但
			// 防呆)。命中邊界外 → skip 該 phase + log warn,不 propagate(整批仍走)。
			if v <= 0 || v > float64(parsers.MaxReasonableMotionIndex) {
				a.logger.Warn("motion-index 分期點越界,跳過", map[string]any{
					"subject": m.Subject,
					"phase":   string(p),
					"value":   v,
					"cap":     parsers.MaxReasonableMotionIndex,
				})
				continue
			}
			t = a.timeSynchronizer.MotionIndexToEMGTime(int(v), m.EMGMotionOffset)
		} else {
			t = a.timeSynchronizer.ForceTimeToEMGTime(v, m.EMGMotionOffset)
		}

		phases = append(phases, phasePoint{name: string(p), time: t})
	}

	if len(phases) < 2 {
		return nil, i18n.T(i18n.KeyErrorMuscleRatioSubjectInsufficientPhases)
	}

	// FindNearestTimeIndex 對 out-of-range target 會靜默 clamp 到首/末 sample
	// (見 synchronizer/time_sync.go:166-174)，這會產生「看似成功但數值錯位」的 Output 2。
	// 在呼叫前自行做 bounds check。
	emgStart, emgEnd := emg.Time[0], emg.Time[len(emg.Time)-1]
	for _, p := range phases {
		if p.time < emgStart || p.time > emgEnd {
			return nil, i18n.T(
				i18n.KeyErrorMuscleRatioSubjectPhaseOutOfEMGRange,
				p.name, p.time, emgStart, emgEnd,
			)
		}
	}

	sort.Slice(phases, func(i, j int) bool { return phases[i].time < phases[j].time })

	phaseTime := make(map[models.PhasePoint]float64, len(phases))
	for _, p := range phases {
		phaseTime[models.PhasePoint(p.name)] = p.time
	}

	points := make([]phasePoint, 0, 2*len(phases)-1+len(biomechanicalIntervalMidpoints))
	for i, p := range phases {
		points = append(points, p)

		if i < len(phases)-1 {
			points = append(points, phasePoint{
				name: fmt.Sprintf("mid_%s_%s", phases[i].name, phases[i+1].name),
				time: (phases[i].time + phases[i+1].time) / 2,
			})
		}
	}

	points = appendIntervalMidpoints(points, phaseTime)

	// 重新排序整個輸出 — biomechanical-interval midpoints 的時間位置不一定在末尾。
	// 用 SliceStable 而非 Slice 防止 D == T0 等罕見等時邊界讓 test snapshot 不穩。
	sort.SliceStable(points, func(i, j int) bool { return points[i].time < points[j].time })

	return points, ""
}

// appendIntervalMidpoints adds biomechanical-interval midpoints (defined in
// biomechanicalIntervalMidpoints) to points. Each interval's midpoint is emitted
// only when:
//
//  1. Both endpoints are present in phaseTime — endpoints may be absent because
//     the corresponding phase was empty in the manifest, parsing failed, or bounds
//     validation upstream removed it. Silently skipping in that case keeps behavior
//     aligned with the existing missing-phase tolerance in collectPhasePoints.
//
//  2. Neither the canonical name `mid_From_To` nor its reverse `mid_To_From` was
//     already produced by the adjacent-pair loop. If the intermediate phase is
//     absent (e.g. C absent makes S and D adjacent in the sorted phase list), the
//     adjacent-pair loop emits the row itself — appending again here would emit a
//     visually-distinct but semantically-duplicate row.
//
//     The reverse-name check matters because the adjacent-pair loop names rows by
//     time-sort order (phases[i].name → phases[i+1].name), not canonical phase
//     order. When motion/force time conversions make D.emg < S.emg (or T.emg <
//     D.emg), the adjacent label flips to `mid_D_S` / `mid_T_D`. Dedup by endpoint
//     pair, not just canonical label.
func appendIntervalMidpoints(
	points []phasePoint, phaseTime map[models.PhasePoint]float64,
) []phasePoint {
	existingNames := make(map[string]struct{}, len(points))
	for _, p := range points {
		existingNames[p.name] = struct{}{}
	}

	for _, iv := range biomechanicalIntervalMidpoints {
		from, fromOK := phaseTime[iv.From]
		to, toOK := phaseTime[iv.To]
		if !fromOK || !toOK {
			continue
		}

		canonicalName := fmt.Sprintf("mid_%s_%s", iv.From, iv.To)
		reversedName := fmt.Sprintf("mid_%s_%s", iv.To, iv.From)
		if _, exists := existingNames[canonicalName]; exists {
			continue
		}
		if _, exists := existingNames[reversedName]; exists {
			continue
		}

		points = append(points, phasePoint{
			name: canonicalName,
			time: (from + to) / 2,
		})
	}

	return points
}

// assertUniqueSanitizedSubjects rejects batches whose subjects collapse to the same output filename
// after SanitizeFileName. Without this check, "A/B" + "A:B" both write to "A_B_muscle_ratio.csv" and
// the latter silently overwrites the former.
//
// 衝突 key 用 NFC + strings.ToLower：
//   - case-insensitive：macOS 與 Windows 預設使用 case-insensitive filesystem，
//     "SF8" 與 "sf8" 雖然 case-sensitive 不同，但寫入磁碟時實為同檔，後者 O_TRUNC 會覆寫前者。
//   - NFC normalization：macOS APFS/HFS+ 對 "café" (NFC, U+00E9) 與 "café" (NFD, e+U+0301)
//     hash 同 on-disk name，但 strings.ToLower 視為相異 — 若不 NFC normalize，會放行兩筆
//     manifest 然後第二筆覆寫第一筆。
//
//nolint:err113 // dynamic errors for user-facing output
func assertUniqueSanitizedSubjects(manifests []models.PhaseManifest) error {
	seen := make(map[string]string, len(manifests))

	for _, m := range manifests {
		safe := calculator.SanitizeFileName(m.Subject)
		key := norm.NFC.String(strings.ToLower(safe))

		if prev, exists := seen[key]; exists {
			return errors.New(i18n.T(
				i18n.KeyErrorMuscleRatioSubjectCollision,
				prev, m.Subject, safe,
			))
		}

		seen[key] = m.Subject
	}

	return nil
}

// writeOutputAll writes the full time-series ratio CSV (Output 1).
// 透過 csvutil.WriteCSVAtomic 保證 tmp+rename atomic write：BOM/header/row
// 任一階段失敗，final path 不會留下截斷檔。NaN/Inf cell → 空字串保留 in formatRatioCell。
//
//nolint:err113 // dynamic errors for user-facing output
func writeOutputAll(outputPath string, times []float64, ratiosAll [][]float64) error {
	pairs := DefaultRatios()
	header := make([]string, 0, 1+len(pairs))
	header = append(header, "Time (s)")
	for _, r := range pairs {
		header = append(header, r.Name)
	}

	return csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header: header,
		Emit: func(emit func([]string) error) error {
			for i, t := range times {
				row := make([]string, 0, 1+len(ratiosAll))
				row = append(row, fmt.Sprintf("%.4f", t))
				for k := range ratiosAll {
					row = append(row, formatRatioCell(ratiosAll[k], i))
				}
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// writeOutputPhases writes the phase+midpoint slice CSV (Output 2). Row count is
// determined by collectPhasePoints (up to 2N-1+K rows, where K is the count of
// biomechanical-interval midpoints with surviving endpoints).
// 同樣透過 csvutil.WriteCSVAtomic atomic 寫入。
//
//nolint:err113 // dynamic errors for user-facing output
func writeOutputPhases(
	outputPath string, times []float64, ratiosAll [][]float64, points []phasePoint,
) error {
	pairs := DefaultRatios()
	header := make([]string, 0, 2+len(pairs))
	header = append(header, "Phase", "Time (s)")
	for _, r := range pairs {
		header = append(header, r.Name)
	}

	return csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header: header,
		Emit: func(emit func([]string) error) error {
			for _, p := range points {
				idx := synchronizer.FindNearestTimeIndex(times, p.time)
				row := make([]string, 0, 2+len(ratiosAll))
				row = append(row, p.name, fmt.Sprintf("%.4f", times[idx]))
				for k := range ratiosAll {
					row = append(row, formatRatioCell(ratiosAll[k], idx))
				}
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
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
