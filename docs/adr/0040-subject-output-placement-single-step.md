# Subject output placement 單一步驟 — ADR-0016 invariant 結構化

**Status**: accepted · **implemented** (2026-10-09)

## Decision

1. `func (h *CSVHandler) placeSubjectOutput(subDir, subject, suffix string, header []string, emit csvutil.RowEmitter) (string, error)` 是所有 Subject-based write 的**唯一** placement 步驟（見 GLOSSARY **Subject output placement**）。流程固定為：
   `filename.SubjectOutputName(subject, suffix) + ".csv"` → `safeJoinOutput`（containment，`fsperm.IsWithin`）→ `PathValidator.ValidateExternalPath(outputPath)` → `os.MkdirAll(Dir)` → `csvutil.WriteCSVAtomic{Header, BasePaths: GetAllowedBasePaths(), Emit}`，回傳實際寫入路徑。
2. 7 個 writer（`WritePhaseSyncResult` / `WriteNormalizedPhaseSyncResult` / `WriteNormalizedPhaseSyncEMG` / `WriteCCIResult` / `WriteCCIPhasesResult` / `WriteMuscleRatioOutputAll` / `WriteMuscleRatioOutputPhases`）只保留 row layout 與 emit closure。`WritePhaseSyncResult` / `WriteNormalizedPhaseSyncResult` 的 `[][]string` 經 `placeSubjectRows`（`data[0]` 當 header、其餘 streaming emit）轉進同一步驟。非 test 的 `internal/io` 中 `WriteCSVAtomic` 只在 `placeSubjectOutput` 內呼叫一次。
3. 刪除：`phaseSyncAtomicWrite`、`writePhaseSyncAtomic`、`validateMuscleRatioOutputDir`（最後一個 `_validation_marker`，其「驗目錄」的需求由對真實輸出檔路徑的 `ValidateExternalPath` 取代，不再捏造 child）、`WriteCCIResult` / `WriteCCIPhasesResult` 的 inline 副本、`calculator.GenerateOutputFileName`（輸出檔名規則統一在 `SubjectOutputName`，檔名 byte-identical：`{subject}_{Start}-{End}_statistics.csv`）。
4. 錯誤文字統一為「輸出路徑無效」「輸出目錄建立失敗」（原有 `PhaseSync` / `CCI` / `muscle_ratio` 前綴取消；這是允許的 user-text 變動），並以 `%w` 包底層 error。`safeJoinOutput` 逸出時只回哨兵 `errOutputPathEscapesOutputDir`，不再把 SubDir / filename / resolved 路徑寫進錯誤文字（目錄末段可能是病患資料夾名）。
5. 測試：`csv_handler_placement_test.go` 以 7 個 writer 的表格鎖定 Filename（逐字）/ StaysInOutputDir / SubDirEscapeRejected / SensitiveOutputDirRejected / NoStrayTmp / BOM / ReturnedPathExists，並單獨斷言逸出錯誤不含 SubDir。各 writer 重複的 placement 測試（SubDir、NoTmp、BOM、PathTraversal、EmptySubDir、FilenameTemplate）刪除，保留 row layout 與 round-trip 測試。

## Why

- ADR-0016 的 invariant「Subject-based ⟹ `WriteCSVAtomic` + `BasePaths`」過去只因為每份副本「記得」傳 `BasePaths` 才成立；placement 配方有三種實作（`phaseSyncAtomicWrite` 3 caller、CCI 兩份 inline、MR 的 marker 變體），錯誤前綴各異，placement 測試覆蓋不均。
- 收成單一步驟後，不可能有 Subject-based writer 繞過 containment / 驗證 / atomic，invariant 由結構而非紀律保證；placement 測試只需寫一次。
- 檔名規則原本有一份在 `calculator`（領域邏輯寄居計算套件），其餘 6 個走 `SubjectOutputName`；統一後檔名只有一個來源。
- 錯誤文字含 SubDir 會繞過 webview sink 的絕對路徑遮蔽（相對片段不會被遮）。

## Considered Options

1. **只把 CCI / MR 改呼叫既有的 `phaseSyncAtomicWrite`**：保留 `[][]string` 與 emit 兩套入口、舊前綴，invariant 仍靠命名慣例，否決。
2. **MR 改用 `ValidateExternalDir` 驗目錄再另行驗檔**：兩次驗證、檔名規則在 MR 與其他 writer 不一致；對真實檔路徑做 `ValidateExternalPath`（位置 + 檔名規則，並解析 leaf symlink）涵蓋目錄敏感位置，且與另外 5 個 writer 行為一致，採用。
3. **保留 per-writer 前綴以利辨識來源**：來源已由 caller 外層 wrap（如「寫入標準化 EMG 失敗：」）表達，前綴只讓訊息分歧，否決。
