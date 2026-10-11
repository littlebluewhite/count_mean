# muscle_ratio compute core:`computeSubject` 與 `analyzeSubject` I/O adapter 分離

**Status**: accepted · **implemented** (2026-10-11)

`analyzeSubject`(`internal/muscle_ratio/analyzer.go`)原本把讀 EMG、算比值、寫 Output 1、組 Output 2 的列、寫 Output 2、sticky-success 交錯在一起:計算夾在兩次寫檔之間,沒有不碰檔案系統就能呼叫的入口。驗證計算是否正確的 14 支測試(Output 2 列組成 9 支、缺通道、分期點邊界 3 支、負時間)因此都要在 disk 上建 EMG CSV 與 manifest、跑完整個 `Analyze`、再把 CSV 讀回來斷言。

另外,`Analyze` 會驗證並建立 `Params.OutputDir`,但檔案其實是 `Params.CSVHandler` 寫的,落點由它的 `config.OutputDir` 決定;`placeSubjectOutput` 每次寫檔也已經做路徑驗證與 `MkdirAll`([[ADR-0040]])。同一個目錄有兩個欄位描述,兩邊各驗一次、各建一次,而 GUI 傳的兩個值永遠來自同一個 `s.config`。

## Decision

1. **新增 compute core**(unexported):

   ```go
   func computeSubject(emg *models.PhaseSyncEMGData, m *models.PhaseManifest) (subjectCompute, warn error, err error)
   ```

   依序做:EMG 不得為空 → `musclemap.RightSideChannels` → `ComputeAllRatios` → `collectPhasePoints` → `buildPhasePoints`,不碰檔案系統。`subjectCompute{times, ratios, points}` 是 Output 1 / Output 2 要寫的值,全部已經算好。
   - `err` 非 nil:連 Output 1 都算不出來(EMG 為空、缺通道),`subjectCompute` 為零值。
   - `warn` 非 nil:Output 1 的值已算好,Output 2 要跳過(分期點不足或落在 EMG 範圍外),`points` 為 nil。
   - 兩者皆 nil:兩個輸出的值都已算好。

   用 free function,不掛 receiver:core 不需要 logger 或其他 dependency。`collectPhasePoints` 的 receiver 本來就沒用到,一併改成 free function。
2. **`analyzeSubject` 只剩 I/O adapter**:檢查 blank Subject → `manifest.LoadEMG` → `computeSubject` → 寫 Output 1 → 沒有 warn 才寫 Output 2 → sticky-success([[ADR-0004]] Boundary 1)。錯誤優先序不變:core err > Output 1 寫檔錯誤 > warn > Output 2 寫檔錯誤。Output 2 的欄名 `(11pt avg)` 屬於寫檔端的 layout,留在 adapter 組。
3. **`Analyze` 開頭的檢查順序不變**:nil ctx → nil params → nil `CSVHandler` → 讀 manifest。gui 的測試 fixture 沒有 `csvHandler`,依賴 nil `CSVHandler` 在讀 manifest 之前就被擋下。
4. **刪 `Params.OutputDir`**,連同 `Analyze` 內的 `ValidateExternalDir`、`os.MkdirAll`,以及兩個 i18n key `error.muscle_ratio.output_dir_invalid`、`error.muscle_ratio.mkdir_failed`(4 個 locale)。輸出目錄就是 `CSVHandler` 的 `config.OutputDir`。GUI 原本的 `Params.OutputDir` 與 `csvHandler` 的 config 都是 `s.config`,輸出位置不變(golden 與 W7 逐位元組相同)。
5. **測試**:
   - 14 支改打 `computeSubject`(`computeSubject_test.go`),用 in-memory fixture `buildInMemoryMRFixture`(仿 cci 的 `buildInMemoryCCIFixture`),對應的 `Analyze` 版刪除。
   - `TestAnalyze_NaNCellWrittenAsEmpty`、`TestAnalyze_EMGUpstreamNaN_OutputCellEmpty` 維持端到端;core 另加 `TestComputeSubject_ZeroDenominatorYieldsNaN`。
   - 其餘測試留在 `Analyze`。新增兩支 adapter 測試:`TestAnalyze_SubjectOutcomes_RoutedPerSubject`(core err / warn / 成功三種結果在同一批的分流)與 `TestAnalyze_OutputDirUnwritable_PerSubjectOutput1Failure`(下方的行為變更與錯誤優先序)。原本由 Output 2 列測試順帶斷言的 `(11pt avg)` 標題列,改由 `TestAnalyze_TwoSubjects_BothExported` 斷言。

## 行為變更

