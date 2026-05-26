// Package security provides secure path validation functionality
// to prevent path traversal attacks and ensure file operations
// are restricted to allowed directories.
//
// 本套件提供兩條路徑驗證 API，**選用規則見 lenient_path.go 開頭的 Decision matrix**：
//   - PathValidator (此檔)：受控內部讀寫路徑（InputDir / OutputDir / 直接 user-input）
//   - ResolveLenientPath (lenient_path.go)：manifest-driven user files（檔名可能含 BTS 字面 "%"）
package security

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

// Path validation error definitions.
var (
	ErrPathTraversal      = errors.New("路徑包含可疑的遍歷模式")
	ErrPathTraversalAbs   = errors.New("絕對路徑仍包含遍歷字符")
	ErrPathOutOfScope     = errors.New("路徑超出允許範圍")
	ErrFilenameTraversal  = errors.New("文件名包含路徑遍歷字符")
	ErrFilenameInvalid    = errors.New("文件名無效或被清理後為空")
	ErrSensitiveDirectory = errors.New("路徑指向系統敏感目錄")
	ErrPathTooLong        = errors.New("路徑長度超過限制")
	ErrFilenameTooLong    = errors.New("文件名長度超過限制")
	// ErrAllowedBasePathsEmpty is returned by SetAllowedBasePaths when the
	// supplied slice is nil, empty, or contains only blank entries — preventing
	// callers from silently disabling the allow-list. If genuinely no allow-list
	// is desired, construct the validator with NewPathValidator(nil) instead.
	ErrAllowedBasePathsEmpty = errors.New("allowedBasePaths 不能為空（請改用 NewPathValidator(nil) 建立無白名單 validator）")

	// ErrPathSanitizationRequired is returned by SanitizePath when the input
	// contains characters or patterns that would require rewriting (NUL byte,
	// control chars, `../` traversal). Silent rewrite would let attackers
	// craft inputs whose post-sanitize form passes downstream ValidateFilePath
	// but resolves to a different file on disk; callers must retry with a
	// clean filename or surface the rejection.
	ErrPathSanitizationRequired = errors.New("路徑包含需淨化的字元，拒絕 silent 改寫")

	// ErrPathContainsNUL is returned when the input contains a NUL byte
	// (\x00). NUL is a classic file-path truncation vector on POSIX/Windows
	// OS APIs — caller must never pass a NUL-containing path to OpenFile.
	ErrPathContainsNUL = errors.New("路徑含 NUL byte (\\x00)")

	// ErrValidatorFrozen is returned by SetAllowedBasePaths when invoked on a
	// frozen PathValidator (currently only the process-wide DefaultValidator()
	// singleton). Prevents post-init callers from silently widening or
	// narrowing the default allow-list, which would break the trust boundary
	// that downstream code relies on.
	ErrValidatorFrozen = errors.New("PathValidator 已凍結，不允許修改 allow-list（請改用 NewPathValidator 建構新 instance）")
)

// Path length constants.
const (
	maxPathLength     = 4096
	maxFilenameLength = 255
	nonASCIIVisible   = 0x00A0 // Non-ASCII but visible characters start here
)

// PathValidator provides secure path validation functionality.
// The allowed base paths are protected by an RWMutex so that callers may
// safely invoke SetAllowedBasePaths from one goroutine while another reads
// the list via ValidateFilePath or GetAllowedBasePaths.
//
// frozen marks the validator immutable (set only by DefaultValidator's
// singleton, never reset). Frozen validators return ErrValidatorFrozen from
// SetAllowedBasePaths.
type PathValidator struct {
	mu               sync.RWMutex
	allowedBasePaths []string
	frozen           bool
}

//nolint:gochecknoglobals // intentional process-wide singleton
var (
	defaultValidator     *PathValidator
	defaultValidatorOnce sync.Once
)

// DefaultValidator 回傳 process-wide default PathValidator singleton(無 base paths
// whitelist)。已 markFrozen(),SetAllowedBasePaths 一律回 ErrValidatorFrozen,
// 確保 default validator 在 init 之後行為一致。需要自訂 allow-list 的 caller
// 請改用 NewPathValidator(...) 建構自己的 instance。
func DefaultValidator() *PathValidator {
	defaultValidatorOnce.Do(func() {
		defaultValidator = NewPathValidator(nil)
		defaultValidator.markFrozen()
	})
	return defaultValidator
}

