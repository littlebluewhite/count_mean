package fsperm_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/security/fsperm"
)

// TestEvalSymlinksWithFallback_* 直接釘住三具合一後 (ADR-0028) resolver 的核心契約 —
// 收斂前各 copy 僅經 caller 間接覆蓋。

// buildMissingDepth 在 existing base dir 底下接上 n 個「不存在」的 segment,回傳該深路徑;
// resolver 須逐層 parent-walk n 次才抵達 base。depth 測試共用。
func buildMissingDepth(base string, n int) string {
	p := base
	for i := 0; i < n; i++ {
		p = filepath.Join(p, string(rune('a'+i)))
	}
	return p
}

func TestEvalSymlinksWithFallback_ExistingPath(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	got, err := fsperm.EvalSymlinksWithFallback(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, want, got, "existing dir 應等同 filepath.EvalSymlinks")
}

func TestEvalSymlinksWithFallback_NonExistentLeafFallsBack(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	leaf := filepath.Join(dir, "not-created-yet.csv")
	got, err := fsperm.EvalSymlinksWithFallback(leaf, 0)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(resolvedDir, "not-created-yet.csv"), got,
		"non-existent leaf 應 fallback 到 resolved parent + 接回 suffix")
}

func TestEvalSymlinksWithFallback_ParentSymlinkResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink 建立在 Windows 需特權,跳過")
	}
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(realDir, 0o750))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(realDir, link))

	got, err := fsperm.EvalSymlinksWithFallback(filepath.Join(link, "child.csv"), 0)
	require.NoError(t, err)
	resolvedReal, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(resolvedReal, "child.csv"), got,
		"parent symlink 應被解析後接回 suffix")
}

// TestEvalSymlinksWithFallback_DepthCapDiscriminates 是 codex R2 [P2] 要求的「配對鑑別
// 測試」:同一條 >8 層 non-existent 路徑,maxDepth=8 必須 error(訊息含 cap 值),
// maxDepth=0 必須成功。用「同一輸入」才能證明 bounded≠unbounded — 避免「反正會在 root
// 終止」的非鑑別性 false-pass(cf. ADR-0022 uniform-grid 盲點)。
func TestEvalSymlinksWithFallback_DepthCapDiscriminates(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	const n = 10 // 10 層 > cap 8
	deep := buildMissingDepth(dir, n)

	_, errBounded := fsperm.EvalSymlinksWithFallback(deep, 8)
	require.Error(t, errBounded, "10 層 > cap 8 應 fail-closed error")
	assert.Contains(t, errBounded.Error(), "(8)",
		"超限訊息應嵌入 cap 值,實際=%v", errBounded)

	gotUnbounded, errUnbounded := fsperm.EvalSymlinksWithFallback(deep, 0)
	require.NoError(t, errUnbounded, "unbounded 對同一深路徑應成功")
	assert.Equal(t, buildMissingDepth(resolvedDir, n), gotUnbounded,
		"unbounded 應解析 parent + 接回完整 suffix")
}

// TestEvalSymlinksWithFallback_DepthCapOffByOneBoundary 釘住 cap 的精確位置 (off-by-one):
// 同一 maxDepth=8 下,恰好 cap-1 (7 層) non-existent 必須成功、恰好 cap (8 層) 必須
// fail-closed error。比 DepthCapDiscriminates(深度遠超 cap)更嚴 — 一個把守衛
// `depth <= 0` 寫成 `depth < 0` 的 off-by-one 實作會讓 8 層誤過,正好被此測試抓到。
func TestEvalSymlinksWithFallback_DepthCapOffByOneBoundary(t *testing.T) {
	dir := t.TempDir()

	// cap-1 (7 層):parent-walk 在 depth 歸 0 前抵達 existing dir → 成功
	_, errAtLimit := fsperm.EvalSymlinksWithFallback(buildMissingDepth(dir, 7), 8)
	require.NoError(t, errAtLimit, "7 層 (= cap-1) 應在上限內成功")

	// cap (8 層):再深一層,depth 在抵達 dir 前歸 0 → fail-closed error
	_, errOverLimit := fsperm.EvalSymlinksWithFallback(buildMissingDepth(dir, 8), 8)
	require.Error(t, errOverLimit, "8 層 (= cap) 應觸發 fail-closed 上限 error")
}

// TestEvalSymlinksWithFallback_DepthCapHonorsParameterValue 釘住 cap 真的用「傳入的
// maxDepth 值」而非硬編 8 (codex R3 [P3]):用 maxDepth=3 重跑邊界——cap-1 (2 層) 成功、
// cap (3 層) fail-closed 且訊息含 `(3)`。一個對任何 >0 maxDepth 都硬編 cap=8 的實作會讓
// 3 層誤過、訊息印 `(8)`,被此測試抓到;與 OffByOneBoundary (cap=8) 互補。
func TestEvalSymlinksWithFallback_DepthCapHonorsParameterValue(t *testing.T) {
	dir := t.TempDir()

	_, errOk := fsperm.EvalSymlinksWithFallback(buildMissingDepth(dir, 2), 3)
	require.NoError(t, errOk, "2 層 (= cap-1, cap=3) 應成功")

	_, errCap := fsperm.EvalSymlinksWithFallback(buildMissingDepth(dir, 3), 3)
	require.Error(t, errCap, "3 層 (= cap=3) 應 fail-closed")
	assert.Contains(t, errCap.Error(), "(3)",
		"訊息應嵌入實際 cap 值 3 (非硬編 8),實際=%v", errCap)
}

// TestEvalSymlinksWithFallback_ExistingSymlinkLeafResolved 釘住「path 本身就是 existing
// symlink」時第一步 filepath.EvalSymlinks(path) 直接全解析 (codex R3 [P3])。沒有此測試,
// 一個「永遠走 parent-fallback、把 leaf basename 原樣接回」的實作會對 existing symlink
// leaf 回傳 link 路徑而非 target、卻仍通過其他子測試。
func TestEvalSymlinksWithFallback_ExistingSymlinkLeafResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink 建立在 Windows 需特權,跳過")
	}
	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.csv")
	require.NoError(t, os.WriteFile(realFile, []byte("x"), 0o600))
	link := filepath.Join(dir, "link.csv")
	require.NoError(t, os.Symlink(realFile, link))

	got, err := fsperm.EvalSymlinksWithFallback(link, 0)
	require.NoError(t, err)
	wantReal, err := filepath.EvalSymlinks(realFile)
	require.NoError(t, err)
	assert.Equal(t, wantReal, got,
		"existing symlink leaf 應由 EvalSymlinks 直接解析到 target,而非接回 link 名")
}

func TestEvalSymlinksWithFallback_EmptyPathErrors(t *testing.T) {
	_, err := fsperm.EvalSymlinksWithFallback("", 0)
	require.Error(t, err, "空字串應 fail-closed error")
}
