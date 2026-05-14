//go:build windows

package fsperm

import "os"

// WriteFlags 是 Windows 上的標準 OpenFile flag。Windows 不暴露 O_NOFOLLOW；
// 建立 symlink 需 SeCreateSymbolicLinkPrivilege（admin），unix 的 symlink-TOCTOU
// 攻擊面大幅縮減。若未來需 reparse-point 拒絕，改用
// golang.org/x/sys/windows.OpenFile 配 FILE_FLAG_OPEN_REPARSE_POINT。
const WriteFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC

// AppendFlags 是 Windows 上 log file 的 OpenFile flag。無 O_NOFOLLOW，理由同上。
const AppendFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND

// ReadFlags 是 Windows 上讀檔的 OpenFile flag。無 O_NOFOLLOW，理由同上。
const ReadFlags = os.O_RDONLY

// TmpCreateFlags 用於 atomic write 流程；Windows 不含 O_NOFOLLOW（同 WriteFlags 設計理由）。
const TmpCreateFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
