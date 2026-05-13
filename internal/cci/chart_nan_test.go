package cci

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateCCIInteractiveChart_FiltersNaN 是 Wave 7 review codex P2 的
// regression test：CalculateCCIRudolph 對無效輸入 (NaN/Inf/負值) 回傳
// math.NaN()，但 NaN 直接灌進 opts.LineData{Value: v} 會讓 go-echarts 產出
// 空的 option block，UI 顯示成功卻是空白圖。修法是 chart 端 filter NaN/Inf
// → echarts null（line gap），與 CSV exporter NaN-row dropping 行為對齊。
func TestGenerateCCIInteractiveChart_FiltersNaN(t *testing.T) {
	t.Run("NaN_renders_as_null_gap_not_empty_option", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "test-subject",
			TimeValues: []float64{0, 0.5, 1.0},
			PairResults: []CCIResult{
				{
					PairName: "RA/ES",
					Values:   []float64{0.5, math.NaN(), 0.8},
				},
			},
			GaitStartTime: 0,
			GaitEndTime:   1.0,
		}

		var buf bytes.Buffer
		err := GenerateCCIInteractiveChart(result, &buf)
		require.NoError(t, err)

		html := buf.String()
		assert.NotEmpty(t, html, "chart HTML 不應為空")
		// go-echarts 對 opts.LineData{Value: nil} 用 omitempty 渲染成 {} (空 object)
		// echarts JS 視 {} 為 missing point (line gap)，與 null 視覺效果相同。
		// 預期 NaN 點位處於中間 → "...{"value":0.5},{},{"value":0.8}..."
		assert.Contains(t, html, `{"value":0.5},{}`, "NaN 點前應緊接 {} gap marker")
		assert.Contains(t, html, `{},{"value":0.8}`, "NaN 點後應緊接 {} gap marker")
		// 必須有 series 結構：option block 非空（含 PairName 與 data 陣列）
		assert.Contains(t, html, "RA/ES", "series PairName 應出現在 chart option")
		// NaN 直送 echarts 會造成 option block 整段空，這條防止退步
		assert.NotContains(t, strings.ToLower(html), "nan", "raw NaN 字串不應出現在輸出")
	})

	t.Run("PosInf_NegInf_also_filtered", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "test-subject",
			TimeValues: []float64{0, 1, 2, 3},
			PairResults: []CCIResult{
				{
					PairName: "IL/GMax",
					Values:   []float64{1.0, math.Inf(1), math.Inf(-1), 2.0},
				},
			},
			GaitStartTime: 0,
			GaitEndTime:   3.0,
		}

		var buf bytes.Buffer
		err := GenerateCCIInteractiveChart(result, &buf)
		require.NoError(t, err)

		html := buf.String()
		// 兩個連續 ±Inf 應該變成兩個連續的 {} gap markers
		assert.Contains(t, html, `{"value":1},{},{},{"value":2}`, "兩個連續 ±Inf 應 render 為 {},{}")
		assert.NotContains(t, html, "+Inf")
		assert.NotContains(t, html, "-Inf")
	})

	t.Run("all_NaN_does_not_panic", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "all-invalid",
			TimeValues: []float64{0, 1, 2},
			PairResults: []CCIResult{
				{
					PairName: "X/Y",
					Values:   []float64{math.NaN(), math.NaN(), math.NaN()},
				},
			},
			GaitStartTime: 0,
			GaitEndTime:   2.0,
		}

		var buf bytes.Buffer
		require.NotPanics(t, func() {
			err := GenerateCCIInteractiveChart(result, &buf)
			require.NoError(t, err)
		})
		assert.NotEmpty(t, buf.String(), "全 NaN 也應產出有效 HTML（雖然線完全斷）")
	})

	t.Run("finite_values_pass_through_unchanged", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "happy",
			TimeValues: []float64{0, 1, 2},
			PairResults: []CCIResult{
				{
					PairName: "RF/BF",
					Values:   []float64{0.1, 0.2, 0.3},
				},
			},
			GaitStartTime: 0,
			GaitEndTime:   2.0,
		}

		var buf bytes.Buffer
		err := GenerateCCIInteractiveChart(result, &buf)
		require.NoError(t, err)

		html := buf.String()
		// finite path 的 series data 應該全是 {"value":x} 沒有 {} gap
		assert.Contains(t, html, `{"value":0.1},{"value":0.2},{"value":0.3}`, "全 finite 應連續 value")
		// 不該有 series-level {} (echarts 空 object = data gap)
		// 用 ",{}" 做唯一識別：series data array 內的 gap marker
		assert.NotContains(t, html, ",{},", "finite 不該出現 series data gap marker")
		assert.Contains(t, html, "RF/BF")
	})
}
