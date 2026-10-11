package cci

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCCIAnalysisResult_Validate 釘住 ADR-0053 的三條不變式。
func TestCCIAnalysisResult_Validate(t *testing.T) {
	t.Parallel()

	valid := func() *CCIAnalysisResult {
		return &CCIAnalysisResult{
			TimeValues:    []float64{0, 0.5, 1},
			GaitStartTime: 0,
			GaitEndTime:   1,
			PairResults:   []CCIResult{{PairName: "A", Values: []float64{1, 2, 3}}},
		}
	}

	cases := []struct {
		name    string
		mutate  func(r *CCIAnalysisResult) *CCIAnalysisResult
		wantErr error
	}{
		{"valid", func(r *CCIAnalysisResult) *CCIAnalysisResult { return r }, nil},
		{"nil_result", func(*CCIAnalysisResult) *CCIAnalysisResult { return nil }, ErrNilResult},
		{"start_nan", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitStartTime = math.NaN(); return r }, ErrInvalidGaitCycle},
		{"start_pos_inf", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitStartTime = math.Inf(1); return r }, ErrInvalidGaitCycle},
		{"start_neg_inf", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitStartTime = math.Inf(-1); return r }, ErrInvalidGaitCycle},
		{"end_nan", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitEndTime = math.NaN(); return r }, ErrInvalidGaitCycle},
		{"end_pos_inf", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitEndTime = math.Inf(1); return r }, ErrInvalidGaitCycle},
		{"end_neg_inf", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitEndTime = math.Inf(-1); return r }, ErrInvalidGaitCycle},
		{"end_equals_start", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitEndTime = r.GaitStartTime; return r }, ErrInvalidGaitCycle},
		{"end_before_start", func(r *CCIAnalysisResult) *CCIAnalysisResult { r.GaitEndTime = -1; return r }, ErrInvalidGaitCycle},
		{"short_pair", func(r *CCIAnalysisResult) *CCIAnalysisResult {
			r.PairResults = append(r.PairResults, CCIResult{PairName: "B", Values: []float64{1}})
			return r
		}, ErrPairLengthMismatch},
		{"nil_pair_values", func(r *CCIAnalysisResult) *CCIAnalysisResult {
			r.PairResults = append(r.PairResults, CCIResult{PairName: "B"})
			return r
		}, ErrPairLengthMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.mutate(valid()).Validate()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestGaitPercent 釘住公式與「不 clamp」。
func TestGaitPercent(t *testing.T) {
	t.Parallel()

	r := &CCIAnalysisResult{GaitStartTime: 1, GaitEndTime: 3}

	assert.Equal(t, 25.0, r.GaitPercent(1.5))
	assert.Equal(t, 0.0, r.GaitPercent(1))
	assert.Equal(t, 100.0, r.GaitPercent(3))
	assert.Equal(t, -25.0, r.GaitPercent(0.5), "start 之前不 clamp 到 0")
	assert.Equal(t, 125.0, r.GaitPercent(3.5), "end 之後不 clamp 到 100")
	assert.Equal(t, gaitPercent(1.5, 1, 3), r.GaitPercent(1.5))
}
