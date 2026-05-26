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

## Example dialogue

Dev:「Phase analysis 寫 CSV 為什麼要在 caller 端 skip 前 3 row？」

Domain expert:「那是因為 `ConvertPhaseAnalysisToCSV` 每次回的 `[][]string` 是 header + max row + mean row + time-index row 的 4-row 結構。當 caller 要把多個 phase 合進一個檔，必須跳過後續 phase 的 header 跟 time row，避免重複。」

Dev:「也就是 row layout 是 CSVHandler 內部知識，但目前 caller 端被迫知道。」

Domain expert:「對。format-aware write 之後，caller 只給 phases `[]PhaseAnalysisResult` + maxTimeIndex，merging 跟 row skip 全部被 CSVHandler 吸進去。caller 不再看到「row」這個字。」