// markFrozen marks the validator as immutable. Once frozen, SetAllowedBasePaths
// returns ErrValidatorFrozen. Currently only called by DefaultValidator() to
// freeze the process-wide singleton; not exported because the immutability
// contract should be a property of construction, not a runtime opt-in (callers
// who want immutability should reach for DefaultValidator() instead).
func (pv *PathValidator) markFrozen() {
	pv.mu.Lock()
	pv.frozen = true
	pv.mu.Unlock()
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
// 嚴格守門：strictPercent=true → URL-decode 4 層後仍含 literal `%` 視為可疑
// 直接 reject。理由：本 API 處理 config / API string 直接送入的 path，trust
// model 較低，雙重編碼攻擊風險高。
func (pv *PathValidator) ValidateFilePath(path string) error {
	if path == "" {
		return nil
	}

	// NUL byte 早期 reject — 對稱 lenient_path 與 validateExternalPathInputs,
	// 所有 strict 路徑 API 必須一致拒絕。
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w", ErrPathContainsNUL)
	}

	absPath, decodedPath, err := pv.validatePathFormat(path, true)
	if err != nil {
		return err
	}

	// 取得允許路徑的快照，避免在驗證過程中與 SetAllowedBasePaths 競爭
	pv.mu.RLock()
	allowed := pv.allowedBasePaths
	pv.mu.RUnlock()

	// 靈活的白名單驗證機制 - 避免絕對路徑長度限制
	if len(allowed) == 0 {
		// 如果沒有設定允許路徑，只進行基本安全檢查
		return performBasicSecurityChecks(absPath)
	}

	// 檢查路徑是否在允許的基礎路徑內
	for _, basePath := range allowed {
		if isPathWithinBase(absPath, basePath) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrPathOutOfScope, decodedPath)
}

// ValidateExternalPath validates an externally-selected path (e.g. from a GUI
// file dialog) without enforcing the allowed-base-paths whitelist. Use for
// GetCSVHeaders / chart preview / any reader where the user has explicitly
// chosen the file.
//
// Symlink defense (critical): lexical Clean+Abs alone is insufficient —
//
//	ln -s /etc /tmp/foo
//	ValidateExternalPath("/tmp/foo/passwd")   // lexical 不中 /etc/,但
//	                                          // os.Open 跟著 symlink 開到 /etc/passwd
//
// 因此 absPath 透過 resolveSymlinksWithFallback 解析到 nearest existing parent,
// resolved 路徑也跑一次 performBasicSecurityChecks。lexical check 保留為雙重
// 防護(symlink 不解析但字串本身已含 /etc/ 的直接攻擊)。
//
// Windows note:filepath.EvalSymlinks 解析 NTFS junction / reparse point。
// Windows 沒有 O_NOFOLLOW 等價 flag,caller-side EvalSymlinks 是唯一可靠的
// symlink 防護(見 fsperm/flags_windows.go);本檢查補強 Windows boundary,不
// 替代 caller 端 O_NOFOLLOW。
func (pv *PathValidator) ValidateExternalPath(path string) error {
	if path == "" {
		return nil
	}

	// 對稱 ValidateFilePath 的 NUL byte reject — boundary 早期擋下惡意 input。
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w", ErrPathContainsNUL)
	}

	// strictPercent=false:GUI dialog 等 user-confirmed 場景,path 可能含合法
	// 字面 `%`(例 `report 50%.csv` / BTS 匯出 `SF_8_BTS%_*.csv`)。嚴格守門
	// 會打斷流程;放寬 % 不影響其他守門(element traversal / 敏感目錄 / symlink
	// resolve 都仍生效)。
	absPath, _, err := pv.validatePathFormat(path, false)
	if err != nil {
		return err
	}

	// Layer 1：lexical absPath 直接擋字串本身就敏感的 case（不依賴 fs 狀態）。
	if err := performBasicSecurityChecks(absPath); err != nil {
		return err
	}

	// Layer 2：resolve symlink 後再檢一次。原因見上方註解。
	resolvedPath, resolveErr := resolveSymlinksWithFallback(absPath)
	if resolveErr != nil {
		// resolveSymlinksWithFallback 只在「整條 path 連 / 都不存在」這類異常時 fail。
		// 對「path 不存在但 parent 存在」會走 fallback。此處保守 reject，避免 silently
		// skip layer 2 留下 false negative。
		return fmt.Errorf("無法解析路徑符號連結 '%s': %w", absPath, resolveErr)
	}

	// Resolved 路徑可能與 absPath 相同（無 symlink）— 重複跑一次也 cheap，且
	// 程式邏輯簡潔（不需特判 absPath == resolvedPath）。
	return performBasicSecurityChecks(resolvedPath)
}

