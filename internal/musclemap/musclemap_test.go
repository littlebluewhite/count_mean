package musclemap

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rightHeaders 是 8 個右側通道 header (含 TA&IO 與 GMax 大小寫變體)。
func rightHeaders() []string {
	return []string{
		"R.RA: EMG 1 (from SF8_...) ->Filter->RMS []",
		"R.ES: EMG 2 (from SF8_...)",
		"R.IL: EMG 3 (from SF8_...)",
		"R.GMax: EMG 4 (from SF8_...)",
		"R.RF: EMG 5 (from SF8_...)",
		"R.BF: EMG 6 (from SF8_...)",
		"R.TA&IO: EMG 7 (from SF8_...)",
		"R.MF: EMG 8 (from SF8_...)",
	}
}

func TestRightSideChannels(t *testing.T) {
	t.Run("CanonicalNames", func(t *testing.T) {
		h := rightHeaders()
		got, err := RightSideChannels(h)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"RA": h[0], "ES": h[1], "IL": h[2], "GMax": h[3],
			"RF": h[4], "BF": h[5], "TAIO": h[6], "MF": h[7],
		}, got)
	})

	t.Run("CaseInsensitiveSuffix", func(t *testing.T) {
		h := rightHeaders()
		h[3] = "R.GMAX: EMG 4"
		got, err := RightSideChannels(h)
		require.NoError(t, err)
		assert.Equal(t, "R.GMAX: EMG 4", got["GMax"])
	})

	t.Run("IgnoresNonRight", func(t *testing.T) {
		h := append(rightHeaders(),
			"L.RA: EMG 9 (left)",
			"EMG without colon",
			"R RECTUS ABDOMINIS: full name",
			"X.RA: unknown side",
			"R.UNKNOWN: not in table",
		)
		got, err := RightSideChannels(h)
		require.NoError(t, err)
		assert.Len(t, got, 8)
		assert.Equal(t, rightHeaders()[0], got["RA"])
	})

	t.Run("RightWinsInterleaved", func(t *testing.T) {
		var h []string
		for _, r := range rightHeaders() {
			h = append(h, "L."+r[2:], r) // L.* 排在 R.* 之前
		}
		got, err := RightSideChannels(h)
		require.NoError(t, err)
		for short, header := range got {
			assert.Equal(t, "R.", header[:2], "短名 %q 應對應 R.* header", short)
		}
	})

	t.Run("DuplicateFailFast", func(t *testing.T) {
		h := append(rightHeaders(), "R.RA: EMG 2 (second session)")
		h[0] = "R.RA: EMG 1 (first session)"
		got, err := RightSideChannels(h)
		require.Error(t, err)
		assert.Nil(t, got)
		var missing *MissingMuscleError
		assert.False(t, errors.As(err, &missing), "重複不是缺失")
		assert.Contains(t, err.Error(), "重複的肌肉通道 RA")
		assert.Contains(t, err.Error(), "first session")
		assert.Contains(t, err.Error(), "second session")
	})

	t.Run("MissingIsTyped", func(t *testing.T) {
		h := rightHeaders()
		h = append(h[:3], h[4:]...) // 拿掉 GMax
		_, err := RightSideChannels(h)
		var missing *MissingMuscleError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "GMax", missing.Muscle)
		assert.Equal(t, "缺少必要的肌肉通道: GMax", err.Error())

		// 只有左側 → 回第一個必要肌肉 RA
		_, err = RightSideChannels([]string{"L.RA: x", "L.ES: x"})
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "RA", missing.Muscle)
	})
}
