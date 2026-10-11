package io

import (
	"os"
	"testing"

	"count_mean/internal/i18n"
)

// TestMain 初始化 i18n global singleton,locale 釘 zh-TW(同 phase_sync)。
//
// ReadCSV 的錯誤是帶 key 的 i18n.Error,Error() 固定以內建 zh-TW catalog 渲染、不依賴
// global;global 供測試切 locale 驗證 i18n.Localize —— globalI18n==nil 時 SetLocale 是
// no-op。InitI18n 內部依 LANG 偵測 locale,故再顯式 SetLocale(LocaleZhTW)。
func TestMain(m *testing.M) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → 只用內建 catalog
	i18n.SetLocale(i18n.LocaleZhTW)
	os.Exit(m.Run())
}
