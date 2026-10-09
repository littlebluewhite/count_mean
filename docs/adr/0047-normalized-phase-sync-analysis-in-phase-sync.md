# Normalized PhaseSync 分析進 phase_sync

**Status**: accepted · **implemented** (2026-10-09)

標準化分期同步分析(NPS)的計算原本寫在 `gui/normalized_phase_sync_handlers.go`:`PhaseSyncAnalyzer.Load` → `ResolvePhaseRange` ×2 → `NormalizeByRangeMax` → 寫 Output 1 → `SliceEMG` → `CalculateStatistics` → 寫 Output 2。後半段「區間 → 切片 → 統計」與 `phase_sync.AnalyzePhaseSync` 重複一份。`Load` / `ResolvePhaseRange` / `LoadAndExtractRange` / `LoadedPhaseSyncContext` 只為這個 handler export。`Load` 吃一對分期點卻不用它,只為了讓 pipeline 裡的 `validatePhaseOrder` 通過(handler 把 Norm 那組塞進 `StartPhase` / `EndPhase`);Stats 那組則由 `ResolvePhaseRange` 另外驗,兩組分期點走兩條驗證路徑。同一份報告有兩個名字(`phase_sync.GenerateAnalysisReport` 只是轉呼叫 `calculator.FormatStatisticsReport`)。整段計算只能經 `App` 加 disk 上的 manifest / motion / ANC / EMG fixture 測試。

## Decision

1. **phase_sync 有兩個入口,共用 load 與 compute core。**

   ```go
   func (a *PhaseSyncAnalyzer) AnalyzePhaseSync(ctx, *models.AnalysisParams) (*models.EMGStatistics, error) // 簽章不變
   func (a *PhaseSyncAnalyzer) AnalyzeNormalizedPhaseSync(ctx, *NormalizedParams) (*NormalizedResult, error)
   func (a *PhaseSyncAnalyzer) computePhaseSync(ctx, emg, m, start, end) (*models.EMGStatistics, error) // 兩個入口共用,file-free
   func (a *PhaseSyncAnalyzer) load(manifestFile, dataFolder string, subjectIndex int) (*models.PhaseManifest, *models.PhaseSyncEMGData, error)
   ```

   - `computePhaseSync` = 在給定 EMG 上解析 manifest row 的 `[start, end]`([[Phase timeline]])→ `SliceEMG` → `CalculateStatistics`。`AnalyzePhaseSync` 傳原始 EMG,NPS 傳標準化後的 EMG。
   - `AnalyzeNormalizedPhaseSync` = 兩組分期點順序 → `load` → 解析 Norm 區間與 Stats 區間 → `NormalizeByRangeMax` → `computePhaseSync`。全部算完才回傳 `NormalizedResult{Subject, NormalizedEMG, ChannelMaxes, NormRange, StatsRange, Stats}`;兩個區間是 Phase timeline 的解析值,`Stats.StartTime / EndTime` 是切片後實際的 sample 時間(與搬移前 UI 顯示的欄位一致)。
   - `load` 不再吃分期點。`Load` / `ResolvePhaseRange` 改成不 export(`resolvePhaseRange(emg, m, start, end)` 成為 free function);`LoadAndExtractRange` 與 `LoadedPhaseSyncContext` 沒有剩下的 caller,刪除。
   - 分期點順序檢查(`validatePhasePair`,文字仍是「分期點順序驗證失敗: …」)從驗證 pipeline 移到兩個入口,排在任何 manifest / 檔案 I/O 之前;`resolvePhaseRange` 不再重驗。
