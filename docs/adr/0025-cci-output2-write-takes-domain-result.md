# CCI Output-2 寫入直接吃 domain result(對稱化、刪 io-local mirror)

**Status**: accepted — implemented (2026-06-15)

## Decision

把 CCI Output-2(`{safeSubject}_CCI_Rudolph_phases.csv`)的寫入入口 `WriteCCIPhasesResult` 簽章從吃 io-local mirror payload 改為**直接吃 `*cci.CCIAnalysisResult`** —— 與孿生寫入 `WriteCCIResult`(Output-1,`csv_handler.go`)完全對稱(後者本就吃這個 domain 型)。三項一併鎖定:

1. **簽章**:`func (h *CSVHandler) WriteCCIPhasesResult(ctx context.Context, req WriteRequest, result *cci.CCIAnalysisResult)`。取代原 `p CCIOutputPhasesPayload`。
2. **內部推導**:pair 欄位 header 由 `result.PairResults`(逐個 `.PairName`)在 method 內推導;資料列由 `result.PhaseStats`(`[]cci.CCIPhaseStatRow`)逐列 emit。原本由 GUI handler(`gui/cci_handlers.go`)逐欄手抄每一列 phase-stat 再傳入的工作,收進 `io` 內。
3. **刪除**:io-local mirror 兩型 `CCIPhaseStatRowPayload` + `CCIOutputPhasesPayload`,以及 handler 那段逐欄複製迴圈,一併移除。

**byte-identical**:emit 的 CSV 一個 byte 不變。原因是 handler 本來就用**同一個** `result.PairResults`、**同序**建出 pair labels,而那段 copy loop 是逐欄 `Item/Metric/Time/HasTime/Values` 的 no-op 搬移 —— 把推導點從 call site 移進 `io`,輸出值與順序完全不動。

**nil 守門**:新增 `result == nil` guard,回傳既有 sentinel `errEmptyCCIPhasesPayload`(與 `len(result.PhaseStats) == 0` 空列情形共用同一個 error),語意上「沒有可寫的列」。

## Why — import 方向原則(本案核心)

mirror 型是「有形狀、無強迫理由」(form without forcing function)。一般原則看 analyzer 與 `io` 的相依方向:

- **compute-only** 的 domain analyzer(如 `cci`,只算、從不自己呼叫 `io` 寫檔):domain 型可以**直接穿過**寫入接縫,因為相依邊 `io → cci` 已存在、而 `cci` 不 import `io` —— 沒有 cycle 要破。`io` 大方吃 `*cci.CCIAnalysisResult` 完全合法。
- **compute+write** 的 analyzer(如 `muscle_ratio`,其 `analyzer.go` 自己呼叫 `params.CSVHandler.WriteMuscleRatioOutput*` 寫檔):**必須**交給 `io` 一個 io-local payload,因為 `io` 不能 import 那個 analyzer —— 會造成 `io ↔ muscle_ratio` 循環相依。

CCI Output-2 的 mirror 抄了 muscle_ratio payload 的**形狀**,卻沒有 muscle_ratio 破循環的**理由**。結果它只付出一次手抄成本、什麼都沒換到。Output-1(`WriteCCIResult`)本來就把 `*cci.CCIAnalysisResult` 穿過去 —— Output-2 才是那個不對稱的異類;本案把兩者拉回一致。

## Considered Options

- **維持 mirror** — 拒:既無 cycle,mirror 就是純儀式 / 純成本,留著只會誘使後人以為「io 不該碰 domain 型」這條規則對 CCI 成立(它不成立)。
- **A1:窄簽章(`[]cci.CCIPhaseStatRow` + 另傳 `pairLabels []string`)** — 拒:仍把 call-site 的管線(自己拆 pair 名稱)漏給呼叫端;A2 的整顆 result 穿透更簡單、且與 Output-1 同形。
- **B:單一寫入同時產兩個 CSV 輸出** — 拒:GUI 的單輸出 handler template 回傳剛好**一條** path,兩條輸出路徑塞不進;且會把 CCI 推離共用 template,與刻意的 template 設計衝突。
- **連 muscle_ratio 一起對稱化** — 拒:那**會**造出 `io ↔ muscle_ratio` 循環;muscle_ratio 的 io-local payload 是正當的、保留不動(與本案正交,見 [[ADR-0012]])。
- **讓 cci import io、自己寫檔** — 拒:破壞 compute-only 分層、且同樣造出 cycle。

## Consequences

- **唯一 byte-level safety net** 是 io 單元測試 `internal/io/csv_handler_cci_phases_test.go`(執行期的 `output/*_phases.csv` 是 gitignored artifact;`internal/cci/phase_stats_test.go` 測的是**計算**、不是寫入)。新增 `TestWriteCCIPhasesResult_NilResult` 覆蓋新的 nil 分支。
- **locality 改善**:兩個 CCI 寫入(Output-1 / Output-2)現在都用**同一段內部迴圈**從 `result.PairResults` 推導 pair 欄位,於是 Output-1/Output-2 的欄位對齊由 `io` 的結構**保證**,而非靠 call-site 的口頭約定(原本 handler 要記得「Output-2 的 PairLabels 沿用 Output-1 的 pairNames 同序」)。
- handler 少一道手抄接縫,handler 尾段縮短。

## Reversibility

容易反轉 —— 重新引入 mirror 兩型與 copy loop 即可。無資料遷移、無持久化格式變動(輸出 byte-identical)。

## Related

- [[ADR-0018]] — 建立 CCI Output-2(分期視窗統計表)與最初的 io payload 兩型;本案刪除的正是那兩個 mirror 型。
- [[ADR-0022]] — Output-2 最近一次**列內容**變更(區間列 → 中點 ±50ms 視窗);本案只動寫入接縫的型,不碰列內容,故與其正交、輸出 byte-identical。
- [[ADR-0012]] — Domain analyzer 維持刻意分歧的形狀;解釋為何 muscle_ratio 的 io-local payload 該保留(compute+write 有破 cycle 的理由),而 CCI(compute-only)不需要。
- [[ADR-0004]] — format-aware write:把列 layout 推進 CSVHandler;本案是其延伸 —— 連 pair 欄位的推導也收進 `io`,呼叫端不再 reach-in。
