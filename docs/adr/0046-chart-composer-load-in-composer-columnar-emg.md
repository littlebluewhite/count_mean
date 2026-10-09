# Chart Composer 載入下移 internal/composer、欄式 EMG、Output-1 讀寫同居 io

**Status**: accepted · **implemented** (2026-10-09) · refines [[ADR-0013]]

[[Chart Composer]] 的資料組裝原本寫在 `gui/chart_composer_handlers.go`:找 manifest row、載入 EMG、motion-index 換算、muscle_ratio Output-1 解析、[[Phase timeline]] 轉成秒數 map,全部在 Wails handler 內,只能經 handler 測試。EMG 還要繞一圈:[[Subject source]] 回傳 columnar 的 [[PhaseSyncEMGData]],gui 的 `phaseSyncEMGToDataset` 把它轉成逐 row 的 [[EMGDataset]],`chart.buildEMGSeries` 再轉回 columnar 才跑 LTTB。兩次轉換對缺值的處理也不同:bridge 把短通道補 **0**,`buildEMGSeries` 把 ragged row 補 **NaN**。而 0 和 NaN 的差別在其他地方是有意義的(muscle_ratio 空 cell 刻意讀成 NaN)。此外 `chart.ComposerInput` 上有兩個死欄位:`SelectedChannels` 唯一的 production writer 傳 nil([[ADR-0013]] 拿掉通道選擇 UI 後留下的),`EMGMotionOffset` chart 從未讀取。

## Decision

1. **`chart.ComposerInput` 改吃 columnar EMG。** `EMGDataset *models.EMGDataset` 換成 `EMG *models.PhaseSyncEMGData`,刪除 `SelectedChannels` 與 `EMGMotionOffset`。`buildEMGSeries` 直接讀 `EMG.Time` / `EMG.Channels`,依 `EMG.Headers` 順序渲染全部通道;slice 缺漏或長度 ≠ `len(EMG.Time)` 的通道略過(不補 0 也不補 NaN)。`ErrComposerEMGRequired` 改守 `EMG == nil`。gui 的 `phaseSyncEMGToDataset` 刪除。
   - **refines ADR-0013**:「預設全通道」原本靠「`SelectedChannels` 為空 → fallback 全選」達成;現在沒有選擇欄位,永遠全通道。行為不變,機制少一層。
2. **新套件 `internal/composer`**:

   ```go
   func Load(manifestPath, dataFolder, subject string) (*chart.ComposerInput, error)

   type Stage int // StageManifest / StageEMGOpen / StageEMGParse / StageMotion / StageMuscleRatio
   type LoadError struct{ Stage Stage; Err error } // Error() 與 Err 逐字相同,Unwrap 回 Err
   var ErrSubjectNotFound, ErrMotionFileEmpty error
   ```

   `Load` = `manifest.LoadManifests` → 找 Subject 的第一筆 row → `manifest.LoadEMG` → motion(`OpenDataFile` + `MotionParser`,motion-index 經 `MotionIndexToEMGTime` 換到 EMG 時間軸)→ muscle_ratio Output-1(`row.MuscleRatioFile` 非空才載,`io.ReadMuscleRatioOutputAll`)→ `synchronizer.NewPhaseTimeline` 轉成 phase 名 → EMG 秒數 map。原 gui 的 `findManifestBySubject`、`loadComposerMotion`、muscle_ratio 組裝與 `composerPhaseTimesEMG` 搬入成 unexported 函式;`ErrChartComposerSubjectNotFound` / `ErrChartComposerMotionFileEmpty` 搬入成 `ErrSubjectNotFound` / `ErrMotionFileEmpty`(文字不變)。
3. **錯誤分兩類。** Subject 不在 manifest 是輸入錯誤:`fmt.Errorf("Subject %q %w", subject, ErrSubjectNotFound)`,不帶 Stage。其餘載入失敗都是 `*LoadError`,Stage 標示步驟。gui 以 `errors.Is` 把前者送 `inputMessage`,後者由 `chartComposerLoadFailKey` 依 Stage 選 i18n key(manifest → `KeyErrorHandlerLoadManifestFailed`、EMG 開檔 → `ResolveEMGPathFailed`、EMG 解析 → `ParseEMGFailed`、motion → `ParseMotionFailed`、muscle_ratio → `ParseMuscleRatioFailed`)再交給 `failMessage`。各步驟的錯誤文字(「Motion 路徑解析失敗: 」「muscle_ratio 路徑解析失敗: 」、muscle_ratio 兩個 sentinel 原樣、其餘讀取失敗加「讀取 muscle_ratio CSV 失敗: 」)隨程式碼搬入 composer,不變。
4. **`GenerateChartComposer` 成為 adapter**:nil params guard → `validateManifestHandlerParams` → Subject 非空 → `composer.Load` → `chart.RenderComposer` → envelope。`ChartComposerResult.PhaseTimes` 直接用 `ComposerInput.PhaseTimesEMG`(與 markLine 同一份 map)。RPC DTO 不變;`LoadChartComposerSubjects`、`DownloadChartComposerImage` 不動。
5. **muscle_ratio Output-1 的 reader 與 writer 同居 `internal/io`**(plan 5.3,同 wave 前一步,在此一併記錄)。`io.ReadMuscleRatioOutputAll`(`internal/io/muscle_ratio_reader.go`)是 `CSVHandler.WriteMuscleRatioOutputAll` 的反函式,一個套件持有格式的兩個方向,以 writer → reader round-trip 測試;gui 的手寫 parser(`loadComposerMuscleRatio` / `parseFloatCell`)與手寫 CSV fixture 刪除。reader 唯一的 production caller 是 `composer.Load`。

