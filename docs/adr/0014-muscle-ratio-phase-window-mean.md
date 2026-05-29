# muscle_ratio Output 2 的 phase-point 值改為 11-sample window mean — 平均計算上游化、CSVHandler 維持純 layout

**Status**: accepted (2026-05-29) — design-only,impl 待 handoff

## Decision

`internal/muscle_ratio` Output 2(`_muscle_ratio_phases.csv`)每個 phase / midpoint 列的 4 個比值,從**「最接近該時間的單一 sample 值」**改為**「以該 sample 為中心、`[idx-5, idx+5]` 共 11 筆的平均」**。四項語意與一項放置決策一併鎖定:

1. **平均對象 = 比值的平均(mean-of-ratios)**:`mean(ratio_i)`,直接平均 Output 1(`ComputeAllRatios`,calculator.go:150)那一欄已算好的比值,**不**改 ratio 計算路徑。
2. **邊界 = 裁切(clamp)**:視窗取 `[idx-5, idx+5] ∩ [0, n-1]`,靠錄製頭/尾時用較少筆數,永遠產出值。
3. **非有限值(NaN / Inf)= 跳過**:分母 = 有效(finite)格數;整窗皆非有限 → 空格(與現有單點版對 NaN/Inf 寫空格一致)。**impl 修正(codex review)**:原設計誤判 `Ratio()` 只會回 finite/NaN,故僅 skip NaN;但 `Ratio()` 對極小非零分母會溢位成 `+Inf`,若不跳過則單一 Inf 樣本會把整個 11 點平均變 `Inf`→空格、抹掉其餘有效樣本。改為 skip 全部非有限值,與 Output 1「Inf 視為無效 cell」對齊。
4. **Time (s) 欄不變**:仍顯示中心 sample 時間 `emg.Time[idx]`。
5. **放置 = 上游 Analyzer + 純 kernel**:新增 `muscle_ratio.WindowMean(series []float64, center, half int) float64`(內含 clamp + skip 非有限值);[[Domain analyzer]] `Analyze` 用 `synchronizer.FindNearestTimeIndex`(time_sync.go:161)算 idx、呼叫 kernel 得每點每比值的平均,透過 payload 把**算好的值**往下傳;[[Format-aware write]] `WriteMuscleRatioOutputPhases`(csv_handler.go:996)退回純 layout、只 emit。handler 內 `nearestTimeIndex`(csv_handler.go:1062,僅此一處 caller)隨改動成孤兒,刪除。

命名同步變更(語意從「點值」變「11 點平均」,且 path 經 `outputPhasePath` 對使用者可見):

- 檔名 `{safeSubject}_muscle_ratio_phases.csv` → `{safeSubject}_muscle_ratio_phases_avg11.csv`
- 比值欄名 `RA/ES` → `RA/ES (11pt avg)`(其餘三欄同;沿用既有 `Time (s)` 括號標註風格);`Phase` / `Time (s)` 欄不變
- i18n 面板描述 `KeyPanelMuscleRatioDesc`(zh-TW / zh-CN / en-US / ja-JP 四語言)更新成 11 點平均語意

## Why

