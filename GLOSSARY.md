# count_mean 領域語言

EMG 肌電訊號分析工具的領域概念字典。架構詞彙（module / interface / seam / depth / leverage / locality 等）見 `improve-codebase-architecture` skill 的 LANGUAGE.md，這裡只列領域 specific 的概念。

## Language

**EMGDataset**
一筆完整的肌電訊號量測，包含 headers（通道名稱）、time-series rows、與 OriginalTimePrecision。[[Max-mean]]、Phase analysis 與 Normalize 等 calculator 路徑先把輸入解析為 EMGDataset；以 [[Manifest]] 為入口的 [[Domain analyzer]] 與 NPS（NormalizedPhaseSync）直接使用 [[PhaseSyncEMGData]]，不經 EMGDataset；[[Chart Composer]] 也是（`chart.ComposerInput.EMG` 直接吃 PhaseSyncEMGData，[[ADR-0046]]）。
_Avoid_: data file, signal data, EMG records.

**PhaseSyncEMGData**
[[Domain analyzer]]（CCI / MuscleRatio / PhaseSync）、NormalizedPhaseSync 與 [[Chart Composer]] 使用的 EMG 資料形狀（`models.PhaseSyncEMGData`）：以**秒**為時間域、**columnar** 排列 —— 一條 `Time []float64` 加 `Channels map[通道名]→[]float64` 與 `Headers` 通道順序。與逐 row 記錄的 [[EMGDataset]] 是不同結構（兩者之間沒有橋接，不可互換）：切片 [[Phase]] 區間時直接以秒定位，不經 row 迭代。
_Avoid_: 把它當成 EMGDataset 的別名.

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
CCI 分析把單一 [[Subject]] 的共收縮曲線正規化到的百分比時間軸:**0% = S(啟動瞬間)、100% = L(落地瞬間)**,duration = `L − S` 的 EMG 時間。落地後尾段(延伸到 L+150ms)以 **>100%** 表示、啟動前引段(到 S−150ms)以 **<0%** 表示 — 因 `pct` 公式無 clamp,延伸範圍自然產生 cycle 外百分比。P0/P1/P2 是啟動**前**的準備點,落在此 cycle 之外,不參與 CCI 的百分比軸與 [[Phase marker]] 渲染。S/L 為必填錨點(缺任一 CCI fail-fast)。百分比與其不變式由 `*CCIAnalysisResult` 持有:`GaitPercent(t)`(不 clamp)換算,`Validate()`(非 nil、gait 時間有限且 duration > 0、每個 pair 長度等於 `TimeValues`)把關,computeCCI 出口、`io.WriteCCIResult`、CCI chart 共用。見 [[ADR-0053]]。CCI 的分期視窗統計(`_CCI_Rudolph_phases.csv`,各分期點 ±50/±25ms、前100ms、L 落地後穩定、與分期區間中點±50ms)即定義在此時間軸上。
_Avoid_: gait %(口語)、normalized time、jump cycle(code 用 gait cycle)、把 P0 當 0%(舊行為,[[ADR-0018]] 重錨為 S).

**Scaled domain**
縮放域:資料時間與 EMG 值經 `util.Str2Number` 解析後已乘 10^scalingFactor 的數值域；相對地，使用者輸入的秒與 CSV 輸出的原單位屬原域。兩域之間的換算由 `calculator.UnitScale` 單一持有：`ToScaled(v) = v × 10^sf`（原域→縮放域，例如把前端 phase range 轉到與 `dataset.Time` 同域再比較）、`FromScaled(v) = v ÷ 10^sf`（縮放域→原域，例如寫 CSV 前）。`scalingFactor` 於 calculator ctor 固定，不 per-call 注入。`util.Str2Number` 因 util 不能 import calculator 而自帶同一乘法，由測試釘住兩者一致。見 [[ADR-0049]]。
_Avoid_: 直接在呼叫端寫 `math.Pow10(scalingFactor)`、scaled-time 域（舊註解用語）、微秒域（已移除的 ×1e6 路徑）.

