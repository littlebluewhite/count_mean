# CCI 結果持有 gait % 與不變式:`CCIAnalysisResult.Validate` / `GaitPercent`

**Status**: accepted · **implemented** (2026-10-11)

`CCIAnalysisResult` 有三條不變式:gait 區間有限且 `duration > 0`、每個 `PairResults[i].Values` 長度等於 `len(TimeValues)`、結果非 nil。這三條原本散在四處各自檢查或各自不檢查:

- `chart.go` 的 `validateCCIResultForChart` 檢查全部三條;
- `downsampleCCIResult` 又重檢一次 pair 長度(入口已檢,這是死的 defense-in-depth),並有兩個直接子測試單獨驗它;
- `io.WriteCCIResult` 只檢查 nil 與 `duration <= 0`,**沒有擋 NaN / Inf**(NaN 的 `<= 0` 恆為 false),NaN 的 gait 時間會寫出整欄 `NaN` 的 `Gait Cycle (%)`;對短 pair 另有「補空字串」的容忍分支;
- `computeCCI` 出口完全不檢查。

百分比公式 `(t - start) / duration * 100` 也在 `calculateGaitCycle`(clamp 版)、`WriteCCIResult`(CSV,`%.2f`)、`buildPercentAxisLabels`(圖表軸標,`%.1f`)各寫一份。另外 `MeanCurves`(與 `PairResults[i].Values` 同一個 slice 的第二份索引,無任何讀取者)與常量 `GaitCyclePoints`(無引用)是死碼,iframe JS 裡的 `allChecked` 分支(`payload.allChecked === true` 時還原 cache)是已死的分支。

## Decision

1. **`*CCIAnalysisResult` 持有不變式**:
   - `Validate() error`:nil → `ErrNilResult`;`GaitStartTime` / `GaitEndTime` 非有限值或 `duration <= 0` → `ErrInvalidGaitCycle`;任一 pair 長度不等於 `len(TimeValues)` → `ErrPairLengthMismatch`。錯誤文字沿用原 chart 的格式。
   - `GaitPercent(t) float64`:不 clamp(<0% / >100% 是 ±150ms 延伸範圍的合法值,見 [[Gait cycle (CCI)]])。
2. **package-level `gaitPercent(t, start, end)`** 是公式唯一寫處,運算順序固定為 `(t - start) / (end - start) * 100`,讓 CSV `%.2f` 與圖表 `%.1f` 的輸出位元組不變。`calculateGaitCycle` 的 clamp 版也呼叫它(clamp 仍在呼叫端)。
3. **呼叫端**:
   - `computeCCI` 回傳前 `Validate()`,往下游的結果已保證合法;
   - `GenerateCCIInteractiveChart` 入口改呼叫 `Validate()`,刪 `validateCCIResultForChart`;
   - `downsampleCCIResult` 刪掉重檢,簽章改成不回 error;`chart_guards_test.go` 刪兩個直接子測試(`nil_pair_values_rejected`、`shorter_pair_values_rejected`),保留 `entry_point_rejects_mismatch`(入口行為仍受測);
   - `io.WriteCCIResult` 改用 `Validate()` 與 `GaitPercent()`,刪 `errEmptyCCIResult` 與對短 pair 的容忍分支。
4. **刪死碼**:`MeanCurves`、`GaitCyclePoints`、iframe 的 `allChecked` 分支(`__cciOriginalPctLabels` 的 cache 與其 fallback 保留)。
5. **欄位維持 exported**:io 測試用 struct literal 建構這個型別,不改成 constructor。
6. 改正過時註解:原指向 `writeCSVFile` 與 `analyzer.go:464-468` 的說明改指向 `Validate`。

## 行為變更

| 情境 | 之前 | 之後 |
| --- | --- | --- |
| `WriteCCIResult` 收到 gait 時間為 NaN / Inf | 寫出 `Gait Cycle (%)` 全為 `NaN` 的 CSV | 回 `ErrInvalidGaitCycle`,不寫檔 |
| `WriteCCIResult` 收到短 pair(`len(Values) < len(TimeValues)`) | 缺的格子補空字串 | 回 `ErrPairLengthMismatch`,不寫檔 |
| `computeCCI` 產出不合法結果 | 往下游傳 | 在出口回錯(實務上 `calculateGaitCycle` 與 `computeAllPairs` 已保證,不會觸發) |

正常資料的 CSV 與圖表輸出位元組不變(golden harness 比對)。

## Why

- **不變式只有一個 owner**:結果自己知道什麼叫合法,producer 出口、CSV、圖表三個 consumer 呼叫同一個方法;不再有「某個 consumer 漏掉 NaN」這種各檢各的漂移(此次的 bug 正是如此)。
- **公式只寫一次**:百分比定義是 [[Gait cycle (CCI)]] 的核心,三處手抄同一個式子,改運算順序就會讓 CSV 與圖表不一致。
- **replace, don't layer**:`Validate` 有測試後,舊的 downsample 內重檢與其子測試是同一契約的第二份,刪掉。

## 與既有 ADR 的關係

- [[ADR-0018]]:gait 錨點 `0%=S`、`100%=L` 與 ±150ms 延伸不變,`GaitPercent` 不 clamp 與其一致。
- [[ADR-0024]]:`computeCCI` 仍是純計算 core,只在出口多一道 `Validate()`。
- [[ADR-0010]]:LTTB union kernel 不變;其 caller invariant(pair 長度對齊)現在由 `Validate` 在入口保證。

## Considered Options

- **A. 結果持有 `Validate` / `GaitPercent`(採用)。**
- **B. 把驗證放進 `io`,chart 另外保留自己的一份。** 拒:兩份會再度漂移。
- **C. 建 constructor `NewCCIAnalysisResult` 強制合法。** 拒:io 測試以 literal 建構,且 writer 對外部傳入的結果仍需檢查,constructor 擋不住。
- **D. `GaitPercent` 內建 clamp。** 拒:CSV 與圖表需要 <0% / >100% 的延伸範圍,clamp 只屬於 phase marker 的百分比。
