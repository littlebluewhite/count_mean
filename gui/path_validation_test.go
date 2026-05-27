package gui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/logging"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
	"count_mean/internal/security"
)

// setupHandlerTestApp 構造一個 minimal App 帶齊所有 Analyze* handler 入口會用到
// 的 nil-safe 依賴：logger、config、4 個 analyzer。downstream 不會跑到（路徑
// 驗證會先 fail），但 nil 解參考會在 boundary check 之前 panic，所以還是 wire 上。
func setupHandlerTestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: t.TempDir()}

	app := &App{
		logger:              logging.GetLogger("path_validation_test"),
		phaseSyncAnalyzer:   phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:         cci.NewCCIAnalyzer(),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
	}
	app.state.Store(&appState{config: cfg})
	return app
}

// traversalPaths 列出 應該被 boundary 擋下的攻擊樣本。涵蓋：
//   - 顯式 ".." element（最常見的 traversal）
//   - URL-encoded 變形（".%2E/" 等，PathValidator 會 decode 後再 check）
//   - 系統敏感目錄（/etc, /root, C:\Windows）
//
// 不放 "/etc/passwd" raw 字串本身——performBasicSecurityChecks 比對 substring，
// 大小寫不敏感、forward-slash 規範化後仍命中 /etc/。
func traversalPaths() []struct {
	name string
	path string
} {
	return []struct {
		name string
		path string
	}{
		{"relative_traversal", "../etc/passwd"},
		{"deep_relative_traversal", "../../../etc/shadow"},
		{"absolute_etc", "/etc/passwd"},
		{"absolute_root", "/root/.ssh/id_rsa"},
		{"url_encoded_traversal", "..%2Fetc%2Fpasswd"},
	}
}

// TestAnalyzePhaseSync_RejectsTraversalManifest 驗證 AnalyzePhaseSync
// 在 boundary 應該擋下 ManifestFile 路徑中的 traversal / 系統敏感目錄樣本。
// 邊界驗證早於 downstream analyzer，所以 err 必須由 validateExternalPathInputs
// 帶出（含 "路徑驗證失敗" 字樣），且 result 為 nil。
func TestAnalyzePhaseSync_RejectsTraversalManifest(t *testing.T) {
	app := setupHandlerTestApp(t)

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			params := PhaseSyncParams{
				ManifestFile: tc.path,
				DataFolder:   "/tmp/legit", // 不會走到，因為 manifest 先被擋
				StartPhase:   "P0",
				EndPhase:     "P2",
				SubjectIndex: 0,
			}

			result, err := app.AnalyzePhaseSync(params)
			require.Error(t, err, "traversal manifest 應被 boundary 擋下: %s", tc.path)
			assert.Nil(t, result, "失敗應回 nil result")
			assert.True(t,
				strings.Contains(err.Error(), "路徑驗證失敗") ||
					errors.Is(err, security.ErrPathTraversal) ||
					errors.Is(err, security.ErrSensitiveDirectory),
				"err 應含 boundary path validation 標誌: got %v", err)
		})
	}
}

// containsAnyTraversalMarker 檢查 message 是否含路徑驗證失敗的標誌字串。
// boundary path validation 失敗在 AnalyzeCCI / MuscleRatio / NormalizedPhaseSync
// 統一通道下會走 result.Message（Wave 3 Batch R 後）。
//
// 標誌包含：「路徑驗證失敗」(validateExternalPathInputs 包裝詞) 或下游 security
// pkg 帶上的 ErrPathTraversal / ErrSensitiveDirectory 文字（兩者皆 wrap 進 err.Error()）。
func containsAnyTraversalMarker(msg string) bool {
	return strings.Contains(msg, "路徑驗證失敗") ||
		strings.Contains(msg, security.ErrPathTraversal.Error()) ||
		strings.Contains(msg, security.ErrSensitiveDirectory.Error())
}

