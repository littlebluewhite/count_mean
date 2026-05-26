// Package fsperm — validated OpenFile wrapper。
//
// # 為何需要 OpenWriteValidated
//
// fsperm.WriteFlags / ReadFlags 在 Unix 帶 O_NOFOLLOW、在 Windows 不帶（標準
// syscall 無等價 flag）。Windows 端的 symlink 防護完全依賴 caller-side
// filepath.EvalSymlinks，但 repo 內 11+ 條 OpenFile callsite 多數沒這道前置
// 守門，只有 ResolveLenientPath 與 PathValidator.GetSafePath 有做。
//
// 本檔提供統一 wrapper：caller 一行 OpenWriteValidated(path, basePaths) 取代
// 三步驟 EvalSymlinks → Rel-check → OpenFile，避免「caller 忘做、Windows 上
// 靜默被 reparse point 騙」的反覆撞牆。
//
// # parent-symlink TOCTOU 修法
//
// 原本實作的問題:
//
//	resolvedPath, _ := EvalSymlinks(path)        // step 1: 校驗
//	isPathWithinAnyBase(resolvedPath, basePaths) // step 2: rel check
//	os.OpenFile(path, WriteFlags, FilePerm)      // step 3: 用 unresolved path 開
//
// step 1 與 step 3 之間有 TOCTOU 縫隙:攻擊者 swap symlink 後,kernel 在 step 3
// re-walk path 跟到新 target,validate-vs-use 結果分歧。即便沒 swap,O_NOFOLLOW
// 也只擋 leaf component 為 symlink 的 case;parent component 為 symlink (例如
// allowed/link → outside,寫 allowed/link/out.csv) 不被 O_NOFOLLOW 攔截 — V1
// verify report 已重現。
//
// 修法分兩層:
//  1. 統一改用 resolvedPath 開檔(close validate-vs-use mismatch)
//  2. 平台原子化:
//     - Linux:openat2(RESOLVE_BENEATH) — kernel-level enforcement
//     (見 validated_open_linux.go)
//     - Darwin:O_NOFOLLOW_ANY — 任一 component symlink reject
//     (見 validated_open_darwin.go)
//     - Windows / 其他 Unix:os.OpenFile(resolvedPath, WriteFlags, ...)
//     倚賴 O_NOFOLLOW (Unix) 與 caller-side EvalSymlinks (Windows)
//
// # Caller 遷移計畫
//
// 不在本 範圍內遷移現有 11+ caller —— 那 scope 過大,留 follow-up。新 caller
// 應優先用 OpenWriteValidated;舊 caller 將分批改寫並補測試。csv_handler.WriteCSV
// 已在 切過來。
package fsperm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrBasePathsEmpty 為 caller 傳入 empty basePaths 時的 sentinel — 拒絕 silently
// 退化到「無 base 守門」模式。如果 caller 真的不需要 base 約束（極罕見場景），
// 應直接呼 os.OpenFile + fsperm.WriteFlags 並在 commit 訊息說明理由。
var ErrBasePathsEmpty = errors.New("fsperm.OpenWriteValidated: basePaths 不能為空")

// ErrPathEscapesBase 為 EvalSymlinks 解析後落在所有 basePaths 之外時回傳。
var ErrPathEscapesBase = errors.New("fsperm.OpenWriteValidated: 解析後路徑落在 basePaths 範圍外")

// OpenWriteValidated 開啟 path 寫入，事前做 symlink-aware boundary check 確保
// resolved path 落在至少一個 basePaths 之下。
//
// 流程：
//  1. EvalSymlinks(path) — fallback 到 EvalSymlinks(parent) + base name（path
//     尚未存在的合理情境，如「即將建立的 .tmp 中介檔」）
//  2. EvalSymlinks(basePath) for each basePath — 解析 basePath 本身的 symlink
//     才能正確 Rel 比對（例 macOS /tmp → /private/tmp）
//  3. filepath.Rel(resolvedBase, resolvedPath) — 不為 `..` 開頭即視為「在 base
//     之下」；至少一個 basePath 命中即放行
//  4. openValidated(resolvedPath, hitBase) — 平台 atomic open。詳見 validated_open_<os>.go
//
// 回傳：成功時 *os.File，caller 自行 Close。失敗時 nil + 包裝錯誤。
//
// 改動:openat 使用 resolvedPath 而非原始 path,close validate-vs-use 縫隙。
// 同時委派給 openValidated (平台 dispatch) 使用 Linux openat2(RESOLVE_BENEATH) /
// Darwin O_NOFOLLOW_ANY 等 kernel-level atomic 保證。
func OpenWriteValidated(path string, basePaths []string) (*os.File, error) {
	if len(basePaths) == 0 {
		return nil, ErrBasePathsEmpty
	}

	resolvedPath, err := evalSymlinksWithFallback(path)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenWriteValidated: 無法解析路徑 %s: %w", path, err)
	}

	hitBase, ok := matchAnyBase(resolvedPath, basePaths)
	if !ok {
		return nil, fmt.Errorf("%w: %s 不在 %v 之下", ErrPathEscapesBase, resolvedPath, basePaths)
	}

	f, err := openValidated(resolvedPath, hitBase)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenWriteValidated: OpenFile failed: %w", err)
	}
	return f, nil
}

