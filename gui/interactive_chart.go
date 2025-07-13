package gui

import (
	"count_mean/internal/chart"
	"count_mean/internal/models"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"image"
)

// InteractiveChart 可交互的圖表小工具
type InteractiveChart struct {
	widget.BaseWidget

	// 圖表數據
	chartImage  *canvas.Image
	dataset     *models.EMGDataset
	chartConfig chart.ChartConfig
	chartGen    *chart.ChartGenerator // 添加圖表生成器

	// 縮放和精度控制
	zoomFactor float64
	xOffset    float64
	xRange     float64

	// 顯示的數據範圍
	displayStartX float64
	displayEndX   float64

	// 已移除懸停功能

	// 點擊處理
	onDoubleClick func(x, y float64)
}

// NewInteractiveChart 創建新的交互式圖表
func NewInteractiveChart(img image.Image, dataset *models.EMGDataset, config chart.ChartConfig) *InteractiveChart {
	chart := &InteractiveChart{
		chartImage:  canvas.NewImageFromImage(img),
		dataset:     dataset,
		chartConfig: config,
		chartGen:    chart.NewChartGenerator(),
		zoomFactor:  1.0,
		xOffset:     0.0,
	}

	// 計算初始X軸範圍
	if len(dataset.Data) > 0 {
		chart.displayStartX = dataset.Data[0].Time
		chart.displayEndX = dataset.Data[len(dataset.Data)-1].Time
		chart.xRange = chart.displayEndX - chart.displayStartX
	}

	chart.chartImage.FillMode = canvas.ImageFillOriginal

	chart.ExtendBaseWidget(chart)
	return chart
}

// NewInteractiveChartWithGenerator 創建新的交互式圖表（帶圖表生成器）
func NewInteractiveChartWithGenerator(img image.Image, dataset *models.EMGDataset, config chart.ChartConfig, chartGen *chart.ChartGenerator) *InteractiveChart {
	chart := &InteractiveChart{
		chartImage:  canvas.NewImageFromImage(img),
		dataset:     dataset,
		chartConfig: config,
		chartGen:    chartGen,
		zoomFactor:  1.0,
		xOffset:     0.0,
	}

	// 計算初始X軸範圍
	if len(dataset.Data) > 0 {
		chart.displayStartX = dataset.Data[0].Time
		chart.displayEndX = dataset.Data[len(dataset.Data)-1].Time
		chart.xRange = chart.displayEndX - chart.displayStartX
	}

	chart.chartImage.FillMode = canvas.ImageFillOriginal

	chart.ExtendBaseWidget(chart)
	return chart
}

// CreateRenderer 創建渲染器
func (c *InteractiveChart) CreateRenderer() fyne.WidgetRenderer {
	return &interactiveChartRenderer{
		chart:   c,
		objects: []fyne.CanvasObject{c.chartImage},
	}
}

// MouseIn 處理鼠標進入事件
func (c *InteractiveChart) MouseIn(event *desktop.MouseEvent) {
	// 已移除hover功能
}

// MouseOut 處理鼠標離開事件
func (c *InteractiveChart) MouseOut() {
	// 已移除hover功能
}

// MouseMoved 處理鼠標移動事件
func (c *InteractiveChart) MouseMoved(event *desktop.MouseEvent) {
	// 已移除hover功能
}

// MouseDown 處理鼠標按下事件
func (c *InteractiveChart) MouseDown(event *desktop.MouseEvent) {
	// 可以處理點擊事件（如果需要）
}

// MouseUp 處理鼠標釋放事件
func (c *InteractiveChart) MouseUp(event *desktop.MouseEvent) {
	// 可以在這裡處理單擊事件
}

// DoubleTapped 處理雙擊事件（用於縮放）
func (c *InteractiveChart) DoubleTapped(event *fyne.PointEvent) {
	c.zoomToPoint(event.Position.X, event.Position.Y)
}

// Scrolled 處理滾輪事件（用於縮放）
func (c *InteractiveChart) Scrolled(event *fyne.ScrollEvent) {
	// 根據滾輪方向調整縮放，使用圖表中心作為縮放點
	centerX := c.chartImage.Size().Width / 2
	if event.Scrolled.DY > 0 {
		c.zoomIn(centerX)
	} else if event.Scrolled.DY < 0 {
		c.zoomOut(centerX)
	}
}

// zoomToPoint 縮放到指定點
func (c *InteractiveChart) zoomToPoint(mouseX, mouseY float32) {
	if c.onDoubleClick != nil {
		chartSize := c.chartImage.Size()
		if chartSize.Width == 0 {
			return
		}

		// 計算點擊的時間位置
		leftMargin := chartSize.Width * 0.1
		plotWidth := chartSize.Width * 0.8
		relativeX := (mouseX - leftMargin) / plotWidth
		dataX := c.displayStartX + float64(relativeX)*(c.displayEndX-c.displayStartX)

		c.onDoubleClick(float64(dataX), float64(mouseY))
	}

	// 執行縮放
	c.zoomIn(mouseX)
}

// ZoomIn 放大 (公開方法)
func (c *InteractiveChart) ZoomIn(centerX float32) {
	c.zoomIn(centerX)
}