// TestAnalyzeCCI_RejectsTraversalManifest 同 PhaseSync — 驗證 CCI handler
// boundary path validation。
//
// Wave 3 Batch R 後改採統一錯誤通道：path validation 失敗 → 回 non-nil
// result.Success=false + result.Message 含 boundary 標誌；Go err 為 nil（保留給 panic）。
func TestAnalyzeCCI_RejectsTraversalManifest(t *testing.T) {
	app := setupHandlerTestApp(t)

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			params := CCIParams{
				ManifestFile: tc.path,
				DataFolder:   "/tmp/legit",
				SubjectIndex: 0,
			}

			result, err := app.AnalyzeCCI(params)
			require.NoError(t, err, "統一通道下 boundary 失敗不應走 err: %s", tc.path)
			require.NotNil(t, result, "統一通道下 boundary 失敗應回 non-nil failedResult")
			assert.False(t, result.Success, "traversal manifest 應被 boundary 擋下: %s", tc.path)
			assert.True(t, containsAnyTraversalMarker(result.Message),
				"result.Message 應含 boundary path validation 標誌: got %q", result.Message)
		})
	}
}

// TestAnalyzeMuscleRatio_RejectsTraversalManifest 同 PhaseSync — 驗證
// MuscleRatio handler boundary path validation。同 AnalyzeCCI，統一錯誤通道。
func TestAnalyzeMuscleRatio_RejectsTraversalManifest(t *testing.T) {
	app := setupHandlerTestApp(t)

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			params := MuscleRatioParams{
				ManifestFile: tc.path,
				DataFolder:   "/tmp/legit",
			}

			result, err := app.AnalyzeMuscleRatio(params)
			require.NoError(t, err, "統一通道下 boundary 失敗不應走 err: %s", tc.path)
			require.NotNil(t, result, "統一通道下 boundary 失敗應回 non-nil failedResult")
			assert.False(t, result.Success, "traversal manifest 應被 boundary 擋下: %s", tc.path)
			assert.Nil(t, result.Subjects,
				"整體 boundary 失敗時 Subjects 應為 nil（pre-analysis fail 狀態）")
			assert.True(t, containsAnyTraversalMarker(result.Message),
				"result.Message 應含 boundary path validation 標誌: got %q", result.Message)
		})
	}
}

// TestAnalyzeNormalizedPhaseSync_RejectsTraversalManifest 同 PhaseSync — 驗證
// NormalizedPhaseSync handler boundary path validation。同 AnalyzeCCI，統一錯誤通道。
func TestAnalyzeNormalizedPhaseSync_RejectsTraversalManifest(t *testing.T) {
	app := setupHandlerTestApp(t)

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			params := NormalizedPhaseSyncParams{
				ManifestFile:    tc.path,
				DataFolder:      "/tmp/legit",
				NormStartPhase:  "P0",
				NormEndPhase:    "P2",
				StatsStartPhase: "P0",
				StatsEndPhase:   "P2",
			}

			result, err := app.AnalyzeNormalizedPhaseSync(params)
			require.NoError(t, err, "統一通道下 boundary 失敗不應走 err: %s", tc.path)
			require.NotNil(t, result, "統一通道下 boundary 失敗應回 non-nil failedResult")
			assert.False(t, result.Success, "traversal manifest 應被 boundary 擋下: %s", tc.path)
			assert.True(t, containsAnyTraversalMarker(result.Message),
				"result.Message 應含 boundary path validation 標誌: got %q", result.Message)
		})
	}
}

// TestLoadPhaseManifest_RejectsTraversal 驗證 LoadPhaseManifest
// boundary 擋下 traversal 路徑。LoadPhaseManifest 是 GUI 第一步 (load manifest
// 取得 subject list)，這條 path 若沒擋會把 user-controlled path 直接送進
// manifest parser。
func TestLoadPhaseManifest_RejectsTraversal(t *testing.T) {
	app := setupHandlerTestApp(t)

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			subjects, err := app.LoadPhaseManifest(tc.path)
			require.Error(t, err, "traversal manifest 應被 boundary 擋下: %s", tc.path)
			assert.Nil(t, subjects)
		})
	}
}

// TestAnalyzePhaseSync_RejectsTraversalDataFolder 驗證 DataFolder 也走 boundary
// validation。即使 ManifestFile 合法，DataFolder 含 traversal 也應被擋。
func TestAnalyzePhaseSync_RejectsTraversalDataFolder(t *testing.T) {
	app := setupHandlerTestApp(t)

	// 用 t.TempDir() 拿一個合法 manifest 路徑，這樣 ManifestFile 不會在 boundary
	// 先 fail；boundary 驗證設計成 fail-fast on first invalid，所以為了壓 DataFolder
	// 那條 path，需確保 manifest 通過。
	legitDir := t.TempDir()

	params := PhaseSyncParams{
		ManifestFile: legitDir + "/manifest.csv", // 不存在但路徑合法 → boundary 通過，後續 analyzer fail
		DataFolder:   "../etc",
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	result, err := app.AnalyzePhaseSync(params)
	require.Error(t, err, "traversal DataFolder 應被 boundary 擋下")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "資料夾", "錯誤訊息應指明是 DataFolder 欄位的問題")
}

