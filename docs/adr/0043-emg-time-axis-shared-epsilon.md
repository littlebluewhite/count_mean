# EMG 時間軸：in-range 與切片共用 emgTimeEpsilon

**Status**: accepted · **implemented** (2026-10-09) · extends [[ADR-0030]]

「時間 t 在不在 EMG 錄製範圍內」與「哪些 sample 屬於 [a, b]」原本有四套邊界規則：[[ADR-0030]] 的 seam `ResolveTimeIndex`（±`emgTimeEpsilon` = 1e-6，CCI phase_stats 與 muscle_ratio 用）；CCI `validateEMGBounds` 自己的私有 `boundsEpsilon = 1e-6`；phase_sync `validateEMGTimeRange` 的 strict-0 比較；`parsers.FindTimeRangeIndices` 把端點與 sample 都取整到毫秒再比（被 `GetEMGDataInTimeRange` 用於 CCI 抽取、phase_sync 統計、NPS 統計與 `calculator.RangeNormalizer`）。`FindTimeRangeIndices` 的註解自稱「全專案的時間比較慣例」，[[ADR-0030]] 則說 `emgTimeEpsilon` 是共享 seam 契約，兩者衝突。同一次 phase_sync 分析裡，`ResolvePhaseRange` 先以 strict-0 驗證，`AnalyzePhaseSync` 再以毫秒取整（約 ±0.5 ms）切片，前後兩套規則。

## Decision

1. **EMG 時間軸的兩個操作放在 `internal/synchronizer/time_sync.go`，與 `emgTimeEpsilon` 同處**：

   ```go
   func OutsideEMG(times []float64, t float64) (before, after bool)
   func SliceEMG(d *models.PhaseSyncEMGData, start, end float64) (*EMGSlice, error) // 含端點 [start−ε, end+ε]
   type EMGSlice struct{ Data *models.PhaseSyncEMGData; ActualStartTime, ActualEndTime float64 }
   ```

   - `OutsideEMG`：`before = !(t >= times[0]−ε)`、`after = !(t <= times[len−1]+ε)`。空時間軸與 NaN 一律回 `(true, true)`（不能證明在範圍內就算越界）。`ResolveTimeIndex` 的 `inRange` 改由它推導（`!before && !after`，謂詞與舊式等價），全專案只剩一條 in-range 規則。
   - `SliceEMG`：保留舊的線性掃描形狀（從第一筆 ≥ start−ε 切起，第一筆 > end+ε 停止），只把比較從整數毫秒換成 ±ε；Data 仍是原資料的子切片。錯誤字樣不變：nil / 空資料 `EMG 數據為空: …`（`parsers.ErrNilData`）、`開始時間 %.3f 不能大於結束時間 %.3f`、`找不到有效的時間範圍數據: …`（sentinel `ErrTimeRangeNotFound` 從 parsers 移到 synchronizer）。通過 `OutsideEMG` 的區間端點，其邊界 sample 一定被切入，檢查與切片不再各說各話。
2. **caller**：
   - CCI `validateEMGBounds`：刪私有 `boundsEpsilon`，改呼叫 `OutsideEMG`；NaN / Inf 守門與 i18n 錯誤訊息不變。CCI 抽取 `[S−150ms, min(L+150ms, 末筆)]`（[[ADR-0018]]）改用 `SliceEMG`。
   - phase_sync `validateEMGTimeRange`：strict-0 改為 `OutsideEMG`，錯誤全文不變；`AnalyzePhaseSync` 改用 `SliceEMG`。
   - gui NPS 統計視窗、`calculator.RangeNormalizer` 標準化視窗改用 `SliceEMG`。calculator 新增對 synchronizer 的 import；`go list -deps ./internal/synchronizer` 不含 calculator，無 cycle。
