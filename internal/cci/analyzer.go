package cci

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"

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
	"count_mean/util"
)

// ErrInvalidGaitCycle 表示步態週期區間 start/end 不合法（duration <= 0）。
var ErrInvalidGaitCycle = errors.New("無效的步態週期區間")

// ErrPairLengthMismatch 表示 PairResults[i].Values 長度與 TimeValues 不對齊。
// CCI spec 要求所有 pair 樣本與 time 軸 1:1 對應；下游圖表 / 統計 / 降採樣
// 都仰賴此 invariant。違反時應 fail-fast，避免 silent X-axis drift。
var ErrPairLengthMismatch = errors.New("CCI pair 樣本長度與時間軸不一致")

// CCIParams holds parameters for CCI analysis.
type CCIParams struct {
	ManifestFile string
	DataFolder   string
	SubjectIndex int
}

// CCIAnalyzer orchestrates the CCI Rudolph analysis pipeline.
//
// Concurrency model:
//   - PathValidator 不掛在 struct 上（request-scoped）— commit 1287572 修過的 race。
//   - timeSynchronizer 是 stateless（無 mutable field write），可安全共用。
//   - manifest loading 與 EMG file resolution 走 internal/manifest 套件（stateless package functions）。
type CCIAnalyzer struct {
	timeSynchronizer *synchronizer.TimeSynchronizer
	logger           *logging.Logger
}

// NewCCIAnalyzer creates a new CCI analyzer instance.
func NewCCIAnalyzer() *CCIAnalyzer {
	return &CCIAnalyzer{
		timeSynchronizer: synchronizer.NewTimeSynchronizer(),
		logger:           logging.GetLogger("cci_analyzer"),
	}
}

// AnalyzeCCI executes the full CCI Rudolph analysis pipeline.
//
// ctx 在每個 step 邊界檢查（pre-load / post-load / pre-compute），並下傳到
// computeAllPairs → computeSinglePair → CalculateCCITimeSeries 的 hot loop，
// caller 可在分析中途取消整段工作（Wails Shutdown / 使用者中止）。
// 對稱 phase_sync.AnalyzePhaseSync 的 ctx plumbing。
func (a *CCIAnalyzer) AnalyzeCCI(
	ctx context.Context, params *CCIParams,
) (*CCIAnalysisResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.logger.Info("開始 CCI Rudolph 分析", map[string]any{
		"manifest": params.ManifestFile,
		"folder":   params.DataFolder,
		"subject":  params.SubjectIndex,
	})

	manifest, err := a.loadAndValidate(params)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	emgData, err := a.loadEMGData(params.DataFolder, manifest)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	channelMap, err := BuildChannelMap(emgData.Headers)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIBuildChannelMapFailed), err)
	}

	gaitStart, gaitEnd, phasePercents, phaseTimes, err := a.calculateGaitCycle(
		manifest, emgData)
	if err != nil {
		return nil, err
	}

	rangeResult, err := parsers.GetEMGDataInTimeRange(emgData, gaitStart, gaitEnd)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIExtractGaitRangeFailed), err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result, err := a.computeAllPairs(
		ctx, manifest.Subject, rangeResult.Data, channelMap, phasePercents, phaseTimes)
	if err != nil {
		return nil, err
	}

	result.GaitStartTime = gaitStart
	result.GaitEndTime = gaitEnd

	a.logger.Info("CCI 分析完成", map[string]any{"subject": manifest.Subject})

	return result, nil
}

// loadAndValidate parses the manifest and validates the subject index.
//
//nolint:err113 // dynamic errors for user-facing output (i18n-backed)
func (a *CCIAnalyzer) loadAndValidate(params *CCIParams) (*models.PhaseManifest, error) {
	manifests, err := manifest.LoadManifests(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIParseManifestFailed), err)
	}

	if params.SubjectIndex < 0 || params.SubjectIndex >= len(manifests) {
		return nil, errors.New(
			i18n.T(i18n.KeyErrorCCIInvalidSubjectIndex, params.SubjectIndex, len(manifests)))
	}

	return &manifests[params.SubjectIndex], nil
}