// TestValidateExternalPathInputs_AcceptsLegitimateAbsolutePaths sanity-check：
// 一般使用者場景 (file dialog 回來的絕對路徑) 必須通過 boundary，避免新增的
// path validation 把合法情境誤擋。t.TempDir() 給的路徑是 absolute、無 traversal、
// 無系統敏感字 — 標準合法樣本。
func TestValidateExternalPathInputs_AcceptsLegitimateAbsolutePaths(t *testing.T) {
	manifestDir := t.TempDir()
	dataDir := t.TempDir()

	err := validateExternalPathInputs(
		"分期總檔案", manifestDir+"/manifest.csv",
		"資料夾", dataDir,
	)
	require.NoError(t, err, "合法絕對路徑必須通過 boundary validation")
}

// TestValidateExternalPathInputs_EmptyPathSkipped 驗證空字串 path 直接 skip
// （由各 handler 自己的 ErrNoManifestFile 等哨兵 error 接住），不會誤判為錯誤。
func TestValidateExternalPathInputs_EmptyPathSkipped(t *testing.T) {
	err := validateExternalPathInputs(
		"分期總檔案", "",
		"資料夾", "",
	)
	require.NoError(t, err, "空字串 path 應 skip，不該回 error")
}

// boundary path validation edge cases
//
// 補上 boundary 在生產環境會碰到的進階攻擊樣本:
//
//  1. Null byte injection: \x00 截斷 — 某些 syscall / log 路徑會在 \0 截斷,
//     使 "/Users/foo/dummy\x00/etc/passwd" lexical 看似 /Users/foo/dummy
//     但 OpenFile 實際開到 /etc/passwd 之類。
//  2. Symlink redirect: 不該由 boundary 擋(filesystem layer 的事),但 boundary
//     應對 symlink path 直接 reject sensitive prefix(ValidateExternalPath
//     已有 layer 2 EvalSymlinks 守護)。此 test 是 regression guard。
//  3. Windows backslash variant: ..\..\Windows\System32 — element traversal
//     必須對 backslash separator 一視同仁。
//  4. 超長 path (> 4096): DoS 防護,validatePathFormat 應 reject。
//  5. Unicode 同形異字: 例如 Cyrillic 'е' (U+0435) vs Latin 'e' (U+0065) —
//     攻擊者用 visually-identical char 偽裝 path element。boundary 不認字形,
//     此 test 守護「形似但 byte 不同的 path 不應觸發特例邏輯」。
//  6. RTL override: U+202E 反向覆寫,讓 "exe.txt" 顯示成 "txt.exe" 之類。

// TestValidateExternalPathInputs_RejectsNullByteInjection 釘住 null byte 攔截:
// path 含 \x00 必須被 reject — 即使 lexical prefix 看似落在合法目錄,filesystem
// layer 會在 null byte 截斷,實際開到不同位置(類 truncation attack)。
func TestValidateExternalPathInputs_RejectsNullByteInjection(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"null_byte_in_middle", "/tmp/legit\x00/etc/passwd"},
		{"null_byte_at_end", "/tmp/legit/manifest.csv\x00"},
		{"null_byte_traversal_combo", "/tmp/dummy\x00/../../../etc/shadow"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExternalPathInputs("分期總檔案", tc.path)
			require.Error(t, err, "null byte 注入 path 必須被擋下: %q", tc.path)
		})
	}
}

