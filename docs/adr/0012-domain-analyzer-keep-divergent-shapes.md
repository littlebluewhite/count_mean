# Domain analyzer 三 sibling 維持刻意分歧的形狀 — 拒絕「把 cci / phase_sync / muscle_ratio 收成統一 analyzer interface」深化

**Status**: accepted (2026-05-29)

## Decision

`internal/cci.AnalyzeCCI` / `internal/phase_sync.AnalyzePhaseSync` / `internal/muscle_ratio.Analyze` 三個 [[Domain analyzer]] 維持目前**刻意分歧**的形狀，不收成統一 interface。分歧沿兩條正交軸：

| Domain analyzer | Subject cardinality | Output ownership | 簽章 |
| --- | --- | --- | --- |
| `cci.AnalyzeCCI`（analyzer.go:61） | single-subject | compute-only | `(ctx, *CCIParams) (*CCIAnalysisResult, error)` |
| `phase_sync.AnalyzePhaseSync`（analyzer.go:494） | single-subject | compute-only | `(ctx, *models.AnalysisParams) (*models.EMGStatistics, error)` |
| `muscle_ratio.Analyze`（analyzer.go:84） | **batch** | **compute+write** | `(ctx, *Params) ([]SubjectResult, error)` |

2026-05-29 architecture review 的 Candidate 3「三 analyzer 收成統一 interface」**拒絕**，採 preserve + document：新增 [[Domain analyzer]] CONTEXT 詞條命名此層與 `Subject cardinality` / `Output ownership` 兩軸，本 ADR 記錄各 member 落點與保留理由。**不動 code**。

## Why

- **Deletion test 兩軸都不過。** 統一只把分歧的複雜度從 analyzer 位移到 caller，不讓它消失：
  - *Cardinality*：把 cci / phase_sync 收成 batch `[]Result`，single-subject GUI panel（subject dropdown → 單一 [[Subject]]）得 index `[0]` 並背 batch-of-one 的 partial-success 處理；把 muscle_ratio 收成 single，subject 迴圈 + partial-success 聚合反推回 GUI caller — 正是 `muscle_ratio.Analyze`（analyzer.go:131 的 manifest 迴圈）今天吸掉的東西。
  - *Output ownership*：把 muscle_ratio 收成 compute-only，GUI 得跨 batch 處理 per-subject Output-1 / Output-2 sticky-success 語意（直接違反 [[ADR-0004]] Boundary 1）；把 cci / phase_sync 收成 compute+write，它們的 `Params` 得帶 `OutputDir` + `CSVHandler`，且 GUI `AnalysisHandler[P, R]` 的 `WriteCSV` closure 單一寫檔 seam 失去對稱（違反 [[ADR-0001]] / [[ADR-0004]] Boundary 2）。
- **muscle_ratio 的兩條分歧是同源，不是兩個獨立怪癖。** batch unit-of-work 帶來 per-subject partial-success（一個 subject 缺通道不該中止整批），而 per-subject sticky write 自然落在迴圈內 → 寫檔 ownership 進 analyzer（[[ADR-0004]] Boundary 3 的 `WriteCSV: nil`）。所以 muscle_ratio 在兩軸**同時**偏離 cci / phase_sync，是一個決策的兩面。兩軸在**設計空間**上正交（任一組合可表達），但對 muscle_ratio 這個 member 由同一需求驅動。
- **membership 判準 = manifest + dataFolder 驅動。** 三者都載 manifest → 解 subject → parse EMG → compute，math 下放 calculator kernel。GUI `AnalyzePhases`（app.go:824）雖在 [[Analysis pipeline family]] 內，但吃單一 raw CSV input file（app.go:853）並委派 `calculator.PhaseAnalyzer`（app.go:858，[[ADR-0005]] calculator family），不符判準 — 所以 GUI handler 家族 4 member、domain analyzer 層只有 3，odd-one-out 是 principled 而非疏漏。
- **每個 analyzer 恰一個 caller**（各自 GUI handler 的 `Execute` closure：cci_handlers.go:73、app.go:1179、muscle_ratio_handlers.go:87），沒有第 4 種 shape 在別處複製。分歧是封閉、可控的，不是失控擴散。

## Considered Options

- **A. 統一 cardinality（全 batch 或全 single）**：拒。deletion test 不過 — single-subject panel 與 batch 聚合互相位移，沒有 caller 因此變簡單。
- **B. 統一 output ownership（全 compute-only 或全 compute+write）**：拒。compute-only 化 muscle_ratio 撞 [[ADR-0004]] Boundary 1 sticky-success；compute+write 化 cci / phase_sync 撞 [[ADR-0001]] / [[ADR-0004]] Boundary 2 的 `WriteCSV` closure 對稱。
- **C. 收成單一 `Analyze(ctx, params) ([]Result, error)` + 內部寫檔**：拒。A + B 的雙重壞處疊加，且把「batch 與 single 是兩種 dimension」「compute 與 write 是兩種 ownership」的真實 asymmetry 一起遮蔽，reversibility 最低。
- **D（本次採）. preserve + document**：CONTEXT 命名層與兩軸，ADR 記錄落點與理由。零 code 動作，把未來 reviewer 會問的「為何三個不一樣」釘在族譜上，擋住「為對稱而收」的回潮。

## Reversibility

低成本 — preserve 是維持現狀，無 migration 動作。日後若觸發條件出現（例：真有第 4 種分析需要 batch + compute-only 這個目前空著的 cell、或 CLI mode land 後 single / batch 的 ergonomic 權衡改變、或 [[ADR-0004]] Boundary 2 的 filename ownership re-collapse 落地連動 output ownership 軸），可重啟 grilling。本 ADR 不鎖死，只記錄「2026-05-29 此時間點下兩軸分歧刻意保留」的理由。

## Related

- [[ADR-0004]] — `Output ownership` 軸的根：muscle_ratio sticky-success 留在 Analyzer（Boundary 1）、filename ownership 沿 unit-of-work 切分（Boundary 2）、`AnalyzeMuscleRatio` 的 `WriteCSV: nil` 是設計而非 TODO（Boundary 3）。本 ADR 把該 ownership 邊界提升為 [[Domain analyzer]] 的一條命名軸。
- [[ADR-0005]] — 同一條 deletion-test-preserve 推理的 sibling 先例（calculator family 維持 dual-shape、拒收統一）。[[Domain analyzer]] 與 calculator family 是上下層：前者 orchestrate、後者是被委派的 math kernel。
- [[ADR-0001]] — phase_sync 寫檔由 Analyzer 搬到 CSVHandler，令 phase_sync 落在 compute-only / GUI-writes 一側。
