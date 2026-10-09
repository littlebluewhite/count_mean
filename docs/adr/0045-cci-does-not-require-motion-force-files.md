# CCI 不要求 Motion / Force 檔

**Status**: accepted · **implemented** (2026-10-09)

`parsers.ValidatePhaseManifest` 要求 `MotionFile`、`ForceFile` 非空。CCI 經此函式驗證選定的 row（`loadAndValidate`），但 CCI 只開 EMG 檔，從不開 Motion 或 Force 檔。同一列 manifest 在 muscle_ratio 與 [[Chart Composer]]（不跑此驗證）可用，卻會被 CCI 以「ForceFile: 力板檔案名不能為空」擋下。

## Decision

1. **`ValidatePhaseManifest` 不再檢查 `MotionFile` / `ForceFile`。** 它保留 Subject、EMGFile、EMGMotionOffset 與分期點不變量（順序 / NaN-Inf / motion-index 單調）。
2. **phase_sync 在 `validateManifestData` 補上這兩個檢查**，因為 phase_sync 會開這兩個檔。錯誤型別與文字不變（`models.PhaseSyncValidationError{Field: "MotionFile"/"ForceFile", …}`，外層仍是「分期總檔案數據驗證失敗: %w」）。
3. **行為變更（唯一）**：CCI 對 `MotionFile` / `ForceFile` 為空的 row 由「驗證失敗」變「接受」。phase_sync 對這類 row 仍然失敗。測試：`TestAnalyzeCCI_AcceptsBlankForceFile`（CCI）、`TestValidateManifestData_RequiresMotionAndForceFile`（phase_sync）。

## Why

- 驗證條件應跟「這個 analyzer 實際要開的檔」一致。必填欄位是 consumer 的需求，不是 manifest 格式本身的不變量。
- 同一列 manifest 在三個 consumer 之間可用性不一致（muscle_ratio / Composer 可用、CCI 拒絕），沒有任何領域理由。

## Considered Options

- **A. 維持現狀。** 拒：CCI 多一道與它無關的必填。
- **B. 拿掉檢查，phase_sync 不補。** 拒：phase_sync 會開 Motion / Force 檔，空檔名會變成較晚、較不明確的開檔錯誤，而且改變 phase_sync 的錯誤文字。
- **C. 在 `ValidatePhaseManifest` 加參數控制是否要求。** 拒：一個只有兩個 caller 的旗標；把條件放在需要它的 analyzer 內更直接。

## Consequences

- 錯誤順序的細微差異：phase_sync 同一列同時有「EMGFile 為空」與「MotionFile 為空」時，原本先報 MotionFile，現在先報 EMGFile（Motion / Force 檢查排在 `ValidatePhaseManifest` 之後）。只影響多重錯誤的 row 先報哪一個。
- `ValidatePhaseManifest` 的單元測試刪除 empty motion / force 兩案例，由 phase_sync 的測試取代。

## Related

- [[ADR-0044]] —— 同 wave：[[Subject source]]。
- [[ADR-0012]] —— 各 analyzer 的驗證 policy 由 analyzer 自己持有。
