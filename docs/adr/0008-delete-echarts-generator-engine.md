# 刪除遺留的 EChartsGenerator 圖表引擎（ADR-0002 殘留 orphan）

**Status**: accepted (2026-05-28)

## Decision

刪除 dead `EChartsGenerator` 引擎及其整個自閉 cluster。[[ADR-0002]] 用 [[Chart Composer]] 取代舊「資料做圖」時，刪除了 consumers（`showChartPanel` + `GenerateChart` / `GenerateInteractiveChart` handler + 對應 i18n），但**留下引擎型別本身與 `gui/app.go` 的建構** —— 現在每次啟動都 construct、production 零呼叫。本 ADR 收乾這個 orphan。

具體刪除（dead cluster，自閉、無 live 依賴）：

1. `internal/chart/echarts_generator.go`（846 行整檔）—— `EChartsGenerator` 型別 + 全部 export（`GenerateInteractiveChart` / `RenderChartToWriter` / `GenerateComparisonChart` / `ConvertToJSON` / `GenerateExportScript` / `UniformSubsample` / `CalculateOptimalSampling` 等）+ 4 個 sentinel（`ErrDatasetNil` / `ErrDatasetNoHeaders` / `ErrDatasetNoData` / `ErrDatasetChannelMismatch`，後者 declared-but-unused、「保留供未來」從未實現）+ package-private const（line width / zoom bounds / scientific notation 邊界 / `uniformSubsampleMaxOutputPoints`），全部 0 production caller。
2. `internal/chart/chart_statistics.go` —— `GetAvailableColumns` / `GetChartStatistics` / `ColumnInfo` / `calculateChannelStatistics`；依賴 `echarts_generator.go:43` 的 `validateDataset`，與其同生共死。
3. `gui/app.go:73`（`chartGen *chart.EChartsGenerator` field）+ `:128`（`chartGen: chart.NewEChartsGenerator()`）—— constructed-never-called orphan，全 codebase 僅此 2 行提及。移除後 `gui/app.go:18` 的 `"count_mean/internal/chart"` import 變 unused（app.go 只為 chartGen 用 chart 套件），需一併移除否則 build 失敗。
4. self-referential 測試 —— **整檔刪 6 檔**：`echarts_generator_test.go`、`chart_statistics_test.go`、`connect_nulls_test.go`、`nil_empty_guards_test.go`、`batch_test.go`、`uniform_subsample_test.go`（全部逐 case 確認整檔測 dead 符號；後 2 檔為 impl-scope 補完，見 process note）。**partial prune 1 檔**：`sanitize_test.go` —— 移除 `TestNormalizeExportFormat_Whitelist` + `TestGenerateExportScript_RejectsXSS`（測 dead `normalizeExportFormat` / `GenerateExportScript` / `ExportConfig`），**保留** 6 個 `SanitizeChartString` / `sanitizeForJSString` live 測試（XSS 守護仍 live、無需 migration）。

**一條 coverage migration（替換刪掉的覆蓋，非 scope creep）：** `connect_nulls_test.go` 透過 dead 路徑（`extractColumnLineData` / `RenderChartToWriter`）pin「NaN value → nil LineData → 線斷開」行為，但這個行為在 `composer.go:743`（`buildComposerLineData`）是 live 且 production-reachable —— muscle_ratio 缺值 → empty cell → `math.NaN()` → line gap（見 `gui/chart_composer_handlers.go:740-744`），且 composer 路徑目前**無** NaN 測試（只有 cci 有 `chart_nan_test.go`）。刪除前**遷移一條 assertion 到 live 路徑**：在 `composer_test.go` 新增 `TestBuildComposerLineData_NaNBreaksLine`，斷言 NaN value 點得 `LineData{Value: nil}`、正常值得 `[]any{t, v}`。這是「替換刪掉的 live-behavior 覆蓋」而非「修 pre-existing 未測 code」，故 in-scope（[[memory:feedback_pr_scope_baseline_debt]]）。

**MUST STAY（已驗 live，勿刪）：**

- `internal/chart/downsampling.go`（`LTTBDownsample`）—— live：`composer.go:332` / `cci/chart.go:229`。
- `gui/chart_helpers.go`（`ErrInvalidImageFormat` sentinel）—— live：`cci_handlers.go:159` / `chart_composer_handlers.go:513`。
- `internal/chart/composer.go`、`internal/cci/chart.go` —— live 圖表引擎。

2026-05-28 architecture review 提出的 Candidate 1「刪掉死掉的 EChartsGenerator 引擎」選 deletion 路線（Option B「保留當 reusable library」與 Option C「janitorial only」拒絕，見 Considered Options）。

## Why

- **[[ADR-0002]] 刪 consumer 留 engine。** ADR-0002 Decision §1 明列它刪除的是 entry points（`showChartPanel` + `GenerateChart` / `GenerateInteractiveChart` handler + i18n），從未提及 engine 型別與 `app.go` 建構。entry points 一刪，engine 即不可達，但型別與 `chartGen` 建構被留下，成為每次啟動 construct、零呼叫的 orphan。本 ADR 是 ADR-0002 未竟刪除的 finalize —— 與 [[ADR-0006]] 對其 Wave 6 前身的關係同形。

