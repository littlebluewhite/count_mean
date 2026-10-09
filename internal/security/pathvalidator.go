// Package security provides secure path validation functionality
// to prevent path traversal attacks and ensure file operations
// are restricted to allowed directories.
//
// 本套件提供兩條路徑驗證 API，**選用規則見 lenient_path.go 開頭的 Decision matrix**：
//   - PathValidator (此檔)：strict file（ValidateFilePath，限 allow-list 內）/
//     external file（ValidateExternalPath）/ external dir（ValidateExternalDir），見 ADR-0038
//   - OpenLenientValidated (lenient_path.go)：manifest-driven user files（檔名可能含 BTS 字面 "%"）
//
// 路徑一律不做 URL-decode：`%`、`+` 皆為字面檔名字元（ADR-0039）。
package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"count_mean/internal/security/fsperm"
)

// Path validation error definitions.
var (
	ErrPathTraversal      = errors.New("路徑包含可疑的遍歷模式")
	ErrPathTraversalAbs   = errors.New("絕對路徑仍包含遍歷字符")
	ErrPathOutOfScope     = errors.New("路徑超出允許範圍")
	ErrSensitiveDirectory = errors.New("路徑指向系統敏感目錄")
	ErrPathTooLong        = errors.New("路徑長度超過限制")
	ErrFilenameTooLong    = errors.New("文件名長度超過限制")
	// ErrPathContainsNUL is returned when the input contains a NUL byte
	// (\x00). NUL is a classic file-path truncation vector on POSIX/Windows
	// OS APIs — caller must never pass a NUL-containing path to OpenFile.
	ErrPathContainsNUL = errors.New("路徑含 NUL byte (\\x00)")
)

// Path length constants.
const (
	maxPathLength     = 4096
	maxFilenameLength = 255
	nonASCIIVisible   = 0x00A0 // Non-ASCII but visible characters start here
)

// PathValidator provides secure path validation functionality.
// 建構後 allowedBasePaths 不可變(只讀),並發讀取安全,故無需鎖。
type PathValidator struct {
	allowedBasePaths []string
}

//nolint:gochecknoglobals // intentional process-wide singleton
var (
	defaultValidator     *PathValidator
	defaultValidatorOnce sync.Once
)

// DefaultValidator 回傳 process-wide default PathValidator singleton(無 base paths
// whitelist)。需要自訂 allow-list 的 caller 請改用 NewPathValidator(...) 建構自己的 instance。
func DefaultValidator() *PathValidator {
	defaultValidatorOnce.Do(func() {
		defaultValidator = NewPathValidator(nil)
	})
	return defaultValidator
}

// NewPathValidator creates a new path validator with allowed base paths.
func NewPathValidator(allowedBasePaths []string) *PathValidator {
	// Convert all base paths to absolute paths
	absPaths := make([]string, len(allowedBasePaths))

	for i, path := range allowedBasePaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			// If we can't get absolute path, use the original
			absPath = path
		}

		absPaths[i] = filepath.Clean(absPath)
	}

	return &PathValidator{
		allowedBasePaths: absPaths,
	}
}

// ValidateFilePath validates that a file path is within allowed directories.
//
// 用於受控的內部讀寫路徑 (InputDir / OutputDir / OperateDir)。對於使用者選的
// 任意檔（GUI file dialog 回來的絕對路徑），改用 ValidateExternalPath，否則
// 路徑落在 allowedBasePaths 外會被 reject。
//
// 路徑不做 URL-decode:`%`、`+` 等一律是字面檔名字元(見 validatePathFormat)。
func (pv *PathValidator) ValidateFilePath(path string) error {
	if path == "" {
		return nil
	}

	// NUL byte 早期 reject — 對稱 lenient_path 與 validateExternalPathInputs,
	// 所有 strict 路徑 API 必須一致拒絕。
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w", ErrPathContainsNUL)
	}

	absPath, err := pv.validatePathFormat(path)
	if err != nil {
		return err
	}

	allowed := pv.allowedBasePaths

	// 靈活的白名單驗證機制 - 避免絕對路徑長度限制
	if len(allowed) == 0 {
		// 如果沒有設定允許路徑，只進行基本安全檢查
		return performBasicSecurityChecks(absPath)
	}

	// 檢查路徑是否在允許的基礎路徑內
	for _, basePath := range allowed {
		// 白名單本應已絕對化(NewPathValidator);再 Abs 一次支援長絕對路徑、容忍手建的 validator。
		absBase, absErr := filepath.Abs(basePath)
		if absErr != nil {
			continue
		}
		if fsperm.IsWithin(absBase, absPath) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrPathOutOfScope, path)
}

