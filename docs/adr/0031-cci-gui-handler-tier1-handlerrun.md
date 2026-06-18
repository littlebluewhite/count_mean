# CCI GUI handler 遷至 Tier-1 HandlerRun 直用，離開 Analysis pipeline family

**Status**: accepted · **implemented** (2026-06-19)

`AnalyzeCCI`（`gui/cci_handlers.go`）的 GUI handler 從 Tier-2 `AnalysisHandler[CCIParams, *cci.CCIAnalysisResult]` 泛型樣板，遷至 Tier-1 `HandlerRun` 直用 —— 六步 body、單一 recover、雙 CSV 輸出，完全鏡像 `AnalyzeNormalizedPhaseSync`。本 ADR 記錄此決策，**並明確鎖定 `AnalyzeCCI` 不應被未來 review 拉回 `AnalysisHandler` 樣板**。

## Decision

`AnalyzeCCI` GUI handler 改為 Tier-1 `HandlerRun` 直用：

1. **六步直列 body**：validate → execute（domain analyzer `cci.AnalyzeCCI`）→ `WriteCCIResult`（Output 1）→ chart generation → report + transform → `WriteCCIPhasesResult`（Output 2），全在同一個 `HandlerRun` closure 內執行（`state.Load` / `ctx := a.context()` 為 body 開頭的 setup，非編號步驟）。
2. **單一 recover**：移除原本以 `defer` 方式疊加在 `AnalysisHandler.Run` 外的第二個 `recoverHandlerPanic`。CCI 是四個 `AnalysisHandler` 家族成員中唯一的「雙 recover 稅」繳費者（因 chart 生成 / Output-2 寫在 `Run` scope 外），改為 Tier-1 後此稅自然消失。
3. **死碼清除**：
   - `handler.Run` 後的 `runErr` 分流（`errors.Is(runErr, ErrInternalPanic)` → 走 Go err 通道；其餘 expected error → `failedCCIResult`）連同 `Run` 外另接的 chart / phases 錯誤返回，整併為六步在同一 HandlerRun body 內一致的各步直接 fail-fast（`failedCCIResult(...), nil`；panic 由 HandlerRun 自帶 recover 接管）。
   - 兩個 CCI 專屬 sentinel（`ErrCCIAnalysisFailed` / `ErrCCICSVExportFailed`）從未透過 `errors.Is` 被外部 caller 使用，確認為死碼，移除。
4. **CONTEXT.md 家族異動**：`AnalyzeCCI` 離開 [[Analysis pipeline family]]（見 CONTEXT.md `Analysis pipeline family` 概念塊）；`cci` domain analyzer 本身仍存在，形狀（compute-only）不變。

### Amends ADR-0018

本 ADR **amends ADR-0018 的 GUI 放置理由**：

- **ADR-0018 line 45**：「GUI `AnalyzeCCI` handler 在 `Run` 之後（比照 chart 生成位置）呼叫…`WriteCCIPhasesResult` 寫 Output 2」
- **ADR-0018 line 54**：「**compute-only seam 維持 CCI 形狀。** Output 2 比照既有 chart 在 Run 後寫，不破壞 `AnalysisHandler` 的單-WriteCSV 抽象…」

遷至 Tier-1 後，CCI 不再存在 `AnalysisHandler` 與 `Run` 邊界，上述「寫在 Run 後以不破壞 AnalysisHandler 單-WriteCSV 抽象」的理由已消滅。**但 ADR-0018 所確立的 CCI compute-only 不變式仍成立**：GUI handler 依然透過 `CSVHandler.WriteCCIPhasesResult` 從外部寫 Output 2，而非在 analyzer 內部寫（ADR-0004 Boundary 2 / ADR-0012「CCI compute-only vs muscle_ratio compute+write」分歧不動）。ADR-0018 本身依 repo 慣例 **不修改**；遷移已由本 ADR 記錄。

## Why

**決定性理由：CCI 是四個 AnalysisHandler 家族成員中唯一的雙 recover 稅繳費者，且結構上鏡像已排除在家族外的 `AnalyzeNormalizedPhaseSync`。**

- `AnalysisHandler.Run` 的 `recoverHandlerPanic` 只覆蓋 `Run` body 內的三步（validate → execute → writeCSV）。CCI Output-2 的 `WriteCCIPhasesResult` 和圖表生成放在 `Run` 之後，這就強迫 CCI 在 `Run` 外再加一個 `defer recoverHandlerPanic`，成為四個成員唯一付雙 recover 稅的例外。
- `AnalyzeNormalizedPhaseSync` 同樣是六步 body、雙 CSV 輸出，已在 Tier-1 `HandlerRun`（排除於 `Analysis pipeline family` 外）。兩者結構相同，讓 CCI 繼續留在 Tier-2 等於讓形狀相同的 handler 落在不同層，無正當理由。
- **契約保持不變**：RPC 簽名、前端 binding、result/message/error-channel 全部 byte-identical。這是純粹的 Go handler 層結構重組。

## Observability Changes（刻意，非零行為變更）

以下三點是 Tier-1 對齊後的**刻意行為變更**，與 `AnalyzeNormalizedPhaseSync` 對齊，不是回歸：

