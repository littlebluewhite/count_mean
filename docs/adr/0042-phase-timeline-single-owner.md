# Phase timeline：manifest row 分期點 → EMG 秒數單一 owner

**Status**: accepted · **implemented** (2026-10-09) · amends [[ADR-0024]]、[[ADR-0029]]

「某個分期點落在 EMG 時間軸的第幾秒」原本由 4 處各自回答:CCI `calculateGaitCycle`(加 `getPhasePointDefs`)、muscle_ratio `collectPhasePoints`、gui `composerPhaseTimesEMG`,以及 phase_sync 經 `PhaseCalculator.GetPhaseTimeRange` → `TimeSynchronizer.GetSyncedTimeRange`。四處做同一件事(讀值 → 判斷力板時間 / motion-index → 換算 → 未提供就略過),規則卻各有出入:CCI 與 muscle_ratio 各有一份逐字相同、對 parse 過的 manifest 永遠不會觸發的 motion-index 上限 guard;Composer 手列 10 個欄位、直接讀 `D` / `O` 原值(commit 385c435 修過它漏掉 D/O 的 bug);`GetSyncedTimeRange` 另外算出沒有任何 consumer 的 motion-index / 力板時間欄位。沒有測試斷言四處一致。

## Decision

1. **新增 `internal/synchronizer/phase_timeline.go`**:

   ```go
   type PhaseTime struct{ Phase models.PhasePoint; EMGTime float64 }
   type PhaseTimeline []PhaseTime
   func NewPhaseTimeline(m *models.PhaseManifest) PhaseTimeline
   func (tl PhaseTimeline) At(p models.PhasePoint) (float64, bool)
   ```

   一個迭代順序(`models.AllPhases()`)、一條出現規則(`parsers.GetPhaseValue`:力板時間 `OptFloat` Set=false、motion-index ≤ 0 為未提供)、一組換算(`ForceTimeToEMGTime` / `MotionIndexToEMGTime`)。**不設** motion-index 上限 guard:D/O 是 int,`parseInt` 已在 parse 階段以 `MaxReasonableMotionIndex` 擋下 > 1e9。
2. **caller 只留各自的 policy**:
   - CCI `calculateGaitCycle`:排除 P0–P2、要求 S/L([[ADR-0018]]);刪 `getPhasePointDefs` / `phasePointDef`。
   - muscle_ratio `collectPhasePoints`:in-range 檢查、排序、中點不變([[ADR-0014]]、[[ADR-0030]])。
   - gui `composerPhaseTimesEMG(m)`:把 timeline 轉成回給前端與 markLine 共用的 phaseTimes map。
   - phase_sync `ResolvePhaseRange`:`tl.At(start)` / `tl.At(end)`。「分期點未提供」與「開始 EMG 時間晚於結束」的錯誤全文 byte-identical;sentinel `ErrPhaseValueZero`、`ErrStartTimeAfterEnd` 從 synchronizer 移到 phase_sync(policy 的 owner)。
3. **刪除**:`TimeSynchronizer.GetSyncedTimeRange`、`SyncedTimeRange`、4 個反向換算(`MotionIndexToTime`、`TimeToMotionIndex`、`MotionIndexToForceTime`、`ForceTimeToMotionIndex`)、`PhaseCalculator.GetPhaseTimeRange`、`models.PhaseTimeRange.StartType` / `EndType` 與 `PhaseTypeMotion` / `PhaseTypeForce`;CCI 與 muscle_ratio 的 `timeSynchronizer` 欄位、重複的 guard 與 warn log。
4. **`chart.composerPhaseOrder` 刪除**,markLine 順序改用 `models.AllPhases()`,Go 端只剩一份分期點順序(frontend 的兩份 `phaseOrder` 不在本範圍)。
5. **測試**:新增 `TestPhaseTimeline` 表(順序、出現規則、兩條換算、t=0、負 EMG 秒數、offset 0、負 motion-index、無上限 guard)與 gui `TestPhaseTimelineAgreement_AllCallers`(characterization:CCI、PhaseSync (S,L)/(D,O)、MuscleRatio Output 2、Composer 對同一張手算表;重構前已在舊碼上綠);刪 synchronizer 的換算與 `GetSyncedTimeRange` / `GetPhaseTimeRange` 測試、`TestComposerPhaseTimesEMG`、muscle_ratio 兩個 motion-index 邊界測試,這些都由表取代。

### Amends ADR-0024

ADR-0024 Considered Options 寫「抽共用 kernel 違反 ADR-0012(三 Domain analyzer 維持刻意分歧)」。本 ADR 把這句**收窄到 analyzer 的外形**:ADR-0012 的 deletion test 針對 analyzer 外層介面的兩條軸(Subject cardinality、Output ownership),不禁止分享 analyzer 之下的純計算。Phase timeline 不改任何 analyzer 的簽章、cardinality 或 output ownership;caller 的 drop / skip / fail policy 仍各自分歧。這和 [[ADR-0030]] 的 `ResolveTimeIndex` 是同一個做法:一個 seam 負責偵測,caller 保留 policy。ADR-0024 本身不修改。

