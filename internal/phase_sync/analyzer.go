// Package phase_sync provides phase synchronization analysis for EMG, motion,
// and force plate data. It coordinates data validation, time synchronization,
// and statistical calculations across multiple data sources.
//
//nolint:revive // package name with underscore maintained for backward compatibility
package phase_sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/i18n"
	"count_mean/internal/manifest"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/synchronizer"
)

// Validation errors.
var (
	// ErrInvalidSubjectIndex indicates an invalid subject index.
	ErrInvalidSubjectIndex = errors.New("invalid subject index")
	// ErrFileNotFound indicates a file was not found.
	ErrFileNotFound = errors.New("file not found")
	// ErrPhasePointOutOfRange indicates a phase point is out of data range.
	ErrPhasePointOutOfRange = errors.New("phase point out of data range")
	// ErrEMGTimeOutOfRange indicates EMG time is out of data range.
	ErrEMGTimeOutOfRange = errors.New("EMG time out of data range")
	// ErrBaseFolderNotFound 指 DataFolder 不存在；修補：原本 baseFolder
	// 不存在會穿透 EvalSymlinks 回到原始字串，後續 PathValidator 才以一個不存在
	// 的 base 比較，錯誤訊息含糊。此 sentinel 讓 caller 一眼看出設定錯誤。
	ErrBaseFolderNotFound = errors.New("base folder not found")
	// ErrPhaseValueZero 表示請求的分期點在 manifest row 未提供(力板時間 Set=false、
	// motion-index ≤ 0),不在 [[Phase timeline]] 內。
	ErrPhaseValueZero = errors.New("phase value is zero or not set")
	// ErrStartTimeAfterEnd 表示開始分期點換算後的 EMG 時間晚於結束分期點。
	ErrStartTimeAfterEnd = errors.New("start time is after end time")
)

// PhaseSyncAnalyzer 分期同步分析器.
//
// 兩個入口共用 load 與 compute core(computePhaseSync):AnalyzePhaseSync(單一分期區間)
// 與 AnalyzeNormalizedPhaseSync(標準化區間 + 統計區間,ADR-0047)。
type PhaseSyncAnalyzer struct {
	motionParser    *parsers.MotionParser
	ancParser       *parsers.ANCParser
	phaseCalculator *synchronizer.PhaseCalculator
	statsCalculator *calculator.EMGStatisticsCalculator
	rangeNormalizer *calculator.RangeNormalizer
}

// NewPhaseSyncAnalyzer 創建新的分期同步分析器.
// 不持有 PathValidator — manifest 引用的資料檔改走 manifest.OpenDataFile 這道
// fused 安全門（見 validateEMGFilePath / load），無 request-scoped validator
// 狀態需要管理。
func NewPhaseSyncAnalyzer() *PhaseSyncAnalyzer {
	return &PhaseSyncAnalyzer{
		motionParser:    parsers.NewMotionParser(),
		ancParser:       parsers.NewANCParser(),
		phaseCalculator: synchronizer.NewPhaseCalculator(),
		statsCalculator: calculator.NewEMGStatisticsCalculator(),
		rangeNormalizer: calculator.NewRangeNormalizer(),
	}
}

// validationContext 驗證上下文，用於在驗證步驟之間傳遞數據.
// baseFolder 在 validateEMGFilePath 內解析後存入，後續 validateMotionFile /
// validateForceFile 與 load 的 EMG 解析共用。資料檔改走 manifest.OpenDataFile
// 這道門（接受 BTS 字面 "%" 檔名），ctx 不再持有 PathValidator — 避免並發污染。
// 不含分期點:分期點順序由入口在 I/O 之前驗證(validatePhasePair)。
type validationContext struct {
	manifestFile string
	dataFolder   string
	subjectIndex int
	manifests    []models.PhaseManifest
	manifest     models.PhaseManifest
	baseFolder   string
	emgFilePath  string
}

// validationStep 定義驗證步驟函數類型.
type validationStep func(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error

// validateManifestFile 驗證分期總檔案.
func validateManifestFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	manifests, err := manifest.LoadManifests(ctx.manifestFile)
	if err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncParseManifestFailed)
	}

	ctx.manifests = manifests

	return nil
}

