package parsers

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// Phase manifest 數值欄位的 sentinel errors（NaN/Inf / negative / DoS scale）。
// 方便 caller 用 errors.Is 區分。
var (
	ErrPhaseManifestInvalidNumeric    = errors.New("非法數值（NaN/Inf 不允許）")
	ErrPhaseManifestNegativeTime      = errors.New("時間欄位不可為負值")
	ErrPhaseManifestTimeTooLarge      = errors.New("時間欄位超出合理範圍")
	ErrPhaseManifestMotionIndexTooBig = errors.New("motion-index 超出合理範圍")
)

// maxReasonablePhase — 86400 秒(1 day)。phase manifest 時間欄位超過視為惡意 / 損毀
// 輸入(DoS:上游 calculation 用此值做 sliding window / chart axis 可能 OOM 或 hang)。
//
// # 從 1e9 (~31 年) 降到 86400 (1 day)
//
// 真實 EMG / motion capture session 時間軸:
//   - 單一 trial 通常 < 10 秒(跳躍 / 衝刺 / squat 動作)
//   - 長時 protocol(疲勞 / 步態分析)< 30 分鐘 = 1800 秒
//   - 1 day 已是「絕無使用情境」的極端上限,留 3-4 個 order of magnitude 緩衝
//
// 86400 秒是 spread sheet 計算錯誤 / 單位混淆 / 攻擊 input 的常見 marker:
//   - 把 milliseconds 誤填為 seconds (3600 sec actual → 3.6M 入欄)
//   - epoch timestamp 直接灌入 (1.7e9, 2024 年 epoch),~31 年舊上限 1e9 攔不下
//   - DoS 灌入 1e8 等大數(舊上限 1e9 通過)上游 sliding window window-size scan
//     O(n) 變極慢甚至 OOM
//
// 1 day 不會誤殺合法 fixture:既有測試 fixture (input/test_phase_manifest.csv 等)
// phase 時間皆 < 10 秒。若未來真有 > 24 hour 的長時 monitoring 需求,可獨立評估
// 是否再放寬上限 — 目前無此 use case。
const maxReasonablePhase = 86400 // 1 day = 24 * 3600 seconds

// phaseManifestReaderBufSize 是 manifest CSV 讀取的 bufio buffer 大小 (32 KiB)。
// Manifest 檔通常 < 1 MiB,32K 已足夠 amortize syscall。
const phaseManifestReaderBufSize = 32 * 1024

// PhaseManifestParser 分期總檔案解析器.
type PhaseManifestParser struct {
	skipHeader bool
}

// NewPhaseManifestParser 創建新的解析器.
func NewPhaseManifestParser() *PhaseManifestParser {
	return &PhaseManifestParser{
		skipHeader: true, // 第一行是標題
	}
}

