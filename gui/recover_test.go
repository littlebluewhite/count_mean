package gui

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"count_mean/internal/logging"
	"count_mean/internal/models"
)

func TestRecoverHandlerPanic_ConvertsPanicToError(t *testing.T) {
	var err error
	func() {
		defer recoverHandlerPanic("TestHandler", nil, &err)
		panic("simulated nil pointer in test")
	}()

	if err == nil {
		t.Fatal("expected error after panic recovery, got nil")
	}
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("expected ErrInternalPanic, got %v", err)
	}
}

func TestRecoverHandlerPanic_NoPanic_LeavesErrUnchanged(t *testing.T) {
	originalErr := errors.New("pre-existing error")
	err := originalErr
	func() {
		defer recoverHandlerPanic("TestHandler", nil, &err)
		// no panic
	}()

	if err != originalErr {
		t.Errorf("expected err unchanged when no panic, got %v", err)
	}
}

// 當 caller 已在 panic 前塞 non-nil err 時,panic 必須*無條件覆寫*為
// ErrInternalPanic wrap — panic 屬於不可預期內部錯誤,優先級高於 caller 預期 err,
// 使用者看到 ErrInternalPanic 比看到 boundary validation err 更重要(前者代表
// 程式 bug 需要 escalate)。Returned err 不可含 caller 原始 err 字面(PII leak);
// audit trail 改由內部 Error log 保留(由 OriginalErrRedactedInInternalLog 守)。
func TestRecoverHandlerPanic_PanicOverwritesExistingErr(t *testing.T) {
	prePanicErr := errors.New("ok-path validation failed")
	err := prePanicErr

	func() {
		defer recoverHandlerPanic("TestHandler", nil, &err)
		// caller 已塞 ok-path err 但隨後 panic 發生
		panic("post-ok-path panic")
	}()

	// 應該被 ErrInternalPanic 覆寫
	if !errors.Is(err, ErrInternalPanic) {
		t.Fatalf("panic 後 err 應被 ErrInternalPanic 覆寫,got %v", err)
	}

	// wrap 訊息應含 handler 名稱 + panic_uuid 識別碼
	if !strings.Contains(err.Error(), "handler=TestHandler") {
		t.Errorf("wrap 訊息應含 handler 名稱,got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "panic_uuid=") {
		t.Errorf("wrap 訊息應含 panic_uuid,got %q", err.Error())
	}

	// returned err 不應再含 caller 原始 err 字面 — 那是 PII 漏點。
	if strings.Contains(err.Error(), "ok-path validation failed") {
		t.Errorf("returned err 不應含 caller 原始 err 字面(改由內部 log 保留),got %q",
			err.Error())
	}

	// 訊息不應洩漏 stack 或 path
	if strings.Contains(err.Error(), "/Users/") ||
		strings.Contains(err.Error(), "recover.go:") {
		t.Errorf("wrap 訊息不應洩漏 stack/path: %q", err.Error())
	}
}

// 當 caller 已塞含 filesystem path 的 ok-path err 時,panic 後 returned err
// **絕不可**把該 path 一起送到 webview(會洩漏病患資料路徑等 PII)。
func TestRecoverHandlerPanic_NoPIIInReturnedErr(t *testing.T) {
	// 模擬 caller named return err 已塞含真實 path 的錯誤訊息
	pathBearingErrs := []error{
		errors.New("open /Users/alice/patient/case_2026_05_18/emg_raw.csv: permission denied"),
		errors.New("failed to read /home/bob/data/recording_001.csv"),
		errors.New(`failed to open C:\Users\carol\Documents\emg.csv: not found`),
	}

	for _, pathErr := range pathBearingErrs {
		t.Run(pathErr.Error()[:20], func(t *testing.T) {
			err := pathErr

			func() {
				defer recoverHandlerPanic("LoadEMG", nil, &err)
				panic("post-validation panic")
			}()

			// 應被 ErrInternalPanic 覆寫且含 panic_uuid
			if !errors.Is(err, ErrInternalPanic) {
				t.Fatalf("panic 後 err 應被 ErrInternalPanic 覆寫,got %v", err)
			}
			if !strings.Contains(err.Error(), "panic_uuid=") {
				t.Errorf("returned err 應含 panic_uuid 供 oncall 對應,got %q", err.Error())
			}

			// returned err 不能含 absolute path PII。check 各平台典型 prefix。
			leakyPrefixes := []string{
				"/Users/",
				"/home/",
				`C:\Users\`,
				"/var/folders",
				"/private/",
			}
			for _, prefix := range leakyPrefixes {
				if strings.Contains(err.Error(), prefix) {
					t.Errorf("returned err leaks path prefix %q:\n%s",
						prefix, err.Error())
				}
			}

			// 連 basename 都不該漏出去(caller 的 err 字面 = `... emg_raw.csv ...`
			// 也是 user-controlled 字串,可能洩漏 patient ID 等命名線索)
			for _, fragment := range []string{
				"emg_raw.csv",
				"recording_001.csv",
				"case_2026_05_18",
				"patient",
			} {
				if strings.Contains(err.Error(), fragment) {
					t.Errorf("returned err 不應含 caller err 字面 fragment %q,got %q",
						fragment, err.Error())
				}
			}
		})
	}
}

// caller 的原始 err 字面不會送到前端(由 NoPIIInReturnedErr 守),但 audit trail
// 不可丟失 — 改由內部 Error log 的 `original_err` context field 保留,寫進 log
// 前必須先經 redactPathsInStack 過濾。
func TestRecoverHandlerPanic_OriginalErrRedactedInInternalLog(t *testing.T) {
	var buf bytes.Buffer
	testLogger := logging.NewLogger(logging.LevelInfo, &buf, false)

	originalErr := errors.New(
		"open /Users/alice/patient/case_2026_05_18/emg_raw.csv: permission denied",
	)
	err := originalErr

	func() {
		defer recoverHandlerPanic("LoadEMG", testLogger, &err)
		panic("post-validation panic")
	}()

	logOutput := buf.String()

	// internal log 應保留 audit context — 至少含 panic_uuid + handler。
	if !strings.Contains(logOutput, "panic_uuid=") {
		t.Errorf("internal Error log 應含 panic_uuid:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "handler=LoadEMG") {
		t.Errorf("internal Error log 應含 handler 名稱:\n%s", logOutput)
	}

	// 應保留 redacted original err 的 audit trail — 至少非 path 部分必須在 log 內。
	// redactPathsInStack 對 `/Users/alice/patient/case_2026_05_18/` 會替換成
	// `<redacted-path>/`,但保留 trailing basename + 後綴訊息(`emg_raw.csv:
	// permission denied`)。注意 basename 在 internal log 內 acceptable — 開發
	// 者需要它對應 source location;它不會出現在 returned err。
	if !strings.Contains(logOutput, "original_err") {
		t.Errorf("internal Error log 應含 original_err context field 保留 audit trail:\n%s",
			logOutput)
	}
	if !strings.Contains(logOutput, "permission denied") {
		t.Errorf("internal Error log 應保留 caller err 的非路徑部分:\n%s", logOutput)
	}

	// 但 absolute path prefix 必須被 redact — 即使在 internal log 也不可洩漏
	// system-root prefix。redactPathsInStack 設計上只砍 /Users/<name>/ 之類
	// system-root prefix,**保留 trailing path 元素 + basename** 供開發者對應
	// source location;因此這裡只 assert prefix 不在,不 assert middle 路徑元素。
	for _, leaky := range []string{
		"/Users/alice",
		"/home/bob",
	} {
		if strings.Contains(logOutput, leaky) {
			t.Errorf("internal Error log 不該含未 redact 的 system-root path prefix %q:\n%s",
				leaky, logOutput)
		}
	}

	// 標誌應存在
	if !strings.Contains(logOutput, "<redacted-path>") {
		t.Errorf("internal Error log 應含 redact 標誌:\n%s", logOutput)
	}
}

// recover 後的 Error level log 不能含 absolute path — 否則前端可見的 frontend
// error 訊息會帶 user home path,EMG 病患資料路徑屬 PII。
func TestRecoverHandlerPanic_NoPathLeakInErrorLog(t *testing.T) {
	var buf bytes.Buffer
	testLogger := logging.NewLogger(logging.LevelInfo, &buf, false)

	var err error
	func() {
		defer recoverHandlerPanic("TestHandler", testLogger, &err)
		panic("simulated panic for path leak test")
	}()

	logOutput := buf.String()

	// Error log 不應含 absolute path(/Users / /home / /var 等都不該出現)
	for _, pathPrefix := range []string{"/Users/", "/home/", "/var/folders", "/private/"} {
		if strings.Contains(logOutput, pathPrefix) {
			t.Errorf("Error level log 不應含 absolute path %q:\n%s", pathPrefix, logOutput)
		}
	}

	// stack 內容(.go: 行號)不應出現在 Info level 輸出
	// 注意:logger 自己會用 addCallerInfo 加 (file:line),那是 logger 本身的格式
	// 我們關心的是 recover.go 寫進去的 stack 是否在 default level 漏出
	if strings.Contains(logOutput, "goroutine ") || strings.Contains(logOutput, "runtime.gopanic") {
		t.Errorf("default Info level 不應含 stack trace:\n%s", logOutput)
	}

	// 應該含 panic_uuid 作為對應索引
	if !strings.Contains(logOutput, "panic_uuid=") {
		t.Errorf("Error log 應含 panic_uuid 供 Debug stack 對應:\n%s", logOutput)
	}
}

// 當 logger level=Debug 時 stack 會被寫出來,但內含 absolute path 必須已被
// redactPathsInStack 換成 `<redacted-path>/`。
func TestRecoverHandlerPanic_StackRedactedAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	testLogger := logging.NewLogger(logging.LevelDebug, &buf, false)

	var err error
	func() {
		defer recoverHandlerPanic("TestHandler", testLogger, &err)
		panic("simulated panic for debug stack test")
	}()

	logOutput := buf.String()

	// Debug level 應該有 stack 輸出
	if !strings.Contains(logOutput, "panic stack (redacted)") {
		t.Fatalf("Debug level 應輸出 redacted stack message:\n%s", logOutput)
	}

	// stack 內絕對 path 應已 redact
	// 此 test 跑在 /Users/.../count_mean/gui/ 下,如果未 redact, stack 一定含
	// /Users/。redact 後應該不見。
	if strings.Contains(logOutput, "/Users/") {
		t.Errorf("Debug level stack 內 /Users/ 路徑應被 redact:\n%s", logOutput)
	}

	// 應該含 redact 標誌
	if !strings.Contains(logOutput, "<redacted-path>") {
		t.Errorf("Debug level stack 應含 redact 標誌:\n%s", logOutput)
	}

	// 應該保留 file basename + 行號(對 debug 仍有用)
	if !strings.Contains(logOutput, "recover.go") {
		t.Errorf("Debug stack 應保留 basename 供 debug:\n%s", logOutput)
	}
}

// 釘住 redact 正則對常見 system path prefix 的覆蓋範圍,確認新加平台/路徑
// prefix 時不會被誤判。
func TestRedactPathsInStack_HandlesSystemPathVariants(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		mustRedact []string // 必須在 output 消失的 path prefix
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
			got := redactPathsInStack(tc.input)
			for _, p := range tc.mustRedact {
				if strings.Contains(got, p) {
					t.Errorf("redact 失敗,path %q 仍在 output:\n%s", p, got)
				}
			}
			// redact 標誌應存在
			if !strings.Contains(got, "<redacted-path>") {
				t.Errorf("output 應含 redact 標誌:\n%s", got)
			}
			// basename 應保留
			if !strings.Contains(got, "recover.go") {
				t.Errorf("output 應保留 basename:\n%s", got)
			}
		})
	}
}

// TestNewPanicUUID_NotEmpty 守 panic UUID generator 不會回空字串(若 uuid lib
// 失敗的 fallback 也應該有 placeholder)。
func TestNewPanicUUID_NotEmpty(t *testing.T) {
	got := newPanicUUID()
	if got == "" {
		t.Fatal("newPanicUUID 應永遠回非空字串")
	}
	// 預設長度 = 8(或 fallback "no-uuid" 長 7)
	if len(got) < 7 {
		t.Errorf("UUID 短於預期(< 7 char): %q", got)
	}
}

func TestRecoverHandlerPanicVoid_SwallowsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("void recover should swallow panic, got re-panic %v", r)
		}
	}()
	func() {
		defer recoverHandlerPanicVoid("VoidHandler", nil)
		panic("void handler panic")
	}()
}

