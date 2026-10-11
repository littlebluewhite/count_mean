# 以名稱取 Subject；輸出檔名會撞的 manifest 整份拒絕；取用時一律驗證

**Status**: accepted · **implemented** (2026-10-11) · supersedes [[ADR-0007]] §4（ctx 的 idx / name 兩形態）；重開 [[ADR-0007]] 的「ctx Option C」

「這個 [[Subject]] 是 [[Manifest]] 的哪一列」分散在五段程式碼，各持一部分規則，沒有一段持有全部：

- **兩個 subject 清單 handler**：`LoadPhaseManifest` → `phase_sync.LoadManifestSubjects` 每列一個名稱、不去重，前端用索引 i 對應第 i 列，失敗走 err 通道；`LoadChartComposerSubjects` 去重、要 dataFolder、另回前端從來不讀的 `MissingFiles`，失敗走 result 通道。後者註解說去重「與 LoadPhaseManifest 對稱」，實際上不對稱。
- **四條取 row 的路徑**：CCI 檢查索引範圍 + `ValidatePhaseManifest`；PhaseSync / NPS 同樣的索引檢查 + `ValidatePhaseManifest` + Motion / Force 非空；Composer 以名稱取第一個相符的列、**不跑** `ValidatePhaseManifest`（分期點不單調的列照樣畫出錯位的 markLine）；MuscleRatio 跑全部列並檢查撞名。
- **同名兩列**：CCI / PhaseSync / NPS 下拉出現兩個、依索引分析；Composer 只列一個、永遠畫第一列；MuscleRatio 整批拒絕。
- **前端**：因為後端分裂，`ManifestPanel` 的 ctx 同時帶 `subjectIdx` 與 `subjectName`，每個 spec 另有 `valueMode`（[[ADR-0007]] §4）。

## Decision

1. **名稱是鍵**。`internal/manifest` 持有：
   - `LoadSubjectNames(path) ([]string, error)`：`LoadManifests` 後回非空 Subject，依 manifest 順序。
   - `LoadSubject(path, name) (*models.PhaseManifest, error)`：`LoadManifests` → 以 rune 序列比對名稱（`string([]rune(s))`；JSON 與 `[]rune` 都把無效 UTF-8 逐 byte 換成 U+FFFD，Big5 Subject 經 webview 往返仍對得上）→ `parsers.ValidatePhaseManifest`。找不到回 `ErrSubjectNotFound`（i18n key `error.manifest.subject_not_found`，輸入錯誤）；row 驗證失敗回 `errors.Is` `ErrSubjectRowInvalid`、`Error()` 與驗證錯誤原文相同，前綴由 caller 加。

2. **名稱唯一由讀檔門保證**：[[ADR-0056]] 讓 `LoadManifests` 拒絕兩列 [[Subject output key]] 相同的 manifest。rune 序列相同的兩列 key 必然相同，所以 `LoadSubject` 最多一列相符；不另訂去重或「取第一列」規則。

3. **取用時一律驗證**：CCI（`cci.loadAndValidate`）、PhaseSync 與 NPS（`phase_sync.validateSubject`，兩個入口共用 `load(…, subject string)`）、[[Chart Composer]]（`composer.Load`）都經 `LoadSubject` 取 row。gui `CCIParams` / `PhaseSyncParams` / `NormalizedPhaseSyncParams` 與內部參數的 `SubjectIndex int` 換成 `Subject string`（json `subject`）；handler 加 `ErrNoSubject`（「Subject 不可為空」，Composer 原文）prelude，`ErrSubjectNotFound` 走 `inputMessage`。MuscleRatio 是 batch（[[ADR-0012]]），仍直接用 `LoadManifests`。

4. **一個 subject 清單 handler**：

   ```go
   func (a *App) LoadManifestSubjects(manifestPath string) (*ManifestSubjectsResult, error)
   type ManifestSubjectsResult struct {
       Subjects []string `json:"subjects"`
       Success  bool     `json:"success"`
       Message  string   `json:"message"`
   }
   ```

   `ErrNoManifestFile` → `validateExternalPathInputs("分期總檔案", …)` → `manifest.LoadSubjectNames`。可預期失敗回 failed result（`inputMessage` / `failMessage(KeyErrorHandlerLoadManifestFailed)`），err 只留給 panic（[[ADR-0051]]）。不吃 dataFolder、不開資料檔。取代 `LoadPhaseManifest` 與 `LoadChartComposerSubjects`；之後沒有 manifest loader 走 err 通道（[[ADR-0051]] §5 留下的項目到此結案）。

5. **前端**：ctx 為 `{ manifestPath, dataFolder, subjectName, subjects }`，`#mpSubject` 的 option.value 一律是 Subject 名稱，`subjectName` 讀 `select.value`（placeholder 為 `''`，必填守門擋得到）。`valueMode`、`subjectIdx`、`subjectsDependOnDataFolder` 刪除；CCI / PhaseSync / NPS / Composer 的 `loadSubjects(manifestPath)` 都呼 `LoadManifestSubjects`，`!success` 時 throw，由 `ManifestPanel` 的 catch 統一 `ShowError`；選 dataFolder 不再 reload subject 清單。