2. **錯誤帶 Stage。** `phase_sync.Stage`(`StageLoad` / `StageNormRange` / `StageStatsRange` / `StageNormalize` / `StageStatsSlice` / `StageStatistics`)與 `*phase_sync.AnalysisError{Stage, Err}`,`Error()` 與 `Err` 逐字相同、`Unwrap` 回 `Err`(與 [[ADR-0046]] 的 `composer.LoadError` 同形)。ctx 取消原樣回 `ctx.Err()`,不帶 Stage。`computePhaseSync` 回 `StageStatsRange / StageStatsSlice / StageStatistics`,`Err` 不加前綴;`AnalyzePhaseSync` 自己把切片與統計兩步補回既有前綴(「提取 EMG 時間範圍數據失敗: 」「計算統計信息失敗: 」),它的錯誤不帶 Stage,文字不變。
3. **gui handler 成為 adapter**:驗證 → `phase_sync.AnalyzeNormalizedPhaseSync` → ctx 檢查 → Output 1 → ctx 檢查 → Output 2 → envelope。`normalizedPhaseSyncFailKey` 把失敗對到搬移前每一步各自的 i18n key:取消 → `KeyErrorHandlerCancelled`、load → `LoadDataFailed`、Norm 區間 → `NormRange`、Stats 區間 → `StatsRange`、標準化 → `NormalizeFailed`、切片 → `ExtractStatsRangeFailed`、統計 → `CalcStatsFailed`;兩個寫入維持 `WriteNormalizedEMGFailed` / `WriteStatsFailed`。RPC DTO 不變。
4. **報告只剩 `calculator.FormatStatisticsReport`**;`phase_sync.GenerateAnalysisReport` 刪除,兩個 handler 直接呼叫前者。
5. **測試**:新增 file-free 的 `computePhaseSync_test.go`(仿 [[ADR-0024]]):力板(S, L)與 motion-index(D, O)兩域落在同一視窗的統計、pre-cancelled ctx、三個步驟各自的 Stage。刪除 `SetParseEMGFileFnForTest` 與靠它卡住 parser 的 in-flight cancel 測試(`load` 只剩 `manifest.LoadEMG` 一條路;`go.uber.org/goleak` 隨之從 go.mod 移除)。gui 新增 stage → key table test;[[ADR-0042]] 的 Phase timeline characterization 改從 NPS 回傳的 `NormRange` / `StatsRange` 讀 PhaseSync 的 (S, L) 與 (D, O) 兩列。
6. **不在範圍內**:驗證仍完整 parse Motion CSV 與 ANC 力板檔,只為了讀最後一筆 index / 時間(`validateMotionFile` / `validateForceFile`)。改成只讀尾端是另一個決定。

### 使用者看得到的改變(三項)

1. **NPS 統計步驟失敗時不再寫 Output 1。** 搬移前 Output 1 在統計之前寫出,「擷取統計區間失敗」「計算統計失敗」之後留下一份孤兒 `{subject}_normalized.csv`;現在任一步失敗都不寫任何輸出。(`TestAnalyzeNormalizedPhaseSync_StatsFailureWritesNoOutput`)
2. **NPS 的分期點順序錯誤在任何 I/O 之前回報。** Norm 那組的前綴由「載入資料失敗: 」改成「標準化區間: 」(如「標準化區間: 分期點順序驗證失敗: 開始分期點 P2 與結束分期點 P0: start phase must be before end phase」,未知分期點同理);Stats 那組文字不變(「統計區間: 分期點順序驗證失敗: …」),但現在排在 manifest / 檔案錯誤之前。(`TestAnalyzeNormalizedPhaseSync_PhaseOrderCheckedBeforeIO`)
3. **AnalyzePhaseSync 的分期點順序錯誤排在 manifest / 檔案錯誤之前。** 文字與 gui 前綴(`KeyErrorHandlerAnalysisFailed`)不變,只改先後。(`TestAnalyzePhaseSync_PhaseOrderCheckedBeforeIO`)

其餘 Message 逐字相同,golden(AnalyzePhaseSync P0–L、NPS Norm P0–L / Stats S–T)無差異。多重錯誤時的先後也維持:NPS 仍在標準化之前解析 Stats 區間,所以 Stats 區間錯誤照舊先於標準化錯誤。取消的時點有一處精化(同 ADR-0024):ctx 在進入點、load 之後與 `computePhaseSync` 內檢查,load 期間被取消的分析會在區間解析之前就回「分析已取消」,而不是先回區間解析的錯誤或跑完標準化。

## Why

- **計算只有一個家。** handler 只剩驗證、兩次 `CSVHandler` 寫入與 envelope;「區間 → 切片 → 統計」只寫一次,兩組分期點走同一條驗證路徑,且都在 I/O 之前。只為 handler 而 export 的四個名字收回套件內。
- **可測。** `computePhaseSync` 是 file-free seam(ADR-0024 的 `computeCCI` 先例):區間換算與統計正確性有 always-run 測試,不需要 ANC fixture。in-flight cancel 測試靠 package-level atomic hook 注入阻塞 parser,換成 compute core 上確定性的 pre-cancel 測試。
- **先算完再寫。** 分析回傳完整結果,寫檔留在 gui(Output 1 由 CSVHandler 寫,[[ADR-0020]]);「統計失敗卻留下 Output 1」是 handler 交錯計算與寫檔的副作用,不是刻意的契約。
- **Stage 而非 i18n key**:handler 層 localize、analyzer 只回 error([[ADR-0036]])。gui 對一個小 enum 做 switch,不必知道 phase_sync 內部呼叫哪些函式;`AnalysisError.Error()` 不加字,所以 Message byte-identical。
- **NPS 不是第 4 個 [[Domain analyzer]]。** 它與 `AnalyzePhaseSync` 一樣是 manifest + dataFolder 驅動、single-subject、compute-only,只是 phase_sync 的第二個入口;[[ADR-0012]] 的兩軸與成員表不變。

