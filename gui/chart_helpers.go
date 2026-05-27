package gui

import "errors"

// ErrInvalidImageFormat 是 PNG base64 dataURL 格式錯誤時回給前端的 sentinel error。
// 沿用自舊「資料做圖」path(已隨 Chart Composer Slice E 移除),
// 仍由 DownloadCCIChart 與 DownloadChartComposerImage 共用。
//
//nolint:gochecknoglobals // sentinel error shared across chart download handlers
var ErrInvalidImageFormat = errors.New("無效的圖片數據格式")
