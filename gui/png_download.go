package gui

import (
	"fmt"
	"strings"

	"count_mean/internal/security/fsperm"
)

// pngDataURLPrefix 是前端 canvas.toDataURL() 產出的 PNG base64 dataURL 前綴。
// 與 DownloadCCIChart / DownloadChartComposerImage 內聯字面值保持一致。
const pngDataURLPrefix = "data:image/png;base64,"

// downloadValidatedPNG 是 CCI / Chart Composer 兩個下載 handler 共用的 PNG
// 安全管線單一抽出點（ADR-0009 Phase 1）。
func (a *App) downloadValidatedPNG(imageData, outputPath string) (*ChartResult, error) {
	if !strings.HasPrefix(imageData, pngDataURLPrefix) {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(imageData, pngDataURLPrefix)

	pngData, err := DecodeAndValidatePNG(base64Data)
	if err != nil {
		return nil, fmt.Errorf("PNG 驗證失敗: %w", err)
	}

	// label 一次到位帶 "PNG 輸出路徑"，validateExternalPathInputs 已會附上
	// 「路徑驗證失敗: <reason>」。caller 不再外層 re-wrap，避免重複 label。
	if pathErr := validateExternalPathInputs("PNG 輸出路徑", outputPath); pathErr != nil {
		return nil, pathErr
	}

	if writeErr := fsperm.WriteFileNoFollow(outputPath, pngData); writeErr != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", writeErr)
	}

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已下載至: %s", outputPath),
	}, nil
}