// loadEMGData resolves EMG file path and parses EMG data.
// 路徑解析委派給 manifest.ResolveEMGFile（內部走 security.ResolveLenientPath，
// 允許含字面 "%" 的 BTS 匯出檔名 — 見該套件 doc）。
func (a *CCIAnalyzer) loadEMGData(
	dataFolder string, m *models.PhaseManifest,
) (*models.PhaseSyncEMGData, error) {
	emgPath, err := manifest.ResolveEMGFile(dataFolder, m.EMGFile)
	if err != nil {
		return nil, err
	}

	emgData, _, err := parsers.NewEMGParser().ParseFile(emgPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIParseEMGFailed), err)
	}

	return emgData, nil
}

// phasePointDef defines a phase point for iteration.
//
// Batch T：value 從 float64 換成 OptFloat — calculateGaitCycle 用 OptFloat.Get()
// 判斷「該分期點是否已標定」，t=0 不再被誤判為未提供。
type phasePointDef struct {
	name          string
	value         models.OptFloat
	isMotionIndex bool
}

// getPhasePointDefs extracts all phase point definitions from a manifest.
// 用 models.AllPhases() + PhasePoint.IsMotionIndex() 集中 domain invariant，
// 避免「D 與 O 是 motion index」散落多處。
func getPhasePointDefs(manifest *models.PhaseManifest) []phasePointDef {
	allPhases := models.AllPhases()
	defs := make([]phasePointDef, 0, len(allPhases))

	for _, phase := range allPhases {
		// GetPhaseValue 只在 default case return error；allPhases 已涵蓋全部 10 個合法
		// case，error path 不可達，故 swallow。isMotionIndex 不用 GetPhaseValue 回的 bool，
		// 改用 phase.IsMotionIndex() 才能跟 models 的 domain invariant 對齊。
		opt, _, _ := parsers.GetPhaseValue(&manifest.PhasePoints, phase) //nolint:errcheck // unreachable error path
		defs = append(defs, phasePointDef{
			name:          string(phase),
			value:         opt,
			isMotionIndex: phase.IsMotionIndex(),
		})
	}

	return defs
}

// gaitMinDurationSampleCount 是 calculateGaitCycle reject「duration ≈ 0」邊界
// 的門檻 — 步態週期長度必須 ≥ gaitMinDurationSampleCount × sample interval。
//
// duration ≈ 0 (例 manifest 中相鄰 phase 時間幾乎相同) 會讓 phase percent
// 計算放大浮點誤差,實質無意義 (pct = (t - start) / duration 在 duration → 0
// 時失去 numerical stability),且 downstream 視覺化將整個 gait cycle 壓在
// 同一 x 位置。10 個 sample interval 對 1000Hz EMG = 10ms,既能擋掉退化情境
// 也不會誤殺真實短週期 (典型 gait cycle ≈ 1s,遠大於 10ms)。
const gaitMinDurationSampleCount = 10

