package cci

import (
	"os"
	"testing"

	"count_mean/internal/i18n"
)

// TestMain 初始化 i18n global singleton,locale 釘 zh-TW。
//
// cci 錯誤是帶 key 的 i18n.Error,Error() 固定以內建 zh-TW catalog 渲染、不依賴 global
// (ADR-0048);global 供測試切 locale 驗證 i18n.Localize —— globalI18n==nil 時
// SetLocale 是 no-op。InitI18n 內部走 DetectSystemLocale,CI 上 LANG=en_US.UTF-8 會把
// locale 蓋成 LocaleEnUS,這裡顯式 SetLocale(LocaleZhTW) 釘回繁中。
func TestMain(m *testing.M) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → fallback 走內建 translationsZhTW
	i18n.SetLocale(i18n.LocaleZhTW)    // 覆寫 InitI18n 內部的 system-locale detection
	os.Exit(m.Run())
}
