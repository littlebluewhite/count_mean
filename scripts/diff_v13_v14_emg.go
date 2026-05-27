//go:build ignore

// diff_v13_v14_emg — Throwaway:比對 V.13(Big5)與 V.14(UTF-8)manifest 的
// EMGFile 欄差異,幫助 user 手動修正 V.14 manifest 內被誤改的 EMG 檔名。
//
// 動機:V.14 manifest 升級時 NSF 系列(及部分 SF)的 EMGFile 欄被誤改
// (BTS → BTS%、Cor_RAE / Cor_RAES 尾綴被刪),導致 Chart Composer 跑不動。
// 此 script 列出 V.13 vs V.14 各 row 的 EMGFile 差異,user 看完手動 patch。
//
// 編碼:V.13 是 Big5(舊版工作流預設),V.14 已被 manual 轉成 UTF-8(2026-05-27
// session 工作 tree 內狀態)— 故 V.13 走 Big5 decoder,V.14 直接讀 UTF-8。
//
// 用法:
//   go run scripts/diff_v13_v14_emg.go <V.13.csv> <V.14.csv>
//
// 不會修改任何檔案 — 純 stdout 輸出供 user review。
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

type manifestRow struct {
	subject string
	emgFile string
}

// loadManifest 讀指定路徑 manifest,挑 Subject(col 0)+ EMGFile(col 3)。
// big5=true 時走 Big5 → UTF-8 transformer;false 直接讀(假設已 UTF-8)。
//
// CSV reader 設定:
//   - FieldsPerRecord = -1:容忍每 row 欄位數不一致(V.13/V.14 欄數略有差異)
//   - LazyQuotes = true:允許未配對的 quote 字元(實務 CSV 常見邊界 case)
func loadManifest(path string, big5 bool) ([]manifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var reader io.Reader = f
	if big5 {
		reader = transform.NewReader(f, traditionalchinese.Big5.NewDecoder())
	}

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv parse %s: %w", path, err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("manifest %s 沒 data row (only %d records)", path, len(records))
	}

	rows := make([]manifestRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		r := records[i]
		if len(r) < 4 {
			continue
		}
		rows = append(rows, manifestRow{subject: r[0], emgFile: r[3]})
	}

	return rows, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <V.13.csv (Big5)> <V.14.csv (UTF-8)>\n", os.Args[0])
		os.Exit(1)
	}

	v13, err := loadManifest(os.Args[1], true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadManifest V.13: %v\n", err)
		os.Exit(1)
	}

	v14, err := loadManifest(os.Args[2], false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadManifest V.14: %v\n", err)
		os.Exit(1)
	}

	v13Map := make(map[string]string, len(v13))
	for _, r := range v13 {
		v13Map[r.subject] = r.emgFile
	}

	fmt.Printf("Subject\tV.13 EMGFile\tV.14 EMGFile\tStatus\n")

	var diffCount, missingCount int
	for _, r14 := range v14 {
		emg13, ok := v13Map[r14.subject]
		status := "MATCH"
		switch {
		case !ok:
			status = "V.13_MISSING"
			missingCount++
		case emg13 != r14.emgFile:
			status = "DIFF"
			diffCount++
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", r14.subject, emg13, r14.emgFile, status)
	}

	fmt.Fprintf(os.Stderr, "\nTotal rows: %d, DIFF: %d, V.13_MISSING: %d\n", len(v14), diffCount, missingCount)
}