// OpenReadValidated 為 OpenWriteValidated 的 read-side 對稱 helper:讀檔前
// 做相同 symlink-aware boundary check。新增以填補 Windows 上 read 路徑
// (例如 config.LoadConfig) 缺 symlink 防護的 gap — Unix ReadFlags 帶 O_NOFOLLOW
// 但 Windows 標準 syscall 無等價 flag,過去 Windows 完全靠 caller-side
// EvalSymlinks 防禦,callsite 反覆撞牆。
//
// 流程與 OpenWriteValidated 完全對稱;差異只在最終 openReadValidated dispatch:
//   - Linux:openat2(RESOLVE_BENEATH | RESOLVE_NO_MAGICLINKS) + ReadFlags
//   - Darwin:O_NOFOLLOW_ANY (kernel-level reject parent-component symlink)
//   - Windows / 其他 Unix:os.OpenFile(resolvedPath, ReadFlags) —
//     Windows 殘餘 TOCTOU 由 EvalSymlinks pre-validation 兜底
//     (見 flags_windows.go package doc)
//
// 回傳:成功時 *os.File,caller 自行 Close。
func OpenReadValidated(path string, basePaths []string) (*os.File, error) {
	if len(basePaths) == 0 {
		return nil, ErrBasePathsEmpty
	}

	resolvedPath, err := evalSymlinksWithFallback(path)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenReadValidated: 無法解析路徑 %s: %w", path, err)
	}

	hitBase, ok := matchAnyBase(resolvedPath, basePaths)
	if !ok {
		return nil, fmt.Errorf("%w: %s 不在 %v 之下", ErrPathEscapesBase, resolvedPath, basePaths)
	}

	f, err := openReadValidated(resolvedPath, hitBase)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenReadValidated: OpenFile failed: %w", err)
	}
	return f, nil
}

// evalSymlinksWithFallback 與 security/pathvalidator.go 的 resolveSymlinksWithFallback
// 同語義 — 但本 wrapper 為避免 fsperm 反向 import security，自帶實作 (短，依靠
// stdlib)。若 absPath 不存在則退到 parent；parent 也不行則抵達 root 仍 fail。
//
// 不 export — 將來若有第二條 caller 需要相同 fallback 再考慮提到 perm.go。
//
//nolint:err113 // dynamic errors for caller-facing output
func evalSymlinksWithFallback(absPath string) (string, error) {
	if absPath == "" {
		return "", errors.New("empty path")
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(absPath)
	if parent == absPath {
		return "", fmt.Errorf("路徑根目錄無法解析: %s", absPath)
	}
	resolvedParent, err := evalSymlinksWithFallback(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(absPath)), nil
}

// matchAnyBase 對 resolvedPath 比對每個 base：先 EvalSymlinks(base)
// 解析 base 本身的 symlink，再 filepath.Rel 看是否落在 base 之下。
//
// 回傳:命中的 resolved base path(供 Linux openat2 用作 dirfd anchor)以及命中旗標。
func matchAnyBase(resolvedPath string, basePaths []string) (string, bool) {
	for _, base := range basePaths {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		resolvedBase, err := filepath.EvalSymlinks(base)
		if err != nil {
			// base 不存在 — 嘗試用 cleaned/abs 比對（caller 可能傳「即將建立」的 dir）
			absBase, absErr := filepath.Abs(base)
			if absErr != nil {
				continue
			}
			resolvedBase = filepath.Clean(absBase)
		}
		rel, err := filepath.Rel(resolvedBase, resolvedPath)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return resolvedBase, true
	}
	return "", false
}

// isPathWithinAnyBase 為 matchAnyBase 的舊版 wrapper,保留給未來可能用得到的
// boolean-only caller (目前無外部 caller)。新 code 應直接用 matchAnyBase。
//
//nolint:unused // 保留為 future-proof helper（golangci-lint v2 已將 deadcode 合進 unused）
func isPathWithinAnyBase(resolvedPath string, basePaths []string) bool {
	_, ok := matchAnyBase(resolvedPath, basePaths)
	return ok
}
