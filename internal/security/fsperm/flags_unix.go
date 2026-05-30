//go:build !windows

// Package fsperm — Unix platform notes.
//
// # Atomic O_NOFOLLOW protection
//
// All four OpenFile flag constants below include `syscall.O_NOFOLLOW`, which
// causes the kernel to reject (`ELOOP`) any open() call whose final path
// component is a symlink. This blocks the classic symlink-TOCTOU race where
// an attacker drops a symlink into a writable directory between path
// validation and open().
//
// # Asymmetry with Windows
//
// `flags_windows.go` lacks O_NOFOLLOW because the Go standard `syscall`
// package on Windows has no equivalent. Cross-platform callers must NOT
// assume kernel-level symlink protection on Windows; pre-validate paths via
// `filepath.EvalSymlinks` (already done in `security.ResolveLenientPath` /
// `security.PathValidator.GetSafePath`) before reaching any fsperm OpenFile
// site. See `flags_windows.go` for the full Windows contract.
package fsperm

import (
	"os"
	"syscall"
)

// WriteFlags 是應用程式建立 CSV / HTML / log / JSON / PNG 等檔案的標準 OpenFile flag。
// O_NOFOLLOW 阻擋 symlink-based TOCTOU：攻擊者無法在 OutputDir 內預先植入 symlink
// 把寫入導向 /etc/passwd 等敏感檔案。darwin / linux 會以 ELOOP 回報。
//
// Windows 對照：`flags_windows.go` 的同名常數 **不含 O_NOFOLLOW**（標準 syscall
// 無等價 flag），caller 必須在 OpenFile 前完成 `filepath.EvalSymlinks` 邊界檢查。
const WriteFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | syscall.O_NOFOLLOW

// AppendFlags 是 log file 等需 append 行為的 OpenFile flag。
// 同樣含 O_NOFOLLOW — 攻擊者若把 logDir/app.log 換成 symlink 指向 cron 配置檔，
// open 會立即 fail 而非靜默把 log 內容寫到別處。
const AppendFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND | syscall.O_NOFOLLOW

// ReadFlags 是讀取 CSV / 翻譯 / config 檔案的 OpenFile flag。O_NOFOLLOW 阻擋
// 攻擊者以 symlink 騙程式讀取敏感檔案；與 write 端對稱保護。
//
// O_NONBLOCK 防止 read-only open 阻塞在 FIFO / device 等 special file 上：本 app
// 只讀 regular file（config / manifest / CSV），對 regular file O_NONBLOCK 是
// **no-op**，零行為差異；但若驗證流程（如 manifest.ValidateAllEMGFiles 的開檔即存在
// 性檢查）誤碰到 FIFO，少了 O_NONBLOCK 會在沒有 writer 時無限阻塞、把整個載入卡死
// （DoS）。加上後 open 立即返回,呼叫端再以 fstat 拒絕 non-regular file。
const ReadFlags = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// TmpCreateFlags 用於 atomic write 流程中建立 .tmp 中介檔：
// O_EXCL 確保撞名 (後因 crypto/rand 後綴極小機率) 安全 fail 而非靜默覆寫;
// 不含 O_TRUNC 因為 O_EXCL 本身保證檔案是新建的。O_NOFOLLOW 與 WriteFlags 對稱
// 拒絕 symlink-based TOCTOU。
//
// 後 tmp filename 在 caller (csvutil.WriteCSVAtomic) 已加 random suffix —
// O_EXCL 主要 fallback 為 entropy 撞名極小機率;legacy `path + ".tmp"` 殘留不再
// block 新寫入。
const TmpCreateFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_NOFOLLOW
