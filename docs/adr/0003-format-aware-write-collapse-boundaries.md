# Format-aware write 深化的邊界 — Candidate 1 收乾所取捨的三條 sticky boundary

**Status**: accepted (2026-05-28)

## Decision

Candidate 1（format-aware write contract 統一）收乾後，三條設計邊界刻意保留，**不**在本次深化內進一步收緊：

1. **muscle_ratio Output 2 sticky-success 留在 Analyzer**：CSVHandler 新增 `WriteMuscleRatioOutputAll` 與 `WriteMuscleRatioOutputPhases` 兩個 method，各只負責一個檔的 row layout。「Output 2 寫檔失敗 → Output 1 sticky-success」與「collectPhasePoints warn-path → Output 2 skip」的業務語意由 `muscle_ratio.Analyzer` 持有，不下沉到 CSVHandler。
2. **Filename ownership 沿 unit-of-work 形狀切分**：Subject-based write（PhaseSync / CCI / MuscleRatioOutput*）在 CSVHandler 內由 `result.Subject` + suffix convention 推導，`req.Filename` 被忽略；File-based write（PhaseAnalysis / MaxMean / Normalized）由 caller 傳入 `req.Filename`。CSVHandler 內部統一負責 OutputDir+SubDir+Filename 的 join，回傳 `outputPath` — caller 不再重算。
3. **`AnalysisHandler[P, R].WriteCSV` closure 簽章不變**：`func(*io.CSVHandler, R) (string, error)` 維持，`AnalyzeMuscleRatio` 仍是 `WriteCSV: nil`。但 nil 的語意從「Candidate 2 待填的 TODO」翻譯為「batch unit-of-work 不適用 single-path closure」 — semantic-correct 的設計選擇。

## Why

三條 boundary 對齊「format-aware write 吸 row layout，**不**吸 business semantics 與 file-context」這條深度上限：

- (1) sticky-success 是 muscle_ratio 對「partial result」的業務語意，不是 row layout —下沉到 CSVHandler 會把 family member 專屬的失敗語意帶進 io 套件，違反 deep module「跨 caller 一致行為」的要求。
- (2) Subject-based 與 File-based 在 input shape 上根本不同（前者帶 Subject key、後者沒有），filename ownership 跟著 input shape 走是 honest depth；硬要統一只能往 AnalyzeResult 塞 SourceInputFile 之類的 file-context 欄位，又跟 Candidate 4（calculator 收 EMGDataset）撞車。
- (3) MR 是 family 裡唯一的 batch unit-of-work。closure 簽章為 MR 改成 `[]string` 或 mutate-in-place，會逼其他三個 single-subject member 一起付代價，且 CCIResult / PhaseSyncResult 等 GUI DTO 連帶動，reversibility 急降；承認 nil 是設計如此（loop 在 Analyzer 內 / paths 透過 `SubjectResult.OutputAllPath` / `OutputPhasePath` 回填）是 honest depth。

ADR-0001 的「擴張 invariant 從 75% 到 100% 覆蓋率」指的是 **row layout** 的 100%，不是 closure shape 或 filename ownership 的 100%。本 ADR 把這條精細解讀刻在族譜上。

## Considered Options

- **Boundary 1 替代**：sticky 改放 CSVHandler（單一 `WriteMuscleRatioSubject` method 內部依 payload 判斷）。拒：CSVHandler 多吞一條 MR 專屬 domain rule，且 `TestMuscleRatio_Output2WriteFailureStickySuccess` 之類測試得跨包搬。
- **Boundary 2 替代 (全內部)**：AnalyzeResult 加 `SourceInputFile` 欄位讓 CSVHandler 內推。拒：污染 compute struct 雙身分（compute result + file-context），Candidate 4 來時解耦成本高。
- **Boundary 2 替代 (全 caller)**：CCI / MR / PhaseSync 也由 caller 算 filename 透過 `req.Filename` 傳入。拒：違反 ADR-0001「filename rule 是 row-layout sibling」結論，PhaseSync `GenerateOutputFileName` 反推回 caller 端，Candidate 1 收乾的 depth 被反向稀釋。
- **Boundary 3 替代**：擴 closure 簽章為 `func(*io.CSVHandler, R) ([]string, error)` 或改 mutate-in-place。拒：CCIResult.OutputCSVPath 等 GUI DTO 全部得改為 slice 或補 mutate；前端 i18n catalog 與測試連動範圍最廣，reversibility 最低；同時遮蔽「batch unit-of-work 跟 single-subject 是兩種 dimension」的真實 asymmetry。

## Reversibility

中 — 三條 boundary 都可個別跳出收緊（例如 Candidate 4 完成後，Boundary 2 可能順勢收乾；或 muscle_ratio sticky-success 未來統一搬進 CSVHandler 仍技術可行），但重新識別原始動機需重走 grilling，成本約 2–3 小時 grilling + 1 工作天 migration。

## Related

- ADR-0001（PhaseSync 走 CSVHandler）— 本 ADR 是其延續的精細化說明。
- 未來 follow-up issue（family err channel 統一）— Q5 出 scope，獨立 ADR/PR 處理。