**Phase timeline**
一筆 [[Manifest]] row(一個 [[Subject]])的分期點在 EMG 時間軸上的位置:`synchronizer.NewPhaseTimeline(row)` 依 canonical 順序(`models.AllPhases()`,P0 → L)列出**已提供**的分期點與其 EMG 秒數,`At(p)` 查單點。是否提供由 `parsers.GetPhaseValue` 判斷(力板時間 `OptFloat` Set=false、motion-index D/O ≤ 0 為未提供);換算:力板時間 `emg = t − (EMGMotionOffset − 1) / 250`,motion-index `emg = (idx − EMGMotionOffset) / 250`,不設 motion-index 上限 guard(parser 已擋)。它是「分期點 → EMG 秒數」的唯一 owner;各 caller 只保留自己的 policy —— CCI 排除 P0–P2 並要求 S/L([[Gait cycle (CCI)]])、MuscleRatio 檢查 in-range 後排序加中點、[[Chart Composer]] 全部渲染成 [[Phase marker]]、PhaseSync 取一對分期點。見 [[ADR-0042]]。
_Avoid_: synced time range(已刪的 `GetSyncedTimeRange`)、phase times map(那是 Composer / CCI 的輸出形狀,不是來源)、在 caller 內自行判斷力板時間 / motion-index 再換算.

**EMG time axis**
[[PhaseSyncEMGData]] 的 `Time`(秒,升冪)。[[Phase timeline]] 的 EMG 秒數落在這條軸上時只有兩個操作,都由 `synchronizer` 持有:**in-range** —— `synchronizer.OutsideEMG(times, t)` 以 ±`emgTimeEpsilon`(1e-6,吸收力板 ↔ EMG 同步後的 ULP 飄移)判斷 t 在首 / 末筆外的哪一側(`ResolveTimeIndex` 的 inRange 由它推導);**切片** —— `synchronizer.SliceEMG(data, start, end)` 把 start、end 與每筆 sample 都取整到整數毫秒,取含端點的 `[startMs, endMs]`。切片刻意不用 ±ε:manifest 力板時間是毫秒精度、以 float32 匯出,印到 6 位小數的雜訊從 1e-6 起跳並隨時間變大,±ε 會靜默少切邊界 sample。通過 in-range 檢查的端點,其邊界 sample 一定被切入。越界時是 fail、drop 還是 skip,由各 analyzer 自己決定。見 [[ADR-0030]]、[[ADR-0043]]。
_Avoid_: 切片用 ±ε、caller 內私有的 epsilon、strict-0 邊界比較、在 caller 內自行毫秒取整切片.

**Manifest**
描述「一場量測」由哪些 EMG 檔、motion 檔與 phase 切點組成的設定檔。CCI、MuscleRatio、PhaseSync 三個分析與 [[Chart Composer]] 都先經 `manifest.LoadManifests` 解析 manifest 取得 row 集合，再經 [[Subject source]] 載入該列的 EMG。CCI、PhaseSync、NormalizedPhaseSync 與 [[Chart Composer]] 以 `manifest.LoadSubject(path, name)` 依名稱取一列：找不到回 `manifest.ErrSubjectNotFound`（輸入錯誤），取得時以 `ValidatePhaseManifest` 驗該列、失敗包 `manifest.ErrSubjectRowInvalid`；`LoadSubjectNames` 依 manifest 順序列出非空 Subject，是 gui `LoadManifestSubjects`（四個 panel 共用的 subject 下拉來源，失敗走 result 通道）唯一的資料來源；MuscleRatio 跑全部列，仍直接用 `LoadManifests`（[[ADR-0057]]）。`LoadManifests` 是分期總檔案的讀檔門：`ValidateExternalPath` → `filename.ValidateName(Base)` → `fsperm.OpenRegularLimited`（一般檔案、≤100 MB，讀取中變大也封頂）→ 解析（不檢查編碼，V.16 為 Big5）→ 撞名檢查：兩列 [[Subject]] 的 [[Subject output key]] 相同即整份拒絕（見 [[ADR-0056]]）。`MotionFile` / `ForceFile` 只有會開該檔的 consumer 要求非空：PhaseSync 兩者都要求，[[Chart Composer]] 要求 `MotionFile`（`internal/composer/load.go` 的 `ErrMotionFileEmpty`），CCI 與 MuscleRatio 兩者都不要求（[[ADR-0045]]）。V.14 之後新增 `MuscleRatioFile` 欄位（filename only、相對數據資料夾、可空 — 空表示該 subject 跳過肌肉比值來源），供 [[Chart Composer]] 使用；既有三個 analyzer 不消費此欄位（僅 [[Chart Composer]] 使用），向後相容。
_Avoid_: config, batch file, descriptor, sheet.