// resolveSymlinksWithFallback 解析 absPath 上所有 symlink，回傳完全解析後的
// 絕對路徑。如果 absPath 本身不存在（典型 case：caller 傳 OutputDir +
// dummy child，dummy child 尚未建立），會逐層往 parent 走找到 nearest existing
// parent，對其 EvalSymlinks 解析後，再 append 未存在的 child suffix。
//
// 為何需要 fallback：filepath.EvalSymlinks 對不存在的路徑直接回 ENOENT，
// 但 config.Validate / cci.ExportToCSV / muscle_ratio.Analyze 都會建構「未來
// 寫入路徑的 prefix + dummy marker」來做 sensitive-prefix 驗證，這些 path
// 都是 not-yet-exists。沒 fallback 會把整批 config validation 打掛。
//
// 演算法：
//  1. 嘗試 EvalSymlinks(absPath)。若成功直接回。
//  2. 否則切出 dir = filepath.Dir(absPath)，base = filepath.Base(absPath)。
//  3. 對 dir 遞迴呼自己（直到能解析或抵達 root）。
//  4. 回傳 filepath.Join(resolvedDir, base)。
//
// 邊界：抵達 root（filepath.Dir(p) == p）仍無法解析時回 error — 表示連 /
// 都不可解析，這在正常系統不會發生（除非檔系統爛掉），保守 fail-closed。
//
//nolint:err113 // dynamic errors for user-facing output
func resolveSymlinksWithFallback(absPath string) (string, error) {
	if absPath == "" {
		return "", nil
	}

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved, nil
	}

	parent := filepath.Dir(absPath)
	// Dir 已抵達 root（"/" on Unix, "C:\" on Windows）仍解析不了 — 異常狀況。
	if parent == absPath {
		return "", fmt.Errorf("路徑根目錄無法解析: %s", absPath)
	}

	resolvedParent, err := resolveSymlinksWithFallback(parent)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolvedParent, filepath.Base(absPath)), nil
}

