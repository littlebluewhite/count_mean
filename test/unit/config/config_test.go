package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	require.NotNil(t, cfg)
	require.Equal(t, 10, cfg.ScalingFactor)
	require.Equal(t, []string{"啟跳下蹲階段", "啟跳上升階段", "團身階段", "下降階段"}, cfg.PhaseLabels)
	require.Equal(t, 10, cfg.Precision)
	require.Equal(t, "csv", cfg.OutputFormat)
	require.True(t, cfg.BOMEnabled)
}

func TestAppConfig_Validate(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := &config.AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1", "階段2"},
			Precision:     5,
			OutputFormat:  "csv",
			BOMEnabled:    true,
			InputDir:      "./input",
			OutputDir:     "./output",
			OperateDir:    "./value_operate",
		}
		err := cfg.Validate()
		require.NoError(t, err)
	})

	t.Run("InvalidScalingFactor_Zero", func(t *testing.T) {
		cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
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
		configs := []*config.AppConfig{
			{ScalingFactor: 10, PhaseLabels: []string{"階段1"}, Precision: 0, OutputFormat: "csv", InputDir: "./input", OutputDir: "./output", OperateDir: "./value_operate"},
			{ScalingFactor: 10, PhaseLabels: []string{"階段1"}, Precision: 15, OutputFormat: "csv", InputDir: "./input", OutputDir: "./output", OperateDir: "./value_operate"},
		}
		for _, cfg := range configs {
			err := cfg.Validate()
			require.NoError(t, err)
		}
	})

	t.Run("ValidOutputFormats", func(t *testing.T) {
		validFormats := []string{"csv", "json", "xlsx"}
		for _, format := range validFormats {
			cfg := &config.AppConfig{
				ScalingFactor: 10,
				PhaseLabels:   []string{"階段1"},
				Precision:     5,
				OutputFormat:  format,
				InputDir:      "./input",
				OutputDir:     "./output",
				OperateDir:    "./value_operate",
			}
			err := cfg.Validate()
			require.NoError(t, err)
		}
	})

	t.Run("InvalidOutputFormat", func(t *testing.T) {
		invalidFormats := []string{"txt", "xml", "pdf", ""}
		for _, format := range invalidFormats {
			cfg := &config.AppConfig{
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
		cfg := &config.AppConfig{
			ScalingFactor: 10,
			PhaseLabels:   []string{"階段1"},
			Precision:     5,
			OutputFormat:  "CSV", // 大寫應該失敗
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "不支援的輸出格式: CSV")
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("FileNotExists_ReturnDefault", func(t *testing.T) {
		loadedConfig, err := config.LoadConfig("nonexistent.json")
		require.NoError(t, err)
		require.NotNil(t, loadedConfig)
		// 應該返回默認配置
		defaultConfig := config.DefaultConfig()
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
			"scaling_factor": 20,
			"phase_labels": ["自定義階段1", "自定義階段2"],
			"precision": 8,
			"output_format": "json",
			"bom_enabled": false,
			"input_dir": "./input",
			"output_dir": "./output",
			"operate_dir": "./value_operate"
		}`

		err := os.WriteFile(configFile, []byte(validJSON), 0644)
		require.NoError(t, err)

		loadedConfig, err := config.LoadConfig(configFile)
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

		err := os.WriteFile(configFile, []byte(invalidJSON), 0644)
		require.NoError(t, err)

		loadedConfig, err := config.LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析配置檔案失敗")
		require.Nil(t, loadedConfig)
	})

	t.Run("InvalidConfigValues", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "invalid_values_config.json")

		invalidConfig := `{
			"scaling_factor": -1,
			"phase_labels": [],
			"precision": 5,
			"output_format": "csv",
			"bom_enabled": true
		}`

		err := os.WriteFile(configFile, []byte(invalidConfig), 0644)
		require.NoError(t, err)

		loadedConfig, err := config.LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "配置檔案無效")
		require.Nil(t, loadedConfig)
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("跳過權限測試（以root用戶運行）")
		}

		// 創建一個無權限訪問的目錄
		tempDir := t.TempDir()
		restrictedDir := filepath.Join(tempDir, "restricted")
		err := os.Mkdir(restrictedDir, 0000) // 無權限
		require.NoError(t, err)
		defer os.Chmod(restrictedDir, 0755) // 清理時恢復權限

		configFile := filepath.Join(restrictedDir, "config.json")
		loadedConfig, err := config.LoadConfig(configFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法開啟配置檔案")
		require.Nil(t, loadedConfig)
	})
}

func TestAppConfig_SaveConfig(t *testing.T) {
	t.Run("ValidSave", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "save_test.json")

		cfg := &config.AppConfig{
			ScalingFactor: 15,
			PhaseLabels:   []string{"保存測試階段1", "保存測試階段2"},
			Precision:     12,
			OutputFormat:  "xlsx",
			BOMEnabled:    false,
			InputDir:      "./input",
			OutputDir:     "./output",
			OperateDir:    "./value_operate",
		}

		err := cfg.SaveConfig(configFile)
		require.NoError(t, err)

		// 檢查文件是否存在
		_, err = os.Stat(configFile)
		require.NoError(t, err)

		// 重新載入並驗證
		reloadedConfig, err := config.LoadConfig(configFile)
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

		originalConfig := config.DefaultConfig()
		originalConfig.ScalingFactor = 25
		originalConfig.Precision = 6

		// 保存
		err := originalConfig.SaveConfig(configFile)
		require.NoError(t, err)

		// 載入
		loadedConfig, err := config.LoadConfig(configFile)
		require.NoError(t, err)

		// 比較
		require.Equal(t, originalConfig.ScalingFactor, loadedConfig.ScalingFactor)
		require.Equal(t, originalConfig.PhaseLabels, loadedConfig.PhaseLabels)
		require.Equal(t, originalConfig.Precision, loadedConfig.Precision)
		require.Equal(t, originalConfig.OutputFormat, loadedConfig.OutputFormat)
		require.Equal(t, originalConfig.BOMEnabled, loadedConfig.BOMEnabled)
	})

	t.Run("InvalidDirectory", func(t *testing.T) {
		cfg := config.DefaultConfig()
		invalidPath := "/nonexistent/directory/config.json"
		err := cfg.SaveConfig(invalidPath)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法創建配置檔案")
	})
}

func TestAppConfig_ToAnalysisConfig(t *testing.T) {
	cfg := &config.AppConfig{
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
	cfg := &config.AppConfig{
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
