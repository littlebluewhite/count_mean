# Analyzer 錯誤帶 i18n key，handler 層在地化

**Status**: accepted · **implemented** (2026-10-10)

[[ADR-0036]] Decision 3 定下「handler 層 localize，analyzer 只回 error / sentinel」，但把三個 [[Domain analyzer]] 內部的文字留到後續 wave：`internal/cci`（15 處）與 `internal/muscle_ratio`（13 處）在 analyzer 內以 `i18n.T` 依 process-global locale 渲染錯誤，`internal/phase_sync` 則有 31 處硬編碼中文（`fmt.Errorf("資料夾不存在 (%s): %w", …)` 之類）。結果是 [[Webview envelope]] 的 Message = handler 的 localized 前綴 + analyzer 語言不一的錯誤文字：en-US 下 CCI / MR 是英文、PhaseSync / NPS 仍是中文；而 cci / MR 的 `err.Error()` 隨建構當下的 locale 變動，log 與逐位元組釘住的測試都得先把 locale 釘在 zh-TW。

## Decision

1. **`internal/i18n` 新增帶 key 的錯誤型別**（`internal/i18n/error.go`）：

   ```go
   type Error struct { Key string; Args []any; Cause error }
   func NewError(key string, args ...any) error              // 只有訊息
   func WrapError(cause error, key string, args ...any) error // 「訊息: cause」
   func (e *Error) Error() string  // 固定以內建 zh-TW catalog 渲染
   func (e *Error) Unwrap() error  // 回 Cause
   func Localize(err error) string // 依目前 locale 渲染
   ```

   - 訊息 = `Key` 的 catalog 文字以 `Args` 格式化（規則同 `T`：沒有 Args 不經 `Sprintf`），`Cause` 非 nil 再接「: 」+ cause 文字。
   - **`Error()` 一律 zh-TW**（內建 catalog，與 global locale、外部翻譯 JSON 無關）：`err.Error()`、log、既有逐位元組斷言與遷移前相同。
   - `Unwrap` 回 `Cause`：既有 sentinel（`ErrBaseFolderNotFound`、`ErrPhaseValueZero`…）以 `Cause` 掛上，`errors.Is` / `errors.As` 照常；`*phase_sync.AnalysisError`、`*composer.LoadError` 等 Stage 型別不變。
   - `Localize(err)`：以 `errors.As` 找鏈上第一個 `*Error`，把 `err.Error()` 裡它的 zh-TW 文字（最後一次出現）換成目前 locale 的渲染；其上的一般 wrapper（`fmt.Errorf("%w: …")`、`AnalysisError`）保留自己的文字。`Cause` 與 error 型別的 `Args` 遞迴 `Localize`。global 未初始化時以 zh-TW 渲染；鏈上沒有 `*Error` 就回 `err.Error()`。
2. **三個 analyzer 遷移**：
   - cci 15 處、muscle_ratio 13 處 `i18n.T`：`errors.New(i18n.T(key, args...))` → `i18n.NewError(key, args...)`，`fmt.Errorf("%s: %w", i18n.T(key), err)` → `i18n.WrapError(err, key)`；沿用既有 key。
   - phase_sync 31 處硬編碼中文中的 29 處遷移 → 27 個新 `error.phase_sync.*` key（4 個 locale；zh-TW 值與遷移前逐位元組相同）；其餘 2 處是 `validateManifestData` 的 `models.PhaseSyncValidationError` 字面值，不在範圍（見 Consequences）。多段前綴（「計算分期時間範圍失敗: 計算同步時間範圍失敗: 開始時間 (…) 大於結束時間 (…): <sentinel>」）以巢狀 `WrapError` 組成，catalog 值只放單段訊息，不含「失敗: 」（`TestI18n_ColonStyle_Consistent`）。`ErrNegativePhaseTime` 改為 package-level `*i18n.Error` sentinel（`errors.Is` 以指標比對）；其餘 sentinel 仍是 `errors.New` 英文文字，作為 `Cause`。
   - **`muscle_ratio.SubjectResult.Error string` 改為 `Err error`**：逐 subject 錯誤（含 Output 2 的分期點越界 warning，`collectPhasePoints` 改回 `error`）不再在 analyzer 內渲染成字串，交給 handler 決定語言。
