# 檔名單一裁判：`filename.ValidateName`；security 只管路徑位置

**Status**: accepted · **implemented** (2026-10-11) · 部分重開 [[ADR-0038]] §3.2.1 · amends [[ADR-0015]]（reserved-name 範圍那一列）

檔名規則原本有三個裁判，規則各不相同：

- `filename.Validator.ValidateFilename`：保留名逐個點分段比對、非法字元、控制 / 格式字元、磁碟代號、長度 255；無副檔名放行。
- `security.checkFilename`（`ValidateExternalPath` 的檔案分支經 `performBasicSecurityChecks` 呼叫）：保留名只剝**最後一個**副檔名再比對、長度 255。`CON.txt.csv` → `CON.txt` 不命中 → 放行。
- `security.IsCSVFile`：先 `TrimRight(" .")` 再取副檔名，接受 `data.csv.`。

分期總檔只經過第二個裁判：GUI 邊界 `validateManifestHandlerParams` → `ValidateExternalPath`，`manifest.LoadManifests` 本身不檢查檔名。已驗證 `…/CON.txt.csv` 一路通過，Windows 上開檔會導向 console device。

另外 `filename.Sanitize` 只看第一段（stem），[[ADR-0015]] 把「first stem vs 每段」列為刻意保留的分歧。結果 `SubjectOutputName("a.NUL.b", "x") + ".csv"` = `a.NUL.b_x.csv`，產生的檔名會被 validator 拒絕。

## Decision

1. **`filename.ValidateName(name string) error`**：不綁副檔名的檔名規則，是唯一的裁判。擋空白、tab 與控制字元、Unicode Cf / Cs、路徑分隔符、Windows 磁碟代號前綴、`<>:"|?*`、**每個點分段**的 Windows 保留名、長度 > 255。
2. **`ValidateFilename` = `ValidateName` + `.csv`**：副檔名在 TrimSpace 之後取、不分大小寫。尾端空白照舊放行，尾點照舊拒絕；**無副檔名改為拒絕**（原本由 `IsCSVFile` 在 ReadCSV / WriteCSV 另外擋）。
3. **`Sanitize` 的保留名改逐段**，與 `ValidateName` 同一條規則：先去頭尾空白（對應 `ValidateName` 的 TrimSpace）再以 `.` 切段，命中段後補 `_safe`，頭尾空白原樣保留。`a.NUL.b` → `a.NUL_safe.b`。property 測試 `TestSubjectOutputName_AlwaysValidName` 釘住 `ValidateName(SubjectOutputName(x, s) + ext) == nil`（任意 Unicode、對抗性 token 組合、截斷邊界；suffix 與 ext 取實際 caller 用的值）。
4. **security 刪檔名規則**：`checkFilename`、`windowsReservedDeviceNames`、`isWindowsReservedFilename`、`ErrFilenameTooLong`、`IsCSVFile`。`performBasicSecurityChecks` 去掉 `checkFilename` 後只剩 `checkSensitiveLocation(p, false)`，一併刪除，呼叫端直接呼叫 `checkSensitiveLocation(p, isDir)`。`maxFilenameLength` 保留（`lenient_path.go` 仍用）。security 仍不 import validation（[[ADR-0015]]）。
5. **io**：刪 `CSVHandler.isCSVFile` 與 `errInvalidCSVFile`。ReadCSV / WriteCSV 的 `.csv` 檢查由兩者已有的 `ValidateFilename(filepath.Base(path))` 承擔。
6. **manifest**：`LoadManifests` 開檔前呼叫 `filename.ValidateName(filepath.Base(path))`。一處涵蓋 6 個 caller：cci、muscle_ratio、phase_sync（`validateManifestFile`、`LoadManifestSubjects`）、composer、gui `LoadChartComposerSubjects`。
7. **不能有檔案路徑失去名字檢查**。今天經 `ValidateExternalPath` → `checkFilename` 拿到保留名 / 長度檢查的檔案路徑，逐一改由 filename 套件把關：

| Caller | 檔案 / 目錄 | 原本的檔名檢查 | 現在的檔名檢查 |
|---|---|---|---|
| gui `validateManifestHandlerParams`（manifestFile；AnalyzePhaseSync / AnalyzeMuscleRatio / AnalyzeCCI / AnalyzeNormalizedPhaseSync / LoadChartComposerSubjects / GenerateChartComposer） | 檔案 | `checkFilename` | 下游 `LoadManifests` → `ValidateName(Base)` |
| gui `LoadPhaseManifest`（`validateExternalPathInputs`） | 檔案 | `checkFilename` | 下游 `LoadManifestSubjects` → `LoadManifests` |
| gui `downloadValidatedPNG`（DownloadCCIChart / DownloadChartComposerImage） | 檔案 | `checkFilename` | 檔名 = `SubjectOutputName(...) + ".png"`，property 測試保證合法，不另加呼叫 |
| io `ReadCSV` | 檔案 | `ValidateFilename(Base)` + `checkFilename` + `IsCSVFile` | `ValidateFilename(Base)`（含 `.csv`） |
| io `placeSubjectOutput` | 檔案 | `checkFilename` | 檔名 = `SubjectOutputName(...) + ".csv"`，property 測試保證合法，不另加呼叫 |
| io `WriteCSV`（`ValidateFilePath`，allow-list 非空，原本就不經 `checkFilename`） | 檔案 | `ValidateFilename(Base)` + `IsCSVFile` | `ValidateFilename(Base)` |
| `ValidateFilePath` 無 allow-list 分支 | — | `checkFilename` | 無 production caller（CSVHandler 永遠帶 3 個 allow-list；`DefaultValidator` 只用於 External 兩操作） |
| config 5 個目錄、gui dataFolder（`ValidateExternalDir`） | 目錄 | 無 | 無（不需要） |