// validatePathFormat performs the URL-decode + traversal + absolute-resolution
// prechecks shared by ValidateFilePath and ValidateExternalPath. Returns the
// cleaned absolute path and the URL-decoded original (used for error messages).
//
// Traversal 檢查採 element-based(filepath.Clean 後 split 比對 `..` element),
// 不用 substring scan — 後者會誤拒 `report..v2.csv` / `backup..2024.csv` 等合法
// 檔名。
//
// strictPercent 控制 URL-decode 4 層後是否仍將「殘留 `%`」視為可疑:
//   - true  (ValidateFilePath)    : 受控路徑,雙重編碼風險高,殘留 % 直接 reject
//   - false (ValidateExternalPath): GUI dialog 等 user-confirmed 場景, 合法檔名
//     可能本就含字面 `%`,殘留 % 放行;其他守門仍生效。
func (*PathValidator) validatePathFormat(path string, strictPercent bool) (absPath, decodedPath string, err error) {
	// URL 解碼 — loop until idempotent (cap 4 層深度) 以擋雙重編碼繞過：
	// 單層 decode 對 `..%252Fetc%252Fpasswd` 只變成 `..%2Fetc%2Fpasswd`，
	// HasTraversalElement 依 / 與 \ split 不會切到 `%2F`，整段被當成單一 element
	// `..%2Fetc...` 比對 `..` 失敗，攻擊 bypass。loop decode 直到不變、或設深度
	// 上限（防超長 encoded payload 形成 DoS 與無限解碼）。
	decoded := path
	for i := 0; i < 4; i++ {
		next, decodeErr := url.QueryUnescape(decoded)
		if decodeErr != nil || next == decoded {
			break
		}
		decoded = next
	}
	// strictPercent=true 時,仍含 literal `%` 視為「無法完全 decode 的可疑 input」直接拒;
	// 適用 ValidateFilePath。ValidateExternalPath 走 false 放行合法字面 % 檔名。
	if strictPercent && strings.Contains(decoded, "%") {
		return "", decoded, fmt.Errorf("%w: 路徑含 URL-encoded 殘留：%s", ErrPathTraversal, decoded)
	}

	// Pre-Clean element check：interior `..`（例如 `./input/../output/test.csv`）
	// 在 Clean 後會被解析消去，但這種跨目錄寫法本身就值得擋，因此在 Clean 之前
	// 先 split 比對 `..` element。filename 含字面雙點（`report..v2.csv`）的 path
	// element 不會被誤拒。
	if HasTraversalElement(decoded) {
		return "", decoded, fmt.Errorf("%w: %s", ErrPathTraversal, decoded)
	}

	cleanPath := filepath.Clean(decoded)

	abs, absErr := filepath.Abs(cleanPath)
	if absErr != nil {
		return "", decoded, fmt.Errorf("無法解析路徑 '%s': %w", decoded, absErr)
	}

	if HasTraversalElement(abs) {
		return "", decoded, fmt.Errorf("%w: %s", ErrPathTraversalAbs, abs)
	}

	return abs, decoded, nil
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

// filterTraversalElements rebuilds path stripping any path element that is
// literally `..`. Filenames that merely contain `..` as a substring (e.g.
// `report..v2.csv`) are preserved — substring-stripping would mangle them
// into different paths and could redirect file ops to unintended targets.
func filterTraversalElements(path string) string {
	if path == "" {
		return path
	}

	leading := ""
	switch {
	case strings.HasPrefix(path, "/"):
		leading = "/"
	case strings.HasPrefix(path, "\\"):
		leading = "\\"
	}

	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	filtered := parts[:0]
	for _, part := range parts {
		if part != ".." {
			filtered = append(filtered, part)
		}
	}

	if len(filtered) == 0 {
		return leading
	}
	return leading + strings.Join(filtered, string(filepath.Separator))
}

// ValidateDirectoryPath validates that a directory path is within allowed directories.
func (pv *PathValidator) ValidateDirectoryPath(path string) error {
	return pv.ValidateFilePath(path)
}

// IsCSVFile checks if the file has a .csv extension.
//
// 取 Ext 前先 TrimRight 把尾端空白與點剝掉:Excel 匯出 / Windows 拖拉常在檔名
// 尾端留 trailing space 或 dot,這類檔名仍能 open,validator 不該誤判為非 CSV。
//
// 本函式只判斷副檔名類別,不負責 sanitize;caller 若要實際開檔請走
// PathValidator.SanitizePath / SafePath。
func IsCSVFile(path string) bool {
	return strings.ToLower(filepath.Ext(strings.TrimRight(path, " ."))) == ".csv"
}

// IsCSVFile checks if the file has a .csv extension (method wrapper).
func (*PathValidator) IsCSVFile(path string) bool {
	return IsCSVFile(path)
}

// charReplacement is a deterministic (from, to) replacement entry used by
// SanitizePath. Slice (not map) is required: Go map iteration is randomized,
// which lets overlapping patterns (e.g. `....//foo.csv` matching both `../`
// and `./`) produce different outputs across invocations. Determinism matters
// for log/audit reproducibility and for callers that hash the sanitized output.
type charReplacement struct {
	From string
	To   string
}

// dangerousReplacements lists the ordered sanitization replacements applied
// by SanitizePath. Order is load-bearing:
//  1. Path traversal patterns (`../`, `..\`) run first so they're removed
//     before any sub-pattern (`./`, `.\`) can chew them apart and leave a
//     stray `..` that filterTraversalElements would then strip out of a
//     legitimate filename.
//  2. Single-dot prefix patterns (`./`, `.\`) follow.
//  3. Multi-separator normalization (`//`, `\\`) runs after traversal removal
//     so collapsing slashes doesn't merge two unrelated path segments into a
//     new `..` pattern.
//  4. Control-character stripping runs last; replacement order among these
//     is order-independent because none overlap.
//
//nolint:gochecknoglobals // intentional package-private constant table
var dangerousReplacements = []charReplacement{
	// Traversal patterns first (longer/more specific runs before subsets).
	{From: "../", To: ""},  // 路徑遍歷
	{From: "..\\", To: ""}, // Windows 路徑遍歷
	{From: "./", To: ""},   // 當前目錄引用
	{From: ".\\", To: ""},  // Windows 當前目錄引用
	// Separator normalization after traversal removal.
	{From: "//", To: "/"},    // 多重斜線規範化
	{From: "\\\\", To: "\\"}, // 多重反斜線規範化
	// Control characters (no overlap among themselves).
	{From: "\x00", To: ""}, // Null bytes
	{From: "\r", To: ""},   // Carriage return
	{From: "\n", To: ""},   // Newline
	{From: "\t", To: ""},   // Tab
	{From: "\x0B", To: ""}, // Vertical tab
	{From: "\x0C", To: ""}, // Form feed
	{From: "\x1C", To: ""}, // File separator
	{From: "\x1D", To: ""}, // Group separator
	{From: "\x1E", To: ""}, // Record separator
	{From: "\x1F", To: ""}, // Unit separator
}

// SanitizePath sanitizes a file path by detecting dangerous characters.
//
// Silent rewrite is NOT allowed: if input contains stripped characters or
// matched traversal patterns, returns ErrPathSanitizationRequired (empty
// string) — callers must retry with a clean filename or surface the
// rejection. Silent rewrite is an attack vector (`...//etc/passwd` and
// `report\x01..csv` post-sanitize forms could pass element-based traversal
// checks but resolve to different files).
//
// dangerousReplacements is used as a detection table; clean inputs go through
// filepath.Clean + ToSlash for normalization (separator collapsing is not a
// security-relevant rewrite).
func SanitizePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	// URL 解碼防止編碼繞過。decode 後若仍含 `%` 形式殘留,我們不在此 layer 攔
	// (validatePathFormat 走 strict % 守門);但 decode 失敗的 input 不允許
	// 進入 sanitization 流程 — 那已是無法判讀的 bytes,reject 比 best-effort 安全。
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		return "", fmt.Errorf("%w: 路徑 URL-decode 失敗: %v", ErrPathSanitizationRequired, err)
	}

	// 偵測階段:任一 dangerousReplacements 命中即視為「需淨化」,直接 reject。
	for _, rep := range dangerousReplacements {
		if strings.Contains(decodedPath, rep.From) {
			return "", fmt.Errorf("%w: 命中 dangerous pattern %q",
				ErrPathSanitizationRequired, rep.From)
		}
	}

	// 控制字元二次檢查 — dangerousReplacements 已涵蓋 \x00 / \r / \n / \t /
	// \x0B / \x0C / \x1C-\x1F,但 U+007F-U+009F 區段歷史上是用「白名單字元」
	// 過濾掉而非顯式拒絕。改為「發現任何非允許字元就 reject」與上一段守門一致。
	for _, r := range decodedPath {
		if (r >= 0x0020 && r <= 0x007E) || // ASCII 可見字符
			(r >= nonASCIIVisible) || // 非 ASCII 但可見的字符
			r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' { // 路徑相關的特殊字符
			continue
		}
		return "", fmt.Errorf("%w: 路徑含不允許的字元 U+%04X",
			ErrPathSanitizationRequired, r)
	}

	// 走到這裡:input 已乾淨,只做 Clean + ToSlash 正規化(無 character drop)。
	// filterTraversalElements 仍呼一次當守門 — element-based check 與 dangerous
	// pattern detection 是互補的:前者擋 element 為 `..` 的 case;後者擋
	// substring `../` / `..\` / 控制字元。dangerousReplacements 已含 `../` 與 `..\`,
	// 所以走到此處不應有 element-`..` 出現,但留 defense-in-depth check。
	finalPath := filepath.Clean(decodedPath)
	if HasTraversalElement(finalPath) {
		return "", fmt.Errorf("%w: Clean 後仍含 `..` element", ErrPathSanitizationRequired)
	}

	// 強制 forward-slash 輸出,讓 Windows 與 Unix 中間值一致。下游 caller 接
	// filepath.Abs/Join 會再 normalize 成 OS native separator,end-to-end 行為不變。
	return filepath.ToSlash(finalPath), nil
}