// validateSubjectIndex 驗證主題索引.
func validateSubjectIndex(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.subjectIndex < 0 || ctx.subjectIndex >= len(ctx.manifests) {
		return i18n.WrapError(ErrInvalidSubjectIndex, i18n.KeyErrorPhaseSyncInvalidSubjectIndex,
			ctx.subjectIndex, len(ctx.manifests))
	}

	ctx.manifest = ctx.manifests[ctx.subjectIndex]

	return nil
}

// validateManifestData 驗證分期總檔案數據.
func validateManifestData(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if err := parsers.ValidatePhaseManifest(&ctx.manifest); err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncManifestDataInvalid)
	}

	// phase_sync 會開 Motion / Force 檔,故在此要求兩欄非空;CCI 不開這兩檔,不要求(ADR-0045)。
	// Message 仍是硬編碼 zh:與 parsers.ValidatePhaseManifest 同型別,不在 ADR-0048 遷移範圍。
	if ctx.manifest.MotionFile == "" {
		return i18n.WrapError(
			models.PhaseSyncValidationError{Field: "MotionFile", Message: "Motion檔案名不能為空"},
			i18n.KeyErrorPhaseSyncManifestDataInvalid)
	}

	if ctx.manifest.ForceFile == "" {
		return i18n.WrapError(
			models.PhaseSyncValidationError{Field: "ForceFile", Message: "力板檔案名不能為空"},
			i18n.KeyErrorPhaseSyncManifestDataInvalid)
	}

	return nil
}

// validatePhasePair 驗證一對分期點:名稱合法、開始嚴格早於結束(start == end 的
// zero-duration 區間也擋下)。兩個入口都在任何 manifest / 檔案 I/O 之前呼叫
// (ADR-0047),resolvePhaseRange 不再重驗。
func (analyzer *PhaseSyncAnalyzer) validatePhasePair(startPhase, endPhase models.PhasePoint) error {
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(startPhase, endPhase); err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncPhaseOrderInvalid)
	}

	return nil
}

// validateEMGFilePath 驗證 EMG 檔案路徑.
//
// 先 Stat baseFolder 確認存在，否則 EvalSymlinks 失敗會 silently 落回
// 原始字串，再下游路徑解析才以一個不存在的 base 失敗，錯誤訊息對使用者沒有
// 直接幫助。顯式擋下，配合 ErrBaseFolderNotFound 給明確訊息。baseFolder 解析後
// 存入 ctx，後續 validateMotionFile / validateForceFile 共用。
//
// EMG / Motion / Force 檔案改走 manifest.OpenDataFile 這道 fused 安全門（lenient
// 路徑解析 + 原子化 validated-open，close validate-vs-open TOCTOU 縫隙），對齊
// cci / muscle_ratio 與 ADR-0017 的 consolidation。門內接受含 literal "%" 的 BTS
// 匯出檔名（如 "NSF_1_BTS%_*.csv"），且以 RESOLVE_NO_SYMLINKS / O_NOFOLLOW_ANY
// 在 open 階段封死 parent-component symlink 攻擊面。
//
// EMG 在此「validate-early」：先開門確認存在性 + 安全性後立即 Close（驗證原子化），
// 真正解析延後到 load()——load() 再開一次門取得 reader。EMG 合法地開兩次，是
// validate-pipeline 結構的固有特性。validateEMGFilePath 仍存 ctx.emgFilePath
// （已驗證的解析後路徑）供 LiteralPercent 等 white-box 驗證觀察。
//
// baseFolder 的 os.Stat 顯式守門保留：EvalSymlinks 失敗會 silently 落回原始字串，
// 顯式擋下配合 ErrBaseFolderNotFound 給明確訊息；解析後存入 ctx.baseFolder，
// 後續 validateMotionFile / validateForceFile 共用（OpenDataFile 接受已解析的 base）。
func validateEMGFilePath(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	baseFolder := ctx.dataFolder
	if info, err := os.Stat(baseFolder); err != nil {
		if os.IsNotExist(err) {
			return i18n.WrapError(ErrBaseFolderNotFound, i18n.KeyErrorPhaseSyncDataFolderNotFound, baseFolder)
		}
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncDataFolderStatFailed, baseFolder)
	} else if !info.IsDir() {
		return i18n.WrapError(ErrBaseFolderNotFound, i18n.KeyErrorPhaseSyncDataFolderNotDir, baseFolder)
	}

	if resolvedBase, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolvedBase
	}

	ctx.baseFolder = baseFolder

	f, err := manifest.OpenDataFile(baseFolder, ctx.manifest.EMGFile)
	if err != nil {
		return mapOpenDataFileErr("EMG", ctx.manifest.EMGFile, err)
	}
	ctx.emgFilePath = f.Name()
	_ = f.Close() //nolint:errcheck // validate-early open; read-only fd, close error not actionable

	return nil
}

