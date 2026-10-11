# 錯誤內插的路徑以 typed arg 帶入（FilePath / DirPath）

**Status**: accepted · **implemented** (2026-10-11) · 延伸 [[ADR-0048]]；縮短 [[ADR-0036]] Decision 5 的留存清單

[[Webview envelope]] 的兩條出口都只做 sink 端 `redact.Paths`（[[ADR-0036]]）。它把目錄段換成 `<redacted-path>/`，但**保留末段**，不符目錄段文法的段也原文留存（[[ADR-0036]] Decision 5）。錯誤若以資料夾結尾，資料夾名（常是病患名）照樣進 webview —— 典型例子是 DataFolder 本身不存在：`manifest.OpenDataFile` 回 `baseFolder 不存在或無法解析: <redacted-path>/PatientAlice`。相對路徑的首段 `Paths` 也遮不到（`Patient/../x.csv` → `Patient<redacted-path>/x.csv`）。於是每個錯誤建構點都得自己記得先處理路徑（`lenientShownName`、fsperm 的 `redactDir`），看建構點本身看不出它是否 PHI-safe。

## Gate

W12 是選做，條件是「會到 webview 的路徑內插點 ≥ 10 處」。以 4b84fc5 的程式碼重數：

- **方法**：以 AST 列出非測試 Go 檔裡所有 `fmt.Errorf`、`errors.New`、`i18n.NewError` / `WrapError` 的非字面引數（含多行呼叫），挑出路徑型的值（絕對 / 相對路徑、資料夾名、取自使用者或設定的檔名），再只留在 bound gui handler 呼叫路徑上的站點。只有 log、只在啟動時（`main.go` 的 `LoadConfig`、`InitLogger`、`InitI18n`，`Startup` 的 `EnsureDirectories`）、benchmark、scripts 的站點不算。每個建構點算一次；以 `%w` 包住已帶路徑的下層錯誤不算新的點。
- **結果：65 處**。fsperm 以外 31 處，其中 24 處 sink 端 `redact.Paths` 只遮到一部分或完全遮不到；fsperm 34 處已在 source 端處理（[[ADR-0036]] Option C）。≥ 10，執行本 ADR。完整清單見 W12 task report。

## Decision

