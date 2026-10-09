package gui

import (
	"errors"

	"count_mean/internal/maxmean"
)

// Sentinel errors for validation.
var (
	ErrNoMainFile         = errors.New("請選擇主要資料檔案")
	ErrNoReferenceFile    = errors.New("請選擇參考資料檔案")
	ErrNoInputFile        = errors.New("請選擇資料檔案")
	ErrNoPhaseLabels      = errors.New("請輸入階段標籤")
	ErrNoValidPhaseLabels = errors.New("請輸入有效的階段標籤")
	ErrNoManifestFile     = errors.New("請選擇分期總檔案")
	ErrNoDataFolder       = errors.New("請選擇數據資料夾")
	ErrNoPhaseSelection   = errors.New("請選擇開始和結束分期點")
	ErrLocaleEmpty        = errors.New("locale 不可為空字串")
	ErrLocaleUnsupported  = errors.New("不支援的 locale")
	ErrInvalidPhasePoint  = errors.New("無效的分期點代碼")
	ErrInvalidPhaseRange  = errors.New("分期時間區間不合法（起始須小於結束且為有限值）")
	ErrNoCSVFilesInFolder = maxmean.ErrNoCSVFilesInFolder
)