## Considered Options

- **A. NPS 獨立成套件(如 `internal/normalized_phase_sync`)。** 拒:它要 phase_sync 的 load 與區間解析,得再 export 回去或複製一份;還會讓 ADR-0012 多一個形狀相同的成員。
- **B. `computePhaseSync` 吃已解析的 `PhaseTimeRange`,避免 NPS 解析 Stats 區間兩次。** 拒:compute core 就不再擁有「分期點 → EMG 秒數」這段,file-free 測試只剩切片 + 統計。兩次解析是同一份 Phase timeline 查表加邊界檢查,標準化資料的 `Time` 是原資料的拷貝,結果相同。
- **C. NPS 只在 `computePhaseSync` 內解析 Stats 區間一次(標準化之後)。** 拒:Stats 區間與標準化同時失敗時,先報的錯會從「統計區間」變成「標準化失敗」—— 第四項使用者看得到的改變,換來省一次查表。
- **D. 讓 `computePhaseSync` 帶 `AnalyzePhaseSync` 的切片 / 統計前綴。** 拒:NPS 的訊息會多出「提取 EMG 時間範圍數據失敗: 」「計算統計信息失敗: 」兩段字。
- **E. analyzer 自己寫 Output 1 / 2(compute+write)。** 拒:phase_sync 是 compute-only([[ADR-0012]]),兩個輸出都經 gui 的 CSVHandler([[ADR-0020]]、[[ADR-0001]])。
- **F. 每個 Stage 一個 sentinel。** 拒:理由同 ADR-0046 的選項 D。

## Consequences

- phase_sync 的 export 面:刪 `Load`、`ResolvePhaseRange`、`LoadAndExtractRange`、`LoadedPhaseSyncContext`、`SetParseEMGFileFnForTest`、`GenerateAnalysisReport`;新增 `AnalyzeNormalizedPhaseSync`、`NormalizedParams`、`NormalizedResult`、`Stage`(6 個常數)、`AnalysisError`。
- `ResolvePhaseRange` 的 in-package 測試改呼叫 `resolvePhaseRange`(錯誤全文、sentinel、ADR-0043 的容差不變);`TestGenerateAnalysisReport` 移到 calculator 成為 `TestFormatStatisticsReport`。
- phase_sync 硬編碼的 zh 錯誤字串不在此遷移(另案 i18n)（已由 [[ADR-0048]] 遷移，除 2 處 PhaseSyncValidationError）。
- GLOSSARY 更新 **Domain analyzer**(phase_sync 兩個入口、NPS 不是第 4 個 member)。

### Amends

- **[[ADR-0024]]**「為何不碰 phase_sync:已把 Load 與 compute 分離」:當時 `Load` 的介面仍吃分期點、compute 沒有 file-free seam;現在 `computePhaseSync` 才是 phase_sync 的這個 seam。
- **[[ADR-0044]] 第 5 點**(`SetParseEMGFileFnForTest` 暫留):hook 與 in-flight cancel 測試已刪,`load` 只走 `LoadEMG`。
- **[[ADR-0042]] Decision 2 與 [[ADR-0043]] 表第 1 列**的 `ResolvePhaseRange` 現為不 export 的 `resolvePhaseRange`,越界規則不變。

## Related

- [[ADR-0012]] —— Domain analyzer 刻意分歧;本 ADR 不加成員。
- [[ADR-0020]] —— NPS Output 1 經 CSVHandler 寫;寫入仍在 gui。
- [[ADR-0024]] —— compute core / I/O adapter seam 的先例。
- [[ADR-0036]] —— Webview envelope:`failMessage` / `inputMessage`。
- [[ADR-0042]] —— Phase timeline 是分期點秒數的唯一 owner。
- [[ADR-0046]] —— 同 wave:帶 Stage 的載入錯誤(`composer.LoadError`)。