**Subject**
[[Manifest]] 一列代表的「一個分析對象」，是 [[Domain analyzer]]、NormalizedPhaseSync 與 [[Chart Composer]] 的 unit of work。在程式碼裡是 `PhaseManifest.Subject` 字串欄位（首欄）；在 UI 上 CCI / PhaseSync / Chart Composer panel 統一以「分析主題」呈現 — 兩個詞**同義**。Subject 名稱經檔名安全化後成為 muscle_ratio output1 (`{safeSubject}_muscle_ratio.csv`) 等下游檔名的 prefix。同一份 manifest 內，Subject 的 [[Subject output key]] 必須唯一，否則 `LoadManifests` 整份拒絕（[[ADR-0056]]），所以 Subject 名稱是 manifest row 的唯一鍵：CCI / PhaseSync / NormalizedPhaseSync / [[Chart Composer]] 四個 panel 的下拉都來自 gui `LoadManifestSubjects`（`manifest.LoadSubjectNames`：非空 Subject、manifest 順序，空 Subject 的列不列出——它們本來就無法分析），選定的名稱經 RPC `subject` 字串送回，後端 `manifest.LoadSubject` 以 rune 序列比對名稱取 row（無效 UTF-8 與 JSON 一樣換成 U+FFFD，Big5 Subject 可往返）並在取得時驗證該列（[[ADR-0057]]）。
_Avoid_: trial, sample, case, 分析主題（UI label only — 內部以 Subject 為準）, subject index / SubjectIndex（已退役，見 [[ADR-0057]]）.

**Subject output key**
[[Subject]] 在 macOS / Windows 上的輸出檔名身分：`filename.SubjectOutputKey(subject)` = NFC(ToLower(Sanitize(subject)))，與 `SubjectOutputName` 同在 filename 套件，Sanitize 規則改了兩者一起改。兩個 Subject 的 key 相同 ⇔ 它們推導出的輸出檔（CCI、PhaseSync、NormalizedPhaseSync、MuscleRatio、Composer PNG）是同一個檔。`manifest.LoadManifests` 以它做撞名檢查：兩列 key 相同即整份 manifest 拒絕 —— `A/B` 與 `A:B`、`SF8` 與 `sf8`、兩列空 Subject 都算（[[ADR-0056]]）；Subject 名稱因此可當 manifest row 的唯一鍵（[[ADR-0057]]）。已知限制：manifest 不解碼，Big5 中文 Subject 逐 byte 變成 U+FFFD，不同的中文名可能得到同一個 key，整份 manifest 被拒；處置是另存為 UTF-8（撞名訊息會提示），解碼 Big5 是可能的後續工作。
_Avoid_: sanitized subject / safeSubject（`Sanitize` 的輸出字串本身，不含大小寫與 NFC 摺疊）、subject id.

**Subject source**
`internal/manifest` 持有的「[[Manifest]] 一列 + 資料夾 → 該 [[Subject]] 的 EMG」入口：`manifest.LoadEMG(dataFolder, row)` 經 `OpenDataFile`（硬化讀檔門：`ValidateName` 檢查 base 檔名、lenient 解析 + validated-open、fstat 一般檔案且 ≤100 MB，[[ADR-0056]]）開檔、`EMGParser` 解析、關檔，回傳 [[PhaseSyncEMGData]]。開檔失敗原樣回 `OpenDataFile` 的錯誤；解析失敗包成 `*manifest.EMGParseError`（`Error()` 與內層相同），caller 以 `errors.As` 區分兩階段並保有自己的失敗 policy（CCI fail-fast、MuscleRatio per-subject、Composer 轉 UI 訊息）。不攜帶取樣頻率（與 CCI 的 sample interval 估計同源）。見 [[ADR-0044]]。
_Avoid_: EMG loader, data source.

**Reference EMG**
標準化（Normalize）時作為分母的參考訊號，常見來源是 MVIC（Maximal Voluntary Isometric Contraction）。Normalizer 把主訊號除以 reference 對應 channel 的代表值。
_Avoid_: baseline, MVIC file, reference signal, max reference.

