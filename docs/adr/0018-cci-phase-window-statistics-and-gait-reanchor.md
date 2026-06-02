# CCI 重構:gait cycle 重錨 S→L + 落地延伸(>100%)+ 分期視窗統計第二輸出

**Status**: implemented (2026-06-03) — accepted design 已落地;見下方 Implementation notes

## Decision

把既有 CCI 分析從「單一曲線輸出」重構成「曲線 + 分期視窗統計」雙輸出,並重新定義 gait cycle 的錨點。四項一併鎖定:

### 1. Gait cycle 重錨:0% = S、100% = L(取代 0% = min(所有分期點) = P0)

- `calculateGaitCycle` 的 `gaitStart` 從「所有已標定分期點的最小 EMG 時間」改為 **S 的 EMG 時間**;`gaitEnd` 維持 L(原本是 max,L 本就是最後一點)。`duration = L − S`。
- **S 與 L 變成必填 anchor**:任一缺漏 → 整個 CCI 分析 fail-fast(明確訊息「0%=S 模型需要 S 與 L」)。比既有「任 2 點即可」嚴格。
- **P0/P1/P2 退出 CCI**:它們換算成新百分比為負值(SF8: P0=−57%、P1=−37%、P2=−13.5%),落在顯示範圍(見 §2)之外,不再渲染 phase marker、不出現在 Output 1 CSV。manifest 仍保留這些欄,phase_sync 等其他功能不受影響。
- **既有 CCI 的百分比軸/CSV/chart 數值改變**(分母 L−P0 → L−S),相關測試需更新。此回歸為選擇 0%=S 的必然代價,已接受。

### 2. 範圍延伸:[S − 150ms, min(EMG末筆, L + 150ms)],落地尾段以 >100% 呈現

- CCI 計算/顯示範圍從 `[gaitStart, gaitEnd]` 改為 `[S − 150ms, min(emgEnd, L + 150ms)]`,Output 1 與 Output 2 共用此範圍。
- 因 `pct = (t − S) / (L − S) × 100` 公式**無 clamp**,延伸範圍自然產生 <0%(引段)與 >100%(落地尾段)。SF8 實算:圖表從 −11% 畫到 ~111%,S=0%、L=100% 仍是錨點。
- CCI Rudolph 為**逐點計算**(`CalculateCCIRudolph(emg1[i], emg2[i])` 只看第 i 個 sample),故 [S, L] 區間內既有值**位元不變**,只是前後各接一段。
- 150ms margin 的用途:後段覆蓋需求 4 的 L+100ms 視窗 + 50ms 餘裕;前段覆蓋需求 3 的 S−100ms 回看 + 50ms 餘裕。短於此範圍的錄製 clamp 到 EMG 末筆。

### 3. 第二輸出 Output 2:`{safeSubject}_CCI_Rudolph_phases.csv`(分期視窗統計,32 指標 × 12 對)

對 7 個分期點(S,C,D,T0,T,O,L)與 8 個分期區間,輸出 12 對 CCI 的視窗平均。四組需求:

- **需求 1 — 區間整段平均(8 列)**:`S-C, S-D, C-D, D-T, D-T0, T0-T, T-O, O-L`。每區間取 `[idx(起), idx(迄)]` 整段索引範圍的簡單平均。`S-D` 跳過 C、`D-T` 跳過 T0(沿用 muscle_ratio `biomechanicalIntervalMidpoints` 的生物力學定義,但此處取整段平均而非中點值,故無需 dedup)。
- **需求 2 — 各分期點前後(14 列)**:每點兩個指標。
  - `±50ms` = centered 11 點 `MeanRange[i−5, i+5]`。
  - `±25ms` = 加權 7 點:`A = mean(CCI[i−4], CCI[i−3])`、中段 `CCI[i−2..i+2]`(5 點)、`B = mean(CCI[i+3], CCI[i+4])`,取 `mean(A, i−2, i−1, i, i+1, i+2, B)`(7 個量的平均;外圍 i±3/i±4 各降半權)。
- **需求 3 — 各分期點前 100ms(7 列)**:`前100ms` = trailing 11 點 `MeanRange[i−10, i]`。
- **需求 4 — L 落地後初期穩定(3 列)**:`落地0~100ms = MeanRange[i, i+10]`、`落地20~50ms = MeanRange[i+2, i+5]`、`落地50~100ms = MeanRange[i+5, i+10]`。