// ParseFile 解析分期總檔案.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func (p *PhaseManifestParser) ParseFile(filepath string) ([]models.PhaseManifest, error) {
	file, err := os.OpenFile(filepath, fsperm.ReadFlags, 0) //nolint:gosec // filepath validated by caller; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with write-side)
	if err != nil {
		return nil, fmt.Errorf("無法開啟檔案 %s: %w", filepath, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	// BOM 處理: Excel 匯出的 UTF-8 manifest CSV 帶 0xEF 0xBB 0xBF 前綴若不剝除,
	// records[startRow][0] 會帶 U+FEFF,Subject 比對失敗。
	// 與 internal/io/csv_handler.go:230 / large_file_handler.go 對稱: bufio + PeekBOM。
	bufReader := bufio.NewReaderSize(file, phaseManifestReaderBufSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return nil, fmt.Errorf("BOM 偵測失敗: %w", err)
	}
	reader := csv.NewReader(bufReader)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("讀取CSV失敗: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("檔案為空")
	}

	// 跳過標題行
	startRow := 0
	if p.skipHeader {
		startRow = 1
	}

	manifests := make([]models.PhaseManifest, 0, len(records)-startRow)

	for i := startRow; i < len(records); i++ {
		record := records[i]
		if len(record) < PhaseManifestMinFields {
			return nil, fmt.Errorf("第 %d 行資料不完整，需要至少 15 個欄位", i+1)
		}

		manifest, err := p.parseRecord(record, i+1)
		if err != nil {
			return nil, fmt.Errorf("解析第 %d 行時發生錯誤: %w", i+1, err)
		}

		manifests = append(manifests, manifest)
	}

	return manifests, nil
}

// parseRecord 解析單行記錄.
//
//nolint:revive // unused-receiver: keep consistent API
func (p *PhaseManifestParser) parseRecord(
	record []string, _ int,
) (models.PhaseManifest, error) {
	var manifest models.PhaseManifest

	var err error

	// 基本欄位
	manifest.Subject = strings.TrimSpace(record[0])
	manifest.MotionFile = strings.TrimSpace(record[1])
	manifest.ForceFile = strings.TrimSpace(record[2])
	manifest.EMGFile = strings.TrimSpace(record[3])

	// EMG Motion Offset — 不是 motion frame index,語意是兩個時間軸的偏移量,
	// 不套用 MaxReasonableMotionIndex 上限。
	manifest.EMGMotionOffset, err = parseInt(record[4], "EMGMotionOffset", 0)
	if err != nil {
		return manifest, err
	}

	// 分期點解析 — float 欄位走 parseFloat → OptFloat (Set=false 表示「未提供」),
	// int 欄位 (D, O) 仍走 parseInt（0 = 未提供 sentinel）。
	phasePoints := models.PhasePoints{}

	if phasePoints.P0, err = parseFloat(record[5], "P0"); err != nil {
		return manifest, err
	}

	if phasePoints.P1, err = parseFloat(record[6], "P1"); err != nil {
		return manifest, err
	}

	if phasePoints.P2, err = parseFloat(record[7], "P2"); err != nil {
		return manifest, err
	}

	if phasePoints.S, err = parseFloat(record[8], "S"); err != nil {
		return manifest, err
	}

	if phasePoints.C, err = parseFloat(record[9], "C"); err != nil {
		return manifest, err
	}

	if phasePoints.D, err = parseInt(record[10], "D", MaxReasonableMotionIndex); err != nil {
		return manifest, err
	}

	if phasePoints.T0, err = parseFloat(record[11], "T0"); err != nil {
		return manifest, err
	}

	if phasePoints.T, err = parseFloat(record[12], "T"); err != nil {
		return manifest, err
	}

	if phasePoints.O, err = parseInt(record[13], "O", MaxReasonableMotionIndex); err != nil {
		return manifest, err
	}

	if phasePoints.L, err = parseFloat(record[14], "L"); err != nil {
		return manifest, err
	}

	manifest.PhasePoints = phasePoints

	// V.14 manifest 第 16 欄 MuscleRatioFile（filename only、相對 DataFolder、可空）。
	// V.10 / V.13 manifest 只有 15 欄,此處留空字串 — PhaseManifestMinFields 維持 15
	// 表示「新欄位純加值,不影響舊 manifest happy path」。
	if len(record) >= 16 {
		manifest.MuscleRatioFile = strings.TrimSpace(record[15])
	}

	return manifest, nil
}

// parseFloat 解析浮點數，處理空值。回傳 OptFloat — Set=false 表示「未提供」
// （空字串 / "NA" / "x" / "-" 等），Set=true 帶有實際解析值（可以是 0）。
//
// **NaN/Inf 拒絕**：strconv.ParseFloat 對 "NaN" / "+Inf" / "-Inf" 等字面回
// (value, nil)。下游 ValidatePhaseManifest 的 monotonicity check 用 `<` 比較，
// NaN 永遠 false 會 bypass 所有合理範圍檢查；最終 phase_sync 除以 NaN 造成
// silent NaN-poisoning（cell 全空）。一律當不合法 fail-fast。
//
// **Batch T**：先前 sentinel 0（空值與真實 0 秒無法區分）的歧義已由 OptFloat 解決。
// "NA"/""/"x"/"-" → NoOpt()；"0" → MakeOpt(0)。下游不再用 `value > 0` 判斷「是否提供」。
func parseFloat(value, fieldName string) (models.OptFloat, error) {
	trimmed := strings.TrimSpace(value)
	// 處理各種空值表示
	if trimmed == "" || trimmed == "NA" || trimmed == "N/A" ||
		trimmed == "x" || trimmed == "X" || trimmed == "-" {
		return models.NoOpt(), nil
	}

	result, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return models.NoOpt(), fmt.Errorf("無法解析 %s 的浮點數值 '%s': %w", fieldName, trimmed, err)
	}

	if math.IsNaN(result) || math.IsInf(result, 0) {
		return models.NoOpt(), fmt.Errorf("%s = '%s': %w", fieldName, trimmed, ErrPhaseManifestInvalidNumeric)
	}

	// NOT IMPLEMENTED：負值 phase point 經測試驗證為「機械測量校準前的時間軸
	// 偏移」合法設計（見 muscle_ratio analyzer_test.go TestAnalyze_NegativeTime_Handled
	// 「負時間 EMG 不應讓批次 Analyze 整體失敗」）。TODO 描述為誤判，已記錄。

	// 巨型 phase value 拒絕（DoS 防護，> 31 年的時間明顯異常）。
	if math.Abs(result) > maxReasonablePhase {
		return models.NoOpt(), fmt.Errorf("%s = %v: %w (max ±%v)", fieldName, result,
			ErrPhaseManifestTimeTooLarge, maxReasonablePhase)
	}

	return models.MakeOpt(result), nil
}

