# EMG 時間軸單一 owner：in-range 用 ±emgTimeEpsilon，切片用 ms 取整（文件化）

**Status**: accepted · **implemented** (2026-10-09) · extends [[ADR-0030]]

「時間 t 在不在 EMG 錄製範圍內」與「哪些 sample 屬於 [a, b]」原本散在四處、各有規則：[[ADR-0030]] 的 seam `ResolveTimeIndex`（±`emgTimeEpsilon` = 1e-6，CCI phase_stats 與 muscle_ratio 用）；CCI `validateEMGBounds` 自己的私有 `boundsEpsilon = 1e-6`；phase_sync `validateEMGTimeRange` 的 strict-0 比較；`parsers.FindTimeRangeIndices` 把端點與 sample 都取整到毫秒再比（被 `GetEMGDataInTimeRange` 用於 CCI 抽取、phase_sync 統計、NPS 統計與 `calculator.RangeNormalizer`）。`FindTimeRangeIndices` 的註解自稱「全專案的時間比較慣例」，[[ADR-0030]] 則說 `emgTimeEpsilon` 是共享 seam 契約，兩者都沒說清楚自己管哪一件事。

## Decision

1. **[[EMG time axis]] 的兩個操作都放在 `internal/synchronizer/time_sync.go`，與 `emgTimeEpsilon` 同處，各有一條文件化的規則**：

   ```go
   func OutsideEMG(times []float64, t float64) (before, after bool)                  // in-range:±emgTimeEpsilon
   func SliceEMG(d *models.PhaseSyncEMGData, start, end float64) (*EMGSlice, error) // 切片:毫秒取整、含端點
   type EMGSlice struct{ Data *models.PhaseSyncEMGData; ActualStartTime, ActualEndTime float64 }
   ```

   - **in-range 用 ±ε**。`OutsideEMG`：`before = !(t >= times[0]−ε)`、`after = !(t <= times[len−1]+ε)`；空時間軸與 NaN 的 t 回 `(true, true)`。`ResolveTimeIndex` 的 `inRange` 改由它推導（`!before && !after`，謂詞與舊式等價），全專案只剩一條 in-range 規則。
   - **切片用毫秒取整**。`SliceEMG`：start、end 與每筆 sample 都 `math.Round` 到整數毫秒，取含端點的 `[startMs, endMs]`；線性掃描，第一筆取整後 > endMs 時停止。算式與舊 `FindTimeRangeIndices` 相同，輸出依構造 byte-identical。錯誤字樣不變：nil / 空資料 `EMG 數據為空: …`（`parsers.ErrNilData`）、`開始時間 %.3f 不能大於結束時間 %.3f`、`找不到有效的時間範圍數據: …`（sentinel `ErrTimeRangeNotFound` 從 parsers 移到 synchronizer）。
   - **兩條規則相容**：`math.Round` 單調，端點在首 / 末筆外側時，取整後不會越過該 sample 的毫秒，所以通過 `OutsideEMG` 的端點，其邊界 sample 一定被切入。
2. **caller**：
   - CCI `validateEMGBounds`：刪私有 `boundsEpsilon`，改呼叫 `OutsideEMG`；NaN / Inf 守門與 i18n 錯誤訊息不變。CCI 抽取 `[S−150ms, min(L+150ms, 末筆)]`（[[ADR-0018]]）改用 `SliceEMG`。
   - phase_sync `validateEMGTimeRange`：strict-0 改為 `OutsideEMG`，錯誤全文不變；`AnalyzePhaseSync` 改用 `SliceEMG`。
   - gui NPS 統計視窗、`calculator.RangeNormalizer` 標準化視窗改用 `SliceEMG`。calculator 新增對 synchronizer 的 import；`go list -deps ./internal/synchronizer` 不含 calculator，無 cycle。