## 行為變更

- **分期總檔名**：`CON.txt.csv`、`a.NUL.csv`、`lpt1.bak.csv` 等改為拒絕（bug fix）。`CON.csv` 原本在 GUI 邊界被擋（「分期總檔案 路徑驗證失敗: …為 Windows 保留裝置名稱」），現在改在 `LoadManifests` 被擋，訊息為各 caller 的「解析分期總檔案失敗」類前綴 + 「檔案名稱不能使用保留字: CON」。分期總檔名現在也套用 `ValidateName` 的其餘規則（`<>:"|?*`、控制 / 格式字元、磁碟代號），這些在 Windows 上本來就是非法檔名；真實的 V.16 分期總檔名通過（golden 比對逐位元相同）。
- **Subject 輸出檔名**：只有 subject 含「保留名作為點分段」（例 `a.NUL.b`）或「頭尾空白後緊接保留名」時改變（補 `_safe`）。真實資料的 subject（`SF2` 等）不受影響，golden 比對逐位元相同。muscle_ratio 的 subject 碰撞偵測也用 `Sanitize`，判定隨之一致。
- **ReadCSV / WriteCSV 無副檔名**：原本 ReadCSV 回 `ErrCodeFileFormat`「檔案格式無效」、WriteCSV 回 `errInvalidCSVFile`；現在兩者都回「檔案名稱驗證失敗: …檔案名稱缺少副檔名」。尾點、尾端空白、`.txt` 的結果不變（原本就由 `ValidateFilename` 先處理）。
- **`ValidateExternalPath` / `ValidateFilePath`（無 allow-list）不再拒絕保留名與 > 255 的檔名**：檔案 caller 自己走 filename 套件（見上表）。

## Why

- **兩個裁判必然漂移**：較弱的那份（security 只剝最後一個副檔名）剛好是分期總檔唯一的裁判，`CON.txt.csv` 就從這個縫溜過。[[ADR-0015]] 已立「保留名集合單一真相」，[[ADR-0041]] 已立「filename 只擋檔案系統非法、讀門與寫門共用同一個 `ValidateFilename`」；本 ADR 把規則本身（不只集合）也收成一份。
- **名字檢查只能放在知道自己拿的是檔名的 caller**：security 不 import validation（[[ADR-0015]]），所以不能由 security 轉呼叫 filename。security 只回答「這個位置安全嗎」，`LoadManifests` 與 `CSVHandler` 回答「這個名字合法嗎」。放在 `LoadManifests` 而非 GUI 邊界，是因為非 GUI 的讀者（composer、analyzer）也經過它。
- **Sanitize 與 validator 同規則，產生的檔名才能證明不用再檢查**：[[ADR-0019]] 讓 `SubjectOutputName` 成為 Subject 進檔名的唯一入口；Sanitize 改逐段後，property 測試證明它的產物必過 `ValidateName`，`placeSubjectOutput` 與兩個 PNG 下載不必各自加檢查。這也取消了 [[ADR-0015]] 表中「reserved-name 範圍：first stem vs 每個 dot-segment」這一列分歧；其餘分歧（replace / reject、200 bytes / 255、untitled / error）不變。
- **副檔名只留一條規則**：`IsCSVFile` 的 `TrimRight(" .")` 與 `ValidateFilename` 的 TrimSpace 語意不同，而 ReadCSV / WriteCSV 都先跑 `ValidateFilename`，`IsCSVFile` 只多擋了「無副檔名」這一種；把它收進 `ValidateFilename` 後即可刪除。

## Considered Options

1. **修 `security.isWindowsReservedFilename` 改逐段，保留兩個裁判**：拒。保留名集合與長度規則仍有兩份，正是這次漏洞的成因。
2. **security 轉呼叫 `filename.ValidateName`**：拒。違反 [[ADR-0015]] 的 import 方向。
3. **把 `ValidateName` 放進 GUI 的 `validateExternalPathInputs`**：拒。只護住 GUI 入口；`LoadManifests` 的 6 個 caller 才是所有讀分期總檔的路徑。PNG 的檔名是後端產生的，不需要在邊界檢查。
4. **`placeSubjectOutput` 與 PNG 下載顯式加 `ValidateName`**：拒。property 測試證明它不會失敗，加了只是死的防禦層。
5. **保留 `IsCSVFile` 當獨立的副檔名規則**：拒。第三個裁判、trim 語意不同，且呼叫端已先跑 `ValidateFilename`。

## Reopens / Amends

- **[[ADR-0038]] §3.2.1**：原文「檔名專屬規則(`checkFilename`：檔名長度、Windows reserved device name)只對檔案跑」不再成立。`ValidateExternalPath` 與 `ValidateExternalDir` 都只跑 `checkSensitiveLocation`，差別只剩 `isDir` 的結尾分隔符語意。ADR-0038 其餘決定（三個操作、containment 單一來源）不變。依 repo 慣例不改 ADR-0038 本文。
- **[[ADR-0015]]**：保留名比對範圍的分歧取消（見 Why）。
- **[[ADR-0039]]** item 2 的 WriteCSV 守門少了 `IsCSVFile`，`.csv` 檢查併入 `ValidateFilename`。

## Related

- [[ADR-0015]]（保留名集合單一真相、security 不 import validation）
- [[ADR-0019]]（`SubjectOutputName` 是 Subject 進檔名的唯一入口；本 ADR 改了它內部的 `Sanitize`）
- [[ADR-0038]]（PathValidator 三個操作；§3.2.1 部分重開）
- [[ADR-0041]]（filename 只擋檔案系統非法、讀寫共用同一個 `ValidateFilename`）