// ValidateExternalPath validates an externally-selected path (e.g. from a GUI
// file dialog) without enforcing the allowed-base-paths whitelist. Use for
// CSV reads (CSVHandler.ReadCSV), manifest / data-folder inputs
// and output / configured directories — any path the user has explicitly chosen.
//
// Symlink defense (critical): lexical Clean+Abs alone is insufficient —
//
//	ln -s /etc /tmp/foo
//	ValidateExternalPath("/tmp/foo/passwd")   // lexical 不中 /etc/,但
//	                                          // os.Open 跟著 symlink 開到 /etc/passwd
//
// 因此 absPath 透過 fsperm.EvalSymlinksWithFallback 解析到 nearest existing parent,
// resolved 路徑也跑一次 performBasicSecurityChecks。lexical check 保留為雙重
// 防護(symlink 不解析但字串本身已含 /etc/ 的直接攻擊)。
//
// Windows note:filepath.EvalSymlinks 解析 NTFS junction / reparse point。
// Windows 沒有 O_NOFOLLOW 等價 flag,caller-side EvalSymlinks 是唯一可靠的
// symlink 防護(見 fsperm/flags_windows.go);本檢查補強 Windows boundary,不
// 替代 caller 端 O_NOFOLLOW。
func (pv *PathValidator) ValidateExternalPath(path string) error {
	return pv.validateExternal(path, false)
}

// ValidateExternalDir 驗證使用者外部選取的「目錄」(output / data folder / config 目錄),
// 與 ValidateExternalPath 擋同一組系統敏感位置,但以目錄語意判定。
//
// 目錄走自己的檢查:只做「敏感位置 + 路徑長度」(checkSensitiveLocation,isDir=true
// 時比對前補結尾分隔符,讓目錄根 `/etc`、`~/.ssh` 與其子孫等價命中 `/etc/` 這類
// pattern),lexical 與 symlink resolve 後各跑一次。「檔名」類規則(檔名長度、
// Windows reserved device name)只對檔案有意義,不套用到目錄。
func (pv *PathValidator) ValidateExternalDir(dir string) error {
	return pv.validateExternal(dir, true)
}

// validateExternal 是 ValidateExternalPath / ValidateExternalDir 共用的實作。
// isDir 為 true 時,path 以目錄語意處理(見 ValidateExternalDir)。
func (pv *PathValidator) validateExternal(path string, isDir bool) error {
	if path == "" {
		return nil
	}

	// 對稱 ValidateFilePath 的 NUL byte reject — boundary 早期擋下惡意 input。
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w", ErrPathContainsNUL)
	}

	// path 可能含合法字面 `%`(例 `report 50%.csv` / BTS 匯出 `SF_8_BTS%_*.csv`),
	// validatePathFormat 不 decode,故照原樣通過;element traversal / 敏感目錄 /
	// symlink resolve 仍生效。
	absPath, err := pv.validatePathFormat(path)
	if err != nil {
		return err
	}

	// 檔案:位置 + 檔名規則;目錄:只有位置規則。
	check := func(p string) error {
		if isDir {
			return checkSensitiveLocation(p, true)
		}
		return performBasicSecurityChecks(p)
	}

	// Layer 1：lexical absPath 直接擋字串本身就敏感的 case（不依賴 fs 狀態）。
	if err := check(absPath); err != nil {
		return err
	}

	// Layer 2：resolve symlink 後再檢一次。原因見上方註解。
	resolvedPath, resolveErr := fsperm.EvalSymlinksWithFallback(absPath, 0)
	if resolveErr != nil {
		// fsperm.EvalSymlinksWithFallback 只在「整條 path 連 / 都不存在」這類異常時 fail。
		// 對「path 不存在但 parent 存在」會走 fallback。此處保守 reject，避免 silently
		// skip layer 2 留下 false negative。
		return fmt.Errorf("無法解析路徑符號連結 '%s': %w", absPath, resolveErr)
	}

	// Resolved 路徑可能與 absPath 相同（無 symlink）— 重複跑一次也 cheap，且
	// 程式邏輯簡潔（不需特判 absPath == resolvedPath）。
	return check(resolvedPath)
}

