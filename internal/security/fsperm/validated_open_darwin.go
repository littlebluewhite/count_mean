//go:build darwin

package fsperm

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// openValidated 在 macOS 上加上 O_NOFOLLOW_ANY (macOS 12.0+) flag,kernel 在
// open(2) 階段拒絕路徑「任一 component」為 symlink。比 O_NOFOLLOW (只擋 leaf)
// 更全面,close parent-component symlink 攻擊面。
//
// resolvedPath 已經 EvalSymlinks 過,理論上沒有 symlink,但加 O_NOFOLLOW_ANY 仍
// 必要 — 防 EvalSymlinksWithFallback 與 openat 之間 TOCTOU 縫隙 (攻擊者 swap
// component 為 symlink 後,kernel 在 syscall 階段重新檢查,O_NOFOLLOW_ANY 觸發
// ELOOP)。
//
// hitBase 在 Darwin 不需要 — macOS 沒 RESOLVE_BENEATH 等價 flag,boundary check
// 已在 caller-side matchAnyBase 完成。沿用既有 *os.File 路徑即可。
//
// macOS 11 與更舊 (理論上不在支援範圍 — Go 1.25 require macOS 12+) 沒有
// O_NOFOLLOW_ANY,openat 會回 EINVAL,屆時 fallback 移除 flag 重試。
func openValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	flagsAny := WriteFlags | unix.O_NOFOLLOW_ANY
	f, err := os.OpenFile(resolvedPath, flagsAny, FilePerm) //nolint:gosec,wrapcheck // resolvedPath 已校驗;裸 fd 包 *os.File 給 caller,err 直傳對齊 openReadValidated pattern
	if err == nil {
		return f, nil
	}

	// macOS 11 與更舊不認識 O_NOFOLLOW_ANY,回 EINVAL。降回 WriteFlags (含 O_NOFOLLOW)。
	// resolvedPath 已 EvalSymlinks 過,leaf 不會是 symlink;若 parent 在 TOCTOU 窗口被
	// 動手腳是殘餘風險 — 但 macOS 12 是當前 baseline,實務上不會 hit 此 fallback。
	if errors.Is(err, unix.EINVAL) {
		return os.OpenFile(resolvedPath, WriteFlags, FilePerm) //nolint:gosec,wrapcheck // resolvedPath 已校驗;fallback 與 read 路徑對稱
	}
	return nil, err //nolint:wrapcheck // 直傳 os.OpenFile err 對齊 openReadValidated pattern
}

// openReadValidated 是 openValidated 的 read-side 對稱版本:flags 換成
// ReadFlags (O_RDONLY | O_NOFOLLOW),保留 O_NOFOLLOW_ANY 對 parent-component
// symlink 的 kernel-level rejection;darwin 11 / 更舊 EINVAL fallback 對齊
// 寫盤路徑。
func openReadValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	flagsAny := ReadFlags | unix.O_NOFOLLOW_ANY
	f, err := os.OpenFile(resolvedPath, flagsAny, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;裸 fd 包 *os.File 給 caller,err 直傳對齊 openValidated pattern
	if err == nil {
		return f, nil
	}

	if errors.Is(err, unix.EINVAL) {
		return os.OpenFile(resolvedPath, ReadFlags, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;fallback 與寫盤路徑對稱
	}
	return nil, err //nolint:wrapcheck // 直傳 os.OpenFile err 對齊 openValidated pattern
}
