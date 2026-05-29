// Package filename provides filename validation functionality.
package filename

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"count_mean/internal/errors"
	"count_mean/internal/validation/patterns"
)

// windowsDriveLetterPrefix 識別 Windows DOS-style drive-letter 前綴 (例 `C:foo.csv`)。
//
// 釘住:`C:test.csv` 在 Windows 上仍會被 OpenFile 解析為「drive C 的 current
// directory + test.csv」(legacy DOS drive-relative resolution),即使檔名不含
// path separator 也能跳脫 caller 認知的 base directory。
// 原本只靠 DetectDangerousChars 偵測 `:` 字元間接擋下,屬 fragile defense:若日後
// `:` 為了 ISO timestamp / time literal 等合法用途鬆綁,drive letter prefix 立刻 bypass。
//
// 採 anchor 在開頭的 `^[A-Za-z]:` regex,defense-in-depth:
//   - 不依賴 dangerous char list 的內容
//   - 提供獨立的明確 error message,日後 audit / log 容易追溯
//   - 涵蓋 single-letter drives (Windows 標準 A-Z),不誤擋 `AB:foo` 等非 drive 形式
//
//nolint:gochecknoglobals // 編譯期正規表達式,並發安全
var windowsDriveLetterPrefix = regexp.MustCompile(`^[A-Za-z]:`)

// Validator provides filename validation functionality.
type Validator struct {
	detector          *patterns.InjectionDetectorImpl
	allowedExtensions []string
}

// NewValidator creates a new filename validator with default extensions.
func NewValidator() *Validator {
	return &Validator{
		detector:          patterns.NewInjectionDetector(),
		allowedExtensions: []string{".csv"},
	}
}

// WithAllowedExtensions sets the allowed file extensions.
func (v *Validator) WithAllowedExtensions(extensions []string) {
	v.allowedExtensions = extensions
}

// GetAllowedExtensions returns the current allowed extensions.
func (v *Validator) GetAllowedExtensions() []string {
	return v.allowedExtensions
}

