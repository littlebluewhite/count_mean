package new_gui

import (
	"bytes"
	"context"
	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/validation"
	"encoding/base64"
	"fmt"
	"gonum.org/v1/plot/vg"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx           context.Context
	config        *config.AppConfig
	logger        *logging.Logger
	csvHandler    *io.CSVHandler
	maxMeanCalc   *calculator.MaxMeanCalculator
	normalizer    *calculator.Normalizer
	phaseAnalyzer *calculator.PhaseAnalyzer
	chartGen      *chart.EChartsGenerator
	validator     *validation.InputValidator
}

// NewApp creates a new App application struct
func NewApp(cfg *config.AppConfig) *App {
	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
	normalizer := calculator.NewNormalizer(cfg.ScalingFactor)
	phaseAnalyzer := calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels)
	chartGen := chart.NewEChartsGenerator()
	validator := validation.NewInputValidator()
	logger := logging.GetLogger("app")

	return &App{
		config:        cfg,
		logger:        logger,
		csvHandler:    csvHandler,
		maxMeanCalc:   maxMeanCalc,
		normalizer:    normalizer,
		phaseAnalyzer: phaseAnalyzer,
		chartGen:      chartGen,
		validator:     validator,
	}
}

// startup is called when the app starts. The context is saved
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.logger.Info("Wails 應用程序啟動")

	// 確保必要的目錄存在
	if err := a.config.EnsureDirectories(); err != nil {
		a.logger.Error("無法創建必要目錄", err)
	}
}

// GetConfig returns the current configuration
func (a *App) GetConfig() *config.AppConfig {
	return a.config
}

// SaveConfig saves the configuration
func (a *App) SaveConfig(cfg *config.AppConfig) error {
	a.config = cfg
	return cfg.SaveConfig("./config.json")
}

// ResetConfig resets to default configuration
func (a *App) ResetConfig() *config.AppConfig {
	a.config = config.DefaultConfig()
	return a.config
}

// SelectFile opens a file dialog for file selection
func (a *App) SelectFile(title string, filters []runtime.FileFilter, buttonType string) (string, error) {
	var defaultDir string
	print("buttonType:", buttonType)
	switch buttonType {
	case "input":
		defaultDir = a.config.InputDir
	case "output":
		defaultDir = a.config.OutputDir
	case "operate":
		defaultDir = a.config.OperateDir
	}
	print("defaultDir:", defaultDir)

	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
		Filters:          filters,
	}

	file, err := runtime.OpenFileDialog(a.ctx, options)
	if err != nil {
		return "", err
	}

	return file, nil
}

// SelectDirectory opens a directory dialog
func (a *App) SelectDirectory(title string) (string, error) {
	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: a.config.InputDir,
	}

	dir, err := runtime.OpenDirectoryDialog(a.ctx, options)
	if err != nil {
		return "", err
	}

	return dir, nil
}

// CalculateMaxMean calculates maximum mean values
func (a *App) CalculateMaxMean(params MaxMeanParams) (*MaxMeanResult, error) {
	a.logger.Info("開始最大平均值計算", map[string]interface{}{
		"input_path":  params.InputPath,
		"window_size": params.WindowSize,
		"is_batch":    params.IsBatch,
	})

	fmt.Printf("%+v\n", params)

	// 批次處理模式
	if params.IsBatch {
		return a.calculateMaxMeanBatch(params)
	}

	// 單檔案處理模式
	return a.calculateMaxMeanSingle(params)
}

