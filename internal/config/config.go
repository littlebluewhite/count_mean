// Package config provides configuration management for the EMG data analysis
// application, including loading, saving, and validating application settings.
package config

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
)

// Configuration validation errors.
var (
	errScalingFactorInvalid = stderrors.New("縮放因子必須大於 0")
	errPhaseLabelsEmpty     = stderrors.New("階段標籤不能為空")
	errPrecisionOutOfRange  = stderrors.New("精度必須在 0-15 之間")
	errUnsupportedFormat    = stderrors.New("不支援的輸出格式")
	errInputDirEmpty        = stderrors.New("輸入目錄路徑不能為空")
	errOutputDirEmpty       = stderrors.New("輸出目錄路徑不能為空")
	errOperateDirEmpty      = stderrors.New("操作目錄路徑不能為空")
	errUnsupportedLanguage  = stderrors.New("不支援的語言")
)

// AppConfig 應用程式配置.
type AppConfig struct {
	ScalingFactor int      `json:"scalingFactor"`
	PhaseLabels   []string `json:"phaseLabels"`
	Precision     int      `json:"precision"`
	OutputFormat  string   `json:"outputFormat"`
	BOMEnabled    bool     `json:"bomEnabled"`
	InputDir      string   `json:"inputDir"`
	OutputDir     string   `json:"outputDir"`
	OperateDir    string   `json:"operateDir"`

	// 日誌配置
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error
	LogFormat    string `json:"logFormat"`    // text, json
	LogDirectory string `json:"logDirectory"` // 日誌目錄

	// 國際化配置
	Language        string `json:"language"`        // zh-TW, zh-CN, en-US, ja-JP
	TranslationsDir string `json:"translationsDir"` // 翻譯文件目錄
}

// DefaultConfig 返回默認配置.
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
		OperateDir:   "./operate",

		// 預設日誌配置
		LogLevel:     "info",
		LogFormat:    "text",
		LogDirectory: "./logs",

		// 預設國際化配置
		Language:        "zh-TW",
		TranslationsDir: "./translations",
	}
}

// LoadConfig 從檔案載入配置.
func LoadConfig(filename string) (*AppConfig, error) {
	file, err := os.OpenFile(filename, fsperm.ReadFlags, 0) //nolint:gosec // filename provided by caller, validated at app level; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with write-side)
	if err != nil {
		if os.IsNotExist(err) {
			// 如果檔案不存在，返回默認配置
			return DefaultConfig(), nil
		}

		return nil, fmt.Errorf("無法開啟配置檔案: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	var config AppConfig

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("解析配置檔案失敗: %w", err)
	}

	// 舊版或 partial config.json 可能省略 language 欄位,留空字串會被下方 Validate
	// 拒絕,進而讓 main 退回 DefaultConfig 並丟棄使用者儲存的 directories / 設定。
	// 前端對空 language 預設視為 zh-TW,後端在此對齊,避免使用者升級後設定被吃掉
	// (codex review P2 fix)。
	if config.Language == "" {
		config.Language = string(i18n.LocaleZhTW)
	}

	// 驗證配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置檔案無效: %w", err)
	}

	return &config, nil
}

// SaveConfig 保存配置到檔案.
func (c *AppConfig) SaveConfig(filename string) error {
	//nolint:gosec // filename provided by caller
	file, err := os.OpenFile(filename, fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("無法創建配置檔案: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("保存配置檔案失敗: %w", err)
	}

	return nil
}

// Validate 驗證配置.
func (c *AppConfig) Validate() error {
	if c.ScalingFactor <= 0 {
		return errScalingFactorInvalid
	}

	if len(c.PhaseLabels) == 0 {
		return errPhaseLabelsEmpty
	}

	const maxPrecision = 15
	if c.Precision < 0 || c.Precision > maxPrecision {
		return errPrecisionOutOfRange
	}

	validFormats := map[string]bool{
		"csv":  true,
		"json": true,
		"xlsx": true,
	}

	if !validFormats[c.OutputFormat] {
		return fmt.Errorf("%w: %s", errUnsupportedFormat, c.OutputFormat)
	}

	validLanguages := map[string]bool{
		string(i18n.LocaleZhTW): true,
		string(i18n.LocaleZhCN): true,
		string(i18n.LocaleEnUS): true,
		string(i18n.LocaleJaJP): true,
	}

	if !validLanguages[c.Language] {
		return fmt.Errorf("%w: %s", errUnsupportedLanguage, c.Language)
	}

	// 驗證目錄路徑
	if c.InputDir == "" {
		return errInputDirEmpty
	}

	if c.OutputDir == "" {
		return errOutputDirEmpty
	}

	if c.OperateDir == "" {
		return errOperateDirEmpty
	}

	// Traversal / system-dir / null-byte 驗證 — 拒絕 "../../etc"、"/etc"、含 \x00 等。
	// security.PathValidator.ValidateExternalPath 自動把相對路徑（如 "./output"）以 cwd
	// 展開為絕對路徑後再檢，因此預設 config 不會誤拒。InputDir / OutputDir / OperateDir /
	// LogDirectory / TranslationsDir 五個 user-controllable directory 都一併保護。
	validator := security.NewPathValidator(nil)

	dirFields := []struct {
		name string
		path string
	}{
		{"InputDir", c.InputDir},
		{"OutputDir", c.OutputDir},
		{"OperateDir", c.OperateDir},
		{"LogDirectory", c.LogDirectory},
		{"TranslationsDir", c.TranslationsDir},
	}

	for _, f := range dirFields {
		if f.path == "" {
			continue // LogDirectory / TranslationsDir 容許空字串（非必填）
		}

		if strings.ContainsRune(f.path, 0) {
			//nolint:err113 // dynamic error for user-facing output
			return fmt.Errorf("%s 含 null byte", f.name)
		}

		// Directory 語意：OutputDir 等是「未來寫入路徑的 prefix」，附 dummy child 後再驗
		// 確保 OutputDir 本身就是 system root（"/etc"）也被擋 — performBasicSecurityChecks
		// 的 sensitive pattern "/etc/" 等需要結尾 slash 才命中。"/etc-backup" 不會誤擋
		// 因為 join 後是 "/etc-backup/_marker" 不含 "/etc/"。
		checkPath := filepath.Join(f.path, "_validation_marker")
		if err := validator.ValidateExternalPath(checkPath); err != nil {
			return fmt.Errorf("%s 驗證失敗: %w", f.name, err)
		}
	}

	return nil
}

// EnsureDirectories 確保配置中的目錄存在.
func (c *AppConfig) EnsureDirectories() error {
	dirs := []string{c.InputDir, c.OutputDir, c.OperateDir, c.LogDirectory, c.TranslationsDir}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, fsperm.DirPerm); err != nil {
			return fmt.Errorf("無法創建目錄 %s: %w", dir, err)
		}
	}

	return nil
}

// ToAnalysisConfig 轉換為分析配置.
func (c *AppConfig) ToAnalysisConfig() *models.AnalysisConfig {
	return &models.AnalysisConfig{
		ScalingFactor: c.ScalingFactor,
		PhaseLabels:   c.PhaseLabels,
		CreatedAt:     time.Now(),
	}
}

// ProcessingOptions 獲取處理選項.
func (c *AppConfig) ProcessingOptions() *models.ProcessingOptions {
	return &models.ProcessingOptions{
		ValidateInput: true,
		Precision:     c.Precision,
		OutputFormat:  c.OutputFormat,
	}
}
