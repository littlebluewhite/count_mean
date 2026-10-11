# Manifest 讀檔門：分期總檔案與資料檔一律經 fsperm 的 regular-file + 大小上限

**Status**: accepted · **implemented** (2026-10-11) · 延伸 [[ADR-0017]] D4 與 [[ADR-0039]] §3.3

[[ADR-0039]] 讓使用者選取 CSV 的唯一讀門 `CSVHandler.ReadCSV` 有了 pre-stat、fstat（必須是 regular file、≤ 100 MB）與 `LimitReader` 封頂。分期總檔案與 manifest 資料檔沒有這些：

- `PhaseManifestParser.ParseFile(path)` 自己 `os.OpenFile`，沒有大小上限、沒有 regular-file 檢查。超過 100 MB 的分期總檔案整檔讀進記憶體，最後才被 cell 長度檢查擋下；名為 `*.csv` 的 FIFO 以 `O_NONBLOCK` 開成功、讀到 EOF，回「檔案為空」。
- `manifest.OpenDataFile` 開檔後不 fstat，FIFO / 超大的 EMG / Motion / Force / muscle_ratio 檔照樣交給 parser。只有 `ValidateAllEMGFiles` 自己補了一次 fstat。
- 資料檔名只有 lenient path 的長度檢查（`maxFilenameLength`），不經 `filename.ValidateName`（[[ADR-0054]] 的檔名單一裁判）。
- 輸出檔名撞名檢查（`assertUniqueSanitizedSubjects`）只在 muscle_ratio；cci / phase_sync / Composer 讀同一份 manifest 卻不檢查。

## Decision

1. **parsers 不開檔**：`PhaseManifestParser.Parse(r io.Reader, name string)` 取代 `ParseFile`，與 EMG / Motion / ANC 同形（[[ADR-0017]] D4）。錯誤訊息只帶 row/col，`name` 未使用。原 `ParseFile` 的 14 個測試位置：13 個改用 reader（`strings.NewReader` / `bytes.NewReader`），「檔案不存在」那一個搬到 manifest（`TestLoadManifests_FileNotFound`）。

2. **fsperm 讀檔門**（`internal/security/fsperm/read_limited.go`）：

   ```go
   const MaxReadBytes = 100 * 1024 * 1024
   var ErrNotRegular, ErrTooLarge error              // 一般 error,fsperm 不 import i18n
   type TooLargeError struct{ Size, Limit int64 }  // Unwrap() == ErrTooLarge
   func OpenRegularLimited(path string, limit int64) (*LimitedFile, error)
   func CheckRegularSize(f *os.File, limit int64) error
   ```

   `OpenRegularLimited` 依序：pre-stat（非 regular 直接 `ErrNotRegular`，FIFO 不必開）→ `ReadFlags` 開檔（錯誤原樣回傳 `*os.PathError`）→ `CheckRegularSize`（fstat：regular 且 ≤ limit）→ 回 `*LimitedFile`，讀到第 limit+1 byte 時回 `*TooLargeError{Size: limit+1}`。100 MB 常數從 io 搬到這裡。user-facing 訊息由 caller 轉換。

3. **`manifest.LoadManifests(path)` 是分期總檔案的讀檔門**，依序：
   1. `security.DefaultValidator().ValidateExternalPath(path)`（traversal、敏感位置）；
   2. `filename.ValidateName(filepath.Base(path))`（[[ADR-0054]]）；
   3. `fsperm.OpenRegularLimited(path, fsperm.MaxReadBytes)`，錯誤包成「無法開啟檔案 %s: %w」；
   4. `PhaseManifestParser.Parse`；
   5. **撞名檢查**：兩列 Subject 的 `filename.SubjectOutputKey` 相同即整份拒絕，回 `i18n.NewError(KeyErrorManifestSubjectCollision, …)`。

4. **`filename.SubjectOutputKey(subject) = NFC(ToLower(Sanitize(subject)))`**，放在 `SubjectOutputName` 旁邊（[[ADR-0019]] 的 owner）：key 相同 ⇔ 兩個 Subject 推導出的輸出檔在 macOS / Windows 上是同一個檔。

5. **撞名檢查從 muscle_ratio 搬到 manifest**：刪 `assertUniqueSanitizedSubjects`；i18n key `error.muscle_ratio.subject_collision` 改為 `error.manifest.subject_collision`（4 locale 文字不變，zh-TW 逐位元組相同）。muscle_ratio 的 3 支撞名測試照舊走 `Analyze`；`LoadManifests` 另有直接測試。

