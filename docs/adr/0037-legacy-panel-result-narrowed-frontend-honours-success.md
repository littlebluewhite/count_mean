# Legacy panel result 收窄 + 前端尊重 success

**Status**: accepted · **implemented** (2026-10-09)

最大平均值 / 標準化 / 階段分析三個 legacy panel 的 result 型別（`MaxMeanResult` / `NormalizeResult` / `PhaseResult`）背著前端從不讀的重資料欄位（`Headers`、`Results`、`Data`、`[]PhaseAnalysis`），各自配一個 convert 函式；成功 `Message` 還內嵌輸出檔絕對路徑。前端 `main.js` 三個 handler 只 `catch` rejected promise，**完全不看 `result.success`** —— 批次全部失敗時後端回 `Success=false`，前端仍跳「計算完成」。本 ADR 接續 [[ADR-0036]]（[[Webview envelope]]）。

## Decision

1. **三個 result 收窄為 `{OutputPath, Success, Message}`**。刪除 `PhaseAnalysis`、`convertPhaseResultToAnalysis`、`convertMaxMeanResultsToArray`、`convertNormalizedDataToArray`、`io.ReverseScale`，以及 `maxmean.BatchResult.Headers/Results` 累積（無其他讀者；`BatchResult` 只留 `SuccessCount` / `FailCount`）。對應的轉換 / 反縮放測試一併刪除；輸出 CSV 的縮放契約由 `internal/io` 既有測試守住。
2. **成功 `Message` 不含絕對路徑**：若指名輸出檔只用 base name（`filepath.Base`）；絕對路徑只走 `OutputPath` 欄位。前端成功對話框原本就用 `result.outputPath` 組 `success.msg.*_done`，不受影響。
3. **前端尊重 `success`**：`calculateMaxMean` / `normalizeData` / `analyzePhases` 在 `!result.success` 時設失敗狀態並 `ShowError(t('dialog.error'), result.message)`，不跳成功對話框。批次全部失敗的 Message 沿用「批次處理完成：成功 N 個檔案，失敗 M 個檔案」，不另增 i18n key。
4. **刪除 `GetCSVHeaders` RPC**（連同 `CSVHeadersParams`）：`main.js` 只 import、從未呼叫。
5. **err-vs-result 切分不變**：三個 handler 仍以 Go err 通道回傳可預期失敗（出口由 `recoverHandlerPanic` redact，[[ADR-0036]]）；`Success=false` 的 result 只出現在批次「全部失敗」。紅燈測試 `TestCalculateMaxMean_BatchAllFailed_SuccessFalse` 釘住此行為 —— 後端在此前已回 `Success=false`，所以該測試在後端修改前即為綠，真正的缺陷在前端未讀 `success`。

### Amends ADR-0026

[[ADR-0026]] §4 寫「`BatchResult` 回 domain 型 `[]models.MaxMeanResult` + 計數；GUI adapter 做 `convertMaxMeanResultsToArray`」。現改為 `BatchResult` 只回計數，GUI adapter 只組 Message 與 `OutputPath`。

## Why

- 前端從不讀這些陣列；保留它們等於維護一條無人消費的輸出路徑，且 `ReverseScale` 讓 GUI 與 CSV 兩處各算一次單位。
- 成功 Message 若帶絕對路徑，等於為 [[Webview envelope]] 留一個未 redact 的出口。
- 後端有 `Success` 欄位但前端不讀，契約形同虛設。

## Considered Options

- **保留陣列欄位，只修前端**：拒。無消費者的欄位仍帶維護成本與路徑 / 單位分歧風險。
- **批次部分失敗時前端也顯示 Message**：拒（本次不做）。目前只有全部失敗走失敗路徑；部分失敗仍顯示成功對話框 + 輸出目錄，失敗檔案列於 log。若要呈現計數需新增 i18n key，另案處理。
- **刪除 `GetCSVHeaders` 前保留為 deprecated**：拒。無 caller，Wails 綁定會一併帶出。
