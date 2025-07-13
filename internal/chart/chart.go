package chart

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
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
)

// ChartGenerator 處理圖表生成
type ChartGenerator struct {
	logger *logging.Logger
}

// NewChartGenerator 創建新的圖表生成器
func NewChartGenerator() *ChartGenerator {
	return &ChartGenerator{
		logger: logging.GetLogger("chart_generator"),
	}
}

// ChartConfig 圖表配置
type ChartConfig struct {
	Title      string
	XAxisLabel string
	YAxisLabel string
	Width      vg.Length
	Height     vg.Length
	Columns    []string // 要繪製的column名稱
}

// GenerateLineChart 生成折線圖
func (c *ChartGenerator) GenerateLineChart(dataset *models.EMGDataset, config ChartConfig, outputPath string) error {
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
	columnIndices := make(map[string]int)
	for i, header := range dataset.Headers {
		columnIndices[header] = i
	}

	// 為每個選中的column創建線條
	colors := []color.RGBA{
		{R: 255, G: 0, B: 0, A: 255},     // 紅色
		{R: 0, G: 255, B: 0, A: 255},     // 綠色
		{R: 0, G: 0, B: 255, A: 255},     // 藍色
		{R: 255, G: 165, B: 0, A: 255},   // 橙色
		{R: 128, G: 0, B: 128, A: 255},   // 紫色
		{R: 255, G: 192, B: 203, A: 255}, // 粉色
		{R: 165, G: 42, B: 42, A: 255},   // 棕色
		{R: 128, G: 128, B: 128, A: 255}, // 灰色
	}

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
		line.Width = vg.Points(2)

		// 添加到圖表
		p.Add(line)
		p.Legend.Add(columnName, line)
	}

	// 設置圖例位置
	p.Legend.Top = true
	p.Legend.Left = true

	// 保存圖片
	img := vgimg.New(config.Width, config.Height)
	dc := draw.New(img)
	p.Draw(dc)

	// 確保輸出目錄存在
	outputDir := filepath.Dir(outputPath)
	if err := c.ensureDir(outputDir); err != nil {
		return fmt.Errorf("創建輸出目錄失敗: %w", err)
	}

	// 保存為PNG
	pngFile, err := os.Create(outputPath)
	if err != nil {
		c.logger.Error("創建PNG文件失敗", err)
		return fmt.Errorf("創建PNG文件失敗: %w", err)
	}
	defer pngFile.Close()

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

// GenerateLineChartImage 生成折線圖並返回圖像
func (c *ChartGenerator) GenerateLineChartImage(dataset *models.EMGDataset, config ChartConfig) (image.Image, error) {
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
	columnIndices := make(map[string]int)
	for i, header := range dataset.Headers {
		columnIndices[header] = i
	}

	// 為每個選中的column創建線條
	colors := []color.RGBA{
		{R: 255, G: 0, B: 0, A: 255},     // 紅色
		{R: 0, G: 255, B: 0, A: 255},     // 綠色
		{R: 0, G: 0, B: 255, A: 255},     // 藍色
		{R: 255, G: 165, B: 0, A: 255},   // 橙色
		{R: 128, G: 0, B: 128, A: 255},   // 紫色
		{R: 255, G: 192, B: 203, A: 255}, // 粉色
		{R: 165, G: 42, B: 42, A: 255},   // 棕色
		{R: 128, G: 128, B: 128, A: 255}, // 灰色
	}

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
		line.Width = vg.Points(2)

		// 添加到圖表
		p.Add(line)
		p.Legend.Add(columnName, line)
	}

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

// GetCSVColumns 獲取CSV文件的所有column
func (c *ChartGenerator) GetCSVColumns(dataset *models.EMGDataset) []string {
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

// ensureDir 確保目錄存在
func (c *ChartGenerator) ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}