// parseInt 解析整數，處理空值。
//
// **maxAbs 上限**:
//   - 0 → 不檢查(用於 EMGMotionOffset 等非 motion-index 欄位,語意不同)。
//   - >0 → 結果絕對值超過 maxAbs 時 fail-fast,回 ErrPhaseManifestMotionIndexTooBig。
//
// **攻擊向量**:`D=1e15` + `O=0`(sentinel)組合下,validateMotionIndexOrder
// 把 O=0 當「未提供」跳過,只有 D 一個 motion-index 點,單元素 list monotonicity
// 永遠 pass — 下游 phase_sync 拿到 1e15 frame index 直接 OOM / hang。caller 對
// D / O motion-index path 必須傳 MaxReasonableMotionIndex,在 parse 階段就攔截。
//
// 負值仍允許通過(由 validateMotionIndexOrder 在 validate 階段 reject,既有契約)。
func parseInt(value, fieldName string, maxAbs int) (int, error) {
	trimmed := strings.TrimSpace(value)
	// 處理各種空值表示
	if trimmed == "" || trimmed == "NA" || trimmed == "N/A" ||
		trimmed == "x" || trimmed == "X" || trimmed == "-" {
		return 0, nil // 空值返回0
	}

	result, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("無法解析 %s 的整數值 '%s': %w", fieldName, trimmed, err)
	}

	if maxAbs > 0 {
		abs := result
		if abs < 0 {
			abs = -abs
		}
		if abs > maxAbs {
			return 0, fmt.Errorf("%s = %d: %w (max ±%d)", fieldName, result,
				ErrPhaseManifestMotionIndexTooBig, maxAbs)
		}
	}

	return result, nil
}

