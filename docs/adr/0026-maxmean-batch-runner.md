# 把 Max-mean 批次 orchestration 從 GUI god object 抽進 `internal/maxmean`，以 file-source port 餵食

**Status**: accepted · **implemented** (2026-06-15)

本 ADR 是 2026-06-15 architecture review Candidate 2 grilling 後的結論。`CalculateMaxMean` 的批次 orchestration（目錄探索 → worker loop → 逐檔 calc+write → partial-success 累積，~290 行、`*appState` snapshot 穿 6 函式）**內聯在 `gui/app.go`** god object 裡；其三個 sibling（`AnalyzeCCI`/`AnalyzePhaseSync`/`AnalyzeMuscleRatio`）都把 orchestration 委派 `internal/{cci,phase_sync,muscle_ratio}`，只有 Max-mean 沒有。本案把編排移到 `internal/maxmean`、由 file-source port 餵食，`CalculateMaxMean` 退化為 thin adapter。

## Decision

新增 package `internal/maxmean`，package-level 函式：

```
RunBatch(ctx, calc *calculator.MaxMeanCalculator, source FileSource, writer ResultWriter, params BatchParams) (*BatchResult, error)
```

六條設計分叉的結論：

**1. 新 category「Max-mean batch runner」，非 Domain analyzer。** Domain analyzer（CONTEXT.md）判準是「manifest + dataFolder 驅動、恰 3 member」，unit of work 是 [[Subject]]。Max-mean 的 unit of work 是**檔案 / EMGDataset**（目錄探索），不符該判準。塞進去會稀釋 Domain analyzer 的定義。故立**同層、不同 category** 的新概念（CONTEXT.md 新增術語）。

**2. seam 取已解析依賴（resolved deps），非自行重解 config。** `appState` + `SaveConfig` 是 GUI/Wails 概念，**不可洩進 `internal/`**。config-binding 不對稱是根因：`maxMeanCalc`（綁 ScalingFactor）與 `csvHandler`（綁 InputDir/OutputDir）在 SaveConfig 時重建，三個 sibling analyzer 則 config-independent、建一次。故 `RunBatch` 收 `calc` 具體型別 + `source`/`writer` port，snapshot 一致性由 GUI adapter「從同一個 `s` 建三者」維持。

**3. file-source port 名正言順（兩 adapter = 真 seam）。** 既有兩條 discovery（configured input dir 經 `ListCSVFilesInDirectory`+`ReadCSVFromDirectory` / external 絕對路徑經 glob+`ReadCSVExternal`）的結果，**在進 `executeBatchLoop` 前都被各自轉接成同一個** `batchFileEntry{displayName, readFunc func() ([][]string, error)}` 再匯流。port 不是新設計、是程式已長出來的這個匯流形狀：`FileSource.Discover() ([]BatchFile, error)`（error 承載 glob/list 失敗）、`BatchFile{Name, Read}`。第 3 個 in-memory adapter 讓 loop 可單元測試。mode 決策（internal vs external）留 GUI；discovery 進 runner。

**4. write 走 ResultWriter port、presentation 留 GUI。** `ResultWriter.Write(name, headers, results, startRange, endRange) (path, error)` 的 production adapter 包 `csvHandler.WriteMaxMean`（SubDir 綁定）。`BatchResult` 回 domain 型 `[]models.MaxMeanResult` + 計數；GUI adapter 做 `convertMaxMeanResultsToArray`、組中文 Message、算 `OutputPath = OutputDir/<outputDirName>`。compute/presentation 一刀切。

**5. scope = batch only，`NormalizeData` OUT。** `NormalizeData` 是線性雙-input 委派（無 loop/discovery/partial-success/snapshot），與 FileSource 零 port 重用，deletion test 弱。單檔路徑 `calculateMaxMeanSingle` 亦留 GUI（非 batch）。

**6. 共用原語 `ResolveTimeRange` 移到 `internal/calculator`。** 它在逐檔迴圈內被呼叫（須進 maxmean 可達範圍）、又被 single 路徑用（留 gui），而 internal 不能 import gui。它是 pure（只依賴 parsers）、語義屬 calc 域，故移到 `internal/calculator` 當 single/batch 共用家。

## Why

- **深模組 + 真 seam。** `executeBatchLoop` 是 latent deep module：整個批次編排（loop + partial-success 帳本）藏在小小的 `batchFileEntry` port 後面。抽出只是把它從 gui 命名搬到 internal，interface（FileSource/ResultWriter port + 具體 calc）遠小於 implementation。
- **snapshot 不變式被設計掉、非變得可測。** 今日「整批共用同一 csvHandler/calc 配對、防 SaveConfig 撕裂」靠穿 6 函式 + 3 段註解人工維持，且**無測試**守它。抽出後 GUI adapter「從同一個 `s` 建 source+writer+calc」一句話釘死，runner 永不見 config —— bug class 從「靠註解祈禱」變「結構上不可能」。
- **deletion test 強（concentrate 非 move）。** 刪 `internal/maxmean` → discover→loop→accumulate + partial-success 帳本原封回到 gui（~290 行）。對照 [[ADR-0005]]/[[ADR-0012]]「relocation-only = pass-through 被拒」判準：本案 loop+不變式是真行為的集中，故過 test。與剛否決的 `NormalizeData`（弱 deletion test）成對比。
- **interface 即 test 面。** partial-success（逐檔讀/算/寫失敗）、name→output 映射、ordering、headers-from-first 從前只能組 App+Wails ctx 才碰得到；抽出後餵 in-memory source + spy writer 直接斷言。

## Considered Options