6. **刪除**：`phase_sync.LoadManifestSubjects`（與 2 支測試、1 支 benchmark）、`phase_sync.ErrInvalidSubjectIndex`、`KeyErrorCCIInvalidSubjectIndex` / `KeyErrorPhaseSyncInvalidSubjectIndex`（4 個 locale）、`composer.ErrSubjectNotFound` 與 `findManifestBySubject`、Composer 的去重迴圈與「對稱」註解、`MissingFiles` / `MissingFileDTO` / `ChartComposerSubjectsResult`、`manifest.ValidateAllEMGFiles` / `MissingRow`（FIFO 測試已由 [[ADR-0056]] 的 `TestOpenDataFile_RejectsFIFO` 涵蓋）。

### 與既有 ADR 的關係

- **[[ADR-0007]] §4 被取代**：§4 讓 ManifestPanel 同時給 idx 與 name、「不擴 scope 動 backend RPC signature」。本 ADR 改後端，ctx 只剩名稱。ADR-0007 Considered Options 的「ctx Option C（改 backend 強制統一 idx 或 name）」當初擱置的理由是範圍（「擴 candidate scope 動 backend…動之前要驗證」），不是否決；本 ADR 重開它並選名稱。§6 範例裡的 `ctx.subjectIdx` 一併成為歷史。ADR-0007 依慣例不修改。
- **[[ADR-0047]]** Decision 裡的 `load(manifestFile, dataFolder string, subjectIndex int)` 是當時的簽名，現為 `load(…, subject string)`；索引的措辭只是歷史紀錄。
- **[[ADR-0002]]** 讓 Composer 以名稱為 canonical key（manifest 改版時索引會位移、名稱不會），本 ADR 把同一理由套到四個有 subject 的 panel。

## 行為變更

- **Composer** 遇到沒通過 `ValidatePhaseManifest` 的列（如分期點不單調）回「載入分期總檔案失敗: <驗證錯誤>」，原本照樣組圖、markLine 錯位。
- **Composer 的 subject 下拉**在選好 manifest 後就載入，不必先選資料夾；`MissingFiles` 消失（前端從未顯示，UI 無差異）。
- **CCI / PhaseSync / NPS 的清單載入失敗**改走 result 通道：原本 err 通道送「載入分期總檔案失敗: 解析分期總檔案失敗: <err>」（只有 zh-TW）；現在是 `failMessage` 的「載入分期總檔案失敗: <err>」，依 locale 在地化。前端 `loadSubjects` throw `Error`，對話框文字前面多「Error: 」，與 Composer 原本的顯示相同。
- **選定的名稱不在 manifest**（例如載入清單後 manifest 被改過）回 `Subject "<name>" 不存在於分期總檔案`；原本索引會靜默指到位移後的另一列。
- **空 Subject 的列**不再出現在任何下拉。這些列從來無法分析（`ValidatePhaseManifest` 與 Composer 的 Subject 非空檢查都拒絕空 Subject），能力沒有改變。
- golden（真實 V.16 資料走 GUI handler）的輸出檔與 W9 逐位元組相同；結果 JSON 只有 Composer 的清單紀錄少了 `missingFiles`。

## Why

- **一個概念一個 owner**：清單、唯一性、取 row、驗證原本散在五處；現在清單與取 row 在 `internal/manifest`，唯一性在讀檔門（[[ADR-0056]]），gui 與前端只傳名稱。
- **名稱比索引穩定**：manifest 改版、插列、刪列時索引位移、名稱不變（[[ADR-0002]]）；名稱錯了會被拒絕，索引錯了會靜默分析別人。
- **取用時驗證**：Composer 跳過 `ValidatePhaseManifest` 的漏洞從介面看不出來；把驗證放進唯一的取 row 入口，新的 Subject-based caller 不會再漏。
- **唯一性不另訂規則**：撞名的 manifest 本來就會讓輸出檔互相覆蓋，[[ADR-0056]] 已整份拒絕；在這個前提下，名稱天然唯一，去重或「取第一列」都是多餘的第二條規則。

## Considered Options

1. **維持索引，後端統一做索引檢查**：拒。索引在 manifest 改版時位移，錯了不會報錯；Composer 已用名稱，統一成索引要改 [[ADR-0002]] 的設計。
2. **名稱為鍵，但同名時取第一列或在清單中去重**：拒。同名兩列的輸出檔本來就會互相覆蓋；靜默挑一列等於把錯誤藏起來。整份拒絕已由 [[ADR-0056]] 在讀檔門完成。
3. **保留兩個清單 handler，只修正註解**：拒。兩者通道、參數、回傳形狀都不同，前端要記住各自的契約；`MissingFiles` 沒有讀者。
4. **合併後保留 `MissingFiles`（清單載入時就掃資料檔）**：拒。前端從未顯示；資料檔缺失在分析時由 `OpenDataFile` 以帶路徑的錯誤回報。掃描還讓清單依賴 dataFolder，選資料夾時得重載清單。
5. **清單由 `ManifestPanel` 直接呼 `LoadManifestSubjects`，spec 不再帶 `loadSubjects`**：本次不做。四個 spec 的 `loadSubjects` 現在相同，但 spec 介面與 MuscleRatio 的 `hideSubject` 不受影響，留作後續收斂。

## Related

- [[ADR-0007]]（§4 被取代；ctx Option C 重開）· [[ADR-0002]]（名稱為 canonical key）· [[ADR-0056]]（讀檔門與撞名檢查）· [[ADR-0051]]（result 通道；§5 結案）· [[ADR-0047]]（`subjectIndex` 措辭為歷史）· [[ADR-0012]]（Subject cardinality 不變）· [[ADR-0044]]（[[Subject source]]）
