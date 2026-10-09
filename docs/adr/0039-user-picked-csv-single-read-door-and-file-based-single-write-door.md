# 使用者選取 CSV 單一讀門 + File-based 單一寫門；檔案路徑一律不 URL-decode

**Status**: accepted · **implemented** (2026-10-09)

本 ADR 同時記錄 W3 計畫 3.3（使用者選取 CSV 的讀取入口）與 3.4（File-based write 的寫入入口）兩個相連的決定；3.3 的 task 未另寫 ADR。

## Decision

### 3.3 使用者選取 CSV 的唯一讀取入口：`CSVHandler.ReadCSV`

1. `func (h *CSVHandler) ReadCSV(path string) ([][]string, error)` 是 **User-picked CSV read** 的唯一入口，流程固定為：
   `ValidateFilename(filepath.Base(path))` → `ValidateExternalPath` → pre-open stat → 以 `fsperm.ReadFlags` 開檔 → fstat（必須是 regular file 且 ≤ 100MB，FIFO 不阻塞）→ `LimitReader` 單次解析 → `validateCSVRecords`。
2. `validatePathFormat` **不再 URL-decode**。`%`、`+`、`%2E%2E` 都是字面檔名字元；traversal 判定只看 `..` element。

### 3.4 File-based write 的唯一寫入入口：`writeFileOutput`

1. `func (h *CSVHandler) writeFileOutput(req WriteRequest, data [][]string) (string, error)`：`safeJoinOutput(SubDir, Filename)`（containment）→ `MkdirAll` → `WriteCSV(path)`，**回傳實際寫入的路徑**。`WriteMaxMean` / `WriteNormalized` / `WritePhaseAnalysis` 直接回傳它的結果，不再自行重組路徑。
2. `WriteCSV` 不再呼叫 `SanitizePath`：改以 `ValidateFilename(Base)`（控制字元、保留名、危險字元）+ `ValidateFilePath`（traversal、allow-list）+ `IsCSVFile` 守門。錯誤字串改為「無法建立檔案: %w」，不附原始路徑（fsperm 錯誤已於來源端遮蔽）。
3. 刪除：`WriteCSVToOutput`、`WriteCSVToOutputDirectory`、`writeToTarget`、`security.SanitizePath`、`security.ErrPathSanitizationRequired`、`PathValidator.GetSafePath`、`FilePathBuilder`（`ensureCSVExtension` / `StripCSVExt` 等純函式搬到 `csv_ext.go`）。
4. 本 ADR 修正 ADR-0038 §3.2.4 的描述：該處保留的 package 函式 `security.SanitizePath` 亦已刪除。

### 範圍外

Subject-based write（`WriteCSVAtomic`、phase-sync / MR / CCI writer）的輸出位置不在本 ADR，留待後續 task。

## Why

- 舊寫路徑兩次 URL-decode（`GetSafePath` 內一次、`WriteCSV` 內再一次）：`a+b.csv` 被寫成 `a b.csv`，`SF_8_BTS%_6.10.csv` 在寫入階段被拒，OutputDir / SubDir 含 `+` 時目錄被改寫；而 `WriteMaxMean` 等回傳的是「未 decode 的 join 結果」，與實際落檔位置不一致。
- 驗證的字串必須等於開檔的字串。URL-decode 讓兩者分歧，是 bypass 與 data-loss 的共同根源；檔案系統路徑不是 URL。
- 讀、寫各一個入口，守門順序只需在一處維護；兩個舊 `WriteCSVTo*` 只是同一件事的兩種前置拼接。

## Considered Options

1. **保留 `SanitizePath` 但移除 URL-decode**：剩下的偵測表（`../`、控制字元）與 `ValidateFilePath` + `ValidateFilename` 重疊，多一層無新行為，否決。
2. **writeFileOutput 內自行 `filepath.Join`**：會漏掉 SubDir 為絕對路徑或含 `..` 的 containment，否決；沿用 `safeJoinOutput`。
3. **維持 `WriteCSVToOutput*` 為公開 API**：除 `writeToTarget` 外無 caller，且回傳值無法表達實際路徑，否決。
