# 深化 FindNearestTimeIndex → ResolveTimeIndex:in-range 升為介面 + 統一 EMG-time epsilon

**Status**: accepted · **implemented** (2026-06-19)

`synchronizer.FindNearestTimeIndex(times, target) int` 對**越界** target 靜默 clamp 到首/末 index。裸 `int` 把「真正最近的 sample」與「因越界被 clamp 到邊界」混為一談 —— in-range 這個前提是模組真實介面的一部分,卻不在型別介面裡。後果:兩個 caller(CCI、muscle_ratio)各自從一句**註解**重新推導 bounds 檢查,in-range 偵測散在 3 處。本 ADR 把 in-range 升格為介面的一部分,並把原本 CCI 私有的 `1e-6` EMG-time 容差升格為共享 seam 契約。承 [[ADR-0029]](它刻意保留了這個 live 函式)。

## Decision

### 1. Seam 深化:`FindNearestTimeIndex → ResolveTimeIndex`

`internal/synchronizer/time_sync.go`:函式改名並改簽章成 `ResolveTimeIndex(times []float64, target float64) (idx int, inRange bool)`。新增私有常數 `emgTimeEpsilon = 1e-6`。

- `inRange` 在 empty 檢查後**算一次**(`target >= times[0]-emgTimeEpsilon && target <= times[len-1]+emgTimeEpsilon`),所有 return 路徑都帶它。
- `idx` 的計算邏輯(clamp short-circuit + binary search + left/right tie-break)**一字不改**;越界時 idx 仍 clamp 到邊界索引(0 / len-1)。
- `idx` 與 `inRange` 正交:idx 的越界 clamp 是零成本保留舊行為,目前無 caller 在 `inRange=false` 時消費 idx —— 所有 caller 一律先看 `inRange`。

### 2. in-range 偵測 3→1(deletion test)

偵測前住 3 處:(a) `FindNearestTimeIndex` 靜默 clamp、(b) CCI `phaseTimeWithinExtractedRange`(2 使用點)、(c) muscle_ratio inline bounds loop。後住 1 處:`ResolveTimeIndex` 的 `inRange`。

- **CCI**(`internal/cci/phase_stats.go`):**刪除** `phaseTimeWithinExtractedRange`;`phaseAt` 與 `dropOutOfRangePhases` 兩處改呼叫 seam 的 `inRange`;`computeRow` 中點 `mid, _ := ResolveTimeIndex(...)`(凸性保證 inRange,丟棄)。**輸出 byte-identical** —— 舊 helper 與 seam 是等價謂詞(同 `1e-6`、同比較式、同邊界)。
- **muscle_ratio**(`internal/muscle_ratio/analyzer.go`):`collectPhasePoints` 的 inline strict bounds loop 改用 seam 的 `inRange`(all-or-nothing 越界政策不變、錯誤訊息參數一字不變);`buildPhasePoints` 的 `idx, _ := ResolveTimeIndex(...)`(points 已驗證/凸中點 → inRange 必 true,丟棄)。

caller 殘留只剩 drop-vs-skip 的 domain policy([[ADR-0012]] 要求其分歧):CCI 逐點 drop+warn、muscle_ratio 整批 skip Output 2。複雜度**集中**(binary search + clamp + epsilon 在一個 seam、一張表測),非位移。

### 3. epsilon 統一(數值衛生,非 domain policy)

把 CCI 私有的 `1e-6`(原 `phaseTimeWithinExtractedRange` 的 `boundsEpsilon`,源頭對齊 `validateEMGBounds`)升格為 seam 常數 `emgTimeEpsilon`,muscle_ratio 自動承接同一容差。

## Why

- **in-range 屬於介面。** 函式的真實契約是「最近 sample + 該 target 是否真的在範圍內」;舊裸 `int` 讓 caller 必須從註解重建後半段,且容易誤用越界 clamp 值。把 `inRange` 放進回傳值,介面說真話、誤用面收斂。
- **epsilon 是數值衛生,可統一。** CCI 的 `1e-6` 明文吸收 **force-plate↔EMG 同步的 ~1e-7 ULP 飄移**(原 `cci/analyzer.go` 的 `validateEMGBounds` 校準理由)。muscle_ratio 的 phase 點走**同一套** `ForceTimeToEMGTime` 同步,卻用 strict-0 —— 它的 strictness 是 latent 不一致(原始 commit `21e370c` 寫 strict 在前,CCI 的 epsilon 是後來 [[ADR-0018]] §3 才加),**非刻意政策**。因此 epsilon 不是 [[ADR-0012]] 意義下的 domain policy(那是 per-phase drop vs all-or-nothing skip),而是兩 caller 該共享的浮點邊界衛生。
- **Deletion test 強。** `phaseTimeWithinExtractedRange` 整刪、muscle_ratio inline loop 塌成一次呼叫、舊 clamp 行為成為文件化契約;偵測邏輯真實集中而非位移。
- **承 [[ADR-0029]]。** 0029 在死碼清除批次中**刻意保留** `FindNearestTimeIndex`(本體 live);本案承接該保留、把它深化成自我說明的介面。

## Considered Options

