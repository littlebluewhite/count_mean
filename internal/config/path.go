package config

import (
	"os"
	"path/filepath"
)

// FallbackConfigPath 是 UserConfigDir() 失敗時的最終 fallback — 走 process CWD
// 的 ./config.json。維持與舊版相容,讓在開發 / 測試環境(專案根目錄啟動)
// 仍能直接讀本地 config.json。
//
// 相對路徑語意:相對於 process working directory (Wails build 後通常是
// app bundle 內部),不是 binary 路徑。
const FallbackConfigPath = "./config.json"

// UserConfigSubdir 是 config 檔案在使用者 config 目錄底下的子目錄名稱。
// 例:macOS 為 ~/Library/Application Support/count_mean/config.json,
// Linux XDG 為 ~/.config/count_mean/config.json,
// Windows 為 %AppData%\count_mean\config.json。
const UserConfigSubdir = "count_mean"

// ResolveDefaultConfigPath 回傳 GUI 啟動時讀取/寫入 config 的預設路徑。
//
// 過去這個 helper 只放在 package main,GUI 內部 SaveConfig 用 hardcoded
// "./config.json" 跟啟動時讀的路徑不對稱 — 病患在 macOS bundle (.app) 啟動
// 環境下 CWD 是 "/",寫進 "/config.json" 寫不到、又靜默 fail,使用者修了設定
// 重新啟動發現「設定沒存到」。把這個 helper 提到 internal/config 後,GUI 構造
// App 時可以注入同一個路徑,讀寫對稱。
//
// 行為:
//   - os.UserConfigDir() 成功 → <dir>/count_mean/config.json
//   - 失敗(罕見,需 HOME / APPDATA 都沒設)→ fallback 到 "./config.json"
//
// 不在這裡建立目錄;config 讀檔走 LoadConfig 的 Stat → 不存在即走 default
// 流程,維持原本「missing 是合法初始狀態」契約;config 寫檔 (SaveConfig
// 走新的 SaveConfigAtomic 路徑)會自己 MkdirAll parent。
func ResolveDefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		// 真的拿不到使用者 config 目錄(例如測試環境完全沒 HOME),
		// 退回 CWD 相對路徑 — 維持 dev / test workflow 不變。
		return FallbackConfigPath
	}

	return filepath.Join(dir, UserConfigSubdir, "config.json")
}
