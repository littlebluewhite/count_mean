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
	"sync/atomic"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
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
)

// PhaseSyncAnalyzer 分期同步分析器.
type PhaseSyncAnalyzer struct {
	manifestParser  *parsers.PhaseManifestParser
	emgParser       *parsers.EMGParser
	motionParser    *parsers.MotionParser
	ancParser       *parsers.ANCParser
	phaseCalculator *synchronizer.PhaseCalculator
	statsCalculator *calculator.EMGStatisticsCalculator
}

// NewPhaseSyncAnalyzer 創建新的分期同步分析器.
// 不持有 PathValidator — manifest 檔案路徑解析改走無狀態的 security.ResolveLenientPath
// （見 validateEMGFilePath），無 request-scoped validator 狀態需要管理。
func NewPhaseSyncAnalyzer() *PhaseSyncAnalyzer {
	return &PhaseSyncAnalyzer{
		manifestParser:  parsers.NewPhaseManifestParser(),
		emgParser:       parsers.NewEMGParser(),
		motionParser:    parsers.NewMotionParser(),
		ancParser:       parsers.NewANCParser(),
		phaseCalculator: synchronizer.NewPhaseCalculator(),
		statsCalculator: calculator.NewEMGStatisticsCalculator(),
	}
}

// validationContext 驗證上下文，用於在驗證步驟之間傳遞數據.
// baseFolder 在 validateEMGFilePath 內解析後存入，後續 validateMotionFile /
// validateForceFile 共用。路徑解析改走無狀態的 security.ResolveLenientPath，
// ctx 不再持有 PathValidator — 避免並發污染，且接受 BTS 字面 "%" 檔名。
type validationContext struct {
	params      *models.AnalysisParams
	manifests   []models.PhaseManifest
	manifest    models.PhaseManifest
	baseFolder  string
	emgFilePath string
}

// validationStep 定義驗證步驟函數類型.
type validationStep func(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error

// validateManifestFile 驗證分期總檔案.
func validateManifestFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	manifests, err := analyzer.manifestParser.ParseFile(ctx.params.ManifestFile)
	if err != nil {
		return fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	ctx.manifests = manifests

	return nil
}

// validateSubjectIndex 驗證主題索引.
func validateSubjectIndex(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.params.SubjectIndex < 0 || ctx.params.SubjectIndex >= len(ctx.manifests) {
		return fmt.Errorf("無效的主題索引: %d (共有 %d 個主題): %w",
			ctx.params.SubjectIndex, len(ctx.manifests), ErrInvalidSubjectIndex)
	}

	ctx.manifest = ctx.manifests[ctx.params.SubjectIndex]

	return nil
}

// validateManifestData 驗證分期總檔案數據.
func validateManifestData(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if err := parsers.ValidatePhaseManifest(&ctx.manifest); err != nil {
		return fmt.Errorf("分期總檔案數據驗證失敗: %w", err)
	}

	return nil
}

// validatePhaseOrder 驗證分期點順序.
func validatePhaseOrder(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(ctx.params.StartPhase, ctx.params.EndPhase); err != nil {
		return fmt.Errorf("分期點順序驗證失敗: %w", err)
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
// EMG / Motion / Force 路徑解析走 security.ResolveLenientPath（允許含 literal "%"
// 的 BTS 匯出檔名，如 "NSF_1_BTS%_*.csv"），對齊 cci / muscle_ratio 與 lenient_path.go
// 的 decision matrix。ResolveLenientPath 內部已對 joined 與 baseFolder 做
// EvalSymlinks + Rel 邊界檢查（涵蓋舊版「再解析 symlink 後重新 ValidateFilePath」
// 那一步的 symlink-escape 防護）；不再額外跑 strict ValidateFilePath —— 後者
// strictPercent=true 會把合法的字面 "%" 誤判為 URL-encoded 殘留而拒載。
func validateEMGFilePath(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	baseFolder := ctx.params.DataFolder
	if info, err := os.Stat(baseFolder); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("資料夾不存在 (%s): %w", baseFolder, ErrBaseFolderNotFound)
		}
		return fmt.Errorf("資料夾狀態檢查失敗 (%s): %w", baseFolder, err)
	} else if !info.IsDir() {
		return fmt.Errorf("資料夾路徑非目錄 (%s): %w", baseFolder, ErrBaseFolderNotFound)
	}

	if resolvedBase, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolvedBase
	}

	ctx.baseFolder = baseFolder

	emgFilePath, err := security.ResolveLenientPath(baseFolder, ctx.manifest.EMGFile)
	if err != nil {
		return fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
	}

	ctx.emgFilePath = emgFilePath

	return nil
}

