# 退役 AppError；err 通道出口在地化

**Status**: accepted · **implemented** (2026-10-11) · 延伸 [[ADR-0048]]（實作其 Option E 附註）

[[ADR-0048]] 讓 [[Domain analyzer]] 回帶 key 的 `i18n.Error`，由 [[Webview envelope]] 的 Message 通道以 `i18n.Localize` 渲染；但 err 通道（bound method 回的 Go err）仍只送 `redact.Paths(err.Error())`，即 zh-TW。同時 `internal/errors` 的 `AppError` / `ValidationError` 是另一套錯誤模型：`Error()` 組出「`[CODE] 訊息 - 詳細: … - 原因: …`」或「`[DATA_VALIDATION] 欄位 '…' 驗證失敗: … (值: …)`」，文字只有中文、會回顯原值（含完整路徑與 header cell），`ErrorCode` / `Recoverable` 沒有任何 caller 拿來分流，唯一讀它們的是 logger 的 `error_code` / `recoverable` 欄位。

W11 的前一步把所有 producer 改為帶 key 的 `i18n.Error`（io `ReadCSV`、`validation/csv`、`validation/filename`、gui legacy panel 與 MaxMean 批次 adapter）或 `fmt.Errorf`（`internal/benchmark`）。本 ADR 收尾：刪除型別，並讓 err 通道出口在地化。

## Decision

1. **err 通道出口先在地化、後脫敏**。`gui/recover.go` 的 `redactForWebview` 改名 `renderForWebview`，文字 = `redact.Paths(i18n.Localize(err))`：
   - 文字與 `err.Error()` 相同（zh-TW 下不含路徑，或 nil）時回原 err，caller 的 error identity 不變（`TestRecoverHandlerPanic_NoPanic_LeavesErrUnchanged`）。
   - 否則回 `webviewErr{msg, original}`：`Error()` 是在地化、脫敏後的文字，`Unwrap()` 回原 err，`errors.Is` / `errors.As` 照常走到 `*i18n.Error`、`*fs.PathError`（保留原路徑，只供 Go 端判斷）。
   - **順序不可對調**：`Localize` 在 `err.Error()` 裡找 `*i18n.Error` 的 zh-TW 原文再換成目前 locale；先 redact 會改掉原文裡的路徑，找不到就整段退回 zh-TW。
2. **刪除 `AppError`、`ValidationError`、`ErrorCode`、`IsRecoverable`**（`internal/errors/errors.go` 與其測試，含 `NewAppError` / `NewAppErrorWithDetails` / `WrapError` / `NewValidationError` / `WithContext`）。`calculator_errors.go`（計算層 sentinel、`CalculatorError`、`WrapParseError`）保留，package 名不變、改寫 package doc。
3. **logger 的 `addErrorInfo` 只留 `entry.Error`**：拿掉 `AppError` 分支（`error_code`、`recoverable`、`AppError.Context` 併入 log context）與「保留 key」的註解。
4. **測試**：
   - `TestRecoverHandlerPanic_NoPanic_LocalizesKeyedErr`：zh-TW 回原 err；en-US 為英文、`errors.As(*i18n.Error)` 仍取得 key。
   - `TestRecoverHandlerPanic_NoPanic_LocalizesThenRedactsPath`：en-US、Cause 是帶絕對路徑的 `*fs.PathError` → 英文且路徑脫敏、`errors.As(*fs.PathError)` 保留原路徑。
   - `TestBoundHandler_ErrChannelLocalizedThenRedacted`：從 `CalculateMaxMean` 端到端驗 en-US 讀不存在的檔。
   - 刪除：`errors_test.go` 的 AppError / ValidationError / IsRecoverable 測試；logger 的 `TestLogger_ErrorWithAppError`、`TestLogger_ReservedKeysNotOverriddenByContext`（測的分支已刪除）；另兩處 logger 測試改用一般 error。

### 延伸 / 修訂既有 ADR

- **[[ADR-0048]]**：Option E 附註「err 通道需要時可在 `recoverHandlerPanic` 出口改用 `Localize`，不必讓 `Error()` 變動」由本 ADR 實作；其 Consequences「err 通道不在地化」不再成立。`Error()` 仍固定 zh-TW。
- **[[ADR-0036]]** Decision 1：無 panic 分支由 `redactForWebview` 改為 `renderForWebview`，`webviewErr` 的文字多了在地化一步；單一出口、`Unwrap` 保留 chain 的設計不變。

兩份 ADR 依 repo 慣例不修改。

## 行為變更

