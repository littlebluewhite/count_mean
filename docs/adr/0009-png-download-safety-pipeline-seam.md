# PNG 下載安全管線收進單一 helper

**Status**: accepted (2026-05-29)

## Decision

`DownloadCCIChart` 與 `DownloadChartComposerImage` 共享同一條 PNG 下載安全管線：檢查 `data:image/png;base64,` prefix、剝掉 dataURL header、呼叫 `DecodeAndValidatePNG`、以 `"PNG 輸出路徑"` 跑 `validateExternalPathInputs`、再用 `fsperm.WriteFileNoFollow` 寫檔並回傳成功的 `ChartResult`。把這條管線收進 `gui/png_download.go` 的 private App method：

```go
func (a *App) downloadValidatedPNG(imageData, outputPath string) (*ChartResult, error)
```

兩個 Wails RPC handler 只保留 adapter 差異：`DownloadCCIChart` 從 `params.Subject` sanitize 後組出 `{safeSubject}_CCI_Rudolph.png`；`DownloadChartComposerImage` 保留 nil params guard，sanitize `filepath.Base(params.OutputPath)`，必要時補 `.png`，再與原目錄組回 final `outputPath`。`recoverHandlerPanic` 留在 RPC handler，因為 panic recovery 是 entrypoint contract，不是 PNG 寫檔 helper 的責任。

`ErrInvalidImageFormat` 與 `DecodeAndValidatePNG` 維持 live，不搬也不刪；本決策只讓它們從「兩個 caller 都知道整條管線」變成「一個 helper 持有管線」。`CONTEXT.md` 不新增詞條：PNG 下載安全管線是 implementation module，不是 EMG 分析領域概念。

## Why

這是 ADR-0004 format-aware write 的同型收束：caller 不應各自知道完整的輸出安全流程。ADR-0004 把 CSV row layout、filename ownership 與 validated write 收進 CSVHandler；本 ADR 把 PNG dataURL 驗證、輸出路徑驗證、no-follow 寫檔與成功 result shape 收進一個 GUI-side helper。兩者的共同點是把「每個 caller 都要手動照抄才安全」的知識集中到單一 module，換取 locality 與 test leverage。

手動同步風險已經出現在現有 code：`DownloadChartComposerImage` 的註解明說它在「鏡像 DownloadCCIChart」並「對齊 DownloadCCIChart」。這代表 seam 已經存在，只是藏在兩份 copy-paste 裡。刪除測試也通過：收束後兩個 handler 的重複管線會消失，並不只是位移到另一個同樣 shallow 的 wrapper；helper 的 interface 只需要 `imageData + final outputPath`，背後隱藏整條驗證與寫檔順序。

## Considered Options

- **Helper 收 `imageData + final outputPath`（chosen）**：sanitize 與 output-path 推導留在 handler adapters。這保留真實差異：CCI 的未信任片段是 Subject，Composer 的未信任片段是 output path base。helper 只接收已推導好的 final path，負責共同安全管線。
- **Helper 收 outputDir / subject / raw OutputPath 並內部決定 sanitize 規則**：拒絕。這會把 CCI filename convention 與 Composer file-dialog convention 混進 helper，讓 shared module 反而知道兩個 adapter 的私有語意。
- **Helper 收已剝 header 的 base64 payload**：拒絕。prefix check 與 `ErrInvalidImageFormat` 是目前兩處完全相同的安全管線一環，若留在 caller，最容易再次 drift。
- **Pure function 而非 `*App` method**：拒絕。helper 需要用 `a.logger` 記錄成功輸出，且語意屬於 App 的 GUI handler layer。它不需要 state，因為 final `outputPath` 已由 caller 推導。
- **把 panic recovery 移進 helper**：拒絕。`recoverHandlerPanic` 是 Wails RPC entrypoint 的 named-return contract；helper 在 handler 的 defer scope 內被呼叫即可。

## Test Migration

實作時把共享管線測試搬到 helper seam：bad prefix 應回 `ErrInvalidImageFormat`、base64/PNG decode failure 應 wrap 成 `"PNG 驗證失敗: %w"`、traversal/sensitive output path 應由 `validateExternalPathInputs("PNG 輸出路徑", outputPath)` 擋下且不產生重複 label、leaf symlink output path 應由 `fsperm.WriteFileNoFollow` 擋下。兩個 handler 的測試縮成 adapter assertions：CCI 算出正確 `{safeSubject}_CCI_Rudolph.png`；Composer nil params、base sanitize 與 `.png` extension normalization 保持不變。`gui/png_validation_test.go` 繼續測 `DecodeAndValidatePNG` 的深層 validator，不搬。

## Reversibility

中。若未來 CCI 與 Composer 的 PNG 寫檔規則真的分歧，可以重新拆開 helper；但拆開後必須再次手動維持 prefix check、validator、path label、no-follow write 與 result shape，這正是本 ADR 要避免的 drift surface。

## Process note

2026-05-29 grilling 開場已重新 cross-check：`DownloadCCIChart` 與 `DownloadChartComposerImage` 仍各自直接呼叫 `DecodeAndValidatePNG`、`validateExternalPathInputs("PNG 輸出路徑", ...)` 與 `fsperm.WriteFileNoFollow`；`ErrInvalidImageFormat` 仍由兩個 download handler 共用；`docs/adr/0009-*` 尚未存在。工作樹另有既存 untracked `docs/adr/0005-calculator-family-keep-dual-shape-interface.md`、`input/NSF1/` 與 `output/NSF1_chart_composer.png`，本 ADR 不碰它們。