// calculateMaxMeanSingle 處理單個檔案
func (a *App) calculateMaxMeanSingle(params MaxMeanParams) (*MaxMeanResult, error) {
	// 驗證檔案名稱
	filename := filepath.Base(params.InputPath)
	if err := a.validator.ValidateFilename(filename); err != nil {
		return nil, fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	// 檢查檔案是否在允許的目錄範圍內，決定使用哪種讀取方法
	var records [][]string
	var err error

	// 檢查檔案是否為絕對路徑且在輸入目錄外
	if filepath.IsAbs(params.InputPath) {
		fileDir := filepath.Dir(params.InputPath)
		relPath, relErr := filepath.Rel(a.config.InputDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			// 檔案在輸入目錄外，使用外部讀取方法（跳過路徑驗證）
			records, err = a.csvHandler.ReadCSVExternal(params.InputPath)
		} else {
			// 檔案在輸入目錄內，使用正常讀取方法
			records, err = a.csvHandler.ReadCSV(params.InputPath)
		}
	} else {
		// 相對路徑，使用正常讀取方法
		records, err = a.csvHandler.ReadCSV(params.InputPath)
	}

	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	fileName := filepath.Base(params.InputPath)
	originalFileName := strings.TrimSuffix(fileName, ".csv")

	// 如果沒有指定時間範圍，使用數據中的預設範圍
	startRange, endRange := params.StartTime, params.EndTime
	useCustomRange := startRange != 0 || endRange != 0

	if !useCustomRange {
		if len(records) > 1 && len(records[1]) > 0 {
			startRange, _ = strconv.ParseFloat(records[1][0], 64)
		}
		if len(records) > 1 && len(records[len(records)-1]) > 0 {
			endRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
		}
	}

	// 計算最大平均值
	var results []models.MaxMeanResult
	if startRange == 0 && endRange == 0 {
		results, err = a.maxMeanCalc.CalculateFromRawData(records, params.WindowSize)
	} else {
		results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, params.WindowSize, startRange, endRange)
	}

	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", originalFileName)

	if err := a.csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
		return nil, fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}

	// 準備回傳結果
	outputPath := filepath.Join(a.config.OutputDir, outputFile)

	// 將結果轉換為回傳格式
	var resultData [][]float64
	for _, result := range results {
		row := []float64{result.MaxMean, result.StartTime, result.EndTime}
		resultData = append(resultData, row)
	}

	return &MaxMeanResult{
		OutputPath: outputPath,
		Headers:    records[0],
		Results:    resultData,
	}, nil
}

// calculateMaxMeanBatch 批次處理資料夾中的所有CSV檔案
func (a *App) calculateMaxMeanBatch(params MaxMeanParams) (*MaxMeanResult, error) {
	fmt.Println("1批次處理資料夾中的所有CSV檔案")
	// 驗證目錄路徑
	if err := a.validator.ValidateDirectoryPath(params.InputPath); err != nil {
		return nil, fmt.Errorf("目錄路徑驗證失敗: %w", err)
	}

	// 取得資料夾名稱（相對於輸入目錄）
	var dirName string
	var csvFiles []string
	var err error

	fmt.Println("2批次處理資料夾中的所有CSV檔案")

	// 檢查是否為輸入目錄的子目錄
	if filepath.IsAbs(params.InputPath) {
		// 如果是絕對路徑，檢查是否在輸入目錄下
		relPath, err := filepath.Rel(a.config.InputDir, params.InputPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			// 不在輸入目錄下，直接使用目錄中的文件
			files, err := filepath.Glob(filepath.Join(params.InputPath, "*.csv"))
			if err != nil {
				return nil, fmt.Errorf("搜尋CSV文件失敗: %w", err)
			}
			if len(files) == 0 {
				return nil, fmt.Errorf("資料夾中沒有找到CSV文件")
			}
			// 對於外部目錄，直接處理文件
			return a.executeBatchCalculationDirect(files, filepath.Base(params.InputPath), params.WindowSize, params.StartTime, params.EndTime)
		}
		dirName = relPath
	} else {
		dirName = params.InputPath
	}

	// 使用CSV處理器的目錄方法列出文件
	csvFiles, err = a.csvHandler.ListCSVFilesInDirectory(dirName)
	if err != nil {
		return nil, fmt.Errorf("列出CSV文件失敗: %w", err)
	}

	if len(csvFiles) == 0 {
		return nil, fmt.Errorf("資料夾中沒有找到CSV文件")
	}

	// 準備批次結果
	var allHeaders []string
	var allResults [][]float64
	successCount := 0
	failCount := 0

	// 處理每個文件
	for _, fileName := range csvFiles {
		fileBaseName := strings.TrimSuffix(fileName, ".csv")

		records, err := a.csvHandler.ReadCSVFromDirectory(dirName, fileName)
		if err != nil {
			failCount++
			a.logger.Error("讀取檔案失敗", err, map[string]interface{}{
				"file": fileName,
			})
			continue // 跳過有錯誤的文件
		}

		// 如果沒有使用自定義範圍，從數據中獲取預設範圍
		actualStartRange, actualEndRange := params.StartTime, params.EndTime
		useCustomRange := actualStartRange != 0 || actualEndRange != 0

		if !useCustomRange {
			if len(records) > 1 && len(records[1]) > 0 {
				actualStartRange, _ = strconv.ParseFloat(records[1][0], 64)
			}
			if len(records) > 1 && len(records[len(records)-1]) > 0 {
				actualEndRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
			}
		}

		// 計算最大平均值
		var results []models.MaxMeanResult
		if actualStartRange == 0 && actualEndRange == 0 {
			results, err = a.maxMeanCalc.CalculateFromRawData(records, params.WindowSize)
		} else {
			results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, params.WindowSize, actualStartRange, actualEndRange)
		}

		if err != nil {
			failCount++
			a.logger.Error("計算失敗", err, map[string]interface{}{
				"file": fileName,
			})
			continue
		}

		// 第一個文件時設置標題
		if len(allHeaders) == 0 && len(records) > 0 {
			allHeaders = records[0]
		}

		// 將結果轉換為回傳格式
		for _, result := range results {
			row := []float64{result.MaxMean, result.StartTime, result.EndTime}
			allResults = append(allResults, row)
		}

		// 輸出結果
		outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, actualStartRange, actualEndRange)
		outputFile := fmt.Sprintf("%s_最大平均值計算.csv", fileBaseName)

		err = a.csvHandler.WriteCSVToOutputDirectory(filepath.Base(dirName), outputFile, outputData)
		if err != nil {
			failCount++
			a.logger.Error("寫入輸出檔案失敗", err, map[string]interface{}{
				"file": outputFile,
			})
			continue
		}

		successCount++
		a.logger.Info("檔案處理成功", map[string]interface{}{
			"file":          fileName,
			"results_count": len(results),
		})
	}

	// 準備回傳結果
	message := fmt.Sprintf("批次處理完成：成功 %d 個檔案，失敗 %d 個檔案", successCount, failCount)

	return &MaxMeanResult{
		OutputPath: filepath.Join(a.config.OutputDir, dirName),
		Headers:    allHeaders,
		Results:    allResults,
		Success:    successCount > 0,
		Message:    message,
	}, nil
}

