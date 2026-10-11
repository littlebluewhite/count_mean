package muscle_ratio

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/manifest"
	"count_mean/internal/models"
	"count_mean/internal/musclemap"
	"count_mean/internal/synchronizer"
	"count_mean/internal/windowmean"
)

// Params holds inputs for a batch muscle-ratio analysis run.
//
// 輸出目錄就是 CSVHandler 的 config.OutputDir:每次寫檔由 CSVHandler 驗證路徑並建目錄,
// analyzer 不另持有輸出目錄(ADR-0052)。
type Params struct {
	ManifestFile string
	DataFolder   string
	CSVHandler   *io.CSVHandler // ADR-0004 Boundary 1: row layout 透過 CSVHandler 落實
}

// SubjectResult records one subject's outcome in the batch.
//
// Success 與 Err 的語意：
//   - Success=true + Err=nil → 兩個 CSV 都產出
//   - Success=true + Err 非 nil → Output 1 產出、Output 2 跳過（warning），Err 解釋原因
//   - Success=false → Output 1 也沒產出（檔案不存在、解析失敗、缺通道、輸出目錄不可寫等），Err 必填
//
// Err 多為帶 i18n key 的 i18n.Error(Error() 是 zh-TW),呈現給使用者時由 gui 以
// i18n.Localize 依目前 locale 渲染(ADR-0048);下游套件的錯誤(開檔、缺通道)原樣保留。
//
// DurationMs：per-subject 從 analyzeSubject 入口到結束的耗時（millisecond），用於
// partial-success batch 的 diagnostic（哪個 subject 拖慢整批、哪個 subject 提早 fail）。
type SubjectResult struct {
	Subject         string
	OutputAllPath   string
	OutputPhasePath string
	Success         bool
	Err             error
	DurationMs      int64
}

// Analyzer orchestrates the batch pipeline. Concurrency model:
//   - PathValidator 不掛在 struct 上（per-Analyze 建立 instance），避免 Wails 並行 RPC 場景下
//     兩個 goroutine 共用 mutable instance → race condition。
//   - manifest loading 與 EMG file resolution 走 internal/manifest 套件（stateless package functions）。
type Analyzer struct {
	logger *logging.Logger
}

// NewAnalyzer creates a new muscle-ratio analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		logger: logging.GetLogger("muscle_ratio_analyzer"),
	}
}

// Analyze runs the full batch pipeline:
//  1. Parse manifest → []PhaseManifest(manifest.LoadManifests 已擋 Subject 輸出檔名撞名,ADR-0056)
//  2. Build a request-scoped PathValidator rooted at DataFolder
//  3. For each subject: load EMG → computeSubject → write 2 CSVs
//
// ctx 帶 Wails Shutdown / 使用者中止訊號。subject 迴圈間檢查 ctx.Done(),
// 收到取消立即停止 — 已完成的 subject 結果保留,未開始的不處理。caller 可以
// 從 returned []SubjectResult 看到 partial 進度,但 returned error 為
// ctx.Err()(包成帶 i18n key 的錯誤),caller(gui handler)可區分「整批失敗」vs「部分取消」。
//
//nolint:err113 // dynamic errors for user-facing output
func (a *Analyzer) Analyze(ctx context.Context, params *Params) ([]SubjectResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx 不能為 nil")
	}
	if params == nil {
		return nil, fmt.Errorf("params 不能為 nil")
	}
	// ADR-0004 Boundary 1: muscle_ratio 寫檔須透過 CSVHandler。如果 caller 沿用舊
	// Params 結構未填 CSVHandler 欄位,在 analyzeSubject 內取用會 nil-deref panic;
	// 提早 fail 給 caller 明確訊息。
	if params.CSVHandler == nil {
		return nil, fmt.Errorf("params.CSVHandler 不能為 nil (ADR-0004: muscle_ratio 寫檔須透過 CSVHandler)")
	}

	manifests, err := manifest.LoadManifests(params.ManifestFile)
	if err != nil {
		return nil, i18n.WrapError(err, i18n.KeyErrorMuscleRatioParseManifestFailed)
	}

	if len(manifests) == 0 {
		return nil, i18n.NewError(i18n.KeyErrorMuscleRatioEmptyManifest)
	}

	// EMG 開檔走 manifest.OpenDataFile（內部走 security.OpenLenientValidated，
	// 允許含 literal "%" 的 BTS 匯出檔名 — 見 internal/manifest 套件 doc）。
	//
	// 每個 subject 處理完後檢查 ctx.Done() — Wails Shutdown 或使用者中止會
	// cancel ctx,迴圈立刻 break。返回 partial results + 包成帶 i18n key 的 ctx.Err()。
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
			return results, i18n.WrapError(ctx.Err(), i18n.KeyErrorMuscleRatioCancelled)
		default:
		}
		m := &manifests[i]
		results = append(results, a.analyzeSubject(m, params))
	}

	a.logger.Info("肌肉比值批次分析完成", map[string]any{
		"manifest": params.ManifestFile,
		"subjects": len(results),
	})

	return results, nil
}

