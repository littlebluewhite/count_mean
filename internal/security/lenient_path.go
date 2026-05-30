package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"count_mean/internal/security/fsperm"
)

// ResolveLenientPath joins baseFolder + filename for the manifest-driven EMG loading scenario,
// defending against path traversal but **allowing literal "%" in the filename**.
//
// # Decision matrix（lenient vs strict）
//
// 用哪個 helper 的判斷依據：
//
//	| 情境                                                      | 用哪個                            |
//	|-----------------------------------------------------------|-----------------------------------|
//	| manifest-driven user files、檔名可能含 vendor encoding     | security.ResolveLenientPath       |
//	| （例：BTS EMG 匯出 "SF_8_BTS%_*.csv"，字面 "%" 屬正常）    | （此檔）                          |
//	|                                                           |                                   |
//	| internal / config / 直接 user-input 的 path               | security.PathValidator.GetSafePath|
//	| （受控路徑，URL-decode 後殘留 "%" 視為可疑）              | （pathvalidator.go）              |
//	|                                                           |                                   |
//	| Output 目錄、批次寫檔目標                                  | security.PathValidator.NewPathValidator + ValidateExternalPath |
//	| （防 traversal + 系統敏感目錄 prefix）                    |                                   |
//
// 跨平台 trust assumption：兩者皆要求 baseFolder / allowedBasePaths 為 user-selected 可信路徑。
//
// # 與 PathValidator.GetSafePath 的取捨
//
//   - PathValidator (pathvalidator.go:144 附近) 對 URL-decode 後仍含 literal "%" 的 path
//     一律拒絕，視為「無法完全 decode 的可疑 input」（Wave 7 anti-bypass design）。對 user-input
//     path 是合理的；但對「manifest CSV 指定 EMG 檔名」這條 path，BTS 匯出檔常包含字面 "%"
//     （例 "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"），用 GetSafePath 會把整批 subject 都
//     誤判為「URL-encoded 殘留」並拒絕，導致 muscle ratio / CCI 對標準資料完全跑不動。
//
//   - 本函式保留 PathValidator 的核心防護（".." element / 絕對路徑 / 結果落在 baseFolder 外）
//     但接受字面 "%"。CCI 與 muscle_ratio 兩個 analyzer 都改 call 它。
//
// **Threat model**：caller 已確認 baseFolder 是 user-selected directory（可信），filename 來自
// manifest CSV（半可信，可能含 BTS 的奇怪檔名但不應有 ".."）。本函式不檢查 baseFolder 本身是否
// 安全 — 由 caller 確保（典型呼叫端先做 EvalSymlinks）。
//
// # i18n strategy（Phase 4 — A2 abstraction）
//
// 本檔 error messages 為內部 technical detail（"檔名為空"、"路徑過長 > 4096"、
// "包含 null byte" 等），**不進 i18n catalog**。理由：
//   - User 看技術細節無 actionable value，應看上層 abstracted message
//   - security-critical 套件耦合 i18n 過深，每加 validation rule 都得動 catalog
//   - 11 條 × 4 locales = 44 entries 純粹膨脹 catalog 與 binary
//
// User-facing message 由上層 wrapper 提供，例如 muscle_ratio/analyzer.go 的
// `result.Error = i18n.T(KeyErrorMuscleRatioSubjectParseEMGFailed, err)` — user
// 主要看到的就是 wrapper 提供的 abstracted message，底層 detail 進 log 即可。
//
// 新增 validation rule 時若需 i18n message，於上層 wrap，不在此檔加 catalog 依賴。
//
//nolint:err113 // dynamic errors for user-facing output
func ResolveLenientPath(baseFolder, filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("檔名為空")
	}

	// Normalize Windows-style "\" → "/" 後再做後續處理。
	// 既有 HasTraversalElement 已把 "\" 視為 separator 偵測 ".." element；但 filepath.Join
	// 在 Unix 不認 "\"，會把 "trial1\\emg.csv" 當成單一檔名（含 literal "\"）開檔失敗。
	// Cross-platform 場景：manifest 在 Windows 端產生，用於 macOS/Linux 端執行。
	normalized := strings.ReplaceAll(filename, `\`, "/")

	// Null byte：os.OpenFile 會拒，但 error path 會把未清理的 byte 寫進 log（observability
	// 污染風險）。提前擋。
	if strings.ContainsRune(normalized, 0) {
		return "", fmt.Errorf("檔名包含 null byte: %q", filename)
	}

	// Whitespace-only / dot-only：filepath.Clean(".") 與 Clean(strings.TrimSpace("  ")) 都
	// 得到 "." 或 ""，會被 Join 成 baseFolder 本身（caller 後續 OpenFile 在 Unix 對目錄成功，
	// 行為未定）。明確拒絕。
	cleaned := filepath.Clean(strings.TrimSpace(normalized))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("檔名無效（空 / 純 whitespace / dot-only）: %q", filename)
	}

	// 長度上限：Wave 7 (PathValidator.GetSafePath) 對 absPath/filename 已有 4096/255 守門；
	// lenient 路徑取代後曾遺失此防護，回補。
	if base := filepath.Base(cleaned); len(base) > maxFilenameLength {
		return "", fmt.Errorf("檔名過長（> %d）: %d 字元", maxFilenameLength, len(base))
	}

	// 同時擋 native absolute path 與 Unix-style leading-slash:
	//   - filepath.IsAbs 在 Windows 只認 `C:\...` / UNC,**不**認 `/etc/passwd`
	//     → manifest 在 Windows 引用 "/etc/passwd" 會被誤判為 relative,join 後可能
	//     落回 baseFolder/etc/passwd(視 filepath.Join 對 leading-`/` 的處理)。
	//   - Threat model:manifest 半可信,絕對路徑(無論 OS 慣例)都不該出現。Unix-style
	//     `/...` 在 Windows 雖非語法 absolute 但**語義**上是「想跳到 root」的意圖,保守 reject。
	//   - macOS / Linux 上 `filepath.IsAbs("/etc/passwd")` = true,HasPrefix 是 redundant
	//     defense-in-depth,沒有 regress。
	if filepath.IsAbs(normalized) || strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("檔名不可為絕對路徑: %s", filename)
	}

	// 複用 security.HasTraversalElement：split on / and \，比對 ".." element（非 substring），
	// 與 PathValidator 對 ".." 的處理對稱（不會誤拒 "report..v2.csv" 這類合法檔名）。
	//
	// 三重檢查 — defense in depth。
	//   1. `normalized`：使用者實際輸入的 `..` element 在這裡能直接抓（原 line 96 行為）
	//   2. `cleaned`：filepath.Clean 後（理論上 Clean 不會「新生」 `..`，但若 cleaned 與
	//      normalized 分歧，例如未來新增 normalization 步驟，這層能補上漏洞）
	//   3. `hasTraversalElementTrimmed(normalized)`：把每個 path element 先 TrimSpace 再
	//      比對，攔下 `foo/ ../bar`、`foo/.. /bar` 等 whitespace-padded 的可疑變形。
	//      Unix/macOS 上「字面 ` ..` 目錄名」不是 traversal、但屬可疑 input（multi-pass
	//      decoder 或檔案系統 normalization 可能再剝除 whitespace），保守地拒絕。
	if HasTraversalElement(normalized) || HasTraversalElement(cleaned) ||
		hasTraversalElementTrimmed(normalized) ||
		hasTraversalElementZeroWidthStripped(normalized) {
		return "", fmt.Errorf("檔名包含 \"..\" 路徑元素: %s", filename)
	}

	joined := filepath.Clean(filepath.Join(baseFolder, normalized))
	cleanBase := filepath.Clean(baseFolder)

	if len(joined) > maxPathLength {
		return "", fmt.Errorf("路徑過長（> %d）: %d 字元", maxPathLength, len(joined))
	}

	// Symlink-aware boundary check：先把 joined 與 baseFolder 的 symlink 完全解析，再做 Rel 檢查。
	// O_NOFOLLOW 只擋「最終 component 是 symlink」（man 2 open: "only affects the interpretation
	// of the last component"），對「parent 是 symlink 指外部」無防護 — 例如 baseFolder/link/emg.csv
	// 其中 link → /etc 會在 lexical Rel 通過後仍實際讀 /etc/emg.csv。
	resolvedJoined, err := evalSymlinksLenient(joined)
	if err != nil {
		return "", fmt.Errorf("無法解析路徑 %s: %w", filename, err)
	}

	resolvedBase, err := filepath.EvalSymlinks(cleanBase)
	if err != nil {
		return "", fmt.Errorf("無法解析 baseFolder %s: %w", baseFolder, err)
	}

	rel, err := filepath.Rel(resolvedBase, resolvedJoined)
	// defense-in-depth:除 `..` prefix 之外,加 `!filepath.IsAbs(rel)` 擋
	// cross-volume Windows edge case。`filepath.Rel` 對跨 volume / UNC vs drive-letter
	// 等情境多會回 error,但極端 case 下可能 silently 回絕對路徑樣式 — 與
	// pathvalidator.go:540 的 `!strings.HasPrefix(rel, string(filepath.Separator))`
	// 對齊;IsAbs 還能擋 Windows `C:\` 形式 (HasPrefix("\") 漏)。
	if err != nil ||
		rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(rel) {
		return "", fmt.Errorf("檔案路徑落在資料夾外 (含 symlink 解析): %s", filename)
	}

	// 回傳原 lexical joined：caller 仍透過 fsperm.ReadFlags 含 O_NOFOLLOW 開最終 component；
	// 安全 boundary 已由上面 resolved Rel 檢查保證。
	return joined, nil
}

// OpenLenientValidated 是 manifest-driven EMG 讀檔的**單一 fused 安全入口**：
// 把 lenient 路徑解析與 atomic validated-open 併成一道門，所有 manifest data-file
// 讀取（Phase C/D 起經 manifest door）皆收斂於此。
//
// # 為何需要這道 fused 門（close validate-vs-open TOCTOU seam）
//
// ResolveLenientPath 回傳的是 **lexical** `joined`，但 boundary 檢查做在
// **EvalSymlinks-解析後**的 path（見 ResolveLenientPath 的 symlink-aware boundary check）。caller 若直接拿
// lexical path 去開檔，就出現「validate 解析後 / open 卻開 lexical」的 TOCTOU 縫隙：
// check 與 open 之間若有 parent-component symlink 被 swap 進來，open 會跟著它走；
// 而 ReadFlags 裡的 O_NOFOLLOW 只保護 **leaf** component，對中間 component 無效
// （man 2 open: "only affects the interpretation of the last component"）。
//
// fsperm.OpenReadValidated 在 open 階段 **重新解析並原子化開檔**
// （Linux openat2 + RESOLVE_BENEATH|RESOLVE_NO_MAGICLINKS|RESOLVE_NO_SYMLINKS；
// Darwin O_NOFOLLOW_ANY），把這道縫隙關掉。OpenLenientValidated 串起兩者，成為
// caller 唯一該呼叫的入口。
//
// # 字面 "%" 不可 regress
//
// BTS EMG 匯出檔名常含字面 "%"（例 "SF_8_BTS%_*.csv"）。ResolveLenientPath 刻意接受
// "%"（與 PathValidator.GetSafePath 不同），fsperm.OpenReadValidated 亦不做 URL-decode、
// 把 "%" 當普通 path byte，故端到端接受字面 "%"。
//
// # baseFolder 傳法
//
// baseFolder 原樣傳給 fsperm（[]string{baseFolder}）——fsperm.matchAnyBase 內部會自行
// EvalSymlinks 解析 base，不需也不應在此先解析或改 ResolveLenientPath 的簽章。
//
// 回傳：成功時 *os.File，caller 自行 Close；失敗時 nil + 包裝錯誤。ResolveLenientPath
// 的錯誤已是 well-formed 的 lenient 訊息，原樣 propagate。
func OpenLenientValidated(baseFolder, filename string) (*os.File, error) {
	joined, err := ResolveLenientPath(baseFolder, filename)
	if err != nil {
		return nil, err
	}
	f, err := fsperm.OpenReadValidated(joined, []string{baseFolder})
	if err != nil {
		return nil, fmt.Errorf("OpenLenientValidated: 開啟 %s 失敗: %w", filename, err)
	}
	return f, nil
}

// hasTraversalElementZeroWidthStripped 為 L13 補強的本地 helper：
// 對每個 split 後的 path element 剝除「零寬字元 / Unicode 空白 / Cf format chars」
// (**包括 element 內部任意位置,不只 edge**) 再比對 `..`。攔下 ZWSP / ZWJ / BOM / WJ
// 等 zero-width 偽裝的 traversal:
//
//	"foo/..<ZWSP>/bar"  → element[1] = "..<ZWSP>" — HasTraversalElement 比對 part=="\"..\""
//	失敗；但「strip 完 zero-width 後」此 element 就是純 "..", 屬於 traversal 必擋。
//	"foo/.<ZWJ>./bar"   → element[1] = ".<ZWJ>." — edge-only Trim 留下 .<ZWJ>.
//	(加強前): EvalSymlinks 失敗 → 順帶 reject (fragile,依賴 path 不存在);
//	(後):     遞迴 fallback 成功 → 必須由 internal-strip 抓出 mid-element 的零寬。
//
// 因此改用 strings.Map 全 element 剝除,而非 TrimFunc 僅 edge 剝除。
//
// 與 hasTraversalElementTrimmed 分離不合併的原因：
//   - Trim 系列已用 unicode.IsSpace 把 IDEOGRAPHIC space 等可見/不可見的 white-space
//     列入 trim 範圍；但 ZWSP/ZWJ/BOM/WJ 屬 \p{Cf} (format chars)，不在 White_Space。
//   - 在 lenient_path 局部加強，威脅模型較窄（manifest-driven path，整段視為單一
//     filename），保守拒絕代價低。HasTraversalElement 給多個 caller 共用，不適合一律
//     收緊（會把字面以 ZWJ 起頭的合法檔名也誤拒，雖然這種檔名極罕見）。
func hasTraversalElementZeroWidthStripped(path string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})

	// 全 element 剝除 (包括 mid-element)。輸入 element 含「零寬字元 + 任意位置 dot」
	// 拼出來的 `.<ZWJ>.` 必須在剝除後變成 `..` 才能命中守門。
	stripFn := func(r rune) rune {
		// Cf：零寬字元（ZWSP / ZWJ / ZWNJ / BOM）、word joiner、bidi controls 等
		// Cs：UTF-16 surrogate（不該出現在 path）
		// IsSpace：所有 Unicode whitespace（含 IDEOGRAPHIC space U+3000）
		if unicode.In(r, unicode.Cf, unicode.Cs) || unicode.IsSpace(r) {
			return -1 // strings.Map: return -1 → 此 rune 從輸出移除
		}
		return r
	}

	for _, part := range parts {
		stripped := strings.Map(stripFn, part)
		if stripped == ".." {
			return true
		}
	}
	return false
}

// hasTraversalElementTrimmed 是 為 lenient_path 補強的本地 helper：
// 對每個 split 後的 path element 先 strings.TrimSpace 再比對 `..`，攔下
// `foo/ ../bar`、`foo/.. /bar` 這類 whitespace-padded 的可疑變形。
//
// 不放進 pathvalidator.go 與 HasTraversalElement 合併的原因：HasTraversalElement
// 被多個 caller 共用，若改成「trim 再比」會把 ` ..filename.csv` 這類字面以空白起
// 頭的合法檔名也誤拒（low risk 但 unnecessary）。在 lenient_path 局部加強，威脅
// 模型更窄（manifest-driven path、整段視為單一 filename 而非可能含合法檔名 element），
// 保守拒絕代價低。
func hasTraversalElementTrimmed(path string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if strings.TrimSpace(part) == ".." {
			return true
		}
	}
	return false
}

// evalSymlinksLenientMaxDepth 限制 evalSymlinksLenient 沿 parent 往上遞迴的層數。
//
// 遞迴 fallback 必須有 fail-closed bound 避免病態構造的 path (極深 nesting)
// 觸發 unbounded recursion / stack growth。8 層遠超出 manifest-driven 流程典型深度
// (常見 1-3 層 OutputDir/subject/trial),legitimate use case 不會撞到上限。
const evalSymlinksLenientMaxDepth = 8

// evalSymlinksLenient 是 filepath.EvalSymlinks 的 lenient 遞迴版本:
//
// 若 path 本身不存在 (典型 case:output file 尚未建立),沿 parent 一路往上遞迴
// 找到 nearest existing dir,EvalSymlinks(dir) 後再接回原本 non-existent 的 child
// suffix。對齊 pathvalidator.go 的 resolveSymlinksWithFallback pattern,避免
// 跨檔行為不一致。
//
// # 為何需要遞迴 (非單層)
//
// 舊版只做單層 fallback,manifest-driven 流程常見「OutputDir/subject/trial/emg.csv」
// 這種 subject 與 trial 資料夾都尚未建立的情境,舊版會因 parent (subject) 也不存在
// 而 fail。新版逐層往上找到 OutputDir (存在) 才解析,符合「先建路徑、後寫檔」流程。
//
// # 為何需要 depth limit
//
// `filepath.Dir` 沿著 path 必然抵達 root (parent == path) 而終止,理論上 unbounded
// recursion 不會發生。但保守加 8 層上限作為 defense-in-depth — 病態構造的長 path
// (e.g. 攻擊者構造 1000 層 non-existent nesting) 即便 < maxPathLength=4096 也可
// 觸發深度遞迴。fail-closed 後上層收到 error 比 hang 死好處理。
//
//nolint:err113 // dynamic errors for user-facing output
func evalSymlinksLenient(path string) (string, error) {
	return evalSymlinksLenientDepth(path, evalSymlinksLenientMaxDepth)
}

// evalSymlinksLenientDepth 是 evalSymlinksLenient 的 inner helper,顯式帶 depth
// counter 避免 unbounded recursion。
//
//nolint:err113 // dynamic errors for user-facing output
func evalSymlinksLenientDepth(path string, depth int) (string, error) {
	if path == "" {
		return "", nil
	}
	if depth <= 0 {
		return "", fmt.Errorf("evalSymlinksLenient: 路徑遞迴層數超過上限 (%d): %s",
			evalSymlinksLenientMaxDepth, path)
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	parent := filepath.Dir(path)
	// 抵達 root (Unix "/" / Windows "C:\\") 仍無法解析 — 異常狀況 (連根都不通)。
	if parent == path {
		return "", fmt.Errorf("路徑根目錄無法解析: %s", path)
	}

	resolvedParent, err := evalSymlinksLenientDepth(parent, depth-1)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}
