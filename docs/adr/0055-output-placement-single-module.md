# Output placement 單一 module：CSV 與 PNG 共用一道落檔門

**Status**: accepted · **implemented** (2026-10-11) · supersedes [[ADR-0016]] Decision 2 與 Why 的「BOM 忠實度」段 · amends [[ADR-0039]] §3.4、[[ADR-0040]]、[[ADR-0038]] §3.2 · [[ADR-0009]] 的寫入半段移到 io

W9 之前，寫進 OutputDir 的輸出有三條路，守門各不相同：

| 輸出 | 落檔路徑 | atomic | 敏感位置 | 建目錄 | BOM | leaf symlink |
|---|---|---|---|---|---|---|
| Subject-based CSV（7 種） | `placeSubjectOutput` → `WriteCSVAtomic` | 是 | `ValidateExternalPath` | `MkdirAll` | 無條件寫 | rename 取代連結 |
| File-based CSV（MaxMean / Normalize / PhaseAnalysis） | `writeFileOutput` → `WriteCSV` → `OpenWriteValidated` | 否 | 只有 `ValidateFilePath` 的 allow-list | `MkdirAll`（在驗證之前） | 照 `config.BOMEnabled` | base 內跟著寫進目標，逸出 base 拒寫 |
| PNG（CCI / Chart Composer 下載） | gui `downloadValidatedPNG` → `validateExternalPathInputs("PNG 輸出路徑")` → `fsperm.WriteFileNoFollow` | 否 | `ValidateExternalPath` | **不建** | — | `O_NOFOLLOW` 拒寫 |

使用者看得到的 bug：SaveConfig 把 OutputDir 換到尚不存在的資料夾後，PNG 下載一律失敗，直到重啟 App（只有 Startup 的 `EnsureDirectories` 會建目錄）。其餘差異（BOM 開關只對 3 種 CSV 生效、File-based 不擋敏感位置、三種 symlink 語意）都是同一件事三份實作的漂移。

## Decision

1. **`CSVHandler.placeOutput(subDir, name string, payload outputPayload) (string, error)` 是所有輸出落檔的唯一步驟**（GLOSSARY **Output placement**），流程固定為：
   `safeJoinOutput(OutputDir/subDir/name)`（containment）→ `payload.checkName(Base(path))` → `ValidateExternalPath`（敏感位置，含 symlink resolve）→ `MkdirAll`（先驗證、後建目錄）→ `fsperm.AtomicWriteFile(path, GetAllowedBasePaths(), payload.write)`（crypto tmp → fsync → rename → parent fsync，parent 以 dirfd 錨定在 allow-list 之下）。回傳實際寫入的路徑。
2. **格式由 payload 帶，檔名規則跟著格式走**：`outputPayload{checkName, write}`。
   - `csvPayload(header, emit)`：`ValidateFilename`（`.csv`，[[ADR-0054]]）+ `csvutil.CSVPayload`（BOM 照 `config.BOMEnabled`、formula 逸出、mid-write 探測）。
   - `pngPayload(png)`：`filename.ValidateName`（不綁副檔名）+ `w.Write(png)`。
   步驟序列只有一份；CSV 的 `.csv` 強制保留。
3. **兩種命名、三組入口，都進這一道門**：
   - File-based CSV：`WriteFileCSV(req WriteRequest, data [][]string)`；`WriteMaxMean` / `WriteNormalized` / `WritePhaseAnalysis` 經它。檔名是 `req.Filename`，`req.SubDir` 給 MaxMean batch 寫進 `OutputDir/<batchName>/`。
   - Subject-based CSV：7 個 writer 經 `placeSubjectOutput(subject, suffix, header, emit)`，檔名 `SubjectOutputName(subject, suffix) + ".csv"`，一律寫在 OutputDir 根。**不再收 `WriteRequest`**：原本 Filename 被忽略、SubDir 在所有 production caller 都是空字串。
   - Subject PNG：新增 `WriteSubjectPNG(subject, suffix string, png []byte) (string, error)`，只收已解碼、已驗證的位元組，檔名 `SubjectOutputName(subject, suffix) + ".png"`。
4. **gui PNG 下載只留驗證**：`downloadValidatedPNG(imageData, subject, suffix)` 只做 dataURL prefix 檢查與 `DecodeAndValidatePNG`（[[ADR-0009]] 的驗證半段留在 gui 邊界），落檔交給 `WriteSubjectPNG`。`DownloadCCIChart` / `DownloadChartComposerImage` 只給 suffix（`CCI_Rudolph` / `chart_composer`）。
5. **MaxMean batch 的 `OutputPath` 取自實際寫入路徑**：`maxMeanResultWriter`（pointer receiver）記住最後一次成功寫入的路徑；`SuccessCount > 0` 時 `OutputPath = filepath.Dir(last)`，全部失敗時為空字串。`maxmean.ResultWriter` port 不變。
6. **刪除收斂後的死碼**：`csvutil.WriteCSVAtomic`（連同 `SafeWriteOptions.BasePaths` 與 `writerWrapHook` test seam）、`csvutil.SanitizeAllRows`、`fsperm.OpenWriteValidated`（連同各平台只服務它的 `openValidated`）、`PathValidator.ValidateFilePath`（連同 `ErrPathOutOfScope`）。Task 8 已刪 `WriteCSV` / `writeFileOutput`。PathValidator 只剩 `ValidateExternalPath` / `ValidateExternalDir`；allow-list 只經 `GetAllowedBasePaths()` 給 `AtomicWriteFile` 當 dirfd 錨點。`lenient_path.go` 的 decision table 改列「寫進 OutputDir 的輸出檔 → io 的 Output placement」。fsperm 的 `ErrBasePathsEmpty` / `ErrPathEscapesBase` 訊息拿掉已不存在的函式名（`fsperm.OpenWriteValidated: …` → `fsperm: …`；`errors.Is` 不受影響）。