**Max-mean**
滑動視窗演算法在每個 channel 上計算「window 平均值」並取其最大者，輸出 channel 的最佳起訖時間與 max-mean 數值。MaxMeanCalculator 的核心操作。
**範圍語意**：可限定時間範圍（`calculator.MaxMeanRange`，原域秒），起點、終點各自可省略 —— 沒給起點從資料首樣本算起，沒給終點算到資料末樣本，兩端都沒給 = 整檔；`0` 是合法邊界值，不代表「未給」。輸出 CSV 的「開始/結束範圍秒數」列印實際採用的範圍：有給的那端是使用者的值，沒給的那端是實際首 / 末樣本時間。範圍語意由 calculator 單獨持有（[[ADR-0050]]）。
_Avoid_: window max, rolling average, peak mean.

**Format-aware write**
CSVHandler 對外的一種寫入操作 —— caller 傳入 result struct（MaxMeanResult / EMGDataset / PhaseAnalysisResult / PhaseSync、CCI、MuscleRatio 結果），CSVHandler 內部負責 row layout、precision、scaling、merging（多 phase 合一檔）與 sanitize，再交給 [[Output placement]] 落檔。
與 raw write 對比：raw write 只接受 `[][]string`（File-based 的 `WriteFileCSV`）；format-aware write 接受分析結果結構，CSVHandler 持有 row layout 的真相。
**Filename ownership 隨 unit-of-work 形狀分**：Subject-based write（PhaseSync / NormalizedPhaseSync / CCI / MuscleRatioOutput*）由 CSVHandler 從 `result.Subject` + suffix convention 推導，不收 [[WriteRequest]]、一律寫在 OutputDir 根；File-based write（PhaseAnalysis / MaxMean / Normalized）由 caller 以 [[WriteRequest]] 傳入檔名（與可選的 SubDir）。詳見 [[ADR-0004]]。**NormalizedPhaseSync 產兩個 Subject-based 輸出**（標準化 EMG 時序 + 統計），檔名皆由 CSVHandler 推導（見 [[ADR-0020]]）；與 File-based 的 plain **Normalized**（EMGDataset 標準化）是不同概念，勿混。
**回傳實際寫入的路徑**：兩種命名都經 [[Output placement]]，回傳值就是落檔位置；檔案路徑一律不 URL-decode（`%`、`+` 為字面字元，見 [[ADR-0039]]）。
_Avoid_: structured write, typed write, formatted output.

**User-picked CSV read**
讀取使用者在 GUI 選取之 CSV 的唯一入口 `CSVHandler.ReadCSV`：base filename 驗證 → `ValidateExternalPath` → `fsperm.OpenRegularLimited`（pre-stat → `fsperm.ReadFlags` 開檔 → fstat：regular file、≤100MB；讀取中變大也封頂，與分期總檔案共用，[[ADR-0056]]）→ 解析 → 內容驗證。路徑不 URL-decode。見 [[ADR-0039]]。
與 manifest-driven 的 lenient 讀取（`OpenLenientValidated`）不同：後者的檔名來自 manifest、相對於 baseFolder。
_Avoid_: raw read, direct open.

**Output placement**
所有輸出（CSV 與 PNG）落檔的唯一步驟 `CSVHandler.placeOutput`：containment join（`safeJoinOutput`）→ 依格式驗檔名（CSV 用 `ValidateFilename`，PNG 用 `ValidateName`）→ `ValidateExternalPath`（敏感位置，含 symlink resolve）→ `MkdirAll`（先驗證、後建目錄）→ `fsperm.AtomicWriteFile`（crypto tmp → fsync → rename，parent 以 dirfd 錨定在 allow-list 之下），回傳實際寫入路徑。格式由 payload 帶：`csvPayload`（BOM 照 `config.BOMEnabled`、formula 逸出）、`pngPayload`（位元組原樣）。三組入口共用這一道門：File-based CSV（`WriteFileCSV`，檔名與 SubDir 由 [[WriteRequest]] 給）、Subject-based CSV（`placeSubjectOutput`，檔名 `SubjectOutputName(subject, suffix)`，寫在 OutputDir 根）、Subject PNG（`WriteSubjectPNG`，只收已解碼並驗證過的位元組）。
leaf 是 symlink 時 rename 取代連結本身、不跟到目標；寫到一半失敗不動既有檔，crash 可能留下 `*.tmp.<hex>`。錯誤文字固定為「輸出路徑無效」「檔案名稱驗證失敗」「輸出目錄建立失敗」。PHI 性質依分支而異：containment 失敗是固定哨兵（不帶 SubDir）；`MkdirAll` 失敗只包底層 errno（不帶目錄路徑）；`ValidateExternalPath` 失敗以 `%w` 包 validator error，只帶輸出檔名：validator 建錯誤時就以 `redact.FilePath` 帶入被拒路徑，`Error()` 與 log 都只有檔名（[[ADR-0059]]）。見 [[ADR-0055]]（取代 [[ADR-0040]] 只管 Subject-based 的版本）。
_Avoid_: subject output placement, phase-sync atomic write, output dir validation, writeFileOutput.

