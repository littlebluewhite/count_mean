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

**Manifest**
描述「一場量測」由哪些 EMG 檔、motion 檔與 phase 切點組成的設定檔。CCI、MuscleRatio、PhaseSync 三個分析都先解析 manifest 取得 dataset 集合再計算。
_Avoid_: config, batch file, descriptor, sheet.

**Reference EMG**
標準化（Normalize）時作為分母的參考訊號，常見來源是 MVIC（Maximal Voluntary Isometric Contraction）。Normalizer 把主訊號除以 reference 對應 channel 的代表值。
_Avoid_: baseline, MVIC file, reference signal, max reference.

**Max-mean**
滑動視窗演算法在每個 channel 上計算「window 平均值」並取其最大者，輸出 channel 的最佳起訖時間與 max-mean 數值。MaxMeanCalculator 的核心操作。
_Avoid_: window max, rolling average, peak mean.

**Format-aware write**
CSVHandler 對外的一種寫入操作 —— caller 傳入 result struct（MaxMeanResult / EMGDataset / PhaseAnalysisResult）與 WriteRequest，CSVHandler 內部負責 row layout、precision、scaling、merging（多 phase 合一檔）與 sanitize 後寫檔。
與 raw write 對比：raw write 只接受 `[][]string`；format-aware write 接受分析結果結構，CSVHandler 持有 row layout 的真相。
_Avoid_: structured write, typed write, formatted output.

**WriteRequest**
所有 format-aware write 共用的請求外殼，欄位有 Filename（檔名）、SubDir（可選的 OutputDir 子目錄，空字串 = 寫到 OutputDir 根）、Headers、Data（generic payload）。
_Avoid_: write options, write spec, csv request.

**Analysis pipeline family**
四個形狀相近的 GUI handler 家族：`AnalyzePhases`、`AnalyzePhaseSync`、`AnalyzeCCI`、`AnalyzeMuscleRatio`。共同形狀為「validate → execute（含 manifest/dataset load）→ CSV write via csvHandler」三步管線，輸入為 manifest 或 dataset、輸出為 result + outputPath。`AnalyzeMuscleRatio` 是 batch 變體，CSV write 暫時摺進 execute 內（per-subject），對應 [[AnalysisHandler[P, R]]] 的 WriteCSV closure 設為 nil；候選 2 推進時可把 write 上移、closure 從 nil 補回實作。
_Not included_: `CalculateMaxMean`（batch loop + file discovery）、`NormalizeData`（雙 input file）、`AnalyzeNormalizedPhaseSync`（multi-step：load → resolve×2 → normalize → write1 → range → stats → write2，雙 output、跨 step ctx 檢查）── 形狀不同，不屬此家族；這些 handler 直接走 [[HandlerRun]]，繞過 Tier 2 樣板。
_Avoid_: GUI handler, analysis function, analyzer.

**HandlerRun**
所有 Wails GUI handler 共吃的 cross-cutting wrapper：吸收 `recoverHandlerPanic`（panic safety via named-return）+ logger entry/exit（「開始 X」/「X 完成」）+ 純 (R, error) 透傳。Contract-neutral —— body 回什麼就傳什麼，不強制 single-channel。Signature：`HandlerRun[R any](logger, name string, body func() (R, error)) (R, error)`。不注入 ctx；body 自己 close over `a.context()`。是 [[AnalysisHandler[P, R]]] 的底層；不在 [[Analysis pipeline family]] 的 handler（例如 `AnalyzeNormalizedPhaseSync`）直接呼叫此 wrapper。
_Avoid_: HandlerWrapper, HandlerBoilerplate, RunWithRecover.

**AnalysisHandler[P, R]**
[[Analysis pipeline family]] 的泛型樣板，以 generic struct + Run method 形式存在。承載三個 field（`Name`、`Logger`、`CSV *io.CSVHandler`）與三個 closure：`Validate(P) error`（required）、`Execute(ctx, P) (R, error)`（required；樣板注入 ctx 給 Execute）、`WriteCSV(*io.CSVHandler, R) (outputPath, error)`（**optional**；nil 時 skip，對應 [[Analysis pipeline family]] batch 變體把 write 摺進 execute 的情況）。Run signature：`Run(ctx, params) (result R, outputPath string, err error)`。內部委派給 [[HandlerRun]] 拿 panic recovery + logger entry/exit；不負責 `state.Load`、result transform、i18n，這三項由 caller 在 Run 外處理。
_Avoid_: AnalysisRunner, GenericHandler, AnalyzerWrapper.

## Example dialogue

Dev:「Phase analysis 寫 CSV 為什麼要在 caller 端 skip 前 3 row？」

Domain expert:「那是因為 `ConvertPhaseAnalysisToCSV` 每次回的 `[][]string` 是 header + max row + mean row + time-index row 的 4-row 結構。當 caller 要把多個 phase 合進一個檔，必須跳過後續 phase 的 header 跟 time row，避免重複。」

Dev:「也就是 row layout 是 CSVHandler 內部知識，但目前 caller 端被迫知道。」

Domain expert:「對。format-aware write 之後，caller 只給 phases `[]PhaseAnalysisResult` + maxTimeIndex，merging 跟 row skip 全部被 CSVHandler 吸進去。caller 不再看到「row」這個字。」
