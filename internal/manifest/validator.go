package manifest

import (
	"count_mean/internal/models"
)

// MissingRow 標示一筆 manifest row 對應的 EMG 檔在 disk 上開檔失敗(可能是
// 不存在,也可能是路徑驗證失敗如 traversal 等)。
//
// caller 可用 errors.Is(Err, ErrManifestDataFileMissing) 區分「檔案不存在」與
// 「路徑驗證失敗」(ErrManifestDataFilePathInvalid)。
//
// Err.Error() 內含 OpenDataFile 解析後的絕對路徑(missing 分支經雙層 %w 保留
// 底層 *os.PathError 的路徑),適合直接給 UI 顯示讓 user 看到「期待的檔放在哪」。
type MissingRow struct {
	Subject string // 對應 PhaseManifest.Subject
	EMGFile string // 對應 PhaseManifest.EMGFile(raw,manifest 內字面)
	Err     error  // 原始 error;可 errors.Is 命中 ErrManifestDataFileMissing / ErrManifestDataFilePathInvalid
}

// ValidateAllEMGFiles 掃過整批 manifest,把每筆 EMGFile 走 OpenDataFile(開檔即
// 存在性確認,確認後立即 Close),收集所有失敗 row 給 caller 在 Load 階段早一步
// surface(而非等到 per-subject Generate 才炸)。
//
// 動機:V.14 manifest 升級 NSF1/2/3 EMGFile 欄誤改 → user 在 Chart Composer
// Generate 才看到「EMG 檔案不存在」,沒有早期警示。Load 階段跑 validator
// 後 caller 可一次列出哪些 subject 缺檔,讓 user 修 manifest 或 data folder。
//
// 順序契約:回傳 slice 順序對齊 manifests 內 row 出現順序 — caller(如
// GUI dropdown)可依此推斷哪一個 subject row 對應到哪一個 missing。
//
// 回 nil 或 empty slice 都代表「全部 row 都 OK」;caller 用 len(missing) == 0
// 判斷即可,不必 nil check。
func ValidateAllEMGFiles(manifests []models.PhaseManifest, dataFolder string) []MissingRow {
	var missing []MissingRow

	for _, m := range manifests {
		// OpenDataFile 開檔成功即確認存在;立即 Close(此處只做存在性檢查,不讀內容)。
		// 失敗則 append + continue — 不在首個 error abort,自然保留 order-aligned、
		// one-row-per-failure 的 cardinality(含 baseFolder 不存在時每筆 row 同樣失敗)。
		f, err := OpenDataFile(dataFolder, m.EMGFile)
		if err != nil {
			missing = append(missing, MissingRow{
				Subject: m.Subject,
				EMGFile: m.EMGFile,
				Err:     err,
			})
			continue
		}
		_ = f.Close() //nolint:errcheck // existence-check only; close error not actionable
	}

	return missing
}