// 確認 generic typed variant 把 panic 轉成 T zero value 而非 propagate。
// 以 string / pointer / struct 三個典型 return type 各跑一次。
func TestRecoverHandlerPanicValue_String_PanicResetsToZero(t *testing.T) {
	out := "pre-existing"
	func() {
		defer recoverHandlerPanicValue("StringHandler", nil, &out)
		out = "should-be-overwritten"
		panic("simulated panic in string handler")
	}()

	if out != "" {
		t.Errorf("expected zero value (empty string) after panic, got %q", out)
	}
}

func TestRecoverHandlerPanicValue_Pointer_PanicResetsToNil(t *testing.T) {
	type cfg struct{ name string }

	out := &cfg{name: "pre"}
	func() {
		defer recoverHandlerPanicValue("PtrHandler", nil, &out)
		panic("simulated panic in pointer handler")
	}()

	if out != nil {
		t.Errorf("expected nil pointer after panic, got %+v", out)
	}
}

func TestRecoverHandlerPanicValue_Struct_PanicResetsToZero(t *testing.T) {
	out := models.MaxMeanResult{MaxMean: 42.5}
	func() {
		defer recoverHandlerPanicValue("StructHandler", nil, &out)
		panic("simulated panic in struct handler")
	}()

	if out.MaxMean != 0 {
		t.Errorf("expected zero struct after panic, got %+v", out)
	}
}

