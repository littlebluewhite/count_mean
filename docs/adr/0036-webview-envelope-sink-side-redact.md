# Webview envelope：err 通道出口 redact + failMessage 唯一建構

**Status**: accepted · **implemented** (2026-10-09)

Go 端文字有兩條路進 Wails webview：**err 通道** —— bound method 回的 Go err，Wails dispatcher 以 `err.Error()` 序列化成 rejected promise，frontend `main.js` 原樣顯示；**Message 通道** —— failed result 的 `Message`，以及 `MuscleRatioSubjectDTO.Error`、Chart Composer `MissingFileDTO.ErrMessage` 等字串欄位。過去兩條通道要不要 redact 逐分支決定：約 31 處手寫 `fmt.Sprintf("<前綴>: %s", redact.RedactForMessage(err))`、9 個 err 通道 handler 原文送出、MR `Subjects[i].Error` 原樣轉送 analyzer 字串 —— 共 10 個實測可重現的病患路徑洩漏點。log 早有單一出口（`Logger.sanitizeMessage`），webview 沒有。

本 ADR 把兩條出口合稱 **[[Webview envelope]]**，記錄 W2「GUI handler seam」的兩步：err 通道出口 redact（commit `862fe9e`，當時未寫 ADR）與 Message 通道唯一建構 + handler 層 i18n。接續 [[ADR-0035]]。

## Decision

1. **err 通道：在 `recoverHandlerPanic` 出口 redact**。無 panic 分支改為 `*errPtr = redactForWebview(*errPtr)`：
   - `webviewErr{msg, original}`（仿 `panicErrWithChain`）：`Error()` 只回 `redact.Paths` 後文字，`Unwrap()` 回原 err，`errors.Is/As` 仍走得到完整 chain（含帶原路徑的 `*fs.PathError`，只供 Go 端判斷，不序列化進 webview）。
   - 文字沒變（不含路徑或 nil）就原樣回傳原 err，保留 identity。panic 分支不變。
   - [[ADR-0035]] 的「首句 defer」AST 規則保證每個回 error 的 bound method 都經過這裡 —— 這是所有 Go err 進 webview 前的唯一出口，handler 不必各自 redact err。
2. **Message 通道：失敗文字只能經 `gui/envelope.go` 三個 helper 建構**（範圍是失敗訊息 —— 成功分支的 Message 由 handler 直接組、不經 helper、不過 redact，故不得帶目錄路徑；PNG 下載的「圖表已下載至: 」原本接 outputPath 絕對路徑，已改為只接檔名）：
   - `(a *App) failMessage(key, err)`：可預期失敗（下游 analyzer / IO / 計算錯誤）。回 `i18n.T(key) + ": " + redact 後的 err 文字`，並以 `a.logger.Error` 記一次；handler 分支不另打 Error log。log 訊息是 localized 前綴，context 帶 `handler`（呼叫端函式名，`runtime.Callers`）、`caller`（呼叫端 file:line）與 `i18n`（key，供跨 locale grep）—— 欄位不叫 `key`，因 logger 的 sensitive pattern 會遮蔽 `key=` 形狀的值。
   - `inputMessage(err)`：驗證 sentinel（`ErrNoManifestFile`、路徑驗證失敗、Composer 找不到 Subject）。只 redact，不加前綴、不 log —— 使用者輸入問題不是系統錯誤。
   - `redactText(s)`：Message 以外的字串欄位（`MuscleRatioSubjectDTO.Error`、Composer `MissingFileDTO.ErrMessage`），只 redact、不 log。
   - `failed*Result(...)` 的引數只能是字串字面值、`failMessage(...)` 或 `inputMessage(...)`。`AnalyzePhaseSync` 的分析 / 寫檔分支改用新 `failedPhaseSyncResult`。AST 規則只檢查這一點；`redactText` 的兩個欄位沒有 AST 規則 —— `MuscleRatioSubjectDTO.Error` 由 runtime 測試（第 4 點）守，`MissingFileDTO.ErrMessage` 只靠呼叫點本身。