// executeBatchCalculationDirect 直接處理外部目錄的批次計算
func (a *App) executeBatchCalculationDirect(files []string, outputDirName string, windowSize int, startTime, endTime float64) (*MaxMeanResult, error) {
	var allHeaders []string
	var allResults [][]float64
	successCount := 0
	failCount := 0

	for _, fullPath := range files {
		fileName := filepath.Base(fullPath)
		fileBaseName := strings.TrimSuffix(fileName, ".csv")
		records, err := a.csvHandler.ReadCSVExternal(fullPath)
		if err != nil {
			failCount++
			continue
		}

		// 如果沒有使用自定義範圍，從數據中獲取預設範圍
		actualStartRange, actualEndRange := startTime, endTime
		useCustomRange := actualStartRange != 0 || actualEndRange != 0

		if !useCustomRange {
			if len(records) > 1 && len(records[1]) > 0 {
				actualStartRange, _ = strconv.ParseFloat(records[1][0], 64)
			}
			if len(records) > 1 && len(records[len(records)-1]) > 0 {
				actualEndRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
			}
		}

		// 計算最大平均值
		var results []models.MaxMeanResult
		if actualStartRange == 0 && actualEndRange == 0 {
			results, err = a.maxMeanCalc.CalculateFromRawData(records, windowSize)
		} else {
			results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, windowSize, actualStartRange, actualEndRange)
		}

		if err != nil {
			failCount++
			continue
		}

		// 輸出結果
		outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, actualStartRange, actualEndRange)
		outputFile := fmt.Sprintf("%s_最大平均值計算.csv", fileBaseName)

		err = a.csvHandler.WriteCSVToOutputDirectory(outputDirName, outputFile, outputData)
		if err != nil {
			failCount++
			continue
		}
		successCount++
		a.logger.Info("檔案處理成功", map[string]interface{}{
			"file":          fileName,
			"results_count": len(results),
		})
	}

	message := fmt.Sprintf("批次處理完成：成功 %d 個檔案，失敗 %d 個檔案", successCount, failCount)

	return &MaxMeanResult{
		OutputPath: outputDirName,
		Headers:    allHeaders,
		Results:    allResults,
		Success:    successCount > 0,
		Message:    message,
	}, nil
}