// calculateGaitCycle determines gait cycle boundaries and phase percentages.
//
//nolint:err113 // dynamic error for user-facing output (i18n-backed)
func (a *CCIAnalyzer) calculateGaitCycle(
	manifest *models.PhaseManifest, emgData *models.PhaseSyncEMGData,
) (float64, float64, map[string]float64, map[string]float64, error) {
	points := getPhasePointDefs(manifest)
	emgTimes := make(map[string]float64)

	for _, pt := range points {
		v, ok := pt.value.Get()
		if !ok {
			continue
		}

		var emgTime float64
		if pt.isMotionIndex {
			// 同 muscle_ratio collectPhasePoints — int(v) 對極端大 v
			// (例如惡意 / 損毀 manifest 帶 1e15) 是 implementation-defined。
			// MaxReasonableMotionIndex (1e9) 是 parseInt cap 邊界,OptFloat
			// 路徑同樣 enforce 此 cap 防止下游 hot loop OOM / wrap-around。
			if v <= 0 || v > float64(parsers.MaxReasonableMotionIndex) {
				a.logger.Warn("motion-index 分期點越界,跳過", map[string]any{
					"subject": manifest.Subject,
					"phase":   pt.name,
					"value":   v,
					"cap":     parsers.MaxReasonableMotionIndex,
				})
				continue
			}
			emgTime = a.timeSynchronizer.MotionIndexToEMGTime(
				int(v), manifest.EMGMotionOffset)
		} else {
			emgTime = a.timeSynchronizer.ForceTimeToEMGTime(
				v, manifest.EMGMotionOffset)
		}

		emgTimes[pt.name] = emgTime
	}

	if len(emgTimes) < 2 {
		return 0, 0, nil, nil, errors.New(i18n.T(i18n.KeyErrorCCIInsufficientPhasePoints))
	}

	// Find min/max EMG times as gait cycle boundaries
	gaitStart := math.Inf(1)
	gaitEnd := math.Inf(-1)

	for _, t := range emgTimes {
		gaitStart = math.Min(gaitStart, t)
		gaitEnd = math.Max(gaitEnd, t)
	}

	// Validate against EMG data range
	if err := validateEMGBounds(emgData, gaitStart, gaitEnd); err != nil {
		return 0, 0, nil, nil, err
	}

	// duration ≈ 0 邊界守門 — 需要 sample_interval 推估,從 emgData.Time 取
	// 首兩筆差值;空資料 / 單筆 EMG 已被 validateEMGBounds 擋下,此處安全。
	//
	// estimateSampleInterval 現在帶 logger,fallback path 會 Warn,
	// 讓 audit log 能標記「sample interval 是估算 vs 預設」的歧異。
	duration := gaitEnd - gaitStart
	sampleInterval := estimateSampleInterval(emgData.Time, a.logger)
	minDuration := sampleInterval * float64(gaitMinDurationSampleCount)
	if duration < minDuration {
		return 0, 0, nil, nil, errors.New(
			i18n.T(i18n.KeyErrorCCIGaitDurationTooSmall, duration, gaitMinDurationSampleCount))
	}

	// Calculate phase percentages and keep actual times
	phasePercents := make(map[string]float64, len(emgTimes))
	phaseTimes := make(map[string]float64, len(emgTimes))

	// phase percent clamp 加 logger.Warn — 過去 silent clamp 把
	// 「manifest phase 落在 gait cycle 外」的資料品質問題藏在 [0,100] 範圍內。
	// 修法後在 clamp 觸發時 Warn,讓使用者能即時排查 manifest / sync offset
	// 是否需要調整。boundary case (pct == -0.0 / 100.0 + ULP) 視為正常浮點誤差
	// 不 Warn — 只對 |超出| > 1e-6 觸發,避免在合法 boundary 噴 spam。
	const clampWarnEpsilon = 1e-6
	for name, t := range emgTimes {
		pct := (t - gaitStart) / duration * 100
		if a.logger != nil && (pct < -clampWarnEpsilon || pct > 100+clampWarnEpsilon) {
			a.logger.Warn("phase percent 被 clamp 至 [0,100] 範圍 (phase 落在 gait cycle 外,請檢查 manifest 或 sync offset)", map[string]any{
				"phase":              name,
				"phase_emg_time":     t,
				"gait_start":         gaitStart,
				"gait_end":           gaitEnd,
				"raw_pct":            pct,
				"clamped_pct":        math.Max(0, math.Min(100, pct)),
				"clamp_warn_epsilon": clampWarnEpsilon,
			})
		}
		phasePercents[name] = math.Max(0, math.Min(100, pct))
		phaseTimes[name] = t
	}

	return gaitStart, gaitEnd, phasePercents, phaseTimes, nil
}

