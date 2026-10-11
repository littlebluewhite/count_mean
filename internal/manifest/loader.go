// Package manifest 是 [[Manifest]] row + 資料夾 → [[Subject source]] 的共用入口。
//
// cci、muscle_ratio、phase_sync 三個 [[Domain analyzer]] 與 [[Chart Composer]] 共用：
//   - LoadManifests：載入分期總檔（manifest CSV）
//   - LoadSubjectNames / LoadSubject：以名稱列出、取得一個 Subject 的 row（取得時驗證該列，ADR-0057）
//   - LoadEMG：一個 manifest row + 資料夾 → 該 Subject 的 EMG（開檔、解析、關檔）
//   - OpenDataFile：把 manifest 內相對檔名解析為 baseFolder 下的路徑、邊界檢查、原子化開檔
//
// 兩道讀檔門(ADR-0056):分期總檔案本身走 LoadManifests,資料檔走 OpenDataFile →
// security.OpenLenientValidated（允許 BTS 匯出含字面 "%" 的檔名）。
// 本套件只負責「載入」並回傳 (data, err)；caller 對錯誤的處置仍各自決定（cci 是 fail-fast，
// muscle_ratio 是 per-subject batch，Composer 轉成 UI 訊息），LoadEMG 不替它們選 policy。
// 見 [[ADR-0044]]。
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/validation/filename"
)

// data-file sentinels:OpenDataFile 這道門服務 EMG / Motion / Force / muscle_ratio
// 四類 manifest 資料檔，message 採泛化「資料檔案…」措辭（非 EMG-specific）。
// caller 用 errors.Is 區分三類失敗：路徑驗證失敗（檔名不合法/traversal/escape/絕對路徑/
// null-byte/非一般檔案/過大）vs 檔案不存在 vs baseFolder 本身無法解析。
var (
	ErrManifestDataFileMissing     = errors.New("資料檔案不存在")
	ErrManifestDataFilePathInvalid = errors.New("資料檔案路徑驗證失敗")
	ErrBaseFolderNotFound          = errors.New("baseFolder 不存在或無法解析")
)

