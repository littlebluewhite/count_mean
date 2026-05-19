package musclemap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssignShort_FirstAssignment 守護:第一次塞 short → 寫入成功,無 error。
func TestAssignShort_FirstAssignment(t *testing.T) {
	cm := make(map[string]string)
	err := AssignShort(cm, "RA", "R.RA: EMG 1")
	require.NoError(t, err)
	assert.Equal(t, "R.RA: EMG 1", cm["RA"])
}

// TestAssignShort_DuplicateFailFast 守護 核心:重複 short → fail-fast,
// error 訊息含「重複的肌肉通道」+ 兩個 header 字串供 user 診斷。
// muscle_ratio 與 cci 兩個 caller 共用此 helper,確保行為對稱。
func TestAssignShort_DuplicateFailFast(t *testing.T) {
	cm := map[string]string{
		"RA": "R.RA: EMG 1 (first)",
	}
	err := AssignShort(cm, "RA", "R.RA: EMG 2 (second)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "重複的肌肉通道")
	assert.Contains(t, err.Error(), "RA")
	assert.Contains(t, err.Error(), "first")
	assert.Contains(t, err.Error(), "second")

	// 重要契約:重複時不能 overwrite,既有 mapping 保留為「前者」
	assert.Equal(t, "R.RA: EMG 1 (first)", cm["RA"], "重複時 map 不該被改寫,前者保留")
}

// TestAssignShort_DifferentShortsCoexist 守護:不同 short name 不衝突,
// 兩個 distinct short 同時存在 map 中。
func TestAssignShort_DifferentShortsCoexist(t *testing.T) {
	cm := make(map[string]string)
	require.NoError(t, AssignShort(cm, "RA", "R.RA: EMG 1"))
	require.NoError(t, AssignShort(cm, "ES", "R.ES: EMG 2"))
	assert.Len(t, cm, 2)
	assert.Equal(t, "R.RA: EMG 1", cm["RA"])
	assert.Equal(t, "R.ES: EMG 2", cm["ES"])
}

// TestAssignShort_ErrorMessageStable 守護 error 訊息穩定性 — 前端 / log 可能解析格式,
// 動詞與字面 substring 不該被未來重構破壞。
func TestAssignShort_ErrorMessageStable(t *testing.T) {
	cm := map[string]string{"RA": "H1"}
	err := AssignShort(cm, "RA", "H2")
	require.Error(t, err)
	msg := err.Error()
	// 必含的 substring,用於前端可預測解析
	expectations := []string{"重複", "RA", "H1", "H2"}
	for _, exp := range expectations {
		assert.True(t, strings.Contains(msg, exp),
			"error message %q 缺少必要 substring %q", msg, exp)
	}
}