3. **刪除**：`parsers.GetEMGDataInTimeRange`、`parsers.EMGTimeRangeResult`、`parsers.FindTimeRangeIndices`、`parsers.ErrTimeRangeNotFound`。
4. **測試**：
   - `TestOutsideEMG`（含與 `ResolveTimeIndex` inRange 一致性）；phase_sync `TestResolvePhaseRange_ToleratesSyncDriftAtEMGEdges`（修補前紅：5e-7 的同步飄移被 strict-0 拒收）。
   - `TestSliceEMG`（毫秒取整規則）、`TestSliceEMG_Float32NoiseAtLargeTime`（60 s 附近的 float32 雜訊端點仍切入邊界 sample，±1e-6 會漏切）、`TestSliceEMG_SubMillisecondEndBoundary`（< 1 ms 間距時的已知行為）、`TestSliceEMG_Errors`（錯誤全文與 sentinel）。
   - parsers 的 `TestFindTimeRangeIndices`、`TestEMGParser_GetDataInTimeRange`、`TestRangeExtractor_NilData` 由上述 synchronizer 測試取代後刪除。

## 行為清單

| # | 位置 | 變更前 | 變更後 | 淨效果 |
|---|---|---|---|---|
| 1 | phase_sync `ResolvePhaseRange`（PhaseSync、NPS 的標準化 / 統計視窗） | strict-0：`StartTime < emgMin`、`EndTime > emgMax` 即越界 | `OutsideEMG`（±1e-6） | 分期點在 EMG 首 / 末筆外 1e-6 內（同步 ULP 飄移）由「`ErrEMGTimeOutOfRange` 失敗」變「接受」；> 1e-6 照擋，訊息不變。NaN 改判越界（parse 過的 manifest 不會出現 NaN）。 |
| 2 | CCI `validateEMGBounds` | `gaitStart+1e-6 < emgMin`、`gaitEnd > emgMax+1e-6` | `OutsideEMG` | 同一容差；兩種寫法只可能在比較式的最後一個 ULP 不同。測試與 golden 皆不變。 |
| 3 | 切片：CCI 抽取、PhaseSync 統計、NPS 統計、`RangeNormalizer` 標準化視窗 | `parsers.FindTimeRangeIndices` 毫秒取整 | `synchronizer.SliceEMG` 毫秒取整（同一算式） | 無。只換 owner 與位置；毫秒取整的適用範圍從「全專案慣例」收窄為「切片」。 |
| 4 | `ResolveTimeIndex` | inRange 自行比較 | 由 `OutsideEMG` 推導 | 謂詞等價，不變。 |

golden（V.16 manifest 的 SF2：CCI、MuscleRatio、PhaseSync、NPS、Chart Composer、MaxMean、Normalize、Phases）與變更前 byte-identical。

## Why

- **[[ADR-0030]] 的理由逐字適用於 phase_sync 的 in-range。** 它的分期點同樣走 `ForceTimeToEMGTime` 同步（現在經 [[ADR-0042]] 的 Phase timeline），strict-0 就是 0030 在 muscle_ratio 修掉的 latent bug：同步飄移會誤拒合法分期點。CCI 的私有 `boundsEpsilon` 則是同一常數的第二份。
- **切片的端點帶 float32 匯出雜訊，量級會超過 ε。** manifest 的力板時間是毫秒精度（3 位小數）的值，以 float32 匯出、印到 6 位小數。float32 的 ULP 隨數值變大：16–32 s 為 1.9e-6、32–64 s 為 3.8e-6。值的誤差 ≤ 半個 ULP，再加上印到 6 位小數的 ≤ 0.5e-6，而印出值與真值的差必為 1e-6 的整數倍，所以：< 16 s 印出無雜訊、16–32 s 最多 ±1e-6、32–64 s 最多 ±2e-6。V.16 manifest 吻合：123 個力板時間中，< 16 s 的 78 個全是乾淨的 3 位小數，≥ 16 s 的 45 個有 26 個恰差 ±1e-6（`16.780001`、`17.809999`；最大值 19.798 s）。
- **實測：±ε 切片在現有資料上只靠浮點捨入保住邊界 sample。** 以 V.16 manifest 全部 16 列、187 個分期 EMG 秒數（10 個分期點加 CCI 的 S−150ms / L+150ms），各當起點與終點，在 100 Hz 時間軸上比對兩種規則：首 / 末 sample 0 處不同。但其中 7 個（NSF1 P1 / S / C / S−150ms、NSF5 C、SF6 L / L+150ms）與某個 sample 的十進位距離恰為 1e-6，浮點運算後落在 ε 內約 1e-15，邊界 sample 是靠捨入方向才被切入。32 s 以上的試次雜訊可達 2e-6，±ε 會**靜默**少切一筆邊界 sample；SF2 是短試次，golden 涵蓋不到。毫秒取整與資料的有效精度一致，雜訊端點與 sample 取整到同一毫秒。
- **in-range 與切片的失敗代價不同。** in-range 越界的結果看得見（錯誤、warn、略過 Output 2），切片少一筆則完全靜默。in-range 的 ±ε 只在分期點恰落在 EMG 首 / 末筆時才會碰上雜訊，所以維持 [[ADR-0030]] 的 ε；切片則改用不受雜訊影響的毫秒取整。
- **Deletion test。** 私有 `boundsEpsilon`、phase_sync 的 strict 比較都刪除，`FindTimeRangeIndices` / `GetEMGDataInTimeRange` 併入 `SliceEMG`；EMG 時間軸的邊界知識集中在 `time_sync.go` 一處，兩條規則各自寫明適用範圍。caller 的 fail / drop / skip policy（[[ADR-0012]]）不變。