// ValidateFilename validates a *base* filename for safety and correctness.
//
// Contract: 本 validator 只驗 base filename — 含 path separator 即 reject。
// Path 安全（traversal、symlink）由 security.PathValidator 負責，不要混用。
//
// 防護面向：
//   - Empty / whitespace-only
//   - ASCII control chars (除 \t)
//   - Path separators `/` `\` （base filename 不可含）
//   - Unicode format chars (\p{Cf}) / surrogate (\p{Cs})：阻擋 RTL override
//     (U+202E) 顯示欺騙、ZWSP smuggling。
//   - Dangerous chars from registry pattern (Windows reserved chars 等)
//   - Windows reserved names (CON / PRN / COMx / LPTx)
//   - 長度上限 255
//   - 限定副檔名白名單
func (v *Validator) ValidateFilename(filename string) error {
	if filename == "" {
		return errors.NewValidationError("filename", filename, "檔案名稱不能為空")
	}

	// L15 — 在 TrimSpace 之前先擋 tab。TrimSpace 把前後 \t 剝掉，會讓
	// "\tdata.csv" / "data.csv\t" 這類 leading/trailing tab 偽裝通過後段的
	// control-char check。filename 含 tab 在 OS 各層（檔案總管顯示 / shell
	// completion / 拷貝貼上）都會壞掉，先擋更保守。中段 tab 由 checkControlChars
	// 額外覆蓋（已不再例外）。
	if strings.ContainsRune(filename, '\t') {
		return errors.NewValidationError("filename", filename, "檔案名稱不可包含 tab 字元")
	}

	filename = strings.TrimSpace(filename)
	if filename == "" {
		return errors.NewValidationError("filename", filename, "檔案名稱不能為空")
	}

	// Reject path separators — 本 validator 只接受 base filename。
	if strings.ContainsAny(filename, `/\`) {
		return errors.NewValidationError("filename", filename,
			"檔案名稱不可包含路徑分隔符（/ 或 \\）— 請改用 PathValidator")
	}

	// explicit Windows drive-letter prefix guard。
	// 在 control-char check 之前,確保 drive-letter 永遠有獨立的 error message,
	// 不依賴 `:` 仍被列為 dangerous char (defense-in-depth)。
	if windowsDriveLetterPrefix.MatchString(filename) {
		return errors.NewValidationError("filename", filename,
			"檔案名稱不可以 Windows 磁碟代號開頭 (drive letter, 例 C:foo.csv)")
	}

	// Check for null bytes, control characters, and Unicode format/surrogate
	// characters (RTL override, ZWSP 等視覺欺騙 vector).
	if err := v.checkControlChars(filename); err != nil {
		return err
	}

	// Check for dangerous characters
	if detected, char := v.detector.DetectDangerousChars(filename); detected {
		return errors.NewValidationError("filename", filename,
			fmt.Sprintf("檔案名稱包含非法字符: %s", char))
	}

	// Check for reserved names (Windows).
	//
	// V9 PoC:Windows 上 reserved device name 不限於「stem 緊接最後一個副檔名」的形式。
	// 原本實作只用 `TrimSuffix(filename, filepath.Ext(filename))` 剝最後一個副檔名,
	// `CON.txt.csv` -> `CON.txt` -> 比對 reserved list 不命中 -> 漏放;
	// 同理 `PRN.bak.csv`、`AUX.log.csv`、`NUL.dat.csv`、`COM1.x.csv`、`LPT1.y.csv` 全部 bypass。
	//
	// 修法:對 `strings.Split(filename, ".")` 每一段做 case-insensitive 比對 reserved list,
	// 任一段命中即 reject。與 `internal/calculator/emg_statistics.go` 的 first-segment 比對
	// 思路一致,但更嚴格(對全部 segments 而非只 first segment),涵蓋如 `foo.CON.csv` 這類
	// middle-segment 情境 — 雖然 Windows 對 middle segment 的 device routing 行為較弱,
	// 保守 reject 不會誤殺合法名稱(reserved list 是 `CON`/`PRN`/`COMx`/`LPTx` 等明確 short token,
	// 出現在 segment 中間幾乎都是 attacker 構造)。
	for _, segment := range strings.Split(filename, ".") {
		if segment == "" {
			continue
		}

		seg := strings.ToUpper(segment)
		if v.detector.IsReservedName(seg) {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱不能使用保留字: %s", seg))
		}
	}

	// Check length (max 255 characters, common filesystem limit)
	if len(filename) > 255 { //nolint:mnd // 255 is the standard max filename length
		return errors.NewValidationError("filename", filename, "檔案名稱過長 (最大 255 字符)")
	}

	// Check extension
	return v.checkExtension(filename)
}

// checkControlChars rejects null bytes, control characters, 以及 Unicode
// format / surrogate 字元。
//
// 為什麼擋 Unicode format chars (\p{Cf})：
//   - U+202E (RTL OVERRIDE) 可讓 "evil.exe.csv" 顯示為 "evilcsv.exe"
//   - U+200B (ZWSP)、U+200D (ZWJ)、U+2060 (WJ) 等 zero-width 字元可繞過
//     肉眼比對的 reserved-name / extension check
//   - Surrogate (\p{Cs}) 通常代表 mis-encoded UTF-16，filename 不應出現
//
// L15 — tab (\t) 不再例外：base filename 中的 tab character 在實務上不合理
// （檔案總管 / CLI 對含 tab 的檔名顯示破碎，shell completion 與拷貝貼上幾乎
// 一定壞掉）。cell-level CSV 的 \t 鬆綁是 CSV 內容的 quote/escape 需求,filename
// 不同 context, 統一收緊。
func (*Validator) checkControlChars(filename string) error {
	for _, r := range filename {
		if !isUnsafeFilenameRune(r) {
			continue
		}
		// classification lives in isUnsafeFilenameRune; re-test Cf/Cs only to pick the message。
		// \p{Cf} 包含 BOM(U+FEFF)、RTL override(U+202E)、bidi controls (U+2066-U+2069)、
		// ZWSP/ZWJ/WJ 等；\p{Cs} 為 UTF-16 surrogate。在 base filename context 都應 reject。
		if unicode.In(r, unicode.Cf, unicode.Cs) {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱包含禁用的 Unicode 格式字元: U+%04X", r))
		}
		return errors.NewValidationError("filename", filename, "檔案名稱包含非法控制字符")
	}

	return nil
}

// checkExtension validates the file extension.
func (v *Validator) checkExtension(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil
	}

	for _, allowedExt := range v.allowedExtensions {
		if ext == allowedExt {
			return nil
		}
	}

	return errors.NewValidationError("filename", filename,
		fmt.Sprintf("不支援的檔案副檔名: %s", ext))
}