// estimateSampleInterval 估算 EMG sample interval (秒/筆)。
//
// 取前兩筆時間差;若資料不足 2 筆 (邏輯上不會發生,但 defensive) 或差值非正,
// fallback 為 0.001s (1000Hz BTS 力板 default)。後者僅作為 sentinel 讓 門檻
// 仍能計算出合理閾值,不影響正常資料路徑。
//
// fallback 路徑加 logger.Warn — 過去 silently fallback 等於把 caller
// 的「資料品質問題」(times 倒退、單筆 EMG、相同時間戳) 偷藏在 sample interval
// 估算裡,下游 duration 守門用了 default 0.001 反推結果仍會 PASS,但實際
// 計算的 phase percent 等下游數值是無意義的。Logger 可選 (nil-safe) — 同套
// 函式被 analyzer / chart / muscle_ratio 等多個 caller 用,部分 path 沒 logger
// 也得能 fallback,因此採 optional logger pattern。
func estimateSampleInterval(times []float64, logger *logging.Logger) float64 {
	const defaultInterval = 0.001
	if len(times) < 2 {
		if logger != nil {
			logger.Warn("estimateSampleInterval:資料不足 2 筆,fallback 至預設 sample interval", map[string]any{
				"len":              len(times),
				"default_interval": defaultInterval,
			})
		}
		return defaultInterval
	}
	dt := times[1] - times[0]
	if dt <= 0 {
		if logger != nil {
			logger.Warn("estimateSampleInterval:前兩筆時間差非正 (時間戳異常),fallback 至預設 sample interval", map[string]any{
				"t0":               times[0],
				"t1":               times[1],
				"dt":               dt,
				"default_interval": defaultInterval,
			})
		}
		return defaultInterval
	}
	return dt
}

// validateEMGBounds checks gait cycle times are within EMG data range.
//
// NaN/Inf guard — gaitStart/gaitEnd 上游從 manifest 推導,
// 若 manifest 帶有 NaN/Inf 時間值 (parse failure / divide-by-zero / corrupted
// data),原本 epsilon 比較對 NaN 永遠回 false (浮點規則:NaN cmp anything = false),
// validateEMGBounds 會 PASS 但 downstream phase percent 計算 (t - gaitStart) /
// duration 全變 NaN,整個 CCI 結果污染為 NaN。Inf 也類似 — Inf - finite = ±Inf,
// 觸發 division by Inf 產生 0/NaN。
//
// 修法:在 epsilon 比較前 fail-fast,把資料品質問題在 boundary 擋下。
// 對 emgMin/emgMax (來自 emgData.Time 首末筆) 也做 NaN/Inf 守門 — 雖然
// upstream parser 通常已過 NaN/Inf scan,defense-in-depth 仍應在這道閘有獨立判斷。
//
//nolint:err113 // dynamic error for user-facing output (i18n-backed)
func validateEMGBounds(
	emgData *models.PhaseSyncEMGData, gaitStart, gaitEnd float64,
) error {
	if len(emgData.Time) == 0 {
		return errors.New(i18n.T(i18n.KeyErrorCCIEMGEmpty))
	}

	// gaitStart/gaitEnd 必須為有限值
	if math.IsNaN(gaitStart) || math.IsInf(gaitStart, 0) {
		return fmt.Errorf("gaitStart 為非有限值 (NaN/Inf),manifest 推導步態起點失敗: %v", gaitStart)
	}
	if math.IsNaN(gaitEnd) || math.IsInf(gaitEnd, 0) {
		return fmt.Errorf("gaitEnd 為非有限值 (NaN/Inf),manifest 推導步態終點失敗: %v", gaitEnd)
	}

	emgMin := emgData.Time[0]
	emgMax := emgData.Time[len(emgData.Time)-1]

	// EMG 時間軸首末筆若為非有限值,後續 epsilon 比較全失準
	if math.IsNaN(emgMin) || math.IsInf(emgMin, 0) {
		return fmt.Errorf("EMG 時間軸首筆為非有限值: %v", emgMin)
	}
	if math.IsNaN(emgMax) || math.IsInf(emgMax, 0) {
		return fmt.Errorf("EMG 時間軸末筆為非有限值: %v", emgMax)
	}

	// 浮點時間比較加 epsilon 容忍。原本 1e-9 比 1000Hz sample interval
	// (1e-3) 還細 6 個量級,等於沒有 tolerance — 1000Hz force plate vs EMG 經
	// sync 後的 1e-7 ULP 飄移會被誤判 out-of-range。
	//
	// 1e-6 為「比 sample interval 細 3 個量級」的安全餘量:真實 out-of-range
	// (>= 1ms ≈ 1e-3) 仍會被擋下,浮點 ULP 飄移 (<= 1e-6) 被吸收。
	const boundsEpsilon = 1e-6

	if gaitStart+boundsEpsilon < emgMin {
		return errors.New(i18n.T(i18n.KeyErrorCCIGaitStartBelowEMGMin, gaitStart, emgMin))
	}

	if gaitEnd > emgMax+boundsEpsilon {
		return errors.New(i18n.T(i18n.KeyErrorCCIGaitEndAboveEMGMax, gaitEnd, emgMax))
	}

	return nil
}