- **Deletion test 過。** dead cluster 15 個符號跨非測試 `gui/`+`internal/` grep 0 production caller；刪除後 complexity 真實消失，無任何 N-callsite 位移（對比 [[ADR-0005]] 拒絕 Candidate #2 的「14 callsite 位移」反例）。cluster 自閉：live 檔（`composer.go` / `downsampling.go` / `sanitize.go`）對 dead 檔定義的任何符號（含型別 / const / sentinel）**0 引用**，刪檔後 package 仍編譯。

- **self-referential test debt。** 4 個測試檔全測 dead 符號的對外契約（nil-guard、ragged-row null、sentinel error、connectNulls）；production 無 caller，刪不掉 prod coverage，只 finalize「surface 沒人用」這個事實。`nil_empty_guards_test.go` 經逐 case 確認**整檔** dead（3 個 test 全打 dead 符號），可整刪而非 partial prune。

- **`chartGen` 是 constructed-never-called。** 全 codebase 僅 `gui/app.go:73` / `:128` 提及，無 appState snapshot / `applyConfig` rebuild 牽連，移除無 cascade。

- **跟 [[ADR-0006]] 同 pattern。** 兩者都是「刪除一個 consumer 已消失、只剩 self-referential test 的 module」：ADR-0006 是 `BackpressureController`，本 ADR 是 `EChartsGenerator`。採同樣 deletion test、同樣結論（整體消失，callsite 不多開）。與 [[ADR-0005]] 同 deletion test、結論相反 —— 三條合讀防止 reviewer 把任一 ADR 誤讀為「總是 collapse」或「總是 preserve」。

## Considered Options

- **A. 整體刪除（chosen）** —— 上述 cluster + 一條 coverage migration。優點：complexity 真實消失、self-referential test debt 收乾、每次啟動的無謂 construct 消失。缺點：幾乎沒有 —— engine 無 production consumer，刪除不影響任何 live 路徑。

- **B. 保留 engine 當 reusable library** —— 把 `EChartsGenerator` 留著當「未來若要 ad-hoc 單檔做圖可重用」的 library。拒：[[ADR-0002]] 已判定舊「資料做圖」的 ad-hoc 任意 CSV 畫圖「無測試覆蓋此用途、使用頻率不明、混淆成本大於彈性收益」並選擇刪除；保留 engine 等於把 ADR-0002 拒絕的彈性以 dead code 形式偷渡回來。若未來真需要，從 fresh deepening 開始（成本與現在保留 anaemic engine 等同，且新形狀會貼合屆時需求）。

- **C. Janitorial only（只移除 `chartGen` 建構，保留型別）** —— 刪 `app.go:73` / `:128`，留 `echarts_generator.go` + `chart_statistics.go` 當「暫時無人用但編譯得過」的 package 內容。拒：留下 846+ 行 dead code + 4 個 self-referential test 檔，未來 architecture review 還會重新挖出來 grill 一遍（[[ADR-0006]] Option C 同樣理由被拒）；ADR-0002 已啟動 retire 方向，janitorial 只 finalize 一半。

## Reversibility

中 —— 可從 git blame / revert 復原，但本 ADR 鎖住「若未來要 data-charting，從 fresh deepening 開始」而非「restore from saved engine」。理由同 [[ADR-0006]]：殘留的 2024–2026 內部 go-echarts wrapper 形狀不會貼合屆時需求（屆時可能走 [[Chart Composer]] 既有 multi-grid 架構延伸，或全新需求）；保留 dead engine 的成本（每次啟動 construct + 846 行需維護的 surface + reviewer 反覆 re-grill）大於從頭重建。

## Related

- [[ADR-0002]]（Chart Composer 取代舊「資料做圖」）—— **orphan 來源**：ADR-0002 刪 consumer（handler + panel + i18n）但留 engine 型別 + `app.go` 建構；本 ADR finalize 該未竟刪除。
- [[ADR-0006]]（BackpressureController 拆除）—— **同 deletion pattern**：consumer 已消失、只剩 self-referential test 的 module 整體刪除；同樣 deletion test、同樣結論。兩條合讀＝「刪除遺留 dead module」的 repo 範式。
- [[ADR-0005]]（calculator family 拒拔 `*FromRawData`）—— **同 deletion test、結論相反**：Candidate #2 失敗（14 callsite 位移），本 candidate 通過（complexity 真實消失）。

## Process note —— cross-check 與 follow-up（防未來 reviewer 重蹈）

2026-05-28 grilling session 開場 cross-check（[[memory:feedback_cross_check_report_vs_code]] 紀律）re-verify handoff 載明的 7 個 open question（這些 evidence 不在任何 committed artifact），結果：