// mapOpenDataFileErr 把 manifest.OpenDataFile 的 sentinel 錯誤映射回 phase_sync
// 自有的 user-facing 錯誤契約。dataType 是 "Motion" / "Force Plate" 等資料類別
// 標籤，filename 是 manifest 欄位（含副檔名）供「期待的檔放在哪」affordance。
//
//   - ErrManifestDataFileMissing → "<dataType> 檔案不存在 (<filename>)" + ErrFileNotFound
//     （保留 phase_sync 自有 ErrFileNotFound sentinel,讓 missing-file 測試與 caller
//     的 errors.Is 仍命中）。
//   - 其餘（路徑驗證失敗 / baseFolder 無法解析）→ "<dataType> 檔案路徑驗證失敗"。
func mapOpenDataFileErr(dataType, filename string, err error) error {
	if errors.Is(err, manifest.ErrManifestDataFileMissing) {
		return i18n.WrapError(ErrFileNotFound, i18n.KeyErrorPhaseSyncDataFileNotFound, dataType, filename)
	}
	return i18n.WrapError(err, i18n.KeyErrorPhaseSyncDataFilePathInvalid, dataType)
}

// validateMotionFile 驗證 Motion 檔案.
//
//nolint:dupl // Similar to validateForceFile but handles different data type
func validateMotionFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.manifest.MotionFile == "" {
		return nil
	}

	f, err := manifest.OpenDataFile(ctx.baseFolder, ctx.manifest.MotionFile)
	if err != nil {
		return mapOpenDataFileErr("Motion", ctx.manifest.MotionFile, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	motionData, err := analyzer.motionParser.Parse(f, ctx.manifest.MotionFile)
	if err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncParseMotionFailed)
	}

	maxMotionIndex := 0
	if len(motionData.Indices) > 0 {
		maxMotionIndex = motionData.Indices[len(motionData.Indices)-1]
	}

	return validateMotionPhasePoints(&ctx.manifest, maxMotionIndex)
}

// validateMotionPhasePoints 驗證 Motion 相關分期點.
func validateMotionPhasePoints(manifest *models.PhaseManifest, maxMotionIndex int) error {
	if manifest.PhasePoints.D > 0 && manifest.PhasePoints.D > maxMotionIndex {
		return i18n.WrapError(ErrPhasePointOutOfRange, i18n.KeyErrorPhaseSyncMotionPhaseOutOfRange,
			"D", manifest.PhasePoints.D, maxMotionIndex)
	}

	if manifest.PhasePoints.O > 0 && manifest.PhasePoints.O > maxMotionIndex {
		return i18n.WrapError(ErrPhasePointOutOfRange, i18n.KeyErrorPhaseSyncMotionPhaseOutOfRange,
			"O", manifest.PhasePoints.O, maxMotionIndex)
	}

	if manifest.EMGMotionOffset > 0 && manifest.EMGMotionOffset > maxMotionIndex {
		return i18n.WrapError(ErrPhasePointOutOfRange, i18n.KeyErrorPhaseSyncMotionOffsetOutOfRange,
			manifest.EMGMotionOffset, maxMotionIndex)
	}

	return nil
}

