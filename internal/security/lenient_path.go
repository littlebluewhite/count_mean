package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveLenientPath joins baseFolder + filename for the manifest-driven EMG loading scenario,
// defending against path traversal but **allowing literal "%" in the filename**.
//
// 與 PathValidator.GetSafePath 的取捨：
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