// NormalizeData performs data normalization
func (a *App) NormalizeData(params NormalizeParams) (*NormalizeResult, error) {
	a.logger.Info("開始資料標準化", map[string]interface{}{
		"main_file":      params.MainFile,
		"reference_file": params.ReferenceFile,
		"output_path":    params.OutputPath,
	})

	// 驗證輸入
	if params.MainFile == "" {
		return nil, fmt.Errorf("請選擇主要資料檔案")
	}

	if params.ReferenceFile == "" {
		return nil, fmt.Errorf("請選擇參考資料檔案")
	}

	// 驗證檔案名稱
	mainFilename := filepath.Base(params.MainFile)
	if err := a.validator.ValidateFilename(mainFilename); err != nil {
		return nil, fmt.Errorf("主要檔案名稱驗證失敗: %w", err)
	}

	refFilename := filepath.Base(params.ReferenceFile)
	if err := a.validator.ValidateFilename(refFilename); err != nil {
		return nil, fmt.Errorf("參考檔案名稱驗證失敗: %w", err)
	}

	// 讀取主要資料檔案
	var mainRecords [][]string
	var err error

	// 檢查主檔案是否在允許的目錄範圍內
	if filepath.IsAbs(params.MainFile) {
		fileDir := filepath.Dir(params.MainFile)
		relPath, relErr := filepath.Rel(a.config.InputDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			mainRecords, err = a.csvHandler.ReadCSVExternal(params.MainFile)
		} else {
			mainRecords, err = a.csvHandler.ReadCSV(params.MainFile)
		}
	} else {
		mainRecords, err = a.csvHandler.ReadCSV(params.MainFile)
	}

	if err != nil {
		return nil, fmt.Errorf("讀取主要資料檔案失敗: %w", err)
	}

	// 讀取參考資料檔案
	var refRecords [][]string

	// 檢查參考檔案是否在允許的目錄範圍內
	if filepath.IsAbs(params.ReferenceFile) {
		fileDir := filepath.Dir(params.ReferenceFile)
		relPath, relErr := filepath.Rel(a.config.OperateDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			refRecords, err = a.csvHandler.ReadCSVExternal(params.ReferenceFile)
		} else {
			// 對於 operate 目錄中的檔案，直接讀取
			refRecords, err = a.csvHandler.ReadCSV(params.ReferenceFile)
		}
	} else {
		refRecords, err = a.csvHandler.ReadCSV(params.ReferenceFile)
	}

	if err != nil {
		return nil, fmt.Errorf("讀取參考資料檔案失敗: %w", err)
	}

	// 執行標準化
	normalizedData, err := a.normalizer.NormalizeFromRawData(mainRecords, refRecords)
	if err != nil {
		return nil, fmt.Errorf("標準化計算失敗: %w", err)
	}

	// 生成輸出檔名
	outputName := params.OutputPath
	if outputName == "" {
		mainFileName := filepath.Base(params.MainFile)
		mainBaseName := strings.TrimSuffix(mainFileName, ".csv")
		outputName = fmt.Sprintf("%s_標準化.csv", mainBaseName)
	} else if !strings.HasSuffix(outputName, ".csv") {
		outputName += ".csv"
	}

	// 轉換為CSV格式
	outputData := a.csvHandler.ConvertNormalizedDataToCSV(normalizedData)

	// 保存結果
	err = a.csvHandler.WriteCSVToOutput(outputName, outputData)
	if err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 準備回傳結果
	outputPath := filepath.Join(a.config.OutputDir, outputName)

	// 將標準化數據轉換為二維浮點數陣列
	var data [][]float64
	for _, row := range normalizedData.Data {
		var floatRow []float64
		floatRow = append(floatRow, row.Time)
		floatRow = append(floatRow, row.Channels...)
		data = append(data, floatRow)
	}

	a.logger.Info("資料標準化完成", map[string]interface{}{
		"output_file":   outputPath,
		"data_points":   len(data),
		"channel_count": len(normalizedData.Headers) - 1,
	})

	return &NormalizeResult{
		OutputPath: outputPath,
		Headers:    normalizedData.Headers,
		Data:       data,
		Success:    true,
		Message:    "資料標準化成功完成",
	}, nil
}

