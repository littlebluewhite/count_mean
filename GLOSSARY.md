# count_mean 領域語言

EMG 肌電訊號分析工具的領域概念字典。架構詞彙（module / interface / seam / depth / leverage / locality 等）見 `improve-codebase-architecture` skill 的 LANGUAGE.md，這裡只列領域 specific 的概念。

## Language

**EMGDataset**
一筆完整的肌電訊號量測，包含 headers（通道名稱）、time-series rows、與 OriginalTimePrecision。所有分析輸入都先解析為 EMGDataset 後送入 calculator。
_Avoid_: data file, signal data, EMG records.

**Channel**
EMGDataset 中的一條獨立肌電訊號欄位（例如「左前脛骨肌」「右股外側肌」）。一個 EMGDataset 有 1 到 N 個 channels。
_Avoid_: column, channel data, muscle, signal stream.

**Phase**
量測時段內由 manifest 標示的一段時間區間（例如「站立期」「擺盪期」），由 PhaseLabel + TimeRange 定義。Phase analysis 對 EMGDataset 按 phase 切片再計算 max/mean。
_Avoid_: stage, interval, segment, period.

**Phase marker**
[[Chart Composer]] 與 CCI chart 上對單一 [[Phase]] 渲染的視覺元素 — 一條垂直虛線 + phase 名稱 + 重算百分比 label(`{phaseName}\n({pct}%)`)。Phase 是時間區間的領域概念,phase marker 是它在 chart 上的渲染對應物。CCI / Composer 的 phase 多選 UX 透過 `{adapter}-update-phase-markers` postMessage 觸發 iframe 內 ECharts setOption 重畫 markLine — 詳見 ADR-0003 chart iframe bridge。payload shape `{checkedPhases: [{name, time, pct}]}` 跨兩 adapter 對稱,parent 不持有 chart-internal 知識(targetIdx / nearestLabel 由 iframe customJS 自算)。
_Avoid_: phase line(舊內部變數名,實際上是 markLine + label 組合)、marker(太泛)、phase indicator.

**Gait cycle (CCI)**
CCI 分析把單一 [[Subject]] 的共收縮曲線正規化到的百分比時間軸:**0% = S(啟動瞬間)、100% = L(落地瞬間)**,duration = `L − S` 的 EMG 時間。落地後尾段(延伸到 L+150ms)以 **>100%** 表示、啟動前引段(到 S−150ms)以 **<0%** 表示 — 因 `pct` 公式無 clamp,延伸範圍自然產生 cycle 外百分比。P0/P1/P2 是啟動**前**的準備點,落在此 cycle 之外,不參與 CCI 的百分比軸與 [[Phase marker]] 渲染。S/L 為必填錨點(缺任一 CCI fail-fast)。CCI 的分期視窗統計(`_CCI_Rudolph_phases.csv`,各分期點 ±50/±25ms、前100ms、L 落地後穩定、與分期區間中點±50ms)即定義在此時間軸上。
_Avoid_: gait %(口語)、normalized time、jump cycle(code 用 gait cycle)、把 P0 當 0%(舊行為,[[ADR-0018]] 重錨為 S).

**Manifest**
描述「一場量測」由哪些 EMG 檔、motion 檔與 phase 切點組成的設定檔。CCI、MuscleRatio、PhaseSync 三個分析都先解析 manifest 取得 dataset 集合再計算。V.14 之後新增 `MuscleRatioFile` 欄位（filename only、相對數據資料夾、可空 — 空表示該 subject 跳過肌肉比值來源），供 [[Chart Composer]] 使用；既有四個 analyzer 不消費此欄位，向後相容。
_Avoid_: config, batch file, descriptor, sheet.

**Subject**
[[Manifest]] 一列代表的「一個分析對象」，是所有 [[Analysis pipeline family]] 與 [[Chart Composer]] 的 unit of work。在程式碼裡是 `PhaseManifest.Subject` 字串欄位（首欄）；在 UI 上 CCI / PhaseSync / Chart Composer panel 統一以「分析主題」呈現 — 兩個詞**同義**。Subject 名稱經檔名安全化後成為 muscle_ratio output1 (`{safeSubject}_muscle_ratio.csv`) 等下游檔名的 prefix。
_Avoid_: trial, sample, case, 分析主題（UI label only — 內部以 Subject 為準）.

