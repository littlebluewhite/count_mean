package integration

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/security"
	"count_mean/internal/validation"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_FullWorkflow(t *testing.T) {
	// Setup test directories
	tempDir := t.TempDir()
	inputDir := filepath.Join(tempDir, "input")
	outputDir := filepath.Join(tempDir, "output")
	operateDir := filepath.Join(tempDir, "operate")

	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.MkdirAll(outputDir, 0755))
	require.NoError(t, os.MkdirAll(operateDir, 0755))

	// Create test config
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		PhaseLabels:   []string{"Phase1", "Phase2", "Phase3"},
		Precision:     2,
		OutputFormat:  "csv",
		BOMEnabled:    false,
		InputDir:      inputDir,
		OutputDir:     outputDir,
		OperateDir:    operateDir,
	}

	// Initialize logging for tests
	err := logging.InitLogger(logging.LevelDebug, tempDir, false)
	require.NoError(t, err)

	t.Run("CompleteEMGDataProcessing", func(t *testing.T) {
		// Create test CSV data
		testData := [][]string{
			{"Time", "EMG_Channel1", "EMG_Channel2"},
			{"0.1", "100.5", "50.2"},
			{"0.2", "120.3", "55.1"},
			{"0.3", "110.8", "52.3"},
			{"0.4", "130.2", "58.7"},
			{"0.5", "125.6", "56.9"},
		}

		// Create CSV handler and write test file
		csvHandler := io.NewCSVHandler(cfg)
		testFilePath := filepath.Join(inputDir, "test_emg.csv")
		err := csvHandler.WriteCSV(testFilePath, testData)
		require.NoError(t, err)

		// Test reading the file back
		records, err := csvHandler.ReadCSV(testFilePath)
		require.NoError(t, err)
		assert.Equal(t, len(testData), len(records))
		assert.Equal(t, testData[0], records[0])

		// Test calculator
		calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
		results, err := calculator.CalculateFromRawData(records, 2)
		require.NoError(t, err)
		assert.Len(t, results, 2) // Two channels

		// Verify results structure
		for i, result := range results {
			assert.Equal(t, i+1, result.ColumnIndex)
			assert.Greater(t, result.MaxMean, 0.0)
			assert.Greater(t, result.EndTime, result.StartTime)
		}

		// Test output generation
		outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, 0.1, 0.5)
		outputPath := filepath.Join(outputDir, "test_results.csv")
		err = csvHandler.WriteCSV(outputPath, outputData)
		require.NoError(t, err)

		// Verify output file exists and is readable
		_, err = csvHandler.ReadCSV(outputPath)
		require.NoError(t, err)
	})

	t.Run("SecurityAndValidationIntegration", func(t *testing.T) {
		validator := validation.NewInputValidator()
		pathValidator := security.NewPathValidator([]string{inputDir, outputDir})

		// Test path validation integration
		validPath := filepath.Join(inputDir, "valid.csv")
		err := pathValidator.ValidateFilePath(validPath)
		assert.NoError(t, err)

		// Test invalid path
		invalidPath := "../../../etc/passwd"
		err = pathValidator.ValidateFilePath(invalidPath)
		assert.Error(t, err)

		// Test filename validation
		err = validator.ValidateFilename("valid.csv")
		assert.NoError(t, err)

		err = validator.ValidateFilename("invalid\x00.csv")
		assert.Error(t, err)

		// Test window size validation
		windowSize, err := validator.ValidateWindowSize("10")
		assert.NoError(t, err)
		assert.Equal(t, 10, windowSize)

		_, err = validator.ValidateWindowSize("-5")
		assert.Error(t, err)
	})

	t.Run("ErrorHandlingIntegration", func(t *testing.T) {
		csvHandler := io.NewCSVHandler(cfg)

		// Test reading non-existent file
		_, err := csvHandler.ReadCSV(filepath.Join(inputDir, "nonexistent.csv"))
		require.Error(t, err)

		// Should be an AppError
		var appErr *errors.AppError
		assert.ErrorAs(t, err, &appErr)
		assert.Equal(t, errors.ErrCodeFileNotFound, appErr.Code)

		// Test writing to invalid path
		err = csvHandler.WriteCSV("/invalid/path/test.csv", [][]string{{"test"}})
		require.Error(t, err)
		assert.ErrorAs(t, err, &appErr)
	})

	t.Run("ConfigurationIntegration", func(t *testing.T) {
		// Test config validation
		err := cfg.Validate()
		assert.NoError(t, err)

		// Test invalid config
		invalidCfg := &config.AppConfig{
			ScalingFactor: -1, // Invalid
			PhaseLabels:   []string{},
			Precision:     20, // Invalid
			OutputFormat:  "invalid",
			InputDir:      "",
			OutputDir:     "",
			OperateDir:    "",
		}

		err = invalidCfg.Validate()
		assert.Error(t, err)

		// Test config save/load
		configPath := filepath.Join(tempDir, "test_config.json")
		err = cfg.SaveConfig(configPath)
		require.NoError(t, err)

		loadedCfg, err := config.LoadConfig(configPath)
		require.NoError(t, err)
		assert.Equal(t, cfg.ScalingFactor, loadedCfg.ScalingFactor)
		assert.Equal(t, cfg.PhaseLabels, loadedCfg.PhaseLabels)
	})

	t.Run("LoggingIntegration", func(t *testing.T) {
		logger := logging.GetLogger("integration_test")

		// Test different log levels
		logger.Debug("debug message")
		logger.Info("info message")
		logger.Warn("warn message")

		// Test error logging with AppError
		appErr := errors.NewAppError(errors.ErrCodeFileNotFound, "test error")
		logger.Error("operation failed", appErr)

		// Test context logging
		contextLogger := logger.WithContext("operation", "test").WithContext("file", "test.csv")
		contextLogger.Info("processing file")
	})

	t.Run("LargeDatasetHandling", func(t *testing.T) {
		// Create a larger dataset
		largeData := [][]string{
			{"Time", "Ch1", "Ch2", "Ch3"},
		}

		// Add 1000 data points
		for i := 0; i < 1000; i++ {
			row := []string{
				fmt.Sprintf("%.3f", float64(i)*0.001),
				fmt.Sprintf("%.1f", float64(i%100+50)),
				fmt.Sprintf("%.1f", float64((i*2)%150+25)),
				fmt.Sprintf("%.1f", float64((i*3)%200+30)),
			}
			largeData = append(largeData, row)
		}

		csvHandler := io.NewCSVHandler(cfg)
		largeFilePath := filepath.Join(inputDir, "large_test.csv")
		err := csvHandler.WriteCSV(largeFilePath, largeData)
		require.NoError(t, err)

		// Test processing large dataset
		calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
		results, err := calculator.CalculateFromRawData(largeData, 10)
		require.NoError(t, err)
		assert.Len(t, results, 3) // Three channels

		// Verify performance is reasonable (should complete quickly)
		// This is a basic performance check
		records, err := csvHandler.ReadCSV(largeFilePath)
		require.NoError(t, err)
		assert.Len(t, records, 1001) // Header + 1000 data rows
	})
}