// GenerateInteractiveChart returns HTML content of an interactive chart.
func (a *App) GenerateInteractiveChart(params InteractiveChartParams) (string, error) {
	// 讀取 CSV 檔案
	records, err := a.csvHandler.ReadCSV(params.FilePath)
	if err != nil {
		return "", fmt.Errorf("讀取 CSV 檔案失敗: %w", err)
	}
	if len(records) < 2 {
		return "", fmt.Errorf("CSV 檔案格式無效：需要至少包含標題和一行數據")
	}
	// 構建資料集
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}
	copy(dataset.Headers, records[0])
	for i := 1; i < len(records); i++ {
		row := records[i]
		timeVal, err := strconv.ParseFloat(row[0], 64)
		if err != nil {
			continue
		}
		channels := make([]float64, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, _ := strconv.ParseFloat(row[j], 64)
			channels[j-1] = val
		}
		dataset.Data = append(dataset.Data, models.EMGData{Time: timeVal, Channels: channels})
	}

	// 準備互動式圖表配置
	chartConfig := chart.InteractiveChartConfig{
		Title:           params.Title,
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "Value",
		SelectedColumns: params.Columns,
		ColumnNames:     nil,
		ShowAllColumns:  false,
		Width:           params.Width,
		Height:          params.Height,
	}

	// 生成互動式圖表 HTML
	var buf bytes.Buffer
	err = a.chartGen.RenderChartToWriter(dataset, chartConfig, &buf)
	if err != nil {
		return "", fmt.Errorf("生成互動式圖表失敗: %w", err)
	}

	return buf.String(), nil
}

// savePNGFromBase64 從 base64 數據保存 PNG 檔案
func (a *App) savePNGFromBase64(params ChartParams) (*ChartResult, error) {
	// 解析 base64 數據
	dataURL := params.ImageData
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, fmt.Errorf("無效的圖片數據格式")
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")
	pngData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("解碼圖片數據失敗: %w", err)
	}

	// 生成輸出檔名
	fileName := filepath.Base(params.FilePath)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	outputPath := filepath.Join(a.config.OutputDir, fmt.Sprintf("%s_%s.png", baseName, params.Title))

	// 保存 PNG 檔案
	if err := os.WriteFile(outputPath, pngData, 0644); err != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", err)
	}

	a.logger.Info("圖表下載完成", map[string]interface{}{
		"output_file": outputPath,
		"file_size":   len(pngData),
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已成功下載至: %s", outputPath),
	}, nil
}