### Amends ADR-0029

ADR-0029 Decision 2 讓 `TimeSynchronizer` 本體「全留」,包括 `GetSyncedTimeRange` 和它依賴的 4 個反向換算。新證據:`GetSyncedTimeRange` 唯一的 production caller `GetPhaseTimeRange` 只取 `StartEMGTime` / `EndEMGTime`,其餘 motion-index / 力板時間欄位零 consumer;4 個反向換算只為了填這些欄位而存在。本 ADR **推翻這項保留**,連同 `SyncedTimeRange` 一起刪除([[ADR-0032]] 已把反向換算延後到此處理)。`MotionIndexToEMGTime`、`ForceTimeToEMGTime`、`ResolveTimeIndex` 仍保留。ADR-0029 本身不修改。

## Why

- **Deletion test 的結果是集中,不是位移。** 刪掉任一份副本,複雜度會在該 caller 重現;四份一起換成 timeline 後,換算、出現規則、順序只在一處,caller 只剩 policy(CCI 取 S..L、MuscleRatio 加中點、Composer 全畫、phase_sync 取一對)。`GetSyncedTimeRange` 的多餘輸出與 4 個反向換算則直接消失。
- **這份重複造成過真實 bug。** commit 385c435 修的是 Composer 手列副本漏掉 D/O;當時只有 gui 那份被 `TestComposerPhaseTimesEMG` 釘住,其他三份沒有一致性測試。現在由一張 `TestPhaseTimeline` 表加一條四 caller agreement 測試涵蓋。
- **上限 guard 不可能觸發。** `parseInt(..., MaxReasonableMotionIndex)` 在 parse 時已擋 |v| > 1e9,`motionIndexOpt` 把 ≤ 0 變成未提供;CCI / MuscleRatio 的 guard 對 parse 過的 manifest 永遠不 fire,Composer 與 phase_sync 本來就沒有。保留它只會讓四處繼續不一致。
- **數值不變。** 換算公式、`int(v)` 轉型、迭代順序都與舊碼相同;characterization 測試在重構前後皆綠,golden(真實 V.16 subject 的全部分析輸出)與 W3 tip byte-identical。

## Considered Options

- **A. `PhaseTimeline` slice + `At`(採用)。** 保留 canonical 順序(MuscleRatio 排序前的輸入順序、Composer markLine 順序都依賴它),`At` 給 phase_sync 做單點查詢。
- **B. 回傳 `map[PhasePoint]float64`。** 拒:map 無序,MuscleRatio 的排序輸入與 Composer 的 markLine 都得各自再排一次,順序知識會回到 caller。
- **C. 保留 `GetSyncedTimeRange`,讓另外三處改呼叫它。** 拒:它一次只處理一對分期點、回傳三個時間系統的欄位,而三個 caller 要的是整排分期點的 EMG 秒數;多餘欄位仍然零 consumer。
- **D. timeline 內保留 motion-index 上限 guard。** 拒:D/O 是 int 且 parser 已設上限,guard 永不觸發。繞過 parser 的 struct-init(只有測試會這樣做)照公式換算,由下游 in-range 檢查處理。
- **E. 維持四份副本,只補一條 agreement 測試。** 拒:測試能抓到分歧,但四份仍要各自維護,Composer 漏 D/O 那一類 bug 仍會重演。

## Consequences

- `synchronizer` 對外新增 `PhaseTime`、`PhaseTimeline`、`NewPhaseTimeline`、`At`,刪除 Decision 3 列出的函式與型別。`TimeSynchronizer` 只剩 `MotionIndexToEMGTime`(gui motion 時間軸與 timeline 共用)與 `ForceTimeToEMGTime`。
- `models.PhaseTimeRange` 只剩 `StartTime` / `EndTime`(EMG 秒數)。
- CCI / MuscleRatio 不再有「motion-index 分期點越界,跳過」warn log;對 parse 過的 manifest 它本來就不會出現。
- phase_sync 錯誤訊息全文不變,sentinel 改由 phase_sync 持有。
- GLOSSARY 新增 **Phase timeline**。

## Related

- [[ADR-0012]] —— 三 Domain analyzer 的外形分歧;本 ADR 位於 analyzer 之下,不動兩條軸。
- [[ADR-0024]] —— **amended**:「共用 kernel 違反 ADR-0012」收窄到外形。
- [[ADR-0029]] —— **amended**:推翻對 `GetSyncedTimeRange` 與 4 個反向換算的保留。
- [[ADR-0030]] —— 同一做法的先例:一個 seam 負責偵測,caller 保留 policy。
- [[ADR-0032]] —— 把反向換算延後到本 wave。
- [[ADR-0018]]、[[ADR-0014]] —— CCI 錨點 policy 與 MuscleRatio Output 2 policy,皆未變。