// computeAllPairs calculates CCI for all 12 muscle pairs using raw time points.
// 各 pair 計算 CPU-bound 且彼此獨立（讀同一份唯讀 emgData / channelMap、寫獨立 slot），
// 用 errgroup.WithContext 跑滿 GOMAXPROCS；缺通道的 pair 留 zero-value (PairName == "")
// 作 skip 哨兵，在 main goroutine 過濾並彙整成 map，避免並行寫 map。
//
// ctx 透過 errgroup.WithContext 派生 gctx 傳給 computeSinglePair，任一 worker 看到
// gctx 取消即往上 return ctx.Err()，errgroup 會 cancel 所有兄弟 goroutine。
// Caller cancel ctx 時 g.Wait() 回 ctx.Err()，computeAllPairs 將其傳遞給 AnalyzeCCI。
func (a *CCIAnalyzer) computeAllPairs(
	ctx context.Context,
	subject string,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
	phasePercents map[string]float64,
	phaseTimes map[string]float64,
) (*CCIAnalysisResult, error) {
	pairs := DefaultMusclePairs()
	slots := make([]CCIResult, len(pairs))

	g, gctx := errgroup.WithContext(ctx)

	for i, pair := range pairs {
		g.Go(func() error {
			cciValues, err := a.computeSinglePair(gctx, pair, emgData, channelMap)
			if err != nil {
				return err
			}

			if cciValues == nil {
				return nil
			}

			// slot 寫入前重檢 ctx。即使 computeSinglePair 已成功回 cciValues,
			// 若 gctx 在 hot-loop 結束後但寫 slots 前被其他 sibling worker 或 caller
			// cancel,本 goroutine 仍可能寫入 partial result。雖然 slots[i] 只由
			// goroutine i 寫 + g.Wait() 後才讀 (技術上不是 data race),但 cancel
			// 之後不再產生任何觀察值是 ctx 契約的一部分;這條 guard 確保 cancel
			// 邊界乾淨,亦避免下游 hook / debug 路徑看到「cancel 之後仍有寫入」。
			select {
			case <-gctx.Done():
				return nil
			default:
			}

			slots[i] = CCIResult{
				PairName: pair.Name,
				Values:   cciValues,
			}

			return nil
		})
	}

	// 任一 worker 回 error（含 ctx.Err()）會被 errgroup 蒐集；缺通道走 nil slot 路徑，
	// 計算失敗（CalculateCCITimeSeries return err）則 swallow 進 nil slot 不擴散。
	// 只有 ctx 取消 / 不可預期錯誤會走到這裡。
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("compute CCI pairs: %w", err)
	}

	pairResults := make([]CCIResult, 0, len(slots))
	meanCurves := make(map[string][]float64, len(slots))

	for _, r := range slots {
		if r.PairName == "" {
			continue
		}

		pairResults = append(pairResults, r)
		meanCurves[r.PairName] = r.Values
	}

	return &CCIAnalysisResult{
		Subject:       subject,
		PairResults:   pairResults,
		TimeValues:    emgData.Time,
		PhasePercents: phasePercents,
		PhaseTimes:    phaseTimes,
		MeanCurves:    meanCurves,
	}, nil
}

