// Package chart provides chart generation functionality for EMG data visualization.
// It supports both static chart generation using gonum/plot and interactive charts using go-echarts.
package chart

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"count_mean/internal/logging"
	"count_mean/internal/models"
)

// Chart generation constants.
const (
	lineWidth           = 2     // 線條寬度
	chartFilePermission = 0o600 // 文件權限
	chartDirPermission  = 0o750 // 目錄權限
)

// Color constants for chart lines.
const (
	colorFull   = 255 // 完整色彩值
	colorOrange = 165 // 橙色RGB值
	colorPurple = 128 // 紫色RGB值
	colorPink   = 192 // 粉色綠色分量
	colorPinkB  = 203 // 粉色藍色分量
	colorBrown  = 42  // 棕色RGB值
)

// Generator 處理圖表生成.
type Generator struct {
	logger *logging.Logger
}

// NewChartGenerator 創建新的圖表生成器.
func NewChartGenerator() *Generator {
	return &Generator{
		logger: logging.GetLogger("chart_generator"),
	}
}

// Config 圖表配置.
type Config struct {
	Title      string
	XAxisLabel string
	YAxisLabel string
	Width      vg.Length
	Height     vg.Length
	Columns    []string // 要繪製的column名稱
}

// getDefaultColors 返回預設的圖表顏色.
func getDefaultColors() []color.RGBA {
	return []color.RGBA{
		{R: colorFull, G: 0, B: 0, A: colorFull},                       // 紅色
		{R: 0, G: colorFull, B: 0, A: colorFull},                       // 綠色
		{R: 0, G: 0, B: colorFull, A: colorFull},                       // 藍色
		{R: colorFull, G: colorOrange, B: 0, A: colorFull},             // 橙色
		{R: colorPurple, G: 0, B: colorPurple, A: colorFull},           // 紫色
		{R: colorFull, G: colorPink, B: colorPinkB, A: colorFull},      // 粉色
		{R: colorOrange, G: colorBrown, B: colorBrown, A: colorFull},   // 棕色
		{R: colorPurple, G: colorPurple, B: colorPurple, A: colorFull}, // 灰色
	}
}

// addColumnLinesToPlot 為圖表添加欄位線條（共用邏輯）.
func (c *Generator) addColumnLinesToPlot(
	p *plot.Plot,
	dataset *models.EMGDataset,
	config *Config,
	columnIndices map[string]int,
) {
	colors := getDefaultColors()

	for i, columnName := range config.Columns {
		columnIndex, exists := columnIndices[columnName]
		if !exists {
			c.logger.Warn("找不到指定的column", map[string]interface{}{
				"column_name": columnName,
			})

			continue
		}

		// 跳過時間列（通常是第一列）
		if columnIndex == 0 {
			continue
		}

		// 準備數據點
		pts := make(plotter.XYs, len(dataset.Data))
		for j, data := range dataset.Data {
			pts[j].X = data.Time
			if columnIndex-1 < len(data.Channels) {
				pts[j].Y = data.Channels[columnIndex-1]
			}
		}

		// 創建線條
		line, err := plotter.NewLine(pts)
		if err != nil {
			c.logger.Error("創建線條失敗", err, map[string]interface{}{
				"column_name": columnName,
			})

			continue
		}

		// 設置線條顏色和樣式
		line.Color = colors[i%len(colors)]
		line.Width = vg.Points(lineWidth)

		// 添加到圖表
		p.Add(line)
		p.Legend.Add(columnName, line)
	}
}

// buildColumnIndices 建立欄位名稱到索引的映射.
func buildColumnIndices(dataset *models.EMGDataset) map[string]int {
	columnIndices := make(map[string]int)
	for i, header := range dataset.Headers {
		columnIndices[header] = i
	}

	return columnIndices
}

// GenerateLineChart 生成折線圖.
func (c *Generator) GenerateLineChart(dataset *models.EMGDataset, config *Config, outputPath string) error {
	c.logger.Info("開始生成折線圖", map[string]interface{}{
		"title":       config.Title,
		"columns":     config.Columns,
		"output_path": outputPath,
		"data_points": len(dataset.Data),
	})

	// 創建新圖表
	p := plot.New()
	p.Title.Text = config.Title
	p.X.Label.Text = config.XAxisLabel
	p.Y.Label.Text = config.YAxisLabel

	// 找到選中的column索引
	columnIndices := buildColumnIndices(dataset)

	// 為每個選中的column創建線條
	c.addColumnLinesToPlot(p, dataset, config, columnIndices)

	// 設置圖例位置
	p.Legend.Top = true
	p.Legend.Left = true

	// 保存圖片
	img := vgimg.New(config.Width, config.Height)
	dc := draw.New(img)
	p.Draw(dc)

	// 確保輸出目錄存在
	outputDir := filepath.Dir(outputPath)
	if err := ensureDir(outputDir); err != nil {
		return fmt.Errorf("創建輸出目錄失敗: %w", err)
	}

	// 保存為PNG
	// G304: outputPath comes from trusted application code
	pngFile, err := os.OpenFile(filepath.Clean(outputPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, chartFilePermission)
	if err != nil {
		c.logger.Error("創建PNG文件失敗", err)
		return fmt.Errorf("創建PNG文件失敗: %w", err)
	}

	defer func() {
		if closeErr := pngFile.Close(); closeErr != nil {
			c.logger.Warn("關閉PNG文件時發生錯誤", map[string]interface{}{"error": closeErr.Error()})
		}
	}()

	// 直接保存PNG
	_, err = vgimg.PngCanvas{Canvas: img}.WriteTo(pngFile)
	if err != nil {
		c.logger.Error("保存圖片失敗", err)
		return fmt.Errorf("保存圖片失敗: %w", err)
	}

	c.logger.Info("折線圖生成完成", map[string]interface{}{
		"output_path": outputPath,
	})

	return nil
}

// GenerateLineChartImage 生成折線圖並返回圖像.
func (c *Generator) GenerateLineChartImage(dataset *models.EMGDataset, config *Config) (image.Image, error) {
	c.logger.Info("開始生成折線圖圖像", map[string]interface{}{
		"title":       config.Title,
		"columns":     config.Columns,
		"data_points": len(dataset.Data),
	})

	// 創建新圖表
	p := plot.New()
	p.Title.Text = config.Title
	p.X.Label.Text = config.XAxisLabel
	p.Y.Label.Text = config.YAxisLabel

	// 找到選中的column索引
	columnIndices := buildColumnIndices(dataset)

	// 為每個選中的column創建線條
	c.addColumnLinesToPlot(p, dataset, config, columnIndices)

	// 設置圖例位置
	p.Legend.Top = true
	p.Legend.Left = true

	// 生成圖像
	img := vgimg.New(config.Width, config.Height)
	dc := draw.New(img)
	p.Draw(dc)

	// 返回圖像
	return img.Image(), nil
}

// GetCSVColumns 獲取CSV文件的所有column.
func GetCSVColumns(dataset *models.EMGDataset) []string {
	if dataset == nil || len(dataset.Headers) == 0 {
		return []string{}
	}

	// 返回除了第一列（時間）之外的所有列
	columns := make([]string, 0, len(dataset.Headers)-1)
	for i := 1; i < len(dataset.Headers); i++ {
		columns = append(columns, dataset.Headers[i])
	}

	sort.Strings(columns)

	return columns
}

// ensureDir 確保目錄存在.
func ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, chartDirPermission); err != nil {
			return fmt.Errorf("創建目錄失敗 %s: %w", dir, err)
		}
	}

	return nil
}
