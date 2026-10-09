package fsperm_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/security/fsperm"
	"count_mean/internal/security/redact"
)

// TestExportedErrors_NoDirSegmentAfterSinkRedact 釘住 fsperm 錯誤字串的 PHI 契約:webview
// sink 對錯誤文字跑 redact.Paths(保留末段,ADR-0036 Decision 5)之後,base 與 target 的
// 目錄段一個都不可留存,只允許 target 檔名。目錄名本身就是 PHI(病患資料夾),sink 端保留
// 末段,只能靠 fsperm 在 source 端脫敏 —— 各平台都要成立(Linux openat2 / atomic-write
// 分支在內),且 errors.Is 的 sentinel / errno 仍須命中。
//
// 不設 build tag:macOS 與 Linux 各走自己的平台分支;Windows 跳過 symlink 列。
func TestExportedErrors_NoDirSegmentAfterSinkRedact(t *testing.T) {
	t.Parallel()

	// 先解析 TempDir 本身的 symlink(macOS /var → /private/var),讓 matchAnyBase 在
	// 各平台都命中,錯誤才會走到平台 open 分支而非提早 ErrPathEscapesBase。
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const (
		baseName    = "PatientAlice_PHI_base"
		subName     = "SubjSecret_PHI_sub"
		missingName = "SubjMissing_PHI_sub"
		ghostName   = "PatientGhost_PHI_base"
		outsideName = "OutsideBob_PHI_dir"
	)
	phiNames := []string{baseName, subName, missingName, ghostName, outsideName}

	base := filepath.Join(root, baseName)
	sub := filepath.Join(base, subName)
	missingSub := filepath.Join(base, missingName) // 不建立
	ghostBase := filepath.Join(root, ghostName)    // 不建立:base 本身不存在
	outside := filepath.Join(root, outsideName)
	escape := filepath.Join(sub, "escape") // symlink → outside
	bases := []string{base}

	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.NoError(t, os.Mkdir(outside, 0o750))
	canSymlink := runtime.GOOS != "windows"
	if canSymlink {
		require.NoError(t, os.Symlink(outside, escape))
	}

	cases := []struct {
		name    string
		call    func(t *testing.T) error
		wantIs  error  // nil = 不要求 sentinel(錯誤型別因平台而異)
		wantMsg string // 非空 = 錯誤須含此子字串,證明走到預期分支(無 sentinel 的列用)
		symlink bool
	}{
		{
			name: "OpenReadValidated/missing file in subdir",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenReadValidated(filepath.Join(sub, "emg.csv"), bases))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenReadValidated/missing base dir",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenReadValidated(filepath.Join(ghostBase, "emg.csv"), []string{ghostBase}))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenReadValidated/symlink escape",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenReadValidated(filepath.Join(escape, "emg.csv"), bases))
			},
			wantIs:  fsperm.ErrPathEscapesBase,
			symlink: true,
		},
		{
			name: "OpenWriteValidated/missing subdir",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenWriteValidated(filepath.Join(missingSub, "out.csv"), bases))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenWriteValidated/missing base dir",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenWriteValidated(filepath.Join(ghostBase, "out.csv"), []string{ghostBase}))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenWriteValidated/symlink escape",
			call: func(*testing.T) error {
				return closeOnly(fsperm.OpenWriteValidated(filepath.Join(escape, "out.csv"), bases))
			},
			wantIs:  fsperm.ErrPathEscapesBase,
			symlink: true,
		},
		{
			name: "OpenAtomicWriteValidated/missing subdir",
			call: func(*testing.T) error {
				target := filepath.Join(missingSub, "out.csv")
				return abortOnly(fsperm.OpenAtomicWriteValidated(target, target+".tmp", bases))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenAtomicWriteValidated/missing base dir",
			call: func(*testing.T) error {
				target := filepath.Join(ghostBase, "out.csv")
				return abortOnly(fsperm.OpenAtomicWriteValidated(target, target+".tmp", []string{ghostBase}))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "OpenAtomicWriteValidated/tmp and target in different dirs",
			call: func(*testing.T) error {
				return abortOnly(fsperm.OpenAtomicWriteValidated(
					filepath.Join(base, "out.csv"), filepath.Join(sub, "out.csv.tmp"), bases))
			},
			wantMsg: "必須同目錄",
		},
		{
			name: "OpenAtomicWriteValidated/symlink escape",
			call: func(*testing.T) error {
				target := filepath.Join(escape, "out.csv")
				return abortOnly(fsperm.OpenAtomicWriteValidated(target, target+".tmp", bases))
			},
			wantIs:  fsperm.ErrPathEscapesBase,
			symlink: true,
		},
		{
			name: "AtomicWriteFile/missing subdir",
			call: func(*testing.T) error {
				return fsperm.AtomicWriteFile(filepath.Join(missingSub, "out.csv"), bases,
					func(io.Writer) error { return nil })
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "Commit/target is a non-empty directory",
			call: func(t *testing.T) error {
				target := filepath.Join(sub, "commit_target")
				require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o750))
				h, err := fsperm.OpenAtomicWriteValidated(target, target+".tmp", bases)
				require.NoError(t, err)
				t.Cleanup(func() { _ = h.Abort() })
				require.NoError(t, h.File().Close())

				return h.Commit()
			},
			// dirfd 路徑 "renameat(…)"、fallback 路徑 "rename tmp → target" 共用此前綴。
			wantMsg: "AtomicWriteHandle.Commit: rename",
		},
		{
			name: "Abort/tmp already removed",
			call: func(t *testing.T) error {
				target := filepath.Join(sub, "abort_target.csv")
				h, err := fsperm.OpenAtomicWriteValidated(target, target+".tmp", bases)
				require.NoError(t, err)
				require.NoError(t, h.File().Close())
				require.NoError(t, os.Remove(target+".tmp"))

				return h.Abort()
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "SyncParentDir/missing parent",
			call: func(*testing.T) error {
				return fsperm.SyncParentDir(filepath.Join(missingSub, "out.csv"))
			},
			wantIs: fs.ErrNotExist,
		},
		{
			name: "EvalSymlinksWithFallback/depth cap",
			call: func(*testing.T) error {
				// missingSub/a/b 三層不存在,cap=3 在 parent-walk 抵達 base 時觸發,訊息帶 base。
				_, err := fsperm.EvalSymlinksWithFallback(filepath.Join(missingSub, "a", "b"), 3)
				return err
			},
			wantMsg: "層數超過上限",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.symlink && !canSymlink {
				t.Skip("symlink 建立在 Windows 需特權,跳過")
			}

			err := tc.call(t)
			require.Error(t, err)

			out := redact.Paths(err.Error())
			for _, name := range phiNames {
				assert.NotContains(t, out, name, "sink redact 後不可留存目錄段;原文=%q", err.Error())
			}
			assert.NotContains(t, out, root, "sink redact 後不可留存絕對路徑")
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs, "改寫訊息後 errors.Is 仍須命中")
			}
			if tc.wantMsg != "" {
				assert.ErrorContains(t, err, tc.wantMsg, "應走到預期的失敗分支")
			}
		})
	}
}

// closeOnly 丟棄(意外)成功開出的 *os.File,只回 error。
func closeOnly(f *os.File, err error) error {
	if f != nil {
		_ = f.Close()
	}
	return err
}

// abortOnly 丟棄(意外)成功開出的 handle(Abort 清 tmp + dirfd),只回 error。
func abortOnly(h *fsperm.AtomicWriteHandle, err error) error {
	if h != nil {
		_ = h.File().Close()
		_ = h.Abort()
	}
	return err
}
