package cci

import (
	"os"
	"testing"

	"count_mean/internal/i18n"
)

// TestMain 初始化 i18n global singleton。
//
// 與 internal/muscle_ratio/analyzer_test.go 的 TestMain 對齊：i18n 化的 CCI errors
// 透過 i18n.T() 取得字串,若 globalI18n==nil 則 T() fallback 回 key 本身
// (例 "error.cci.parse_manifest_failed"),會破壞既有 test 對中文字面 (如 "步態週期")
// 與英文 marker 字串 (如 "EMG 檔案路徑驗證失敗") 的子字串比對。
//
// InitI18n 內部走 DetectSystemLocale 後 SetLocale,在 CI 上 LANG=en_US.UTF-8 會把
// locale 蓋成 LocaleEnUS。這裡顯式 SetLocale(LocaleZhTW) 把 locale 釘死成繁中,
// 保證 catalog 解析到 zh-TW 字串,test substring 比對才會通過。
func TestMain(m *testing.M) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → fallback 走內建 translationsZhTW
	i18n.SetLocale(i18n.LocaleZhTW)    // 覆寫 InitI18n 內部的 system-locale detection
	os.Exit(m.Run())
}
