# CCI compute core 與 I/O adapter 分離 — `computeCCI` method seam

**Status**: accepted (2026-06-15) · implemented

## Context

`AnalyzeCCI`(`internal/cci/analyzer.go`)把兩件截然不同的事焊死在一起：

1. **I/O 前置**：`loadAndValidate` + `loadEMGData`(讀檔、驗證、回傳 in-memory data)
2. **Compute 組裝**：`BuildChannelMap` → gait re-anchor → ±150ms extract → pair CCI → `dropOutOfRange` → `buildPhaseStats`

組裝段正是 ADR-0018(gait 重錨 + 0% = S)與 ADR-0022(區間中點 ±50ms 視窗)的邊界邏輯所在，卻沒有任何 file-free seam：每一條觸及它的測試路徑都要在 disk 建真實檔。既有的測試覆蓋也留有明確的缺口：

- `analyzer_test.go`：`%`-filename / 無 race 的測試**不斷言 compute 正確性**，目的是驗 I/O 邊界行為。
- `cancellation_test.go`：只測取消語意。
- `phase_stats_test.go` 的 SF8 端到端正確性測試：有真實 EMG 檔時執行，**CI / 乾淨 checkout 下直接 skip**。

結果是：「組裝正確」這個最需要被 always-run 測試守住的事實，沒有 always-run 測試保護它。

## Decision

在 `analyzer.go` 新增一個 method：

```go
func (a *CCIAnalyzer) computeCCI(
    ctx context.Context,
    emgData *models.PhaseSyncEMGData,
    m *models.PhaseManifest,
) (*CCIAnalysisResult, error)
```

「filesystem line」之後的所有組裝邏輯搬進此 method。`AnalyzeCCI` 縮身為薄 I/O adapter：讀入 `emgData` / `m`，再委派 `computeCCI`。Subject 由 `m.Subject` 取得；ctx 保留全程（支援熱迴圈取消）；`computeCCI` 進入點加一道 `ctx.Err()` 快速返回守衛，與 repo 既有的進入點 pre-cancel 慣例一致。

**不新增** `compute.go`；**不搬移** 現有 helper；diff ≈ +1 method。

### 為何選 receiver method，而非 free function

`computeCCI` 需要 `a.timeSynchronizer` 與 `a.logger`。若改成 free function，就必須把這兩個 dep 注入為參數，增加簽章複雜度。`NewCCIAnalyzer()` 本身不做任何 I/O，因此 receiver method 就已經是 file-free by construction：測試只需組建 analyzer、直接呼叫 `computeCCI` 並傳 in-memory data，無需 disk。

### 為何不碰 `phase_sync`

`phase_sync.AnalyzePhaseSync` 已把 Load 與 compute 分離，是這個模式的先例，無需改動。

## Why

**唯一主因是可測試性，而非「有多個 adapter」。** `computeCCI` 目前只有一個 production caller，用「多 adapter 合理化 seam」根本站不住腳。真正的理由是：regression-prone 的組裝段沒有 always-run 的正確性斷言；提取出來的 seam 本身就是測試介面。

**外部行為不變**，除了一個取消優先順序的邊緣精化。`AnalyzeCCI` 在讀檔後仍保有既有的 `ctx.Err()` 守衛，因此唯一受影響的是「該守衛通過後、`computeCCI` 進入點新守衛之前」這個函式呼叫間隙：若 ctx 恰在此間隙被取消，新守衛會立即回傳 `ctx.Err()`（`context.Canceled` 或 `context.DeadlineExceeded`），搶占原本會先執行的 pre-extract 組裝（`BuildChannelMap`→`calculateGaitCycle`→extract→`GetEMGDataInTimeRange`）所產生的任何錯誤或結果——而非僅「channel map 無效」一種。**在取消時回傳 `ctx.Err()` 才是正確契約**，不是 regression。

## Considered Options

- **Free function**：需額外注入 `timeSynchronizer` + `logger`；receiver method 用 `NewCCIAnalyzer()`（零 I/O）已達同等 file-free，代價更低。拒。
- **新增 `compute.go` / 搬移 helper**：不必要的搬移，diff 變大且無 payoff。拒。
- **觸碰 `phase_sync` 或抽共用 kernel**：`phase_sync` 已有正確形狀、無缺口；共用 kernel 違反 ADR-0012(三 Domain analyzer 維持刻意分歧)。拒。
- **只加強現有 file-based 測試**：正確性斷言仍依賴 disk，SF8 在 CI 仍 skip，缺口沒有補上。拒。

## Relationship to ADR-0012

這是 **CCI 套件內部** I/O adapter / compute core 的切割，不是跨 analyzer 的共用 kernel。ADR-0012 確立的三個 Domain analyzer 刻意分歧形狀**完整保留**：`cci` 仍是 single-subject / compute-only；其外部簽章與 output ownership 一律不變。

## Consequences

新增 `computeCCI_test.go`，以 hand-built in-memory inputs 驗證 gait re-anchor + `dropOutOfRange` + phase-stats 組裝的正確性，且在 CI / 乾淨 checkout 下 always-run。`AnalyzeCCI` 的公開簽章不變，外部行為除上述取消優先序精化（見 Why）外不變；既有全部測試繼續通過。

**CONTEXT.md 不動。** "compute core" / "I/O adapter" / "seam" 是架構詞彙，屬 LANGUAGE.md 語境，非 CONTEXT.md 領域術語。Domain analyzer 的領域職責（load → parse → compute）沒有改變，不需要更新領域詞彙表。

## Related

- [[ADR-0004]] — Format-aware write 邊界（Boundary 2：compute 結構不帶 file-context）；本 seam 把 I/O 留在 adapter、compute core 維持 file-free，與此原則一致。
- [[ADR-0012]] — Domain analyzer 三 sibling 刻意分歧；本決策屬 intra-package 切割，不動跨 analyzer 邊界。
- [[ADR-0018]] — Gait 重錨 + Output 2 分期視窗統計；`computeCCI` 包含並保護這段邏輯。
- [[ADR-0022]] — 區間列改為中點 ±50ms 視窗；`buildPhaseStats` 位於 `computeCCI` 中，由新 always-run 測試守住。