// validateForceFile 驗證 Force Plate 檔案.
//
//nolint:dupl // Similar to validateMotionFile but handles different data type
func validateForceFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.manifest.ForceFile == "" {
		return nil
	}

	f, err := manifest.OpenDataFile(ctx.baseFolder, ctx.manifest.ForceFile)
	if err != nil {
		return mapOpenDataFileErr("Force Plate", ctx.manifest.ForceFile, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	forceData, err := analyzer.ancParser.Parse(f, ctx.manifest.ForceFile)
	if err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncParseForceFailed)
	}

	maxForceTime := 0.0
	if len(forceData.Time) > 0 {
		maxForceTime = forceData.Time[len(forceData.Time)-1]
	}

	return validateForcePhasePoints(&ctx.manifest, maxForceTime)
}

// validateForcePhasePoints 驗證 Force Plate 相關分期點.
// 只檢 force-time phase（!IsMotionIndex()），motion-index 的 D 與 O 由
// validateMotionPhasePoints 另行驗證。用 PhasePoint.IsMotionIndex() 集中
// domain invariant，避免逐項硬編碼 "P0"/"P1"/.../"L"。
//
// Batch T：用 OptFloat.Get() 判斷「是否標定」，t=0 不再被誤判為未提供。
// 仍要求 value > maxForceTime 才回 out-of-range —— 負值（合法的「校準前偏移」）
// 不會 trigger out-of-range，與 NOT IMPLEMENTED 註解一致。
func validateForcePhasePoints(manifest *models.PhaseManifest, maxForceTime float64) error {
	for _, phase := range models.AllPhases() {
		if phase.IsMotionIndex() {
			continue
		}

		// GetPhaseValue 對 10 個合法 PhasePoint 都 return nil error；allPhases 已涵蓋
		// 全部，error path 不可達。
		opt, _, _ := parsers.GetPhaseValue(&manifest.PhasePoints, phase) //nolint:errcheck // unreachable error path
		value, ok := opt.Get()
		if !ok {
			continue
		}
		if value > maxForceTime {
			return i18n.WrapError(ErrPhasePointOutOfRange, i18n.KeyErrorPhaseSyncForcePhaseOutOfRange,
				phase, value, maxForceTime)
		}
	}

	return nil
}

// runValidationPipeline 執行驗證管線.
func (analyzer *PhaseSyncAnalyzer) runValidationPipeline(ctx *validationContext) error {
	steps := []validationStep{
		validateManifestFile,
		validateSubjectIndex,
		validateManifestData,
		validateEMGFilePath,
		validateMotionFile,
		validateForceFile,
	}

	for _, step := range steps {
		if err := step(analyzer, ctx); err != nil {
			return err
		}
	}

	return nil
}

// load 執行兩個入口共用的前置載入：路徑驗證、manifest 解析、EMG 檔案解析，回傳選定
// [[Subject]] 的 manifest row 與 EMG。不吃分期點、不算任何分期時間範圍 —— 分期點順序
// 由入口在 load 之前驗證(validatePhasePair),區間由 computePhaseSync / resolvePhaseRange
// 解析。
//
// validateEMGFilePath 內部解析並存入 ctx.baseFolder，後續 Motion / Force 與此處的
// EMG 解析共用；三者皆走 manifest.OpenDataFile 這道 fused 安全門。
//
// EMG 走 validate-twice 結構：validateEMGFilePath 先開門驗證 + 立即 Close，此處
// 經 [[Subject source]](manifest.LoadEMG)再開門一次取得 reader 交給 parser。重開是
// validate-pipeline 的固有特性,門內原子化 validated-open 保證每次都安全。
//
// 驗證仍完整 parse Motion / ANC 檔只為讀最後一筆 index / 時間(validateMotionFile /
// validateForceFile);改成只讀尾端不在 ADR-0047 範圍內。
func (analyzer *PhaseSyncAnalyzer) load(
	manifestFile, dataFolder string, subjectIndex int,
) (*models.PhaseManifest, *models.PhaseSyncEMGData, error) {
	ctx := &validationContext{manifestFile: manifestFile, dataFolder: dataFolder, subjectIndex: subjectIndex}
	if err := analyzer.runValidationPipeline(ctx); err != nil {
		return nil, nil, err
	}

	emgData, err := manifest.LoadEMG(ctx.baseFolder, &ctx.manifest)
	if err != nil {
		return nil, nil, i18n.WrapError(err, i18n.KeyErrorPhaseSyncParseEMGFailed)
	}

	row := ctx.manifest

	return &row, emgData, nil
}

