# 把 normalized-phase-sync Output 1（EMG 時序）的寫入收回 CSVHandler，與 Output 2 共用 dirfd 原子寫 seam

**Status**: accepted (2026-06-03, design) · **implementation pending**（後續獨立 `/handoff` 進 worktree）

本 ADR 是 2026-06-03 architecture review Candidate 2 grilling 後的結論。`parsers.ExportPhaseSyncDataToCSV`（normalized-phase-sync **Output 1**、標準化 EMG 時序的寫入）是最後一條繞過 CSVHandler 的 phase-sync 系列寫入；它在 `parsers` package 內重造了 CSVHandler 已持有的 row layout / precision / atomic-write 編排 / 路徑驗證契約。本案把它收回，補完 [[ADR-0001]] 啟動的「phase-sync 寫入統一走 CSVHandler」遷移（[[ADR-0001]] 做了 stats 路徑，本案做最後的時序路徑）。

## Decision

新增 Subject-based 方法 `CSVHandler.WriteNormalizedPhaseSyncEMG(req WriteRequest, data *models.PhaseSyncEMGData, subject string) (string, error)`，刪除 `internal/parsers/emg_writer.go` 的 `ExportPhaseSyncDataToCSV` escapee。四條設計分叉的結論：

**1. 收回路線 = factored collapse（非 naive materialize）。** 抽出 [[ADR-0016]]/[[ADR-0017]] 那條 dirfd-anchored 原子寫編排成共用 seam：

```
phaseSyncAtomicWrite(subDir, filename, header []string, emit csvutil.RowEmitter) (string, error)
  = safeJoinOutput → ValidateExternalPath → MkdirAll
    → csvutil.WriteCSVAtomic{Header, BasePaths: GetAllowedBasePaths(), Emit}   # dirfd 錨定
```

兩個 adapter 跨此 seam：
- **materialized**（既有 `writePhaseSyncAtomic([][]string)` 退化為 thin wrapper：`data[0]` 當 header、slice-loop emit `data[1:]`）— Output 2 / `WritePhaseSyncResult` 路徑**不變**；
- **streaming**（新）— `WriteNormalizedPhaseSyncEMG` 以 `data.Time` / `data.Channels` 建 streaming emit。

**明確拒絕** materialize Output 1 後複用 `writePhaseSyncAtomic`：Output 1 是完整標準化 EMG 時序（10⁵⁺ rows × N channels），materialize 會把整個 `[][]string` 堆進記憶體再寫，相對現行 row-by-row `emit` 是記憶體回歸。streaming emit 是 load-bearing，必須保留。

**2. Filename ownership = Subject-based（顯式 subject 參數）。** `PhaseSyncEMGData` **不帶 Subject**（`internal/models/phase_sync_models.go:84`，只有 `Time/Channels/Headers`），故 Subject 以**獨立參數**傳入、不塞進 data struct。CSVHandler 內部推導 `{Subject}_normalized.csv` — 透過 `filename.SubjectOutputName(subject, "normalized")`（[[ADR-0019]] 的 mechanic，Output 1 成為其**第 7 個 consumer**）。caller（`gui/normalized_phase_sync_handlers.go`）因此卸下 `:149-153` 的 filename 拼接與 `:160` 的 `validateExternalPathInputs`。

**3. Precision = 去參數、複用 `phaseSyncPrecision`。** 刪除 `normalizedPhaseSyncPrecision`（gui `:17`）與 `defaultEMGCSVPrecision`（parsers `:20`）兩個 `=6` 常量；新方法直接讀 `internal/io/csv_converter.go:17` 的 `phaseSyncPrecision`，成為 phase-sync 家族**唯一**的 precision 來源。現行 `ExportPhaseSyncDataToCSV` 的 `precision int` 參數是 false degree of freedom（唯一 caller 恆傳 6），移除。`parse_helpers.go:134` 的 `defaultFloatCellPrecision`（**讀**側）與 `config.Precision` 不碰（scope creep）。

**4. 路徑驗證契約反轉 + 安全升級。** 契約從 emg_writer 的「caller 負責驗證」翻為「CSVHandler 內部驗證」（seam 內的 `ValidateExternalPath`）。**移除** `ExportPhaseSyncDataToCSV` 的 `os.Lstat` symlink 預檢，改由 dirfd seam 守門 — Output 1 從此走與 Output 2 / CCI / muscle_ratio **同一條**已驗收的 [[ADR-0017]] 寫入姿態。