| 情境 | 之前 | 之後 |
| --- | --- | --- |
| 輸出目錄不可寫(例如該路徑是一般檔案) | `Analyze` 回一筆整批錯誤「建立輸出目錄失敗」,沒有任何 `SubjectResult` | 每個 subject 各一筆 `Success=false`、「寫入 Output 1 失敗:輸出目錄建立失敗: …」;`Analyze` 的 err 為 nil |
| 輸出目錄落在系統敏感目錄 | 整批錯誤「OutputDir 驗證失敗」 | 每個 subject 各一筆「寫入 Output 1 失敗:輸出路徑無效: …」(`placeSubjectOutput` 的 `ValidateExternalPath` 擋下)。GUI 的 config 載入時已驗證 OutputDir,實務上到不了這裡 |

GUI 端的差別:原本是一個「分析失敗」對話框;現在是逐 subject 的錯誤列,加上「部分主題未完成」。

## Why

- **可測試性**(同 [[ADR-0024]]):Output 2 列組成、中點去重、分期點邊界這些容易回歸的邏輯,現在用記憶體內的輸入就能直接斷言,不必走 disk、也不必把 CSV 讀回來解析。
- **輸出目錄只有一個 owner**:真正決定檔案落點的是 `CSVHandler` 的 config,`Params.OutputDir` 只是同一個值的第二份副本,再加一層與 `placeSubjectOutput` 重複的驗證與建目錄。
- **錯誤走 batch 原本的通道**([[ADR-0012]] batch + partial-success):寫檔失敗本來就回報成該 subject 的 Output 1 失敗;輸出目錄不可寫只是「每個 subject 都寫不了」的特例,不需要另開一條整批錯誤通道。代價是 N 個 subject 會看到 N 筆相同的錯誤,而不是一筆。

## 與既有 ADR 的關係

- [[ADR-0012]]:`muscle_ratio.Analyze` 仍是 batch + compute+write,簽章 `(ctx, *Params) ([]SubjectResult, error)` 不變,只少了一個欄位。切割發生在套件內部,不是跨 analyzer 共用 kernel。
- [[ADR-0014]]:11 點 window mean(`buildPhasePoints` → `windowmean.WindowMean`)與 `(11pt avg)` 欄名不變,只是改由 core 呼叫。
- [[ADR-0024]]:同樣的 compute core / I/O adapter 切法與 in-memory fixture 測試。差別在 `computeCCI` 是 method,`computeSubject` 沒有 dependency,所以用 free function。
- [[ADR-0042]]:分期點換算成 EMG 秒數仍由 [[Phase timeline]] 負責,`collectPhasePoints` 只保留 in-range 檢查、排序、中點這些 policy。

## Considered Options

- **A. free function `computeSubject` + 刪 `Params.OutputDir`(採用)。**
- **B. 完全照 `computeCCI` 寫成 `*Analyzer` 的 method。** 拒:沒有 receiver 狀態要用;free function 一看就知道不帶 I/O。
- **C. 只抽 core,保留 `Params.OutputDir`。** 拒:同一目錄兩個欄位的重複仍在,GUI 永遠傳同一個值。
- **D. 保留整批的建目錄 / 驗證,但改從 `CSVHandler` 取目錄。** 拒:`CSVHandler` 得對外暴露 OutputDir 或新增 `EnsureOutputDir`,而 `placeSubjectOutput` 已逐檔做同樣的事;換來的只是錯誤「一筆或 N 筆」的差別。
- **E. core 直接產出 `io` payload(含 Subject 與欄名)。** 拒:欄名是寫檔端的 layout([[ADR-0004]]),Subject 是檔名來源;core 只回算好的值。

## Consequences

- `muscle_ratio.Params` 只剩 `ManifestFile`、`DataFolder`、`CSVHandler`。
- 原本 file-based 的負時間測試會順帶讓 EMG parser 讀一次負時間 CSV;core 版用記憶體內的 EMG,不再經過 parser。
- GLOSSARY 不動:compute core / I/O adapter 是架構詞彙,不是領域術語(同 [[ADR-0024]])。

## Related

- [[ADR-0004]](Boundary 1:sticky-success 留在 Analyzer)
- [[ADR-0012]]、[[ADR-0014]]、[[ADR-0024]]、[[ADR-0042]](見上)
- [[ADR-0040]](`placeSubjectOutput` 單一 placement 步驟:containment → `ValidateExternalPath` → `MkdirAll` → atomic write)
- [[ADR-0048]](刪除的兩個 key 屬於 muscle_ratio 帶 i18n key 的錯誤)
