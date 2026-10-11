package gui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// pngDataURLPrefix 是前端 canvas.toDataURL() 產出的 PNG base64 dataURL 前綴。
// 與 DownloadCCIChart / DownloadChartComposerImage 內聯字面值保持一致。
const pngDataURLPrefix = "data:image/png;base64,"

// downloadValidatedPNG 是 CCI / Chart Composer 兩個下載 handler 共用的 PNG 管線(ADR-0009)。
// gui 只持有 dataURL prefix 檢查與 DecodeAndValidatePNG;落檔(SubjectOutputName 命名、
// containment、敏感位置、MkdirAll、atomic 寫入)交給 CSVHandler.WriteSubjectPNG
// ([[Output placement]],ADR-0055)。檔名 = OutputDir/{SubjectOutputName(subject, suffix)}.png。
func (a *App) downloadValidatedPNG(imageData, subject, suffix string) (*ChartResult, error) {
	if !strings.HasPrefix(imageData, pngDataURLPrefix) {
		return nil, ErrInvalidImageFormat
	}

	pngData, err := DecodeAndValidatePNG(strings.TrimPrefix(imageData, pngDataURLPrefix))
	if err != nil {
		return nil, fmt.Errorf("PNG 驗證失敗: %w", err)
	}

	outputPath, err := a.state.Load().csvHandler.WriteSubjectPNG(subject, suffix, pngData)
	if err != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", err)
	}

	// Message 由 Composer 前端原樣顯示:只帶檔名,輸出目錄的絕對路徑不進 webview 文字。
	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    "圖表已下載至: " + filepath.Base(outputPath),
	}, nil
}