- **A. 統一 epsilon(chosen)。** seam 烤入 `1e-6`,兩 caller 共享。優點:消除 latent 不一致、in-range 升介面、3→1。缺點:muscle_ratio 獲得一筆**有意的行為變更**(見行為帳本),已誠實切開、有 regression test 釘住。
- **B. 介面深化但保留 muscle_ratio strict-0。** 拒:seam 仍需暴露兩種容差或讓 caller 自行再判,等於把剛集中的偵測又拆開;且 strict-0 本身是 latent bug(同步飄移會誤拒合法 phase),保留它是固化錯誤偏置。
- **C. 維持裸 `int` 只做改名。** 拒:不解決核心問題(in-range 不在介面、偵測散 3 處),純改名是位移非深化。
- **介面形狀 `(idx, inRange)` tuple vs sentinel idx(如 -1)。** 選 tuple:sentinel 會與既有「empty → -1」語意打架,且 caller 仍須記得檢查 sentinel;tuple 讓「先看 inRange」成為型別層的提示。(Design-It-Twice + grilling 已跑完,user 選 Option A。)

## 行為帳本(誠實切開 — 一筆變更)

| Caller | 變更前 | 變更後 | 淨效果 |
|---|---|---|---|
| CCI | `phaseTimeWithinExtractedRange` 用 `1e-6` | seam 用 `1e-6` | **byte-identical** |
| muscle_ratio | strict-0:`p.time < emgStart \|\| > emgEnd` | seam `1e-6` 容差 | 邊界 `1e-6` 內的合法 phase 從「拒收+跳 Output 2」變「接受+snap 到邊界」。真實 typo(≥1ms ≈ 1e-3)照擋、all-or-nothing 政策不變 |

這筆 muscle_ratio 變更是**獨立、有意**的,不藏在「純 refactor」措辭下([[memory:feedback_pr_body_verification_integrity]])。`TestAnalyze_PhaseTimeWithinEpsilon` 釘住其接線(phase 落在 `(emgEnd, emgEnd+1e-6)` 開區間 → Output 2 應產出);mutation(`emgTimeEpsilon → 0`)經實測精準擊倒此測試 + CCI `ToleratedBoundaryDriftStaysPresent` + synchronizer 4 格 epsilon 邊界測試,證明容差跨三 pkg load-bearing。

## Consequences

- **CONTEXT.md 不動**:`ResolveTimeIndex` 是 implementation 細節非 domain 詞彙([[ADR-0029]] 先例)。
- **i18n 不動**:muscle_ratio 越界錯誤 key/訊息不變,只是觸發門檻從 strict-0 放寬到 `1e-6`。
- **歷史 ADR 不回改**:[[ADR-0014]]/[[ADR-0018]]/[[ADR-0022]]/[[ADR-0029]] 字面引用舊名 `FindNearestTimeIndex`,是 immutable 歷史,留著。`ResolveTimeIndex` 的 docstring 保留一句對舊名的溯源(說明 idx clamp 行為的來歷)。
- **承 [[ADR-0018]] §3**:CCI「present-but-out-of-range 分期點視為缺漏」政策不變;本案把 0018 §3 對齊 `validateEMGBounds` 的 `1e-6` 容差**升格為共享 seam 契約**。
- **正交 [[ADR-0018]]/[[ADR-0022]] 的 window 數學**:32 列表、中點±50ms 視窗、landing 視窗不受影響(只換 index 解析的呼叫形狀,idx 值不變)。

## Reversibility

低成本回復(git revert)。若未來需要把 muscle_ratio 改回 strict-0(例如證實某資料源不該容忍任何邊界飄移),改的是 caller 端政策、非 seam —— 但本 ADR 鎖住「epsilon 是兩 caller 共享的數值衛生」這個判斷,推翻它需要新證據證明兩者的同步來源其實不同。

## Related

- [[ADR-0029]] —— **承**:0029 刻意保留 live 的 `FindNearestTimeIndex`,本案深化它。
- [[ADR-0018]] —— **承其 §3**:CCI present-but-out-of-range 缺漏政策 + `1e-6` 容差源頭;本案升格為 seam 契約。
- [[ADR-0012]] —— **守**:CCI drop-vs-warn 與 muscle_ratio all-or-nothing-skip 的越界**政策**分歧保留,本案只統一容差(數值軸)不動政策軸。
- [[ADR-0022]] —— **正交**:CCI 中點±50ms 視窗數學不受影響,只換中點 index 的解析呼叫。

## Notes

- 本 ADR 屬深化型重構(deepening),非死碼移除。介面形狀經 Design-It-Twice(3 sub-agent)+ grilling 鎖定為 `(idx, inRange)` tuple、user 選 Option A(統一 epsilon)。計畫經 codex×2 加固(R1 機械精確性 / R2 設計風險,0 P1/P2 設計問題)。
- **實作**:subagent-driven(seam / CCI / muscle_ratio impl=sonnet 分波;Wave1 seam 為阻塞前置、Wave2 兩 caller 平行),整合 main agent 統合 + opus 唯讀對抗 review(自驗 byte-identical 謂詞等價、行為變更 revert-fail、兩處 `_` 丟棄之 in-range 安全性)+ 實機 mutation 自驗。
- **驗證**:`go test` 三 pkg → `make test`(unit+integration)→ `make test-race` → `make lint`(cache clean 後 0 issues;跨 worktree 陳舊快取曾假紅)→ `GOOS=linux/windows go build ./...` 全綠。**GUI smoke 不需要**(純後端 compute,未動 GUI/RPC/frontend)。