// GenerateChart 依照目前預覽設定輸出 PNG 到 output_dir
func (a *App) GenerateChart(params ChartParams) (*ChartResult, error) {
	// 如果有 ImageData，直接保存 PNG
	if params.ImageData != "" {
		return a.savePNGFromBase64(params)
	}

	a.logger.Info("開始生成圖表", map[string]interface{}{
		"file_path": params.FilePath,
		"columns":   params.Columns,
		"title":     params.Title,
	})

	// 驗證輸入
	if params.FilePath == "" {
		return nil, fmt.Errorf("請選擇資料檔案")
	}

	if len(params.Columns) == 0 {
		return nil, fmt.Errorf("請選擇至少一個欄位")
	}

	// 驗證檔案名稱
	filename := filepath.Base(params.FilePath)
	if err := a.validator.ValidateFilename(filename); err != nil {
		return nil, fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	// 讀取 CSV 檔案
	var records [][]string
	var err error

	// 檢查檔案是否在允許的目錄範圍內
	if filepath.IsAbs(params.FilePath) {
		fileDir := filepath.Dir(params.FilePath)
		relPath, relErr := filepath.Rel(a.config.InputDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			records, err = a.csvHandler.ReadCSVExternal(params.FilePath)
		} else {
			records, err = a.csvHandler.ReadCSV(params.FilePath)
		}
	} else {
		records, err = a.csvHandler.ReadCSV(params.FilePath)
	}

	if err != nil {
		return nil, fmt.Errorf("讀取 CSV 檔案失敗: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV 檔案格式無效：需要至少包含標題和一行數據")
	}

	// 構建資料集
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}
	copy(dataset.Headers, records[0])

	for i := 1; i < len(records); i++ {
		row := records[i]
		timeVal, err := strconv.ParseFloat(row[0], 64)
		if err != nil {
			continue
		}
		channels := make([]float64, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, _ := strconv.ParseFloat(row[j], 64)
			channels[j-1] = val
		}
		dataset.Data = append(dataset.Data, models.EMGData{Time: timeVal, Channels: channels})
	}

	// 準備圖表配置
	chartConfig := chart.ChartConfig{
		Title:      params.Title,
		XAxisLabel: "Time (s)",
		YAxisLabel: "Value",
		Width:      vg.Length(800),
		Height:     vg.Length(600),
		Columns:    make([]string, len(params.Columns)),
	}

	// 將選中的列索引轉換為列名
	for i, colIndex := range params.Columns {
		if colIndex < len(dataset.Headers) {
			chartConfig.Columns[i] = dataset.Headers[colIndex]
		}
	}

	// 生成輸出路徑
	outputPath := filepath.Join(a.config.OutputDir, fmt.Sprintf("%s_chart.png",
		strings.TrimSuffix(filepath.Base(params.FilePath), filepath.Ext(params.FilePath))))

	a.logger.Info("圖表生成完成", map[string]interface{}{
		"output_file":  outputPath,
		"column_count": len(params.Columns),
		"data_points":  len(dataset.Data),
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已成功生成並保存到: %s", outputPath),
	}, nil
}

// AnalyzePhases performs phase analysis
func (a *App) AnalyzePhases(params PhaseParams) (*PhaseResult, error) {
	a.logger.Info("開始階段分析", map[string]interface{}{
		"input_file":   params.InputFile,
		"phase_labels": params.PhaseLabels,
		"output_path":  params.OutputPath,
	})

	// 驗證輸入
	if params.InputFile == "" {
		return nil, fmt.Errorf("請選擇資料檔案")
	}

	if len(params.PhaseLabels) == 0 {
		return nil, fmt.Errorf("請輸入階段標籤")
	}

	// 清理階段標籤（移除空白）
	var cleanLabels []string
	for _, label := range params.PhaseLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			cleanLabels = append(cleanLabels, trimmed)
		}
	}

	if len(cleanLabels) == 0 {
		return nil, fmt.Errorf("請輸入有效的階段標籤")
	}

	// 驗證檔案名稱
	filename := filepath.Base(params.InputFile)
	if err := a.validator.ValidateFilename(filename); err != nil {
		return nil, fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	// 讀取資料檔案
	var records [][]string
	var err error

	// 檢查檔案是否在允許的目錄範圍內
	if filepath.IsAbs(params.InputFile) {
		fileDir := filepath.Dir(params.InputFile)
		relPath, relErr := filepath.Rel(a.config.InputDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			records, err = a.csvHandler.ReadCSVExternal(params.InputFile)
		} else {
			records, err = a.csvHandler.ReadCSV(params.InputFile)
		}
	} else {
		records, err = a.csvHandler.ReadCSV(params.InputFile)
	}

	if err != nil {
		return nil, fmt.Errorf("讀取資料檔案失敗: %w", err)
	}

	// 執行階段分析
	analysisResult, err := a.phaseAnalyzer.AnalyzeFromRawData(records, cleanLabels)
	if err != nil {
		return nil, fmt.Errorf("階段分析失敗: %w", err)
	}

	// 生成輸出檔名
	outputName := params.OutputPath
	if outputName == "" {
		dataFileName := filepath.Base(params.InputFile)
		dataBaseName := strings.TrimSuffix(dataFileName, ".csv")
		outputName = fmt.Sprintf("%s_階段分析.csv", dataBaseName)
	} else if !strings.HasSuffix(outputName, ".csv") {
		outputName += ".csv"
	}

	// 轉換為CSV格式
	outputData := a.csvHandler.ConvertPhaseAnalysisToCSV(records[0], &analysisResult.PhaseResults[0], analysisResult.MaxTimeIndex)

	// 保存結果
	err = a.csvHandler.WriteCSVToOutput(outputName, outputData)
	if err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 準備回傳結果
	outputPath := filepath.Join(a.config.OutputDir, outputName)

	// 轉換分析結果
	var results []PhaseAnalysis
	for i, phaseResult := range analysisResult.PhaseResults {
		if i >= len(cleanLabels) {
			break
		}

		// 將 map 轉換為 slice
		// 獲取通道數量（從 records 標題數量推算）
		channelCount := len(records[0]) - 1
		maxValues := make([]float64, channelCount)
		meanValues := make([]float64, channelCount)

		for colIdx, val := range phaseResult.MaxValues {
			if colIdx-1 < len(maxValues) {
				maxValues[colIdx-1] = val
			}
		}

		for colIdx, val := range phaseResult.MeanValues {
			if colIdx-1 < len(meanValues) {
				meanValues[colIdx-1] = val
			}
		}

		phaseAnalysis := PhaseAnalysis{
			PhaseLabel: phaseResult.PhaseName,
			StartTime:  0, // 這些資訊需要從其他地方獲取
			EndTime:    0,
			Duration:   0,
			Average:    meanValues,
			MaxValues:  maxValues,
			MinValues:  []float64{}, // PhaseAnalysisResult 不包含最小值
		}
		results = append(results, phaseAnalysis)
	}

	a.logger.Info("階段分析完成", map[string]interface{}{
		"output_file":   outputPath,
		"phase_count":   len(results),
		"channel_count": len(records[0]) - 1,
	})

	return &PhaseResult{
		OutputPath: outputPath,
		Headers:    records[0],
		Results:    results,
		Success:    true,
		Message:    fmt.Sprintf("階段分析成功完成，結果已保存到: %s", outputPath),
	}, nil
}