1. **`internal/security/redact` 新增兩個型別**，渲染 policy 放在型別的 `String()`：
   - `FilePath`：只留檔名。`\` 正規化成 `/` 後用 `path.Base`（只認 `/`），結果與 OS 無關 —— Windows 產生的 manifest 檔名 `S01\emg.csv` 在各 OS 都是 `emg.csv`（同 `manifest.OpenDataFile`，[[ADR-0056]]）。不用 `filepath.Base`：它在 Windows 會先剝掉 volume（`C:emg.csv` → `emg.csv`），在其他 OS 不會。
   - `DirPath`：整條換成 `<redacted-path>/`，不含任何一段。字樣與 `Paths` 換掉目錄段的相同（抽成常數 `redactedDir`，`Paths` 也改用它）。
   - fmt 的 `%s` / `%v` / `%q`、`i18n.Error` 的 Args（`Error()` 的 zh-TW 與 `Localize` 的各 locale）、`fmt.Errorf` 的即時格式化都經 `String()`。i18n、logger、envelope 都不必改。logger 記的是 `err.Error()`，同一套 policy 自然生效，**logger 不改**。
   - 型別放 redact 而不是 i18n：redact 是 leaf package，fsperm、security、manifest、phase_sync、config 都能 import；i18n 會 import fsperm，fsperm 與 security 不能反過來 import i18n。
2. **先修已知外洩**：
   - `manifest.OpenDataFile` 的 `ErrBaseFolderNotFound: %s` → `DirPath`。cci、muscle_ratio、Composer 的「資料夾不存在」經此回報。
   - phase_sync `validateEMGFilePath` 的三個資料夾 Arg（不存在 / 狀態檢查失敗 / 非目錄）→ `DirPath`。
3. **再轉換**：
   - `PathValidator`：traversal、絕對化、symlink 解析失敗時的被拒路徑由 `shownPath(path, isDir)` 帶入 —— `ValidateExternalPath` 用 `FilePath`，`ValidateExternalDir` 用 `DirPath`。`validatePathFormat` 多一個 `isDir` 參數。`ErrSensitiveDirectory` 帶的是固定 pattern（`/etc/`），不是使用者路徑，不變。
   - `manifest.LoadManifests` 的「無法開啟檔案 %s」→ `FilePath`。這是原 `phase_manifest_parser` 的站點，[[ADR-0056]] 把開檔搬到這裡。
   - `config.SaveConfigAtomic` 的「無法建立 config parent 目錄 %s」→ `DirPath`。
   - 刪除 `lenientShownName`，`lenient_path.go` 的 7 個站點改用 `FilePath`（macOS / Linux 上渲染不變；Windows 見行為變更）。
4. **保留**：
   - fsperm 的 `redactBasePaths` / `redactDir`（[[ADR-0036]] Option C）。
   - 兩個出口的 sink 端 `redact.Paths`（[[ADR-0036]] Decision 1 / 2），作為 stdlib `*PathError` / `*LinkError`、panic 文字與未轉換站點的安全網。
   - **不加** policy 開關、第三種型別或泛用的「PII」抽象。
5. **測試**：
   - redact：`TestFilePath_RendersBaseNameOnly`（包括 `Paths` 會留段的形狀）、`TestDirPath_HidesEveryComponent`（sink 端再跑 `Paths` 不變）。
   - 端到端：gui `TestEnvelope_LocalizesAnalyzerErrors` 的兩列 DataFolderMissing 改斷言整條隱藏；`TestRPCMessage_NoAbsolutePath` 新增 `AnalyzeCCI_DataFolderMissing`，植入段放在末段。
   - 產生點：phase_sync 錯誤表（資料夾不存在、新增非目錄）；`TestOpenDataFile_BaseFolderNotFound`、`TestLoadManifests_FileNotFound`、`TestValidateExternal_ErrorTextHidesDirectories`、`TestSaveConfigAtomic_MkdirErrorHidesParentDir`。

### 延伸 / 修訂既有 ADR

- **[[ADR-0048]]（延伸）**：`i18n.Error` 的路徑型 Args 改以 `FilePath` / `DirPath` 傳入。`Error()` 仍固定 zh-TW；policy 與 locale 無關，所以 `Error()` 與 `Localize` 對路徑的渲染相同。Decision 3「先在地化、後脫敏」不變，只是路徑型 Args 在 `Paths` 之前就已不含目錄。
- **[[ADR-0036]] Decision 5（縮短留存清單）**。移除一項、縮窄一項：
  - **移除**「**末段保留**」的 DataFolder 例子：資料夾不存在、非目錄、無法解析，以及 config parent 目錄建立失敗、`PathValidator` 拒絕的目錄，產生點自己內插的資料夾都以 `DirPath` 帶入，那段文字不再出現資料夾名。
  - **縮窄**「**不符文法的目錄段原文留存**」：不再適用於 typed arg 本身的文字（`FilePath` / `DirPath` 不經 regex，`Study: Phase 1`、以空白開頭的 Windows 段、相對路徑首段都不會從 typed arg 出現），但仍適用於 stdlib / 第三方錯誤文字與未轉換的站點 —— 包括已轉換站點以 `%w` 帶著的 `*PathError` cause。例：`LoadManifests` 開檔失敗經 sink 端 `Paths` 後是 `無法開啟檔案 manifest.csv: open <redacted-path>/Study: Phase 1/manifest.csv: …`；相對路徑的 cause 完全不遮（`無法開啟檔案 manifest.csv: open Patient/manifest.csv: …`）。
  - 仍留在清單上的：stdlib / 第三方錯誤文字（`os.Stat`、`MkdirAll`、`open` 的 `*PathError`）—— 檔案只有在路徑是絕對路徑、且每個目錄段都符合文法時才只剩檔名（與 `FilePath` 一致）；資料夾留末段（例：`資料夾狀態檢查失敗 (<redacted-path>/): stat <redacted-path>/PatientAlice: permission denied`）。以及下方 Consequences 列出的未轉換站點。Decision 5 的文法說明與「刻意接受的過度脫敏」不變。Option C 不變。

兩份 ADR 依 repo 慣例不修改。

## 行為變更

zh-TW 與其他 locale 相同（policy 在 `String()`，與 locale 無關）。所列文字是送進 webview 的結果（已過 sink 端 `Paths`）。

- **PhaseSync / NPS 資料夾錯誤**（Message）：「資料夾不存在 (`<redacted-path>/missing`)」→「資料夾不存在 (`<redacted-path>/`)」；「資料夾路徑非目錄 (…)」與「資料夾狀態檢查失敗 (…)」的 Arg 同樣整條隱藏。狀態檢查失敗的 cause 是 `os.Stat` 的 `*PathError`，那段仍是 `stat <redacted-path>/<資料夾名>: …`。
- **CCI / MR / Composer 資料夾不存在**（Message；MR 在 `Subjects[i].Error`）：「baseFolder 不存在或無法解析: `<redacted-path>/<資料夾名>`」→「…: `<redacted-path>/`」。
- **分期總檔案開檔失敗**（所有 manifest handler 與 `LoadManifestSubjects` 的 Message）：「無法開啟檔案 `<redacted-path>/manifest.csv`: open …」→「無法開啟檔案 `manifest.csv`: open …」。前段的檔名不再帶目錄（相對路徑也一樣）；cause 那段（`open <path>: …`）不變，仍只靠 sink 端 `Paths` —— 相對路徑與不符文法的目錄段照樣出現（見上方 Decision 5 的縮窄）。
- **`PathValidator` 拒絕**（handler 入口驗證 → Message；`SaveConfig` 的設定目錄驗證、legacy panel 的 `ReadCSV` → err 通道；`LoadManifests` 的「路徑驗證失敗」；Output placement）：「路徑包含可疑的遍歷模式」「絕對路徑仍包含遍歷字符」「無法解析路徑」「無法解析路徑符號連結」後面的路徑，檔案只剩檔名（過去是 `<redacted-path>/x.csv`，相對路徑則首段原文留存），目錄是 `<redacted-path>/`。
- **`SaveConfig` 的 config parent 目錄建立失敗**（err 通道）：「無法建立 config parent 目錄 `<redacted-path>/count_mean`: mkdir …」→「…目錄 `<redacted-path>/`: mkdir …」。cause 那段不變。
- **lenient 檔名錯誤**：macOS / Linux 上文字不變（原本就只帶檔名）。Windows 上原 `lenientShownName` 用 `filepath.Base`，現在用 `path.Base`：volume 後直接接檔名（`C:emg.csv`）時保留 `C:`，只有 UNC 根（`\\server\share`）時是 `share` —— 與其他 OS 相同。
- **log**：上述錯誤的 `error=` 欄位同樣不含資料夾名（過去 logger 的 `Paths` 留末段）。handler 入口的參數 log（`params={… <redacted-path>/<資料夾名> …}`）不是錯誤文字，不變。
- golden（真實 V.16 資料走 GUI handler，預設 zh-TW）與 W11 逐位元組相同。

## Why

- **policy 跟著值走，不跟著每個站點走**。過去哪些路徑要先處理、用哪個 helper，由每個建構點自己決定，靠 review 守。半年內約 20 個 redact 補丁 commit 都是這一類。型別讓「這是資料夾」在建構點就寫明，渲染只有一處，讀建構點就知道它是否 PHI-safe。
- **sink 端無法分辨檔案與資料夾**。`Paths` 看到的是一段文字，末段可能是檔名（要留，供除錯）也可能是資料夾名（要藏）。只有建構點知道是哪一種。
- **放 `String()` 而不是放在各 renderer**：`Error()`、`Localize`、`fmt.Errorf`、logger 同時生效，不需要在 i18n 或 logger 加特判。`Error()` 仍是 deterministic zh-TW（[[ADR-0048]] 的前提不變）。

## Considered Options

### A. redact 的 `FilePath` / `DirPath`，policy 放 `String()`（chosen）

見 Decision。

### B. 型別放 i18n，由 `Localize` / logger 渲染（rejected）

fsperm、security 不能 import i18n（i18n import fsperm），這些站點用不到。而且 `Error()` 若照原值渲染，log 仍帶完整路徑，`fmt.Errorf` 的站點也用不上。

### C. 改 `Paths`，連末段一起遮（rejected）

`Paths` 分不出檔案與資料夾，全遮會讓每個檔案錯誤失去檔名（除錯唯一線索）。文法留存的段（[[ADR-0036]] Decision 5）也仍在，regex 補丁只會繼續增加。

### D. 維持各站點自己的 helper（status quo，rejected）

`lenientShownName`、`redactDir`、`*PathError` 去路徑各自存在，新站點要記得選對的 helper；決定不綁在值上，沒有結構性保證。

### E. 泛用 PII 型別或 policy 開關（rejected）

目前只有兩種渲染：檔案只留檔名、資料夾整條隱藏。開關或第三種型別沒有使用者。

### F. 另實作 `fmt.Formatter`（不需要）

`Stringer` 已涵蓋 `%s`、`%v`、`%q`、`%x`；catalog 裡沒有 `%#v`（`%#v` 不走 `String()`）。錯的動詞也印出原值：fmt 回報動詞錯誤時不呼叫方法，`%d` 會得到 `%!d(redact.DirPath=/Users/x/PatientAlice)`。目前不會發生 —— 帶 typed arg 的位置全是 `%s` / `%q`（三個資料夾 key 在四個 locale 都是 `%s`，`TestI18n_VerbConsistencyAcrossLocales` 讓各 locale 的動詞序列一致）。