// GetPhaseValue 根據分期點獲取對應的值。回傳 OptFloat — Set=false 表示「未提供」。
// 第二個 bool 與 phase.IsMotionIndex() 一致：false 表示力板時間，true 表示 motion index。
//
// 行為一致性：
//   - 力板時間 (P0/P1/P2/S/C/T0/T/L)：直接回傳 PhasePoints 中的 OptFloat。
//   - motion-index (D/O)：仍走 int 0-sentinel — v==0 → NoOpt()，v!=0 → MakeOpt(float64(v))。
//     （任務範圍外，D/O 暫保留 int 型別；未來若一併改 OptInt 再消除最後 sentinel）
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetPhaseValue(points *models.PhasePoints, phase models.PhasePoint) (models.OptFloat, bool, error) {
	switch phase {
	case models.PhaseP0:
		return points.P0, false, nil
	case models.PhaseP1:
		return points.P1, false, nil
	case models.PhaseP2:
		return points.P2, false, nil
	case models.PhaseS:
		return points.S, false, nil
	case models.PhaseC:
		return points.C, false, nil
	case models.PhaseD:
		return motionIndexOpt(points.D), true, nil
	case models.PhaseT0:
		return points.T0, false, nil
	case models.PhaseT:
		return points.T, false, nil
	case models.PhaseO:
		return motionIndexOpt(points.O), true, nil
	case models.PhaseL:
		return points.L, false, nil
	default:
		return models.NoOpt(), false, fmt.Errorf("未知的分期點: %s", phase)
	}
}

// motionIndexOpt 將 int motion-index 包成 OptFloat：
//   - 0 視為「未提供」(NoOpt)
//   - 正值 → MakeOpt
//   - 負值 → NoOpt(defense-in-depth)
//
// 理論上 ValidatePhaseManifest 已 fail-fast reject 負 motion-index,
// 但對於繞過 parser 直接 struct-init 的 caller(例如測試 helper),這裡多一層
// 防禦避免下游 phase_sync 拿到負 index frame number。與 D/O 暫保留 int + 0-sentinel
// 的契約一致(見 PhasePoints doc)。
func motionIndexOpt(v int) models.OptFloat {
	if v <= 0 {
		return models.NoOpt()
	}
	return models.MakeOpt(float64(v))
}

// ValidatePhaseManifest 驗證分期總檔案數據.
func ValidatePhaseManifest(manifest *models.PhaseManifest) error {
	if manifest.Subject == "" {
		return models.PhaseSyncValidationError{Field: "Subject", Message: "主題名稱不能為空"}
	}

	if manifest.MotionFile == "" {
		return models.PhaseSyncValidationError{Field: "MotionFile", Message: "Motion檔案名不能為空"}
	}

	if manifest.ForceFile == "" {
		return models.PhaseSyncValidationError{Field: "ForceFile", Message: "力板檔案名不能為空"}
	}

	if manifest.EMGFile == "" {
		return models.PhaseSyncValidationError{Field: "EMGFile", Message: "EMG檔案名不能為空"}
	}

	if manifest.EMGMotionOffset < 0 {
		return models.PhaseSyncValidationError{Field: "EMGMotionOffset", Message: "EMG Motion Offset 不能為負數"}
	}

	// 驗證分期點的邏輯關係
	points := manifest.PhasePoints

	// NaN/Inf 防線：parseFloat 已 reject，但 caller 可能直接 struct-init OptFloat，加雙保險。
	// 任何時間欄位含 NaN/Inf 都會讓 monotonicity check `<` 比較永遠 false 而 bypass。
	// 只對 Set=true 的欄位檢查 — Set=false 的零值不該被當成 NaN/Inf 誤報。
	for _, fv := range []struct {
		name string
		opt  models.OptFloat
	}{
		{"P0", points.P0}, {"P1", points.P1}, {"P2", points.P2},
		{"S", points.S}, {"C", points.C}, {"T0", points.T0},
		{"T", points.T}, {"L", points.L},
	} {
		v, ok := fv.opt.Get()
		if !ok {
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return models.PhaseSyncValidationError{
				Field:   "PhasePoints." + fv.name,
				Message: fmt.Sprintf("時間欄位 %s 為非法數值 (NaN/Inf)", fv.name),
			}
		}
	}

	// 檢查力板時間的順序（僅納入已標定的欄位 — Set=false 跳過）。
	// Batch T 重構後不再用 `value > 0` 判定「是否提供」，t=0 是合法時間。
	type phaseTimePoint struct {
		name  models.PhasePoint
		value float64
	}
	var forceTimePoints []phaseTimePoint

	appendIfSet := func(name models.PhasePoint, opt models.OptFloat) {
		if v, ok := opt.Get(); ok {
			forceTimePoints = append(forceTimePoints, phaseTimePoint{name, v})
		}
	}

	appendIfSet(models.PhaseP0, points.P0)
	appendIfSet(models.PhaseP1, points.P1)
	appendIfSet(models.PhaseP2, points.P2)
	appendIfSet(models.PhaseS, points.S)
	appendIfSet(models.PhaseC, points.C)
	appendIfSet(models.PhaseT0, points.T0)
	appendIfSet(models.PhaseT, points.T)
	appendIfSet(models.PhaseL, points.L)

	// 驗證時間順序
	for i := 1; i < len(forceTimePoints); i++ {
		if forceTimePoints[i].value < forceTimePoints[i-1].value {
			return models.PhaseSyncValidationError{
				Field: "PhasePoints",
				Message: fmt.Sprintf("%s 時間 (%.3f) 不能早於 %s 時間 (%.3f)",
					forceTimePoints[i].name, forceTimePoints[i].value,
					forceTimePoints[i-1].name, forceTimePoints[i-1].value),
			}
		}
	}

	// P1-A4-3 (D/O)：motion-index monotonicity 走獨立 list（單位是 motion frame
	// index，與 force-time 秒數不可比較）。動作生理上 D（下蹲結束）必在 O（展體
	// 轉間）之前。0 仍代表「未提供」 sentinel，跳過檢查。strict < 而非 ≤ 與 force-time
	// 一致（允許等號）。
	if err := validateMotionIndexOrder(points); err != nil {
		return err
	}

	return nil
}