// ZoomOut 縮小 (公開方法)
func (c *InteractiveChart) ZoomOut(centerX float32) {
	c.zoomOut(centerX)
}

// zoomIn 放大
func (c *InteractiveChart) zoomIn(centerX float32) {
	if c.zoomFactor >= 10.0 {
		return // 限制最大縮放倍數
	}

	c.zoomFactor *= 2.0
	c.updateZoom(centerX)
}

// zoomOut 縮小
func (c *InteractiveChart) zoomOut(centerX float32) {
	if c.zoomFactor <= 0.1 {
		return // 限制最小縮放倍數
	}

	c.zoomFactor /= 2.0
	c.updateZoom(centerX)
}

// updateZoom 更新縮放
func (c *InteractiveChart) updateZoom(centerX float32) {
	if len(c.dataset.Data) == 0 {
		return
	}

	chartSize := c.chartImage.Size()
	if chartSize.Width == 0 {
		return
	}

	// 計算縮放中心對應的數據時間
	leftMargin := chartSize.Width * 0.1
	plotWidth := chartSize.Width * 0.8
	relativeX := (centerX - leftMargin) / plotWidth
	centerTime := c.displayStartX + float64(relativeX)*(c.displayEndX-c.displayStartX)

	// 計算新的顯示範圍
	totalRange := c.dataset.Data[len(c.dataset.Data)-1].Time - c.dataset.Data[0].Time
	newRange := totalRange / c.zoomFactor

	// 確保新範圍不會超出數據邊界
	newStart := centerTime - newRange*float64(relativeX)
	newEnd := newStart + newRange

	if newStart < c.dataset.Data[0].Time {
		newStart = c.dataset.Data[0].Time
		newEnd = newStart + newRange
	}
	if newEnd > c.dataset.Data[len(c.dataset.Data)-1].Time {
		newEnd = c.dataset.Data[len(c.dataset.Data)-1].Time
		newStart = newEnd - newRange
	}

	c.displayStartX = newStart
	c.displayEndX = newEnd

	// 重新生成圖表（這裡需要重新繪製縮放後的圖表）
	c.regenerateChart()
}

// regenerateChart 重新生成圖表
func (c *InteractiveChart) regenerateChart() {
	if c.chartGen == nil {
		// 圖表生成器為空，無法重新生成
		c.Refresh()
		return
	}
	if c.dataset == nil {
		// 數據集為空，無法重新生成
		c.Refresh()
		return
	}

	// 創建過濾後的數據集，只包含當前顯示範圍的數據
	filteredDataset := &models.EMGDataset{
		Headers: make([]string, len(c.dataset.Headers)),
		Data:    make([]models.EMGData, 0),
	}

	// 複製headers
	copy(filteredDataset.Headers, c.dataset.Headers)

	// 過濾數據到當前顯示範圍
	for _, dataPoint := range c.dataset.Data {
		if dataPoint.Time >= c.displayStartX && dataPoint.Time <= c.displayEndX {
			filteredDataset.Data = append(filteredDataset.Data, dataPoint)
		}
	}

	// 如果沒有數據在範圍內，使用完整數據集
	if len(filteredDataset.Data) == 0 {
		// 復制完整數據集
		filteredDataset.Data = make([]models.EMGData, len(c.dataset.Data))
		copy(filteredDataset.Data, c.dataset.Data)
	}

	// 重新生成圖表圖像
	img, err := c.chartGen.GenerateLineChartImage(filteredDataset, c.chartConfig)
	if err != nil {
		// 如果生成失敗，保持原圖表
		c.Refresh()
		return
	}

	if img == nil {
		// 生成的圖像為空，保持原圖表
		c.Refresh()
		return
	}

	// 更新圖表圖像
	c.chartImage.Resource = nil
	c.chartImage.Image = img
	c.chartImage.Refresh()

	// 確保widget重新渲染
	c.Refresh()
}

// SetOnDoubleClick 設置雙擊事件處理函數
func (c *InteractiveChart) SetOnDoubleClick(callback func(x, y float64)) {
	c.onDoubleClick = callback
}

// ResetZoom 重置縮放
func (c *InteractiveChart) ResetZoom() {
	if len(c.dataset.Data) > 0 {
		c.displayStartX = c.dataset.Data[0].Time
		c.displayEndX = c.dataset.Data[len(c.dataset.Data)-1].Time
		c.zoomFactor = 1.0
		c.xOffset = 0.0
		c.regenerateChart()
	}
}

// interactiveChartRenderer 交互式圖表渲染器
type interactiveChartRenderer struct {
	chart   *InteractiveChart
	objects []fyne.CanvasObject
}

func (r *interactiveChartRenderer) Layout(size fyne.Size) {
	r.chart.chartImage.Resize(size)
	r.chart.chartImage.Move(fyne.NewPos(0, 0))
}

func (r *interactiveChartRenderer) MinSize() fyne.Size {
	return fyne.NewSize(800, 600)
}

func (r *interactiveChartRenderer) Refresh() {
	for _, obj := range r.objects {
		obj.Refresh()
	}
}

func (r *interactiveChartRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *interactiveChartRenderer) Destroy() {
	// 清理資源
}