3. **i18n 規則：handler 層 localize，analyzer 只回 error / sentinel**。新增 19 個 `error.handler.*` key（4 locale），值只是前綴（無 verb、無冒號），多個 handler 的同義步驟共用一個 key；zh-TW 值與遷移前硬編碼前綴逐位元組相同。`internal/cci` / `muscle_ratio` / `phase_sync` 內部既有的 `i18n.T` 呼叫與硬編碼中文**這次不遷移**（後續 W4 / W5）；在那之前 Message = handler 的 localized 前綴 + analyzer 的（語言不一的）錯誤文字。刪除 orphan key `KeyErrorCCIOutputDirInvalid` / `KeyErrorCCIMkdirFailed`，以及 MR 改走 `failMessage` 後無 production caller 的 `KeyErrorMuscleRatioHandlerAnalysisFailed`。
4. **守門測試**：
   - AST：`TestFailedResultArgs_OnlyLiteralOrEnvelope`（第 2 點的引數規則）、`TestRedactImport_OnlyEnvelopeAndRecover`（gui 非測試檔中只有 `envelope.go` 與 `recover.go` 可 import `internal/security/redact`）。
   - runtime：`TestRPCErrChannel_NoAbsolutePath`（8 列 err 通道 handler）、`TestRPCMessage_NoAbsolutePath`（6 列：CCI、NPS、Composer×2、PhaseSync 分析分支、MR `Subjects[i].Error`），共用 `requireNoDirLeak`（system-root 前綴、植入的病患目錄段、反向保險 `<redacted-path>` 標記）；`TestFailMessage_LocalizedAndRedacted`（zh-TW / en-US / 缺 key 時回 bare key）。
   - `gui/main_test.go` 的 `TestMain` 比照 production 載入內建 catalog 並 `SetLocale(zh-TW)`。
5. **已知限制：sink-side redact 只脫敏「符合目錄段文法的非末段」**。兩條通道都只過 `redact.Paths`（與 log 的 `sanitizeMessage` 同一個 pattern）：
   - **末段保留**：目錄段換成 `<redacted-path>/`，**最後一段（檔名或資料夾名）保留**。錯誤若以病患資料夾名結尾（例如 DataFolder 本身不存在：`stat /Users/x/PatientAlice: no such file` → `stat <redacted-path>/PatientAlice: …`），那個名字仍會出現在 err 文字與 Message 裡。這正是 fsperm 保留 source-side `redactBasePaths`（對 base path 先補 `/`，連末段一併脫敏）的原因。
   - **目錄段文法**：段 = 以一或多個半形空白分隔的詞，不以空白開頭或結尾。
     - POSIX（`/…/`）：詞不含空白、`/`、`"`；`\` 是一般字元（段尾的 `\`、`%q` 的 `\\`、`Doe\nancy` 都算詞的一部分）；`'` 與 `:` 只能在詞中間（`O'Neil`；macOS Finder 名稱裡的 `/` 在 POSIX 層是 `:`，如 `2026:05:18`）。
     - drive-letter（`C:\…\`、`C:/…/`）/ UNC（`\\server\share\…\`）：詞不含空白、`\`、`/`、`:`、`"`（後兩者在 Windows 名稱不合法），`'` 可在任何位置；分隔字元是 `\`、`/` 或 `%q` 格式化後成對的 `\\`（UNC 開頭可為 `\\\\`）。
     - 換行是空白，所以 `Paths` 必須吃原始文字、且每段文字只吃一次：`Logger.sanitizeMessage` 先 `Paths` 再跳脫控制字元；text 格式的 `writeText` 組 `k=v` 時，value 已在 `sanitizeContextValue` 處理過（已跳脫），只再套跳脫與 keyword mask（`escapeAndMask`），`Paths` 只補在 key 上。對已跳脫文字再跑 `Paths` 的話，字面 `\n` / `\t` 會把多行 stack 黏成一個段、只剩最後一個 frame —— panic 的 Debug stack 正是走 context 欄位（`TestSanitizeMessage_MultiLineStackRedactedPerLine`、`TestWriteText_MultiLineContextValueRedactedPerLine`、gui `TestRecoverHandlerPanic_TextLogKeepsStackFrames`）。
     - 涵蓋 `Jane Doe`、`OneDrive - Hospital`、`EMG Data`、`O'Neil`、Windows 的 `'Jane'`、雙空白、`2026:05:18`、`resolved=%q` 形狀的 POSIX / Windows / UNC 路徑。歷史：`0c320ed` 時 drive-letter / UNC 段不接受空白，drive-letter / POSIX 段不接受 `'`（UNC 段則接受空白與 `\` 以外的任何字元），POSIX 段不接受雙空白與 `:`，`%q` 的 `\\` 分隔完全不匹配 —— 這些段原文留存。
   - **不符文法的目錄段原文留存**，其後的目錄段是否脫敏取決於分隔字元：
     - POSIX：段含 `"`、tab 等非半形空白、詞頭尾的 `'` 或 `:`（`'Jane'`、`Study: Phase 1`），或以空白開頭 / 結尾 —— 只有該段留存，POSIX 分支在下一個 `/` 重新起始。例：`/Users/x/Study: Phase 1/S01/emg.csv` → `<redacted-path>/Study: Phase 1<redacted-path>/emg.csv`。
     - drive-letter / UNC 以 `\` 分隔：段含 `"`、`:`、非半形空白，或以空白開頭 / 結尾 —— 單一 `\` 不會重新起始任何分支，**該段與其後到末段前的所有目錄段都留存**。例：`C:\Users\x\ Jane\S01\emg.csv` → `<redacted-path>/ Jane\S01\emg.csv`。`"`、`:`、控制字元在 Windows 名稱不合法，結尾空白會被 Windows 去掉，所以真實 Windows 路徑上只有「以空白開頭的段」會觸發。以 `/` 分隔時同 POSIX（只有該段留存）；`%q` 的 `\\` 可讓 UNC 分支在其後重新起始，但需其後至少兩個目錄段，否則同樣留存。
     - 放寬文法就會吃掉路徑後的錯誤文字 —— 允許 `: ` 時，`open …/c.csv: input/output error` 的 `c.csv: input/` 會被當成目錄段。`TestPaths_DocumentedSurvivingSegments` 釘住留存的形狀，`TestPaths_KeepsOrdinaryTextAroundPaths` 釘住不得過度脫敏的文字。
   - **刻意接受的過度脫敏**（安全方向，只損失可讀性；`TestPaths_DocumentedOverRedaction`）：http URL 的 host 與 path（`http://localhost:34115/index.html` → `http:/<redacted-path>/index.html`）；詞緊接 `/` 被當成目錄段（`copied C:\a\x.csv and/or C:\b\y.csv` → `copied <redacted-path>/or <redacted-path>/y.csv`）；單引號包住的兩條 UNC 路徑黏成一條（第二條開頭的 `\\` 被當成 `%q` 分隔字元）。log 改為先 `Paths` 後，`Paths` 的行首 fallback 也作用在多行訊息的每一行：以 `/`、`\\`、`C:\` 開頭的行（例如 `/help`）會被改寫成 `<redacted-path>/<最後一段>`。
   - 測試把植入目錄放在非末段、名稱符合文法，只斷言 `redact.Paths` 真正保證的部分。

