package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSaveConfigAtomic_ByteIdenticalEncoding 鎖死 JSON byte-identity 契約:
// SaveConfigAtomic 委派 fsperm.AtomicWriteFile 後,寫出的 bytes 必須與
// json.NewEncoder(w).SetIndent("", "  ").Encode(c) 完全相同(2-space indent +
// Encode 附加的結尾換行)。
//
// 為何不靠 RoundTrip:TestSaveConfigAtomic_RoundTrip 經 LoadConfig 把 bytes 正規化
// 回 AppConfig,故抓不到格式回歸(改 minify、掉 SetIndent、掉 trailing newline 都仍
// 往返成功)。此測試用「不同 API」json.MarshalIndent + 手動補 '\n' 交叉推導期望值
// (Encoder.Encode == MarshalIndent 輸出 + 一個結尾換行),避免套套邏輯。
// codex review round 3 [P3]。
func TestSaveConfigAtomic_ByteIdenticalEncoding(t *testing.T) {
	c := DefaultConfig()
	path := filepath.Join(t.TempDir(), "config.json")

	require.NoError(t, c.SaveConfigAtomic(path))

	got, err := os.ReadFile(path) //nolint:gosec // test path
	require.NoError(t, err)

	// json.Encoder.Encode(v) 寫 v 的縮排 JSON 後附一個 '\n';MarshalIndent 不附。
	want, err := json.MarshalIndent(c, "", "  ")
	require.NoError(t, err)
	want = append(want, '\n')

	require.Equal(t, string(want), string(got), "config bytes 必須符合 byte-identity 契約")
}
