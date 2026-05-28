# Calculator family 三 sibling 維持 dual-shape interface — 拒絕「拔 `*FromRawData` 把 calculator 收成純 EMGDataset」深化

**Status**: accepted (2026-05-28)

## Decision

`MaxMeanCalculator` / `Normalizer` / `PhaseAnalyzer` 三個 calculator 維持目前 **dual-shape interface**:primary method 接受 `*EMGDataset`(`Calculate(ctx, *EMGDataset, windowSize)` / `Normalize(dataset, reference)` / `Analyze(dataset, phases)`),並存 `*FromRawData` convenience method 接受 `[][]string` 並 ctor 階段 embed `dataParser *parsers.DataParser`(以 ctor-time `scalingFactor` 注入)。2026-05-28 architecture review 提出的 Candidate #2「calculator interface 收 [[EMGDataset]] 拔除 `*FromRawData`」**拒絕**;同時把 [[ADR-0004]] §Boundary 2 末尾「Candidate 4(calculator 收 EMGDataset)撞車」這句精確 disambiguate(見 Related)。

## Why

- **Primary interface 今天已經是 EMGDataset。** `MaxMeanCalculator.Calculate(ctx, *EMGDataset, int)`(maxmean.go:155)、`Normalizer.Normalize(*EMGDataset, *EMGDataset)`(normalizer.go:40)、`PhaseAnalyzer.Analyze(*EMGDataset, []TimeRange)`(phase_analyzer.go:168)都是 EMGDataset-入口;Candidate #2 報告 problem statement「caller threads rows · headers · path · precision · BOM · scaling 跨 seam」在 code 端 over-stated 到失準 — GUI 實際傳給 calculator 的是 `[][]string` + ctor-time `scalingFactor` + window + range,path / BOM 在 `csvHandler.ReadCSV` 那層、time precision 是 dataParser 內部 detect、headers 從 records[0] 取,**沒有 file-context 跨 seam 流進 calculator**。
- **Deletion test 不過。** 拔 `*FromRawData` + embedded `dataParser` 不讓複雜度消失,只把 parse step 推到 **14 處 callsite**(GUI 5 處 + internal/benchmark 5 處 + test/integration 3 處 + test/demo 1 處),每處 +1 line 顯式 `parsers.ParseRawData(scalingFactor, records)` 呼叫。複雜度位移而非 concentration,違反 deep-module 收乾的本意。
- **`*FromRawData` 是 honest 的 API ergonomic,不是 shallow leak。** 三 sibling 對稱讓 test / benchmark / 未來 CLI 單行 round-trip 不必每處重複 parse setup;struct embedded `dataParser` 攜帶 ctor-time `scalingFactor` 確保 convenience path 跟 primary path 用同一份 scaling 配置,沒有 drift 風險(對齊 `app.go:120` snapshot-consistency 測試的「`s.maxMeanCalc` 跟 `s.config.ScalingFactor` 配對同源」契約)。

## Considered Options

- **A. 三 sibling 一起拔 `*FromRawData` + 拔 struct embedded `dataParser`**:14 callsite 改為顯式 `parsers.ParseRawData(scalingFactor, records)` 再餵 primary。拒:deletion test 不過(位移而非消失),三 sibling「pure compute + parser pre-step」的形式美但 14 處 boilerplate 加上 test/bench 的 parse helper 複製代價更高;且原始 friction signal 本身 over-stated。
- **B. 只動 MaxMean,Normalizer / PhaseAnalyzer 留 dual-shape**:違反三 sibling parity(三者今天形狀對稱、深化代價跟收益對稱),且 [[ADR-0004]] §Boundary 2 末尾的「Candidate 4」概念上指三 sibling conceptual unification,只動一個既不解 [[ADR-0004]] follow-on 也製造額外 asymmetry。
- **D. 把 `scalingFactor` 從三 calculator ctor 移出,per-call 注入**(本次 grill 探索的 alternative shape):拒。`scalingFactor` 不只 parser 用 — `MaxMeanCalculator.resolveDataRange`(maxmean.go:447)做 time-axis math 直接消費,`PhaseAnalyzer.parsePhases`(phase_analyzer.go:254)做時間點解析也直接消費;移出 ctor 變 per-call 注入會擴張三 method 簽章,且 GUI snapshot pattern(`s.maxMeanCalc` 配對同一份 `*appState`)的 `ScalingFactor` consistency 保證會失效。

## Reversibility

低成本 — 拒絕拔除是維持現狀,沒有 migration 動作。日後若觸發條件出現(例如:`EMGDataset` 真的 stamp 上 source identifier 後 [[ADR-0004]] §Boundary 2 File-based filename re-collapse 落地、或 CLI mode 真正 land 後 ergonomic 論點變弱、或三 sibling 形狀因其他原因需要對稱破壞)仍可重啟 grilling;本 ADR 不鎖死,只記錄「2026-05-28 這時間點下不採」的理由。

## Related

- [[ADR-0004]] **§Boundary 2 末尾 disambiguation**:「硬要統一只能往 AnalyzeResult 塞 SourceInputFile 之類的 file-context 欄位,又跟 Candidate 4(calculator 收 EMGDataset)撞車」這句在 [[ADR-0004]] 內未展開、容易被未來 reviewer 誤讀為「拔 `*FromRawData` = Candidate 4」。本 ADR 把精確讀法刻在族譜上:
  - 「Candidate 4(calculator 收 EMGDataset)」的真正所指,**不**是拔 `*FromRawData` convenience。primary interface 今天已經是 EMGDataset、拔 convenience 對 Boundary 2 collapse 沒有貢獻。
  - 「撞車」的真正內容是 **File-based filename ownership re-collapse 的 dependency** — 亦即「PhaseAnalysis / MaxMean / Normalized 三條 File-based write 由 CSVHandler 內推 filename 而非 caller 傳 `req.Filename`」這個動作所需的前提。前提內容是二擇一:**(i)** `EMGDataset` 帶 source identifier 讓 CSVHandler 反推 filename;或 **(ii)** 維持 caller 傳 `req.Filename` 並接受 filename ownership 留在 caller。本次 grill 不解這條,留給獨立 follow-up grill / ADR 處理。
- 本 ADR 不否認 [[ADR-0004]] §Boundary 2 的決定 — 兩條決定平行成立:`*FromRawData` 保留(本 ADR)+ filename ownership 沿 unit-of-work 形狀切分([[ADR-0004]] §Boundary 2)。