6. **`manifest.OpenDataFile(baseFolder, name)`** 仍回 `*os.File`（phase_sync 用 `f.Name()`），流程：`EvalSymlinks(baseFolder)` → `filename.ValidateName(path.Base(name))`（先把 `\` 正規化成 `/`；用只認 `/` 的 `path.Base` 而非 `filepath.Base`，各 OS 結果相同）→ `security.OpenLenientValidated` → `fsperm.CheckRegularSize(f, MaxReadBytes)`（失敗先 Close 再回錯）。檔名與 fstat 失敗都包成 `ErrManifestDataFilePathInvalid` + 原錯誤（`fsperm.ErrNotRegular` / `ErrTooLarge` 可 `errors.Is`），phase_sync 的 `mapOpenDataFileErr` 因此對應到「檔案路徑驗證失敗」。刪除：`resolveLenientPath` 的長度檢查與 `security.maxFilenameLength`（`RejectsLongFilename` 測試搬到 manifest）、`ValidateAllEMGFiles` 自己的 fstat（改由 `OpenDataFile` 承擔，FIFO 的錯誤文字逐位元組相同）。

7. **`CSVHandler.ReadCSV` 改用 `fsperm.OpenRegularLimited`**：`ErrNotRegular` → 既有 `notRegularError`；`*TooLargeError` → 既有 `tooLargeError(path, Size)`（「檔案過大（101 MB，上限 100 MB），請分割檔案後再試」逐位元組相同）；其餘 → 既有 `openFailure`。

8. **GUI prelude 的分期總檔案 `ValidateExternalPath` 保留**（見下表）。LoadManifests 的第 1 步是給非 GUI caller 與未來入口的同一道門，不是取代 RPC 邊界。

| LoadManifests caller | 進來前已做的檢查 | 本 ADR 後 |
|---|---|---|
| `muscle_ratio.Analyze` | gui `AnalyzeMuscleRatio` → `validateManifestHandlerParams` | 保留；自己的撞名檢查刪除 |
| `cci.loadAndValidate` | gui `AnalyzeCCI` → `validateManifestHandlerParams` | 保留 |
| `phase_sync.validateManifestFile` | gui `AnalyzePhaseSync` / `AnalyzeNormalizedPhaseSync` → `validateManifestHandlerParams` | 保留 |
| `phase_sync.LoadManifestSubjects` | gui `LoadPhaseManifest` → `validateExternalPathInputs("分期總檔案", …)` | 保留 |
| `composer.Load` | gui `GenerateChartComposer` → `validateManifestHandlerParams` | 保留 |
| gui `LoadChartComposerSubjects` | `validateManifestHandlerParams` | 保留 |

### 範圍外

- **編碼**：不套用 user-picked pipeline 的 UTF-8 規則，真實 V.16 manifest 是 Big5（[[ADR-0041]]）。
- **xlsx 解壓後的大小**：上限只看磁碟上的位元組數；ANC `.xlsx` 經 excelize 解壓後的大小不在此限。
- **資料檔開檔後變大**：`OpenDataFile` 回 `*os.File`，只在開檔時 fstat；分期總檔案與 ReadCSV 才有讀取中封頂。
- **Windows**：`ReadFlags` 沒有 `O_NONBLOCK`，非一般檔案只靠 pre-stat / fstat 擋；FIFO 測試只在非 Windows 跑。

## 行為變更

- **manifest 資料檔名**新擋 `<>:"|?*`、控制字元、磁碟代號（`C:emg.csv`）、每個點分段的 Windows 保留名（`CON.csv`、`emg.NUL.csv`、`sub\LPT1.x.csv`），回 `ErrManifestDataFilePathInvalid`。真實 `input/` 檔名全部通過：V.16 manifest 16 列、64 個資料檔名（EMG / Motion / Force / MuscleRatio），`ValidateName` 拒絕 0 個；`OpenDataFile` 對 `input/SF` 開成功 32 個、不存在 32 個（NSF* 不在 `input/SF`）、路徑無效 0 個。
- **資料檔名過長**的訊息從 lenient 的「檔名過長（> 255）: N 字元」變為 `ValidateName` 的「檔案名稱過長 (最大 255 字符)」，sentinel 不變。
- **分期總檔案** > 100 MB：「無法開啟檔案 <path>: 檔案過大（N MB，上限 100 MB）」，開檔時 fstat 即拒，不再整檔讀進記憶體。FIFO / 目錄：「無法開啟檔案 <path>: 非一般檔案 (mode …)」，原本 FIFO 回「檔案為空」。
- **manifest 資料檔** FIFO / > 100 MB：`OpenDataFile` 回 `ErrManifestDataFilePathInvalid`（phase_sync 顯示「<資料類別> 檔案路徑驗證失敗」）；原本 FIFO 開成功交給 parser。`ValidateAllEMGFiles` 的 FIFO 結果與文字不變。
- **撞名檢查擴及所有 caller**：cci、phase_sync、NPS、Composer、`LoadPhaseManifest` 讀到撞名（或兩列都是空 Subject）的 manifest 時整份拒絕，原本只有 muscle_ratio 拒絕。muscle_ratio 的撞名錯誤因為改由 `LoadManifests` 回傳，前面多了它既有的「解析分期總檔案失敗: 」前綴；後半段文字不變。
- **非 GUI 直接呼叫 `LoadManifests`** 也會擋 traversal 與系統敏感位置；GUI 入口原本就先擋，無感。
- **ReadCSV**：開檔成功但 fstat 失敗（實務上不可達）的訊息從「無法獲取文件信息」變為「無法開啟檔案」，error code 同為 `FILE_NOT_FOUND`。其餘訊息不變。
- golden（真實 V.16 資料走 GUI handler）與 W9 比對 45 檔逐位元組相同。