3. **刪除**：`parsers.GetEMGDataInTimeRange`、`parsers.EMGTimeRangeResult`、`parsers.FindTimeRangeIndices`、`parsers.ErrTimeRangeNotFound`。
4. **測試**：新增 `TestOutsideEMG`（含與 `ResolveTimeIndex` inRange 一致性）、`TestSliceEMG`、`TestSliceEMG_Errors`（錯誤全文與 sentinel）、`TestSliceEMG_SubMillisecondEndBoundary`（記錄下表第 3 列的預期改變）、phase_sync `TestResolvePhaseRange_ToleratesSyncDriftAtEMGEdges`（修補前紅）。parsers 的 `TestFindTimeRangeIndices`、`TestEMGParser_GetDataInTimeRange`、`TestRangeExtractor_NilData` 由上述 synchronizer 測試取代後刪除。

## 行為清單

| # | 位置 | 變更前 | 變更後 | 淨效果 |
|---|---|---|---|---|
| 1 | phase_sync `ResolvePhaseRange`（PhaseSync、NPS 的標準化 / 統計視窗） | strict-0：`StartTime < emgMin`、`EndTime > emgMax` 即越界 | `OutsideEMG`（±1e-6） | 分期點在 EMG 首 / 末筆外 1e-6 內（同步 ULP 飄移）由「`ErrEMGTimeOutOfRange` 失敗」變「接受」；> 1e-6 照擋，訊息不變。NaN 改判越界（parse 過的 manifest 不會出現 NaN）。 |
| 2 | CCI `validateEMGBounds` | `gaitStart+1e-6 < emgMin`、`gaitEnd > emgMax+1e-6` | `OutsideEMG` | 同一容差；兩種寫法只可能在比較式的最後一個 ULP 不同。測試與 golden 皆不變。 |
| 3 | 切片：CCI 抽取、PhaseSync 統計、NPS 統計、`RangeNormalizer` 標準化視窗 | 端點與 sample 都 `math.Round` 到毫秒再含端點比較（約 ±0.5 ms） | `[start−1e-6, end+1e-6]` | 落在區間外 (1e-6, 0.5 ms] 的 sample 不再切入。只在分期 EMG 秒數不在 sample 格點上、且相差不到 0.5 ms 時發生 —— 實務上是 > 1 kHz 的 EMG，或帶次毫秒小數的分期時間。 |
| 4 | `ResolveTimeIndex` | inRange 自行比較 | 由 `OutsideEMG` 推導 | 謂詞等價，不變。 |

**真實資料**：golden（V.16 manifest 的 SF2：CCI、MuscleRatio、PhaseSync、NPS、Chart Composer、MaxMean、Normalize、Phases）與變更前 byte-identical。另以 V.16 manifest 全部 16 列做端點比對：187 個分期 EMG 秒數（10 個分期點加 CCI 的 S−150ms / L+150ms），各當起點與終點，在 100 Hz 時間軸上新舊規則的首 / 末 sample **0 處不同**。`input/` 內的 EMG 檔全是 100 Hz；分期 EMG 秒數落在 1 ms 格點上（力板時間 3 位小數、offset 為 4 ms 倍數），與 sample 的距離不是 0 就是 ≥ 1 ms，第 3 列不會觸發。

**已知邊界（未觸發，記錄在案）**：V.16 manifest 的力板時間帶 float32 匯出雜訊（`16.780001`、`17.809999`）。187 個中有 7 個（NSF1 P1 / S / C / S−150ms、NSF5 C、SF6 L / L+150ms）與某個 sample 的十進位距離恰為 1e-6，浮點運算後落在 ε 內約 1e-15，因此邊界 sample 照舊切入。這個餘量來自浮點捨入，不是設計：`x.xxx001` 當起點、`x.xxx999` 當終點時，結果可能落在 ε 任一側。力板時間 ≥ 32 s 時 float32 ULP 為 3.8e-6，印到 6 位小數的雜訊可達 ±2e-6，會超出 ε，邊界 sample 就不再切入（舊的毫秒規則會切入）。目前資料的力板時間都 < 20 s。

## Why