// TestValidateExternalPathInputs_RejectsWindowsBackslashTraversal 釘住 cross-platform
// element traversal:即使在 Unix 上,backslash separator 的 .. element 仍應被擋
// (跨平台 manifest CSV 可能來自 Windows 端產生)。
func TestValidateExternalPathInputs_RejectsWindowsBackslashTraversal(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"backslash_traversal", `..\..\Windows\System32\config`},
		{"mixed_separator_traversal", `../..\etc/passwd`},
		{"backslash_appdata", `C:\Users\victim\AppData\Roaming\credentials.json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExternalPathInputs("分期總檔案", tc.path)
			require.Error(t, err, "Windows backslash traversal/sensitive 路徑必須被擋下: %q", tc.path)
		})
	}
}

// TestValidateExternalPathInputs_RejectsOversizePath 釘住超長 path DoS 防護:
// > maxPathLength (4096) 的 path 必須 reject(防止下游 syscall buffer overflow
// 或 log corruption)。
func TestValidateExternalPathInputs_RejectsOversizePath(t *testing.T) {
	// 構造一個 > 4096 char 的 path
	longSegment := strings.Repeat("a", 100)
	parts := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		parts = append(parts, longSegment)
	}
	oversize := "/" + strings.Join(parts, "/") + "/manifest.csv"

	require.Greater(t, len(oversize), 4096, "test fixture 必須真的超過 4096 char")

	err := validateExternalPathInputs("分期總檔案", oversize)
	require.Error(t, err, "超長 path (%d char) 必須被擋下", len(oversize))
	assert.True(t,
		strings.Contains(err.Error(), "路徑驗證失敗") ||
			errors.Is(err, security.ErrPathTooLong),
		"err 應指明 path too long: %v", err)
}

// TestValidateExternalPathInputs_AcceptsUnicodeLookalikeButNotTraversal 釘住
// Unicode homoglyph 攻擊的 boundary 行為:
//
//   - Cyrillic 'е' (U+0435) 看起來像 Latin 'e' 但 byte 不同 — boundary 不會
//     誤把 "/Users/раssword/..." 當 sensitive prefix(因為 sensitivePatterns
//     用 ASCII 比對)。此 test 確認 lookalike 仍走正常路徑(無 traversal /
//     無 sensitive prefix → 通過)。
//   - 但若 lookalike 跟真正的 `..` 組合,traversal element check 仍應觸發。
//
// 關鍵 invariant:boundary 不會「假裝聰明」做 homoglyph normalize — 那會引入
// 自己的 false positive(合法 multi-script 檔名被誤拒)。homoglyph normalize
// 應留給 UX layer(讓使用者警覺,而非 silently rewrite)。
func TestValidateExternalPathInputs_AcceptsUnicodeLookalikeButNotTraversal(t *testing.T) {
	// path 含 Cyrillic 'е' (U+0435), 不含 .. 或 sensitive prefix → 應通過
	cyrillicPath := "/tmp/раssword.csv" // 'р' (U+0440), 'а' (U+0430)
	err := validateExternalPathInputs("分期總檔案", cyrillicPath)
	require.NoError(t, err,
		"含 Cyrillic 但無 traversal/sensitive prefix 的 path 應通過(boundary 不做 homoglyph normalize)")

	// 但若 Cyrillic 字串配 .. element,traversal check 仍應觸發
	cyrillicWithTraversal := "/tmp/раssword/../../../etc/passwd"
	err = validateExternalPathInputs("分期總檔案", cyrillicWithTraversal)
	require.Error(t, err, "Cyrillic + .. traversal 必須被擋下")
}

// TestValidateExternalPathInputs_RejectsRTLOverride 釘住 RTL Unicode override
// (U+202E) 的攔截:RTL 字元能讓檔名顯示顛倒(攻擊者用此偽裝 .exe 為 .txt)。
//
// boundary 本身不需要顯式解 RTL(filesystem layer 不認 RTL semantics),但若
// path 含 .. element + RTL 偽裝,element-based traversal check 仍應觸發。
// 此 test 守該行為:即使有 RTL override 字元,只要 .. 還在 element 中,擋。
func TestValidateExternalPathInputs_RejectsRTLOverride(t *testing.T) {
	// U+202E (RIGHT-TO-LEFT OVERRIDE) + traversal: RTL override 不應讓 .. element
	// 被誤切。用 \u escape 而非裸 unicode 字元避免 staticcheck ST1018 與
	// editor 顯示混淆。
	rtlPath := "/tmp/legit/..\u202e/etc/passwd"
	err := validateExternalPathInputs("分期總檔案", rtlPath)
	require.Error(t, err,
		"RTL override 嵌入的 .. traversal 必須被擋下: %q", rtlPath)
}

// TestValidateExternalPathInputs_SymlinkBoundary 釘住 ValidateExternalPath 的
// layer 2 (EvalSymlinks 解析後再 sensitive prefix 比對) 對 boundary 仍生效。
//
// 構造:在 t.TempDir() 建一個 symlink 指向 /etc;送進 boundary 的 path 是
// symlink path,lexical 看起來無害,但 resolve 後落在 sensitive prefix。
// boundary 必須擋下,而不是「lexical 通過 → 後續 filesystem layer 才意外開到 /etc」。
//
// 跳 Windows:Windows 上 symlink 創建需要 admin權限,CI 較難穩定跑;
// macOS/Linux 跑此 test 已足夠覆蓋 boundary symlink resolve 路徑。
func TestValidateExternalPathInputs_SymlinkBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires admin; skipping cross-platform symlink boundary test")
	}

	tmpDir := t.TempDir()
	symlinkPath := filepath.Join(tmpDir, "evil_link")

	// 建一個 symlink 指 /etc
	if err := os.Symlink("/etc", symlinkPath); err != nil {
		t.Fatalf("無法建立 symlink fixture: %v", err)
	}

	// 透過 symlink 嘗試到 /etc/passwd
	maliciousPath := filepath.Join(symlinkPath, "passwd")
	err := validateExternalPathInputs("分期總檔案", maliciousPath)
	require.Error(t, err,
		"指向 sensitive prefix 的 symlink path 必須在 boundary EvalSymlinks layer 2 擋下: %q", maliciousPath)
}

// TestValidateExternalPathInputs_OddArityReturnsError 釘住
// 過去 odd-arity caller bug 走 panic — production 路徑(handlers →
// recoverHandlerPanic → ErrInternalPanic)會誤判為「系統等級事故」,blast radius
// 過大且 audit log 留 panic stack trace。改為 return structured error,讓 caller
// 走統一 failedResult 通道、UI 訊息能清楚指明是 programmer error。
//
// 此 test 守住兩條 invariant:
//
//  1. odd args → returns non-nil error (不再 panic)
//  2. error message 含「程式 bug」標誌 ("programmer error") 方便 audit log 標記
func TestValidateExternalPathInputs_OddArityReturnsError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"single_arg", []string{"分期總檔案"}},
		{"three_args", []string{"分期總檔案", "/tmp/a.csv", "資料夾"}},
		{"five_args", []string{"分期總檔案", "/tmp/a.csv", "資料夾", "/tmp/b", "額外標籤"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 不應 panic — 用 require.NotPanics 守住「不再走 panic 路徑」契約
			require.NotPanics(t, func() {
				err := validateExternalPathInputs(tc.args...)
				require.Error(t, err, "odd-arity args 必須回 error")
				assert.True(t, errors.Is(err, ErrOddArityValidatorArgs),
					"err 應 wrap ErrOddArityValidatorArgs (caller 可 errors.Is 對應): %v", err)
				assert.Contains(t, err.Error(), "programmer error",
					"err 應含 programmer error 標誌方便 audit log 區分")
			})
		})
	}
}

// TestValidateExternalPathInputs_RejectsWindowsReservedDevice 釘住 Windows
// reserved device name(CON / NUL / COM1 等)的 cross-platform 拒絕:
// 即使在 Unix 上,boundary 也應該擋(避免 manifest CSV 帶 reserved name 在
// Windows 端 deploy 時行為失常)。
func TestValidateExternalPathInputs_RejectsWindowsReservedDevice(t *testing.T) {
	cases := []string{
		"/tmp/CON",
		"/tmp/PRN.csv",
		"/tmp/COM1",
		"/tmp/NUL.txt",
		"/tmp/LPT1.csv",
	}

	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := validateExternalPathInputs("分期總檔案", p)
			require.Error(t, err, "Windows reserved device name 必須被擋下: %s", p)
		})
	}
}

// TestValidateManifestHandlerParams_HappyPath 釘住合法 manifestFile + dataFolder
// 兩者皆為 t.TempDir() 給的絕對路徑時, helper 應回 nil — 這是 7 個 manifest+dataFolder
// Wails handler 的 normal happy-path 入口。
func TestValidateManifestHandlerParams_HappyPath(t *testing.T) {
	manifestDir := t.TempDir()
	dataDir := t.TempDir()

	err := validateManifestHandlerParams(manifestDir+"/manifest.csv", dataDir)
	require.NoError(t, err, "合法 manifestFile + dataFolder 應通過 boundary")
}

// TestValidateManifestHandlerParams_EmptyManifestFile 釘住 strict empty check
// 第一條:manifestFile 為空字串時必須回 ErrNoManifestFile sentinel(用 errors.Is
// 對應),即使 dataFolder 合法亦然 — 不能 silent skip 或誤把 dataFolder 當主體報錯。
func TestValidateManifestHandlerParams_EmptyManifestFile(t *testing.T) {
	dataDir := t.TempDir()

	err := validateManifestHandlerParams("", dataDir)
	require.Error(t, err, "空 manifestFile 必須回 error")
	assert.True(t, errors.Is(err, ErrNoManifestFile),
		"err 應 wrap ErrNoManifestFile (caller 可 errors.Is 對應): %v", err)
}

// TestValidateManifestHandlerParams_EmptyDataFolder 釘住 strict empty check
// 第二條:dataFolder 為空字串時必須回 ErrNoDataFolder(manifestFile 合法時)。
func TestValidateManifestHandlerParams_EmptyDataFolder(t *testing.T) {
	manifestDir := t.TempDir()

	err := validateManifestHandlerParams(manifestDir+"/manifest.csv", "")
	require.Error(t, err, "空 dataFolder 必須回 error")
	assert.True(t, errors.Is(err, ErrNoDataFolder),
		"err 應 wrap ErrNoDataFolder (caller 可 errors.Is 對應): %v", err)
}

// TestValidateManifestHandlerParams_BothEmptySentinelOrder 釘住 sentinel order
// invariant:兩者皆空時, manifestFile 先檢查 → 必須回 ErrNoManifestFile(而非
// ErrNoDataFolder)。caller / UI 文案依賴此順序給出第一個該補的欄位提示。
func TestValidateManifestHandlerParams_BothEmptySentinelOrder(t *testing.T) {
	err := validateManifestHandlerParams("", "")
	require.Error(t, err, "兩者皆空必須回 error")
	assert.True(t, errors.Is(err, ErrNoManifestFile),
		"兩者皆空時應優先回 ErrNoManifestFile (manifestFile 先檢查): %v", err)
	assert.False(t, errors.Is(err, ErrNoDataFolder),
		"兩者皆空時不應回 ErrNoDataFolder (manifestFile 第一個 fail-fast): %v", err)
}

// TestValidateManifestHandlerParams_RejectsTraversalManifest 釘住 helper 對
// manifestFile 走 boundary path validation:traversal / 系統敏感目錄樣本必須擋下,
// 且 error message 帶有「分期總檔案」label 與「路徑驗證失敗」標誌(來自
// validateExternalPathInputs 的 wrap),讓 UI / audit log 能定位是哪個欄位失敗。
//
// Reuse traversalPaths() 把樣本集中,確保 7 個 caller handler 共享同一套防線。
func TestValidateManifestHandlerParams_RejectsTraversalManifest(t *testing.T) {
	dataDir := t.TempDir()

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManifestHandlerParams(tc.path, dataDir)
			require.Error(t, err, "traversal manifestFile 必須被擋下: %s", tc.path)
			assert.Contains(t, err.Error(), "分期總檔案",
				"err 應含 manifestFile label「分期總檔案」: %v", err)
			assert.Contains(t, err.Error(), "路徑驗證失敗",
				"err 應含 boundary path validation 標誌「路徑驗證失敗」: %v", err)
		})
	}
}

// TestValidateManifestHandlerParams_RejectsTraversalDataFolder 釘住 helper 對
// dataFolder 走 boundary path validation:manifestFile 合法 + dataFolder 含
// traversal/sensitive 樣本時,error message 必須帶「資料夾」label 與
// 「路徑驗證失敗」標誌(避免兩個欄位失敗訊息互相混淆)。
func TestValidateManifestHandlerParams_RejectsTraversalDataFolder(t *testing.T) {
	manifestDir := t.TempDir()
	legitManifest := manifestDir + "/manifest.csv"

	for _, tc := range traversalPaths() {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManifestHandlerParams(legitManifest, tc.path)
			require.Error(t, err, "traversal dataFolder 必須被擋下: %s", tc.path)
			assert.Contains(t, err.Error(), "資料夾",
				"err 應含 dataFolder label「資料夾」: %v", err)
			assert.Contains(t, err.Error(), "路徑驗證失敗",
				"err 應含 boundary path validation 標誌「路徑驗證失敗」: %v", err)
		})
	}
}