func TestIntegration_ErrorRecovery(t *testing.T) {
	tempDir := t.TempDir()
	inputDir := filepath.Join(tempDir, "input")
	outputDir := filepath.Join(tempDir, "output")

	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	cfg := &config.AppConfig{
		ScalingFactor: 10,
		PhaseLabels:   []string{"Phase1"},
		Precision:     2,
		OutputFormat:  "csv",
		BOMEnabled:    false,
		InputDir:      inputDir,
		OutputDir:     outputDir,
		OperateDir:    tempDir,
	}

	t.Run("RecoveryFromBadCSVData", func(t *testing.T) {
		// Create CSV with inconsistent columns
		badData := [][]string{
			{"Time", "Ch1", "Ch2"},
			{"0.1", "100"}, // Missing column
			{"0.2", "120", "50"},
			{"0.3", "110", "55", "extra"}, // Extra column
		}

		csvHandler := io.NewCSVHandler(cfg)
		badFilePath := filepath.Join(inputDir, "bad_data.csv")

		// Write bad data (this should work)
		err := csvHandler.WriteCSV(badFilePath, badData)
		require.NoError(t, err)

		// Reading should fail with validation error
		_, err = csvHandler.ReadCSV(badFilePath)
		require.Error(t, err)

		// Should be a validation error
		var validationErr *errors.ValidationError
		assert.ErrorAs(t, err, &validationErr)
	})

	t.Run("RecoveryFromInvalidNumbers", func(t *testing.T) {
		// Create CSV with invalid numeric data
		invalidData := [][]string{
			{"Time", "Ch1"},
			{"0.1", "100"},
			{"invalid", "120"},      // Invalid time
			{"0.3", "not_a_number"}, // Invalid channel data
		}

		csvHandler := io.NewCSVHandler(cfg)
		invalidFilePath := filepath.Join(inputDir, "invalid_numbers.csv")
		err := csvHandler.WriteCSV(invalidFilePath, invalidData)
		require.NoError(t, err)

		// Calculator should handle parsing errors gracefully
		calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
		_, err = calculator.CalculateFromRawData(invalidData, 2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "解析")
	})
}

func TestIntegration_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent operations test in short mode")
	}

	tempDir := t.TempDir()
	inputDir := filepath.Join(tempDir, "input")
	outputDir := filepath.Join(tempDir, "output")

	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	cfg := &config.AppConfig{
		ScalingFactor: 10,
		PhaseLabels:   []string{"Phase1"},
		Precision:     2,
		OutputFormat:  "csv",
		BOMEnabled:    false,
		InputDir:      inputDir,
		OutputDir:     outputDir,
		OperateDir:    tempDir,
	}

	t.Run("ConcurrentFileOperations", func(t *testing.T) {
		csvHandler := io.NewCSVHandler(cfg)

		// Create multiple test files concurrently
		const numFiles = 5
		done := make(chan bool, numFiles)

		for i := 0; i < numFiles; i++ {
			go func(fileNum int) {
				defer func() { done <- true }()

				testData := [][]string{
					{"Time", "Ch1"},
					{"0.1", fmt.Sprintf("%.1f", float64(fileNum*10+100))},
					{"0.2", fmt.Sprintf("%.1f", float64(fileNum*10+110))},
				}

				filePath := filepath.Join(inputDir, fmt.Sprintf("concurrent_test_%d.csv", fileNum))
				err := csvHandler.WriteCSV(filePath, testData)
				assert.NoError(t, err)

				// Read back the file
				records, err := csvHandler.ReadCSV(filePath)
				assert.NoError(t, err)
				assert.Len(t, records, 3)
			}(i)
		}

		// Wait for all operations to complete
		for i := 0; i < numFiles; i++ {
			<-done
		}
	})
}
