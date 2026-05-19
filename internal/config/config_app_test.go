package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"count_mean/internal/security/fsperm"
	"count_mean/test/testutil"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)
	require.Equal(t, 10, cfg.ScalingFactor)
	require.Equal(t, []string{"啟跳下蹲階段", "啟跳上升階段", "團身階段", "下降階段"}, cfg.PhaseLabels)
	require.Equal(t, 10, cfg.Precision)
	require.Equal(t, "csv", cfg.OutputFormat)
	require.True(t, cfg.BOMEnabled)
}

func TestAppConfig_Validate(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1", "階段2"},
			Precision:     5,
			OutputFormat:  "csv",
			BOMEnabled:    true,
			InputDir:      "./input",
			OutputDir:     "./output",
			OperateDir:    "./value_operate",
			Language:      "zh-TW",
		}
		err := cfg.Validate()
		require.NoError(t, err)
	})

	t.Run("InvalidScalingFactor_Zero", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 0,
			PhaseLabels:   []string{"階段1"},
			Precision:     5,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "縮放因子必須大於 0")
	})

	t.Run("InvalidScalingFactor_Negative", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: -5,
			PhaseLabels:   []string{"階段1"},
			Precision:     5,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "縮放因子必須大於 0")
	})

	t.Run("EmptyPhaseLabels", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{},
			Precision:     5,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "階段標籤不能為空")
	})

	t.Run("NilPhaseLabels", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   nil,
			Precision:     5,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "階段標籤不能為空")
	})

	t.Run("InvalidPrecision_Negative", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1"},
			Precision:     -1,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "精度必須在 0-15 之間")
	})

	t.Run("InvalidPrecision_TooHigh", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1"},
			Precision:     16,
			OutputFormat:  "csv",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "精度必須在 0-15 之間")
	})

	t.Run("ValidPrecision_Boundary", func(t *testing.T) {
		// 測試邊界值
		configs := []*AppConfig{
			{
				ScalingFactor: 10, PhaseLabels: []string{"階段1"}, Precision: 0,
				OutputFormat: "csv", InputDir: "./input", OutputDir: "./output", OperateDir: "./value_operate",
				Language: "zh-TW",
			},
			{
				ScalingFactor: 10, PhaseLabels: []string{"階段1"}, Precision: 15,
				OutputFormat: "csv", InputDir: "./input", OutputDir: "./output", OperateDir: "./value_operate",
				Language: "zh-TW",
			},
		}
		for _, cfg := range configs {
			err := cfg.Validate()
			require.NoError(t, err)
		}
	})

	t.Run("ValidOutputFormats", func(t *testing.T) {
		validFormats := []string{"csv", "json", "xlsx"}
		for _, format := range validFormats {
			cfg := &AppConfig{
				ScalingFactor: 10,
				PhaseLabels:   []string{"階段1"},
				Precision:     5,
				OutputFormat:  format,
				InputDir:      "./input",
				OutputDir:     "./output",
				OperateDir:    "./value_operate",
				Language:      "zh-TW",
			}
			err := cfg.Validate()
			require.NoError(t, err)
		}
	})

	t.Run("InvalidOutputFormat", func(t *testing.T) {
		invalidFormats := []string{"txt", "xml", "pdf", ""}
		for _, format := range invalidFormats {
			cfg := &AppConfig{
				ScalingFactor: 10,
				PhaseLabels:   []string{"階段1"},
				Precision:     5,
				OutputFormat:  format,
			}
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "不支援的輸出格式")
		}
	})

	t.Run("CaseSensitiveOutputFormat", func(t *testing.T) {
		// 測試大小寫敏感性
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1"},
			Precision:     5,
			OutputFormat:  "CSV", // 大寫應該失敗
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "不支援的輸出格式: CSV")
	})

	t.Run("ValidLanguages", func(t *testing.T) {
		validLanguages := []string{"zh-TW", "zh-CN", "en-US", "ja-JP"}
		for _, lang := range validLanguages {
			cfg := &AppConfig{
				ScalingFactor: 10,
				PhaseLabels:   []string{"階段1"},
				Precision:     5,
				OutputFormat:  "csv",
				Language:      lang,
				InputDir:      "./input",
				OutputDir:     "./output",
				OperateDir:    "./operate",
			}
			err := cfg.Validate()
			require.NoError(t, err)
		}
	})

	t.Run("InvalidLanguage", func(t *testing.T) {
		invalidLanguages := []string{"klingon", "fr-FR", "en", "zh", ""}
		for _, lang := range invalidLanguages {
			cfg := &AppConfig{
				ScalingFactor: 10,
				PhaseLabels:   []string{"階段1"},
				Precision:     5,
				OutputFormat:  "csv",
				Language:      lang,
			}
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "不支援的語言")
		}
	})

	t.Run("CaseSensitiveLanguage", func(t *testing.T) {
		cfg := &AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1"},
			Precision:     5,
			OutputFormat:  "csv",
			Language:      "ZH-TW",
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "不支援的語言: ZH-TW")
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("FileNotExists_ReturnDefault", func(t *testing.T) {
		loadedConfig, err := LoadConfig("nonexistent.json")
		require.NoError(t, err)
		require.NotNil(t, loadedConfig)
		// 應該返回默認配置
		defaultConfig := DefaultConfig()
		require.Equal(t, defaultConfig.ScalingFactor, loadedConfig.ScalingFactor)
		require.Equal(t, defaultConfig.PhaseLabels, loadedConfig.PhaseLabels)
		require.Equal(t, defaultConfig.Precision, loadedConfig.Precision)
		require.Equal(t, defaultConfig.OutputFormat, loadedConfig.OutputFormat)
		require.Equal(t, defaultConfig.BOMEnabled, loadedConfig.BOMEnabled)
	})

	t.Run("ValidJSONFile", func(t *testing.T) {
		// 創建臨時配置文件
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "test_config.json")

		validJSON := `{
			"scalingFactor": 20,
			"phaseLabels": ["自定義階段1", "自定義階段2"],
			"precision": 8,
			"outputFormat": "json",
			"bomEnabled": false,
			"inputDir": "./input",
			"outputDir": "./output",
			"operateDir": "./value_operate",
			"language": "zh-TW"
		}`

		err := os.WriteFile(configFile, []byte(validJSON), fsperm.FilePerm)
		require.NoError(t, err)

		loadedConfig, err := LoadConfig(configFile)
		require.NoError(t, err)
		require.NotNil(t, loadedConfig)
		require.Equal(t, 20, loadedConfig.ScalingFactor)
		require.Equal(t, []string{"自定義階段1", "自定義階段2"}, loadedConfig.PhaseLabels)
		require.Equal(t, 8, loadedConfig.Precision)
		require.Equal(t, "json", loadedConfig.OutputFormat)
		require.False(t, loadedConfig.BOMEnabled)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "invalid_config.json")

		invalidJSON := `{
			"scaling_factor": 10,
			"invalid_json": 
		}`

		err := os.WriteFile(configFile, []byte(invalidJSON), fsperm.FilePerm)
		require.NoError(t, err)

		loadedConfig, err := LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析配置檔案失敗")
		require.Nil(t, loadedConfig)
	})

	t.Run("InvalidConfigValues", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "invalid_values_config.json")

		// 升級：原本 snake_case key 不會被 camelCase JSON tag 解析，靠
		// var-zero-value 偶然 fail validate。改用正確 camelCase key + 真實 invalid
		// 值（scalingFactor=-1）測 Validate 邏輯。
		invalidConfig := `{
			"scalingFactor": -1,
			"phaseLabels": [],
			"precision": 5,
			"outputFormat": "csv",
			"bomEnabled": true
		}`

		err := os.WriteFile(configFile, []byte(invalidConfig), fsperm.FilePerm)
		require.NoError(t, err)

		loadedConfig, err := LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "配置檔案無效")
		require.Nil(t, loadedConfig)
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		testutil.SkipIfChmodIneffective(t)

		// 創建一個無權限訪問的目錄
		tempDir := t.TempDir()
		restrictedDir := filepath.Join(tempDir, "restricted")
		err := os.Mkdir(restrictedDir, 0o000) // 無權限
		require.NoError(t, err)

		defer os.Chmod(restrictedDir, fsperm.DirPerm) // 清理時恢復權限

		configFile := filepath.Join(restrictedDir, "config.json")
		loadedConfig, err := LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法開啟配置檔案")
		require.Nil(t, loadedConfig)
	})

	// Regression for codex review P2: 舊版或 partial config 缺 language 欄位時,
	// LoadConfig 應 default 為 zh-TW 而不是讓 Validate 失敗、整個 config 被丟棄。
	t.Run("EmptyLanguage_DefaultsToZhTW", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "legacy_config.json")

		// 模擬升級前產生的 config.json — 完全沒有 language 欄位
		legacyJSON := `{
			"scalingFactor": 25,
			"phaseLabels": ["階段A", "階段B"],
			"precision": 6,
			"outputFormat": "csv",
			"bomEnabled": true,
			"inputDir": "./input",
			"outputDir": "./output",
			"operateDir": "./operate"
		}`
		err := os.WriteFile(configFile, []byte(legacyJSON), fsperm.FilePerm)
		require.NoError(t, err)

		loaded, err := LoadConfig(configFile)
		require.NoError(t, err, "舊版 config 缺 language 不應導致 LoadConfig 失敗")
		require.NotNil(t, loaded)
		require.Equal(t, "zh-TW", loaded.Language, "缺 language 時應 default 為 zh-TW")
		// 確認其他使用者設定值有保留(沒因 Validate 失敗被吞掉)
		require.Equal(t, 25, loaded.ScalingFactor)
		require.Equal(t, []string{"階段A", "階段B"}, loaded.PhaseLabels)
		require.Equal(t, 6, loaded.Precision)
	})
}

