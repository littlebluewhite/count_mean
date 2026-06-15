package fsperm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// tmpRandSuffixLen 為 atomic-write tmp 檔名的 crypto/rand 後綴長度 (bytes,hex 後翻倍)。
// 8 bytes → 16 hex chars,2^64 entropy 足以擋 birthday attack。
const tmpRandSuffixLen = 8

// fsNameMaxBytes 為單一路徑元件 (basename) 的保守 byte 上限。ext4 / APFS / XFS /
// NTFS 皆為 255 bytes。atomic tmp basename = final basename + ".tmp." + 16 hex;
// 即使 final basename 合法 (caller + filename.Sanitize 截到 200 bytes),tmp 疊上
// 隨機後綴後仍可能跨越此限 → ENAMETOOLONG。makeTmpPath 對 tmp basename 做預算
// 截斷,避免「最終檔名合法但 atomic 寫入失敗」—— 長 subject + 長後綴 writer
// (如 normalized PhaseSync,後綴 ~38 bytes) 在 ~196+ byte subject 下會踩到。
const fsNameMaxBytes = 255

// AtomicWriteFile writes path via crypto-tmp → open → sync → atomic-rename,
// invoking write(w) to emit the payload. It owns the entire durable-placement
// protocol; callers only describe their bytes.
//
//   - basePaths empty → legacy os.OpenFile(TmpCreateFlags) + os.Rename +
//     SyncParentDir (today's fallback; byte-identical placement to the
//     pre-extraction adapters).
//   - basePaths set   → path-validated via OpenAtomicWriteValidated; dirfd-anchored
//     (openat2 RESOLVE_NO_SYMLINKS) only where the platform supports it (Linux ≥5.6).
//     On Windows / old kernels the primitive itself degrades to validated-parent +
//     legacy os.Rename (dirfd == fdNone). handle.Commit() fsyncs the parent dir
//     internally on both of its branches.
//
// write MUST flush any buffering it owns before returning; AtomicWriteFile
// fsyncs + closes the file after write returns. On any error after the tmp is
// created, the tmp is aborted/removed and path is left untouched. The
// ErrPathEscapesBase / ErrBasePathsEmpty sentinels from the validated branch are
// preserved through %w.
func AtomicWriteFile(path string, basePaths []string, write func(io.Writer) error) (err error) {
	tmp, err := makeTmpPath(path)
	if err != nil {
		return err
	}

	// Branch on basePaths: non-empty → dirfd-anchored primitive (closes the
	// parent-swap TOCTOU); empty → legacy os.OpenFile (byte-identical to before).
	// Both branches hand a writable tmp *os.File to the shared body below.
	var (
		file   *os.File
		handle *AtomicWriteHandle
	)
	if len(basePaths) > 0 {
		handle, err = OpenAtomicWriteValidated(path, tmp, basePaths)
		if err != nil {
			return fmt.Errorf("建立 tmp 檔案失敗: %w", err)
		}
		file = handle.File()
	} else {
		//nolint:gosec // G304: legacy fallback; tmp lives in the caller-validated parent dir (validated branch goes through OpenAtomicWriteValidated above)
		file, err = os.OpenFile(tmp, TmpCreateFlags, FilePerm)
		if err != nil {
			return fmt.Errorf("建立 tmp 檔案失敗: %w", err)
		}
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		// dirfd path: handle.Abort() unlinks tmp AND closes the dirfd — a bare
		// os.Remove(tmp) would leak the dirfd (security-review invariant). Legacy
		// path has no dirfd, so best-effort os.Remove of the orphan tmp.
		if handle != nil {
			_ = handle.Abort() //nolint:errcheck // best-effort cleanup; closes dirfd + unlinks tmp
		} else {
			_ = os.Remove(tmp) //nolint:errcheck // best-effort cleanup of orphan tmp
		}
	}()

	// The payload owns its own buffering + flush; AtomicWriteFile owns the Sync +
	// Close + commit. write MUST have flushed before it returns.
	if werr := write(file); werr != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return werr
	}

	if err := file.Sync(); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("fsync 失敗: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("關閉 tmp 檔案失敗: %w", err)
	}

	// Commit branches with the open: dirfd path → handle.Commit() (renameat
	// anchored under the same dirfd, fsyncs the parent dir, then closes the dirfd;
	// does NOT re-close the already-closed file); legacy path → os.Rename +
	// SyncParentDir. handle.Commit on its own error leaves the dirfd open for the
	// deferred Abort to clean up.
	if handle != nil {
		if err := handle.Commit(); err != nil {
			return fmt.Errorf("rename tmp → final 失敗: %w", err)
		}
	} else if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename tmp → final 失敗: %w", err)
	} else {
		// Legacy 路徑 rename 成功後補 fsync parent dir — handle.Commit() 路徑
		// 已在 AtomicWriteHandle.Commit 內部覆蓋,不重複。
		_ = SyncParentDir(path) //nolint:errcheck // best-effort dir durability
	}

	committed = true
	return nil
}

// makeTmpPath 為 atomic-write 流程產生不可預測的 tmp 路徑。
//
// 釘住:舊版 tmp = `path + ".tmp"` 完全可預測,攻擊者可在 OpenFile 前 plant
// 同名 file (race) 或單純 DoS final commit。crypto/rand 後綴讓 attacker 無法
// pre-compute,且 O_EXCL 仍保留 (撞名極小機率下安全 fail)。
//
// 失敗回 error,caller 應 surface — 沒有 crypto/rand 的環境屬於異常狀態,
// 不該 silently fall back 到 deterministic 名稱 (那等於 disable 此防護)。
//
// tmp basename = base + suffix 可能跨越 NAME_MAX (見 fsNameMaxBytes):截 base
// 前綴讓 tmp 元件 fit (截的是 base 而非 suffix — 後者是 collision-resistance
// 隨機熵,削了會降低撞名抵抗);final os.Rename 仍用完整 path,對外檔名與
// 目錄不變。用 filepath.Split 而非字串拼接,確保 dir+base==path 不被 clean 改寫。
func makeTmpPath(path string) (string, error) {
	b := make([]byte, tmpRandSuffixLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand 失敗,無法為 tmp 產生 random suffix: %w", err)
	}

	suffix := ".tmp." + hex.EncodeToString(b)
	dir, base := filepath.Split(path)
	if len(base)+len(suffix) > fsNameMaxBytes {
		base = truncateToBytes(base, fsNameMaxBytes-len(suffix))
	}

	return dir + base + suffix, nil
}

// truncateToBytes 把 s 截到最多 maxBytes,且不切斷 multi-byte UTF-8 rune
// (退回最後一個完整 rune 邊界) — 避免 tmp basename 含半個 rune 被 strict
// filesystem (FAT32 reject / APFS 顯示亂碼) 拒絕。
func truncateToBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// maxBytes 落在 s 內部;若切到 multi-byte rune 中段,退到 rune 邊界。
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
