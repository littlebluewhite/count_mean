# DataParser 與 LargeFileHandler 維持 dual `parseDataRow` — 拒絕「抽共用 channel-cell kernel」深化

**Status**: accepted (2026-05-29)

## Decision

`internal/parsers/data_parser.go` 的 `(*DataParser).parseDataRow`(:126)與 `internal/io/large_file_handler.go` 的 `(*LargeFileHandler).parseDataRow`(:1017)維持**兩個獨立實作**。2026-05-29 architecture review candidate 4 提出的「抽出共用 channel-cell 解析 kernel(典型形狀 `parseChannelCells(record []string, sf int, dst []float64) ([]float64, error)`,caller 自己擁有 destination slice)」**拒絕**。

兩者概念上同一份工作(`[time, ch1, ch2, …]` 一列 → `*models.EMGData`,用 `scalingFactor` 縮放),但唯一**真正重疊**的邏輯——per-cell 解析 `util.Str2Number[float64, int]`——**早已是共用 deep helper**。再往上抽一層 loop 不 concentrate 複雜度,只搬動 ~4 行;而真正帶複雜度的部分(allocation 策略、tolerance policy、logging、error 訊息)在兩邊**分歧且必須留在 caller 端**。

## Why

- **Deletion test 不過,但機制與 [[ADR-0005]] 不同。** 這裡只有 **2 個 callsite**(各一),所以失敗模式**不是** [[ADR-0005]] 的「拔除後位移到 14 處 callsite」。失敗模式是 **interface-widening**:要把 A+B 收成一個函式,必得參數化 *allocation 策略(pool vs fresh)+ tolerance policy(skip vs abort)+ logging*,shared interface 隨之變寬 → merged module 變 **shallow**。同屬「不為對稱而 collapse」家族,但**機制是 interface-widening 而非 callsite-displacement** — 未來 reviewer 不要把兩者混為一談。

- **buffer-pool divergence 是 load-bearing(牆一)。** streaming 路徑 `processSlidingWindowRecord`(:921)borrow `pool.GetFloat64Slice()`(:1035)→ `state.feed` 複製進 ring → **立刻 `PutFloat64Slice` 歸還並 nil 別名**(:931-937),註解明載這是 300k-point × 16-channel 串流不 GC thrash 的設計。任何改成 fresh-allocate 的 unified `parseDataRow` 都會 regress streaming 記憶體行為。DataParser 反向用 `make([]float64, 0, len(row)-1)`(parseChannels,:106)fresh 配置,full-file 路徑無 pool 生命週期可言。

- **tolerance policy divergence 是 load-bearing(牆二,且兩牆不相依)。** 兩邊對「壞 row」的 severity 映射**直接衝突**:
  - **A(DataParser)對 time 欄寬容、對 channel cell 嚴格**:空/單欄/空白時間/時間無法解析 → `ErrSkipRow`(:128-150),caller `ParseRawDataWithOptions` `continue`(:174-185);但 channel cell 解析失敗 → **hard error**(:152-155),沿 :181 propagate,**中止整檔 parse**。
  - **B(LargeFileHandler)對所有 error 一律寬容**:`parseDataRow` 對 short row / 壞 time / 壞 channel 一律回 error,但 caller `processSlidingWindowRecord` 把**每一種** error 吞成 skip(`return nil //nolint:nilerr`,:921-928 並 log Debug)。
  - 結論:同一筆「壞 channel cell」資料,A 中止整檔、B 跳過續跑——這是**互斥的容錯語意**,不是可參數化的旋鈕。且 B 的 skip 概念其實活在 **caller** 而非 `parseDataRow`;把它下推共用函式 = 把 caller 的決策權搶過來。

- **唯一誠實可共用的 kernel 太 anaemic;肥 kernel 又太 shallow。** caller-owns-dst 的 narrow `parseChannelCells` 能 concentrate 的只剩 `for i:=1; i<len(record); i++ { Str2Number; append }` ~4 行 trivial loop。分歧部分塞不進去:error 訊息格式不同(A `解析數據失敗在第 %d 行第 %d 列` 帶 rowNumber+col;B `解析通道 %d 失敗` 帶 channel index,兩邊 test 各自斷言)、logging 不同(A 在 parseChannels Error log;B 不 log)、pool-return-on-error 不同(B 必須 `PutFloat64Slice`,:1040;A 丟給 GC)。kernel 的 interface 幾乎跟它 4 行 implementation 一樣複雜 → shallow。這正是 *「pure function 為了 testability 抽出,真正的 bug 卻藏在它怎麼被呼叫」* 反模式:複雜度在呼叫情境,而呼叫情境恰恰不能共用。

## Considered Options

- **A. 抽 narrow `parseChannelCells`(caller-owns-dst)** — *本次最有希望、probe 最硬的角度*。拒:deletion test 不過(只搬 ~4 行 trivial loop,不 concentrate;error/logging/pool-return 分歧仍留 caller),kernel shallow。
- **B. 肥 kernel(連 skip-policy + logging + pool-return-strategy 一起參數化)** — 拒:把三種 caller 決策塞進一個簽章,interface 比 A 更寬、更 shallow,且要把 B 的 caller-side `//nolint:nilerr` skip 語意硬搬進共用層,違反 [[ADR-0005]]/[[ADR-0006]] 確立的「不為對稱而 collapse」。
- **C. 反向收斂:streaming 改呼叫 `DataParser.parseDataRow`** — 拒:撞兩道牆。(i)DataParser fresh-allocate,regress streaming 的 pool 復用(牆一);(ii)DataParser 對壞 channel cell 中止整檔,streaming 要 skip 續跑(牆二),語意對不上。
- **D. status quo:維持 dual `parseDataRow`(chosen)** — 兩函式各自深、各自鎖一套 divergent 契約;唯一重疊的 `util.Str2Number` 已在正確深度共用。無 code 變更、無 migration。