// ErrNegativePhaseTime 表示請求的 phase point force-time 為負,phase_sync 拒絕用負時間
// 做 `time * frequency` 取 index 的計算(可能 silently 撞 motion-index < 1 boundary
// 或下游 sliding window panic)。在 resolvePhaseRange 入口顯式 reject。
//
// 注意:這只 reject 對應到 force-time 的 phase point(P0/P1/P2/S/C/T0/T/L);
// motion-index 型 phase point(D/O)是 frame number,非時間,負值已由 ValidatePhaseManifest
// 在 validateMotionIndexOrder 攔下。負時間在 parseFloat 階段是合法的(機械校準前
// 偏移,見 muscle_ratio TestAnalyze_NegativeTime_Handled),但只能存活到 batch
// 時間序列 dump(Output 1);phase_sync 真正取 time-range 才必須擋。
//
// 帶 i18n key 的 sentinel(ADR-0048):Error() 固定 zh-TW,errors.Is 照常以指標比對。
var ErrNegativePhaseTime = i18n.NewError(i18n.KeyErrorPhaseSyncNegativePhaseTime)

// resolvePhaseRange 把 manifest row m 的一對分期點解析為 EMG 時間範圍,並驗證該範圍
// 落在 emgData 時間範圍內。前提:分期點順序已由入口的 validatePhasePair 驗過(名稱
// 合法、start 嚴格早於 end),這裡不重驗。
//
// 兩端的 EMG 秒數取自 manifest row 的 [[Phase timeline]]（synchronizer.NewPhaseTimeline）；
// phase_sync 只保留自己的 policy：兩端都必須提供（ErrPhaseValueZero）、開始不得晚於
// 結束（ErrStartTimeAfterEnd）、區間須落在 EMG 資料內（validateEMGTimeRange）。
// 錯誤訊息全文維持既有字樣（含「計算分期時間範圍失敗」「計算同步時間範圍失敗」前綴）。
//
// 在計算 PhaseTimeRange 之前先 reject「對應到 force-time 的負 phase point」。
// 參考 muscle_ratio TestAnalyze_NegativeTime_Handled 的設計取捨 — 負時間在
// manifest parse / muscle_ratio batch 是合法輸入,但 phase_sync 用「time × frequency」
// 算 motion-index 對負時間沒有有效意義,fail-fast 比 silently 計算錯誤 index 安全。
func resolvePhaseRange(
	emgData *models.PhaseSyncEMGData,
	m *models.PhaseManifest,
	startPhase, endPhase models.PhasePoint,
) (*models.PhaseTimeRange, error) {
	if err := rejectNegativeForceTime(&m.PhasePoints, startPhase); err != nil {
		return nil, err
	}
	if err := rejectNegativeForceTime(&m.PhasePoints, endPhase); err != nil {
		return nil, err
	}

	timeline := synchronizer.NewPhaseTimeline(m)
	startTime, ok := timeline.At(startPhase)
	if !ok {
		return nil, i18n.WrapError(
			i18n.WrapError(ErrPhaseValueZero, i18n.KeyErrorPhaseSyncStartPhaseNotSet, startPhase),
			i18n.KeyErrorPhaseSyncPhaseRangeFailed)
	}
	endTime, ok := timeline.At(endPhase)
	if !ok {
		return nil, i18n.WrapError(
			i18n.WrapError(ErrPhaseValueZero, i18n.KeyErrorPhaseSyncEndPhaseNotSet, endPhase),
			i18n.KeyErrorPhaseSyncPhaseRangeFailed)
	}
	if startTime > endTime {
		return nil, i18n.WrapError(
			i18n.WrapError(
				i18n.WrapError(ErrStartTimeAfterEnd, i18n.KeyErrorPhaseSyncStartAfterEnd, startTime, endTime),
				i18n.KeyErrorPhaseSyncSyncRangeFailed),
			i18n.KeyErrorPhaseSyncPhaseRangeFailed)
	}

	phaseTimeRange := &models.PhaseTimeRange{StartTime: startTime, EndTime: endTime}

	if err := validateEMGTimeRange(emgData, phaseTimeRange, m.EMGMotionOffset); err != nil {
		return nil, err
	}

	return phaseTimeRange, nil
}

