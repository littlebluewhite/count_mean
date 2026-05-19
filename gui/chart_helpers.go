package gui

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/fsperm"
)

// Chart generation errors.
var (
	ErrInvalidImageFormat = errors.New("無效的圖片數據格式")
	ErrNoDataFile         = errors.New("請選擇資料檔案")
	ErrNoColumns          = errors.New("請選擇至少一個欄位")
	ErrInvalidCSVFormat   = errors.New("CSV 檔案格式無效：需要至少包含標題和一行數據")
)

// buildDatasetFromRecords constructs an EMGDataset from CSV records.
// Records must have at least 2 rows (header + data). First column is time.
func buildDatasetFromRecords(records [][]string) *models.EMGDataset {
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}
	copy(dataset.Headers, records[0])

	for i := 1; i < len(records); i++ {
		timeVal, channels, ok := parsers.ParseTimeAndChannels(records[i], 1)
		if !ok {
			continue
		}

		dataset.Data = append(dataset.Data, models.EMGData{Time: timeVal, Channels: channels})
	}

	return dataset
}

// GenerateInteractiveChart returns HTML content of an interactive chart.
//
// 走 readCSVWithPathValidation 路由（同 GetCSVHeaders），避免使用者透過 file
// dialog 餵入任意絕對路徑後 bypass 驗證。
//
// 加 defer recoverHandlerPanic + named returns + nil params guard。上輪
// H4-H5 修了 9 個 RPC,chart 家族 (GenerateInteractiveChart / savePNGFromBase64 /
// GenerateChart) 被漏 — 三個 method 都裸 deref params.FilePath / ImageData,
// 任何 panic(nil params 或下游 nil deref)會擊潰整個 Wails desktop process。
func (a *App) GenerateInteractiveChart(params *InteractiveChartParams) (htmlOut string, err error) {
	defer recoverHandlerPanic("GenerateInteractiveChart", a.logger, &err)

	if params == nil {
		return "", ErrNoDataFile
	}

	s := a.state.Load()

	records, readErr := a.readCSVWithPathValidation(s, params.FilePath, s.config.InputDir)
	if readErr != nil {
		return "", fmt.Errorf("讀取 CSV 檔案失敗: %w", readErr)
	}

	if len(records) < 2 {
		return "", ErrInvalidCSVFormat
	}

	dataset := buildDatasetFromRecords(records)

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

	if renderErr := a.chartGen.RenderChartToWriter(dataset, chartConfig, &buf); renderErr != nil {
		return "", fmt.Errorf("生成互動式圖表失敗: %w", renderErr)
	}

	return buf.String(), nil
}

// savePNGFromBase64 從 base64 數據保存 PNG 檔案.
//
// 三層 PNG 守門(size cap / magic signature / IHDR dimension)
// 集中在 DecodeAndValidatePNG;此 helper 只負責「驗證 → 路徑 → 寫檔」flow。
//
// 加 defer recoverHandlerPanic + named returns。雖然 savePNGFromBase64
// 不直接由 Wails binding expose,但 GenerateChart 內部呼叫;為了讓 defer
// 安全網真的覆蓋整個 chart family,此 helper 也加 defer(double-defer 在
// GenerateChart panic recovery 已生效時不會 re-fire — recover 只回應一次)。
func (a *App) savePNGFromBase64(params ChartParams) (result *ChartResult, err error) {
	defer recoverHandlerPanic("savePNGFromBase64", a.logger, &err)

	dataURL := params.ImageData
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")

	pngData, decodeErr := DecodeAndValidatePNG(base64Data)
	if decodeErr != nil {
		return nil, fmt.Errorf("PNG 驗證失敗: %w", decodeErr)
	}

	fileName := filepath.Base(params.FilePath)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	// params.Title 來自前端，需先 sanitize 避免路徑穿越（"../x" 之類）。
	// 與 cci_handlers.go:114 的 params.Subject 處理對稱（cross-compare review 補 Wave 2 缺漏）。
	safeTitle := calculator.SanitizeFileName(params.Title)
	s := a.state.Load()
	outputPath := filepath.Join(s.config.OutputDir, fmt.Sprintf("%s_%s.png", baseName, safeTitle))

	// boundary 路徑驗證 — 對齊 cci_handlers.go:158 DownloadCCIChart 模式。
	//
	// 為何需要:OutputDir 雖來自 config 不直接由前端控,但仍可能被惡意修改
	// (編輯 config.json / 舊版 bug 讓 traversal 寫進去);params.Title 與
	// params.FilePath 都來自前端,SanitizeFileName 對 Title 做基本處理仍不能
	// 保證最終 outputPath 不落到系統敏感目錄。fsperm.WriteFileNoFollow 雖會擋
	// symlink follow,但 boundary 提早 reject 對 audit log 與錯誤訊息都更友善。
	//
	// 為何驗合成後的 outputPath(而非 outputDir / fileName 分開):外部攻擊面是
	// 「OutputDir + baseName + safeTitle」的合成結果,單獨驗 OutputDir 不能完全
	// 覆蓋(SanitizeFileName 雖剝掉 path separator 仍可能拼出意外路徑),驗
	// 合成後路徑是 strongest invariant — 對齊 DownloadCCIChart。
	//
	// 同步:對齊 cci_handlers.go DownloadCCIChart 的 label 修法 —
	// 把 "PNG 輸出路徑" 一次帶到 validateExternalPathInputs 而非由 caller
	// 再 wrap 一層。原本「PNG 輸出 輸出路徑 路徑驗證失敗: ...」label 重複。
	if pathErr := validateExternalPathInputs("PNG 輸出路徑", outputPath); pathErr != nil {
		return nil, pathErr
	}

	if writeErr := fsperm.WriteFileNoFollow(outputPath, pngData); writeErr != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", writeErr)
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

// GenerateChart 依照目前預覽設定輸出 PNG 到 output_dir.
// 僅支援透過 params.ImageData 帶入前端 canvas 截圖的路徑；舊版的靜態 PNG
// 生成路徑（chart.Generator）已移除，因為它建出 chartConfig 後從未實際寫檔，
// 卻回傳 Success — 對前端是 silent bug。前端缺 ImageData 應改用互動圖表。
//
// 加 defer recoverHandlerPanic + named returns。同 GenerateInteractiveChart
// 補完 H4-H5 漏修的 chart 家族 panic safety net。
func (a *App) GenerateChart(params ChartParams) (result *ChartResult, err error) {
	defer recoverHandlerPanic("GenerateChart", a.logger, &err)

	if params.ImageData == "" {
		return nil, ErrInvalidImageFormat
	}

	return a.savePNGFromBase64(params)
}