## 行為變更

本 ADR（Task 9）：

- **PNG 下載在 SaveConfig 換到新資料夾後不必重啟**（本 ADR 修的 bug）。PNG 也得到 containment、敏感位置、dirfd 錨點與 fsync。
- **PNG 錯誤不再帶「PNG 輸出路徑」標籤**：敏感位置現在是「保存圖片失敗: 輸出路徑無效: …」，與 CSV 同一組文字。
- **MaxMean batch 全部檔案失敗時 `OutputPath` 是空字串**（原本照報 `OutputDir/<batchName>`，那個目錄可能根本沒建）。有成功時的值不變。

W9 整體（Task 8 落地 CSV 部分、本 ADR 補上 PNG，一併記錄於此）：

- **File-based CSV 改成 atomic 寫入**：寫到一半失敗不留半截檔，既有同名檔保持原樣。
- **leaf symlink 語意反轉為「取代連結」**：目標檔名是 symlink 時，File-based CSV 原本經 `OpenWriteValidated` 先 EvalSymlinks，base 內的連結**跟著寫進目標檔**、逸出 base 的拒寫；PNG 原本以 `O_NOFOLLOW` 拒寫。現在一律由 rename **取代連結本身**，連結指向的檔案不被碰到（Subject-based CSV 本來就是這樣）。測試：`TestCSVHandler_LeafSymlink*IsReplaced`、`TestDownloadCCIChart_LeafSymlinkIsReplaced`。
- **既有檔換成新 inode**：覆寫不再保留 hard link、owner、ACL 等附屬於舊 inode 的屬性；mode 一律 0600（`fsperm.FilePerm`）。
- **當機可能留下 `*.tmp.<hex>`**：寫入途中 process 被殺或斷電時，OutputDir 可能殘留 `<name>.tmp.<16 hex>`，目標檔不受影響。
- **MaxMean / Normalize / PhaseAnalysis 的 OutputDir 也擋敏感位置**（UNC、AppData 非 Temp 等）：原本 File-based 只跑 `ValidateFilePath` 的 allow-list（OutputDir 本身在 allow-list 內，所以全放行），現在跑 `ValidateExternalPath`，與 Subject-based 一致。
- **BOM 開關套到全部 10 種 CSV**：`bomEnabled:false` 的使用者，7 種 Subject CSV 也不再有 BOM（原本無條件寫），Excel 可能把中文顯示成亂碼。這是使用者確認過的選擇。預設 `bomEnabled:true` 時位元組不變（golden 比對逐位元相同）。
- **File-based 先驗證、後建目錄**：原本 `writeFileOutput` 先 `MkdirAll` 再由 `WriteCSV` 驗證，被拒的路徑會留下空目錄。
- **File-based 寫入不再 log**：`WriteCSV` 原本有 Debug / Info / Error log；placement 不 log（與 Subject-based 一致），GUI handler 仍 log 輸出路徑。
- **File-based 錯誤文字與 Subject 側相同**：`無法創建輸出目錄: …` → `輸出目錄建立失敗: <errno>`；`路徑驗證失敗` → `輸出路徑無效`；`無法建立檔案` / `無法寫入資料到 <path>` → `AtomicWriteFile` 的原始錯誤；`檔案名稱驗證失敗` 不變。
- **絕對路徑的 Filename 接在 OutputDir 之下**：Normalize 的自由輸入 `/abs/out.csv` 落在 `OutputDir/abs/out.csv`（`safeJoinOutput` 是 join，不是取代），`../escape.csv` 仍被拒。這是既有行為，W9 補測試釘住（`TestWriteNormalized_FreeFormNameWithSlash`）。
- raw `WriteFileCSV` 的兩個邊界（production converter 不會產生）：`data[0]` 為 nil 改回錯誤（原本寫出沒有 header 的 CSV）；nil body row 寫成空行（原本 skip 並 Warn）。

## Why

