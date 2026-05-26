package chart

import (
	"encoding/json"
	"math"
	"testing"

	"count_mean/internal/models"
)

// TestGetChartStatistics_AllSameTimestamps_NoInfSamplingRate 釘住 修補:
// 當 dataset 所有 time 戳相同時 (退化 case,例如 sensor lockup / data dump bug),
// avgInterval = 0,sampling_rate = 1/0 = +Inf。
//
// JSON encoder 對 +Inf / NaN 不支援 (encoding/json 標準行為:
// json: unsupported value: +Inf),會讓下游 RPC 整段 fail。
//
// 修補後:avgInterval == 0 或 1/avgInterval 非有限值時不寫入 sampling_rate key。
func TestGetChartStatistics_AllSameTimestamps_NoInfSamplingRate(t *testing.T) {
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 5.0, Channels: []float64{1.0}},
			{Time: 5.0, Channels: []float64{2.0}},
			{Time: 5.0, Channels: []float64{3.0}},
		},
	}

	stats := GetChartStatistics(dataset, []int{1})

	// 修補後:sampling_rate 不存在,或存在但為 finite 值
	if rate, ok := stats["sampling_rate"]; ok {
		f, isFloat := rate.(float64)
		if !isFloat {
			t.Errorf("sampling_rate should be float64; got %T", rate)
		} else if math.IsInf(f, 0) || math.IsNaN(f) {
			t.Errorf("sampling_rate should not be Inf/NaN; got %v", f)
		}
	}

	// 修補後:整個 stats map 必須能 JSON marshal (json.Marshal 對 +Inf 會 error)
	if _, err := json.Marshal(stats); err != nil {
		t.Errorf("stats map should be JSON-marshalable; got error: %v", err)
	}
}

// TestGetChartStatistics_NegativeDuration_NoInfSamplingRate 釘住:當 last <
// first time (亂序 / corrupted dataset),duration 為負,sampling_rate 變負 Inf 或負值。
// 修補後不寫入 negative sampling_rate (取餘 finite 仍可寫,但典型情境下會被 guard 擋住)。
func TestGetChartStatistics_NegativeDuration_NoInfSamplingRate(t *testing.T) {
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 10.0, Channels: []float64{1.0}},
			{Time: 5.0, Channels: []float64{2.0}}, // 倒退
		},
	}

	stats := GetChartStatistics(dataset, []int{1})

	if rate, ok := stats["sampling_rate"]; ok {
		f := rate.(float64)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			t.Errorf("sampling_rate should not be Inf/NaN even for negative duration; got %v", f)
		}
	}

	if _, err := json.Marshal(stats); err != nil {
		t.Errorf("stats map should be JSON-marshalable; got error: %v", err)
	}
}

// TestChartStatistics_SanitizesHeaders 守護
//
// dataset.Headers 是 caller-controlled (來自 CSV 第一行) 內容,可能含
// `</script>` / `<!--` / U+2028 / 控制字元等 XSS payload。即使目前 chart
// 套件下游用 html/template + JSON marshal 仍可 escape,但 defense-in-depth
// 要求 chart_statistics.go 自己也要過一次 sanitizeChartString,避免未來
// caller 改用 raw HTML / iframe / SSR 模板時留下隱含注入破口。
//
// 此 test 構造一個含 `</script><script>alert(1)</script>` payload 的 header,
// 驗證:
//  1. GetAvailableColumns 回的 ColumnInfo.Name 已被 sanitize (含 `</` → `<\/`)
//  2. GetChartStatistics 回的 column_statistics[*]["name"] 同樣已 sanitize
func TestChartStatistics_SanitizesHeaders(t *testing.T) {
	const xssPayload = "</script><script>alert(1)</script>"
	const sanitizedFragment = "<\\/script>"

	dataset := &models.EMGDataset{
		Headers: []string{"Time", xssPayload},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0}},
			{Time: 0.001, Channels: []float64{2.0}},
		},
	}

	t.Run("GetAvailableColumns_SanitizesName", func(t *testing.T) {
		cols := GetAvailableColumns(dataset)
		if len(cols) != 1 {
			t.Fatalf("expected 1 column, got %d", len(cols))
		}
		name := cols[0].Name
		// 原始 payload 不應原樣保留 (至少 `</` 要被 escape 成 `<\/`)
		if name == xssPayload {
			t.Errorf("ColumnInfo.Name 必須被 sanitize,但仍為 raw payload: %q", name)
		}
		// sanitizeChartString 對 `</` → `<\/` 是固定行為
		if !contains(name, sanitizedFragment) {
			t.Errorf("ColumnInfo.Name 應含 escaped fragment %q,實際=%q", sanitizedFragment, name)
		}
	})

	t.Run("GetChartStatistics_ColumnStatsSanitizesName", func(t *testing.T) {
		stats := GetChartStatistics(dataset, []int{1})
		colStats, ok := stats["column_statistics"].([]map[string]any)
		if !ok {
			t.Fatalf("column_statistics 型別錯誤: %T", stats["column_statistics"])
		}
		if len(colStats) != 1 {
			t.Fatalf("expected 1 column stat, got %d", len(colStats))
		}
		name, ok := colStats[0]["name"].(string)
		if !ok {
			t.Fatalf("column_statistics[0][name] 型別錯誤: %T", colStats[0]["name"])
		}
		if name == xssPayload {
			t.Errorf("column_statistics name 必須被 sanitize,但仍為 raw payload: %q", name)
		}
		if !contains(name, sanitizedFragment) {
			t.Errorf("column_statistics name 應含 escaped fragment %q,實際=%q", sanitizedFragment, name)
		}
	})
}

// contains 是 strings.Contains 的內聯版,避免 chart_statistics_test.go 為了一個
// fragment match 引入完整 strings import (test 檔已有 json/math/testing,
// 加 strings 等於 import 4 個 stdlib for 1 個 helper)。
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestGetChartStatistics_NormalCase_FiniteSamplingRate 確認正常 dataset 仍能
// 計算出有限 sampling_rate。
func TestGetChartStatistics_NormalCase_FiniteSamplingRate(t *testing.T) {
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0}},
			{Time: 0.001, Channels: []float64{2.0}},
			{Time: 0.002, Channels: []float64{3.0}},
		},
	}

	stats := GetChartStatistics(dataset, []int{1})

	rate, ok := stats["sampling_rate"].(float64)
	if !ok {
		t.Fatalf("sampling_rate missing or wrong type; got %T", stats["sampling_rate"])
	}
	if math.IsInf(rate, 0) || math.IsNaN(rate) {
		t.Errorf("sampling_rate should be finite; got %v", rate)
	}
	// 1ms 間隔 → 1000Hz
	if rate < 999 || rate > 1001 {
		t.Errorf("sampling_rate should be ≈1000; got %v", rate)
	}
}
