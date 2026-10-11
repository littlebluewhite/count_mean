//go:build darwin

package fsperm

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// openReadValidated 在 macOS 上以 ReadFlags (O_RDONLY | O_NOFOLLOW) 加 O_NOFOLLOW_ANY
// (macOS 12.0+) 開啟,kernel 在 open(2) 階段拒絕路徑「任一 component」為 symlink —
// 比 O_NOFOLLOW (只擋 leaf) 更全面,close parent-component symlink 攻擊面。
//
// resolvedPath 已經 EvalSymlinks 過,理論上沒有 symlink,但 O_NOFOLLOW_ANY 仍必要 —
// 防 EvalSymlinksWithFallback 與 open 之間的 TOCTOU swap (kernel 在 syscall 階段
// 重新檢查,觸發 ELOOP)。
//
// hitBase 在 Darwin 不需要 — macOS 沒 RESOLVE_BENEATH 等價 flag,boundary check
// 已在 caller-side matchAnyBase 完成。
//
// macOS 11 與更舊 (Go 1.25 require macOS 12+) 不認識 O_NOFOLLOW_ANY,open 回 EINVAL,
// 屆時降回 ReadFlags 重試。
func openReadValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	flagsAny := ReadFlags | unix.O_NOFOLLOW_ANY
	f, err := os.OpenFile(resolvedPath, flagsAny, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;err 直傳,由 OpenReadValidated 包裝
	if err == nil {
		return f, nil
	}

	if errors.Is(err, unix.EINVAL) {
		return os.OpenFile(resolvedPath, ReadFlags, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;err 直傳,由 OpenReadValidated 包裝
	}
	return nil, err //nolint:wrapcheck // 直傳 os.OpenFile err,由 OpenReadValidated 包裝
}