// validatePathFormat 做 ValidateFilePath / ValidateExternalPath 共用的前置檢查:
// traversal + 絕對化。回傳 Clean 後的絕對路徑。
//
// 不做 URL-decode:path 是檔案系統路徑,不是 URL。`%2E%2E`、`%2F`、`+` 都是字面檔名
// 字元,OS 開檔時也不會解碼;先前的 decode loop 會讓「驗證的路徑」與「實際開的路徑」
// 不一致(`a+b.csv` 被驗成 `a b.csv`),還會誤拒合法的 `%` 檔名。
//
// Traversal 檢查採 element-based(split 比對 `..` element),不用 substring scan —
// 後者會誤拒 `report..v2.csv` / `backup..2024.csv` 等合法檔名。
func (*PathValidator) validatePathFormat(path string) (absPath string, err error) {
	// Pre-Clean element check：interior `..`（例如 `./input/../output/test.csv`）
	// 在 Clean 後會被解析消去，但這種跨目錄寫法本身就值得擋，因此在 Clean 之前
	// 先 split 比對 `..` element。filename 含字面雙點（`report..v2.csv`）的 path
	// element 不會被誤拒。
	if HasTraversalElement(path) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, path)
	}

	abs, absErr := filepath.Abs(filepath.Clean(path))
	if absErr != nil {
		return "", fmt.Errorf("無法解析路徑 '%s': %w", path, absErr)
	}

	if HasTraversalElement(abs) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversalAbs, abs)
	}

	return abs, nil
}