## Why

- **讀門要有同一套保護**：分期總檔案與資料檔是整檔讀進記憶體的輸入，與 user-picked CSV 同樣有 OOM 與 FIFO 的風險。檢查放在 fsperm 一份，ReadCSV 與 manifest 共用，不再有第二份 pre-stat / fstat / LimitReader。
- **錯誤分層**：fsperm 被 i18n import，只能回一般 sentinel；ReadCSV 轉成既有的 AppError、manifest 包成既有的「無法開啟檔案」/「資料檔案路徑驗證失敗」。`TooLargeError` 帶 `Size`，ReadCSV 的「N MB」才能維持原樣。
- **parsers 收 reader**：開檔屬於讀檔門，parser 只解析，[[ADR-0017]] D4 已對 EMG / Motion / ANC 這樣做，分期總檔案是最後一個例外。
- **撞名是 manifest 的性質**：所有 Subject-based 輸出（CCI、PhaseSync、NPS、MuscleRatio、Composer PNG）的檔名都由 `SubjectOutputName` 從 Subject 推導，撞名不是 muscle_ratio 獨有的問題。key 與 `SubjectOutputName` 同住 filename 套件，Sanitize 規則改了兩者一起改。
- **檔名單一裁判延伸到資料檔**：[[ADR-0054]] 讓 `ValidateName` 成為唯一裁判；lenient path 的長度檢查是同一規則的殘片，收掉。

## Considered Options

1. **manifest 自己 stat / 限大小**：拒。與 ReadCSV 的實作重複一份，兩份會漂移。
2. **`OpenDataFile` 改回 `*LimitedFile`，資料檔也讀取中封頂**：拒。phase_sync 需要 `f.Name()`（解析後路徑），改回傳型別要動全部 caller；開檔時的 fstat 已擋下 FIFO 與磁碟上的超大檔，剩下「開檔後才變大」的情境不值得。
3. **fsperm 回帶 i18n key 的錯誤**：不可行，i18n import fsperm。
4. **撞名檢查留在 muscle_ratio，或每個 analyzer 各自檢查**：拒。輸出檔名身分屬於 Subject，同一條規則寫多份必然漂移；放在讀檔門，所有讀 manifest 的路徑一致。
5. **刪 GUI prelude 對分期總檔案的 `ValidateExternalPath`**：拒。prelude 在 RPC 邊界先擋、錯誤帶「分期總檔案」label，且與 dataFolder 的目錄檢查同一步；改由 `LoadManifests` 擋會改變 UI 文字與檢查順序，換不到東西。
6. **`ValidateName` 直接吃原字串、不正規化 `\`**：拒。Windows 產生的 manifest 用 `\` 分隔子目錄（`sub\emg.csv`），在 Unix 上 `filepath.Base` 不切 `\`，`ValidateName` 會把整串當檔名、以「路徑分隔符」誤拒；lenient path 本來就把 `\` 當分隔符。正規化後也不用 `filepath.Base`：它在 Windows 會先剝掉 volume，`C:emg.csv` 變成 `emg.csv`，磁碟代號規則就不會觸發；manifest 內的檔名不是本機路徑，取 base 不該依 OS。

## Related

- [[ADR-0017]] D4（parser 收 reader）· [[ADR-0039]] §3.3（ReadCSV 讀門；100 MB 上限搬到 fsperm）· [[ADR-0054]]（檔名單一裁判）· [[ADR-0019]]（`SubjectOutputName`）· [[ADR-0048]]（帶 i18n key 的錯誤）· [[ADR-0041]]（manifest 不檢查 UTF-8）
