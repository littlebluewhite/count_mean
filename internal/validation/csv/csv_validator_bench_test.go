package csv

import (
	"fmt"
	"testing"
)

// BenchmarkValidateCSVData_TypicalEMG 量測 cell-level CSV 守門對「典型 EMG 資料」
// 的吞吐成本。EMG CSV 通常:
//   - 1 header row + N body rows
//   - 每 row ~16-32 channel column (Time + multi-channel)
//   - 每 cell 是浮點數字串(< 16 char),不會觸發 SQL/Command/formula injection
//
// 這個 benchmark 的目的:量測讀取側 sanity check(結構 / cell 長度 / UTF-8)
// 在 typical hot path 的 overhead。
//
// 用 b.SetBytes 讓 -benchmem 輸出包含 throughput (MB/s),方便對「validator on vs off」
// 兩種 baseline 直接比較。
//
// 跑法:make bench-csv-validator,或直接
//
//	go test -bench=BenchmarkValidateCSVData -benchmem ./internal/validation/csv/
func BenchmarkValidateCSVData_TypicalEMG(b *testing.B) {
	const (
		rowCount    = 1000
		channelCols = 16 // Time + 16 channel
	)

	// Build typical EMG records (header + rowCount body rows)
	records := make([][]string, 0, rowCount+1)
	header := make([]string, channelCols+1)
	header[0] = "Time"
	for i := 1; i <= channelCols; i++ {
		header[i] = fmt.Sprintf("Ch%d", i)
	}
	records = append(records, header)
	for r := 0; r < rowCount; r++ {
		row := make([]string, channelCols+1)
		row[0] = fmt.Sprintf("%.3f", float64(r)*0.001)
		for c := 1; c <= channelCols; c++ {
			row[c] = fmt.Sprintf("%.4f", float64(r%50)*0.05+float64(c)*0.1)
		}
		records = append(records, row)
	}

	v := NewValidator()
	const filename = "bench_emg.csv"

	// 計算總 byte size,讓 b.SetBytes 報 throughput
	var totalBytes int64
	for _, row := range records {
		for _, cell := range row {
			totalBytes += int64(len(cell))
		}
	}
	b.SetBytes(totalBytes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.ValidateCSVData(records, filename); err != nil {
			b.Fatalf("ValidateCSVData unexpected err: %v", err)
		}
	}
}
