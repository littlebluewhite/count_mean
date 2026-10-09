package gui

import (
	"os"
	"testing"

	"count_mean/internal/i18n"
)

// TestMain 比照 production(main.go:InitI18n → SetLocale(cfg.Language))初始化 i18n
// global singleton:載入內建 catalog,locale 釘死 zh-TW。
//
// handler 的失敗訊息前綴由 failMessage 經 i18n.T 取得([[Webview envelope]]);
// globalI18n==nil 時 T() 回 key 本身,result.Message 會是「error.handler.*: …」而非
// 中文前綴。InitI18n 內部依 LANG 偵測 locale(CI 上 en_US.UTF-8 → en-US),故再顯式
// SetLocale(LocaleZhTW)。會切換 locale 的測試各自以 t.Cleanup 還原。
func TestMain(m *testing.M) {
	// 不存在路徑 → 不讀外部 JSON,只用內建 catalog。
	if err := i18n.InitI18n("./nonexistent"); err != nil {
		panic(err)
	}

	i18n.SetLocale(i18n.LocaleZhTW)
	os.Exit(m.Run())
}