// computeSinglePair calculates CCI for one muscle pair.
//
// ctx 下傳到 CalculateCCITimeSeries 的 hot loop，使長 EMG 序列可在 mid-flight 取消。
// 業務性失敗（缺通道、計算公式錯誤）回 (nil, nil) 由 caller 走 nil slot 跳過；
// 只有 ctx 取消會 surface (nil, ctx.Err())，讓 errgroup cancel 所有兄弟 goroutine。
func (a *CCIAnalyzer) computeSinglePair(
	ctx context.Context,
	pair MusclePair,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
) ([]float64, error) {
	header1, ok1 := channelMap[pair.Muscle1]
	header2, ok2 := channelMap[pair.Muscle2]

	if !ok1 || !ok2 {
		a.logger.Warn("跳過肌肉配對（缺少通道）", map[string]any{
			"pair": pair.Name,
		})

		return nil, nil //nolint:nilnil // 缺通道為合法 skip，與 ctx 取消區分
	}

	ch1Data := emgData.Channels[header1]
	ch2Data := emgData.Channels[header2]

	cciValues, err := CalculateCCITimeSeries(ctx, ch1Data, ch2Data)
	if err != nil {
		// ctx 取消必須 surface 給 errgroup，立即停下其他 11 個 pair 的 worker。
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		a.logger.Warn("CCI 計算失敗", map[string]any{
			"pair":  pair.Name,
			"error": err.Error(),
		})

		return nil, nil //nolint:nilnil // 業務錯誤 swallow，nil slot 由 caller 過濾
	}

	return cciValues, nil
}

// ExportToCSV exports CCI results to a CSV file.
// result.Subject 來自外部 manifest，需先做 SanitizeFileName 去除路徑分隔符與特殊字元，
// 否則 "../foo" 之類的值會讓 filepath.Join 寫到 outputDir 之外（路徑穿越）。
//
// ctx 為第一個參數,讓 caller 在大 dataset CSV 匯出中斷流程。
// pre-cancel 直接拒絕寫檔;writeCSVFile 的 emit loop 中也每 cciChartCtxCheckInterval
// 點檢查一次 ctx.Done。
func (a *CCIAnalyzer) ExportToCSV(
	ctx context.Context, result *CCIAnalysisResult, outputDir string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Defense-in-depth：附 dummy child 後 ValidateExternalPath 擋 traversal /
	// system-dir prefix（與 muscle_ratio.Analyze 對稱）。
	checkPath := filepath.Join(outputDir, "_validation_marker")
	if err := security.NewPathValidator(nil).ValidateExternalPath(checkPath); err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIOutputDirInvalid), err)
	}

	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIMkdirFailed), err)
	}

	safeSubject := calculator.SanitizeFileName(result.Subject)
	fileName := fmt.Sprintf("%s_CCI_Rudolph.csv", safeSubject)
	outputPath := filepath.Join(outputDir, fileName)

	// 過去版本對 writeCSVFile error 仍回 outputPath — caller 看到 "(path, err)"
	// 兩者皆非零,容易誤把 path 當「寫進去了但有 warn」處理。實際上 WriteCSVAtomic
	// 失敗時 tmp 檔已被 abort,outputPath 上不會有檔案(或為前次留下的舊版本)。
	// 改為:寫檔失敗時 return "" + err,讓 caller 無法誤用 path。
	if err := writeCSVFile(ctx, outputPath, result, a.logger); err != nil {
		return "", err
	}
	return outputPath, nil
}

