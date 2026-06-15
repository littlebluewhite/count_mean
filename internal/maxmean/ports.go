package maxmean

import (
	"errors"

	"count_mean/internal/models"
)

// FileSource 列舉一個批次要處理的檔案(各為一筆 EMGDataset)。
// 兩個 production adapter(configured input dir / external 絕對路徑)在 gui;
// in-memory adapter 在測試。
type FileSource interface {
	Discover() ([]BatchFile, error)
}

// BatchFile 是批次中的一個待處理檔:原本內聯在 gui 批次迴圈裡的 latent port
// (逐檔讀取來源的匯流形狀),於此 package 顯名為 BatchFile。
// Name 已去 .csv 副檔名,作為輸出檔名 stem 與 log 標籤;Read 延遲讀取 raw records。
type BatchFile struct {
	Name string
	Read func() ([][]string, error)
}

// ResultWriter 寫出單一檔的 max-mean 結果,回傳實際寫入路徑。
// production adapter 包 csvHandler.WriteMaxMean(SubDir 已綁定);spy adapter 在測試。
type ResultWriter interface {
	Write(name string, headers []string, results []models.MaxMeanResult, startRange, endRange float64) (path string, err error)
}

// ErrNoCSVFilesInFolder 代表目標資料夾中找不到任何 CSV 檔案。
var ErrNoCSVFilesInFolder = errors.New("資料夾中沒有找到CSV文件")