1. **Q1 cluster boundary** —— airtight。除 function 名外，另查 package-private const（6 + `uniformSubsampleMaxOutputPoints`）+ 4 個 sentinel + 型別（`ColumnInfo` / `channelStatistics` / `EChartsGenerator` / `InteractiveChartConfig` / `DataPoint` / `ExportConfig`），live 檔 0 引用 —— 確認 type/const 層級也無 leak（function-only grep 不夠）。
2. **Q2 Wails RPC surface** —— `GetAvailableColumns` / `GetChartStatistics` 名字像 column-picker RPC，但 frontend grep + gui caller 全空，無 `App` method 包裝。
3. **Q3 `nil_empty_guards_test.go`** —— 整檔 dead（3 個 test 全測 dead 符號），非 partial，整刪。
4. **Q4 connect-nulls coverage gap** —— **唯一有實質 trade-off 的決策**。decision：遷移一條 assertion 到 live composer 路徑（`TestBuildComposerLineData_NaNBreaksLine`），見 Decision。
5. **Q5 `chartGen` cascade** —— 僅 2 行，無 cascade。
6. **Q6 coverage** —— 刪 tested-but-dead code 預期 coverage 上升；impl 時驗 `make coverage-check` 維持綠（[[ADR-0006]] 同對話）。
7. **Q7 ADR 編號** —— 確認 0008 為 free（最高 committed 為 0007；[[ADR-0006]] process note point 4 曾記 0005 編號碰撞，本次無）。

**新發現（不在 handoff，已拆 follow-up issue #26）：** 被刪 engine 在 3 處（`echarts_generator.go:279` / `:408` / `:775`）顯式設 `ConnectNulls: false`，其註解明說 go-echarts v2/v3 預設不同（見 `connect_nulls_test.go:62-67`）；但 live `composer.go` / `cci/chart.go` 都**沒設**、依賴版本相依預設，NaN gap 可能被橋接成連續線。這是 **pre-existing latent issue、與本刪除無關**，依 baseline-debt 紀律（[[memory:feedback_pr_scope_baseline_debt]]）拆成獨立 issue #26（`bug` / `needs-triage`），不併入刪除 PR；尚未視覺驗證，issue 載明需 render 檢查。

**Impl-scope 補完（2026-05-28 handoff 撰寫時 grep，對齊 [[ADR-0006]] impl-time 補完紀律）：** grilling cross-check 對 production 符號做 caller 掃描時**慣性排除 `*_test.go`**（`grep -v '_test.go'`），故 test-file 邊界當時承自前次 handoff 的「4 檔」。撰寫 impl handoff 時補掃 test 檔，發現實際 dead test 為 **6 整檔 + 1 partial**：除原 4 檔外，另有 `batch_test.go`（3 test 全打 `UniformSubsample` / `GetChartStatistics` / `extractColumnLineData`）與 `uniform_subsample_test.go`（4 test 全打 `UniformSubsample`）整檔 dead，及 `sanitize_test.go` 的 2 個 export-script test（partial）。repo-wide 補掃確認 `internal/chart/` 外無 test 引用 dead 符號、gui test 無 `chartGen` 引用、`DataPoint` 僅在 dead file。Decision 已更新為完整集。**Lesson：** deletion candidate 的 cross-check 必須對 test 檔做**獨立**符號掃描 —— prod-symbol caller grep 慣性排除 `_test.go`，會漏看「測 dead function 的 test 檔」這層；本案在 handoff（pre-impl）就補上，比 [[ADR-0006]] 在 impl 階段才挖到更早。

**Impl-time 提醒（對齊 [[ADR-0006]] 經驗）：** grilling 階段 cross-check 必要但不充分 —— impl 開工前應再 grep 一輪（含 `*_test.go` invariant / panic 系列 + docstring example block），ADR-0006 impl 時即挖到 2 個漏網 caller。本 candidate 的 dead cluster 較 self-contained（live 端 0 引用），漏網風險低，但仍建議 impl 前重跑符號 grep。impl 後 `make build-wails` 會自動從 `frontend/wailsjs/go/**` 移除任何殘留 binding（[[feedback-wails-frontend-dist-rebuild]]）。CONTEXT.md **不動**：「資料做圖」retire 已記在 [[Chart Composer]] term 的 `_Avoid_`，而 `EChartsGenerator` 是 implementation 型別、非 domain term，不入 glossary。

**Codex round-2 補正（2026-05-28，user-approved）：** ADR 原文 Decision §4「保留 `sanitizeForJSString` live 測試（XSS 守護仍 live）」的前提**對 `sanitizeForJSString` 不成立**：其唯一 production caller 是 `GenerateExportScript`，已隨本刪除移除，使 `sanitizeForJSString` 降為 test-only orphan（`SanitizeChartString` 才是真正 live —— `composer.go` + `cci/chart.go` 呼叫，保留）。修正決策：刪除 `sanitizeForJSString` + 套件內 alias `sanitizeChartString` + `TestSanitizeForJSString_QuoteSafe`；alias 原本的 2 個 test caller repoint 到 exported `SanitizeChartString`（行為等價，alias 只是 forward）。`encoding/json` import 隨 `sanitizeForJSString` 移除後從 `sanitize.go` 自動 drop（`goimports`）。同 round 順手退役 `docs/api.md` + `docs/usage_patterns.md` 內已 stale（自 [[ADR-0002]] 起）的 EChartsGenerator API 文件（section 整刪 + TOC entry 移除 + FAQ snippet 移除）。
