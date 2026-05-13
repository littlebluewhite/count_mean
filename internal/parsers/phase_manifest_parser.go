package parsers

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/models"
	"count_mean/internal/csvutil"
	"count_mean/internal/security/fsperm"
)

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

	// EMG Motion Offset
	manifest.EMGMotionOffset, err = parseInt(record[4], "EMGMotionOffset")
	if err != nil {
		return manifest, err
	}

	// 分期點解析
	phasePoints := models.PhasePoints{}

	// P0 - 力板時間
	phasePoints.P0, err = parseFloat(record[5], "P0")
	if err != nil {
		return manifest, err
	}

	// P1 - 力板時間
	phasePoints.P1, err = parseFloat(record[6], "P1")
	if err != nil {
		return manifest, err
	}

	// P2 - 力板時間
	phasePoints.P2, err = parseFloat(record[7], "P2")
	if err != nil {
		return manifest, err
	}

	// S - 啟動瞬間-力板時間
	phasePoints.S, err = parseFloat(record[8], "S")
	if err != nil {
		return manifest, err
	}

	// C - 下蹲加速減速轉換瞬間-力板時間
	phasePoints.C, err = parseFloat(record[9], "C")
	if err != nil {
		return manifest, err
	}

	// D - 下蹲結束時間-motion index
	phasePoints.D, err = parseInt(record[10], "D")
	if err != nil {
		return manifest, err
	}

	// T0 - 正沖涼結束時間-力板時間
	phasePoints.T0, err = parseFloat(record[11], "T0")
	if err != nil {
		return manifest, err
	}

	// T - 起跳瞬間-力板時間
	phasePoints.T, err = parseFloat(record[12], "T")
	if err != nil {
		return manifest, err
	}

	// O - 展體轉間-motion index
	phasePoints.O, err = parseInt(record[13], "O")
	if err != nil {
		return manifest, err
	}

	// L - 著地瞬間-力板時間
	phasePoints.L, err = parseFloat(record[14], "L")
	if err != nil {
		return manifest, err
	}

	manifest.PhasePoints = phasePoints

	return manifest, nil
}

// parseFloat 解析浮點數，處理空值.
func parseFloat(value, fieldName string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	// 處理各種空值表示
	if trimmed == "" || trimmed == "NA" || trimmed == "N/A" ||
		trimmed == "x" || trimmed == "X" || trimmed == "-" {
		return 0, nil // 空值返回0
	}

	result, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("無法解析 %s 的浮點數值 '%s': %w", fieldName, trimmed, err)
	}

	return result, nil
}

// parseInt 解析整數，處理空值.
func parseInt(value, fieldName string) (int, error) {
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

	return result, nil
}

// GetPhaseValue 根據分期點獲取對應的值。回傳值與 phase.IsMotionIndex() 一致：
// false 表示力板時間 (float64)，true 表示 motion index (int → float64 cast)。
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetPhaseValue(points *models.PhasePoints, phase models.PhasePoint) (float64, bool, error) {
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
		return float64(points.D), true, nil
	case models.PhaseT0:
		return points.T0, false, nil
	case models.PhaseT:
		return points.T, false, nil
	case models.PhaseO:
		return float64(points.O), true, nil
	case models.PhaseL:
		return points.L, false, nil
	default:
		return 0, false, fmt.Errorf("未知的分期點: %s", phase)
	}
}

// ValidatePhaseManifest 驗證分期總檔案數據.
func ValidatePhaseManifest(manifest *models.PhaseManifest) error {
	if manifest.Subject == "" {
		return models.ValidationError{Field: "Subject", Message: "主題名稱不能為空"}
	}

	if manifest.MotionFile == "" {
		return models.ValidationError{Field: "MotionFile", Message: "Motion檔案名不能為空"}
	}

	if manifest.ForceFile == "" {
		return models.ValidationError{Field: "ForceFile", Message: "力板檔案名不能為空"}
	}

	if manifest.EMGFile == "" {
		return models.ValidationError{Field: "EMGFile", Message: "EMG檔案名不能為空"}
	}

	if manifest.EMGMotionOffset < 0 {
		return models.ValidationError{Field: "EMGMotionOffset", Message: "EMG Motion Offset 不能為負數"}
	}

	// 驗證分期點的邏輯關係
	points := manifest.PhasePoints

	// 檢查力板時間的順序（如果值不為0）
	type phaseTimePoint struct {
		name  models.PhasePoint
		value float64
	}
	var forceTimePoints []phaseTimePoint

	if points.P0 > 0 {
		forceTimePoints = append(forceTimePoints, phaseTimePoint{models.PhaseP0, points.P0})
	}

	if points.P1 > 0 {
		forceTimePoints = append(forceTimePoints, phaseTimePoint{models.PhaseP1, points.P1})
	}

	if points.P2 > 0 {
		forceTimePoints = append(forceTimePoints, phaseTimePoint{models.PhaseP2, points.P2})
	}

	// 驗證時間順序
	for i := 1; i < len(forceTimePoints); i++ {
		if forceTimePoints[i].value < forceTimePoints[i-1].value {
			return models.ValidationError{
				Field: "PhasePoints",
				Message: fmt.Sprintf("%s 時間 (%.3f) 不能早於 %s 時間 (%.3f)",
					forceTimePoints[i].name, forceTimePoints[i].value,
					forceTimePoints[i-1].name, forceTimePoints[i-1].value),
			}
		}
	}

	return nil
}
