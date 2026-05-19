//go:build windows

// Package fsperm — Windows platform notes.
//
// # Platform asymmetry: Windows has no atomic O_NOFOLLOW
//
// The Go standard `syscall` package on Windows does NOT expose an O_NOFOLLOW
// equivalent. `golang.org/x/sys/windows` offers `FILE_FLAG_OPEN_REPARSE_POINT`
// when calling `CreateFile` directly, but that is NOT wired into `os.OpenFile`
// — meaning the flag constants in this file (`WriteFlags` / `AppendFlags` /
// `ReadFlags` / `TmpCreateFlags`) provide **no kernel-level symlink rejection
// on Windows**, in contrast to their Unix siblings in `flags_unix.go`.
//
// # Why we still ship these constants on Windows
//
//   - API symmetry: callers compile the same source on Linux / macOS / Windows
//     without `runtime.GOOS` branches at every OpenFile site.
//   - Defense-in-depth on Windows happens at a layer above:
//     callers MUST pre-validate paths via `filepath.EvalSymlinks` (already
//     done in `internal/security/lenient_path.go:ResolveLenientPath` and
//     `internal/security/pathvalidator.go:PathValidator.GetSafePath`) BEFORE
//     reaching the OpenFile site. The lexical path is rejected if it resolves
//     outside the trusted base directory.
//   - Windows-only attack surface is narrower: creating a reparse point /
//     symlink requires `SeCreateSymbolicLinkPrivilege` (admin or Developer
//     Mode), so an unprivileged attacker who can drop files into OutputDir
//     typically cannot also drop a reparse point. NTFS junction points are
//     more accessible but only redirect within the local filesystem.
//
// # Caller contract (Windows)
//
// Any code path that uses these flag constants on a user-supplied path MUST
// have already run through either:
//
//   - `security.PathValidator.GetSafePath()` — strict input validation +
//     symlink-aware boundary check; OR
//   - `security.ResolveLenientPath()` — manifest-driven path, ditto check;
//     OR
//   - an explicit `filepath.EvalSymlinks()` step that confirms the final
//     resolved path stays within an allow-listed base directory.
//
// Direct `os.OpenFile(userInput, fsperm.WriteFlags, ...)` on Windows without
// one of the above guards is a known TOCTOU hole — the kernel will silently
// follow reparse points / junctions. Audit every new callsite.
//
// # Future work
//
// To get kernel-level rejection on Windows (matching Unix O_NOFOLLOW):
// switch to `golang.org/x/sys/windows.CreateFile` with
// `FILE_FLAG_OPEN_REPARSE_POINT`, wrap the returned handle into `*os.File`
// via `os.NewFile`, and replace the `os.OpenFile(..., fsperm.WriteFlags, ...)`
// pattern everywhere. That refactor is tracked separately (TODO: Windows CI
// needed first — `flags_windows_contract_test.go` is ready and waiting for a
// Windows runner in `.github/workflows/`).
package fsperm

import "os"

// WriteFlags is the standard OpenFile flag for application-created files on
// Windows (CSV / HTML / log / JSON / PNG). Unlike `flags_unix.go`, this does
// NOT contain O_NOFOLLOW — Windows standard `syscall` has no atomic
// equivalent. Caller-side symlink validation via `filepath.EvalSymlinks` is
// REQUIRED for any user-supplied path; see the package doc above for the
// caller contract.
const WriteFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC

// AppendFlags is the OpenFile flag for append-mode files (log files) on
// Windows. No O_NOFOLLOW — see WriteFlags doc and the package doc for the
// required pre-validation step.
const AppendFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND

// ReadFlags is the OpenFile flag for reading files on Windows. No
// O_NOFOLLOW — see WriteFlags doc and the package doc for the required
// pre-validation step.
const ReadFlags = os.O_RDONLY

// TmpCreateFlags is the OpenFile flag for atomic-write intermediate files
// (`.tmp`) on Windows. `O_EXCL` is preserved so a stale tmp collision (極小
// 機率,因 後 tmp 名含 crypto/rand 後綴) 被偵測為錯誤而非靜默覆寫。
// No O_NOFOLLOW — same caveat as WriteFlags.
//
// # 已知限制:NTFS junction / reparse point
//
// 本常數 **不防 NTFS junction / reparse point**。若 tmp 寫入路徑的某個 parent
// 是 NTFS junction (junction 不需 admin 即可建立,unprivileged attacker 可植入),
// kernel 會跟著解析到 junction 目標,可能寫到 attacker 控制的目錄。
//
// 為何沒在 SDK 層擋:Windows 上要拒絕 reparse point 需用 `FILE_FLAG_OPEN_REPARSE_POINT`
// 旗標,但這只能透過 `golang.org/x/sys/windows.CreateFile` 直接呼叫,然後用
// `os.NewFile` 包成 `*os.File`。Go `os.OpenFile` 本身 **不接受** 此 flag,所以
// 不能單純 OR 進 TmpCreateFlags;要切換實作需大規模重構整條 OpenFile chain
// (csv_handler / safe_writer / logging 等多處 caller)。
//
// 目前 mitigation 是 caller-side `filepath.EvalSymlinks` (在 PathValidator /
// ResolveLenientPath / OpenWriteValidated 內已實作),解析 reparse point 後比對
// allowed base — Windows 上這是 only line of defense。
//
// **追蹤計畫**:見本檔 package doc 的 "Future work" 段;Windows runner 上的
// `flags_windows_contract_test.go` 已準備好驗證 kernel-level guard 何時 land。
const TmpCreateFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