### Amends ADR-0035

[[ADR-0035]] Decision 3 記錄「錯誤通道逐 handler 不變（…訊息文字逐位元組不變）」，實作（`ef1e617`）為此讓 MR 分析失敗保留外層 `redact.Paths`（舊 `UIError` 二次 redact 的位元組行為）。本 ADR 不改任何 handler 的錯誤**通道**，但刻意改變兩處**文字**：MR 分析失敗由「分析失敗:<err>」改為與其他 handler 一致的「分析失敗: <err>」（外層 `redact.Paths` 一併移除，`Paths` 冪等）；zh-TW 以外的 locale 下，CCI / NPS / Composer / PhaseSync 的失敗前綴改為該 locale 翻譯。[[ADR-0031]] Consequences 描述的 `failedCCIResult(fmt.Sprintf("...: %s", redact.RedactForMessage(err)))` 構造形狀同樣由 `failMessage` 取代，single-channel 契約不變。兩份 ADR 本身依 repo 慣例不修改。

## Why

- **逐分支 redact 是反覆出事的形狀**：redact 正則早已定案，反覆發生的是「某個分支忘了呼叫」—— `b1cadc0`、`2521146`、`62a0bf2` 都是同一類補丁，review 每輪又挖出新的漏網分支；實測仍有 10 個洩漏點。每條出站通道一個 sink-side redact 點，與 log 的 `sanitizeMessage` 同構。
- **err 通道已有強制必經點**：[[ADR-0035]] 讓 `recoverHandlerPanic` 成為每個回 error 的 bound method 的首句 defer，redact 搭上它即可，不需要新的 enforcement（[[ADR-0035]] Consequences 已預留此用途）。
- **Message 是 handler 組出來的值，defer 攔不到**：single-channel handler 回 `(failedResult, nil)`，`err` 為 nil、文字在 result struct 裡。只能讓建構點唯一，再用 AST 規則把「唯一」釘住。
- **log 一次、在同一處**：過去有的分支打 Error log、有的沒打（CCI 4 個下游分支、MR analyze、Composer render、NPS ctx 取消都沒打）；收進 `failMessage` 後「可預期失敗 = 一筆 Error log」成為不變式，驗證失敗（`inputMessage`）則刻意不 log。
- **i18n 落在 presentation 邊界**：原本只有 MR handler 走 `i18n.T`，其餘 handler 硬編碼 zh-TW，但 backend 透過 `SetLanguage` 支援 4 個 locale；analyzer 各自 localize 又不一致（cci / muscle_ratio 走 `i18n.T`、phase_sync 硬編碼中文 + sentinel）。選「handler localize、analyzer 回 error」：前綴在送出前的最後一站決定，analyzer 不必讀 process-global locale。

