package cci

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateCCIInteractive_ZeroDuration_Rejected — regression.
//
// 對齊 analyzer.go:464-468 CSV 出口的 duration guard：當 GaitEndTime <=
// GaitStartTime 時 buildPercentAxisLabels 會除以 0 產生 "NaN%" / "+Inf%"
// label。CSV 流程因 fail-fast 已擋住，但 library caller 直接呼叫
// GenerateCCIInteractiveChart 仍會中毒。chart 入口必須跟 CSV 一樣 fail-fast。
func TestGenerateCCIInteractive_ZeroDuration_Rejected(t *testing.T) {
	cases := []struct {
		name      string
		startTime float64
		endTime   float64
	}{
		{"equal_times", 1.5, 1.5},
		{"negative_duration", 2.0, 1.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &CCIAnalysisResult{
				Subject:    "test_subject",
				TimeValues: []float64{tc.startTime, (tc.startTime + tc.endTime) / 2, tc.endTime},
				PairResults: []CCIResult{
					{PairName: "RA/ES", Values: []float64{0.1, 0.2, 0.3}},
				},
				GaitStartTime: tc.startTime,
				GaitEndTime:   tc.endTime,
			}

			var buf bytes.Buffer
			err := GenerateCCIInteractiveChart(context.Background(), result, &buf)
			require.Error(t, err, "duration <= 0 應被拒絕")
			assert.ErrorIs(t, err, ErrInvalidGaitCycle,
				"必須 wrap ErrInvalidGaitCycle 與 CSV 路徑共用 sentinel")
			assert.Empty(t, buf.String(),
				"reject 路徑不該寫出半成品 HTML（避免下游覆蓋既有正常檔）")
		})
	}
}

// TestDownsampleCCI_MismatchedLength — regression.
//
// 選 A 嚴格：CCI spec 要求 PairResults[i].Values 與 TimeValues 1:1 對齊
// （calculator.go:34 comment 已明述）。舊版 downsampleCCIResult 對長度不符的
// pair 走 silent passthrough — 結果是 *該 pair 維持原長度而其他 pair 被壓縮*，
// 違反「all series share one X-axis」的 invariant，是把對齊 bug 從 producer
// 推到 renderer 的 anti-pattern。
//
// 修法：downsampleCCIResult 對長度不符立即 ErrPairLengthMismatch，
// GenerateCCIInteractiveChart 在 entry 也走同一份校驗。
func TestDownsampleCCI_MismatchedLength(t *testing.T) {
	t.Run("nil_pair_values_rejected", func(t *testing.T) {
		const n = 500

		timeValues := make([]float64, n)
		full := make([]float64, n)
		for i := range timeValues {
			timeValues[i] = float64(i)
			full[i] = float64(i) * 0.5
		}

		result := &CCIAnalysisResult{
			Subject:    "mismatch_nil",
			TimeValues: timeValues,
			PairResults: []CCIResult{
				{PairName: "Full", Values: full},
				{PairName: "Empty", Values: nil},
			},
			GaitStartTime: 0,
			GaitEndTime:   float64(n - 1),
		}

		out, err := downsampleCCIResult(result, 50)
		require.Error(t, err, "nil pair Values 違反對齊 invariant，必須 reject")
		assert.ErrorIs(t, err, ErrPairLengthMismatch)
		assert.Nil(t, out, "reject 時不該回傳半成品 result（避免 caller 誤用）")
	})

	t.Run("shorter_pair_values_rejected", func(t *testing.T) {
		const n = 500

		timeValues := make([]float64, n)
		full := make([]float64, n)
		short := make([]float64, n/2) // 半長度，模擬上游 producer bug
		for i := range timeValues {
			timeValues[i] = float64(i)
			full[i] = float64(i)
		}
		for i := range short {
			short[i] = float64(i)
		}

		result := &CCIAnalysisResult{
			Subject:    "mismatch_short",
			TimeValues: timeValues,
			PairResults: []CCIResult{
				{PairName: "Full", Values: full},
				{PairName: "Short", Values: short},
			},
			GaitStartTime: 0,
			GaitEndTime:   float64(n - 1),
		}

		out, err := downsampleCCIResult(result, 50)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPairLengthMismatch)
		assert.Nil(t, out)
	})

	// GenerateCCIInteractiveChart 入口必須跟 downsampleCCIResult 走同一份
	// 校驗 — caller-facing API 不該洩漏內部 downsample 細節。
	t.Run("entry_point_rejects_mismatch", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "entry_mismatch",
			TimeValues: []float64{0, 1, 2},
			PairResults: []CCIResult{
				{PairName: "Full", Values: []float64{0.1, 0.2, 0.3}},
				{PairName: "Bad", Values: []float64{0.4, 0.5}},
			},
			GaitStartTime: 0,
			GaitEndTime:   2,
		}

		var buf bytes.Buffer
		err := GenerateCCIInteractiveChart(context.Background(), result, &buf)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrPairLengthMismatch),
			"entry-level rejection 必須與 downsample 共用 sentinel")
		assert.Empty(t, buf.String(),
			"reject 路徑不該寫出半成品 HTML")
	})
}
