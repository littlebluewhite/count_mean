# BackpressureController 整體拆除,admission gate 內聯回 MaxMeanCalculator

**Status**: accepted (2026-05-28)

## Decision

`internal/models/BackpressureController` 與 `BackpressureStats` 整體刪除;memory-aware admission gate 內聯回 `internal/calculator` package,以 ~25 行 helper(建議 `waitForMemoryCapacity(ctx) error`)收乾、留在 `MaxMeanCalculator` 內部。同時清理三層 dead surface:

1. `gui/app.go:1246-1252` `App.GetBackpressureStats` handler 與配套 panic recovery test(`gui/app_panic_recovery_test.go:87-89`、`gui/recover_test.go:384`);Wails auto-generated binding(`frontend/wailsjs/go/gui/App.js:45-46`、`App.d.ts:28`、`models.ts:658-663`)下次 build 自然消失。
2. `internal/calculator/maxmean.go:125-150` 兩個 wrapper getter(`GetBackpressureStats` / `getMemoryUsageInfo`),以及 `maxmean.go` 內 5 處 `if c.backpressureController != nil` 防禦性 nil-guard(lines 127、136、567-570、680-689、817-829)。
3. `internal/models/backpressure_test.go` 整檔(5 個 self-referential test:`GetStats_ConcurrentRead_NoRace`、`GetStats_DerivedFieldsComputed`、`ZeroValueSweep`、`PartialZeroValue`、`NormalizesNonpositiveInterval` — 全部測一個 production 永不觸發的 zero-value config + 已無 consumer 的 stats getter)。

memory threshold 與 worker 上限的 default 常數(`defaultMaxMemoryMB = 1024` / `defaultMemoryThreshold = 0.8` / `defaultThrottleThreshold = 0.9` / `CheckInterval = 100ms`)以 calculator package 內部常數形式落地;`defaultWorkerReductionFactor` 與 `defaultGCIntervalSeconds` 因 Wave 6 已刪 `adjustWorkers` 與 `maybeRunGC`,直接消失。

2026-05-28 architecture review 提出的 Candidate #3「BackpressureController seam 深化或刪除」**選 deletion 路線**;Option B(controller 接管 worker pool 補回 deep module)與 status-quo + janitorial 拒絕(見 Considered Options)。

## Why

- **Wave 6 已實質清空 controller 的 muscle,留下 anaemic skeleton。** 監控生命週期(`Start/Stop/monitor/adjustWorkers/maybeRunGC`)已刪、`BackpressureStats` 4 個 orphan fields(`PeakMemoryUsage / AverageWorkers / ThrottleEvents / GCTriggers`)已刪(`backpressure.go:55-64` 註解明說 Wave 6 清理)、dynamic worker adjustment 已死(`GetOptimalWorkerCount` 退化為 return 常值欄位)。剩下 7 個 public method 中 4 個整體可刪(`GetStats / RecordJobStart / RecordJobComplete / Reset` 配套 stats 與 job counters)、3 個內聯到唯一 caller(`WaitForCapacity` 到 `processJob`、`GetMemoryUsageInfo` 到 calculator 內部 logging helper、`GetOptimalWorkerCount` 直接用 `c.workerCount`)。