**Reference EMG**
標準化（Normalize）時作為分母的參考訊號，常見來源是 MVIC（Maximal Voluntary Isometric Contraction）。Normalizer 把主訊號除以 reference 對應 channel 的代表值。
_Avoid_: baseline, MVIC file, reference signal, max reference.

**Max-mean**
滑動視窗演算法在每個 channel 上計算「window 平均值」並取其最大者，輸出 channel 的最佳起訖時間與 max-mean 數值。MaxMeanCalculator 的核心操作。
_Avoid_: window max, rolling average, peak mean.

**Format-aware write**
CSVHandler 對外的一種寫入操作 —— caller 傳入 result struct（MaxMeanResult / EMGDataset / PhaseAnalysisResult）與 WriteRequest，CSVHandler 內部負責 row layout、precision、scaling、merging（多 phase 合一檔）與 sanitize 後寫檔。
與 raw write 對比：raw write 只接受 `[][]string`；format-aware write 接受分析結果結構，CSVHandler 持有 row layout 的真相。
**Filename ownership 隨 unit-of-work 形狀分**：Subject-based write（PhaseSync / NormalizedPhaseSync / CCI / MuscleRatioOutput*）由 CSVHandler 內部從 `result.Subject` + suffix convention 推導（`req.Filename` 被忽略）；File-based write（PhaseAnalysis / MaxMean / Normalized）由 caller 傳入 `req.Filename`。詳見 [[ADR-0004]]。**NormalizedPhaseSync 產兩個 Subject-based 輸出**（標準化 EMG 時序 + 統計），檔名皆由 CSVHandler 推導（見 [[ADR-0020]]）；與 File-based 的 plain **Normalized**（EMGDataset 標準化）是不同概念，勿混。
_Avoid_: structured write, typed write, formatted output.

**WriteRequest**
所有 format-aware write 共用的請求外殼，欄位有 Filename（檔名）、SubDir（可選的 OutputDir 子目錄，空字串 = 寫到 OutputDir 根）、Headers、Data（generic payload）。
_Avoid_: write options, write spec, csv request.

**Domain analyzer**
`internal/{cci, muscle_ratio, phase_sync}` 三個以 [[Manifest]] + dataFolder 為入口的領域計算 orchestrator。每個 analyzer 載入 manifest → 解析 [[Subject]] → parse EMG → 計算該分析種類的領域結果，math 細節下放給 calculator kernel（[[ADR-0005]] calculator family）/ synchronizer / parsers。位於 GUI handler 層（唯一 caller；`AnalyzePhaseSync`/`AnalyzeMuscleRatio` 在 [[Analysis pipeline family]] 內，`AnalyzeCCI` 已 Tier-1 化離開該家族但仍為 `cci` 的 GUI caller）之下、calculator kernel 之上。
三者形狀**刻意分歧**，沿兩條正交軸：
- **Subject cardinality**：`single-subject`（`cci.AnalyzeCCI` / `phase_sync.AnalyzePhaseSync` 吃 [[Subject]] index、回單一 result struct）｜ `batch`（`muscle_ratio.Analyze` 迴圈整份 manifest、回 `[]SubjectResult` partial-success slice）。
- **Output ownership**：`compute-only`（cci / phase_sync 只回 compute struct，CSV 由 GUI handler 寫 — phase_sync 經 Tier-2 `WriteCSV` closure、cci 自 Tier-1 化後在 `HandlerRun` body 內直接呼叫 `csvHandler.WriteCCIResult`/`WriteCCIPhasesResult`）｜ `compute+write`（muscle_ratio 在 analyzer 內部寫 CSV 並回填 path，GUI `WriteCSV: nil` — 見 [[ADR-0004]]）。
membership 判準是 **manifest + dataFolder 驅動**：GUI `AnalyzePhases` 雖在 [[Analysis pipeline family]] 內，但吃單一 raw CSV input file 並委派 `calculator.PhaseAnalyzer`，**不是** domain analyzer；反向地，`cci` 是 domain analyzer，但其 GUI handler `AnalyzeCCI` 已遷為 Tier-1 `HandlerRun`，**不在** [[Analysis pipeline family]] 內（見 [[ADR-0031]]）── 所以即使兩層現在都是 3 member，集合仍不相同。兩軸上的分歧為何刻意保留（deletion test）見 [[ADR-0012]]。
_Avoid_: [[Analysis pipeline family]]（GUI caller 層，非 compute 核）、AnalysisHandler[P, R]（GUI 泛型樣板）、calculator family（被委派的 math kernel，[[ADR-0005]]）、把某個 GUI handler 叫 analyzer（analyzer 專指此 internal 層）、analysis engine（engine 一詞 [[ADR-0008]] 已用於已刪的 chart 引擎）.

