package config

import (
	"count_mean/internal/models"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// AppConfig 應用程式配置
type AppConfig struct {
	ScalingFactor int      `json:"scaling_factor"`
	PhaseLabels   []string `json:"phase_labels"`
	Precision     int      `json:"precision"`
	OutputFormat  string   `json:"output_format"`
	BOMEnabled    bool     `json:"bom_enabled"`
	InputDir      string   `json:"input_dir"`
	OutputDir     string   `json:"output_dir"`
	OperateDir    string   `json:"operate_dir"`
}

// DefaultConfig 返回默認配置
func DefaultConfig() *AppConfig {
	return &AppConfig{
		ScalingFactor: 10,
		PhaseLabels: []string{
			"啟跳下蹲階段",
			"啟跳上升階段",
			"團身階段",
			"下降階段",
		},
		Precision:    10,
		OutputFormat: "csv",
		BOMEnabled:   true,
		InputDir:     "./input",
		OutputDir:    "./output",
		OperateDir:   "./value_operate",
	}
}

// LoadConfig 從檔案載入配置
func LoadConfig(filename string) (*AppConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// 如果檔案不存在，返回默認配置
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("無法開啟配置檔案: %w", err)
	}
	defer file.Close()

	var config AppConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("解析配置檔案失敗: %w", err)
	}

	// 驗證配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置檔案無效: %w", err)
	}

	return &config, nil
}

// SaveConfig 保存配置到檔案
func (c *AppConfig) SaveConfig(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("無法創建配置檔案: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("保存配置檔案失敗: %w", err)
	}

	return nil
}

// Validate 驗證配置
func (c *AppConfig) Validate() error {
	if c.ScalingFactor <= 0 {
		return fmt.Errorf("縮放因子必須大於 0")
	}

	if len(c.PhaseLabels) == 0 {
		return fmt.Errorf("階段標籤不能為空")
	}

	if c.Precision < 0 || c.Precision > 15 {
		return fmt.Errorf("精度必須在 0-15 之間")
	}

	validFormats := map[string]bool{
		"csv":  true,
		"json": true,
		"xlsx": true,
	}

	if !validFormats[c.OutputFormat] {
		return fmt.Errorf("不支援的輸出格式: %s", c.OutputFormat)
	}

	// 驗證目錄路徑
	if c.InputDir == "" {
		return fmt.Errorf("輸入目錄路徑不能為空")
	}

	if c.OutputDir == "" {
		return fmt.Errorf("輸出目錄路徑不能為空")
	}

	if c.OperateDir == "" {
		return fmt.Errorf("操作目錄路徑不能為空")
	}

	return nil
}

// EnsureDirectories 確保配置中的目錄存在
func (c *AppConfig) EnsureDirectories() error {
	dirs := []string{c.InputDir, c.OutputDir, c.OperateDir}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("無法創建目錄 %s: %w", dir, err)
		}
	}

	return nil
}

// ToAnalysisConfig 轉換為分析配置
func (c *AppConfig) ToAnalysisConfig() *models.AnalysisConfig {
	return &models.AnalysisConfig{
		ScalingFactor: c.ScalingFactor,
		PhaseLabels:   c.PhaseLabels,
		CreatedAt:     time.Now(),
	}
}

// ProcessingOptions 獲取處理選項
func (c *AppConfig) ProcessingOptions() *models.ProcessingOptions {
	return &models.ProcessingOptions{
		ValidateInput: true,
		Precision:     c.Precision,
		OutputFormat:  c.OutputFormat,
	}
}