// analyzeSubject 是單一 subject 的 I/O adapter(ADR-0052):檢查 blank Subject → LoadEMG →
// computeSubject → 寫 Output 1 → 沒有 warn 才寫 Output 2 → sticky-success。
// Errors are captured in SubjectResult.Err and never abort the batch.
//
// 錯誤優先序:core err > Output 1 寫檔錯誤 > warn > Output 2 寫檔錯誤。Output 1 沒寫成時,
// Output 2 的 warn 沒有意義,故 Output 1 寫檔錯誤優先於 warn。
//
// 計時：用 named return + defer 在所有 return 路徑（含 error fast-path）統一寫入 DurationMs，
// 避免各 return point 各自重複呼叫 time.Since。
func (*Analyzer) analyzeSubject(
	m *models.PhaseManifest,
	params *Params,
) (result SubjectResult) {
	start := time.Now()
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()

	result.Subject = m.Subject

	// Subject 為空時擋下：filename.Sanitize("") 會 fallback 成 "untitled"，
	// 經 SubjectOutputName 組出 "untitled_muscle_ratio.csv"。
	//   - 多筆空 Subject 共用此 "untitled" 前綴 → 全寫同一檔、後者 O_TRUNC 覆寫前者；
	//     此情境其實已由 manifest.LoadManifests 的撞名檢查(同 SubjectOutputKey 即整份拒絕)先擋下。
	//   - 單筆空 Subject 能通過批次唯一性檢查，但 "untitled_muscle_ratio.csv" 是
	//     非預期且易與真實命名碰撞的檔名。故在此 per-subject 再 fail-fast，要求顯式 Subject。
	if strings.TrimSpace(m.Subject) == "" {
		result.Err = i18n.NewError(i18n.KeyErrorMuscleRatioSubjectEmptyName)
		return result
	}

	emg, err := manifest.LoadEMG(params.DataFolder, m)
	if err != nil {
		var parseErr *manifest.EMGParseError
		if errors.As(err, &parseErr) {
			result.Err = i18n.NewError(i18n.KeyErrorMuscleRatioSubjectParseEMGFailed, err)
		} else {
			result.Err = err
		}
		return result
	}

	computed, warn, err := computeSubject(emg, m)
	if err != nil {
		result.Err = err
		return result
	}

	pairLabels := defaultRatioLabels()

	outAllPath, writeAllErr := params.CSVHandler.WriteMuscleRatioOutputAll(
		io.MuscleRatioOutputAllPayload{
			Subject:    m.Subject,
			PairLabels: pairLabels,
			Times:      computed.times,
			Ratios:     computed.ratios,
		},
	)
	if writeAllErr != nil {
		result.Err = i18n.NewError(i18n.KeyErrorMuscleRatioSubjectWriteOutput1Failed, writeAllErr)
		return result
	}

	result.OutputAllPath = outAllPath

	// Output 2 — phases + midpoints (ADR-0004 Boundary 1: sticky-success 規則
	// 留在 Analyzer。computeSubject 的 warn 與 Output 2 寫檔失敗都讓
	// Output 1 視為 sticky-success)。
	if warn != nil {
		result.Success = true
		result.Err = warn
		return result
	}

	avgLabels := make([]string, len(pairLabels))
	for i, l := range pairLabels {
		avgLabels[i] = fmt.Sprintf("%s (%dpt avg)", l, 2*phaseWindowHalf+1)
	}

	outPhasePath, writePhaseErr := params.CSVHandler.WriteMuscleRatioOutputPhases(
		io.MuscleRatioOutputPhasesPayload{
			Subject:    m.Subject,
			PairLabels: avgLabels,
			Points:     computed.points,
		},
	)
	if writePhaseErr != nil {
		// Output 1 已寫入磁碟，依 SubjectResult 文件契約 Output 1 success 為 sticky：
		// 與 warn-path 對稱（Success=true + Err 解釋為何 Output 2 跳過）。
		result.Success = true
		result.Err = i18n.NewError(i18n.KeyErrorMuscleRatioSubjectWriteOutput2Failed, writePhaseErr)
		return result
	}

	result.OutputPhasePath = outPhasePath
	result.Success = true

	return result
}

// subjectCompute 是 computeSubject 算好、交給 analyzeSubject 寫檔的值(ADR-0052)。
type subjectCompute struct {
	times  []float64                  // Output 1 時間軸(EMG 秒數)
	ratios [][]float64                // Output 1:ratios[k][i] 是 DefaultRatios()[k] 在第 i 筆 sample 的比值
	points []io.MuscleRatioPhasePoint // Output 2 的列(phase + 中點,11 點 window mean);warn 非 nil 時為 nil
}