1. **expected failure 也發出 exit log**：原 Tier-2 路徑在 validation fail-fast 時不走到 exit 日誌；Tier-1 body 中的 `CCI 分析完成` exit log 在 expected failure 路徑也會發出（與其他 Tier-1 handler 一致）。
2. **panic diagnostic handler-name 改為 `CCI 分析`**（原為 `AnalyzeCCI`）：user-message 契約與 `errors.Is(ErrInternalPanic)` 不受影響，只有 diagnostic 文字改變；這修正了之前 log 裡 handler-name 用英文程式識別子而非中文領域名稱的不一致。
3. **log 順序調整**：entry → params → output → exit，對齊 Tier-1 標準順序（無 log 被丟棄或重複）。

以上均為 **Tier-1 對齊、刻意選擇**，不是 zero-behavior-change 重構。

## Considered Options

### A. Tier-1 HandlerRun 直用（chosen）

鏡像 `AnalyzeNormalizedPhaseSync`：六步直列、單一 recover、雙輸出均在同一 HandlerRun body。

優點：消除雙 recover 稅；與結構相同的 `AnalyzeNormalizedPhaseSync` 保持一致；移除兩個死碼 sentinel；`handler.Run` 後的 `runErr` 分流溶解。
缺點：三個 observability 行為改變（見上方，均屬刻意）。

### B. 維持 Tier-2 AnalysisHandler + 保留第二個 recover（status quo，rejected）

CCI 繼續是四個成員唯一的雙 recover 稅繳費者，且與結構相同的 `AnalyzeNormalizedPhaseSync` 繼續落在不同層。拒：無法移除死碼 sentinel；結構分歧無正當理由；未來 review 再次被挖出。

### C. 在 AnalysisHandler 內部為 CCI 追加 exit-log suppression（codex 建議，rejected）

在 sentinel 或 runErr 邏輯中加一個分支，對 expected failure 靜默不發 exit log，以「在 Tier-2 內模擬 Tier-1 行為」。拒：為維持 AnalysisHandler 而引入 CCI 專屬條件分支，正好是 Option A 試圖消除的 divergence；複雜度不降反升。

### D. 雙 HandlerRun name param（codex 建議，rejected）

在 `HandlerRun` 本身加一個 `name` 參數，讓 Tier-2 包裝器可傳入中文名稱，以維持 handler 留在 Tier-2 的同時修正 panic diagnostic 名稱。拒：為解決 `AnalyzeCCI` 一個的 naming 問題而改動通用 Tier-1 API；且根本問題（雙 recover 稅、結構分歧）仍在。

## Consequences

- **`AnalyzeCCI` 離開 Analysis pipeline family**（CONTEXT.md 已更新）：家族由 4 → 3 member（`AnalyzePhases`、`AnalyzePhaseSync`、`AnalyzeMuscleRatio`）。`AnalysisHandler[P,R]` 樣板仍服務剩餘 3 個成員。
- **`cci` domain analyzer 不受影響**：`internal/cci` 的 compute-only 形狀、ADR-0004 Boundary 2、ADR-0012 的「CCI compute-only vs muscle_ratio compute+write」分歧全部不變。
- **兩個 sentinel 消失**：`ErrCCIAnalysisFailed`、`ErrCCICSVExportFailed` 為確認死碼（無 `errors.Is` 外部 caller）；移除後 production 行為不變，expected failure 仍以 `failedCCIResult(fmt.Sprintf("...: %s", redact.RedactForMessage(err)))` + `nil` err 回傳（single-channel envelope，**不**走 Go err 通道；僅 panic 才走 err 通道）。
- **測試**：nil-logger panic test 斷言存活（HandlerRun 在 logger 前無呼叫）；`runErr` 分流相關的既有覆蓋隨之溶解；Tier-1 六步 body 的 failure-path 測試覆蓋直列路徑。

## Reversibility

成本低（git revert），但本 ADR 鎖住：CCI 的 GUI handler 形狀定義為 Tier-1 HandlerRun、與 `AnalyzeNormalizedPhaseSync` 保持鏡像。若未來 CCI 的業務形狀回歸到三步管線（單 CSV 輸出、單 recover 足夠），才有回到 Tier-2 的正當理由 —— 不應以「統一都用樣板」為由強迫相同結構用不同層。

## Related

- [[ADR-0018]]（**amended** — GUI 放置理由 lines 45 & 54 由本 ADR 取代；CCI compute-only 不變式仍成立）
- [[ADR-0004]]（format-aware write Boundary 2，GUI handler 外寫 CSV，本 ADR 後仍維持）
- [[ADR-0012]]（CCI compute-only vs muscle_ratio compute+write 刻意分歧，本 ADR 後不動）
- [[ADR-0025]]（CCI Output-2 對稱寫入 `*cci.CCIAnalysisResult`，本 ADR 的 Output-2 step 以此為基礎）

## Notes

- **GUI smoke 未驗**：native webview 在 headless 環境下無法呼叫 `window.go` binding（見 `feedback_wails_dev_browser_binding_gap`）。本次重構對 RPC 簽名、前端 binding 與 result struct 均無變更，risk 低；但比照 repo 慣例，GUI smoke 標記為 **unchecked**。
- **CONTEXT.md 更新**：`Analysis pipeline family` 概念塊由 4 → 3 member，`_Not included_` 新增 `AnalyzeCCI` 離開理由，`Domain analyzer` 概念塊的 membership 旁注更新為「兩層現在都是 3 member，但集合仍不相同」── 完整保留「兩軸、不同集合」的 ADR-0012 設計意圖。
- 本 ADR 屬 2026-06-19 CCI Tier-1 HandlerRun 遷移，與 [[InputValidator facade collapse 候選#2]] 同日正交。
