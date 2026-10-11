// Package security provides secure path validation functionality
// to prevent path traversal attacks and ensure file operations
// are restricted to allowed directories.
//
// 本套件提供兩條路徑驗證 API，**選用規則見 lenient_path.go 開頭的 Decision matrix**：
//   - PathValidator (此檔)：external file（ValidateExternalPath）/ external dir
//     （ValidateExternalDir），見 ADR-0038。原 strict file 的 ValidateFilePath 無 production
//     caller，ADR-0055 刪除；寫入 OutputDir 的檔案由 io 的 Output placement 守門
//   - OpenLenientValidated (lenient_path.go)：manifest-driven user files（檔名可能含 BTS 字面 "%"）
//
// 路徑一律不做 URL-decode：`%`、`+` 皆為字面檔名字元（ADR-0039）。
//
// PathValidator 只管路徑位置（traversal、敏感位置、symlink、路徑長度），不檢查檔名；
// 檔名規則（Windows 保留名、非法字元、長度、副檔名）由 validation/filename 單一裁判（ADR-0054）。
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
	ErrSensitiveDirectory = errors.New("路徑指向系統敏感目錄")
	ErrPathTooLong        = errors.New("路徑長度超過限制")
	// ErrPathContainsNUL is returned when the input contains a NUL byte
	// (\x00). NUL is a classic file-path truncation vector on POSIX/Windows
	// OS APIs — caller must never pass a NUL-containing path to OpenFile.
	ErrPathContainsNUL = errors.New("路徑含 NUL byte (\\x00)")
)

// Path length constants.
const (
	maxPathLength   = 4096
	nonASCIIVisible = 0x00A0 // Non-ASCII but visible characters start here
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
// resolved 路徑也跑一次敏感位置檢查。lexical check 保留為雙重
// 防護(symlink 不解析但字串本身已含 /etc/ 的直接攻擊)。
//
// 不檢查檔名(Windows 保留名、檔名長度):檔案 caller 自行經 validation/filename
// 把關(ADR-0054)。
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
// 兩者都只做「敏感位置 + 路徑長度」(checkSensitiveLocation),lexical 與 symlink
// resolve 後各跑一次;差別只在 isDir=true 時比對前補結尾分隔符,讓目錄根 `/etc`、
// `~/.ssh` 與其子孫等價命中 `/etc/` 這類 pattern。
func (pv *PathValidator) ValidateExternalDir(dir string) error {
	return pv.validateExternal(dir, true)
}

// validateExternal 是 ValidateExternalPath / ValidateExternalDir 共用的實作。
// isDir 為 true 時,path 以目錄語意處理(見 ValidateExternalDir)。
func (pv *PathValidator) validateExternal(path string, isDir bool) error {
	if path == "" {
		return nil
	}

	// NUL byte 早期 reject — 對稱 lenient_path,boundary 早期擋下惡意 input。
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

	// Layer 1：lexical absPath 直接擋字串本身就敏感的 case（不依賴 fs 狀態）。
	if err := checkSensitiveLocation(absPath, isDir); err != nil {
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
	return checkSensitiveLocation(resolvedPath, isDir)
}

// validatePathFormat 做 validateExternal 的前置檢查:
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

// checkSensitiveLocation 檢查 absPath 是否落在系統敏感位置、或超過路徑長度上限。
// isDir 為 true 時比對前補結尾分隔符,讓目錄根本身(`/etc`)也命中以 `/` 結尾的
// pattern(`/etc/`);`/etc-backup` 補完是 `/etc-backup/`,不會誤中。
//
// ValidateExternalPath / ValidateExternalDir 共用:對 lexical absPath 與 EvalSymlinks
// 解析後的 resolved path 各跑一次。
// sensitivePatterns 含 `/private/etc/`(macOS 上 EvalSymlinks(/etc) 的真實結果),
// 讓 layer-2 也能擋住經 symlink 跳轉的攻擊;但**不**加 `/private/var/`、`/private/tmp/`,
// 避免誤擋 macOS `t.TempDir()` 與 GUI app working dir(真實落在 `/private/var/folders/`)。
//
// Windows 端有完全對稱的權衡:`\AppData\` 整段擋(底下含 `Roaming\`、`LocalLow\`、
// `Local\Microsoft\Credentials\` 等真實 credential / persistence 入口),但對
// `\AppData\Local\Temp\` 子樹開「explicit carve-out」放行 — 它是 Windows runner
// 與一般 user 的 `t.TempDir()` 落點,等同 macOS `/private/var/folders/`,屬合法
// test fixture 與 GUI 暫存區。Carve-out 比「pattern enumeration 列敏感 vendor」
// 維護成本低、安全姿態更保守(預設擋,明示放行)。
//
// 檔名規則(保留名、檔名長度)不在這裡,由 validation/filename 裁判(ADR-0054)。
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

// GetAllowedBasePaths 獲取當前允許的基礎路徑.
func (pv *PathValidator) GetAllowedBasePaths() []string {
	// 返回副本以防止外部修改
	result := make([]string, len(pv.allowedBasePaths))
	copy(result, pv.allowedBasePaths)

	return result
}