**Max-mean batch runner**
以「一個輸入目錄」為入口、對目錄內每一個 raw CSV 檔（各為一筆 [[EMGDataset]]）逐檔跑 [[Max-mean]]、再把各檔結果累積成單一 partial-success 輸出的批次 orchestrator。與 [[Domain analyzer]] **同一層**（GUI handler 之下、calculator kernel 之上）但**不同 category**：unit of work 是**檔案 / [[EMGDataset]]**（目錄探索），而非 [[Subject]]（manifest 驅動）── 因此不符 Domain analyzer 的「manifest + dataFolder 驅動、恰 3 member」判準，也不在 [[Analysis pipeline family]]（該家族明文排除 `CalculateMaxMean`）。兩種輸入目錄形態 ── configured input dir vs external 絕對路徑目錄 ── 是它天生的兩個 file-source 來源。kernel 數學下放給 calculator family 的 `MaxMeanCalculator`（[[ADR-0005]]）。設計與抽出計畫見 [[ADR-0026]]。
_Avoid_: [[Domain analyzer]]（不同 category ── manifest 驅動）、Max-mean analyzer（analyzer 一詞專指 Domain analyzer 層，本概念刻意不叫 analyzer）、batch processor / 批次處理器（太泛、未對齊 domain）、[[Analysis pipeline family]]（GUI handler 層，且該家族不含 `CalculateMaxMean`）。

**Analysis pipeline family**
三個形狀相近的 GUI handler 家族：`AnalyzePhases`、`AnalyzePhaseSync`、`AnalyzeMuscleRatio`。共同形狀為「validate → execute（含 manifest/dataset load）→ CSV write via csvHandler」三步管線，輸入為 manifest 或 dataset、輸出為 result + outputPath。
_Not included_: `CalculateMaxMean`（batch loop + file discovery）、`NormalizeData`（雙 input file）── 形狀不同，不屬此家族。其中 `CalculateMaxMean` 的 orchestration 規劃抽進 [[Max-mean batch runner]]（同層、不同 category）後退化為 thin adapter，見 [[ADR-0026]]。`AnalyzeCCI` 於 2026-06-19 由 Tier-2 `AnalysisHandler` 樣板遷出，改走 Tier-1 `HandlerRun` 直用（六步 body、雙輸出、形狀同 `AnalyzeNormalizedPhaseSync`）── 此後 `AnalyzeCCI` **不屬本家族**；`cci` domain analyzer 仍存在，其 GUI handler 的形狀遷移見 [[ADR-0031]]。
_Backend 委派_: 三 member 中 `AnalyzePhaseSync` / `AnalyzeMuscleRatio` 的 Execute 委派 [[Domain analyzer]] 層（backend compute 核）；`AnalyzePhases` 委派 `calculator.PhaseAnalyzer`（[[ADR-0005]] calculator family）。本家族是 GUI handler 層，與 [[Domain analyzer]] 不同層。
_Avoid_: GUI handler, analysis function, analyzer.

**AnalysisHandler[P, R]**
[[Analysis pipeline family]] 的泛型樣板，以 generic struct + Run method 形式存在。承載 `csvHandler`、`logger` 依賴，接收三個 closure（validate、execute、write CSV）注入差異。Run body 內建 `recoverHandlerPanic`、logger entry/exit、generic error wrapping；不負責 `state.Load`、result transform、i18n，這三項由 caller 在 Run 外處理。
_Avoid_: AnalysisRunner, GenericHandler, AnalyzerWrapper.

**Manifest handler prelude**
manifest+dataFolder 系列 GUI handler 共用入口三步：sentinel empty check（`ErrNoManifestFile` / `ErrNoDataFolder`，manifest 先檢）+ `validateExternalPathInputs` 帶固定 label「分期總檔案」「資料夾」。由 `gui/path_validation.go` 的 `validateManifestHandlerParams(manifestFile, dataFolder string) error` 持有。[[Analysis pipeline family]]（現 3 member）的 manifest 驅動成員 `AnalyzePhaseSync`/`AnalyzeMuscleRatio`（`AnalyzePhases` 不在其內 — 接 InputFile 而非 manifest）、已 Tier-1 化離開家族但仍吃此 prelude 的 `AnalyzeCCI` 與 `AnalyzeNormalizedPhaseSync`、與 [[Chart Composer]] 三個 RPC handler 共 7 處消費。**strict 行為**：Chart Composer 既有對空 `ManifestPath` 的 silent fall-through（落入 `LoadManifests("")`）在此 prelude 落地後同步收緊為 `ErrNoManifestFile` 提早 reject。
_Avoid_: handler entry guard, manifest precheck, path prelude, validate prelude.