// LoadManifests 是分期總檔案的讀檔門(ADR-0056),依序:
//
//  1. ValidateExternalPath:traversal、系統敏感位置(含 symlink resolve)。
//  2. filename.ValidateName(Base):檔名規則只由 filename 套件裁判(ADR-0054)。
//  3. fsperm.OpenRegularLimited:必須是一般檔案(FIFO 不阻塞)且 ≤ 100 MB,讀取中變大也封頂。
//  4. PhaseManifestParser.Parse。
//  5. 撞名檢查:兩列 Subject 的 filename.SubjectOutputKey 相同即整份拒絕(含兩列都是空 Subject)。
//
// 所有讀分期總檔的路徑(cci / muscle_ratio / phase_sync / Composer)都經過這裡。編碼不檢查:
// 真實 V.16 manifest 是 Big5。
func LoadManifests(path string) ([]models.PhaseManifest, error) {
	if err := security.DefaultValidator().ValidateExternalPath(path); err != nil {
		return nil, fmt.Errorf("路徑驗證失敗: %w", err)
	}

	if err := filename.ValidateName(filepath.Base(path)); err != nil {
		return nil, err
	}

	f, err := fsperm.OpenRegularLimited(path, fsperm.MaxReadBytes)
	if err != nil {
		return nil, fmt.Errorf("無法開啟檔案 %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	manifests, err := parsers.NewPhaseManifestParser().Parse(f, filepath.Base(path))
	if err != nil {
		return nil, err
	}

	if err := assertUniqueSubjectOutputs(manifests); err != nil {
		return nil, err
	}

	return manifests, nil
}

// assertUniqueSubjectOutputs 拒絕兩列 Subject 會寫到同一個輸出檔的 manifest:輸出檔名由
// filename.SubjectOutputName 從 Subject 推導,兩列的 SubjectOutputKey 相同時,後寫的會靜默
// 覆蓋先寫的("A/B" 與 "A:B" 都寫成 "A_B_muscle_ratio.csv";"SF8" 與 "sf8"、NFC 與 NFD 的
// "café" 在 macOS / Windows 上是同一檔)。
func assertUniqueSubjectOutputs(manifests []models.PhaseManifest) error {
	seen := make(map[string]string, len(manifests))

	for _, m := range manifests {
		key := filename.SubjectOutputKey(m.Subject)
		if prev, exists := seen[key]; exists {
			return i18n.NewError(i18n.KeyErrorManifestSubjectCollision, prev, m.Subject, filename.Sanitize(m.Subject))
		}

		seen[key] = m.Subject
	}

	return nil
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
// 流程：EvalSymlinks(baseFolder) → filename.ValidateName(path.Base(name))（先把 `\` 正規化成 `/`,
// Windows 產生的 manifest 可能用 `\` 分隔子目錄;用 path.Base 各 OS 一致,ADR-0056）→ security.OpenLenientValidated
// （lenient 解析 + 原子 validated-open，fused 一道門關閉 validate-vs-open TOCTOU 縫隙）→
// fsperm.CheckRegularSize（fstat:一般檔案且 ≤ 100 MB;失敗先 Close 再回錯）。回傳「已開啟
// 且已驗證」的 *os.File，**caller 必須自行 defer Close**（直接交出開好的 fd，避免 caller 拿
// path 重開時再生 TOCTOU;phase_sync 用 f.Name() 取解析後路徑）。大小只在開檔時檢查,
// xlsx 解壓後的大小不在此限。
//
// # 錯誤分類契約（caller 用 errors.Is 區分）
//
//   - ErrBaseFolderNotFound — baseFolder 本身無法 EvalSymlinks（不存在 / 壞 symlink），
//     本門明確 fail-closed。
//   - ErrManifestDataFileMissing — 路徑合法但檔案不存在（OpenLenientValidated 的 ENOENT
//     經雙層 %w 包裝仍保留 os.ErrNotExist，故以 errors.Is(err, os.ErrNotExist) 判定）。
//   - ErrManifestDataFilePathInvalid — 其餘一切「路徑本身有問題」的 catch-all：檔名不合法
//     （ValidateName）/ traversal (".." element) / 絕對路徑 / null-byte / 解析後落在 base 外
//     （fsperm 的 ErrPathEscapesBase）/ 非一般檔案（另命中 fsperm.ErrNotRegular）/ 過大
//     （另命中 fsperm.ErrTooLarge）。這些皆非 os.ErrNotExist。
//
// # 字面 "%" 不可 regress
//
// BTS EMG 匯出檔名常含字面 "%"（例 "SF_8_BTS%_*.csv"）。ValidateName 與
// OpenLenientValidated 都接受字面 "%"，本門不另加 "%" 處理 — 只需不誤拒。
func OpenDataFile(baseFolder, name string) (*os.File, error) {
	resolvedBase, err := filepath.EvalSymlinks(baseFolder)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBaseFolderNotFound, baseFolder)
	}

	// name 是 manifest 內的相對檔名、不是本機路徑:`\` 正規化成 `/` 後用 path.Base(只認 `/`),
	// 各 OS 結果相同。filepath.Base 在 Windows 會先剝掉 volume,`C:emg.csv` 變 `emg.csv`,
	// 磁碟代號規則就不會觸發。
	if err := filename.ValidateName(path.Base(strings.ReplaceAll(name, `\`, "/"))); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrManifestDataFilePathInvalid, err)
	}

	f, err := security.OpenLenientValidated(resolvedBase, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// wrap inner err (含解析後的 *os.PathError 路徑),保留 MissingRow UI
			// 「期待的檔放在哪」affordance;雙層 %w 不影響 errors.Is(ErrManifestDataFileMissing)。
			return nil, fmt.Errorf("%w: %w", ErrManifestDataFileMissing, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrManifestDataFilePathInvalid, err)
	}

	if err := fsperm.CheckRegularSize(f, fsperm.MaxReadBytes); err != nil {
		_ = f.Close() //nolint:errcheck // 已在錯誤路徑;read-only fd 的 close 錯誤不影響結果
		return nil, fmt.Errorf("%w: %w", ErrManifestDataFilePathInvalid, err)
	}

	return f, nil
}