func TestAppConfig_SaveConfig(t *testing.T) {
	t.Run("ValidSave", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "save_test.json")

		cfg := &AppConfig{
			ScalingFactor: 15,
			PhaseLabels:   []string{"保存測試階段1", "保存測試階段2"},
			Precision:     12,
			OutputFormat:  "xlsx",
			BOMEnabled:    false,
			InputDir:      "./input",
			OutputDir:     "./output",
			OperateDir:    "./value_operate",
			Language:      "zh-TW",
		}

		err := cfg.SaveConfig(configFile)
		require.NoError(t, err)

		// 檢查文件是否存在
		_, err = os.Stat(configFile)
		require.NoError(t, err)

		// 重新載入並驗證
		reloadedConfig, err := LoadConfig(configFile)
		require.NoError(t, err)
		require.Equal(t, cfg.ScalingFactor, reloadedConfig.ScalingFactor)
		require.Equal(t, cfg.PhaseLabels, reloadedConfig.PhaseLabels)
		require.Equal(t, cfg.Precision, reloadedConfig.Precision)
		require.Equal(t, cfg.OutputFormat, reloadedConfig.OutputFormat)
		require.Equal(t, cfg.BOMEnabled, reloadedConfig.BOMEnabled)
	})

	t.Run("SaveLoadRoundTrip", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "roundtrip_test.json")

		originalConfig := DefaultConfig()
		originalConfig.ScalingFactor = 25
		originalConfig.Precision = 6

		// 保存
		err := originalConfig.SaveConfig(configFile)
		require.NoError(t, err)

		// 載入
		loadedConfig, err := LoadConfig(configFile)
		require.NoError(t, err)

		// 比較
		require.Equal(t, originalConfig.ScalingFactor, loadedConfig.ScalingFactor)
		require.Equal(t, originalConfig.PhaseLabels, loadedConfig.PhaseLabels)
		require.Equal(t, originalConfig.Precision, loadedConfig.Precision)
		require.Equal(t, originalConfig.OutputFormat, loadedConfig.OutputFormat)
		require.Equal(t, originalConfig.BOMEnabled, loadedConfig.BOMEnabled)
	})

	t.Run("InvalidDirectory", func(t *testing.T) {
		cfg := DefaultConfig()
		invalidPath := "/nonexistent/directory/config.json"
		err := cfg.SaveConfig(invalidPath)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法創建配置檔案")
	})
}

func TestAppConfig_ToAnalysisConfig(t *testing.T) {
	cfg := &AppConfig{
		ScalingFactor: 15,
		PhaseLabels:   []string{"測試階段1", "測試階段2", "測試階段3"},
		Precision:     8,
		OutputFormat:  "json",
		BOMEnabled:    true,
	}

	analysisConfig := cfg.ToAnalysisConfig()
	require.NotNil(t, analysisConfig)
	require.Equal(t, cfg.ScalingFactor, analysisConfig.ScalingFactor)
	require.Equal(t, cfg.PhaseLabels, analysisConfig.PhaseLabels)
	require.WithinDuration(t, time.Now(), analysisConfig.CreatedAt, time.Second)
}

func TestAppConfig_ProcessingOptions(t *testing.T) {
	cfg := &AppConfig{
		ScalingFactor: 10,
		PhaseLabels:   []string{"階段1"},
		Precision:     12,
		OutputFormat:  "xlsx",
		BOMEnabled:    false,
	}

	options := cfg.ProcessingOptions()
	require.NotNil(t, options)
	require.True(t, options.ValidateInput)
	require.Equal(t, cfg.Precision, options.Precision)
	require.Equal(t, cfg.OutputFormat, options.OutputFormat)
}
