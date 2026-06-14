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

// TestPaths_RedactsSpaceAndRootLevelPaths 釘住 PHI redact 兩個漏洞(whole-project
// review P1):
//
//  1. 含空白元件的路徑(macOS /Volumes/pCloud Drive/...)先前因 char class 排除
//     whitespace,整段路徑(含 patient 資料夾名)漏進 webview。
//  2. root-level 檔(/tmp/patient_123.csv,無子目錄)先前因 regex 要求至少一層
//     trailing `/` 而完全不匹配,目錄前綴漏出。
//
// 兩者夾在訊息中段(非行首)時 line-fallback 也救不到。修法須在不破壞「多路徑
// 不互相 over-match、basename 與散文保留」前提下補匹配。
func TestPaths_RedactsSpaceAndRootLevelPaths(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			name:  "volumes_path_with_space_component",
			input: "open /Volumes/pCloud Drive/patient_jane/ch1.csv: permission denied",
			mustNotLeak: []string{
				"/Volumes/pCloud Drive",
				"patient_jane",
			},
			mustPreserve: []string{"<redacted-path>", "ch1.csv", "permission denied"},
		},
		{
			name:  "root_level_file_mid_string",
			input: "failed to open /tmp/patient_123.csv here",
			mustNotLeak: []string{
				"/tmp/patient_123.csv",
				"/tmp/",
			},
			mustPreserve: []string{"<redacted-path>", "patient_123.csv", "failed to open", "here"},
		},
		{
			name:  "two_paths_no_overmatch",
			input: "copied /Users/a/My Docs/x.csv and /tmp/y.csv done",
			mustNotLeak: []string{
				"/Users/a/My Docs",
				"/tmp/y",
			},
			mustPreserve: []string{"x.csv", "y.csv", " and ", "done"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky fragment %q 仍在 output:\n%s", leak, got)
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

// TestPaths_RedactsNonAllowlistedAndProtectsRelative 覆蓋「任意絕對路徑」修法的新用例
// (codex R2 要求):非白名單根(NAS、datapool、Applications)、中文路徑、閉引號保留、
// stack 行號 :42 完整保留、相對路徑不被改寫。
func TestPaths_RedactsNonAllowlistedAndProtectsRelative(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			// NAS 非白名單根 + 中文 + 行中嵌入
			name:  "nas_non_allowlist_root_chinese_midstring",
			input: "open /Network/Servers/clinic-nas/patients/患者_X/raw.csv: permission denied",
			mustNotLeak: []string{
				"/Network/Servers",
				"患者_X",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"raw.csv",
				"permission denied",
				"open ",
			},
		},
		{
			// /datapool 非白名單根
			name:  "datapool_non_allowlist_root",
			input: "read /datapool/study2026/subjectA/trial.csv failed",
			mustNotLeak: []string{
				"/datapool/study2026",
				"subjectA",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"trial.csv",
				"failed",
			},
		},
		{
			// /Applications 非白名單根
			name:  "applications_non_allowlist_root",
			input: "open /Applications/data/patient/case.csv: not found",
			mustNotLeak: []string{
				"/Applications/data",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"case.csv",
				"not found",
			},
		},
		{
			// 中文 + 閉引號保留(引號在邊界 group1 被捕獲後回吐)
			name:  "chinese_closed_quote_preserved",
			input: `open "/Network/clinic/患者_Y/raw.csv": 找不到檔案`,
			mustNotLeak: []string{
				"/Network/clinic",
				"患者_Y",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"raw.csv",
				"找不到檔案",
			},
		},
		{
			// stack 行號 :42 完整保留
			name:  "stack_line_number_colon_preserved",
			input: "goroutine:\n\t/Users/x/proj/gui/recover.go:42 +0x1a\n",
			mustNotLeak: []string{
				"/Users/x/proj",
			},
			mustPreserve: []string{
				"<redacted-path>",
				"recover.go:42",
				"+0x1a",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky fragment %q 仍在 output:\n%s", leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 fragment %q 不在 output:\n%s", want, got)
				}
			}
		})
	}

	// 相對路徑不被改寫 — 用獨立斷言鎖定
	t.Run("relative_path_unchanged", func(t *testing.T) {
		relAbs := "internal/x.go:12"
		if got := Paths(relAbs); got != relAbs {
			t.Errorf("相對路徑應保持不變: got %q, want %q", got, relAbs)
		}

		dotRel := "see ./internal/x.go here"
		gotDot := Paths(dotRel)
		if strings.Contains(gotDot, "<redacted-path>") {
			t.Errorf("相對路徑 %q 不應被 redact,got: %q", dotRel, gotDot)
		}
		if !strings.Contains(gotDot, "./internal/x.go") {
			t.Errorf("相對路徑 %q 應完整保留 ./internal/x.go,got: %q", dotRel, gotDot)
		}
	})

	// 閉引號數量驗證 — chinese_closed_quote_preserved 案例引號必須成對保留
	t.Run("closed_quote_count", func(t *testing.T) {
		input := `open "/Network/clinic/患者_Y/raw.csv": 找不到檔案`
		got := Paths(input)
		if count := strings.Count(got, `"`); count != 2 {
			t.Errorf("閉引號應保留 2 個,got %d in: %s", count, got)
		}
		if !strings.Contains(got, `raw.csv"`) {
			t.Errorf("basename 後的閉引號應緊跟 raw.csv,got: %s", got)
		}
	})
}