// rejectNegativeForceTime 對單一 phase point 做「force-time 不可為負」防線。
// motion-index 型 phase point(D/O)直接 pass — 它們是 frame number 不是時間,
// 由 ValidatePhaseManifest 的 validateMotionIndexOrder 把關;未設定(Set=false)
// 也直接 pass — 由 resolvePhaseRange 後續的 timeline 查詢回 ErrPhaseValueZero。
func rejectNegativeForceTime(points *models.PhasePoints, phase models.PhasePoint) error {
	opt, isMotionIndex, err := parsers.GetPhaseValue(points, phase)
	if err != nil {
		return i18n.WrapError(err, i18n.KeyErrorPhaseSyncParsePhaseValueFailed, phase)
	}
	if isMotionIndex {
		return nil
	}
	v, ok := opt.Get()
	if !ok {
		return nil
	}
	if v < 0 {
		return fmt.Errorf("%w: %s = %v", ErrNegativePhaseTime, phase, v)
	}
	return nil
}

// AnalyzePhaseSync 執行分期同步分析:分期點順序(任何 I/O 之前)→ load → computePhaseSync。
//
// ctx 在進入點與 computePhaseSync 的 step 邊界檢查,caller 可在分析中途取消整段工作
// （Wails Shutdown / 使用者中止）。file load 與 stats 計算本身仍同步執行。
//
// 錯誤全文與搬移前相同(ADR-0047 只改了順序錯誤的先後:它排在 manifest / 檔案錯誤
// 之前);不帶 Stage —— gui 對 AnalyzePhaseSync 只用一個訊息前綴。
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(
	ctx context.Context, params *models.AnalysisParams,
) (*models.EMGStatistics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := analyzer.validatePhasePair(params.StartPhase, params.EndPhase); err != nil {
		return nil, err
	}

	m, emgData, err := analyzer.load(params.ManifestFile, params.DataFolder, params.SubjectIndex)
	if err != nil {
		return nil, err
	}

	stats, err := analyzer.computePhaseSync(ctx, emgData, m, params.StartPhase, params.EndPhase)
	if err != nil {
		return nil, phaseSyncComputeError(err)
	}

	return stats, nil
}

// phaseSyncComputeError 把 computePhaseSync 的 *AnalysisError 換回 AnalyzePhaseSync 既有的
// 錯誤全文:切片與統計兩步各加自己的前綴,區間解析錯誤原樣。NPS 的這兩步改由 gui 依
// Stage 選前綴,不加這兩段字。ctx 錯誤不是 *AnalysisError,原樣回傳。
func phaseSyncComputeError(err error) error {
	var stageErr *AnalysisError
	if !errors.As(err, &stageErr) {
		return err
	}

	switch stageErr.Stage {
	case StageStatsSlice:
		return i18n.WrapError(stageErr.Err, i18n.KeyErrorPhaseSyncExtractRangeFailed)
	case StageStatistics:
		return i18n.WrapError(stageErr.Err, i18n.KeyErrorPhaseSyncCalcStatsFailed)
	default:
		return stageErr.Err
	}
}