// validateMotionIndexOrder 檢查 motion-index 配對 (D, O) 是否單調 + 是否非負。
// 與 force-time monotonicity 走獨立 list,避免單位不同的混雜比較。
//
// 負值 D/O 是 silent miscalculation 入口 — parseInt 對 "-150" 合法,
// 但下游 phase_sync / muscle_ratio 把負 frame-index 當合法時間流入 sliding window
// 會 silent miscalc(或讀越界 → corrupted output)。0 仍代表「未提供」 sentinel
// 跳過 monotonicity 檢查;但負值是真正的「非法輸入」,必須 fail-fast。
func validateMotionIndexOrder(points models.PhasePoints) error {
	// 負值 reject — 動作 frame index 必為正整數,負值來自惡意 / 損毀 manifest。
	if points.D < 0 {
		return models.PhaseSyncValidationError{
			Field:   "PhasePoints",
			Message: fmt.Sprintf("D motion-index (%d) 不可為負值", points.D),
		}
	}
	if points.O < 0 {
		return models.PhaseSyncValidationError{
			Field:   "PhasePoints",
			Message: fmt.Sprintf("O motion-index (%d) 不可為負值", points.O),
		}
	}

	type motionIndexPoint struct {
		name  models.PhasePoint
		value int
	}

	var motionPoints []motionIndexPoint
	if points.D > 0 {
		motionPoints = append(motionPoints, motionIndexPoint{models.PhaseD, points.D})
	}
	if points.O > 0 {
		motionPoints = append(motionPoints, motionIndexPoint{models.PhaseO, points.O})
	}

	for i := 1; i < len(motionPoints); i++ {
		if motionPoints[i].value < motionPoints[i-1].value {
			return models.PhaseSyncValidationError{
				Field: "PhasePoints",
				Message: fmt.Sprintf("%s index (%d) 不能早於 %s index (%d)",
					motionPoints[i].name, motionPoints[i].value,
					motionPoints[i-1].name, motionPoints[i-1].value),
			}
		}
	}

	return nil
}
