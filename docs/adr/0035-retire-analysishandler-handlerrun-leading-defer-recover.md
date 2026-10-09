# 退役 Tier-2 AnalysisHandler 與 HandlerRun；所有 bound method 首句 defer recover

**Status**: accepted · **implemented** (2026-10-09)

Wails bound method 的 panic 安全網原本有三種機制並存：約 17 個 method 首句 `defer recoverHandlerPanic*`、4 個走 Tier-1 `HandlerRun`、3 個走 Tier-2 `AnalysisHandler[P, R].Run`。AST 測試只看「首句是不是 defer」，後兩種靠 `knownGapEntries()` 依名稱豁免 —— 掃不到 wrapper 之前跑的程式碼。本 ADR 收斂為**單一入口形狀**：每個 exported `*App` method 的第一個 statement 都是與其簽章相符的 `defer recoverHandlerPanic*`，並刪除兩層樣板。本 ADR 為架構重構 W2「GUI handler seam」的第一步。

## Decision

1. **入口形狀**：每個 exported `*App` method（Wails 全數 bind，含 `Startup` / `Shutdown` lifecycle hook）首句依簽章選 variant：
   - 最後一個回傳值是 `error` → `defer recoverHandlerPanic(name, a.logger, &err)`
   - 只回單一非 error 值（`GetConfig` / `GetVersion` 等）→ `defer recoverHandlerPanicValue(name, a.logger, &out)`
   - 無回傳值（`ShowMessage` / `Startup` / `Shutdown` 等）→ `defer recoverHandlerPanicVoid(name, a.logger)`

   `gui/recover.go` 既有三個 variant 已涵蓋全部簽章，未新增。原 `HandlerRun` / `AnalysisHandler` 成員沿用原本的中文 handler 名（「階段分析」「CCI 分析」…），panic 診斷文字不變。
2. **刪除兩層樣板**：`AnalysisHandler[P, R]`（`gui/analysis_handler.go`）與 `HandlerRun`（`gui/handler_run.go`）連同測試刪除。`AnalyzePhases` / `AnalyzePhaseSync` / `AnalyzeMuscleRatio`（原 Tier-2）與 `AnalyzeCCI` / `AnalyzeNormalizedPhaseSync` / `LoadChartComposerSubjects` / `GenerateChartComposer`（原 Tier-1）改為直列 body，形狀同 [[ADR-0031]] 的六步 CCI body。
3. **錯誤通道逐 handler 不變**（RPC 簽章、前端 binding、訊息文字逐位元組不變）：
   - `AnalyzePhases`：dual —— 任一步失敗回 `(nil, err)`。
   - `AnalyzePhaseSync`：mixed —— validate 失敗回 `(nil, err)`；分析 / 寫檔失敗回 `(failedResult, nil)`。
   - 其餘五個：single-channel —— 非 panic 失敗一律 `(failedResult, nil)`。
   - panic 一律 `(nil, ErrInternalPanic-wrap)`。
4. **只為樣板存在的 workaround 一併消失**：`phaseSyncValidateGateErr`（標記「validate 失敗」的 error type）、`runErr` 四路分流（gate type / `ErrInternalPanic` / `stats != nil` 猜是寫檔還是分析失敗）、`phaseRunData`、`labels`/`ranges` 跨 closure capture、MuscleRatio 的 `errors.Is(runErr, ErrInternalPanic)` 重檢；`UIError` / `ErrMuscleRatioAnalysisFailed`（`gui/uierror.go`）無外部 `errors.Is` caller，刪除。
5. **entry/exit log 保留**：`HandlerRun` 打的「開始<name>」/「<name>完成」兩條 Info log 文字與觸發條件不變 —— 原 Tier-1 成員在每個非 panic 返回都打 exit（[[ADR-0031]] Observability 第 1 點），以 deferred closure 判斷 `result != nil`（single-channel 契約下正常返回的 result 必 non-nil、panic 路徑仍為 nil）；原 Tier-2 成員只在成功路徑打 exit。唯一可見差異是 log 的 caller 位置改指向 handler 本身而非 `handler_run.go`。
6. **守門測試**：
   - `TestBoundMethods_FirstStatementIsMatchingRecover`（AST）：每個 exported method 首句必須是對應 variant，且第三引數指向具名回傳；method 集合與 reflect 比對防漏檔。`knownGapEntries()` 豁免清單與 `TestKnownGap_AllEntriesHaveReason` 刪除。
   - `TestBoundMethods_NilDeps_PanicNeverEscapes`（runtime）：以 reflect 對零依賴 `&App{}` 呼叫每個 method（引數全 zero value），斷言沒有 panic 逃出。改動前此測試在 3 個原 Tier-2 handler、`AnalyzeNormalizedPhaseSync`、`Startup`、`Shutdown` 失敗。

### Amends ADR-0031