- **塞進 Domain analyzer（擴 category）。** 拒：Max-mean 是檔案/目錄驅動、非 manifest/Subject，硬塞會破壞「恰 3 member、manifest 驅動」這條 load-bearing 判準（見 [[ADR-0012]] 兩軸分歧的刻意保留）。
- **`calc` 也走 port。** 拒：calc 是純函式 kernel（[[ADR-0005]] family），具體傳入即可決定性測試，無第 2 adapter，是 hypothetical seam。
- **`io.CSVHandler` 直接進 `internal/maxmean`。** 拒：會把 GUI 的 IO+安全+config-binding 全拖進 internal；port + gui-side adapter 讓 internal 只認 `[][]string` in / `[]MaxMeanResult` out。
- **`NormalizeData` 一起抽。** 拒：見 Decision §5（弱 deletion test、形狀不同）。
- **`ResolveTimeRange` gui 保留 + maxmean 複製。** 拒：跨 package 重複純原語、需同步維護，不如 calculator 單一家（見 Decision §6）。

## Consequences

- **Test surface 遷移 / 新增**（interface 即 test surface）：
  - **新增** `internal/maxmean/runner_test.go`：in-memory FileSource + spy ResultWriter + 真 calc，斷言 partial-success 計數、name→write 映射、ordering、headers-from-first、empty→ErrNoCSVFilesInFolder、時間範圍 **resolved-pair 轉發**（spy 收到的 startRange/endRange 等於 `ResolveTimeRange` 輸出，非 raw input；刻意不斷言 `(0,0)→CalculateFromRawData` 那條 dispatch 分支）。**這是本案 payoff**（以前碰不到）。
  - **不改即綠**（行為保持安全網）：`gui/maxmean_result_test.go` 兩個 GUI 層整合測試（single 回 Success/Message；external batch OutputPath==OutputDir/<batchName>）。
  - **更新** `gui/app_panic_ast_test.go` `unexportedHelpers` 白名單：移除 4 個搬走的 method。
- **無 user-observable 行為變更**（GUI envelope + CSV 檔案輸出 byte-identical：`convert(串接)`==`串接(convert)`、outputDirName/OutputPath/per-file write 路徑全保持）。唯一刻意 delta 在**非 user-facing 的 log surface**：batch 逐檔 log module `"app"`→`"maxmean"`（sibling 慣例）、log raw error 非內層中文 wrap。
- **snapshot 一致性不變式從「註解維持」升級為「結構保證」。**
- **CONTEXT.md**：新增術語 [[Max-mean batch runner]]、更新 [[Analysis pipeline family]] 的 `_Not included_` 註記（`CalculateMaxMean` orchestration 抽進此 runner 後退化 thin adapter）。

## Reversibility

中。回頭要把 `RunBatch` + 2 port 重新內聯回 gui、復原 6 函式 snapshot 穿線、把 `ResolveTimeRange` 移回 gui、刪 internal/maxmean 測試。重識別動機需重走 grilling。估 2–3 小時 grilling + 0.5 工作天。

## Related

- [[ADR-0005]] — calculator family kernel；`MaxMeanCalculator` 數學下放於此，本案不動 kernel。
- [[ADR-0007]] — ManifestPanel（frontend 把 panel boilerplate 收進 deep module）；本案是其 **backend twin**（把 orchestration boilerplate 收進 internal seam）。
- [[ADR-0012]] — Domain analyzer 兩軸分歧的刻意保留 + deletion-test 判準；本案立新 category 而非擴此三員。
- [[ADR-0004]] — Format-aware write / filename ownership；ResultWriter adapter 包的 `WriteMaxMean` 是 File-based write（caller 給 Filename）。

## Notes

實作 as-built（2026-06-15，branch `worktree-maxmean-runner-impl`、基於 main `95ac4c8`；subagent-driven 多 task 實作 + opus 對抗審查 + codex 收尾加固）：

- **與設計一致**：`RunBatch` 簽章、`FileSource`/`ResultWriter`/`BatchFile` port、`ResolveTimeRange` 移入 `internal/calculator`、`ErrNoCSVFilesInFolder` 於 gui 留 alias（指向 `maxmean.ErrNoCSVFilesInFolder`，保留 exported API + 共用 sentinel identity）、GUI thin adapter（`buildMaxMeanFileSource` + `dirFileSource`/`externalFileSource`/`maxMeanResultWriter`）全數落地。`gui/app.go` 淨 −250 行。
- **dispatch 刻意重複（勿 DRY 回去）**：`startRange==0 && endRange==0` 的二分 dispatch 同時存在於 `RunBatch`（`runner.go` inline）與 single 路徑的 `calculateWithTimeRange`（`gui/file_helpers.go`）。這是 Decision §6 的必然結果——single 留 gui、batch 進 internal，而 `internal/` 不能 import `gui`。**把兩者合一會重新引入 gui→internal import cycle**；保持分離是刻意的。
- **行為保持**：既有 `gui/maxmean_result_test.go` 兩整合測試不改即綠；新增 `internal/maxmean/runner_test.go`（partial-success 計數、name→write 映射、ordering + headers-from-first 並斷言 `Results` 內容/順序、empty→sentinel、time-range resolved-pair 轉發）。全 28 套件綠、build/vet 綠。
- **唯一非保持 delta（非 user-facing）**：batch 逐檔 log 從 module `"app"` 改走 `logging.GetLogger("maxmean")`、log raw error 而非內層中文 wrap。GUI envelope 與 CSV 輸出 byte-identical。
- **GUI smoke 未驗**（native webview headless 跑不了；比照慣例可授權無 smoke 直接 merge）。
