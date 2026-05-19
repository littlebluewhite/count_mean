package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadTranslations_AtomicFailureLeavesNoPartialState 釘住 atomic publish:
// 若多 locale 載入過程中第 K 個 locale 失敗,前 K-1 個已成功處理的 locale 也不能
// 落地到 i.messages — 整個操作要嘛全部成功要嘛全部不變。
//
// 為何此契約重要:
//   - 原本實作在 loop 內直接 i.messages[locale] = merged,zh-TW + zh-CN 成功
//     寫入後,en-US 失敗 → in-memory 半新半舊
//   - UX 看到「zh-TW 是新版翻譯,en-US 是舊版」的詭異組合,settings save 顯示成功
//     但實際 state 不對齊
//   - 修補:staging map + 全部成功才 commit,任一失敗 i.messages 完全不變
//
// 測試手法:
//   1. 第一次 load good dir → snapshot state(builtin only,合法狀態)
//   2. 構造 mixed dir(zh-TW 有效 JSON,en-US 注入 verb mismatch error)
//   3. 第二次 load 該 dir → 應 return err
//   4. assert i.messages 沒有殘留 zh-TW 的新 override —— 仍是 step 1 的 state
func TestLoadTranslations_AtomicFailureLeavesNoPartialState(t *testing.T) {
	t.Parallel()

	inst := NewI18n()

	// Step 1:用 nonexistent dir 載入 — 全 builtin,合法 baseline。
	require.NoError(t, inst.LoadTranslations("./nonexistent"))

	// Snapshot 一個 key 在 step 1 的值,用來驗證 step 3 沒被覆寫。
	const sentinelKey = KeyAppTitle
	baselineZhTW := inst.T(sentinelKey)
	require.NotEmpty(t, baselineZhTW, "sentinel key %s 應有 builtin 翻譯", sentinelKey)

	// Step 2:構造 mixed dir,zh-TW 是合法 override,en-US 注入 verb mismatch
	// (master zh-TW 對 KeyHelpWindowSize 是無 verb 的 plain string;en-US 故意
	//  塞 %s/%d 等多餘 verb → 觸發 ErrTranslationVerbCountMismatch reject)
	tmpDir := t.TempDir()

	// zh-TW override:把 sentinel key 換成 "MUTATED" 看看後續是否殘留。
	zhTWPath := filepath.Join(tmpDir, "zh-TW.json")
	zhTWContent := `{"app.title": "MUTATED zh-TW 翻譯 (不該落地)"}`
	require.NoError(t, os.WriteFile(zhTWPath, []byte(zhTWContent), 0o600))

	// en-US 故意觸發 verb mismatch:master 對 app.title 是 "EMG Data Analysis"(0 verbs),
	// en-US 故意塞含 %s 的 value 讓 master(0) vs user(1) verb count 不一致。
	enUSPath := filepath.Join(tmpDir, "en-US.json")
	enUSContent := `{"app.title": "Broken en-US %s"}`
	require.NoError(t, os.WriteFile(enUSPath, []byte(enUSContent), 0o600))

	// Step 3:load 失敗的 dir,zh-TW 雖在 en-US 之前處理成功,但 atomic publish
	// 規則下不該寫入 i.messages。
	err := inst.LoadTranslations(tmpDir)
	require.Error(t, err, "en-US verb mismatch 應該讓 LoadTranslations 失敗")

	// Step 4:i.messages 完全不變 — sentinel key 仍是 step 1 的 baseline,
	// 不是 step 2 的 "MUTATED ..." 字串。
	postFailZhTW := inst.T(sentinelKey)
	require.Equal(t, baselineZhTW, postFailZhTW,
		"P2-N atomic: en-US load 失敗時,zh-TW 的 override 不該落地,實際=%q,baseline=%q",
		postFailZhTW, baselineZhTW)
	require.NotContains(t, postFailZhTW, "MUTATED",
		"P2-N atomic: 失敗的 LoadTranslations 不該留下 zh-TW 的 partial override")
}

// TestLoadTranslations_SuccessFullyCommitsAllLocales 對偶測試:全部 locale 都
// 合法時 atomic commit 確實生效(防止 重構過頭把 happy path 也廢掉)。
func TestLoadTranslations_SuccessFullyCommitsAllLocales(t *testing.T) {
	t.Parallel()

	inst := NewI18n()

	// 全部用 nonexistent — 4 個 locale 都 fallback 到 builtin,全部成功 commit。
	require.NoError(t, inst.LoadTranslations("./nonexistent"))

	// 4 個 locale 都應該在 messages 內,且每個 locale 都有 sentinel key。
	const sentinelKey = KeyAppTitle
	for _, locale := range []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP} {
		inst.SetLocale(locale)
		val := inst.T(sentinelKey)
		require.NotEmpty(t, val, "locale=%s 應該載入 builtin 翻譯", locale)
		require.NotEqual(t, sentinelKey, val,
			"locale=%s 不該 return raw key(代表 fallback failure)", locale)
	}
}
