# Subject source：manifest 擁有 row + 資料夾 → Subject EMG

**Status**: accepted · **implemented** (2026-10-09) · refines [[ADR-0017]]

「載入一個 [[Subject]] 的 EMG」原本是四段手寫的 open → parse → close → wrap：CCI `loadEMGData`、muscle_ratio `analyzeSubject`、phase_sync `Load`、[[Chart Composer]] `GenerateChartComposer`。四處都走 [[ADR-0017]] 的 `manifest.OpenDataFile`，但各自再串 `parsers.NewEMGParser().Parse` 與關檔，錯誤包裝也各寫一份。

## Decision

1. **`internal/manifest` 新增 [[Subject source]] 入口**：

   ```go
   func LoadEMG(dataFolder string, row *models.PhaseManifest) (*models.PhaseSyncEMGData, error)
   type EMGParseError struct{ Err error } // Error() 與 Err 逐字相同,Unwrap 回 Err
   ```

   `LoadEMG` = `OpenDataFile(dataFolder, row.EMGFile)` → `EMGParser.Parse` → Close。開檔失敗原樣回 `OpenDataFile` 的錯誤（三條 sentinel 的 `errors.Is` 契約不變）；解析失敗包成 `*EMGParseError`。caller 用 `errors.As` 區分兩階段，各自套用原本的 user-facing 前綴（CCI / muscle_ratio / Composer 的 i18n key、phase_sync 的「解析 EMG 檔案失敗」）。`EMGParseError.Error()` 不加字，所以所有輸出文字 byte-identical。
2. **caller**：CCI `loadEMGData`、muscle_ratio `analyzeSubject`、Composer 改呼叫 `LoadEMG`；phase_sync `Load` 在無 test hook 時呼叫 `LoadEMG`。phase_sync 的兩處 `manifestParser.ParseFile` 改走 `manifest.LoadManifests`，`PhaseSyncAnalyzer` 刪除 `manifestParser` 與 `emgParser` 欄位。
3. **不攜帶取樣頻率。** `Parse` 回傳的 frequency 與 CCI `estimateSampleInterval` 讀的是同一對前兩筆 sample，同源；多帶一個值只會讓兩者可能不一致。四個 caller 本來都丟棄它，`LoadEMG` 也不回傳。同時修正 `estimateSampleInterval` 過時的 doc（它不是「被 analyzer / chart / muscle_ratio 多個 caller 用」，只有 cci 內兩處使用）。
4. **manifest 套件 doc 重寫**：刪除「不要 bundle 後續 ParseFile / 迭代 / 錯誤處理」。該句的理由是 caller 對錯誤的處置不同（CCI fail-fast、muscle_ratio per-subject batch）；回傳 `(data, err)` 的載入器不替 caller 選 policy，理由不適用於 `LoadEMG`。[[ADR-0012]] 的 [[Domain analyzer]] 分歧形狀不受影響：載入是每個 analyzer 之下的一步，fail / batch / write ownership 仍由各 analyzer 持有。`PhaseManifest` 的 consumer 清單補上 Chart Composer（第 5 個）。
5. **phase_sync 的 `SetParseEMGFileFnForTest` 暫留。** 它服務 in-flight cancel test；`Load` 在 hook 有注入時改走「`OpenDataFile` → hook」，無 hook 才走 `LoadEMG`。hook 與該 test 一併於後續 task 刪除，屆時 `Load` 只剩 `LoadEMG` 一條路。phase_sync 的 `validateEMGFilePath` 仍先開檔確認存在性再 Close、`Load` 再載入一次（validate-early 的結構不變）。

## Why

- **Deletion test**：`LoadManifests` 是單純 pass-through（本次仍保留，因為現在 phase_sync 也走它，四個 caller 共用同一入口）；四段 open / parse / close 刪掉後，複雜度會重新出現在四個 caller，所以 `LoadEMG` 是賺到的深度，不是 pass-through。
- **用 typed error 而非多回傳值或兩個函式**：caller 需要的只是「這是開檔還是解析失敗」。`errors.As` 讓成功路徑的簽章保持 `(data, err)`，且不改變任何錯誤字樣。
- **local-substitutable**：檔案系統走 `OpenDataFile`，測試用 `t.TempDir`，不需要 mock。

## Considered Options

- **A. `LoadEMG` 回 `(data, freq, err)`。** 拒：頻率與 sample interval 同源（見 Decision 3）。
- **B. `LoadEMG` 把錯誤包成 i18n 字串。** 拒：四個 caller 的前綴不同，且 phase_sync 的錯誤文字要等後續 task 一起遷 i18n key。
- **C. 拆成 `OpenEMG` + `ParseEMG` 兩個函式。** 拒：等於把原本四段重複搬到 manifest 套件，caller 仍要自己接。
- **D. 現在就刪 `SetParseEMGFileFnForTest`。** 拒：它服務的 cancel test 要重寫，另案處理。

## Consequences

- 行為不變：所有輸出數值與錯誤字樣 byte-identical，golden 無差異。
- 新增 `manifest.LoadEMG`、`manifest.EMGParseError`；`PhaseSyncAnalyzer` 少兩個欄位。
- GLOSSARY 新增 **Subject source**，更新 **Manifest**。

## Related

- [[ADR-0017]] —— `OpenDataFile` 仍是唯一的硬化讀檔門，`LoadEMG` 站在它之上。
- [[ADR-0012]] —— analyzer 形狀不變。
- [[ADR-0045]] —— 同 wave：CCI 不再要求 Motion / Force 檔。