## Reversibility

低成本 — 拒絕抽 kernel 是維持現狀,沒有 migration 動作。本決策由**兩道不相依的 load-bearing 牆**支撐(buffer-pool / tolerance-policy);robustness 來自此。**只有當兩牆同時倒**才該重啟 grilling,例如:streaming 不再需要 buffer-pool(牆一倒)**且**兩邊 tolerance policy 收斂成同一套 severity 映射(牆二倒)。單一條件變化不足以翻案。本 ADR 不鎖死,只記錄「2026-05-29 這時間點下不採」的理由與翻案門檻。

## Related

- [[ADR-0005]](calculator family 維持 dual-shape interface)— **sibling,但機制不同**。0005 拒絕拔 `*FromRawData` 的 deletion-test 失敗是 **callsite-displacement(14 處)**;本 ADR 拒絕抽 channel-cell kernel 的失敗是 **interface-widening(只有 2 處 callsite,失敗在 interface 變寬→shallow,不在位移)**。兩者都是「parser/calculator 形狀不為對稱而 collapse」,但拒絕的*力學*不同——刻在族譜上避免混讀。`scalingFactor` field-vs-param 的近-cosmetic 差異見 §Process note,牽涉 [[ADR-0005]] §Option D 所保護的 ctor-snapshot 契約。
- **Deletion-test 四重奏**:[[ADR-0005]](displacement 失敗 → preserve)/ [[ADR-0006]](complexity 真消失 → collapse)/ [[ADR-0008]](dead cluster → delete)/ **本 ADR-0011(shallow-merge → preserve)**。本 ADR 是第 4 個 case、第 2 個「preserve」,但 preserve 的理由與 0005 不同(widening vs displacement)。四條合讀:**case-by-case 跑 deletion test,不為 collapse 而 collapse、不為 symmetry 而 preserve**。

## Process note — cross-check 與 framing 修正(防未來 reviewer 重蹈)

2026-05-29 grilling 開場 cross-check（[[memory:feedback_cross_check_report_vs_code]] 紀律）對照 handoff 載明的 candidate 4 file:line evidence vs 真實 code,結果 file:line 大致全中,但有兩處 framing 需修正,記於此:

1. **ADR 編號 collision(兩重)** — (i)handoff exit criteria 寫「next likely **0009**」失準:`0009`(PNG download safety pipeline,已 commit `70af1b2`)與 `0010`(LTTB downsampling kernel,untracked)皆已存在。(ii)更隱蔽的一重:**2026-05-29 同日有另一平行 grilling session 開 candidate 3(domain-analyzer)ADR**,兩 session 在各自 grill-start `ls` 都只見 max=0010、都算出 0011;經 mtime 定序,本檔(parse-row,較早 ~96s)保留 **0011**,domain-analyzer 改 **0012**(見 [[ADR-0012]])。對齊 [[ADR-0006]] §Process note point 4(0005 撞號)與 [[ADR-0008]] §Process note Q7 的同類紀錄。**教訓升級**:不只 grill-start,**write 當下需再 `ls docs/adr/` 一次**——平行 session 會在你 grill 期間搶號;撞號時以 mtime 定先後、較晚者改自己檔名與自身 cross-ref、**絕不動對方 in-progress 檔**。

2. **skip-policy framing 精修** — handoff 載 A=「skip semantics」/ B=「fail-only, no skip concept」,精度不足。cross-check 修正為:兩套 **tolerance policy 落在不同 layer**,且對「壞 channel cell」**直接打架**(A `parseDataRow` 內 abort-via-hard-error,large_file_handler.go:921-928 caller `//nolint:nilerr` swallow-as-skip)。B 的 skip 概念活在 caller 而非函式本身。此精修**強化** interface-widening 論點(見 §Why 牆二)——不是「B 沒有 skip」,而是「B 的 skip 更寬容且不在同一層」。

3. **`scalingFactor` source(grill Q4)近-cosmetic、非 load-bearing** — DataParser `p.scalingFactor` 是 `NewDataParser` ctor-field;streaming `state.scalingFactor`（large_file_handler.go:627）亦從 `h.config.ScalingFactor` init-snapshot（:740）後以 param 串接。兩者皆 config 的 init-time 快照,差異僅「struct-field 存取 vs 顯式 param threading」,**不構成 merge 理由也不構成 split 理由**。但天真地「全改 param」會觸及 [[ADR-0005]] §Option D 所保護的 snapshot-field 契約(`s.maxMeanCalc` ↔ `s.config.ScalingFactor` 同源配對),故非完全無關。本決策 load-bearing 的是 Q1(pool)+ Q3(tolerance),Q4 誠實降級。

4. **CONTEXT.md 不動(grill Q6)** — shared parse kernel 即使被採納也是 implementation helper、非 domain concept,對齊 [[ADR-0008]] 對 `EChartsGenerator` 的判定(impl 型別不入 glossary)。本 ADR 無新 domain term。

5. **無 code 變更、無新 test(grill Q5)** — design-only + preserve 現狀。`internal/parsers/data_parser_test.go` 與 `internal/io/large_file_handler_streaming_test.go`（及 `large_file_handler_test.go`）既有 contract test 各自鎖兩邊 divergent 契約,不需動。若 candidate 4 的 kernel 當初 pass,才需要 kernel 自己的 unit test + 兩 caller 各保留 contract test。
