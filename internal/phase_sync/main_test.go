package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"os"
	"testing"

	"count_mean/internal/i18n"
)

// TestMain 初始化 i18n global singleton,locale 釘 zh-TW(同 cci / muscle_ratio)。
//
// phase_sync 的錯誤是帶 key 的 i18n.Error,Error() 固定以內建 zh-TW catalog 渲染、不依賴
// global(ADR-0048);global 供測試切 locale 驗證 i18n.Localize —— globalI18n==nil 時
// SetLocale 是 no-op。InitI18n 內部依 LANG 偵測 locale,故再顯式 SetLocale(LocaleZhTW)。
func TestMain(m *testing.M) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → 只用內建 catalog
	i18n.SetLocale(i18n.LocaleZhTW)
	os.Exit(m.Run())
}