**Read sanity / write escape**
驗證詞彙依 context 分開：讀端（`ValidateCSVData`、`ReadCSVRecords`、manifest parser）只做 sanity —— 結構、cell ≤32KB（`CheckCells`），user-picked pipeline 另加 UTF-8；不判斷內容像不像攻擊。注入防禦只在寫端，由 `csvutil` 逸出 formula starter。filename 驗證只擋檔案系統非法（`<>:"|?*`、控制字元、分隔符、磁碟代號、保留裝置名）。見 [[ADR-0041]]。檔名規則只有 `validation/filename` 一個裁判（`ValidateName`，保留名逐個點分段比對），security 只管路徑位置、不檢查檔名（[[ADR-0054]]）。
_Avoid_: injection detection on read, dangerous-pattern registry.

**WriteRequest**
File-based write（MaxMean / Normalized / PhaseAnalysis，以及 raw `WriteFileCSV`）的請求外殼，欄位只有 Filename（caller 給的檔名，須為 `.csv`）與 SubDir（可選的 OutputDir 子目錄，空字串 = 寫到 OutputDir 根；MaxMean batch 用它寫進 `OutputDir/<batchName>/`）。Subject-based write 不收它 —— 檔名由 subject 推導、一律寫在 OutputDir 根（[[ADR-0055]]）。headers 與資料由各 write 方法的 result struct 提供。
_Avoid_: write options, write spec, csv request.

**Domain analyzer**
`internal/{cci, muscle_ratio, phase_sync}` 三個以 [[Manifest]] + dataFolder 為入口的領域計算 orchestrator。每個 analyzer 載入 manifest → 解析 [[Subject]] → parse EMG → 計算該分析種類的領域結果，math 細節下放給 calculator kernel（[[ADR-0005]] calculator family）/ synchronizer / parsers。位於 GUI handler 層之下、calculator kernel 之上；caller 只在 GUI handler 層 —— 各 analyzer 的完整分析入口由 `AnalyzeCCI` / `AnalyzePhaseSync` / `AnalyzeNormalizedPhaseSync` / `AnalyzeMuscleRatio` 呼叫；subject 清單不經 analyzer（gui `LoadManifestSubjects` 直接用 `manifest.LoadSubjectNames`，[[ADR-0057]]）。
phase_sync 有**兩個入口**：`AnalyzePhaseSync`（單一分期區間的統計）與 `AnalyzeNormalizedPhaseSync`（NPS：先以 Norm 區間各通道最大值標準化整段 EMG，再算 Stats 區間的統計），共用 load 與 compute core（`computePhaseSync`：區間解析 → 切片 → 統計）。NPS **不是第 4 個 member** —— 同樣 manifest + dataFolder 驅動、single-subject、compute-only（Output 1 / 2 仍由 GUI handler 經 CSVHandler 寫），只是 phase_sync 的第二個入口（[[ADR-0047]]）。
三者形狀**刻意分歧**，沿兩條正交軸：
- **Subject cardinality**：`single-subject`（`cci.AnalyzeCCI` / `phase_sync.AnalyzePhaseSync` / `phase_sync.AnalyzeNormalizedPhaseSync` 吃 [[Subject]] 名稱、回單一 result struct）｜ `batch`（`muscle_ratio.Analyze` 迴圈整份 manifest、回 `[]SubjectResult` partial-success slice）。
- **Output ownership**：`compute-only`（cci / phase_sync 只回 compute struct，CSV 由 GUI handler 經 CSVHandler 寫）｜ `compute+write`（muscle_ratio 在 analyzer 內部寫 CSV 並回填 path — 見 [[ADR-0004]]）。
membership 判準是 **manifest + dataFolder 驅動**：GUI `AnalyzePhases` 吃單一 raw CSV input file 並委派 `calculator.PhaseAnalyzer`，**不是** domain analyzer。兩軸上的分歧為何刻意保留（deletion test）見 [[ADR-0012]]。
_Avoid_: Analysis pipeline family / AnalysisHandler[P, R]（已退役的 GUI handler 家族名與泛型樣板，見 [[ADR-0035]]）、calculator family（被委派的 math kernel，[[ADR-0005]]）、把某個 GUI handler 叫 analyzer（analyzer 專指此 internal 層）、analysis engine（engine 一詞 [[ADR-0008]] 已用於已刪的 chart 引擎）.