3. **Webview envelope 以 `Localize` 渲染 analyzer 錯誤**（`gui/envelope.go`）：
   - `failMessage(key, err)` = `i18n.T(key) + ": " + redact.Paths(i18n.Localize(err))`；`inputMessage(err)` = `redact.Paths(i18n.Localize(err))`。
   - `redactText` 由吃字串改為吃 error：`redactText(err)` = `redact.Paths(i18n.Localize(err))`，nil 回 `""`。`MuscleRatioSubjectDTO.Error` = `redactText(sr.Err)`、Composer `MissingFileDTO.ErrMessage` = `redactText(m.Err)`；wire DTO 欄位與型別不變。
   - **先在地化、後脫敏**：路徑型 Args（如「資料夾不存在 (%s)」的資料夾）在每個 locale 都經 `redact.Paths`。
   - `redact.RedactForMessage` 失去最後一個 caller，連同它的 3 個測試刪除。
4. **守門測試**：
   - `internal/i18n/error_test.go`：`Error()` 不隨 locale 變、`Unwrap`、`Localize`（en-US、zh-TW、global 未初始化）。
   - 每個 analyzer 一份 `errors_i18n_test.go`（`TestCCIErrors_…`、`TestMuscleRatioErrors_…`、`TestPhaseSyncErrors_ZhTWTextLocalizedAtHandler`）：en-US 下 `err.Error()` 仍是遷移前的 zh-TW 全文、`Localize` 為英文、sentinel 仍以 `errors.Is` 命中；涵蓋路徑 Arg、巢狀前綴、一般 wrapper 之下的 `*i18n.Error`、`AnalysisError` 透明。
   - gui：`TestFailMessage_LocalizedAndRedacted` 加 analyzer 錯誤列（zh-TW / en-US）；`TestInputMessageAndRedactText_LocalizedAndRedacted`；`TestEnvelope_LocalizesAnalyzerErrors` 從 handler 端到端驗 `AnalyzeCCI`、`AnalyzeMuscleRatio`（`Subjects[i].Error`）、`AnalyzePhaseSync`、`AnalyzeNormalizedPhaseSync` 在 zh-TW / en-US 下的 Message（含路徑 redact）。

### Amends ADR-0036

- Decision 2：`redactText(s)` 改為 `redactText(err)`，三個 helper 都經 `i18n.Localize`。
- Decision 3：「`internal/cci` / `muscle_ratio` / `phase_sync` 內部既有的 `i18n.T` 呼叫與硬編碼中文這次不遷移」由本 ADR 完成；規則精確化為「analyzer 回**帶 i18n key 的** error / sentinel，不讀 locale；handler 層（envelope）在地化」。
- Consequences 的「`redact.RedactForMessage` 在 gui 的唯一 caller 是 `envelope.go`」不再成立 —— 該函式已刪除。

[[ADR-0036]] 本身依 repo 慣例不修改。

## Why

- **plan 原本的「純 sentinel + handler 對照 key」不夠用 —— 錯誤帶參數**。phase_sync 31 處裡大多數帶執行期參數：主題索引與總數、分期點名稱與 index / 時間、資料範圍上限、EMGMotionOffset、資料夾路徑、檔案類別與檔名。sentinel 只有固定文字，handler 以 `errors.Is` 對應 key 時這些值已經遺失；要保留就得每種訊息一個帶欄位的 struct 型別，再在 handler 寫一張型別 → key 的 switch。
- **sentinel 與訊息不是一對一**：`ErrPhasePointOutOfRange` 由 4 種訊息共用（D / O index、EMGMotionOffset、Force Plate 時間），`ErrBaseFolderNotFound` 由 2 種共用，`ErrPhaseValueZero` 分開始 / 結束兩種。sentinel → key 的對照表無法分辨。
- **前綴是巢狀的**：「計算分期時間範圍失敗: 計算同步時間範圍失敗: 開始時間 …」三層各自是一段可翻譯訊息。handler 對照表只能翻最外層，或得知道每條 chain 的結構。key 跟著每一層錯誤走，`Localize` 沿 chain 遞迴即可。
- **在 analyzer 內 `i18n.T` 也不對**（cci / MR 原本的做法）：analyzer 讀 process-global locale，違反 [[ADR-0036]] 的 presentation 邊界；`err.Error()` 隨建構當下的 locale 變，log 與測試不穩定；建構與顯示之間切換語言（`SetLanguage`）時，顯示的仍是舊語言。
- **`Error()` 固定 zh-TW 而非目前 locale**：log 跨 locale 一致、可 grep；既有逐位元組斷言（phase_sync / gui / `test/`）不必改；`Error()` 不依賴 global 是否已初始化。語言只在送進 webview 的最後一站決定，與 [[ADR-0036]] 前綴的規則同構。

## Considered Options

### A. 帶 key 的 `i18n.Error` + envelope 以 `Localize` 渲染（chosen）

見上。一個型別、兩個建構子、一個渲染函式；analyzer 改寫是機械式的一對一替換，sentinel / Stage 型別不動。

### B. 純 sentinel，handler 依 `errors.Is` 對照 key（plan 原案，rejected）