// writeCSVFile writes the CCI result data to a CSV via csvutil.WriteCSVAtomic
// (tmp+rename atomic). NaN/Inf 處理保留 row-level 邏輯：
//   - cell 為 NaN/Inf → 輸出空字串（CSV 慣例代表 missing data）
//   - 若整 row 所有 pair 都 NaN/Inf → skip 整 row 並計入 droppedRowCount
//   - 結束時 log warning，與 large_file_handler 的 -Inf channel skip 邏輯一致
//
// ctx 為第一個參數,emit loop 中每 cciChartCtxCheckInterval 點檢查一次
// ctx.Done(),caller cancel 後立即停寫並回 ctx.Err。csvutil.WriteCSVAtomic
// 對 emit 回 error 會走 tmp file abort 路徑,不留下半成品。
func writeCSVFile(ctx context.Context, outputPath string, result *CCIAnalysisResult, logger *logging.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	duration := result.GaitEndTime - result.GaitStartTime
	if duration <= 0 {
		return fmt.Errorf("%w: start=%v end=%v", ErrInvalidGaitCycle, result.GaitStartTime, result.GaitEndTime)
	}

	header := []string{"Time (s)", "Gait Cycle (%)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	var droppedRowCount int
	numPoints := len(result.TimeValues)

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header: header,
		Emit: func(emit func([]string) error) error {
			for i := 0; i < numPoints; i++ {
				if i > 0 && i%cciChartCtxCheckInterval == 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
				}

				t := result.TimeValues[i]
				pct := (t - result.GaitStartTime) / duration * 100

				pairCells := make([]string, 0, len(result.PairResults))
				allNonFinite := true

				for _, pr := range result.PairResults {
					if i >= len(pr.Values) {
						pairCells = append(pairCells, "")
						continue
					}
					v := pr.Values[i]
					if math.IsNaN(v) || math.IsInf(v, 0) {
						pairCells = append(pairCells, "")
						continue
					}
					pairCells = append(pairCells, fmt.Sprintf("%.6f", v))
					allNonFinite = false
				}

				if len(result.PairResults) > 0 && allNonFinite {
					droppedRowCount++
					continue
				}

				row := []string{
					fmt.Sprintf("%.4f", t),
					fmt.Sprintf("%.2f", pct),
				}
				row = append(row, pairCells...)

				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	if droppedRowCount > 0 && logger != nil {
		logger.Warn("CCI 匯出 CSV 時跳過全 NaN/Inf 的 row", map[string]any{
			"dropped_rows": droppedRowCount,
			"total_rows":   numPoints,
			"output_path":  outputPath,
		})
	}

	return nil
}

// GenerateReport produces a text summary of the CCI analysis.
func GenerateReport(result *CCIAnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("=== CCI Rudolph 分析報告 ===\n")
	fmt.Fprintf(&sb, "主題: %s\n", result.Subject)
	fmt.Fprintf(&sb, "肌肉配對數: %d\n\n", len(result.PairResults))

	for _, pr := range result.PairResults {
		if len(pr.Values) == 0 {
			continue
		}

		mean, max := computePairStats(pr.Values)
		fmt.Fprintf(&sb, "  %s: 平均 CCI = %.4f, 最大 CCI = %.4f\n",
			pr.PairName, mean, max)
	}

	return sb.String()
}

// computePairStats returns mean and max of a CCI curve.
// Caller (FormatResultReport) guards against empty values, so util helpers
// that assume len > 0 are safe here.
func computePairStats(values []float64) (float64, float64) {
	mean := util.ArrayMean(values)
	maxVal, _ := util.ArrayMax(values)

	return mean, maxVal
}