NaN/Inf → 空字串的 `formatCell`（normalized 時序的 missing-data 慣例）是 Output 1 **專屬**，隨 streaming adapter 一起搬，**不**與 Output 2 的 `fmt.Sprintf("%.6f")`（寫字面 `NaN`）合併。

## Why

- **安全 locality 是 collapse 的主要理由（非 precision 去重）。** 收回前 Output 1 是唯一一條跳過 dirfd seam 的 phase-sync 寫入，只靠較弱的 `Lstat` 預檢防護。`Lstat` 只查 leaf、不查 parent component，且 Lstat→create 之間有 swap window（TOCTOU）。dirfd seam 把單一 fd 釘在 *validated, resolved parent*、`renameat` 相對它執行：leaf 上的 pre-planted symlink 被原子**替換**（非跟隨，故無 `/etc/passwd` 式改向）、parent 任一 component 為 symlink 直接 reject（`O_NOFOLLOW_ANY` / `RESOLVE_NO_SYMLINKS`）。移除 `Lstat`「reject symlink-at-target」**不是安全回歸**（`renameat` 替換而非寫穿，本就無 arbitrary-write 向量），換得 parent-component 防護 + TOCTOU 關閉 + dirfd 錨定，是**淨升級**。同時消解今日 Output1-rejects / Output2-replaces 的不對稱。
- **deletion test pass（concentrate 不只是 move）。** 刪 `ExportPhaseSyncDataToCSV` 後，path/precision/atomic 編排**併進** CSVHandler 既有能力（concentrate）；streaming emit **relocate** 進 io、與 Output 2 sibling 同居並複用 hard part。load-bearing、易錯的部分（dirfd 原子寫）集中、寫一次涵蓋兩 output；mechanical 的 emit closure 位移但不重複。對照 [[ADR-0005]]/[[ADR-0012]]「relocation-only = pass-through（被拒）」的判準：本案 hard part 是 consolidation、非單純位移，故 collapse 成立。
- **Filename ownership 跟 unit-of-work 形狀走（honest depth）。** Output 1 與 Output 2 是同一分析的兩個檔（`{Subject}_normalized.csv` 與 `{Subject}_normalized_norm-…_stats-….csv`），naming convention 應同居一處。data struct 不帶 Subject 這點不改變 ownership 結論 — 顯式 subject 參數讓 `PhaseSyncEMGData` 維持乾淨，正是 [[ADR-0004]] Boundary 2「別把 file-context 塞進 compute struct」的**意圖**（非機械式「struct 有無 Subject 欄位」判斷）。[[ADR-0019]] 已明示 CSVHandler 呼叫 `filename` 推 Subject-based 名**不是** Boundary 2 violation。

## Considered Options

- **Fork B 替代 — preserve + ADR（保留 parsers escapee）。** 論點：streaming emit 對 CSVHandler 全 materialized 的 seam 是 alien，collapse 只位移 streaming 複雜度。拒：低估了安全 locality — Output 1 跳過 dirfd seam 是真實不一致，factored seam 讓 streaming emit 複用 hard part、只位移 mechanical part。安全升級的價值大於 emit relocation 的成本。
- **Fork B 替代 — naive materialize。** materialize Output 1 → 複用 `writePhaseSyncAtomic`。拒：丟掉 streaming、大檔記憶體膨脹，是 move + 加成本。
- **Fork A 替代 — File-based（caller 傳 req.Filename）。** 機械套用「input shape 決定 ownership：無 Subject key → File-based」。拒：違背 [[ADR-0004]] Boundary 2 的*意圖*（重點是別污染 compute struct，顯式參數不污染），且把同一分析的兩檔命名拆給兩個 owner、保留 caller 端 `:160` 路徑驗證（喪失 Fork D locality）。
- **Fork C 替代 — 保留 precision 參數。** 拒：唯一 caller 恆傳 6、零變異，是 hypothetical seam（one adapter）；Output 2 已把 6 當家族內 physical-unit 常規（無參數）。
- **Fork D 替代 — 保留 `Lstat` reject。** 在 seam 前另加 Output-1 專屬 symlink reject。拒：重新製造 Output1-rejects/Output2-replaces 不對稱，且加一條 TOCTOU-prone 的 `Lstat` 在不需要它的 dirfd 路徑之上；symlink-at-target 非安全邊界。