// ShowMessage displays an informational dialog
func (a *App) ShowMessage(title string, message string) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowError displays an error dialog
func (a *App) ShowError(title string, message string) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.ErrorDialog,
		Title:   title,
		Message: message,
	})
}

// CSVHeadersParams holds parameters for GetCSVHeaders.
type CSVHeadersParams struct {
	FilePath string `json:"filePath"`
}

// GetCSVHeaders returns the first row (headers) of a CSV file.
func (a *App) GetCSVHeaders(params CSVHeadersParams) ([]string, error) {
	// 讀取 CSV 檔案
	records, err := a.csvHandler.ReadCSV(params.FilePath)
	if err != nil {
		return nil, fmt.Errorf("讀取 CSV 標題失敗: %w", err)
	}
	// 確保有標題行
	if len(records) == 0 {
		return nil, fmt.Errorf("CSV 檔案沒有標題行")
	}
	return records[0], nil
}

// InteractiveChartParams holds parameters for generating interactive ECharts.
type InteractiveChartParams struct {
	FilePath string `json:"filePath"`
	Columns  []int  `json:"columns"`
	Title    string `json:"title"`
	Width    string `json:"width"`
	Height   string `json:"height"`
}

// Parameter structures

type MaxMeanParams struct {
	InputPath  string  `json:"inputPath"`
	WindowSize int     `json:"windowSize"`
	StartTime  float64 `json:"startTime"`
	EndTime    float64 `json:"endTime"`
	IsBatch    bool    `json:"isBatch"`
}

type MaxMeanResult struct {
	OutputPath string      `json:"outputPath"`
	Headers    []string    `json:"headers"`
	Results    [][]float64 `json:"results"`
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
}

type NormalizeParams struct {
	MainFile      string `json:"mainFile"`
	ReferenceFile string `json:"referenceFile"`
	OutputPath    string `json:"outputPath"`
}

type NormalizeResult struct {
	OutputPath string      `json:"outputPath"`
	Headers    []string    `json:"headers"`
	Data       [][]float64 `json:"data"`
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
}

type ChartParams struct {
	FilePath  string `json:"filePath"`
	Columns   []int  `json:"columns"`
	Title     string `json:"title"`
	ImageData string `json:"imageData"` // base64 PNG 數據
}

type ChartResult struct {
	OutputPath  string `json:"outputPath"`
	HTMLContent string `json:"htmlContent"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
}

type PhaseParams struct {
	InputFile   string   `json:"inputFile"`
	PhaseLabels []string `json:"phaseLabels"`
	OutputPath  string   `json:"outputPath"`
}

type PhaseResult struct {
	OutputPath string          `json:"outputPath"`
	Headers    []string        `json:"headers"`
	Results    []PhaseAnalysis `json:"results"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
}

type PhaseAnalysis struct {
	PhaseLabel string    `json:"phaseLabel"`
	StartTime  float64   `json:"startTime"`
	EndTime    float64   `json:"endTime"`
	Duration   float64   `json:"duration"`
	Average    []float64 `json:"average"`
	MaxValues  []float64 `json:"maxValues"`
	MinValues  []float64 `json:"minValues"`
}
