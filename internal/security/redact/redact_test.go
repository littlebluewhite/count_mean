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

	// no-boundary 取捨:單段相對參照(無 trailing-slash 目錄段)仍保留。
	t.Run("relative_single_seg_preserved", func(t *testing.T) {
		relRef := "internal/x.go:12" // 單一 `/`、`x.go` 後無 trailing-slash → 不匹配
		if got := Paths(relRef); got != relRef {
			t.Errorf("單段相對參照應保持不變: got %q, want %q", got, relRef)
		}
	})

	// no-boundary 取捨:多段相對路徑被過度脫敏(刻意 — 寧可過度脫敏絕不洩漏 PHI)。
	t.Run("multi_seg_relative_over_redacted", func(t *testing.T) {
		got := Paths("see ./internal/x.go here") // `/internal/` 為 trailing-slash 目錄段 → 脫敏
		if !strings.Contains(got, "<redacted-path>") {
			t.Errorf("多段相對路徑應被過度脫敏(no-boundary 設計): got %q", got)
		}
		if !strings.Contains(got, "x.go") {
			t.Errorf("basename 應保留: got %q", got)
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
// no-boundary 設計下,跳脫換行前綴的路徑仍直接脫敏。下方輸入用 Go raw string,`\n`/`\r` 即字面兩字元。
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

// TestPaths_RedactsURLPaths 鎖定 no-boundary 行為:file:// 本地路徑被脫敏(含可選
// host);http:// 的 host+path 也一併過度脫敏(安全方向 — 非本地 PHI 但脫敏無妨)。
// 核心保證:任何 URL 內嵌的絕對路徑都不洩漏(codex R3/R5 的 file-URL gap 由 no-boundary
// 根因消除)。
func TestPaths_RedactsURLPaths(t *testing.T) {
	t.Run("file_url_hostless_redacted", func(t *testing.T) {
		got := Paths("open file:///Users/alice/patient/raw.csv failed")
		for _, leak := range []string{"/Users/alice", "patient"} {
			if strings.Contains(got, leak) {
				t.Errorf("file:// 本地路徑洩漏 %q:\n%s", leak, got)
			}
		}
		for _, want := range []string{"<redacted-path>", "raw.csv"} {
			if !strings.Contains(got, want) {
				t.Errorf("必要 %q 不在 output:\n%s", want, got)
			}
		}
	})

	t.Run("file_url_with_host_redacted", func(t *testing.T) {
		got := Paths("open file://localhost/Users/alice/patient/raw.csv failed")
		for _, leak := range []string{"/Users/alice", "patient"} {
			if strings.Contains(got, leak) {
				t.Errorf("file://host 本地路徑洩漏 %q:\n%s", leak, got)
			}
		}
		if !strings.Contains(got, "<redacted-path>") || !strings.Contains(got, "raw.csv") {
			t.Errorf("必要 fragment(<redacted-path>/raw.csv)不在 output:\n%s", got)
		}
	})

	t.Run("http_url_path_over_redacted", func(t *testing.T) {
		// no-boundary 取捨:http URL 的 host+path 一併脫敏(安全方向,非本地 PHI 但無妨)。
		got := Paths("fetch http://example.com/Users/docs/page failed")
		if strings.Contains(got, "/Users/docs") {
			t.Errorf("URL path 應被脫敏: %q", got)
		}
		if !strings.Contains(got, "<redacted-path>") {
			t.Errorf("應含 redact 標誌: %q", got)
		}
	})
}

// TestPaths_RedactsAfterArbitrarySeparator 鎖定 no-boundary 行為:路徑無論前接何種字符
// (冒號標籤、bracket、comma、中文…)都直接脫敏。中文 error message 接絕對路徑是 zh-TW
// 工具常見洩漏面;中文相對路徑則被一併過度脫敏(no-boundary 取捨,寧可過度脫敏絕不洩漏)。
func TestPaths_RedactsAfterArbitrarySeparator(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			// 中文 error + 冒號(zh-TW `:%v` 慣例)接絕對路徑 → 仍脫敏(`:` 邊界)
			name:         "chinese_colon_then_abs_path",
			input:        "找不到檔案:/Users/alice/患者/raw.csv",
			mustNotLeak:  []string{"/Users/alice", "患者"},
			mustPreserve: []string{"<redacted-path>", "raw.csv", "找不到檔案"},
		},
		{
			// no-boundary 取捨:中文相對路徑也被過度脫敏(刻意 — 寧可過度脫敏絕不洩漏)
			name:         "chinese_relative_path_over_redacted",
			input:        "讀取 資料/輸入/raw.csv",
			mustNotLeak:  []string{"資料/輸入", "輸入"},
			mustPreserve: []string{"<redacted-path>", "raw.csv"},
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

// TestPaths_RedactsDottedDirectorySegment 鎖定「目錄末段帶點(看似副檔名)」不得讓
// 上游 PHI 目錄漏出 — no-boundary 設計不靠副檔名啟發法判斷目錄/檔案,故反例
// `/Volumes/clinic/patient.v1`(patient.v1 是目錄)的 PHI 父段 `clinic` 仍被脫敏。
// 含 `%v` bracket 形式(`[/.../patient.v1]`)與裸 underscore 目錄末段(patient_Smith)。
// 同時釘住 Go stack frame `recover.go:42 +0x...` 完整保留(`:42` 後接數字非 `/`,
// 不可當路徑脫敏)。
func TestPaths_RedactsDottedDirectorySegment(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			// dotted 目錄末段:patient.v1 像有副檔名實為目錄;父段 clinic 必脫敏
			name:         "dotted_dir_segment",
			input:        "/Volumes/clinic/patient.v1",
			mustNotLeak:  []string{"/Volumes/clinic", "/Volumes/"},
			mustPreserve: []string{"<redacted-path>", "patient.v1"},
		},
		{
			// %v bracket 形式(fmt.Errorf("...%v", path))— bracket 在邊界外保留
			name:         "dotted_dir_bracket_v_form",
			input:        "[/Volumes/clinic/patient.v1]",
			mustNotLeak:  []string{"/Volumes/clinic"},
			mustPreserve: []string{"<redacted-path>", "patient.v1", "[", "]"},
		},
		{
			// 裸目錄末段(無副檔名):patient_Smith 為末段保留,父段 clinic 脫敏
			name:         "bare_dir_trailing_segment",
			input:        "/Volumes/clinic/patient_Smith",
			mustNotLeak:  []string{"/Volumes/clinic", "/Volumes/"},
			mustPreserve: []string{"<redacted-path>", "patient_Smith"},
		},
		{
			// Go stack frame::42 行號 + +0x offset 必完整保留(不可被當路徑吃掉)
			name:         "go_stack_frame_line_offset_preserved",
			input:        "recover.go:42 +0x1a2b",
			mustNotLeak:  []string{"<redacted-path>"}, // 無絕對路徑 → 完全不脫敏
			mustPreserve: []string{"recover.go:42", "+0x1a2b"},
		},
		{
			// basename 可保留:/x/y/missing.csv → 父段 /x/y/ 脫敏、basename 留
			name:         "basename_preserved_root_file",
			input:        "/x/y/missing.csv",
			mustNotLeak:  []string{"/x/y", "/x/"},
			mustPreserve: []string{"<redacted-path>", "missing.csv"},
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

// TestPaths_RedactsColonLabeledPath 鎖定無空白冒號標籤(`file:/Users/...`、
// `path:C:\...`)路徑被脫敏(no-boundary 下直接匹配;`\b` 防 `file:/` 的 `e:` 被誤當盤符)。
// 同時守護 `recover.go:42` 的 `:42`(後接數字非 `/`)不被誤觸發。
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
