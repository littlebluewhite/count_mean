# MaxMean 時間範圍由 calculator 持有

**Status**: accepted · **implemented** (2026-10-11) · amends [[ADR-0026]]

[[Max-mean]] 的計算核心早已支援半開範圍：`resolveDataRange` 以 `HasStartRange` / `HasEndRange` 顯式旗標判斷兩端，`0` 是合法邊界。但對外介面只有「不給範圍」與「兩端都給」兩種（`CalculateWithRange` 一律把兩個旗標設成 true），於是「範圍是什麼意思」由外面三處各自決定：前端 `parseFloat(...) || 0` 把留空與填 0 都送成 `0`；`calculator.ResolveTimeRange` 把 `(0, 0)` 換成檔案首 / 末列的時間，其餘原樣回傳；single（`gui.calculateWithTimeRange`）與 batch（`maxmean.RunBatch`）再各自用 `startRange == 0 && endRange == 0` 二分 dispatch。三處都沒法表達「只給起點」，因此只填起點 `(2, 0)` 會變成範圍 `[2, 0]`，回 `ErrInvalidTimeRange`（已以 overlay probe 與紅燈測試確認）。本 ADR 把範圍語意收回 calculator。

## Decision

1. **calculator 新介面**（`internal/calculator/maxmean.go`）：

   ```go
   type MaxMeanRange struct{ Start, End *float64 } // nil = 未給，原域秒（避開既有 models.TimeRange）
   type MaxMeanRun struct {
       Results    []models.MaxMeanResult
       Start, End float64 // 原域秒：有給的那端印使用者的值；沒給的那端印實際首/末樣本（UnitScale.FromScaled）
   }
   func (c *MaxMeanCalculator) CalculateInRange(ctx, dataset *models.EMGDataset, windowSize int, r MaxMeanRange) (*MaxMeanRun, error)
   func (c *MaxMeanCalculator) CalculateFromRawDataInRange(ctx, records [][]string, windowSize int, r MaxMeanRange) (*MaxMeanRun, error)
   ```

   內部仍把 `MaxMeanRange` 轉成 `CalculationOptions`（只有給了的那端設 `Has*`），`resolveDataRange` 與直接打 `calculateWithOptions` 的測試都不動。`Calculate` / `CalculateFromRawData` 保持不變（[[ADR-0005]] 的雙形狀）。
2. **刪除** `CalculateWithRange`、`CalculateFromRawDataWithRange`、`ResolveTimeRange`（`time_range.go`）、gui 的 `calculateWithTimeRange`，以及 `runner.go` 的 dispatch 複本。single 與 batch 都直接呼叫 `CalculateFromRawDataInRange`。
3. **Wire**：`gui.MaxMeanParams.StartTime/EndTime` 改為 `*float64`（json `omitempty`，Wails TS 產生 `startTime?: number`）；`maxmean.BatchParams{WindowSize int; Range calculator.MaxMeanRange}`。前端解析抽成 `frontend/src/timeRangeInput.mjs` 的 `parseOptionalSeconds`：留空 → `null`，以 `Number.isFinite` 守門。
4. **CSV 範圍列**：「開始/結束範圍秒數」改印 `MaxMeanRun.Start/End`。`ResultWriter` port 與 `CSVHandler.WriteMaxMean` 的簽名不變。

### 行為清單（刻意變更）

| 輸入 | 舊行為 | 新行為 |
|---|---|---|
| 只填起點 | `[起點, 0]` → `ErrInvalidTimeRange` 失敗 | 成功，範圍 `[起點, 末樣本]` |
| 只填終點 | 起點當 `0`：負時間樣本被丟掉，開始範圍列印 `0` | 從首樣本起算，負時間不再丟；開始範圍列改印實際首樣本 |
| 兩欄都填 `0` | 等同整檔 | `[0, 0]` → 範圍不足錯誤（`ErrInvalidTimeRange`） |
| 起點填 `0`、終點留空 | 等同整檔（含負時間樣本） | `[0, 末端]` |
| 單檔路徑計算錯誤 | 兩種前綴：「計算失敗: 計算最大平均值失敗: …」與「計算失敗: 計算指定範圍最大平均值失敗: …」 | 統一成一種：「計算失敗: …」 |
| 整檔且首 / 末列時間欄空白或無法解析 | `ResolveTimeRange` 把該格當 `0` → `[首, 0]` 失敗（或範圍列印 `0`） | 依實際解析到的首 / 末樣本；範圍列印實際值 |

兩端都留空（整檔）與兩端都填時，輸出 CSV 與舊版 byte-identical（W0 golden 驗證，見 Notes）。

### Amends ADR-0026

- **Notes「dispatch 刻意重複（勿 DRY 回去）」**：當時的理由是「single 留 gui、batch 進 internal，合一會造成 gui→internal import cycle」。那只考慮了把 dispatch 放進 gui 或 `internal/maxmean`；放進兩邊都已 import 的 `internal/calculator` 不會造成 import cycle。dispatch 現在只存在 calculator 內（由 `MaxMeanRange` 是否為 nil 決定），兩份複本皆刪。
- **Decision §6**（`ResolveTimeRange` 移到 calculator 當 single/batch 共用家）：`ResolveTimeRange` 已刪，其職責由 `CalculateInRange` 回傳的 `MaxMeanRun.Start/End` 取代。
- **Consequences 的 runner 測試「resolved-pair 轉發」**：該測試以 `ResolveTimeRange` 自己算期望值（自己驗自己），改為斷言具體數字（`internal/maxmean/runner_test.go` 的 `TestRunBatch_TimeRange`）。

