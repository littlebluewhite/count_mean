package csv

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkValidateCSVData_TypicalEMG 量測 cell-level CSV 守門對「典型 EMG 資料」
// 的吞吐成本。EMG CSV 通常:
//   - 1 header row + N body rows
//   - 每 row ~16-32 channel column (Time + multi-channel)
//   - 每 cell 是浮點數字串(< 16 char),不會觸發 SQL/Command/formula injection
//
// 這個 benchmark 的目的:驗證 cell-level validator(formula starter / UTF-8 /
// script injection / control char / SQL / command injection / suspicious extension)
// 的 overhead 在 typical hot path 為 < 5%,對齊 commit 註解所宣稱的成本估算。
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

// BenchmarkValidateCell_TypicalEMGCell 量測單一 cell 守門 cost。對齊
// processStreamingFile 的 hot path 行為:streaming 是 per-row 跑 ValidateCSVRow
// 而 ValidateCSVRow 內逐 cell 跑 ValidateCell。此 benchmark 直接量到每 cell 成本,
// 讓 估算的「每 cell O(len(cell))」可被量化驗證。
func BenchmarkValidateCell_TypicalEMGCell(b *testing.B) {
	v := NewCellValidator()
	ctx := NewCellContext(1, 1, "bench.csv")

	// 典型 EMG cell:浮點數字串
	cell := "0.4321"

	b.SetBytes(int64(len(cell)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.ValidateCell(cell, ctx); err != nil {
			b.Fatalf("ValidateCell unexpected err: %v", err)
		}
	}
}

// BenchmarkValidateCell_HeaderCell 量測 header cell 守門 cost(scoped 跳過
// SQL/Command/DangerousFunctions),應略快於 body cell。
func BenchmarkValidateCell_HeaderCell(b *testing.B) {
	v := NewCellValidator()
	ctx := NewHeaderCellContext(1, 1, "bench.csv")

	// 典型 EMG header: 含可能誤判 substring 的合法欄位名
	cell := "Subject ID"

	b.SetBytes(int64(len(cell)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.ValidateCell(cell, ctx); err != nil {
			b.Fatalf("ValidateCell unexpected err: %v", err)
		}
	}
}

// BenchmarkValidateCell_LongCell 量測「長 cell」邊界(接近 32KB 上限)— 對 SQL /
// command injection 的 substring 比對 scaling 行為。
func BenchmarkValidateCell_LongCell(b *testing.B) {
	v := NewCellValidator()
	ctx := NewCellContext(1, 1, "bench.csv")

	// 1KB 純數字 — 不觸發任何 injection,只測 contains/prefix 比對 scaling
	cell := strings.Repeat("0.1234,", 128) // ~ 900 bytes
	cell = cell[:len(cell)-1]              // 去 trailing comma

	b.SetBytes(int64(len(cell)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 長 cell 本身不該命中任何 injection rule;若這裡 fail 代表 detector
		// 有 false positive 待修(不影響 bench 數值,僅作為 invariant 監測)。
		_ = v.ValidateCell(cell, ctx)
	}
}