// computeSubject 是 muscle_ratio 的 compute core(ADR-0052,仿 ADR-0024 的 computeCCI):
// 對已載入的 EMG 與 manifest row 依序做空 EMG 檢查 → RightSideChannels → ComputeAllRatios →
// collectPhasePoints → buildPhasePoints,不碰檔案系統。analyzeSubject 是它的 I/O adapter。
//
// 回傳值:
//   - err 非 nil:連 Output 1 都算不出來(EMG 為空、缺通道),computed 為零值。
//   - warn 非 nil:Output 1 的值已算好,Output 2 要跳過([[Phase timeline]] 分期點不足或
//     落在 EMG 範圍外),computed.points 為 nil。
//   - 兩者皆 nil:Output 1 與 Output 2 的值都已算好。
func computeSubject(
	emg *models.PhaseSyncEMGData, m *models.PhaseManifest,
) (computed subjectCompute, warn, err error) {
	if len(emg.Time) == 0 {
		return subjectCompute{}, nil, i18n.NewError(i18n.KeyErrorMuscleRatioSubjectEmptyEMG)
	}

	channelMap, err := musclemap.RightSideChannels(emg.Headers)
	if err != nil {
		return subjectCompute{}, nil, err
	}

	computed = subjectCompute{
		times:  emg.Time,
		ratios: ComputeAllRatios(emg, channelMap),
	}

	points, warn := collectPhasePoints(m, emg)
	if warn != nil {
		return computed, warn, nil
	}

	computed.points = buildPhasePoints(points, computed.ratios, emg.Time)

	return computed, nil, nil
}

// phasePoint represents one row in Output 2 — either an actual phase or a midpoint between two phases.
type phasePoint struct {
	name string
	time float64 // EMG-time-aligned
}

// phaseWindowHalf 是 Output-2 11 點 window mean 的半徑:center ± 5。(ADR-0014)
const phaseWindowHalf = 5

// buildPhasePoints 把 collected phase points 轉成 Output-2 payload points:
// 每個 pair 取 11 點(center ± phaseWindowHalf)window mean,顯示時間 snap 到最近 sample。(ADR-0014)
func buildPhasePoints(points []phasePoint, ratiosAll [][]float64, times []float64) []io.MuscleRatioPhasePoint {
	out := make([]io.MuscleRatioPhasePoint, len(points))
	for i, p := range points {
		idx, _ := synchronizer.ResolveTimeIndex(times, p.time)
		values := make([]float64, len(ratiosAll))
		for k := range ratiosAll {
			values[k] = windowmean.WindowMean(ratiosAll[k], idx, phaseWindowHalf)
		}
		out[i] = io.MuscleRatioPhasePoint{Name: p.name, Time: times[idx], Values: values}
	}
	return out
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

// collectPhasePoints takes the manifest row's [[Phase timeline]] (EMG-time phase points,
// empties already dropped — ADR-0042), validates bounds, sorts, and emits two kinds of rows
// interleaved by time:
//   - actual phase points (up to 10)
//   - adjacent-pair midpoints between consecutive phases (up to N-1)
//   - biomechanical-interval midpoints from biomechanicalIntervalMidpoints (up to K, skipped if endpoints absent)
//
// Output length is up to 2N-1+K rows where K = len(biomechanicalIntervalMidpoints).
//
// 第二個回傳值是 warning — 非 nil 表示 Output 2 該跳過（Output 1 已成功）。
func collectPhasePoints(
	m *models.PhaseManifest, emg *models.PhaseSyncEMGData,
) ([]phasePoint, error) {
	timeline := synchronizer.NewPhaseTimeline(m)
	phases := make([]phasePoint, 0, len(timeline))
	for _, pt := range timeline {
		phases = append(phases, phasePoint{name: string(pt.Phase), time: pt.EMGTime})
	}

	if len(phases) < 2 {
		return nil, i18n.NewError(i18n.KeyErrorMuscleRatioSubjectInsufficientPhases)
	}

	// ResolveTimeIndex 對 out-of-range target 會靜默 clamp 到首/末 sample，產生「看似成功但
	// 數值錯位」的 Output 2，故建表前用其 inRange 旗標攔截:任一 phase 越界即整批跳過 Output 2
	// (all-or-nothing 政策)。其 1e-6 容差吸收 force-plate↔EMG 同步的 ULP 飄移；真實 typo
	// (≥1ms) 仍被擋。emgStart/emgEnd 保留供錯誤訊息。
	emgStart, emgEnd := emg.Time[0], emg.Time[len(emg.Time)-1]
	for _, p := range phases {
		if _, inRange := synchronizer.ResolveTimeIndex(emg.Time, p.time); !inRange {
			return nil, i18n.NewError(
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

	return points, nil
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

// defaultRatioLabels 提取 DefaultRatios() 的 pair name slice,供 CSVHandler payload 用。
func defaultRatioLabels() []string {
	pairs := DefaultRatios()
	labels := make([]string, len(pairs))
	for i, r := range pairs {
		labels[i] = r.Name
	}
	return labels
}