// SanitizePath sanitizes a file path (method wrapper).
func (*PathValidator) SanitizePath(path string) (string, error) {
	return SanitizePath(path)
}

// GetSafePath returns a safe path within the allowed directories.
func (pv *PathValidator) GetSafePath(basePath, filename string) (string, error) {
	if err := pv.ValidateDirectoryPath(basePath); err != nil {
		return "", fmt.Errorf("基礎路徑無效: %w", err)
	}

	// 在清理之前檢查文件名是否包含路徑遍歷攻擊；改為 element-based 比對，
	// 與 validatePathFormat 同步（substring `..` 會誤拒 `report..v2.csv` 等合法檔名）。
	// 先 URL-decode 才比對：攻擊者常用 `..%2F..%2F` 繞過原始字串 check。
	decodedFilename, decodeErr := url.QueryUnescape(filename)
	if decodeErr != nil {
		decodedFilename = filename
	}

	if HasTraversalElement(decodedFilename) {
		return "", fmt.Errorf("%w: %s", ErrFilenameTraversal, filename)
	}

	// SanitizePath 失敗時 propagate 給 caller — invalid 是「沒料」,sanitize
	// 失敗是「原料髒」,語意不同不該 fall back 到 ErrFilenameInvalid。
	safeFilename, err := SanitizePath(filename)
	if err != nil {
		return "", fmt.Errorf("檔名 %q 淨化失敗: %w", filename, err)
	}

	// 檢查清理後的文件名是否為空或只包含無效字符
	if safeFilename == "" || safeFilename == "." {
		return "", fmt.Errorf("%w: %s", ErrFilenameInvalid, filename)
	}

	// Join paths safely
	fullPath := filepath.Join(basePath, safeFilename)

	// Validate the final path
	if err := pv.ValidateFilePath(fullPath); err != nil {
		return "", fmt.Errorf("最終路徑無效: %w", err)
	}

	return fullPath, nil
}

