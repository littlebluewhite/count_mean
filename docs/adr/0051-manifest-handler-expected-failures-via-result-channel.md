# manifest handler 家族的可預期失敗一律走 result 通道

**Status**: accepted · **implemented** (2026-10-11)

manifest + dataFolder 驅動的 GUI handler（`AnalyzeCCI`、`AnalyzeMuscleRatio`、`AnalyzeNormalizedPhaseSync`、Chart Composer 兩個入口）早已把可預期失敗統一成 `(failedXxxResult(inputMessage(err)), nil)`；只有 `AnalyzePhaseSync` 仍是混合通道：入口 validate（`ErrNoManifestFile` / `ErrNoDataFolder`、路徑驗證）、`ErrNoPhaseSelection`、`ErrInvalidPhasePoint` 回 `(nil, err)` 走 err 通道，分析 / 寫檔失敗才走 `failedPhaseSyncResult`。同一個家族共用同一支 prelude（`validateManifestHandlerParams`），卻有兩種失敗形狀，呼叫端與測試都得記住例外。本 ADR 把 `AnalyzePhaseSync` 補齊。

## Decision

1. `AnalyzePhaseSync` 的 prelude（`validateManifestHandlerParams`）、`ErrNoPhaseSelection`、`ErrInvalidPhasePoint` 三個分支改為 `return failedPhaseSyncResult(inputMessage(err)), nil`。`ErrInvalidPhasePoint` 仍帶 `StartPhase=%q EndPhase=%q` 前綴，經 `inputMessage` 在地化與脫敏。
2. err 通道只剩 panic（`recoverHandlerPanic` 轉成 `ErrInternalPanic`），與家族其他成員一致。
3. 測試：`assertUnifiedFailureChannel` 新增 PhaseSync 三種情況（`ErrNoManifestFile`、`ErrNoDataFolder`、`ErrNoPhaseSelection`）；`path_validation_test.go` 的 PhaseSync traversal 測試改斷言 `Message`；`rpc_redact_test.go` 的 PhaseSync validate 列從 err 通道表移到 Message 表。
4. 前端不改：`phase_sync_spec` 的 `onResult` 本來就處理 `!success`。使用者可見差異：驗證失敗的對話框文字不再帶「分析失敗:」前綴（原本 err 通道由前端 catch 時加上）。
5. `LoadPhaseManifest` 的通道不在本 ADR 範圍，留到 W10 合併時處理。

## Why

- 同家族、同 prelude、同失敗語意，通道就該相同；「validate 走 err、分析走 result」的切法沒有領域理由，只是 `AnalyzePhaseSync` 沒被當時的統一（Wave 3 Batch R）涵蓋。
- err 通道在 Wails 是 rejected promise，前端要另寫 catch 分支；result 通道由 `onResult` 一處處理。
- [[ADR-0035]] 只凍結「首句 defer recover」那次重構的形狀，沒有否決統一錯誤通道。

## Considered Options

### A. 全部走 result 通道（chosen）

與家族一致，err 通道只留給 panic。缺點：對話框文字少了「分析失敗:」前綴（刻意）。

### B. 維持 dual / mixed（status quo，rejected）

家族內留一個例外，測試與文件都要特別標註。

### C. 連 `LoadPhaseManifest` 一起改（rejected for now）

它的 `(data, err)` 形狀與 result handler 不同，W10 合併時一併評估。

## Related

- [[ADR-0035]]（首句 defer recover；本 ADR 不牴觸）
- [[ADR-0036]]、[[ADR-0048]]（Message 通道的 envelope 與 redact）