// computePhaseSync 是兩個入口共用的 compute core(file-free,仿 ADR-0024 的 computeCCI;
// ADR-0047):在 emgData 上解析 manifest row m 的 [startPhase, endPhase] → 切片 → 統計。
// AnalyzePhaseSync 傳原始 EMG,AnalyzeNormalizedPhaseSync 傳標準化後的 EMG。
//
// 前提:分期點順序已由入口驗過(validatePhasePair)。統計的 StartTime / EndTime 是切片後
// 實際選到的第一 / 最後一筆 sample 時間(synchronizer.SliceEMG)。
//
// 錯誤:ctx 取消原樣回 ctx.Err();其餘為 *AnalysisError —— StageStatsRange(區間解析)、
// StageStatsSlice(切片)、StageStatistics(統計),Err 是未加前綴的 cause。
func (analyzer *PhaseSyncAnalyzer) computePhaseSync(
	ctx context.Context,
	emgData *models.PhaseSyncEMGData,
	m *models.PhaseManifest,
	startPhase, endPhase models.PhasePoint,
) (*models.EMGStatistics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phaseTimeRange, err := resolvePhaseRange(emgData, m, startPhase, endPhase)
	if err != nil {
		return nil, &AnalysisError{Stage: StageStatsRange, Err: err}
	}

	rangeResult, err := synchronizer.SliceEMG(emgData, phaseTimeRange.StartTime, phaseTimeRange.EndTime)
	if err != nil {
		return nil, &AnalysisError{Stage: StageStatsSlice, Err: err}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stats, err := analyzer.statsCalculator.CalculateStatistics(
		rangeResult.Data,
		calculator.StatisticsParams{
			Subject:    m.Subject,
			StartPhase: startPhase,
			StartTime:  rangeResult.ActualStartTime,
			EndPhase:   endPhase,
			EndTime:    rangeResult.ActualEndTime,
		},
	)
	if err != nil {
		return nil, &AnalysisError{Stage: StageStatistics, Err: err}
	}

	return stats, nil
}

// validateEMGTimeRange 驗證 EMG 時間範圍.
func validateEMGTimeRange(
	emgData *models.PhaseSyncEMGData,
	phaseTimeRange *models.PhaseTimeRange,
	emgMotionOffset int,
) error {
	// nil 或空 Time slice 無法做範圍比對,且空 EMG 資料是上游 contract 缺陷
	// (對齊 synchronizer.SliceEMG 的 nil/empty guard)。
	// 空 Time 時原本 emgMinTime=emgMaxTime=0.0,任何 StartTime<0 都通過,
	// 反而默許了錯位結果 — fail-fast 比 silent miscompute 安全。
	if emgData == nil || len(emgData.Time) == 0 {
		return i18n.WrapError(parsers.ErrNilData, i18n.KeyErrorPhaseSyncEMGEmpty)
	}

	emgMinTime := emgData.Time[0]
	emgMaxTime := emgData.Time[len(emgData.Time)-1]

	// 越界判斷走 [[EMG time axis]] 的共用規則(synchronizer.OutsideEMG,±emgTimeEpsilon):
	// [[Phase timeline]] 經 ForceTimeToEMGTime 同步後的 ULP 飄移不誤拒,與 CCI /
	// muscle_ratio 同一容差(ADR-0043)。
	if before, _ := synchronizer.OutsideEMG(emgData.Time, phaseTimeRange.StartTime); before {
		return i18n.WrapError(ErrEMGTimeOutOfRange, i18n.KeyErrorPhaseSyncEMGStartBeforeMin,
			phaseTimeRange.StartTime, emgMinTime, emgMotionOffset)
	}

	if _, after := synchronizer.OutsideEMG(emgData.Time, phaseTimeRange.EndTime); after {
		return i18n.WrapError(ErrEMGTimeOutOfRange, i18n.KeyErrorPhaseSyncEMGEndAfterMax,
			phaseTimeRange.EndTime, emgMaxTime, emgMotionOffset)
	}

	return nil
}

// LoadManifestSubjects 載入分期總檔案中的所有主題.
func (analyzer *PhaseSyncAnalyzer) LoadManifestSubjects(manifestPath string) ([]string, error) {
	manifests, err := manifest.LoadManifests(manifestPath)
	if err != nil {
		return nil, i18n.WrapError(err, i18n.KeyErrorPhaseSyncParseManifestFailed)
	}

	subjects := make([]string, len(manifests))
	for i := range manifests {
		subjects[i] = manifests[i].Subject
	}

	return subjects, nil
}