## Considered Options

- **A. in-range 用 ±ε、切片用文件化的毫秒取整，同一模組持有（採用）。** 輸出依構造不變；兩條規則相容（見 Decision 1）。
- **B. in-range 與切片共用 ±ε（plan 4.3 的原案）。** 曾在 `d62bc92` 實作；golden 與全 manifest 端點比對雖無差異，但上述 7 個值只靠 ~1e-15 的捨入餘量，32 s 以上的試次會靜默漏切。拒。
- **C. 放大 `emgTimeEpsilon`（例如 1e-5）讓切片也能用 ε。** 拒：重開 [[ADR-0030]] 的常數與三個 package 的邊界測試；float32 雜訊隨時間成長，固定 ε 只是把問題延後到更長的試次；切片的語意本來就該對齊資料的毫秒精度。
- **D. 兩端先以 `ResolveTimeIndex` snap 到最近 sample 再含端點切。** 拒：端點落在兩 sample 之間時，會切入區間外最多半個 sample interval 的資料，結果隨 sample 間距變動。
- **E. phase_sync 保留 strict-0。** 拒：即 [[ADR-0030]] Option B 拒絕的 latent bug。

## Consequences

- `synchronizer` 新增 `OutsideEMG`、`SliceEMG`、`EMGSlice`、`ErrTimeRangeNotFound`；`parsers` 刪除 Decision 3 列出的項目。新增依賴邊 calculator → synchronizer。
- 所有錯誤訊息全文不變（`SliceEMG` 沿用舊 parsers 字樣，CCI / phase_sync 的越界訊息不動）。
- **毫秒取整的已知限制**（沿用舊行為）：假設 sample interval ≥ 1 ms。> 1 kHz 時（[[ADR-0002]] 提到 EMG 2000 Hz）相鄰 sample 可能取整到同一毫秒，區間兩端各可能多切入一筆距端點 < 0.5 ms 的 sample；半毫秒的 sample 會碰上 round-half-away-from-zero，結果取決於 `t*1000` 的浮點表示（`TestSliceEMG_SubMillisecondEndBoundary`）。`input/` 的 EMG 全是 100 Hz。NaN 端點的取整結果依平台而定，與舊碼相同；parse 過的 manifest 不會產生 NaN。若日後出現 > 1 kHz 的 EMG，再評估以 sample interval 為尺度的切片規則。
- in-range 的 ±ε 同樣會碰上 float32 雜訊，但只在分期點恰落於 EMG 首 / 末筆時，且結果可見。若真實資料出現這種誤拒，調整 `emgTimeEpsilon`（屬 [[ADR-0030]] 的判斷）。
- GLOSSARY 新增 **EMG time axis**。

## Related

- [[ADR-0030]] —— **extends**：0030 把 `emgTimeEpsilon` 統一給 CCI phase_stats 與 muscle_ratio；本 ADR 擴到 CCI / phase_sync 的範圍檢查，並把切片規則明訂為毫秒取整。
- [[ADR-0042]] —— Phase timeline 產生分期 EMG 秒數，本 ADR 決定它們在 EMG 時間軸上的檢查與切片規則。
- [[ADR-0018]] —— CCI ±150ms 抽取範圍不變，只換切片實作的位置。
- [[ADR-0012]] —— 各 analyzer 的越界 policy（fail / drop / skip）不變。
- [[ADR-0002]] —— EMG 2000 Hz 的描述，毫秒取整的已知限制。