[[ADR-0026]] 本身依 repo 慣例不修改。

## Why

- **bug 不在任何單一 pure 片段，而在片段之間的約定。** `resolveDataRange` 早已移除「`0` = 未給」的 sentinel，但 `ResolveTimeRange`、兩份 dispatch 與前端 `|| 0` 又把它重新造了出來。每個單元都綠，組起來卻表達不了「只給起點」。把語意收在擁有它的 module，外面就只剩「有給 / 沒給」這個事實要傳遞。
- **Deletion test**：刪 `ResolveTimeRange` 與兩份 dispatch 不會把複雜度推回 caller —— calculator 自己就能選分支，也最清楚實際採用了哪個範圍（CSV 範圍列要的正是這個）。
- **指標比旗標更難用錯**：`*float64` 讓「有沒有給」與值不可分離；JSON 的 `null` / 缺欄位天然對應 nil，前端不必再用 `0` 冒充「未給」。

## Considered Options

- **A. calculator 持有範圍：`MaxMeanRange{Start, End *float64}` + `MaxMeanRun`（chosen）**：見上。
- **B. 保留 `ResolveTimeRange`，只在它與 dispatch 裡修 sentinel**：拒。dispatch 仍有兩份、CSV 範圍列仍由 caller 拼湊，下一個範圍變化又要同步改兩處。
- **C. 把既有的 `CalculationOptions` 匯出成公開範圍型別**：拒。四個欄位裡 `Has*` 與值可以彼此矛盾（`HasStartRange == false` 但 `StartRange != 0`），正是 `resolveDataRange` 註解記錄過的 latent bug 來源；指標不會有這種狀態。
- **D. `MaxMeanParams` 改用 `HasStartTime` / `HasEndTime` bool 欄位**：拒。前端得同步送兩組欄位，忘了一個就退回 sentinel 問題；`*float64` + `omitempty` 只需一個欄位。
- **E. `MaxMeanRun` 沒給的那端印檔案首 / 末列的原始字串值（舊 `ResolveTimeRange` 作法）**：不採為預設。`FromScaled(ToScaled(x))` 理論上可能與 `x` 差 1 ULP，但輸出以 `%.{precision}f` 格式化，W0 golden 的整檔範圍列 byte-identical，故不需要此 fallback；且原始字串作法對空白 / 無法解析的首末列會印出 `0`。若日後真的出現 ULP 差，再退回印已解析首 / 末列的原始值。

## Consequences

- 公開 API：`CalculateWithRange` / `CalculateFromRawDataWithRange` / `ResolveTimeRange` 消失，由 `CalculateInRange` / `CalculateFromRawDataInRange` 取代；`docs/api.md`、`docs/usage_patterns.md`、GLOSSARY **Max-mean**（補範圍語意）同步更新。
- 測試：`TestRunBatch_TimeRange` 以真 calc 斷言五種範圍（整檔、兩端、只給起點、末列時間空白、只給終點含負時間）的具體數字；`TestMaxMeanCalculator_CalculateInRange_RunBounds` 釘 `MaxMeanRun.Start/End` 在 sf=10 下經 `FromScaled` 轉回，以及兩端填 `0` 不等於整檔。原本打 `CalculateWithRange` / `CalculateFromRawDataWithRange` 的 15 處測試改打對應的 `*InRange`（兩端都給，語意不變）；直接打 `calculateWithOptions` 的 10 處不動。
- `gui.CalculateMaxMean` 的 debug log 改記解參考後的 `start_time` / `end_time`（nil = 未給），避免印出指標位址。
- `CalculationOptions` 仍是 exported，但已無任何 exported 方法接受它；本 ADR 不處理。

## Related

- [[ADR-0026]] —— **amended**：Notes「勿 DRY 回去」與 Decision §6 由本 ADR 取代。
- [[ADR-0005]] —— 雙形狀維持：`Calculate` / `CalculateFromRawData` 不變，範圍版本也成對（`CalculateInRange` / `CalculateFromRawDataInRange`）；`scalingFactor` 仍是 ctor 參數。附註：Option D 拒絕理由引用的 `PhaseAnalyzer.parsePhases` 已於 commit `9f3a5d3` 刪除；理由的另一半（`MaxMeanCalculator.resolveDataRange` 直接消費 ctor-time `scalingFactor`）仍成立，拒絕結論不變。
- [[ADR-0049]] —— `MaxMeanRun` 沒給的那端以 `UnitScale.FromScaled` 轉回原域秒。
- [[ADR-0037]] —— 本案只改 `MaxMeanParams`（輸入），不動收窄後的 result 通道。

## Notes

- **W0 golden**（`gui.App` handler 層，真實資料 SF2，window 100）：整檔、兩端 `(5, 10)`、只給終點 `(nil, 10)` 三個 case byte-identical（該檔首樣本時間為 `0`，所以只給終點的開始範圍列仍印 `0.0000000000`）；只給起點 `(5, nil)` 由 `maxmean_startonly.err`（「計算失敗: 計算指定範圍最大平均值失敗: 指定時間範圍內的數據不足以進行窗口分析」）變為成功，範圍列 `5.0000000000` / `14.4400000000`。其他功能輸出零差異。
- **GUI smoke 未驗**：native webview 在 headless 環境無法呼叫 `window.go` binding；前端解析由 `timeRangeInput.test.mjs` 覆蓋，Wails binding 以 `wails generate module` 重生確認 `startTime?: number`。