- **單點抽樣對雜訊敏感。** Output 1 取樣為 100Hz(0.01s/筆),11 筆 ≈ 110ms 的局部平均,給 phase-point 一個比單一 sample 更穩健的代表值。視窗大小 11 是使用者指定。
- **放置上游化是把 [[ADR-0004]] 的邊界套用到新 aggregation。** ADR-0004 立下「[[Format-aware write]] 吸 row layout,**不**吸 business semantics 與 math」。handler 既有的 `nearestTimeIndex` 回答「**哪一格** source 對應這個輸出列」(row→source 映射,layout-adjacent),`formatMuscleRatioCell(p.Ratios[k], idx)`(csv_handler.go:1031)是**純取一格**;但「11 格 skip-NaN 平均」回答「這格的**值是什麼**」——是 aggregation 計算,跨過 pick→compute 的線。按 ADR-0004 自己的原則,該往 Analyzer / calculator kernel 搬(math 下放 kernel,見 [[Domain analyzer]] / [[ADR-0005]])。反向(塞進 handler)只省約 10 行,卻違背本 feature 自己 ADR 立的邊界。
- **mean-of-ratios 而非 ratio-of-means,是接受離群值風險的明確取捨。** A 最符合「那個點的值的平均」字面意思、與 Output 1 同源、且不需改資料流。已知代價:EMG 經整流+RMS 後分母(如 ES)偶爾逼近 0,使單筆比值爆成很大的有限值,在 A 裡會綁架整個平均;B(`mean(num_i)/mean(den_i)`)先平均分母較穩健,但需把原始分子/分母通道值往下傳、改 payload 與計算路徑。為「平均那個點的值」這個需求,B 的穩健性不值那個複雜度——接受 A 的離群風險。(注:分母恰為 0 → NaN;大分子/極小非零分母 → 可能溢位 `+Inf`;兩者皆由 skip 非有限值處理,見 Decision 3。此處接受的離群風險僅指**有限**大值仍綁架平均。)
- **clamp 與 skip-NaN 對齊既有寬容語意。** `collectPhasePoints`(analyzer.go:292)本就靜默跳過缺漏 phase、clamp 越界,partial-success batch 哲學一貫;邊界 require-full-11 會新拒「合法但靠邊」的 phase 點,NaN poison-whole-cell 會讓單一零分母打掉整個平均點——兩者都比現狀更脆,不採。
- **改名安全。** 全 repo **無任何程式碼 READ** `_muscle_ratio_phases.csv`(唯一引用是寫檔端);Chart Composer 讀的是 manifest `MuscleRatioFile` 指向的 output1 形態檔,不碰 Output 2。故 rename 不打斷任何消費者,只更新 user-facing 路徑。

## Considered Options

- **放置:in-handler inline(min diff)** — 在 `WriteMuscleRatioOutputPhases` 內緊著 `nearestTimeIndex` 加 windowMean 取代單點 pick,~10 行、不動 payload 契約。**拒**:把 aggregation 塞進寫檔層,違反 [[ADR-0004]]「CSVHandler 不吸 math」,反而需要一條 ADR 去解釋這個例外。
- **放置:上游 Analyzer + 純 kernel(本次採)** — math 下放 kernel、Analyzer orchestrate、handler 純 layout。改動面較大(payload 契約 + analyzer + handler)但不破界。
- **平均對象:B ratio-of-means** — 對近零分母穩健。**拒**:需把原始通道值往下傳、改 payload 與 compute 路徑,且偏離「平均那個點的(比)值」的字面需求。
- **邊界:require-full-11(湊不滿即 warn 跳過 Output 2)** — **拒**:新拒合法但靠邊的 phase 點,比現狀脆。
- **NaN:任一 NaN → 整格作廢** — **拒**:單筆零分母打掉整個平均點。

## Reversibility

中 — payload 契約變更(`MuscleRatioOutputPhasesPayload` / `MuscleRatioPhasePoint` 改帶算好的平均值,不再帶 `Times`/`Ratios` 供 handler reach-in)需重新串接才能回退;視窗大小 11、mean-of-ratios、clamp/skip-NaN 皆為可重訪的參數/語意。`WindowMean` kernel 為純函式,日後若 CCI / Chart Composer 要同款局部平均可上提共用(比照 [[ADR-0010]] LTTB kernel)。檔名變更對既有產出是單向(舊 `_muscle_ratio_phases.csv` 留為 stale artifact,不自動清理)。

## Related

- [[ADR-0004]] — 本 ADR 把其「Format-aware write 吸 layout 不吸 math」邊界套用到新增的 window-mean aggregation:計算留上游、handler 純 emit。Output 2 sticky-success 語意(Boundary 1)不受影響,仍在 Analyzer。
- [[ADR-0005]] — calculator family 是被 [[Domain analyzer]] 委派的 math kernel 層;`WindowMean` 即落在此層的 muscle_ratio 套件內。
- [[ADR-0010]] — 同款「純計算 kernel」先例(LTTB downsampling);`WindowMean` 若未來跨 feature 共用可比照上提。
- [[ADR-0012]] — muscle_ratio 屬 batch + compute+write 形狀;本改動維持該形狀,只深化 Output 2 的值語意與層歸屬。