## Consequences

- **Test surface 遷移**（interface 即 test surface — 10 個 `internal/parsers/emg_writer_test.go` 隨方法遷入 io package）：
  - **migrate**（re-point 至 `WriteNormalizedPhaseSyncEMG`）：`WritesBOM`、`HeaderOrderPreserved`、`NilDataReturnsError`、`AtomicWrite_NoTmpLeftover`、`AtomicWrite_LeavesFinalUntouchedOnEmitError`、`RoundTripParseAndWrite`。
  - **die**：`PrecisionApplied`、`DefaultPrecisionWhenZero`（無 precision 參數；意圖併入固定-6 round-trip）。
  - **die-as-written**：`RejectsSymlinkTarget`（P1-A4-4，reject→replace）；安全意圖改由 dirfd seam 既有測試（csvutil/fsperm）守。
  - **invert**：`DocContract_PathValidationIsCallerResponsibility`（caller-validates → CSVHandler-validates）。
- **User-observable 行為變更**：pre-existing symlink 於 `{Subject}_normalized.csv` 從「報錯」變「被替換」（與其他所有 Subject-based 寫入一致）。已於 grilling 核准。
- **與 [[ADR-0019]] 正交、互補**：本案是 ownership（Output 1 入 Subject-based camp）；0019 是 mechanic（`SubjectOutputName`）。`WriteNormalizedPhaseSyncEMG` 是 0019 primitive 的第 7 consumer。
- **CONTEXT.md**：*Format-aware write* 條目已把 NormalizedPhaseSync 列為 Subject-based — 本案使該斷言**成真**（Output 1 原為 parsers escapee、與該斷言矛盾，收回後消除矛盾）。僅需極小銳化，不引入 impl 細節。

## Reversibility

中。回頭分裂要在 `parsers` 重新發明 streaming export API、migrate test、復原 caller 端 filename+path 邏輯，並把 Output 1 退出 dirfd seam（安全回退）。重識別動機需重走 grilling，估 2–3 小時 grilling + 0.5–1 工作天 migration。

## Related

- [[ADR-0001]] — phase-sync export 走 CSVHandler；本案補完其最後一條（時序）escapee，沿用其 invariant「Subject-based write ⟹ WriteCSVAtomic」。
- [[ADR-0004]] — §Boundary 2 filename ownership 隨 unit-of-work 形狀分；Fork A 在其*意圖*層 reconcile（顯式參數不污染 compute struct）。
- [[ADR-0005]]/[[ADR-0012]] — deletion-test 判準（relocation-only = pass-through 被拒）；本案 hard part 是 consolidation 故過 test。
- [[ADR-0015]] — `filename.Sanitize` 與 validator 同居（`SubjectOutputName` 的家）。
- [[ADR-0016]]/[[ADR-0017]] — dirfd-anchored 原子寫 invariant；Fork D 把 Output 1 對齊此基準。
- [[ADR-0019]] — `SubjectOutputName` mechanic；正交互補，本案的 filename 推導委派之。

## Notes