// HasTraversalElement reports whether any path element equals "..".
// Splits on both `/` and `\` so cross-platform paths receive a consistent check
// regardless of which separator the caller passed in.
//
// Exported so downstream packages (e.g. internal/io) can apply the same
// element-aware semantics instead of substring `..` checks that misclassify
// legitimate filenames like `report..v2.csv`.
func HasTraversalElement(path string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// IsCSVFile checks if the file has a .csv extension.
//
// 取 Ext 前先 TrimRight 把尾端空白與點剝掉:Excel 匯出 / Windows 拖拉常在檔名
// 尾端留 trailing space 或 dot,這類檔名仍能 open,validator 不該誤判為非 CSV。
//
// 本函式只判斷副檔名類別,不負責路徑驗證;caller 若要實際開檔請先走
// ValidateFilePath / ValidateExternalPath。
func IsCSVFile(path string) bool {
	return strings.ToLower(filepath.Ext(strings.TrimRight(path, " ."))) == ".csv"
}

// performBasicSecurityChecks 執行基本安全檢查,適用於無白名單限制的情況。
//
// ValidateExternalPath 用此函式檢查 lexical absPath 與 EvalSymlinks 解析後的
// resolved path 兩條路徑。sensitivePatterns 含 `/private/etc/`(macOS 上
// EvalSymlinks(/etc) 的真實結果),讓 layer-2 也能擋住經 symlink 跳轉的攻擊;
// 但**不**加 `/private/var/`、`/private/tmp/`,避免誤擋 macOS `t.TempDir()`
// 與 GUI app working dir(真實落在 `/private/var/folders/`)。
//
// Windows 端有完全對稱的權衡:`\AppData\` 整段擋(底下含 `Roaming\`、`LocalLow\`、
// `Local\Microsoft\Credentials\` 等真實 credential / persistence 入口),但對
// `\AppData\Local\Temp\` 子樹開「explicit carve-out」放行 — 它是 Windows runner
// 與一般 user 的 `t.TempDir()` 落點,等同 macOS `/private/var/folders/`,屬合法
// test fixture 與 GUI 暫存區。Carve-out 比「pattern enumeration 列敏感 vendor」
// 維護成本低、安全姿態更保守(預設擋,明示放行)。
func performBasicSecurityChecks(absPath string) error {
	if err := checkSensitiveLocation(absPath, false); err != nil {
		return err
	}

	return checkFilename(absPath)
}

// checkSensitiveLocation 檢查 absPath 是否落在系統敏感位置、或超過路徑長度上限。
// isDir 為 true 時比對前補結尾分隔符,讓目錄根本身(`/etc`)也命中以 `/` 結尾的
// pattern(`/etc/`);`/etc-backup` 補完是 `/etc-backup/`,不會誤中。
func checkSensitiveLocation(absPath string, isDir bool) error {
	// 跨平台系統敏感路徑。`.ssh/` / `.aws/` / `.kube/` 用 `/` 開頭涵蓋
	// Unix home(`~/.ssh/`)與 Windows %USERPROFILE%。Windows 端用 `\Windows\`、
	// `\System32\` 等 leading-backslash 形式涵蓋非 C: drive(VM / Bootcamp)。
	// `\\\\` 命中 UNC paths 與 device namespace(`\\?\` 還會繞過 Win32 path
	// normalization,高風險入口);POSIX `//foo/bar` 屬可接受 trade-off。
	sensitivePatterns := []string{
		"/etc/",               // Unix 系統配置
		"/root/",              // Unix root 目錄
		"/proc/",              // Unix 進程文件系統
		"/sys/",               // Unix 系統文件系統
		"/dev/",               // Unix 設備文件
		"/boot/",              // Unix 啟動文件
		"/var/log/",           // Unix audit / 系統 log
		"/private/etc/",       // macOS EvalSymlinks(/etc) 真實路徑
		"/Library/Keychains/", // macOS keychain
		"/.ssh/",              // SSH key 目錄(跨平台慣例)
		"/.aws/",              // AWS credentials
		"/.kube/",             // Kubernetes config + tokens
		"C:\\Windows\\",       // Windows 系統目錄
		"C:\\System32\\",      // Windows 系統32
		"C:\\Program Files\\", // Windows 程式文件
		"\\Windows\\",         // 非 C: drive 的 Windows
		"\\System32\\",        // 非 C: drive 的 System32
		"\\Program Files\\",   // 非 C: drive 的 Program Files
		"\\AppData\\",         // Windows AppData (Roaming/Local/LocalLow) — carve-out 見下方
		"\\\\",                // UNC prefix (server share、device namespace、global)
	}

	// 用 forward-slash 統一比對:sensitivePatterns 混用 `/etc/`、`C:\Windows\`、
	// `\System32\` 等多種 separator,直接 strings.Contains 在 Windows 上的
	// `c:\etc\passwd` 找不到 `/etc/` — 真實安全破口。
	//
	// filepath.ToSlash 只把「當前 OS separator」轉 `/`,Unix 上對 `\AppData\` 無
	// 作用,因此 absPath 與 pattern 兩端都得做 strings.ReplaceAll(s, `\`, "/"),
	// 確保 mac/linux CI 也能驗到 Windows 路徑樣本。
	normalizePath := func(s string) string {
		return strings.ReplaceAll(filepath.ToSlash(strings.ToLower(s)), `\`, "/")
	}
	absPathSlash := normalizePath(absPath)
	if isDir && !strings.HasSuffix(absPathSlash, "/") {
		absPathSlash += "/"
	}

	// `\AppData\Local\Temp\` carve-out:Windows runner 與一般 user 的 `t.TempDir()`
	// 都落在這個子樹下(`C:\Users\<u>\AppData\Local\Temp\Test...\001\...`),等同
	// macOS `/private/var/folders/`,屬合法 test fixture 與 GUI 暫存區。
	//
	// 設計權衡(對應 doc comment 上方說明):
	//   - 不擋 `\AppData\Local\Temp\<anything>`,即便 path 也含 `\AppData\` 子串。
	//   - 仍擋 `\AppData\Local\Microsoft\Credentials\`、`\AppData\Roaming\<vendor>\` 等。
	//
	// Carve-out 必須在 sensitivePatterns substring match 之前 short-circuit,因為
	// `\AppData\` 一定先命中。**只**對 AppData 這一條 pattern 套 carve-out — 其他
	// pattern(/etc/、UNC 等)在 Temp 子樹下出現仍應擋。
	const (
		appDataPattern         = "/appdata/"
		appDataLocalTempPrefix = "/appdata/local/temp/"
	)
	inAppDataLocalTemp := strings.Contains(absPathSlash, appDataLocalTempPrefix)

	for _, pattern := range sensitivePatterns {
		patternSlash := normalizePath(pattern)
		if patternSlash == appDataPattern && inAppDataLocalTemp {
			continue // carve-out: `\AppData\Local\Temp\` 不視為敏感
		}
		if strings.Contains(absPathSlash, patternSlash) {
			return fmt.Errorf("%w: %s", ErrSensitiveDirectory, pattern)
		}
	}

	// 檢查路徑長度（防止過長路徑攻擊）
	if len(absPath) > maxPathLength {
		return fmt.Errorf("%w (%d 字符): %d", ErrPathTooLong, maxPathLength, len(absPath))
	}

	return nil
}

// checkFilename 檢查檔名專屬規則(長度、Windows reserved device name);僅適用檔案。
func checkFilename(absPath string) error {
	// 檢查文件名長度
	filename := filepath.Base(absPath)

	if len(filename) > maxFilenameLength {
		return fmt.Errorf("%w (%d 字符): %d", ErrFilenameTooLong, maxFilenameLength, len(filename))
	}

	// Windows reserved device names(CON / PRN / AUX / NUL / COM1-9 / LPT1-9):
	// 即使加副檔名("CON.txt")仍會 redirect 到實體 console device,後續 OpenFile
	// 行為怪異(write 對方終端 echo、read 永遠 block)。跨平台都 reject,保守
	// 一致性遠比「Unix 允許 CON 檔」價值高。
	if isWindowsReservedFilename(filename) {
		return fmt.Errorf("%w: %s 為 Windows 保留裝置名稱", ErrSensitiveDirectory, filename)
	}

	return nil
}

// windowsReservedDeviceNames 列出 Windows 上 OpenFile 會被 redirect 到實體
// device 的保留名稱。完整清單見 MSDN Win32 namespace。
//
//nolint:gochecknoglobals // 編譯期 static lookup table
var windowsReservedDeviceNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// isWindowsReservedFilename 判斷檔名（去除副檔名後）是否為 Windows reserved
// device name。比對採全字 case-insensitive 比對 — `econom_report.csv` 含子字串
// `con` 不視為命中，僅 `CON` / `CON.csv` 等真實 reserved name 才回 true。
func isWindowsReservedFilename(filename string) bool {
	if filename == "" {
		return false
	}

	// 去除副檔名後比對，因為 `CON.txt` 在 Windows 仍 redirect 到 console
	stem := filename
	if ext := filepath.Ext(filename); ext != "" {
		stem = strings.TrimSuffix(filename, ext)
	}
	_, ok := windowsReservedDeviceNames[strings.ToUpper(stem)]
	return ok
}

// GetAllowedBasePaths 獲取當前允許的基礎路徑.
func (pv *PathValidator) GetAllowedBasePaths() []string {
	// 返回副本以防止外部修改
	result := make([]string, len(pv.allowedBasePaths))
	copy(result, pv.allowedBasePaths)

	return result
}