func TestRecoverHandlerPanicValue_NoPanic_LeavesValueUnchanged(t *testing.T) {
	out := "preserved"
	func() {
		defer recoverHandlerPanicValue("StringHandler", nil, &out)
		// no panic
	}()

	if out != "preserved" {
		t.Errorf("expected unchanged value when no panic, got %q", out)
	}
}

// 確認 mount / drive-letter / UNC pattern 在實際出現於 error message 內
// (mid-string 或標準 stack line)時都會被切掉 system-root prefix。
// 覆蓋 macOS /Volumes、Linux /mnt、Windows drive-letter、UNC、mid-string POSIX。
// 不依賴 logger,直接驗 redactPathsInStack 正則本身;與 logPanic 整合行為由
// TestLogPanic_PanicWithPathError_RedactsRecovered 守。
func TestRedactPathsInStack_CoversExtendedPlatformRoots(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string // 必須在 output 消失
		mustPreserve []string // 必須保留(basename / 周圍 context)
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
				`alice\AppData`,
				`AppData\Local`,
				`Local\Temp`,
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
			got := redactPathsInStack(tc.input)

			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("redact 失敗,leaky prefix %q 仍在 output:\n%s",
						leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("redact 過度,必要 fragment %q 不在 output:\n%s",
						want, got)
				}
			}
		})
	}
}

