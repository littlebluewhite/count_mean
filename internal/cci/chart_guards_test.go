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
// 對齊 CCIAnalysisResult.Validate（ADR-0053）的 duration guard：當 GaitEndTime <=
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
// 修法：長度不符由 CCIAnalysisResult.Validate 持有（ADR-0053），
// GenerateCCIInteractiveChart 在 entry 呼叫它；downsample 不再重檢。
func TestDownsampleCCI_MismatchedLength(t *testing.T) {
	// GenerateCCIInteractiveChart 入口走 Validate — caller-facing API 不該洩漏內部 downsample 細節。
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
			"entry-level rejection 必須回 ErrPairLengthMismatch")
		assert.Empty(t, buf.String(),
			"reject 路徑不該寫出半成品 HTML")
	})
}