[[ADR-0031]] 把 `AnalyzeCCI` 鎖為「Tier-1 `HandlerRun` 直用、與 `AnalyzeNormalizedPhaseSync` 鏡像」，並記錄「`AnalysisHandler[P,R]` 樣板仍服務剩餘 3 個成員」。兩層樣板現已不存在：ADR-0031 的實質意圖 —— CCI 單一 recover、六步直列 body、與 NPS 同形、不被拉回樣板 —— 仍成立，只是由「首句 defer」而非 `HandlerRun` closure 表達；其 Observability 第 1、2 點（expected failure 也打 exit log、panic 診斷名「CCI 分析」）原樣保留。ADR-0031 本身依 repo 慣例不修改。

### ADR-0004 Boundary 3 成為 moot

[[ADR-0004]] Boundary 3 保留 `AnalysisHandler.WriteCSV` closure 簽章、`AnalyzeMuscleRatio` 為 `WriteCSV: nil`。closure 不復存在，此邊界不再有對象；muscle_ratio 在 analyzer 內 per-subject 寫檔（Boundary 1、[[ADR-0012]] compute+write）不變。Boundary 1、2 不受影響。[[ADR-0001]] 的決策（PhaseSync 經 CSVHandler 寫檔）不變，只是其 rationale 引用的「樣板單一寫檔策略」不再適用。[[ADR-0002]] §1「Chart Composer 不進 Analysis pipeline family」同理成為 moot —— Composer 不計算、不寫 CSV 的形狀不變。

## Why

- **Tier-2 樣板丟掉了「哪一步失敗」**：三個成員有三種錯誤通道契約，樣板對所有 closure 失敗一律走 err，於是每個 caller 在 `Run` 之後再把錯誤分流回去（gate-error type、四路 unpack、closure capture、`phaseRunData`）。直列 body 下每個失敗分支的通道就寫在該分支的 `return`。
- **`HandlerRun` 幾乎是 pass-through**：它只是 defer 加兩行 log，卻強迫 closure 形狀；AST 規則看不到 wrapper 之前的程式碼，豁免清單實際放行了真缺口 —— 3 個 Tier-2 handler 與 `AnalyzeNormalizedPhaseSync` 都在 recover 之前呼叫 `a.logger.Info` 或 `a.state.Load()` 再 deref，nil 依賴下 panic 直達 Wails runtime（新 runtime 測試重現，並另抓到 `Startup` / `Shutdown`）。
- **「全部走 `HandlerRun`」做不成單一形狀**：7 個 method 不回 error（`GetConfig` / `GetVersion` / `ShowMessage` …），要套 `HandlerRun` 必須改 Wails 簽章與前端 binding；首句 defer 則三種簽章都能表達。

## Considered Options

### A. 所有 bound method 首句 defer recover*（chosen）

見上。一條規則、AST 可直接驗證、無豁免清單。

### B. 全部改 `return HandlerRun(...)`，AST 認此形狀（rejected）

7 個非 error 回傳 method 無法套用（需改 Wails 簽章）；AST 仍得另檢「wrapper 之前沒有 statement」；closure 形狀與直列 body 相衝。

### C. 保留兩層樣板，只補 pre-wrapper 缺口（rejected）

gate-error / 四路分流等 workaround 留著；豁免清單仍要人工維護；三種機制並存，下一個新 handler 仍可能挑錯。

### D. 新的泛型 entry wrapper 統一 recover + log（rejected）

與 B 相同的簽章問題；log 只有兩行，不值得一層抽象。

## Consequences

- `recoverHandlerPanic` 成為每個回 error 的 bound method 的必經點，之後要在 err channel 統一處理（例如 redact）只需改一處；AST 測試保證新增 method 不會繞過。
- GLOSSARY：退役「Analysis pipeline family」與「AnalysisHandler[P, R]」兩個條目；Subject、Domain analyzer、Max-mean batch runner、Manifest handler prelude、ManifestPanel、Chart Composer 條目改寫為不再引用它們。舊 ADR 中的 `[[Analysis pipeline family]]` / `[[AnalysisHandler[P, R]]]` 僅具歷史意義。
- 刪除的測試：`analysis_handler_test.go`（7）、`handler_run_test.go`（4）、`uierror_test.go`（3）、`TestKnownGap_AllEntriesHaveReason`；舊 AST 測試由新版取代。CCI / Chart Composer 的 nil-logger panic 測試斷言的是使用者可見契約（`errors.Is(err, ErrInternalPanic)`、result 為 nil），保留。
- 補三則 characterization 測試釘住原本缺的覆蓋：`TestAnalyzePhaseSync_HappyPath`、`TestAnalyzePhaseSync_WriteFailure_FailedResultNilErr`、`TestAnalyzePhases_MissingInput_ErrChannel`。

## Related

- [[ADR-0031]]（**amended** —— Tier-1 `HandlerRun` 形狀改由首句 defer 表達）
- [[ADR-0004]]（Boundary 3 moot）
- [[ADR-0001]]、[[ADR-0002]]（決策不變，rationale 中的家族 / 樣板引用不再適用）
- [[ADR-0012]]（Domain analyzer 形狀分歧不變）
