# CCI 分期區間統計改為中點 ±50ms 視窗(amends ADR-0018 §3 需求 1)

**Status**: accepted — **implemented**(2026-06-05;feat `c490a38` + tests `53540d1`/`adbf9c1`)。amends [[ADR-0018]] §3 需求 1(原「區間整段平均」)。

## Decision

把 CCI Output 2(`{safeSubject}_CCI_Rudolph_phases.csv`)的 **8 個分期區間列**(`S-C, S-D, C-D, D-T, D-T0, T0-T, T-O, O-L`)從「整段索引範圍平均」改為「兩端點**時間中點**的 ±50ms(11 點)視窗平均」。五項一併鎖定:

1. **演算法**:`mid = synchronizer.FindNearestTimeIndex(TimeValues, (PhaseTimes[from] + PhaseTimes[to]) / 2)`,再 `windowmean.WindowMean(series, mid, band50Half)`(`band50Half = 5` → `MeanRange(mid−5, mid+5)`)。取代既有 `windowmean.MeanRange(s, iFrom, iTo)`。與 muscle_ratio `buildPhasePoints`(`analyzer.go:271-276`)的中點視窗機制逐行對應。
2. **中點定義 = time-midpoint(非 index-midpoint)**:取兩端點 EMG 時間平均後 snap 最近 sample。100Hz 均勻取樣下與 `(iFrom+iTo)/2` 幾乎等價(僅索引和為奇數時差 1 sample);選 time-midpoint 是為跨 feature 與 muscle_ratio 機制一致。
3. **指標標籤**:`指標` 欄常數 `metricInterval` 由 `"區間平均"` 改為 `"中點±50ms"`。
4. **`Time (s)` 欄**:interval 列由 `HasTime=false`(留空)改為 `HasTime=true`、`Time = TimeValues[mid]`(snapped 中點 sample 時間),比照點列與 muscle_ratio 顯示視窗中心。
5. **形狀不變**:總列數維持 **32**;`band50Half` 與點列 ±50ms 共用同一常數(鎖在一起);`±25ms` / `前100ms` / `落地` 列完全不動;缺端點(`!okFrom || !okTo`)仍輸出 NaN×nPairs 列。

**衍生**:`computeRow` 的 `if lo > hi { lo, hi = hi, lo }` motion 反轉 swap(`phase_stats.go:198-201`)刪除 —— 中點 `(a+b)/2` 可交換,D/O motion-index 反序自動處理。

## Why

- **局部視窗比整段平均更能代表分期「轉換」的瞬時共收縮**,且與點列(±50ms)同尺度、可直接比較。沿用 [[ADR-0014]]/[[ADR-0018]] 對視窗平均抗雜訊的同款理由,只是 center 從分期點移到區間中點。
- **與 muscle_ratio 語意對齊**:[[ADR-0018]] §3 需求 1 當初借用 muscle_ratio `biomechanicalIntervalMidpoints` 的生物力學區間定義(S-D 跳 C、D-T 跳 T0),卻刻意「取整段平均**而非中點值**」。本案把值的算法也對齊中點,使兩 feature 對「區間」的表徵一致 —— 都是「中點 window mean」。
- **time-midpoint 而非 index-midpoint**:CCI `phase_stats.go` 雖原生 index-space(用 index-midpoint 更簡單、可省一次時間換算),但決策取跨 feature 機制一致。數值差異上界 1 sample(10ms @ 100Hz),可忽略。

## Considered Options

- **維持整段平均(ADR-0018 原案)** — 拒:需求即是改為中點視窗值。
- **index-midpoint `(iFrom+iTo)/2`** — 評估後採 time-midpoint:雖與 CCI 既有 index-space 其餘視窗(`i±5`、`i−10`)同一心智模型、更簡單,但 user 選與 muscle_ratio 機制一致;數值差異 ≤1 sample。
- **只改 7 列、排除 O-L** — 拒:初始需求只列 7 個(漏 O-L),但 O-L 結構與其他 7 列相同,排除會造成「7 列中點窗 + 1 列整段平均」混合語意;確認為抄漏,全 8 列一致改。
- **保留 `區間平均` 標籤** — 拒:改算法後名實不符,且與點列 `±50ms` 自相矛盾(項目欄已足以區分點/區間)。
- **`Time (s)` 留空** — 拒:中點視窗有明確 center sample,填 `times[mid]` 與點列/muscle_ratio 一致、利人工判讀。

## Consequences

- **既有 8 列輸出值改變**,新舊 CCI Output 2 區間列不可比(需重跑分析)。視窗半徑、time-vs-index、標籤、`Time` 欄皆為可重訪參數。
- **ADR-0018「故無需 dedup」結論不變、理由更新**:原靠「取整段平均」避免與 muscle_ratio 的中點 dedup 糾纏;現靠 CCI 的**固定 32 列具名 schema**(S-D、C-D 永遠各自一列,即使值偶然相同也不合併)保證不重複。muscle_ratio 的 `appendIntervalMidpoints` dedup(`analyzer.go:425`)是因其列數動態、會與 adjacent-pair 重名 —— CCI 無此問題,不需移植 dedup。
- **測試要求(關鍵)**:既有 golden fixture 為線性斜坡(`phase_stats_test.go:34` `rampA[i]=float64(i)`)。線性下「任一對稱範圍平均 = 其中點值」,而整段 `[from,to]` 與視窗 `[mid−5,mid+5]` 共用同一中點 → **新舊 8 列數值全等**(S-C=15、S-D=20…O-L=65 不變)。換言之改算法在此 fixture 上**不可見**,線上更新 golden 只動 label/`HasTime`/`Time`、數值 assertion 照樣綠 = 假綠燈。實作**必須新增非線性 discriminating fixture**(例:單一尖峰落在整段範圍內但在中點 ±5 窗外 → 舊值非零、新值為 0),把值路徑差異鎖住。
- **邊界安全**:`[S−150ms, L+150ms]` 抽取範圍給中點窗 ≥15 sample 餘裕,8 個區間中點的 ±5 窗永不觸 `[0, n−1]` 邊界 clamp。

## Reversibility

中等。純重算(revert + 重跑分析),無資料遷移、無 schema 列數變動。視窗半徑、time/index 中點、標籤、`Time` 欄填法皆可重訪。`band50Half` 共用使「±50ms 到底幾點」單一真相,日後調取樣假設兩處同動。

## Related

- [[ADR-0018]] — 本案 **amends §3 需求 1**(區間=整段平均 → 中點±50ms);其餘(gait 重錨、範圍延伸、±25/前100/落地、compute-only seam)不變。
- [[ADR-0014]] — `WindowMean` 來源 + 視窗平均抗雜訊理由;本案把同款語意的 center 從點移到區間中點。
- [[ADR-0010]] — kernel 跨 feature 上提先例;`windowmean` 共用核沿用。
- [[ADR-0012]] — CCI(single-subject / compute-only)vs muscle_ratio(batch / compute+write)形狀分歧;本案維持 CCI compute-only,Output 2 仍由 GUI handler 經 CSVHandler 寫。