**ManifestPanel**
Frontend panel layer 的 deep module,收乾「以 [[Manifest]] + dataFolder 為入口的 panel」共通 boilerplate:panel template、subject dropdown load(含 idx + subject string 兩形態規範化)、RPC envelope(try / `updateStatus` running→done→failed / `ShowMessage` / `ShowError` / reentrant guard)、[[Phase marker]] checkbox render。5 個 member:CCI、PhaseSync、NormalizedPhaseSync、MuscleRatio、[[Chart Composer]]。每個 panel 不再各自重寫 boilerplate,而是傳一個 spec object 給 `ManifestPanel.run` — spec shape:`{ titleKey, statusRunningKey, formBody: (t) => string, rpc: async (ctx) => result, onResult: (result, ctx, mp) => void, silentSuccess?: boolean }`,其中 ctx 為 `{ manifestPath, dataFolder, subjectIdx, subjectName, subjects }`(Composer 取 `subjectName`、其他 4 panel 取 `subjectIdx`)。helpers:`mp.attachIframe`(iframe lifecycle + ready promise,不碰 bridge)、`mp.bindPhaseCheckboxes`(phase whitelist + bridge.subscribe + recalcPercents)、`mp.openOutputFolder`、`mp.registerCleanup`(unsub bag 跨 re-attach 自動 call)。詳見 [[ADR-0007]]。
**與 [[Analysis pipeline family]] 是不同層的 module** — 後者是 Go-side backend handler 家族(compute + CSV);ManifestPanel 是 frontend panel layer。Chart Composer 在 backend 不入 Analysis pipeline family(ADR-0002 §1),但在 frontend 入 ManifestPanel(panel boilerplate 同形)— 兩條規則屬不同軸,不衝突。MaxMean / Normalize / Phase 三個舊 panel 不吃 manifest,不入 ManifestPanel。
_Avoid_: AnalysisPanel(與 backend Analysis pipeline family 字面衝突,且 Chart Composer 不在後者)、SubjectPanel(subject 是 panel 內部 navigation 而非入口)、WorkflowPanel(過泛、無對齊 domain)、panel handler(handler 是 backend 詞)、panel component(component 是泛框架詞,與本 module 的領域定位無關).

**Chart Composer**
Visualization-only feature：讀 [[Manifest]] + 數據資料夾後，把單一 subject 的 EMG / motion / muscle_ratio output1 三類資料同框渲染成三張帶 [[Phase]] 虛線與時期百分比軸的圖。**不計算、不寫 CSV、不產生新的 result struct** —— 與 [[Analysis pipeline family]] 的形狀差異就在這裡：它是 multi-source viewer，不是 analyzer。Phase line / 百分比軸的 UX 機制沿用既有 CCI chart（go-echarts + Wails postMessage），但資料來源不同。
在 UI(panel 標題)以「資料做圖」呈現 — 與 Chart Composer **同義**;canonical code/domain 術語仍為 Chart Composer(比照 [[Subject]] ↔ 分析主題)。「資料做圖」原為舊單檔流程口語名,Composer panel 沿用為標題,故由 _Avoid_ 升為 UI 同義詞。
_Avoid_: data plotting, chart panel, multi-chart viewer.

## Example dialogue

Dev:「Phase analysis 寫 CSV 為什麼要在 caller 端 skip 前 3 row？」

Domain expert:「那是因為 `ConvertPhaseAnalysisToCSV` 每次回的 `[][]string` 是 header + max row + mean row + time-index row 的 4-row 結構。當 caller 要把多個 phase 合進一個檔，必須跳過後續 phase 的 header 跟 time row，避免重複。」

Dev:「也就是 row layout 是 CSVHandler 內部知識，但目前 caller 端被迫知道。」

Domain expert:「對。format-aware write 之後，caller 只給 phases `[]PhaseAnalysisResult` + maxTimeIndex，merging 跟 row skip 全部被 CSVHandler 吸進去。caller 不再看到「row」這個字。」
