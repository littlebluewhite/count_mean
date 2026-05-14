//go:build !windows

package fsperm

import (
	"os"
	"syscall"
)

// WriteFlags 是應用程式建立 CSV / HTML / log / JSON / PNG 等檔案的標準 OpenFile flag。
// O_NOFOLLOW 阻擋 symlink-based TOCTOU：攻擊者無法在 OutputDir 內預先植入 symlink
// 把寫入導向 /etc/passwd 等敏感檔案。darwin / linux 會以 ELOOP 回報。
const WriteFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | syscall.O_NOFOLLOW

// AppendFlags 是 log file 等需 append 行為的 OpenFile flag。
// 同樣含 O_NOFOLLOW — 攻擊者若把 logDir/app.log 換成 symlink 指向 cron 配置檔，
// open 會立即 fail 而非靜默把 log 內容寫到別處。
const AppendFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND | syscall.O_NOFOLLOW

// ReadFlags 是讀取 CSV / 翻譯 / config 檔案的 OpenFile flag。O_NOFOLLOW 阻擋
// 攻擊者以 symlink 騙程式讀取敏感檔案；與 write 端對稱保護。
const ReadFlags = os.O_RDONLY | syscall.O_NOFOLLOW

// TmpCreateFlags 用於 atomic write 流程中建立 .tmp 中介檔：
// O_EXCL 確保「前次 crash 留下的 stale tmp」被偵測為錯誤而非靜默覆寫；
// 不含 O_TRUNC 因為 O_EXCL 本身保證檔案是新建的。O_NOFOLLOW 與 WriteFlags 對稱
// 拒絕 symlink-based TOCTOU。
const TmpCreateFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_NOFOLLOW