// isPathWithinBase 檢查目標路徑是否在基礎路徑內，支援長絕對路徑.
func isPathWithinBase(targetPath, basePath string) bool {
	// 獲取基礎路徑的絕對路徑
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}

	// 標準化路徑分隔符
	absBasePath = filepath.Clean(absBasePath)
	targetPath = filepath.Clean(targetPath)

	// 使用 filepath.Rel 檢查相對關係
	rel, err := filepath.Rel(absBasePath, targetPath)
	if err != nil {
		return false
	}

	// 檢查相對路徑是否有效（不包含 .. 且不是絕對路徑）
	return !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator))
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

// SetAllowedBasePaths 動態設置允許的基礎路徑(支援長路徑),寫鎖保護避免並行
// 呼叫者覆蓋彼此設定。
//
// 拒絕 nil / 空 slice / 全為空白字串的 slice — 接受 empty 會 silently 把
// allow-list 清空,等同無聲關閉白名單;若確實要「無白名單」模式,請改用
// `NewPathValidator(nil)` 在建構時表達意圖。
//
// 回傳 ErrAllowedBasePathsEmpty 時保持原有 allow-list 不變(atomic),避免半套狀態。
func (pv *PathValidator) SetAllowedBasePaths(paths []string) error {
	// frozen 守門提早 reject,避免下方 Abs 等 work 白做。
	pv.mu.RLock()
	frozen := pv.frozen
	pv.mu.RUnlock()
	if frozen {
		return ErrValidatorFrozen
	}

	absPaths := make([]string, 0, len(paths))

	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			// 如果無法獲取絕對路徑，使用清理後的原始路徑
			absPath = filepath.Clean(path)
		}

		absPaths = append(absPaths, absPath)
	}

	if len(absPaths) == 0 {
		return ErrAllowedBasePathsEmpty
	}

	// 二次檢查 frozen — 雙 mutex acquisition 之間理論上可能被 markFrozen 截胡,
	// 保守起見再驗一次。
	pv.mu.Lock()
	defer pv.mu.Unlock()
	if pv.frozen {
		return ErrValidatorFrozen
	}
	pv.allowedBasePaths = absPaths

	return nil
}

// GetAllowedBasePaths 獲取當前允許的基礎路徑.
func (pv *PathValidator) GetAllowedBasePaths() []string {
	pv.mu.RLock()
	defer pv.mu.RUnlock()

	// 返回副本以防止外部修改
	result := make([]string, len(pv.allowedBasePaths))
	copy(result, pv.allowedBasePaths)

	return result
}
