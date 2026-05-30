// Package manifest 集中 phase manifest 載入與 EMG 檔案路徑解析的共用流程。
//
// cci 與 muscle_ratio 兩個 analyzer 共用：
//   - 載入分期總檔（manifest CSV）
//   - 把 manifest 內相對檔名解析為 baseFolder 下的絕對路徑、檢查存在性
//
// 路徑解析整合 security.ResolveLenientPath（允許 BTS 匯出含字面 "%" 的檔名），
// 不要 bundle 後續 ParseFile / 迭代 / 錯誤處理 — caller 行為刻意不同（cci 是 fail-fast，
// muscle_ratio 是 per-subject batch）。
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
)

// sentinel errors 讓 caller 可用 errors.Is 區分原因。原本 fmt.Errorf
// 動態字串 caller 只能 strings.Contains 比對，重構時 message 一改就 break。
var (
	ErrManifestEMGPathInvalid = errors.New("EMG 檔案路徑驗證失敗")
	ErrManifestEMGFileMissing = errors.New("EMG 檔案不存在")
)

// data-file sentinels:OpenDataFile 這道門服務 EMG / Motion / Force / muscle_ratio
// 四類 manifest 資料檔，message 採泛化「資料檔案…」措辭（非 EMG-specific）。
// caller 用 errors.Is 區分三類失敗：路徑驗證失敗（traversal/escape/絕對路徑/null-byte/
// 過長）vs 檔案不存在 vs baseFolder 本身無法解析。
var (
	ErrManifestDataFileMissing     = errors.New("資料檔案不存在")
	ErrManifestDataFilePathInvalid = errors.New("資料檔案路徑驗證失敗")
	ErrBaseFolderNotFound          = errors.New("baseFolder 不存在或無法解析")
)

// LoadManifests 解析分期總檔案，回傳所有 manifest 紀錄。
func LoadManifests(filepath string) ([]models.PhaseManifest, error) {
	return parsers.NewPhaseManifestParser().ParseFile(filepath)
}

// ResolveEMGFile 把 manifest.EMGFile 相對檔名解析為 baseFolder 下的絕對路徑。
// 整合：EvalSymlinks(baseFolder) → security.ResolveLenientPath → os.Stat IsNotExist。
// 錯誤 wraps sentinel ErrManifestEMGPathInvalid / ErrManifestEMGFileMissing，
// caller 可用 errors.Is 區分（路徑驗證失敗 vs. 檔案不存在）。
func ResolveEMGFile(baseFolder, emgFile string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolved
	}

	emgPath, err := security.ResolveLenientPath(baseFolder, emgFile)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrManifestEMGPathInvalid, err)
	}

	if _, err := os.Stat(emgPath); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrManifestEMGFileMissing, emgPath)
	}

	return emgPath, nil
}

// OpenDataFile 是開啟 manifest 引用之資料檔（EMG / Motion / Force / muscle_ratio）的
// **單一加固讀檔門**：整合過去散落的 ResolveEMGFile 與手寫 security.ResolveLenientPath
// 站點，把「解析路徑 → 邊界檢查 → 原子化開檔」收斂成一道入口。
//
// 流程：EvalSymlinks(baseFolder) → security.OpenLenientValidated（lenient 解析 + 原子
// validated-open，fused 一道門關閉 validate-vs-open TOCTOU 縫隙）。回傳「已開啟且已驗證」
// 的 *os.File，**caller 必須自行 defer Close**（與回傳 path 的 ResolveEMGFile 不同，這裡
// 直接交出開好的 fd，避免 caller 拿 path 重開時再生 TOCTOU）。
//
// # 錯誤分類契約（caller 用 errors.Is 區分）
//
//   - ErrBaseFolderNotFound — baseFolder 本身無法 EvalSymlinks（不存在 / 壞 symlink）。
//     刻意與 ResolveEMGFile 不同：ResolveEMGFile 容忍無法解析的 base（解析失敗時保留原值
//     續走），本門明確 fail-closed。
//   - ErrManifestDataFileMissing — 路徑合法但檔案不存在（OpenLenientValidated 的 ENOENT
//     經雙層 %w 包裝仍保留 os.ErrNotExist，故以 errors.Is(err, os.ErrNotExist) 判定）。
//   - ErrManifestDataFilePathInvalid — 其餘一切「路徑本身有問題」的 catch-all：traversal
//     (".." element) / 絕對路徑 / null-byte / 過長 / 解析後落在 base 外（fsperm 的
//     ErrPathEscapesBase）。這些在 lenient 解析或 open 階段被擋，皆非 os.ErrNotExist。
//
// # 字面 "%" 不可 regress
//
// BTS EMG 匯出檔名常含字面 "%"（例 "SF_8_BTS%_*.csv"）。OpenLenientValidated 端到端接受
// 字面 "%"，本門不另加 "%" 處理 — 只需不誤拒。
func OpenDataFile(baseFolder, filename string) (*os.File, error) {
	resolvedBase, err := filepath.EvalSymlinks(baseFolder)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBaseFolderNotFound, baseFolder)
	}

	f, err := security.OpenLenientValidated(resolvedBase, filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// wrap inner err (含解析後的 *os.PathError 路徑),保留 MissingRow UI
			// 「期待的檔放在哪」affordance;雙層 %w 不影響 errors.Is(ErrManifestDataFileMissing)。
			return nil, fmt.Errorf("%w: %w", ErrManifestDataFileMissing, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrManifestDataFilePathInvalid, err)
	}

	return f, nil
}
