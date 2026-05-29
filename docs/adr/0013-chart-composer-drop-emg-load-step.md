# Chart Composer 流程簡化 — 刪除「載入 EMG 欄位」步驟,改一鍵生成(預設全通道)

**Status**: accepted (2026-05-29) — design-time;實作待 handoff worktree

## Decision

把 [[Chart Composer]] 的兩步流程收斂成一步。

舊:選 manifest / 資料夾 / 主題 → 按「載入 EMG 欄位」(打 `LoadChartComposerEMGChannels` RPC、render channel checkbox、勾選)→ 按「生成圖表」。
新:選 manifest / 資料夾 / 主題 → **直接按「生成圖表」**,預設載入**全部** EMG 通道(不再有 channel 勾選 UI)。

具體變更:

**後端(`gui/chart_composer_handlers.go`)**
1. 刪 `LoadChartComposerEMGChannels` RPC + `LoadChartComposerEMGChannelsParams` + `ChartComposerChannelsResult` + `failedChartComposerChannelsResult`。其三個產物(channel 清單 / `HasMuscleRatio` / `EMGMotionOffset`)在新流程都不需前端先取。
2. `GenerateChartComposerParams` 砍掉 `SelectedChannels` 與 `EMGMotionOffset` 兩欄。
3. `GenerateChartComposer` 改**直接讀 `row.EMGMotionOffset`**(原本 line 313 `LoadChartComposerEMGChannels` 就是從這取),並移除 line 370「`SelectedChannels` 為空 → fail-fast」;空 channel 交給 `chart.RenderComposer` / `buildEMGSeries`(`composer.go:267-273`)既有的「empty → fallback 全 channel」。

**前端**
4. `main.js`:刪 `loadComposerEMGChannels()`、`_composerSelectedChannels` / `_composerEMGMotionOffset` / `_composerLoadedSubject` 狀態、`LoadChartComposerEMGChannels` import;`onComposerSubjectChange()` 退化為僅「清 chart container」(offset / loadedSubject reset 不再需要)。
5. `chart_composer_spec.mjs`:`formBody` 移除「載入 EMG 欄位」鈕 + channel selector + warning banner(收斂近空);`rpc` 移除 `_composerSelectedChannels` 空檢查與 `_composerLoadedSubject` 一致性 guard、不再傳 `emgMotionOffset` / `selectedChannels`。
6. `wails generate` 重生 `frontend/wailsjs/go/**`(移除已刪 binding;[[memory:feedback_wails_frontend_dist_rebuild]] / [[memory:feedback_ci_wailsjs_stub_fidelity]])。
7. 測試同步:`chart_composer_handlers_test.go`、`app_panic_ast_test.go`、`chart_composer_spec.test.mjs`、`iframe_security.test.mjs`。

(同 PR 順帶的色票 + 欄位序渲染、版面調整、「標準化視圖」新按鈕屬可逆 presentation / feature 變更,**不在本 ADR**;見設計摘要 + inline 註解。)

## Why

- **offset 單一來源。** 舊流程讓前端從 `LoadChartComposerEMGChannels` 拿 `EMGMotionOffset`、Generate 時再回傳。但 Generate **自己**已 load manifest + `findManifestBySubject`,`row.EMGMotionOffset` 當場可得。前端往返反而**製造**「上個 subject 的 stale offset 套到新 subject」的風險 —— `_composerLoadedSubject` 一致性 guard(codex P2#3)正是為了補這個自找的洞。直接讀 row,把整類 bug 連同 guard 一起消掉。
- **「預設全通道」後端早就支援。** `buildEMGSeries` 對空 `SelectedChannels` 已 fallback 全 channel;handler line 370 的 fail-fast 是為舊「預設不勾」UI 才加的守門。UI 既改全載,鬆綁這道守門即對齊。
- **淨刪。** 一支 RPC + 一組 params/result + 三個前端狀態 + 一個 guard + channel selector UI 全消;符合本 repo collapse 範式([[ADR-0006]] / [[ADR-0008]] 皆淨刪)。
- **deletion test 過。** `LoadChartComposerEMGChannels` / `_composerEMGMotionOffset` / `_composerLoadedSubject` / 兩個 param 欄位除自身與測試外 **0 live caller**(grep 確認,2026-05-29);刪後 Generate 路徑仍完整(offset 來自 row、channel fallback 全選)。

## Considered Options

- **A. 一鍵生成、刪載入步驟(chosen)** — 上述。優:offset 單一來源、消 stale-offset guard、淨刪。缺:動 Wails RPC 簽章(regen bindings + 改測試);Image 1 的「本主題提供肌肉比值資料 → 3 grid」預先提示消失(user 已確認不留 —— 3 grid 生成時自然出現)。
- **B. 保留 `LoadChartComposerEMGChannels`,改成 subject 選好時自動呼叫** — UI 不顯示但底層仍跑 load。拒:保留了 `_composerEMGMotionOffset` / `_composerLoadedSubject` + stale-subject guard,而這些 state 的存在理由(手動載入)已消失;等於把可刪的耦合以「自動化」名義留著。淨增 vs 淨刪。
- **C. 維持兩步手動流程** — 拒:與 user 需求(預設全載、不用選)直接衝突。

## Reversibility

中 —— 可從 git revert 復原 RPC + params + 前端 state。但本 ADR 鎖住「Composer 是一鍵 viewer」方向;若未來真要 per-channel 選擇,應從 Composer 既有架構重新長(屆時需求形狀未必同舊 checkbox 流程),而非 restore 舊 RPC。

## Related

- [[ADR-0002]](Chart Composer 架構)—— 本 ADR 簡化其前端流程;ADR-0002 §1 canonical-key(Composer 用 subject **name** 非 idx)不變。
- [[ADR-0007]](ManifestPanel)—— 本 ADR 動到 Composer spec(`formBody` / `rpc` 收斂);同 PR 另刪共用 shell 第二顆返回鈕(5 panel 一致),spec-driven 差異模式不變。
- [[ADR-0006]] / [[ADR-0008]] —— 同 collapse / deletion 範式(consumer 消失即刪 surface)。

## Process note

- **2026-05-29 grill-with-docs session 產物。** 6 個決策(EMG 載入機制 / 渲染順序 / 色票 / 標準化視圖語意 / 命名 / UI 布局)逐一 grill。本 ADR 只 capture 架構面(EMG 載入機制 = Option B);其餘(色票 hex + 欄位序、版面、標準化視圖按鈕)可逆性高,寫進設計摘要 + inline 註解,不另開 ADR(grill-with-docs「ADR sparingly」)。
- **ADR 編號:** 0013 確認 free(0001–0012 全 committed、無 untracked ADR、無平行 charting worktree;2026-05-29 驗,對齊 [[memory:feedback_adr_number_collision]])。
- **CONTEXT.md 同步:** 「資料做圖」由 [[Chart Composer]] 詞條 `_Avoid_` 升為 UI 同義詞(比照 [[Subject]] ↔ 分析主題)。注意:[[ADR-0008]] process note 曾載「資料做圖 retire 記在 `_Avoid_`」,該句指**舊單檔流程**的 retire;現「資料做圖」專指 Chart Composer 的 UI 標題,語意已轉。
- **Impl-time 提醒:** 開工前重跑一輪 grep(含 `*_test.go` + panic / AST 系列 —— `app_panic_ast_test.go` 已知引用 `LoadChartComposerEMGChannels`)確認無漏網 caller;impl 後 `wails generate` + `make build-wails` 移除殘留 binding,`make test` / `make lint` 綠。走 TDD([[memory:feedback_handoff_after_design]])。
