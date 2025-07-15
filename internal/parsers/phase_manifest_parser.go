package parsers

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/models"
)

// PhaseManifestParser 分期總檔案解析器
type PhaseManifestParser struct {
	skipHeader bool
}

// NewPhaseManifestParser 創建新的解析器
func NewPhaseManifestParser() *PhaseManifestParser {
	return &PhaseManifestParser{
		skipHeader: true, // 第一行是標題
	}
}

// ParseFile 解析分期總檔案
func (p *PhaseManifestParser) ParseFile(filepath string) ([]models.PhaseManifest, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟檔案 %s: %w", filepath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
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

	var manifests []models.PhaseManifest
	for i := startRow; i < len(records); i++ {
		record := records[i]
		if len(record) < 15 {
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

// parseRecord 解析單行記錄
func (p *PhaseManifestParser) parseRecord(record []string, lineNum int) (models.PhaseManifest, error) {
	var manifest models.PhaseManifest

	// 基本欄位
	manifest.Subject = strings.TrimSpace(record[0])
	manifest.MotionFile = strings.TrimSpace(record[1])
	manifest.ForceFile = strings.TrimSpace(record[2])
	manifest.EMGFile = strings.TrimSpace(record[3])

	// EMG Motion Offset
	offset, err := strconv.Atoi(strings.TrimSpace(record[4]))
	if err != nil {
		return manifest, fmt.Errorf("無法解析 EMGMotionOffset: %w", err)
	}
	manifest.EMGMotionOffset = offset

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

// parseFloat 解析浮點數，處理空值
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

// parseInt 解析整數，處理空值
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

// GetPhaseValue 根據分期點名稱獲取對應的值
func GetPhaseValue(points models.PhasePoints, phaseName string) (float64, bool, error) {
	switch phaseName {
	case "P0":
		return points.P0, false, nil // false 表示是力板時間
	case "P1":
		return points.P1, false, nil
	case "P2":
		return points.P2, false, nil
	case "S":
		return points.S, false, nil
	case "C":
		return points.C, false, nil
	case "D":
		return float64(points.D), true, nil // true 表示是 motion index
	case "T0":
		return points.T0, false, nil
	case "T":
		return points.T, false, nil
	case "O":
		return float64(points.O), true, nil
	case "L":
		return points.L, false, nil
	default:
		return 0, false, fmt.Errorf("未知的分期點: %s", phaseName)
	}
}

// ValidatePhaseManifest 驗證分期總檔案數據
func ValidatePhaseManifest(manifest models.PhaseManifest) error {
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
	var forceTimePoints []struct {
		name  string
		value float64
	}

	if points.P0 > 0 {
		forceTimePoints = append(forceTimePoints, struct {
			name  string
			value float64
		}{"P0", points.P0})
	}

	if points.P1 > 0 {
		forceTimePoints = append(forceTimePoints, struct {
			name  string
			value float64
		}{"P1", points.P1})
	}

	if points.P2 > 0 {
		forceTimePoints = append(forceTimePoints, struct {
			name  string
			value float64
		}{"P2", points.P2})
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
