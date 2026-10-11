package maxmean

import (
	"context"

	"count_mean/internal/calculator"
	"count_mean/internal/logging"
)

// BatchParams 是批次計算的輸入參數(時間窗口與時間範圍)。
// Range 套用到每一個檔案;兩端各自可省略(見 calculator.MaxMeanRange)。
type BatchParams struct {
	WindowSize int
	Range      calculator.MaxMeanRange
}

// BatchResult 是 compute 端累積的成敗計數;GUI 負責 presentation
// (中文 Message / OutputPath)。各檔結果已由 ResultWriter 寫出,不再累積。
type BatchResult struct {
	SuccessCount int
	FailCount    int
}

// RunBatch 對 source 列舉的每個 BatchFile 逐檔執行 max-mean 計算並寫出,
// 累積 partial-success 結果。比照 cci/phase_sync 各自取 logger 的 sibling 慣例,
// 從 logging.GetLogger("maxmean") 取 logger。
func RunBatch(
	ctx context.Context,
	calc *calculator.MaxMeanCalculator,
	source FileSource,
	writer ResultWriter,
	params BatchParams,
) (*BatchResult, error) {
	logger := logging.GetLogger("maxmean")

	files, err := source.Discover()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, ErrNoCSVFilesInFolder
	}

	successCount, failCount := 0, 0

	for _, f := range files {
		records, err := f.Read()
		if err != nil {
			failCount++
			logger.Error("讀取檔案失敗", err, map[string]any{"file": f.Name})

			continue
		}

		run, err := calc.CalculateFromRawDataInRange(ctx, records, params.WindowSize, params.Range)
		if err != nil {
			failCount++
			logger.Error("處理檔案失敗", err, map[string]any{"file": f.Name})

			continue
		}

		if _, err := writer.Write(f.Name, records[0], run.Results, run.Start, run.End); err != nil {
			failCount++
			logger.Error("處理檔案失敗", err, map[string]any{"file": f.Name})

			continue
		}

		successCount++

		logger.Info("檔案處理成功", map[string]any{
			"file":          f.Name,
			"results_count": len(run.Results),
		})
	}

	return &BatchResult{
		SuccessCount: successCount,
		FailCount:    failCount,
	}, nil
}