**Max-mean batch runner**
以「一個輸入目錄」為入口、對目錄內每一個 raw CSV 檔（各為一筆 [[EMGDataset]]）逐檔跑 [[Max-mean]]、經 `ResultWriter` 各自寫出一份輸出 CSV 的批次 orchestrator；只累積成敗計數（`BatchResult{SuccessCount, FailCount}`），單檔失敗記 log 後繼續下一檔（partial-success）。與 [[Domain analyzer]] **同一層**（GUI handler 之下、calculator kernel 之上）但**不同 category**：unit of work 是**檔案 / [[EMGDataset]]**（目錄探索），而非 [[Subject]]（manifest 驅動）── 因此不符 Domain analyzer 的「manifest + dataFolder 驅動、恰 3 member」判準。兩種輸入目錄形態 ── configured input dir vs external 絕對路徑目錄 ── 是它天生的兩個 file-source 來源。kernel 數學下放給 calculator family 的 `MaxMeanCalculator`（[[ADR-0005]]）。設計與抽出計畫見 [[ADR-0026]]。
_Avoid_: [[Domain analyzer]]（不同 category ── manifest 驅動）、Max-mean analyzer（analyzer 一詞專指 Domain analyzer 層，本概念刻意不叫 analyzer）、batch processor / 批次處理器（太泛、未對齊 domain）。

**Manifest handler prelude**
manifest+dataFolder 系列 GUI handler 共用入口三步：sentinel empty check（`ErrNoManifestFile` / `ErrNoDataFolder`，manifest 先檢）+ 路徑驗證，帶固定 label「分期總檔案」「資料夾」：manifest 走檔案語意（`validateExternalPathInputs`），dataFolder 走目錄語意（`validateExternalDirInput` → `ValidateExternalDir`），後者連 `/etc`、`~/.ssh` 這類敏感目錄本身也擋（[[ADR-0038]]）。由 `gui/path_validation.go` 的 `validateManifestHandlerParams(manifestFile, dataFolder string) error` 持有。消費者為 `AnalyzePhaseSync`、`AnalyzeMuscleRatio`、`AnalyzeCCI`、`AnalyzeNormalizedPhaseSync` 與 [[Chart Composer]] 的 `GenerateChartComposer`。subject 清單的 `LoadManifestSubjects` 不讀 dataFolder，只走 manifest 那一半（`ErrNoManifestFile` + `validateExternalPathInputs("分期總檔案", …)`，[[ADR-0057]]）；`AnalyzePhases` 接 InputFile 而非 manifest，不吃此 prelude。**strict 行為**：Chart Composer 既有對空 `ManifestPath` 的 silent fall-through（落入 `LoadManifests("")`）在此 prelude 落地後同步收緊為 `ErrNoManifestFile` 提早 reject。
_Avoid_: handler entry guard, manifest precheck, path prelude, validate prelude.

