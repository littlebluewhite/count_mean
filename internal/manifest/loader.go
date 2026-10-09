// Package manifest 是 [[Manifest]] row + 資料夾 → [[Subject source]] 的共用入口。
//
// cci、muscle_ratio、phase_sync 三個 [[Domain analyzer]] 與 [[Chart Composer]] 共用：
//   - LoadManifests：載入分期總檔（manifest CSV）
//   - LoadEMG：一個 manifest row + 資料夾 → 該 Subject 的 EMG（開檔、解析、關檔）
//   - OpenDataFile：把 manifest 內相對檔名解析為 baseFolder 下的路徑、邊界檢查、原子化開檔
//
// 開檔走 OpenDataFile → security.OpenLenientValidated（允許 BTS 匯出含字面 "%" 的檔名）。
// 本套件只負責「載入」並回傳 (data, err)；caller 對錯誤的處置仍各自決定（cci 是 fail-fast，
// muscle_ratio 是 per-subject batch，Composer 轉成 UI 訊息），LoadEMG 不替它們選 policy。
// 見 [[ADR-0044]]。
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

// EMGParseError 表示 [[Subject source]] 的 EMG 檔已開成功、但解析失敗。
// LoadEMG 的開檔失敗直接回 OpenDataFile 的錯誤（errors.Is 三條 sentinel）；
// 解析失敗包成本型別，讓 caller 以 errors.As 區分「開檔」與「解析」兩階段，
// 各自套用既有的 user-facing 訊息。Error() 與內層錯誤逐字相同，不改變任何輸出文字。
type EMGParseError struct{ Err error }

func (e *EMGParseError) Error() string { return e.Err.Error() }

func (e *EMGParseError) Unwrap() error { return e.Err }

// LoadEMG 以 [[Manifest]] row 的 EMGFile 與資料夾載入該 [[Subject]] 的 EMG：
// OpenDataFile（硬化讀檔門）→ 解析 → Close。
//
// 不回傳取樣頻率：parser 的 frequency 與 CCI 的 sample interval 估計讀同一對 sample，
// 兩者本來就同源，沒有需要傳遞的額外資訊。
//
// 錯誤：開檔失敗原樣回 OpenDataFile 的錯誤；解析失敗回 *EMGParseError。
func LoadEMG(dataFolder string, row *models.PhaseManifest) (*models.PhaseSyncEMGData, error) {
	f, err := OpenDataFile(dataFolder, row.EMGFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	data, _, err := parsers.NewEMGParser().Parse(f, row.EMGFile)
	if err != nil {
		return nil, &EMGParseError{Err: err}
	}

	return data, nil
}

// OpenDataFile 是開啟 manifest 引用之資料檔（EMG / Motion / Force / muscle_ratio）的
// **單一加固讀檔門**：整合過去散落的手寫 security.ResolveLenientPath 站點，把
// 「解析路徑 → 邊界檢查 → 原子化開檔」收斂成一道入口。
//
// 流程：EvalSymlinks(baseFolder) → security.OpenLenientValidated（lenient 解析 + 原子
// validated-open，fused 一道門關閉 validate-vs-open TOCTOU 縫隙）。回傳「已開啟且已驗證」
// 的 *os.File，**caller 必須自行 defer Close**（直接交出開好的 fd，避免 caller 拿 path
// 重開時再生 TOCTOU）。
//
// # 錯誤分類契約（caller 用 errors.Is 區分）
//
//   - ErrBaseFolderNotFound — baseFolder 本身無法 EvalSymlinks（不存在 / 壞 symlink），
//     本門明確 fail-closed。
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
