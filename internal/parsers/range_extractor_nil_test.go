package parsers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// TestRangeExtractor_NilData pins the nil-data guard contract for
// GetEMGDataInTimeRange / GetANCDataInTimeRange.
//
// Validator path (ValidateEMGData / ValidateForceData) already guards nil,
// but the extractor path historically slipped through and panicked on
// data.Time index access. This test ensures both extractors reject nil and
// empty-data inputs symmetrically with the validators.
func TestRangeExtractor_NilData(t *testing.T) {
	t.Run("GetEMGDataInTimeRange rejects nil data", func(t *testing.T) {
		result, err := GetEMGDataInTimeRange(nil, 0.0, 1.0)
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrNilData)
		// Domain-specific zh-TW message is part of the user-facing contract
		// (前端會直接顯示 err.Error() 給使用者).
		require.Contains(t, err.Error(), "EMG 數據為空")
	})

	t.Run("GetEMGDataInTimeRange rejects empty data slice", func(t *testing.T) {
		empty := &models.PhaseSyncEMGData{
			Time:     []float64{},
			Channels: map[string][]float64{},
			Headers:  []string{},
		}
		result, err := GetEMGDataInTimeRange(empty, 0.0, 1.0)
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrNilData)
		require.Contains(t, err.Error(), "EMG 數據為空")
	})

	t.Run("GetANCDataInTimeRange rejects nil data", func(t *testing.T) {
		result, err := GetANCDataInTimeRange(nil, 0.0, 1.0)
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrNilData)
		require.Contains(t, err.Error(), "力板數據為空")
	})

	t.Run("GetANCDataInTimeRange rejects empty data slice", func(t *testing.T) {
		empty := &models.ForceData{
			Time:    []float64{},
			Forces:  map[string][]float64{},
			Headers: []string{},
		}
		result, err := GetANCDataInTimeRange(empty, 0.0, 1.0)
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrNilData)
		require.Contains(t, err.Error(), "力板數據為空")
	})
}