## Consequences

- `redact` 多兩個 exported 型別與 `redactedDir` 常數；`Paths` 行為不變。`lenientShownName` 刪除；`validatePathFormat` 多一個 `isDir` 參數。
- **仍未轉換、會到 webview 的站點**（reopen 條件：任何一處被回報外洩，或新增同類站點時順手轉換；每處都是一行，以 `FilePath` / `DirPath` 包住引數）：
  - manifest 相對檔名，原文內插：phase_sync `mapOpenDataFileErr` 的「%s 檔案不存在 (%s)」、`parsers` 的 EMG（1）、Motion（1）與 ANC xlsx（10）。`Paths` 對 `S01/emg.csv` 完全不遮，更深的路徑留首段；只有裸檔名時等同 policy。
  - `io` `listEntries` 的「無法讀取目錄 %s」（MaxMean 批次，err 通道）：資料夾名留末段。
  - fsperm `EvalSymlinksWithFallback` 的「路徑根目錄無法解析: %s」：只帶根（磁碟代號或 UNC 的 server\share）。
  - stdlib `*PathError` 的 cause：交給 sink 端 `Paths`，資料夾留末段。
- 不到 webview、未轉換：`LoadConfig` / `EnsureDirectories` / `LoadTranslations` / log rotation 的路徑（只在啟動時、只進 log 或 stderr）。
- 測試變更：phase_sync `TestValidateEMGFilePath_NonExistentBaseFolder` 原本斷言「錯誤要帶 baseFolder 路徑」，與本決策相反，改為斷言不帶；gui `TestRPCMessage_NoAbsolutePath` 的 `AnalyzePhaseSync_Validate` 列錯誤只帶檔名，反向保險由「含 `<redacted-path>`」改為「含檔名」（`shownFile`）。沒有刪除測試。
- GLOSSARY：**Webview envelope** 的限制段改寫（typed arg 優先、`Paths` 是安全網）；`docs/api.md` 新增 `FilePath` / `DirPath`。

## Related

- [[ADR-0048]]（**extended** —— 路徑型 Args 改為 typed arg；`Error()` 仍固定 zh-TW）
- [[ADR-0036]]（Decision 5 的留存清單縮短；Option C 保留；Decision 1 / 2 的 sink 端 `Paths` 保留為安全網）
- [[ADR-0056]]（**amended** —— `LoadManifests` 開檔站點的來源；其 Decision 3 第 3 步與行為變更「分期總檔案」的「無法開啟檔案 <path>」改為只帶檔名）
- [[ADR-0058]]（err 通道出口先在地化後脫敏，順序不變）