- **Deletion test 過。** 套用 deletion test 後,7 個 method 沒有任何 N-callsite 位移現象(對比 [[ADR-0005]] 拒絕 Candidate #2 的「14 callsite 位移」反例)。`BackpressureController` 真實 leverage 已被 Wave 6 收回上游,當前型別只是 anaemic wrapper round `runtime.MemStats` + `sync.Mutex` + 兩個 int counter。`processJob` 內聯 ~10-15 行 admission gate logic 後仍在 golangci-lint `funlen=100` 內。

- **5 個 dedicated test 是 self-referential debt。** `backpressure_test.go` 5 套全部測 controller 對外契約(zero-value normalize × 3、race-free GetStats × 2)。Production 唯一 caller `NewMaxMeanCalculator` 永遠走 `DefaultBackpressureConfig()`,zero-value path 從未被觸發;`GetStats` 經 `frontend/src/**` grep 確認零 consumer(Wails binding 是 auto-generated stub,frontend 端無 Vue/JS callsite)。test 鎖的契約在 production 不存在 — 刪除不掉 prod coverage,只 finalize 「surface 沒人用」這個事實。

- **跟既有 ADR 框架對位成立。** [[ADR-0004]] §Boundary 1 拒絕「business semantics 下沉到 wrong-shaped seam」;本 ADR 反方向但同源 — 拒絕「為了讓 anaemic type 看起來 deep 而保留 shallow seam」。[[ADR-0005]] 拒絕 Candidate #2 拔 `*FromRawData` 的論點是 deletion test 不過(14 callsite 位移);本 ADR 採同樣 deletion test、結論相反(整體消失,callsite 不多開)。兩條 ADR 形成「深度 honesty 雙語」:case-by-case 跑 deletion test,不為 collapse 而 collapse,也不為 symmetry 而 preserve。

## Considered Options

- **A. 整體拆除(chosen)** — 上述 9 條動作。優點:complexity 真實消失、self-referential test debt 一併收乾、dead stats wails route 清理。缺點:三 sibling(MaxMean / Normalizer / PhaseAnalyzer)在 memory profile handling 上不再對稱 — 但 sliding-window 累積記憶體壓力本來就是 MaxMean 專屬(Normalizer per-channel reference division、PhaseAnalyzer per-phase O(1) mean 都無跨 window 累積),不對稱本來就是事實,Option A 只是明面化。
- **B. Controller 接管 worker pool 所有權,補回 deep module** — 把 orchestrator 目前的 goroutine pool 邏輯(`maxmean.go:596-626`)搬進 controller,讓 controller 變 deep。拒:現有 orchestrator pool 邏輯擺位合理(calculator-local,沒有跨 module 痛點);Option B 為了「讓某個 type 看起來 deep」而搬家既有合理 code,跟 [[ADR-0004]] §Boundary 1 拒絕的 over-collapse 原則同源(方向反過來)。額外:Option B 後 controller 與 calculator 變雙向依賴(calculator 持 controller,controller 持 calculator 的 job channel)— 比目前 anaemic 還糟。
- **C. Status-quo + janitorial(只清 dead consumer surface)** — 刪 `App.GetBackpressureStats` wails route + 5 處 nil-guard,保留 controller 內部。拒:留下 anaemic type + 5 個 self-referential test,未來 reviewer 還會重新挖出來 grill 一遍;Wave 6 已啟動 collapse 方向,janitorial 只是 finalize 一半。

## Reversibility

中 — controller 可重建,但本 ADR 鎖住「重建應做為 fresh deepening」而非「restore from saved seam」。三條 memory threshold default + admission gate ~10-15 行邏輯可從 git blame 重生,但更務實的場景是:**若未來真要做 backpressure telemetry,從 fresh deepening 開始(可能伴隨 OpenTelemetry / 正式 metric exporter stack 等)** — 成本跟現在保留 anaemic controller 等於零,且新形狀會更貼合屆時的 observability 需求,而非 2024-2026 殘留的 internal-only mutex-counter 形狀。

## Related

- [[ADR-0004]] §Boundary 1(sticky-success 不下沉到 CSVHandler)— **反方向同源**:同樣的「不為 collapse 而 collapse」深度 honesty 原則,反方向(本 ADR 是「不為 preserve 而 preserve」)。
- [[ADR-0005]](calculator family 拒拔 `*FromRawData`)— **同樣 deletion test、結論相反**:Candidate #2 失敗(14 callsite 位移),Candidate #3 通過(complexity 真實消失)。兩條合讀防止 reviewer 把任一 ADR 誤讀為「總是 collapse」或「總是 preserve」。
- 既有 `BackpressureController` 註解(`backpressure.go:151-154` Wave 6 cleanup note)— 本 ADR 是這條註解所啟動的 collapse 方向的 finalize。

## Process note — framing-mismatch finding(防未來 reviewer 重蹈)

2026-05-28 grilling session 開場時發現 candidate handoff 與 HTML architecture review report 有四處 framing 與 code 對不上,記錄於此防未來 reviewer 重新走一遍同樣 surprise:

1. **「BackpressureStats still ships 4 orphan fields」** — 失準。Wave 6 已刪這 4 fields(`backpressure.go:55-64`),struct 現在只剩 2 個 healthy field(`TotalProcessingTime` / `ThroughputJobsPerSec`)。Report 把 Wave 6 已完成的清理當作「待辦」報告。
2. **「Downstream readers may display zeros as real data」** — 失準。Frontend `src/**` grep 對 `GetBackpressureStats` 與 `BackpressureStats` 零 hit;只有 `frontend/wailsjs/go/*` auto-generated binding 命中,無實際 Vue/JS consumer。
3. **「Sequencing with Candidate #2」** — moot。[[ADR-0005]](2026-05-28)已 reject Candidate #2,#3 sequencing 問題無對象。
4. **ADR 編號** — handoff exit criteria 寫 `docs/adr/0005-*.md`,但 0005 已被 calculator family ADR 佔走,本 ADR 落到 0006。
5. **`internal/calculator/maxmean_invariants_test.go` 3 處 `calc.backpressureController.ActiveJobs()` + 2 處 `Reset()` 是 impl 開工後才被發現的漏網 caller(grep 補完於 2026-05-28 impl session)。** 三個 test 的 framing 補完:
   - `TestCalculate_PreCancelledContext_DoesNotStartWorkers` — production contract 仍有效(`execute()` 第一行 `ctx.Err()` explicit 守護),改寫:保留 test,移除 `ActiveJobs() == 0` 那行 assertion,只靠 `errors.Is(err, context.Canceled)` 表達契約。
   - `TestProcessJob_PanicResetsActiveJobs` — subject (= `defer RecordJobComplete` 對稱性) 在 collapse 後消失,直接刪除整 test。production 端的 panic-safe goroutine 行為由 `worker` 的 `wg.Done` defer 保證,不需單獨測 `processJob` 層的 counter 對稱。
   - `TestProcessJob_PreCancelledCtx_NoCountLeak` — 同上,subject 消失,直接刪除。
6. **`gui/recover.go:149` docstring example 列 `GetBackpressureStats (struct)`** — handler 刪除後 example 失效。改成 `GetTranslations (map)` 展示 3 種泛型 T 用法(string / pointer / map),依然涵蓋 generic 用法多樣性。

Lesson:future architecture review 在生成 HTML report 之前,Explore subagent 對「已完成清理」與「待辦清理」應以 git log / 註解時序為準,而非僅看當前 struct shape;framing-mismatch 開場是 [[memory:feedback_cross_check_report_vs_code]] 的紀律,本 ADR 是該紀律的 case study。Impl-time grep 補完(point 5/6)說明即使 grilling 階段做了 cross-check,真實 impl 仍會挖到 1-2 個額外 caller — `superpowers:executing-plans` 對「STOP + 補完 process note」的指示是必要,而非過度防禦。
