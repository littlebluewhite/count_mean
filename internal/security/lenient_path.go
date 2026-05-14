package security

import (
	"fmt"
	"path/filepath"
	"strings"
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
// # i18n strategy（P3-E Phase 4 — A2 abstraction）
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

	if filepath.IsAbs(normalized) {
		return "", fmt.Errorf("檔名不可為絕對路徑: %s", filename)
	}

	// 複用 security.HasTraversalElement：split on / and \，比對 ".." element（非 substring），
	// 與 PathValidator 對 ".." 的處理對稱（不會誤拒 "report..v2.csv" 這類合法檔名）。
	if HasTraversalElement(normalized) {
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
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("檔案路徑落在資料夾外 (含 symlink 解析): %s", filename)
	}

	// 回傳原 lexical joined：caller 仍透過 fsperm.ReadFlags 含 O_NOFOLLOW 開最終 component；
	// 安全 boundary 已由上面 resolved Rel 檢查保證。
	return joined, nil
}

// evalSymlinksLenient 是 filepath.EvalSymlinks 的 lenient 版本：
// 若 path 本身不存在（典型 case：output file 尚未建立），fall back 到 EvalSymlinks(parent) 後
// 接 base name。這允許「先建檔路徑、後寫檔」的流程不會在 path resolution 時誤報。
//
//nolint:err113 // dynamic errors for user-facing output
func evalSymlinksLenient(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	parent := filepath.Dir(path)

	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("parent dir 無法解析: %w", err)
	}

	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}
