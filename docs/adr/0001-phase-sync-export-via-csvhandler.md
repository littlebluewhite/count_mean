# PhaseSync 的 CSV export 整併進 csvHandler 統一路徑

**Status**: accepted (2026-05-27)

## Decision

`PhaseSyncAnalyzer.ExportResults`（`internal/phase_sync/analyzer.go:571`）拆掉，PhaseSync 的 CSV 寫入改走 `io.CSVHandler.WritePhaseSyncResult`，與其他三個 analysis handler 走同一條 format-aware write 路徑。`internal/phase_sync` 不再持有寫檔職責，自包性失去；換得的是 [[Analysis pipeline family]] 在 CSV 寫入這一步是單一明確策略。

## Why

候選 6 引入 [[AnalysisHandler[P, R]]] 樣板，第 3 步是 CSV write。若 PhaseSync 維持 analyzer-internal 的 `ExportResults`，樣板就要在第 3 步包容兩種策略，generic 邊界變寬鬆，違背樣板「單一明確管線」原則；且候選 2（format-aware write）剛收乾的 invariant「caller 不再看到 row layout」會留在 75% 覆蓋率，PhaseSync 成為違例。統一是擴張候選 2 invariant 到 100% 的順手機會。

## Considered Options

- **維持並存**：csvHandler 走主流，PhaseSync 繼續 `ExportResults`。拒絕原因：樣板 generic 簽章被迫接受兩種 CSV write 策略，第 3 步抽象的 depth 直接打折，且 future-proofing 上 CCI/MR 之一也可能想跟著 ExportResults 走，殷鑑不遠。
- **反向下沉**：放棄候選 2，讓所有 analyzer 都各自持有 ExportResults。拒絕原因：直接撤銷候選 2 的成果，且每加一個 analyzer 都要重新發明一份 row layout / sanitize / BOM 處理。

## Reversibility

低。一旦四個 analyzer 都統一走 csvHandler，回頭分裂要重新發明 internal export API 並 migrate test，預估代價 ≥ 1 工作天。