其中 `i = FindNearestTimeIndex(序列時間, 分期點 EMG 時間)`;D/O 經 `MotionIndexToEMGTime` 換算後再取最近索引。

**版面**:rows = 32 項目(照需求 1→2→3→4 順序)、cols = `項目, 指標, Time (s), <12 對 CCI>`。`Time (s)` = 點視窗類填該分期點 EMG 時間、區間類留空。值 `%.6f`、時間 `%.4f`、帶 BOM。pair 欄名與 Output 1 一致(`RA/ES … TAIO/GMax`)。

**缺漏政策**:S/L 必填(見 §1);中間點(C/D/T0/T/O)缺漏 → **固定 32 列、該列值留空**(非如 muscle_ratio 變動列數,因本表需跨 subject 對齊比對);跨點區間(S-D / D-T)只要自身兩端在即照算。

**取樣率**:視窗以**點數**定義,ms 標籤假設 100Hz(0.01s/點);非 100Hz 時點數視窗照算、ms 標籤名目化,並 log 一筆 warning(沿用 `estimateSampleInterval` 偵測)。邊界 clamp 到 `[0, n−1]` + skip 非有限值(沿用 ADR-0014 `WindowMean` 寬容語意)。

### 4. 放置:MeanRange kernel 上提共用 + CCI 維持 compute-only

- 新增共用套件 `internal/windowmean`,持 `MeanRange(series, lo, hi)`(clamp + skip 非有限值)。muscle_ratio 既有 `WindowMean(s, c, h)` 改為 `MeanRange(s, c−h, c+h)` 的 wrapper、re-point import。`±25ms` 加權 7 點為 CCI 專屬,留 `internal/cci`。
- analyzer 算出 Output 2 資料 → `CCIAnalysisResult.PhaseStats`(新欄位);GUI `AnalyzeCCI` handler 在 `Run` 之後(比照 chart 生成位置)呼叫新的 `CSVHandler.WriteCCIPhasesResult` 寫 Output 2。**維持 CCI compute-only 形狀**(ADR-0004 Boundary 2,與 muscle_ratio 的 compute+write 刻意分歧,ADR-0012),CSV 寫入仍全走 CSVHandler(ADR-0001/0004 不變式)。
- GUI:`CCIResult` DTO 加 `OutputPhasesPath`,結果區多顯示一行路徑;**不加新按鈕、不在畫面顯示統計表**(只寫 CSV)。Output 2 每次 CCI 分析都產生(非 opt-in)。

## Why

- **讀既有 CCI output CSV 做不到需求 4。** CCI CSV 裁切在 `gaitEnd = L`,L 之後零資料點(SF8 實證:CCI CSV 止於 gait 99.58% ≈ L);只有原始 EMG(SF8 到 17.74s、L 後 ~570 點)有落地後資料。故採「比照 muscle_ratio 讀原始 EMG、用既有 `CalculateCCIRudolph` kernel 重算 CCI」而非讀已算好的 CSV。逐點計算保證 [S, L] 內值與既有 CCI 完全一致,只是能延伸過 L。
- **0% = S 是生物力學取捨。** 跳躍 gait cycle 從啟動瞬間(S)到落地(L);P0/P1/P2 是啟動**前**的準備點,不屬動作本體。錨點固定在 S/L 使 [0,100%] 含義穩定;落地自然落在 >100%,且 `pct` 公式本就無 clamp,>100% 免特例。代價:既有 CCI 百分比全變、新舊結果不可比 — 明確接受。
- **視窗平均比單點抗雜訊。** 同 ADR-0014 對 muscle_ratio 的理由:100Hz 下 11 點 ≈ 110ms 局部平均給分期點更穩健的代表值。`±25ms` 的加權 7 點是 user 指定的中心加權形狀(外圍 i±3/i±4 降半權)。
- **MeanRange 上提是兌現 ADR-0014 的預留。** ADR-0014 Reversibility 已明文「日後若 CCI 要同款局部平均可上提共用(比照 ADR-0010 LTTB kernel)」。`MeanRange` 是比 `WindowMean` 更基本的 primitive(後者是前者特例),上提避免 clamp+skip 邏輯兩份漂移。
- **compute-only seam 維持 CCI 形狀。** Output 2 比照既有 chart 在 Run 後寫,不破壞 `AnalysisHandler` 的單-WriteCSV 抽象,也維持 ADR-0012 立下的「CCI compute-only vs muscle_ratio compute+write」分歧。