// 當 caller 直接 panic 一個含 absolute path 的 fs.PathError 時,recoverHandlerPanic
// 把 recovered value 文字化後塞進 logger.Error 前必須先過 redactPathsInStack —
// logger.sanitizeMessage 只攔 password/token/secret keyword,對純路徑無感。
func TestLogPanic_PanicWithPathError_RedactsRecovered(t *testing.T) {
	var buf bytes.Buffer
	testLogger := logging.NewLogger(logging.LevelInfo, &buf, false)

	var err error
	func() {
		defer recoverHandlerPanic("LoadCSV", testLogger, &err)
		// caller 用 `panic(err)` 包了一個含 absolute path 的 fs.PathError —
		// 典型情境:io 失敗時 caller 認定 invariant violation 直接 panic。
		pathErr := &fs.PathError{
			Op:   "open",
			Path: "/Users/alice/patient/case_2026_05_18/emg_raw.csv",
			Err:  errors.New("permission denied"),
		}
		panic(fmt.Errorf("讀檔失敗: %w", pathErr))
	}()

	logOutput := buf.String()

	// 應該有 panic_uuid + handler 供 audit
	if !strings.Contains(logOutput, "panic_uuid=") {
		t.Errorf("Error log 應含 panic_uuid:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "handler=LoadCSV") {
		t.Errorf("Error log 應含 handler 名稱:\n%s", logOutput)
	}

	// 應有 redact 標誌
	if !strings.Contains(logOutput, "<redacted-path>") {
		t.Errorf("Error log 應含 redact 標誌(代表 recovered 過 redact):\n%s",
			logOutput)
	}

	// 任何 caller-controlled path fragment 都不應出現在 Error log 內。
	// logger 自身會 addCallerInfo 加 (file:line),但 caller name 是此 test 檔
	// (`recover_test.go`),不會含 `/Users/alice/`(redact 後)。
	for _, leak := range []string{
		"/Users/alice",
		"/Users/alice/patient",
		"/Users/alice/patient/case_2026_05_18",
		"/Users/alice/patient/case_2026_05_18/emg_raw.csv",
	} {
		if strings.Contains(logOutput, leak) {
			t.Errorf("recovered 路徑洩漏到 Error log: %q 出現在:\n%s",
				leak, logOutput)
		}
	}

	// 非路徑部分(panic message 文字 + error message 文字)可以保留
	// 供 audit trail 識別 — 這是設計意圖,不是 leak。
	if !strings.Contains(logOutput, "permission denied") {
		t.Errorf("Error log 應保留 panic err 的非路徑部分供 audit:\n%s", logOutput)
	}
}
