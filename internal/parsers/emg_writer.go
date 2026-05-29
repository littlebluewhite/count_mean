package parsers

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
)

// ErrEMGWriterSymlinkTarget 表示 outputPath 是 symlink — atomic write 流程不會
// 跟蹤 symlink target,但為了與原本 direct-write 的 O_NOFOLLOW 行為對齊(defense
// in depth),pre-check 直接 reject。Caller 不應該把 symlink 當輸出檔。
var ErrEMGWriterSymlinkTarget = errors.New("輸出路徑為 symlink,拒絕寫入")

// defaultEMGCSVPrecision 為 EMG CSV 預設小數位數，與分期同步分析統計輸出一致。
const defaultEMGCSVPrecision = 6

// ExportPhaseSyncDataToCSV 將 PhaseSyncEMGData 寫入 outputPath 為 CSV 檔。
//
// 輸出格式：
//   - UTF-8 BOM 開頭（Excel 相容）
//   - 第一列：`Time` + 各肌肉名稱（依 data.Headers 順序）
//   - 後續每列：時間點 + 各肌肉於該時間點的取樣值
//
// precision 控制浮點數欄位的小數位數；若 <= 0 則使用預設值 6。
//
// # Path-validation contract
//
// 此函式 **不** 負責路徑驗證 — 呼叫端必須先用 internal/security.PathValidator
// 或 lenient_path 確認 outputPath 在允許的 OutputDir 範圍內、不含 traversal
// segment。Audit (2026-05-17)：所有 production caller 都已符合：
//   - gui/normalized_phase_sync_handlers.go:131 — outputDir 來自 app config，
//     檔名走 filename.Sanitize(subject) 再 filepath.Join 後傳入。
//
// # Atomic write
//
// 改走 csvutil.WriteCSVAtomic：tmp 檔（path+".tmp.<crypto/rand hex>"，O_EXCL,
// 後 random suffix 不可預測）→ BOM → SanitizeHeaderRow(header) → 逐列 emit
// (emit 自動 sanitize) → Flush → fsync → Close → os.Rename(tmp, path)。
// 任一步驟失敗即清掉 tmp，final path 不變動，避免中途 crash 留下 partial / truncated CSV。
//
// 同步沿用 WriteCSVAtomic 內部 fsperm.TmpCreateFlags / fsperm.FilePerm — tmp
// 檔在 unix 下 owner-only、與原本 direct-write 的 0o600 一致;O_NOFOLLOW 由
// WriteCSVAtomic 處理（symlink 防護不在本函式邊界,而是 fsperm 層）。
//
// Caller signature 不變：仍是 (data, outputPath, precision) → error。
func ExportPhaseSyncDataToCSV(
	data *models.PhaseSyncEMGData,
	outputPath string,
	precision int,
) error {
	if data == nil {
		return fmt.Errorf("EMG 數據為空: %w", ErrNilData)
	}

	if precision <= 0 {
		precision = defaultEMGCSVPrecision
	}

	// atomic write 配套:tmp+rename 對 symlink path 會「替換 symlink entry」而
	// 非「跟著寫到 target」— target 不會被覆寫,但 symlink 本身會被替換成 regular
	// file。原本 direct-write 模式靠 O_NOFOLLOW 對 symlink path 直接 fail-open,改
	// atomic 後失去這個語義。Pre-check Lstat 補上 reject — 與 ExportPhaseSyncDataToCSV_RejectsSymlinkTarget
	// 契約對齊。Lstat 不解 link,所以 outputPath 本身為 symlink 會被識別。
	if info, err := os.Lstat(outputPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrEMGWriterSymlinkTarget, outputPath)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("檢查輸出路徑失敗 %s: %w", outputPath, err)
	}

	// 統一用 strconv.FormatFloat 取代雙軌（time 用 FormatFloat，channel 用
	// Sprintf "%%.Nf"）。Sprintf 對 NaN/Inf 寫出「NaN」/「+Inf」字面，而 muscle_ratio /
	// phase_sync 的 NaN-missing-data 慣例期望輸出空 cell — 雙軌不對稱會造成 row 內混
	// 「NaN 字面」與「空 cell」。改全程 FormatFloat 並對 NaN/Inf 顯式輸出空字串。
	formatCell := func(v float64) string {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}
		return strconv.FormatFloat(v, 'f', precision, 64)
	}

	emit := func(write func(row []string) error) error {
		for i := range data.Time {
			row := make([]string, 0, len(data.Headers)+1)
			row = append(row, formatCell(data.Time[i]))

			for _, name := range data.Headers {
				channel := data.Channels[name]

				if i >= len(channel) {
					row = append(row, "")
					continue
				}

				row = append(row, formatCell(channel[i]))
			}

			if err := write(row); err != nil {
				return fmt.Errorf("寫入第 %d 列失敗: %w", i+1, err)
			}
		}
		return nil
	}

	if err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header: buildEMGCSVHeader(data.Headers),
		Emit:   emit,
	}); err != nil {
		return fmt.Errorf("無法寫入輸出檔案 %s: %w", outputPath, err)
	}

	return nil
}

// buildEMGCSVHeader 組合 CSV 標頭：`Time` + 各肌肉名稱。
func buildEMGCSVHeader(channelHeaders []string) []string {
	header := make([]string, 0, len(channelHeaders)+1)
	header = append(header, "Time")
	header = append(header, channelHeaders...)

	return header
}
