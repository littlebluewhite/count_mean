package parsers

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// defaultEMGCSVPrecision 為 EMG CSV 預設小數位數，與分期同步分析統計輸出一致。
const defaultEMGCSVPrecision = 6

// ExportPhaseSyncDataToCSV 將 PhaseSyncEMGData 寫入 outputPath 為 CSV 檔。
//
// 輸出格式：
//   - UTF-8 BOM 開頭（Excel 相容）
//   - 第一列：`Time` + 各肌肉名稱（依 data.Headers 順序）
//   - 後續每列：時間點 + 各肌肉於該時間點的取樣值
//
// precision 控制浮點數欄位的小數位數；若 <= 0 則使用預設值 6。
//
// 此函式不負責路徑驗證 — 呼叫端必須先確認 outputPath 在允許範圍內。
//
// Flush 與 Close 的錯誤都顯式透過 named return 回傳：csv.Writer 把寫入錯誤延後到
// Flush 才丟，磁碟滿/網路磁碟掉線等情境若靠裸 defer 忽略，caller 拿到 nil 但檔案
// 實際內容不完整。對齊 cci/analyzer.go:writeCSVFile 與 emg_statistics.go:ExportToCSV。
func ExportPhaseSyncDataToCSV(
	data *models.PhaseSyncEMGData,
	outputPath string,
	precision int,
) (err error) {
	if data == nil {
		return fmt.Errorf("EMG 數據為空: %w", ErrNilData)
	}

	if precision <= 0 {
		precision = defaultEMGCSVPrecision
	}

	// fsperm.WriteFlags 含 O_NOFOLLOW (unix) 拒絕 symlink；FilePerm=0o600 限定 owner 讀寫。
	//nolint:gosec // outputPath validated by caller
	file, err := os.OpenFile(outputPath, fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("無法創建輸出檔案 %s: %w", outputPath, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	if err := csvutil.WriteBOM(file); err != nil {
		return fmt.Errorf("無法寫入 BOM: %w", err)
	}

	writer := csv.NewWriter(file)

	if err := writer.Write(csvutil.SanitizeHeaderRow(buildEMGCSVHeader(data.Headers))); err != nil {
		return fmt.Errorf("寫入標頭失敗: %w", err)
	}

	floatFormat := fmt.Sprintf("%%.%df", precision)

	for i := range data.Time {
		row := make([]string, 0, len(data.Headers)+1)
		row = append(row, strconv.FormatFloat(data.Time[i], 'f', precision, 64))

		for _, name := range data.Headers {
			channel := data.Channels[name]

			if i >= len(channel) {
				row = append(row, "")
				continue
			}

			row = append(row, fmt.Sprintf(floatFormat, channel[i]))
		}

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入第 %d 列失敗: %w", i+1, err)
		}
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV writer 錯誤: %w", err)
	}

	return nil
}

// buildEMGCSVHeader 組合 CSV 標頭：`Time` + 各肌肉名稱。
func buildEMGCSVHeader(channelHeaders []string) []string {
	header := make([]string, 0, len(channelHeaders)+1)
	header = append(header, "Time")
	header = append(header, channelHeaders...)

	return header
}