// TestPaths_RedactsPathAfterEscapedNewline 釘住 codex R1 [P2] 回歸:
// logger.sanitizeMessage 先把 raw \n/\r escape 成字面 `\n`/`\r` 再呼叫 Paths,
// 導致多行 error 第二行的絕對路徑前綴變成字面 `\n`(非空白邊界)而漏脫敏。
// 前導邊界須涵蓋跳脫換行(`\\[nr]`)。下方輸入用 Go raw string,`\n`/`\r` 即字面兩字元。
func TestPaths_RedactsPathAfterEscapedNewline(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			name:         "escaped_lf_then_posix_path",
			input:        `open failed\n/Users/alice/patient/raw.csv: permission denied`,
			mustNotLeak:  []string{"/Users/alice", "patient"},
			mustPreserve: []string{"<redacted-path>", "raw.csv", "permission denied", `\n`},
		},
		{
			name:         "escaped_cr_then_posix_path_chinese",
			input:        `line1\r/var/private/患者_X/data.csv`,
			mustNotLeak:  []string{"/var/private", "患者_X"},
			mustPreserve: []string{"<redacted-path>", "data.csv", `\r`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky %q 仍在 output:\n%s", leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 %q 不在 output:\n%s", want, got)
				}
			}
		})
	}
}

// TestPaths_RedactsAfterArbitrarySeparator 釘住 denylist 邊界(codex R1/R2 揭示
// allowlist 系統性不完整後的根因修法):路徑前接**任何非詞/點/斜線字符**即視為絕對
// 路徑。最高價值案例 = 中文 error message 無空白直接接路徑(zh-TW 工具常見洩漏面)。
func TestPaths_RedactsAfterArbitrarySeparator(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			// 中文緊接路徑(無空白)— allowlist 漏、denylist 抓
			name:         "chinese_then_path",
			input:        "找不到/Users/alice/患者/raw.csv",
			mustNotLeak:  []string{"/Users/alice", "/Users/alice/患者"},
			mustPreserve: []string{"<redacted-path>", "raw.csv", "找不到"},
		},
		{
			name:         "bracket_then_path",
			input:        "paths=[/Users/a/secret.csv]",
			mustNotLeak:  []string{"/Users/a"},
			mustPreserve: []string{"<redacted-path>", "secret.csv"},
		},
		{
			name:         "comma_then_path",
			input:        "a,/Users/b/x.csv done",
			mustNotLeak:  []string{"/Users/b"},
			mustPreserve: []string{"<redacted-path>", "x.csv", "done"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky %q 仍在 output:\n%s", leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 %q 不在 output:\n%s", want, got)
				}
			}
		})
	}
}

// TestPaths_RedactsColonLabeledPath 釘住 codex R2 [P2] 回歸:無空白的冒號標籤
// (`file:/Users/...`、`path:C:\...`)路徑前接 `:`,前導邊界須涵蓋 `:` 才不洩漏。
// 同時守護 `recover.go:42` 的 `:42`(後接數字非 `/`)不被 `:` 邊界誤觸發。
func TestPaths_RedactsColonLabeledPath(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			name:         "colon_label_posix",
			input:        "file:/Users/alice/patient/raw.csv: permission denied",
			mustNotLeak:  []string{"/Users/alice", "patient"},
			mustPreserve: []string{"<redacted-path>", "raw.csv", "permission denied", "file:"},
		},
		{
			name:         "colon_label_windows",
			input:        `path:C:\Users\alice\patient\raw.csv`,
			mustNotLeak:  []string{`C:\Users`, `\alice`},
			mustPreserve: []string{"<redacted-path>", "raw.csv"},
		},
		{
			// 守護:`:` 成為邊界後,recover.go:42 的 :42 仍須完整保留(後接數字非 /)
			name:         "stack_colon_line_number_not_triggered",
			input:        "at \t/Users/x/proj/gui/recover.go:42 +0x1a",
			mustNotLeak:  []string{"/Users/x/proj"},
			mustPreserve: []string{"<redacted-path>", "recover.go:42", "+0x1a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Paths(tc.input)
			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky %q 仍在 output:\n%s", leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 %q 不在 output:\n%s", want, got)
				}
			}
		})
	}
}
