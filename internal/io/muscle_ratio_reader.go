package io

import (
	stderrors "errors"
	stdio "io"
	"math"
	"strconv"
	"strings"

	"count_mean/internal/csvutil"
	"count_mean/internal/parsers"
)

// err113 sentinel — 承載 user-facing 訊息;internal/composer 的
// loadMuscleRatio 以 errors.Is 比對這兩個 sentinel 決定 Stage 與訊息。
var (
	ErrMuscleRatioCSVEmpty    = stderrors.New("muscle_ratio CSV 為空或缺少資料行")
	ErrMuscleRatioCSVNoHeader = stderrors.New("muscle_ratio CSV 標題不足: 至少需要時間欄與一個 ratio 欄")
)

// ReadMuscleRatioOutputAll 讀回 [CSVHandler.WriteMuscleRatioOutputAll] 寫出的
// per-subject Output 1(full time-series ratio CSV),是該 writer 的反函式。
//
// layout: header ["Time (s)", PairLabels...] + N data rows。
//   - BOM 由 parsers.ReadCSVRecords 剝除。
//   - 每個 cell 先經 csvutil.UnsanitizeCell 還原 writer 的 formula-injection escape。
//   - 空 / 不可解析的 ratio cell → NaN(不是 0):writer 把 NaN/Inf 寫成空 cell,
//     若靜默給 0 等於把缺值畫成真實 0,對共收縮比值研究是嚴重誤導。
//   - 欄數少於 header 的 jagged row、Time 不可解析的 row 直接 skip。
//   - header 名稱為空白的欄整欄忽略。
//
// 回傳 payload 的 Subject 為空(檔名才帶 subject,reader 不推導)。
func ReadMuscleRatioOutputAll(r stdio.Reader) (*MuscleRatioOutputAllPayload, error) {
	records, err := parsers.ReadCSVRecords(r)
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, ErrMuscleRatioCSVEmpty
	}
	header := records[0]
	if len(header) < 2 {
		return nil, ErrMuscleRatioCSVNoHeader
	}

	var cols []int // 有名稱的 ratio 欄在 header 內的 index
	var labels []string
	for j := 1; j < len(header); j++ {
		name := strings.TrimSpace(csvutil.UnsanitizeCell(header[j]))
		if name == "" {
			continue
		}
		cols = append(cols, j)
		labels = append(labels, name)
	}

	dataRows := records[1:]
	p := &MuscleRatioOutputAllPayload{
		PairLabels: labels,
		Times:      make([]float64, 0, len(dataRows)),
		Ratios:     make([][]float64, len(cols)),
	}
	for k := range p.Ratios {
		p.Ratios[k] = make([]float64, 0, len(dataRows))
	}

	for _, row := range dataRows {
		if len(row) < len(header) {
			continue
		}
		t, ok := parseMuscleRatioCell(row[0])
		if !ok {
			continue
		}
		p.Times = append(p.Times, t)
		for k, j := range cols {
			v, ok := parseMuscleRatioCell(row[j])
			if !ok {
				v = math.NaN()
			}
			p.Ratios[k] = append(p.Ratios[k], v)
		}
	}
	return p, nil
}

// parseMuscleRatioCell 還原 escape 後解析 float;空 / 不可解析回 ok=false。
func parseMuscleRatioCell(cell string) (float64, bool) {
	s := strings.TrimSpace(csvutil.UnsanitizeCell(cell))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
