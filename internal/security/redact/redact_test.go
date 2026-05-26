// Package redact 的測試守護:
//
//  1. Paths 行為與 gui/recover.go::redactPathsInStack 完全對齊(等於是把舊測試
//     在新位址重新跑一遍 — 守 migration 等價)。
//  2. RedactForMessage 對 error 文字做相同 redact 處理,並對 nil error 回空字串。
//
// 把 redactPathsInStack 從 gui/recover.go 抽到 internal/security/redact 作為
// process-wide 共用 helper。新位址要先有 test 才落實 helper(TDD),
// 避免「搬一搬就少抓某個 pattern」silent regression。
package redact

import (
	"errors"
	"strings"
	"testing"
)

// TestPaths_CoversExtendedPlatformRoots 是 主守的對齊測試:
// migration 後 Paths 必須涵蓋 已加入的 mount / drive-letter / UNC pattern。
func TestPaths_CoversExtendedPlatformRoots(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			name:  "macos_volumes_pcloud_mount",
			input: "open /Volumes/EMG_Backup/patient_2026_05_18/ch1.csv: permission denied",
			mustNotLeak: []string{
				"/Volumes/EMG_Backup",
				"/Volumes/",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"ch1.csv",
				"permission denied",
			},
		},
		{
			name:  "linux_mnt_nas_mount",
			input: "open /mnt/nas/foo/bar.csv: i/o error",
			mustNotLeak: []string{
				"/mnt/nas",
				"/mnt/",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"bar.csv",
				"i/o error",
			},
		},
		{
			name:  "windows_drive_letter_backslash",
			input: `open C:\Users\alice\AppData\Local\Temp\x.tmp: access denied`,
			mustNotLeak: []string{
				`C:\Users`,
				`C:\`,
			},
			mustPreserve: []string{
				"<redacted-path>",
				"x.tmp",
				"access denied",
			},
		},
		{
			name:  "windows_unc_path",
			input: `open \\fileserver\share\report.csv: cannot connect`,
			mustNotLeak: []string{
				`\\fileserver`,
				`\\fileserver\share`,
			},
			mustPreserve: []string{
				"<redacted-path>",
				"report.csv",
				"cannot connect",
			},
		},
		{
			name:  "mid_string_posix_path",
			input: "open /Users/alice/foo/bar.txt: not found",
			mustNotLeak: []string{
				"/Users/alice",
				"/Users/",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"bar.txt",
				"open ",
				"not found",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky prefix %q 仍在 output:\n%s", leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 fragment %q 不在 output:\n%s", want, got)
				}
			}
		})
	}
}

// TestPaths_HandlesSystemPathVariants 守住 stack-trace 整段的 line-by-line redact 行為,
// 與舊 gui/recover_test.go::TestRedactPathsInStack_HandlesSystemPathVariants 對齊。
func TestPaths_HandlesSystemPathVariants(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		mustRedact []string
	}{
		{
			name: "macos_users_home",
			input: "goroutine 1 [running]:\n" +
				"main.foo()\n" +
				"\t/Users/wilson/IdeaProjects/count_mean/gui/recover.go:42 +0x1a\n",
			mustRedact: []string{"/Users/wilson/IdeaProjects/"},
		},
		{
			name: "linux_home",
			input: "goroutine 1 [running]:\n" +
				"\t/home/runner/work/count_mean/gui/recover.go:42 +0x1a\n",
			mustRedact: []string{"/home/runner/work/"},
		},
		{
			name: "macos_var_folders_temp",
			input: "goroutine 1 [running]:\n" +
				"\t/var/folders/abc/xyz/count_mean/gui/recover.go:42 +0x1a\n",
			mustRedact: []string{"/var/folders/abc/xyz/"},
		},
		{
			name: "private_var_macos",
			input: "goroutine 1 [running]:\n" +
				"\t/private/var/folders/abc/T/count_mean/gui/recover.go:42 +0x1a\n",
			mustRedact: []string{"/private/var/folders/"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, p := range tc.mustRedact {
				if strings.Contains(got, p) {
					t.Errorf("redact 失敗,path %q 仍在 output:\n%s", p, got)
				}
			}
			if !strings.Contains(got, "<redacted-path>") {
				t.Errorf("output 應含 redact 標誌:\n%s", got)
			}
			if !strings.Contains(got, "recover.go") {
				t.Errorf("output 應保留 basename:\n%s", got)
			}
		})
	}
}

// TestRedactForMessage_NilReturnsEmpty 守 contract:nil error → "" 空字串,
// caller 可安心把回傳值塞進 result.Message 而不必先做 nil-check。
func TestRedactForMessage_NilReturnsEmpty(t *testing.T) {
	if got := RedactForMessage(nil); got != "" {
		t.Errorf("RedactForMessage(nil) = %q, want \"\"", got)
	}
}

// TestRedactForMessage_StripsAbsolutePaths 守 主目標:handler 把 err.Error()
// 塞進 user-facing message 時,path PII 必須先過 redact。
func TestRedactForMessage_StripsAbsolutePaths(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		mustNotLeak []string
	}{
		{
			name: "posix_users_home_in_error",
			err:  errors.New("open /Users/alice/patient/case_2026_05_18/emg_raw.csv: permission denied"),
			mustNotLeak: []string{
				"/Users/alice",
				"/Users/",
			},
		},
		{
			name: "linux_home_in_error",
			err:  errors.New("failed to read /home/bob/data/recording_001.csv"),
			mustNotLeak: []string{
				"/home/bob",
				"/home/",
			},
		},
		{
			name: "windows_drive_letter_in_error",
			err:  errors.New(`failed to open C:\Users\carol\Documents\emg.csv: not found`),
			mustNotLeak: []string{
				`C:\Users`,
			},
		},
		{
			name: "volumes_pcloud_in_error",
			err:  errors.New("open /Volumes/pCloud/patient_xx/ch1.csv: permission denied"),
			mustNotLeak: []string{
				"/Volumes/pCloud",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactForMessage(tc.err)
			if got == "" {
				t.Fatal("RedactForMessage 不應對非 nil error 回空字串")
			}
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("leaky path %q 仍出現在 redacted message: %q",
						leak, got)
				}
			}
			if !strings.Contains(got, "<redacted-path>") {
				t.Errorf("redacted message 應含 <redacted-path> 標誌: %q", got)
			}
		})
	}
}

// TestRedactForMessage_PreservesNonPathParts 守:non-path 部分必須保留,
// 才能讓 user 看到「實際發生什麼錯誤」(permission denied / not found 等)。
func TestRedactForMessage_PreservesNonPathParts(t *testing.T) {
	err := errors.New("open /Users/alice/foo.csv: permission denied")
	got := RedactForMessage(err)
	for _, want := range []string{"permission denied", "foo.csv", "<redacted-path>"} {
		if !strings.Contains(got, want) {
			t.Errorf("redacted message 應保留 %q,got: %q", want, got)
		}
	}
}