## Why

- **為何獨立成 `internal/composer`**:`Load` 要 import `io`(Output-1 reader)與 `chart`(`ComposerInput`)。`io` → `cci` → `chart` 的 import 鏈讓 chart 不能 import io;manifest 被 io import,也不能反過來 import io / chart。`go list -deps` 確認 io、cci、chart 都不依賴 composer。
- **刪掉 bridge 就刪掉 0 / NaN 不一致**:columnar 從 parser 一路傳到 LTTB,沒有任何轉換需要補值。parser(`validateEMGDataIntegrity`)保證每個 header 的 slice 與 Time 等長,所以「長度不符略過」對真實資料不會觸發,只是 guard;它與 `downsampleSeriesMap` 的 graceful skip、motion 的 jagged-series skip 一致。
- **可測**:資料組裝現在用 `t.TempDir` 直接測 `Load`,不需要 Wails App。commit 385c435 修的兩個 bug(motion 欄位位移、漏 D/O)都在這段 gui-local 載入碼。
- **Stage 而非 i18n key**:handler 層 localize、下游只回 error([[ADR-0036]])。Stage 讓 gui 不必知道 composer 內部用哪些函式、包哪些錯誤型別,只對一個小 enum 做 switch。`LoadError.Error()` 不加字,所以所有 Message byte-identical(以搬移前後的 handler 對 11 種失敗各跑一次比對,逐字相同;golden 無差異)。

## Considered Options

- **A. `Load` 放進 chart。** 拒:chart 不能 import io(見 Why)。
- **B. `Load` 放進 manifest,與 [[Subject source]] 同處。** 拒:manifest 被 io import,import io / chart 會形成循環;且 Subject source 只負責 EMG,Composer 的 motion / Output-1 / phase 組裝是 Composer 專屬的。
- **C. `Load` 直接回 i18n key 或 localized 字串。** 拒:違反 ADR-0036 的「handler 層 localize」。
- **D. 每個 Stage 一個 sentinel,gui 用 `errors.Is` 串。** 拒:錯誤要同時命中 stage sentinel 又保住 cause 的原文字,得用 `fmt.Errorf("%w: %w")`(`Error()` 多出 sentinel 文字)或自訂多 Unwrap 型別;一個帶 Stage 的型別更直接,形狀與 `manifest.EMGParseError` 相同。
- **E. 保留 `SelectedChannels` 給未來的通道選擇。** 拒:ADR-0013 已拿掉 UI,沒有 caller。需要時再加。
- **F. 長度不符的通道補 NaN 照畫。** 拒:X 軸對不上的 series 不該渲染;略過與 chart 其他 grid 的處理一致。

## Consequences

- 行為不變:HTML、PhaseTimes、所有錯誤 Message byte-identical,golden 無差異。
- `chart.ComposerInput` 少兩個欄位、EMG 型別改變;`chart` 測試改用 columnar fixture,刪 `TestRenderComposer_SelectedChannelsFilter`,新增 `TestBuildEMGSeries_SkipsLengthMismatch`。
- gui 刪 `phaseSyncEMGToDataset`、`findManifestBySubject`、`loadComposerMotion`、`composerPhaseTimesEMG` 與兩個 sentinel;資料組裝測試(phase 換算、motion 首通道、muscle_ratio 空 cell → NaN)移到 `internal/composer`,gui 留 adapter 測試(含 stage → Message 前綴的 table test)。
- [[ADR-0042]] 列出的 gui `composerPhaseTimesEMG` 現為 `composer.phaseTimesEMG`,仍只是把 Phase timeline 轉成 map。
- GLOSSARY 更新 **Chart Composer**、**EMGDataset**、**PhaseSyncEMGData**(bridge 已刪)。

## Related

- [[ADR-0013]] —— 預設全通道;本 ADR 把它的機制從 fallback 改成無選擇欄位。
- [[ADR-0044]] —— [[Subject source]],`Load` 的 EMG 步驟。
- [[ADR-0042]] —— Phase timeline 是分期點秒數的唯一 owner。
- [[ADR-0036]] —— Webview envelope:`failMessage` / `inputMessage`。
- [[ADR-0002]] —— Chart Composer 不寫 CSV、不是 analyzer;讀 Output-1 不違反。