- **[[ADR-0030]] 的理由逐字適用於 phase_sync。** 它的分期點同樣走 `ForceTimeToEMGTime` 同步（現在經 [[ADR-0042]] 的 Phase timeline），strict-0 就是 0030 在 muscle_ratio 修掉的 latent bug：同步飄移會誤拒合法分期點。CCI 的私有 `boundsEpsilon` 則是同一個常數的第二份。
- **兩份文件都自稱「慣例」，必須有一份勝出。** `emgTimeEpsilon` 有物理理由（吸收同步後的 ULP 飄移，比 1 kHz sample interval 細 3 個量級），也已是共享 seam。毫秒取整則假設 sample interval ≥ 1 ms：> 1 kHz 時相鄰 sample 取整到同一毫秒，切片邊緣會多一筆或少一筆（[[ADR-0002]] 提到 EMG 2000 Hz）；2 kHz 的半毫秒 sample 還會碰上 round-half-away-from-zero，結果取決於 `t*1000` 的浮點表示。
- **檢查與切片用同一把尺。** 過去 phase_sync 先以 strict-0 拒收、再以 ±0.5 ms 切片；現在通過 `OutsideEMG` 的端點，其邊界 sample 一定在 `SliceEMG` 的結果內。
- **Deletion test。** 私有 `boundsEpsilon`、phase_sync 的 strict 比較、`FindTimeRangeIndices` 的取整都刪除，邊界知識集中在 `time_sync.go` 一處。caller 的 fail / drop / skip policy（[[ADR-0012]]）不變。

## Considered Options

- **A. in-range 與切片共用 ±`emgTimeEpsilon`（採用）。** 一條規則、一個常數；golden 與全 manifest 端點比對皆無數值變化。
- **B. 把毫秒取整寫成文件化的切片規則（plan 的 fallback）。** 拒：真實資料上新舊規則輸出相同，沒有觸發 fallback 的證據；毫秒取整在 > 1 kHz 不成立，且會讓同一次分析保留兩把尺。若日後真實資料出現第 3 列的差異且不能解釋為邊界 ±ε，再回到這個選項。
- **C. 兩端先以 `ResolveTimeIndex` snap 到最近 sample 再含端點切。** 拒：端點落在兩 sample 之間時，會切入區間外最多半個 sample interval 的資料，切片結果隨 sample 間距變動，與 `OutsideEMG` 的 ±ε 檢查又成兩把尺。
- **D. phase_sync 保留 strict-0。** 拒：即 [[ADR-0030]] Option B 拒絕的 latent bug。

## Consequences

- `synchronizer` 新增 `OutsideEMG`、`SliceEMG`、`EMGSlice`、`ErrTimeRangeNotFound`；`parsers` 刪除 Decision 3 列出的項目。新增依賴邊 calculator → synchronizer。
- 所有錯誤訊息全文不變（`SliceEMG` 沿用舊 parsers 字樣，CCI / phase_sync 的越界訊息不動）。
- 若日後資料來源的分期雜訊接近或超過 1e-6（見「已知邊界」），應調整 `emgTimeEpsilon`（所有 caller 一起，屬 [[ADR-0030]] 的判斷），不要讓切片另立一套規則。
- GLOSSARY 新增 **EMG time axis**。

## Related

- [[ADR-0030]] —— **extends**：0030 把 `emgTimeEpsilon` 統一給 CCI phase_stats 與 muscle_ratio；本 ADR 擴到 CCI / phase_sync 的範圍檢查與全部切片。
- [[ADR-0042]] —— Phase timeline 產生分期 EMG 秒數，本 ADR 決定它們在 EMG 時間軸上的檢查與切片規則。
- [[ADR-0018]] —— CCI ±150ms 抽取範圍不變，只換切片實作。
- [[ADR-0012]] —— 各 analyzer 的越界 policy（fail / drop / skip）不變。
- [[ADR-0002]] —— EMG 2000 Hz 的描述，毫秒取整不成立的情境。