// validateMotionFile 驗證 Motion 檔案.
//
//nolint:dupl // Similar to validateForceFile but handles different data type
func validateMotionFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.manifest.MotionFile == "" {
		return nil
	}

	motionFilePath, err := security.ResolveLenientPath(ctx.baseFolder, ctx.manifest.MotionFile)
	if err != nil {
		return fmt.Errorf("Motion 檔案路徑驗證失敗: %w", err) //nolint:staticcheck // Chinese error message
	}

	if _, err := os.Stat(motionFilePath); os.IsNotExist(err) {
		//nolint:staticcheck // Chinese error message for user display
		return fmt.Errorf("Motion 檔案不存在 (%s): %w", motionFilePath, ErrFileNotFound)
	}

	motionData, err := analyzer.motionParser.ParseFile(motionFilePath)
	if err != nil {
		return fmt.Errorf("解析 Motion 檔案失敗: %w", err)
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
		//nolint:staticcheck // Chinese error message for user display
		return fmt.Errorf("D 分期點 index %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.PhasePoints.D, maxMotionIndex, ErrPhasePointOutOfRange)
	}

	if manifest.PhasePoints.O > 0 && manifest.PhasePoints.O > maxMotionIndex {
		//nolint:staticcheck // Chinese error message for user display
		return fmt.Errorf("O 分期點 index %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.PhasePoints.O, maxMotionIndex, ErrPhasePointOutOfRange)
	}

	if manifest.EMGMotionOffset > 0 && manifest.EMGMotionOffset > maxMotionIndex {
		return fmt.Errorf("EMGMotionOffset %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.EMGMotionOffset, maxMotionIndex, ErrPhasePointOutOfRange)
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

	forceFilePath, err := security.ResolveLenientPath(ctx.baseFolder, ctx.manifest.ForceFile)
	if err != nil {
		return fmt.Errorf("Force Plate 檔案路徑驗證失敗: %w", err) //nolint:staticcheck // Chinese error message
	}

	if _, err := os.Stat(forceFilePath); os.IsNotExist(err) {
		//nolint:staticcheck // Chinese error message for user display
		return fmt.Errorf("Force Plate 檔案不存在 (%s): %w", forceFilePath, ErrFileNotFound)
	}

	forceData, err := analyzer.ancParser.ParseFile(forceFilePath)
	if err != nil {
		return fmt.Errorf("解析 Force Plate 檔案失敗: %w", err)
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
			return fmt.Errorf("%s 分期點時間 %.3f 超出 Force Plate 數據範圍 (最大: %.3f): %w",
				phase, value, maxForceTime, ErrPhasePointOutOfRange)
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
		validatePhaseOrder,
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

// LoadedPhaseSyncContext 表示完成驗證與 EMG 解析的中間結果，供需要相同前置步驟
// 的多個分析流程共用。PhaseTimeRange 由 LoadAndExtractRange 在內部一併計算後填入；
// 直接呼叫 Load 取得的 context 中 PhaseTimeRange 為 nil，需後續呼叫 ResolvePhaseRange
// 取得具體區間。
type LoadedPhaseSyncContext struct {
	Manifest       *models.PhaseManifest
	EMGData        *models.PhaseSyncEMGData
	PhaseTimeRange *models.PhaseTimeRange
}

// parseEMGFileFunc 是 EMG 檔案解析 test hook 的函式型別 alias。
// 用 named type 讓 atomic.Pointer 的型別參數可讀。
type parseEMGFileFunc func(filepath string) (*models.PhaseSyncEMGData, float64, error)

// parseEMGFileFnPtr 是 EMG 檔案解析的 test hook (P2-H24, P2-25)。
//
// Production 預設為 nil pointer → Load() 用 analyzer.emgParser.ParseFile;test 可
// override (用 setParseEMGFileFnForTest) 注入 fake parser,讓 in-flight cancel
// test 可以「卡住 ParseFile 直到 cancel 被觸發」確定性化測試 cancel path。
//
// 從裸 package-level var 改 atomic.Pointer 以提供讀寫同步。第一個
// t.Parallel() test 會踩到 data race — atomic.Pointer.Load/Store 提供 happens-before
// 保證,讓 race detector 通過。
//
// 為何維持 package-level (而非 struct field DI):
//   - struct field 改型別 (從 *parsers.EMGParser → interface) 屬於 invasive change,
//     違反 surgical changes 原則。
//   - package-level atomic hook 對 test 是 zero-cost,production 行為完全不變。
//   - test 用 t.Cleanup 還原成 nil pointer,避免污染其他 test。
//
//nolint:gochecknoglobals // test hook by design; nil pointer in production
var parseEMGFileFnPtr atomic.Pointer[parseEMGFileFunc]

// SetParseEMGFileFnForTest 注入 EMG 解析 test hook,並回傳 cleanup 函式還原 hook。
// caller (test) 應該用 t.Cleanup 註冊 cleanup,避免污染其他 test。
//
// fn 為 nil 表示「不注入 fake,使用 production parser」;Production code path 也是
// nil pointer。
func SetParseEMGFileFnForTest(fn parseEMGFileFunc) (cleanup func()) {
	previous := parseEMGFileFnPtr.Load()
	if fn == nil {
		parseEMGFileFnPtr.Store(nil)
	} else {
		parseEMGFileFnPtr.Store(&fn)
	}
	return func() { parseEMGFileFnPtr.Store(previous) }
}

// Load 執行分期同步分析的前置載入流程：路徑驗證、manifest 解析、EMG 檔案解析。
// 不計算任何分期時間範圍（PhaseTimeRange 為 nil），供需要對同一份載入資料解析多組
// 不同分期區間的工作流共用（例如標準化分期同步分析需要分別解析 normalize 範圍與
// statistics 範圍）。後續呼叫 ResolvePhaseRange 取得具體時間區間。
//
// validateEMGFilePath 內部解析並存入 ctx.baseFolder，後續 Motion / Force 路徑解析
// 共用；三者皆走無狀態的 security.ResolveLenientPath，無 request-scoped validator。
//
// 走 parseEMGFileFn test hook 取得 parser (production 預設 nil → 走
// analyzer.emgParser.ParseFile)。
func (analyzer *PhaseSyncAnalyzer) Load(
	params *models.AnalysisParams,
) (*LoadedPhaseSyncContext, error) {
	ctx := &validationContext{params: params}
	if err := analyzer.runValidationPipeline(ctx); err != nil {
		return nil, err
	}

	parseFn := analyzer.emgParser.ParseFile
	if hook := parseEMGFileFnPtr.Load(); hook != nil {
		parseFn = *hook
	}

	emgData, _, err := parseFn(ctx.emgFilePath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	manifestCopy := ctx.manifest

	return &LoadedPhaseSyncContext{
		Manifest:       &manifestCopy,
		EMGData:        emgData,
		PhaseTimeRange: nil,
	}, nil
}

// ErrNegativePhaseTime 表示請求的 phase point force-time 為負,phase_sync 拒絕用負時間
// 做 `time * frequency` 取 index 的計算(可能 silently 撞 motion-index < 1 boundary
// 或下游 sliding window panic)。在 ResolvePhaseRange 入口顯式 reject。
//
// 注意:這只 reject 對應到 force-time 的 phase point(P0/P1/P2/S/C/T0/T/L);
// motion-index 型 phase point(D/O)是 frame number,非時間,負值已由 ValidatePhaseManifest
// 在 validateMotionIndexOrder 攔下。負時間在 parseFloat 階段是合法的(機械校準前
// 偏移,見 muscle_ratio TestAnalyze_NegativeTime_Handled),但只能存活到 batch
// 時間序列 dump(Output 1);phase_sync 真正取 time-range 才必須擋。
var ErrNegativePhaseTime = errors.New("phase point force-time 為負值,phase_sync 不接受")

// ResolvePhaseRange 從已 Load 的 context 解析指定的一對分期點為時間範圍，
// 並驗證該範圍落在 EMG 資料時間範圍內。同一個 LoadedPhaseSyncContext 可重複呼叫
// 此函式以取得多組不同分期區間。
//
// 先呼叫 phaseCalculator.ValidatePhaseOrder 顯式驗證 start < end（嚴格小於）
// 與 phase 名稱合法性。這個驗證對「直接走 Load + ResolvePhaseRange」的呼叫端
// （例如標準化分期同步分析對 Stats 那組區間）至關重要——否則 start == end 的
// 退化情形會穿透到 GetPhaseTimeRange，產生 zero-duration 區間。對「走
// LoadAndExtractRange」的呼叫端則與 Load 內 pipeline 的 validatePhaseOrder
// 形成冗餘檢查（無害）。
//
// 在計算 PhaseTimeRange 之前先 reject「對應到 force-time 的負 phase point」。
// 參考 muscle_ratio TestAnalyze_NegativeTime_Handled 的設計取捨 — 負時間在
// manifest parse / muscle_ratio batch 是合法輸入,但 phase_sync 用「time × frequency」
// 算 motion-index 對負時間沒有有效意義,fail-fast 比 silently 計算錯誤 index 安全。
func (analyzer *PhaseSyncAnalyzer) ResolvePhaseRange(
	loaded *LoadedPhaseSyncContext,
	startPhase, endPhase models.PhasePoint,
) (*models.PhaseTimeRange, error) {
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(startPhase, endPhase); err != nil {
		return nil, fmt.Errorf("分期點順序驗證失敗: %w", err)
	}

	if err := rejectNegativeForceTime(&loaded.Manifest.PhasePoints, startPhase); err != nil {
		return nil, err
	}
	if err := rejectNegativeForceTime(&loaded.Manifest.PhasePoints, endPhase); err != nil {
		return nil, err
	}

	phaseTimeRange, err := analyzer.phaseCalculator.GetPhaseTimeRange(
		loaded.Manifest.PhasePoints,
		startPhase,
		endPhase,
		loaded.Manifest.EMGMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算分期時間範圍失敗: %w", err)
	}

	if err := validateEMGTimeRange(loaded.EMGData, phaseTimeRange, loaded.Manifest.EMGMotionOffset); err != nil {
		return nil, err
	}

	return phaseTimeRange, nil
}

// rejectNegativeForceTime 對單一 phase point 做「force-time 不可為負」防線。
// motion-index 型 phase point(D/O)直接 pass — 它們是 frame number 不是時間,
// 由 ValidatePhaseManifest 的 validateMotionIndexOrder 把關;未設定(Set=false)
// 也直接 pass — 由下游 GetPhaseTimeRange 回 ErrPhaseValueZero。
func rejectNegativeForceTime(points *models.PhasePoints, phase models.PhasePoint) error {
	opt, isMotionIndex, err := parsers.GetPhaseValue(points, phase)
	if err != nil {
		return fmt.Errorf("解析分期點 %s 失敗: %w", phase, err)
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

// LoadAndExtractRange 為 Load + ResolvePhaseRange 的薄包裝，保留給只需要單一
// 分期區間的呼叫端使用（例如 AnalyzePhaseSync）。需要多組區間時請改用 Load
// 搭配多次 ResolvePhaseRange。
func (analyzer *PhaseSyncAnalyzer) LoadAndExtractRange(
	params *models.AnalysisParams,
) (*LoadedPhaseSyncContext, error) {
	loaded, err := analyzer.Load(params)
	if err != nil {
		return nil, err
	}

	phaseTimeRange, err := analyzer.ResolvePhaseRange(loaded, params.StartPhase, params.EndPhase)
	if err != nil {
		return nil, err
	}

	loaded.PhaseTimeRange = phaseTimeRange

	return loaded, nil
}

// AnalyzePhaseSync 執行分期同步分析。
//
// ctx 在每個 step 邊界檢查（load / extract / stats），caller 可在分析中途取消
// 整段工作（Wails Shutdown / 使用者中止）。內部的 file load 與 stats 計算本身
// 仍同步執行，未來若改為 streaming 可再深度 plumb ctx。
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(
	ctx context.Context, params *models.AnalysisParams,
) (*models.EMGStatistics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	loaded, err := analyzer.LoadAndExtractRange(params)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 6. 提取指定時間範圍的 EMG 數據
	rangeResult, err := parsers.GetEMGDataInTimeRange(
		loaded.EMGData,
		loaded.PhaseTimeRange.StartTime,
		loaded.PhaseTimeRange.EndTime,
	)
	if err != nil {
		return nil, fmt.Errorf("提取 EMG 時間範圍數據失敗: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 7. 計算統計信息
	stats, err := analyzer.statsCalculator.CalculateStatistics(
		rangeResult.Data,
		calculator.StatisticsParams{
			Subject:    loaded.Manifest.Subject,
			StartPhase: params.StartPhase,
			StartTime:  rangeResult.ActualStartTime,
			EndPhase:   params.EndPhase,
			EndTime:    rangeResult.ActualEndTime,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("計算統計信息失敗: %w", err)
	}

	return stats, nil
}

// validateEMGTimeRange 驗證 EMG 時間範圍.
func validateEMGTimeRange(
	emgData *models.PhaseSyncEMGData,
	phaseTimeRange *models.PhaseTimeRange,
	emgMotionOffset int,
) error {
	emgMinTime := 0.0
	emgMaxTime := 0.0

	if len(emgData.Time) > 0 {
		emgMinTime = emgData.Time[0]
		emgMaxTime = emgData.Time[len(emgData.Time)-1]
	}

	if phaseTimeRange.StartTime < emgMinTime {
		return fmt.Errorf(
			"計算出的 EMG 開始時間 %.3f 小於 EMG 數據最小時間 %.3f (offset: %d): %w",
			phaseTimeRange.StartTime, emgMinTime, emgMotionOffset, ErrEMGTimeOutOfRange)
	}

	if phaseTimeRange.EndTime > emgMaxTime {
		return fmt.Errorf(
			"計算出的 EMG 結束時間 %.3f 超出 EMG 數據範圍 (最大: %.3f, offset: %d): %w",
			phaseTimeRange.EndTime, emgMaxTime, emgMotionOffset, ErrEMGTimeOutOfRange)
	}

	return nil
}

// LoadManifestSubjects 載入分期總檔案中的所有主題.
func (analyzer *PhaseSyncAnalyzer) LoadManifestSubjects(manifestPath string) ([]string, error) {
	manifests, err := analyzer.manifestParser.ParseFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	subjects := make([]string, len(manifests))
	for i := range manifests {
		subjects[i] = manifests[i].Subject
	}

	return subjects, nil
}

// GenerateAnalysisReport 生成分析報告.
func GenerateAnalysisReport(stats *models.EMGStatistics) string {
	return calculator.FormatStatisticsReport(stats)
}