- **zh-TW 錯誤文字少了 AppError 外框**：沒有 `[CODE]` 前綴、「詳細:」「原因:」段，也沒有 ValidationError 的「欄位 '…' 驗證失敗: … (值: …)」回顯；過大只帶 MB 數、非一般檔案不帶路徑、CSV 驗證只帶行號 / 欄號、檔名規則只帶觸發規則的片段（W11 前一步）。
- **log 不再有 `error_code` / `recoverable` 欄位**，`AppError.Context` 的欄位也不再併入 log context；caller 傳入的同名 context key 不再被覆蓋（不再是保留 key）。
- **非 zh-TW locale 下 err 通道改送該 locale 的文字**：帶 key 的部分（io `ReadCSV`、`validation/csv`、`validation/filename`、legacy panel 與 MaxMean 批次的 `error.handler.*` 前綴、analyzer 的 `i18n.Error`）以目前 locale 渲染，路徑仍脫敏。zh-TW 下文字與過去逐位元組相同 —— 限內建 catalog：外部 zh-TW 翻譯 JSON 覆寫了某個 key 時，`Localize` 用覆寫後的文字而 `Error()` 用內建文字，兩者不同，err 通道回 `webviewErr`、送出覆寫後的文字（[[ADR-0048]] Consequences）。
- **仍是中文的部分**（不在本 ADR 範圍）：calculator、Output placement、security、manifest loader 的包裝訊息，以及 io `listEntries` 的「無法讀取目錄 …」，在 en-US 下仍顯示中文，與英文前綴混排。
- **gui err 通道自己的 sentinel 也仍是中文**：panic 分支的 `ErrInternalPanic`（「內部錯誤，請聯絡技術支援（無需重啟）」）與其 `handler=` 欄位的中文 handler 名（「CCI 分析」「階段分析」等），以及 `ErrAppNotReady`（「App 尚未完成啟動，請稍候再試」）—— 都是 `errors.New` 字面值，不經 `Localize`。
- golden（真實 V.16 資料走 GUI handler，預設 zh-TW）與 W10、W11 前一步逐位元組相同。

## Why

- **一套錯誤模型**：使用者可見的錯誤只剩帶 key 的 `i18n.Error`（分類用 `errors.As(...).Key`、顯示用 `i18n.Localize`）與計算層 sentinel（`errors.Is`）。`AppError` 的 `Code` / `Recoverable` 沒有 caller 分流，只留下中文外框與回顯原值的 PII 風險。
- **err 通道與 Message 通道同一條 i18n 規則**：[[Webview envelope]] 的三個 helper 早已先在地化、後脫敏；err 通道是唯一例外，en-US 使用者在 legacy panel 看到整段中文。出口只有一個（`app_panic_ast_test.go` 強制每個回 error 的 bound method 首句是 `recoverHandlerPanic`），改一處即涵蓋全部。
- **`Error()` 維持 zh-TW**：log 跨 locale 一致、既有逐位元組斷言不必改；語言仍只在送進 webview 的最後一站決定（[[ADR-0048]] Why 最後一點）。

## Considered Options

### A. 出口 `redact.Paths(i18n.Localize(err))`，文字不變時回原 err（chosen）

見 Decision。zh-TW 下 identity 不變，golden 與 sentinel 比較不受影響。

### B. 先 redact 再 Localize（rejected）

`Localize` 以 `*i18n.Error` 的 zh-TW 原文在 err 文字裡定位；先 redact 會改掉原文裡的路徑（`*fs.PathError` 的 Cause、路徑型 Args），定位失敗就整段退回 zh-TW —— 正是 en-US 下帶路徑的錯誤仍是中文的情形。

### C. `Error()` 依目前 locale 渲染（[[ADR-0048]] Option E 本文，rejected）

理由同 ADR-0048：log 不一致、逐位元組斷言都要釘 locale、`Error()` 依賴 global 是否初始化。

### D. 保留 `AppError`，加 `Key` 欄位（rejected）

兩套錯誤模型並存，`[CODE]` 外框與回顯原值仍在；`Code` / `Recoverable` 依舊沒有 caller。

### E. 各 handler 自行在地化 err（rejected）

回 error 的 bound method（目前 15 個）各寫一次，漏一個就退回中文；單一出口已存在且有 AST 守門。

## Consequences

- `internal/errors` 只剩計算層錯誤（sentinel、`CalculatorError`、`WrapParseError`）。
- err 通道在非 zh-TW 下回 `webviewErr`（而非原 err）；用 `==` 比對 sentinel 的 caller 在 en-US 會失準，應改用 `errors.Is`。目前 Go 端沒有 caller 在 bound method 回傳後再比對（前端只顯示 `String(err)`）。
- `Localize` 的限制（[[ADR-0048]] Consequences）同樣適用於 err 通道：只在地化 `errors.As` 找到的第一個 `*i18n.Error`；wrapper 改寫其文字時退回 zh-TW。
- GLOSSARY：**Webview envelope** 的 err 通道描述改為「先在地化再 redact」，刪除「err 通道不在地化」；`docs/api.md`、`docs/usage_patterns.md` 的錯誤處理段改寫為 `i18n.Error` + `errors.As(...).Key` + `i18n.Localize`。

## Related

- [[ADR-0048]]（**extended** —— Option E 附註由本 ADR 實作；「err 通道不在地化」不再成立）
- [[ADR-0036]]（**amended** —— `redactForWebview` → `renderForWebview`，出口多一步在地化）
- [[ADR-0054]]（檔名規則的單一裁判；其錯誤改為帶 key 的 `i18n.Error`）