**Webview envelope**
Go 端文字進 Wails webview 的兩條出口，各有唯一的 redact 點：**err 通道**（bound method 回的 Go err，Wails 以 `err.Error()` 序列化成 rejected promise）在 `recoverHandlerPanic` 出口先 `i18n.Localize` 再過 `redact.Paths`（`renderForWebview`；文字沒變時回原 err，否則包成 `webviewErr`：`Error()` 回在地化、脫敏後的文字，`Unwrap()` 保留原 chain）；**Message 通道**（failed result 的 `Message` 與 `MuscleRatioSubjectDTO.Error`）的失敗文字只能經 `gui/envelope.go` 建構 —— `failMessage(key, err)`（可預期失敗：i18n 前綴 + 在地化後脫敏的 err，記一筆 Error log）、`inputMessage(err)`（驗證 sentinel：只在地化、脫敏，不 log）、`redactText(err)`（其他字串欄位；nil 回空字串）。AST 測試只檢查兩件事：`failed*Result` 的引數限字串字面值 / `failMessage` / `inputMessage`，以及 gui 內只有 `envelope.go` 與 `recover.go` 可 import redact 套件；成功分支的 Message 由 handler 直接組、不經 redact，不得帶目錄路徑。i18n 規則：handler 層 localize —— 前綴用 `error.handler.*` key，err 文字經 `i18n.Localize`（三個 helper 都先在地化、後脫敏）；[[Domain analyzer]] 不讀 locale，回帶 i18n key 的 `i18n.Error`（`Error()` 固定 zh-TW，log 跨 locale 一致）或 sentinel。err 通道出口同樣先在地化、後脫敏（[[ADR-0058]]）。**路徑**：錯誤建構點自己內插的路徑以 `redact.FilePath` / `redact.DirPath` 帶入（[[ADR-0059]]）—— 檔案只留檔名、資料夾整條換成 `<redacted-path>/`，policy 在型別的 `String()`，`Error()`、`Localize` 與 log 都套用；sink-side `redact.Paths` 是其餘文字（stdlib `*PathError`、panic、未轉換的站點）的安全網。**限制**：sink-side 脫敏保留路徑末段（檔名或資料夾名），這類文字以病患資料夾名結尾時仍露出該名字，故 fsperm 也保留 source-side `redactBasePaths`；不符 redact 目錄段文法的目錄段也原文留存 —— POSIX 段含 `"`、tab、詞頭尾的 `'` 或 `:`（`'Jane'`、`Study: Phase 1`）或以空白開頭 / 結尾時只留該段（POSIX 分支在下一個 `/` 重新起始）；drive-letter / UNC 以 `\` 分隔時，不符文法的段（真實 Windows 路徑上只有以空白開頭的段）與其後到末段前的所有目錄段都留存。空白分隔的詞、POSIX 詞中的 `'` / `:` 與任何 `\`、Windows 詞任何位置的 `'`、`%q` 跳脫的路徑都會脫敏。log 端 `sanitizeMessage` 先 `redact.Paths` 再跳脫控制字元，text 格式的 `k=v` 也不對已跳脫的 value 重跑 `Paths`（`Paths` 要看到原始換行，且不得對已脫敏 / 已跳脫的文字再跑 `Paths`，它不是冪等的）。詳見 [[ADR-0036]]、[[ADR-0048]]、[[ADR-0058]]、[[ADR-0059]]。
_Avoid_: RPC envelope（[[ManifestPanel]] 的 frontend try / `updateStatus` / `ShowError` 外殼，不同層）、UIError（已退役的 gui error 型別，見 [[ADR-0035]]）、error formatter（Wails `ErrorFormatter` 是被否決的方案）、redact wrapper.

**ManifestPanel**
Frontend panel layer 的 deep module,收乾「以 [[Manifest]] + dataFolder 為入口的 panel」共通 boilerplate:panel template、subject dropdown load(有 subject 的 4 個 spec 都設 `loadSubjects: loadManifestSubjects`,同檔 export:呼 `LoadManifestSubjects`、`!success` 時 throw;清單只依賴 manifest,選 dataFolder 不 reload;option.value 為 Subject 名稱)、RPC envelope(try / `updateStatus` running→done→failed / `ShowMessage` / `ShowError` / reentrant guard)、[[Phase marker]] checkbox render。5 個 member:CCI、PhaseSync、NormalizedPhaseSync、MuscleRatio、[[Chart Composer]]。每個 panel 不再各自重寫 boilerplate,而是傳一個 spec object 給 `ManifestPanel.run` — spec shape:`{ titleKey, statusRunningKey, formBody: (t) => string, rpc: async (ctx) => result, onResult: (result, ctx, mp) => void, silentSuccess?: boolean }`,其中 ctx 為 `{ manifestPath, dataFolder, subjectName, subjects }`(有 subject 的 4 panel 都取 `subjectName`,[[ADR-0057]] 取代 [[ADR-0007]] §4 的 idx / name 兩形態)。helpers:`mp.attachIframe`(iframe lifecycle + ready promise,不碰 bridge)、`mp.bindPhaseCheckboxes`(phase whitelist + bridge.subscribe + recalcPercents)、`mp.openOutputFolder`、`mp.registerCleanup`(unsub bag 跨 re-attach 自動 call)。詳見 [[ADR-0007]]。
**與 backend 分析 handler 是不同層的 module** — 後者是 Go-side Wails handler(compute + CSV);ManifestPanel 是 frontend panel layer。Chart Composer 在 backend 不計算、不寫 CSV(ADR-0002 §1),但在 frontend 入 ManifestPanel(panel boilerplate 同形)— 兩條規則屬不同軸,不衝突。MaxMean / Normalize / Phase 三個舊 panel 不吃 manifest,不入 ManifestPanel。
_Avoid_: AnalysisPanel(Chart Composer 不是分析,且易與 backend 分析 handler 混淆)、SubjectPanel(subject 是 panel 內部 navigation 而非入口)、WorkflowPanel(過泛、無對齊 domain)、panel handler(handler 是 backend 詞)、panel component(component 是泛框架詞,與本 module 的領域定位無關).

**Chart Composer**
Visualization-only feature：讀 [[Manifest]] + 數據資料夾後，把單一 subject 的 EMG / motion / muscle_ratio output1 三類資料同框渲染成三張帶 [[Phase]] 虛線與時期百分比軸的圖。**不計算、不寫 CSV、不產生新的 result struct** —— 與分析 handler 的形狀差異就在這裡：它是 multi-source viewer，不是 analyzer。Phase line / 百分比軸的 UX 機制沿用既有 CCI chart（go-echarts + Wails postMessage），但資料來源不同。
資料組裝由 `internal/composer` 持有：`composer.Load(manifestPath, dataFolder, subject)` 以 `manifest.LoadSubject` 取 Subject 的 row（取得時驗證該列），經 [[Subject source]] 載入 EMG（columnar [[PhaseSyncEMGData]] 原樣交給 chart、渲染全部通道）、motion-index 換到 EMG 時間軸、以 `io.ReadMuscleRatioOutputAll`（與 Output-1 writer 同在 `internal/io`）讀 muscle_ratio output1（`MuscleRatioFile` 非空才載）、[[Phase timeline]] 轉成 phase 名 → EMG 秒數，組成 `chart.ComposerInput`。載入失敗是帶 Stage 的 `*composer.LoadError`（Subject 不在 manifest 則是 `manifest.ErrSubjectNotFound` 輸入錯誤）；gui `GenerateChartComposer` 只是 adapter：驗證 → `composer.Load` → `chart.RenderComposer` → 依 Stage 選訊息前綴的 envelope。見 [[ADR-0046]]。
在 UI(panel 標題)以「資料做圖」呈現 — 與 Chart Composer **同義**;canonical code/domain 術語仍為 Chart Composer(比照 [[Subject]] ↔ 分析主題)。「資料做圖」原為舊單檔流程口語名,Composer panel 沿用為標題,故由 _Avoid_ 升為 UI 同義詞。
_Avoid_: data plotting, chart panel, multi-chart viewer.

## Example dialogue

Dev:「Phase analysis 寫 CSV 為什麼要在 caller 端 skip 前 3 row？」

Domain expert:「那是因為 `ConvertPhaseAnalysisToCSV` 每次回的 `[][]string` 是 header + max row + mean row + time-index row 的 4-row 結構。當 caller 要把多個 phase 合進一個檔，必須跳過後續 phase 的 header 跟 time row，避免重複。」

Dev:「也就是 row layout 是 CSVHandler 內部知識，但目前 caller 端被迫知道。」

Domain expert:「對。format-aware write 之後，caller 只給 phases `[]PhaseAnalysisResult` + maxTimeIndex，merging 跟 row skip 全部被 CSVHandler 吸進去。caller 不再看到「row」這個字。」