## Considered Options

### A. Webview envelope：err 通道出口 redact + Message 唯一建構（chosen）

見上。兩個出口、兩條 AST 規則、一張 runtime 表格。

### B. Wails `options.App.ErrorFormatter`（rejected）

Wails v2 可在 dispatcher 層統一格式化 bound method 回的 error。但 handler 測試直接呼叫 Go method，**繞過 dispatcher** —— redact 是否生效無法由現有測試驗證，測試看到的 err 與 production 送出的文字不同；它也只管 err 通道，碰不到 result 的 Message。

### C. 既然 sink 端會 redact，移除 fsperm 的 source-side redaction（rejected）

sink-side `redact.Paths` 保留末段（Decision 5）。fsperm 的 `ErrPathEscapesBase` 等訊息會列出 base path，而 base path 的末段正是病患目錄名；`redactBasePaths` 先補 `/` 讓末段一併脫敏（`ee323b6`），sink-side 無法替代。保留 source-side 作為「目錄名本身是 PHI」情境的唯一防線。

### D. 維持逐分支 redact，補齊每個 handler 的 NoAbsolutePath 測試（rejected）

即 status quo 加測試。下一個新分支仍要靠人記得；沒有結構性保證，正是 Why 第 1 點的反覆補丁模式。

### E. handler 回 error，由泛型 wrapper 轉成 failed result（rejected）

single-channel handler 的 result 型別各異，wrapper 需要每型別的 failed-result 建構子與 closure 形狀 —— 與 [[ADR-0035]] Option D 相同的理由；且 handler 仍需決定 i18n key 與「是否為驗證失敗」，wrapper 省不掉這些決定。

## Consequences

- **log 可見差異**：`failMessage` 的 Error log 訊息是 localized 前綴（隨 locale 變動，例如「分析失敗」），logger 自動記的 `(file:line)` 固定指向 `envelope.go`；指認失敗分支靠 context —— 例：`[ERROR] 分析失敗 (envelope.go:…) error=… context=[handler=gui.(*App).AnalyzeCCI caller=cci_handlers.go:… i18n=error.handler.analysis_failed]`。共用 key（如 `analysis_failed`）不區分 handler，併發 Wails 呼叫下也不能靠前一筆 entry log 對應，所以 `handler` / `caller` 是必要欄位，由 `TestHandlerLogs_ExpectedFailureShape` 釘住。
- **i18n 初始化失敗的曝露面變大**：`InitI18n` 失敗（例如外部翻譯 JSON 無效）時 `i18n.T` 回 key 本身，所有 handler 的失敗前綴會變成 `error.handler.*`（原本只有 MR 有此曝露）。刪除的 3 個 key 若仍出現在外部翻譯 JSON，`LoadTranslations` 會以 `ErrTranslationKeyUnknown` 拒絕整批載入；production 不產生該 JSON（只有 demo 會 `SaveTranslations`）。
- `redact.RedactForMessage` 在 gui 的唯一 caller 是 `envelope.go`。
- 刪除的測試：`TestCCIHandler_ErrorMessage_NoAbsolutePath`、`TestPhaseSyncHandler_ErrorMessage_NoAbsolutePath`（由 `TestRPCMessage_NoAbsolutePath` 涵蓋）；3 份 `leakyPrefixes` 合併為 `requireNoDirLeak`。
- GLOSSARY：新增「Webview envelope」條目。

## Related

- [[ADR-0035]]（**amended** —— 訊息文字兩處刻意改變；`recoverHandlerPanic` 必經點是本 ADR err 通道的前提）
- [[ADR-0031]]（Consequences 的 failedCCIResult 構造形狀由 `failMessage` 取代）
- [[ADR-0017]]（validated read-open；fsperm source-side redaction 所在的 seam）