見 Why 前三點：參數遺失、sentinel 與訊息多對一、巢狀前綴只能翻最外層。

### C. 每種訊息一個帶欄位的錯誤型別，各自實作在地化（rejected）

約 40 種訊息就是約 40 個型別加各自的格式化方法，與 A 等價但沒有共用機制；新增訊息要同時加型別、key 與 handler 對照。

### D. 維持 analyzer 內 `i18n.T`，phase_sync 也改用 `T`（rejected）

改動最小，但保留 Why 第四點的所有問題，且 phase_sync 的 zh-TW 位元組只在 locale 剛好是 zh-TW 時成立。

### E. `Error()` 依目前 locale 渲染（rejected）

可以順帶在地化 err 通道，但 `err.Error()` 會隨 locale 變 —— log 不一致、所有逐位元組斷言都要釘 locale，`Error()` 也得在 global 未初始化時另有退路。err 通道需要時可在 `recoverHandlerPanic` 出口改用 `Localize`，不必讓 `Error()` 變動。

## Consequences

- **zh-TW 下使用者可見文字逐位元組不變**；en-US / ja-JP / zh-CN 下，PhaseSync / NPS 的 analyzer 錯誤改為該 locale 翻譯，CCI / MR 不變（過去建構時就依 locale 渲染）。
- **非 zh-TW locale 下 log 裡的 cci / MR 錯誤文字改為 zh-TW**（過去隨 locale）；`failMessage` 的 log 訊息（前綴）仍是 localized，`error=` 欄位固定 zh-TW。
- **err 通道不在地化**：`recoverHandlerPanic` 仍送 `redact.Paths(err.Error())`，即 zh-TW。目前經 err 通道送出 analyzer 錯誤的只有 `LoadPhaseManifest`（phase_sync `LoadManifestSubjects`），它過去就是硬編碼中文，文字不變。
- **仍是硬編碼中文、任何 locale 都顯示中文**（不在本 ADR 範圍）：cci `validateEMGBounds` 的 4 個 NaN/Inf 守門與 chart pipeline 的 sentinel（`ErrInvalidGaitCycle`、`ErrPairLengthMismatch`、`ErrNilResult`）；`internal/composer` 的「Motion 路徑解析失敗」「解析 Motion 失敗」「muscle_ratio 路徑解析失敗」「讀取 muscle_ratio CSV 失敗」、`ErrSubjectNotFound`、`ErrMotionFileEmpty`，以及 `internal/io` 的兩個 MR sentinel（`ErrMuscleRatioCSVEmpty`、`ErrMuscleRatioCSVNoHeader`）—— 這些會進 Composer 的 Message 通道，en-US 下顯示中英混合（follow-up）；muscle_ratio `Analyze` 的 nil 參數守門（programmer error）；phase_sync `validateManifestData` 的兩個 `models.PhaseSyncValidationError` Message（與 `parsers.ValidatePhaseManifest` 同型別，該型別與 parsers 不在範圍）；`internal/config` 等其他套件。英文 sentinel 文字（如 `base folder not found`）在任何 locale 都維持英文，同遷移前。
- **`Localize` 的限制**：只在地化 `errors.As` 找到的第一個 `*Error`（含其 Cause 與 error Args）—— `errors.Join` 的其他分支、或 wrapper 文字重複嵌入同一段 zh-TW 時的較前一次出現，維持 zh-TW。目前沒有這種形狀的 producer。外部翻譯 JSON 若覆寫 zh-TW 值，`Error()` 仍用內建文字，`Localize` 在 zh-TW 下則用覆寫後的文字。
- 刪除的測試：i18n `TestI18n_CallerWrapPatternPreservesErrorsIs`（它守的 `fmt.Errorf("%s: %w", T(key), err)` 寫法已無 production 使用者，由 `TestError_UnwrapReachesCause` 取代）；cci 4 個「建構時 locale」測試（由 `TestCCIErrors_ZhTWTextLocalizedAtHandler` 取代）；redact 的 3 個 `TestRedactForMessage_*`。
- GLOSSARY：更新「Webview envelope」條目（`redactText(err)`、三個 helper 經 `i18n.Localize`、analyzer 回帶 key 的錯誤）。

## Related

- [[ADR-0036]]（**amended** —— `redactText` 簽章；analyzer i18n 遷移完成；`RedactForMessage` 刪除）
- [[ADR-0047]]（phase_sync 兩個入口共用的錯誤路徑；`AnalysisError` 的 Stage 不變）
- [[ADR-0046]]（`composer.LoadError` 的 Stage 不變；`MissingFileDTO.ErrMessage` 經 `redactText(err)`）