## Considered Options

- **讀 CCI output CSV(字面需求)** — 拒:需求 4 無資料(L 後被裁掉),且 CSV 無 phase marker、仍需 manifest + 時間換算定位分期點,複雜度不低於重算。
- **0% = P0(零回歸)** — 拒:user 要跳躍 cycle 錨在 S;P0 錨點下 S 落在 ~36%、落地語意混亂,且不符 user 心智模型。
- **gait cycle 不延伸、需求 4 另尋資料源** — 拒:雙來源(CCI CSV + 原始 EMG)的值可能不一致,維護兩條真相。
- **改 CCI 讓既有 Output 1 重算 0-100% 含落地(L 不再 = 100%)** — 拒:user 採「L 仍 = 100%、落地以 >100% 表示」更乾淨(錨點不動、既有 [0,100] 值的相對關係保留)。
- **Output 2 在 analyzer 內寫檔(compute+write,比照 muscle_ratio)** — 拒:破壞 CCI compute-only 形狀(ADR-0012 分歧)。
- **MeanRange 留 CCI 自有一份** — 評估後採上提:ADR-0014 已預告、duplication 風險實在。

## Implementation notes (2026-06-03)

落地於 feature branch `worktree-feat+adr0018-cci-phase-stats`(Phase A–F)。對應實作:`internal/windowmean`(`MeanRange`/`WindowMean` kernel + muscle_ratio re-point)、`internal/cci/weighted25.go`(±25ms 加權核 + G1c 判別 golden)、`internal/cci/phase_stats.go`(32 列 `PhaseStats` builder)、`internal/io` `WriteCCIPhasesResult`(Output 2 writer)、`gui/cci_handlers.go` + `frontend/src/panels/cci_spec.mjs`(接線 + 路徑顯示)、i18n `KeyErrorCCIMissingSLAnchor` + `result.label.output_phases`(×4 語)。

**SF8 端到端實證**:`gaitStart=10.633`(S=0%)、`gaitEnd=11.999`(L=100%);Output 1 首尾 pct = **−10.47% / 110.32%**(引段 / 落地,印證 §2 延伸);Output 2 = **32 列 × 12 對 + BOM**,Time 欄點列填值、區間列留空。

**§3 缺漏政策的延伸(codex review 強化,原設計未涵蓋)— present-but-out-of-range 分期點:** 固定 `[S−150ms, L+150ms]` 範圍下,畸形 manifest 的中間點(C/D/T0/T/O typo)可能落在範圍外,而 `FindNearestTimeIndex` 會 clamp 到邊界索引而非報錯。此類點現一律**視為缺漏**:從 `PhaseTimes`/`PhasePercents` 移除(不渲染 marker)、Output 2 該列留空 — 與「中間點缺漏留空」語意一致,且 Output 2 與 chart marker 保持一致。範圍判定用 **1e-6 容差**對齊 `validateEMGBounds`,使被容忍的 S/L ULP 飄移仍視為 present;S/L 恆界定範圍故永不觸發。

## Reversibility

**高成本回退。** gait cycle 重錨改動既有 CCI 全部輸出語意,新舊 CCI 結果不可比(回退需重跑所有既有分析)。範圍延伸量(150ms)、`±25ms` 加權形狀、各視窗大小、檔名皆為可重訪參數。`MeanRange` 上提為純函式搬移,可逆。第二輸出本身為純加值(不回退既有 Output 1 行為,除了 §1/§2 的錨點/範圍改動)。

## Related

- [[ADR-0001]] — CSV 寫入走 CSVHandler 守門;`WriteCCIPhasesResult` 沿用。
- [[ADR-0004]] — CCI compute-only(GUI handler 寫 CSV);Output 2 維持此 Boundary 2 形狀。
- [[ADR-0005]] — calculator kernel 層;`MeanRange` / `±25ms` 落於此層。
- [[ADR-0010]] — kernel 跨 feature 上提先例(LTTB);`MeanRange` 比照。
- [[ADR-0012]] — CCI(single-subject / compute-only)vs muscle_ratio(batch / compute+write)形狀分歧;本改動維持 CCI 形狀。
- [[ADR-0014]] — `WindowMean` 來源 + 視窗平均抗雜訊理由 + 預告 kernel 上提;本 ADR 兌現上提並把同款語意套到 CCI。