- **As-built（2026-06-03 實作落地）**：設計已於獨立 `/handoff` worktree 實作（branch `worktree-adr-0020-normalized-emg`，subagent-driven 4 task + 每 task 兩階段審查 + 最終全量 review + `codex-review-fix` ×3）。下列 impl process note 記錄 as-built 與本 ADR 設計**預測**的差異（memory `feedback_cross_check_report_vs_code`：漏網寫進 process note 而非 re-grill）。
- **Impl process note — NaN formatCell 陷阱**：Output 1 與 Output 2 共用 precision *值*（6）但**不可**共用 cell formatter。Output 1 的 `formatCell` 把 `NaN/Inf → ""`（ragged/normalized 時序的 missing-data 慣例）；Output 2 的 `ConvertPhaseSyncResult` 用 `fmt.Sprintf("%.6f")` 會寫字面 `NaN`。去重的是**常量**、不是 formatting。impl 勿誤併。
- **Impl process note — 與 [[ADR-0019]] 的協作順序（as-built：本案先落）**：cross-check 確認 `filename.SubjectOutputName` 仍不存在（0019 design-only/untracked），故 as-built 採「本案先落」路徑——filename 用 `fmt.Sprintf("%s_normalized.csv", filename.Sanitize(subject))`（與 `WriteNormalizedPhaseSyncResult` 同 idiom）。Decision §2 描述的 `SubjectOutputName(subject, "normalized")` 委派是**設計終態**；0019 impl 的 `_normalized`/`safeSubject` re-grep sweep 接手把本 call site 對齊（0019 Notes 已承諾補網）。
- **Impl process note — 最小 churn**：建議保留 `writePhaseSyncAtomic([][]string)` 為 thin materialized adapter（covers Output 2 + `WritePhaseSyncResult`），新增 `phaseSyncAtomicWrite(header, emit)` 為共用 seam；避免動到 Output 2 既有路徑。
- **reachability 含 handler + test caller**（memory `feedback_reachability_includes_handler_tests`）：lock 修法方向前跑 broad suite，別只看生產 caller。
- **As-built process note — GUI-test invert（第 11 個 test surface）**：§Consequences 的 test 表列 10 個 unit-scoped surface（`emg_writer_test.go`）。實作另揭一個 **GUI 層** surface：`gui/normalized_phase_sync_handlers_test.go` 的 `TestAnalyzeNormalizedPhaseSync_RejectsInvalidExternalPath`，原斷言 `result.Message` 含 GUI helper `validateExternalPathInputs` 的 wrap「路徑驗證失敗」。本案移除該 GUI 層呼叫後，`/etc` reject 改由 seam 內 `ValidateExternalPath` 守、wrap 為「PhaseSync 輸出路徑無效」（外層再 wrap「寫入標準化 EMG 失敗：」）。斷言字串隨之 invert，**反-PII-leak 意圖保留**（Success==false、raw `/etc/<subdir>` 路徑不洩、加 `NotContains` guard）。precedent ADR-0015/0017，不 re-grill。
- **As-built process note — `RejectsSymlinkTarget` 落地為 invert（非 die-as-written）**：§Consequences 預測此 test「die-as-written，安全意圖改由 dirfd seam 既有測試守」。實作 cross-check 發現既有 `TestCSVHandler_WriteAllowsSymlinkWithinBase`（`csv_handler_symlink_test.go:67`）只覆蓋 `WriteCSV` 經 `fsperm.OpenWriteValidated` 的 follow-symlink 路徑，**未**覆蓋 `phaseSyncAtomicWrite → WriteCSVAtomic` 的 tmp+rename 路徑「leaf-at-target symlink 被 replace、原 target 內容不變」這條性質。故 as-built **新增** replace-contract test `TestWriteNormalizedPhaseSyncEMG_ReplaceContract_SymlinkWithinBase`（非純刪）補釘該安全性質（unix-only skip，mirror 既有慣例）。淨效果與 ADR 安全立場一致（dirfd seam 替換而非寫穿），但測試覆蓋比預測更完整。
- **As-built process note — `formatNormalizedEMGCell` vs 既有 `formatRatioValue` 的 DRY 考量（review 揭、刻意保留分離）**：final code review 指出新 `formatNormalizedEMGCell`（`NaN/Inf→"" else FormatFloat 'f' phaseSyncPrecision`）對 finite 值與既有 `formatRatioValue`（`NaN/Inf→"" else %.6f`）byte-identical。**刻意不合併**：(1) 本 §Notes NaN 陷阱已立「去重常量非 formatting」原則；(2) `formatRatioValue` 的 consumer 是 muscle_ratio（[[ADR-0014]]）+ CCI Output 2（[[ADR-0018]]），抽共用 helper 會動到本案 scope 外的兩個 feature 路徑（CLAUDE.md surgical）；(3) ratio 域硬寫 `%.6f`、EMG 時序域讀 `phaseSyncPrecision` 常數——今日皆 6 但理由不同，應能獨立演化（強行統一會讓改 `phaseSyncPrecision` 靜默改動 ratio 輸出）；(4) 合 [[ADR-0005]]/[[ADR-0011]]/[[ADR-0012]] keep-divergent-shapes 先例（divergent-domain 的 2 行 idiom 重複可容忍、勝過 interface-widening）。若要刻意整併 `csv_handler.go` 內三處 `NaN→""` formatter，屬獨立 scoped change。