- **一道門才擋得住漂移**：上表每一欄的差異都是某條路「忘了做」或「做法不同」，PNG 不建目錄就是其中一格。收成一道門後，任何輸出都不可能繞過 containment、敏感位置與 atomic；placement 契約由一張表測一次（`csv_handler_placement_test.go`，11 種輸出 × Filename / 敏感目錄 / 不留 tmp / BOM / 建目錄）。
- **[[ADR-0016]] Decision 2 的分界是實作史，不是需求**：「Subject ⟹ atomic、File-based ⟹ 非 atomic」來自 PhaseSync 遷移時各自沿用既有路徑。File-based 的輸出同樣值得 atomic。ADR-0016 Why 的「BOM 忠實度」主張 Subject CSV 無條件寫 BOM 以忠於被刪的 `ExportToCSV`；但 `bomEnabled` 是使用者設定，同一個設定只對部分 CSV 生效才是不忠實（使用者已確認）。
- **檔名規則綁在格式上，而不是 caller 或門內的 switch**：CSV 必須 `.csv`，PNG 不能套 `ValidateFilename`。payload 自帶 `checkName`，步驟序列仍只有一份，新增格式不必改門。
- **驗證在邊界、落檔在 io**：「這是不是合法 PNG」是 gui 邊界的事（dataURL、解碼、尺寸），「寫到哪、怎麼寫」是 io 的事。[[ADR-0009]] 把兩半收在 gui 一個 helper 裡，寫入那半如今與 CSV 同門。
- **回報的路徑必須是實際寫入的位置**（[[ADR-0039]] 同一原則）：batch 全部失敗時沒有這個位置，就不報。
- **死碼刪掉而非留著**：`WriteCSVAtomic` / `OpenWriteValidated` / `ValidateFilePath` / `SanitizeAllRows` 收斂後都只剩自己的測試在養，留著會讓下一個 caller 以為還有第二條路。它們測試中仍有價值的部分已搬到 `CSVPayload`（payload 位元組）、`fsperm.AtomicWriteFile` / `OpenAtomicWriteValidated`（落檔協定、NAME_MAX、stale tmp、base 比對）。

## Considered Options

1. **PNG 自己補 `MkdirAll` 就好**：修得掉 bug，但保留第三套守門，下次漂移照樣發生。否決。
2. **`placeOutput` 依副檔名 switch 檔名規則**：格式知識散進門內，每加一種格式就要改門。否決，改由 payload 帶 `checkName`。
3. **Subject writer 保留被忽略的 `WriteRequest`**：參數存在卻不生效，caller 會以為能指定 SubDir。否決。
4. **保留 `WriteCSVAtomic` 當一行 wrapper**：沒有 production caller；位元組測試直接測 `CSVPayload`、落檔測試直接測 `AtomicWriteFile` 更準。否決。
5. **batch `OutputPath` 照舊在 gui 以 `OutputDir + batchName` 重組**：全部失敗時報一個不存在的目錄，且 gui 重複了 placement 的路徑規則。否決。

## Supersedes / Amends

- **[[ADR-0016]] Decision 2**（「Subject-based ⟹ `WriteCSVAtomic`；File-based ⟹ `WriteCSV`，非 atomic」）與 Why 的「BOM 忠實度」段：由本 ADR 取代。ADR-0016 其餘決定（PhaseSync 走 CSVHandler、刪 `ExportToCSV` 與 precision 欄位）不變。
- **[[ADR-0039]] §3.4**：File-based 單一寫門不再是 `writeFileOutput → WriteCSV`（`ValidateFilename` + `ValidateFilePath` + `OpenWriteValidated`，非 atomic），而是 `WriteFileCSV → placeOutput`。§3.3（`ReadCSV`）與「路徑不 URL-decode」不變。
- **[[ADR-0040]]**：`placeSubjectOutput` 不再是獨立的 placement 步驟，而是 `placeOutput` 的 Subject 命名入口，也不再收 subDir；GLOSSARY 詞條 **Subject output placement** 改名 **Output placement**。錯誤文字「輸出路徑無效」「輸出目錄建立失敗」沿用，並擴及 File-based 與 PNG。
- **[[ADR-0038]] §3.2**：PathValidator 的三個操作剩兩個（`ValidateFilePath` 刪除）；`test/integration` 的 SecurityAndValidationIntegration 改用 `ValidateExternalPath`。§3.1（containment 單一來源）不變。
- **[[ADR-0009]]**：PNG 下載 helper 的寫入半段（`validateExternalPathInputs("PNG 輸出路徑")` + `WriteFileNoFollow`）移到 io 的 `WriteSubjectPNG`；驗證半段（prefix + `DecodeAndValidatePNG`）與「兩個 handler 共用一個 helper」留在 gui。

依 repo 慣例不改被取代 / 修訂的 ADR 本文。

## Related

- [[ADR-0004]]（Subject / File 命名軸；本 ADR 不動該軸，只把兩種命名接到同一道門）
- [[ADR-0017]]、[[ADR-0027]]（validated atomic write 與 `AtomicWriteFile` deep module）
- [[ADR-0036]] Decision 5（webview sink 路徑遮蔽；placement 錯誤的 PHI 性質沿用 [[ADR-0040]]）
- [[ADR-0054]]（檔名單一裁判；`ValidateFilename` = `ValidateName` + `.csv`）
