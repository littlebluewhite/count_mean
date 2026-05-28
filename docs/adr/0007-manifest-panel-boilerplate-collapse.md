# ManifestPanel:frontend panel-level boilerplate 收乾(含 Chart Composer)

**Status**: accepted (2026-05-28)

## Decision

新增 `frontend/src/manifestPanel.mjs` 作為 frontend panel layer 的 deep module,收乾「以 [[Manifest]] + dataFolder 為入口的 panel」共通 boilerplate。5 個 member panel:CCI、PhaseSync、NormalizedPhaseSync、MuscleRatio、[[Chart Composer]]。核心架構選擇:

1. **Scope 含 [[Chart Composer]]**:雖然 [[ADR-0002]] §1 明確 Chart Composer 不在 [[Analysis pipeline family]](backend handler 家族:compute + CSV),但 frontend panel layer 是不同軸,5 個 panel 的 boilerplate 同形 — `two adapters = real seam` threshold 在 panel 層已過(實為 5),不收乾 Composer 等於製造只有 1 個 caller 的 sibling seam(反模式)。**panel layer 是 frontend deepening,跟 backend pipeline family 是兩個正交軸,不衝突**。

2. **命名 `ManifestPanel`,不叫 `AnalysisPanel`**:後者跟 backend [[Analysis pipeline family]] 字面衝突且 Composer 不在後者。`ManifestPanel` 跟既有 [[Manifest handler prelude]] 對稱(backend 入口 prelude vs frontend 入口 panel),範圍語意精準 —「以 manifest + dataFolder 為入口的 panel」5/5 全中,MaxMean / Normalize / Phase 三個 panel 不吃 manifest 自動排除。`SubjectPanel` 拒:subject 是 panel 內部 navigation 非入口;`WorkflowPanel` 拒:過泛無對齊 domain。

3. **Path 3 hybrid(template + closure 注入,對齊 backend [[AnalysisHandler[P, R]]])**:`ManifestPanel.run(spec)` 通過 spec 接「panel 差異」、own 共通 boilerplate(panel shell + RPC envelope + status + subject load + ctx 規範化)。對齊已 ship 的 backend deepening pattern,maintainer 無需學第二套 deepening shape。framework path(整段 declarative spec)拒:`onResult` 等 closure 反而變大;toolkit path(純 helper utility)拒:depth 退化,5 panel 還是各自 6+ 行樣板。

4. **ctx 規範化(`{ manifestPath, dataFolder, subjectIdx, subjectName, subjects }`)**:[[Subject]] 形狀不對稱(CCI / PhaseSync / NormalizedPhaseSync 用 idx、Composer 用 string)由 ManifestPanel own — 兩種形狀同時給,caller 各取所需,**不擴 scope 動 backend RPC signature**(Composer 用 subject string 是 [[ADR-0002]] canonical-key 設計,manifest 升版時 idx 位移、name 不會,改 backend 需另外驗證)。`subjects` array 一併給,讓特殊 caller 不需要再 load。

5. **`onResult` closure + ManifestPanel helpers,不用 declarative 兩軸**:原本提案「`result.chart` / `result.csvSuccess` 兩軸 hold 不對稱」cross-check 既有程式碼後不夠表達實際 panel-specific 結果區(CCI 有 info + chart + report + openOutputFolder 4 段、Composer 有 chart + PNG btn、MuscleRatio 有 subjects list)。改為 `onResult(result, ctx, mp)` 整段 closure + ManifestPanel 提供 `mp.attachIframe` / `mp.bindPhaseCheckboxes` / `mp.openOutputFolder` / `mp.registerCleanup` helpers 收共通片段。

6. **Spec shape(final)**:
   ```js
   ManifestPanel.run({
     titleKey: 'panel.cci.title',
     statusRunningKey: 'status.cci_running',
     formBody: (t) => `... ${t('form.label.manifest')} ...`,
     rpc: async (ctx) => AnalyzeCCI(ctx.manifestPath, ctx.dataFolder, ctx.subjectIdx),
     onResult: (result, ctx, mp) => { mp.attachIframe(...); mp.bindPhaseCheckboxes(...); },
     silentSuccess: false,  // Composer 寫 true,其他 4 panel 省略
   });
   ```
   `formBody` 為 builder function 而非 string only — 對齊既有 `buildChartComposerPanelHtml(t)` pattern,locale change 自然套用新翻譯。i18n key 只暴露 2 個(panel-specific:titleKey + statusRunningKey),其他(`status.analysis_done` / `dialog.title.complete` / `dialog.title.partial_failed` / `error.msg.analysis_failed_dynamic`)由 ManifestPanel hardcode 共通 key — 不擴 i18n schema,避免 multi-locale 風險。

7. **Envelope own reentrant guard(button.disabled doubleclick race)**:MuscleRatio 既有 `:2522` 註解明寫「防雙擊產生並發 RPC 搶寫同一個輸出檔」,其他 4 panel 沒寫只是漏寫,理論上同樣會 race。ManifestPanel envelope 統一 own 此 guard,5 panel 全員受惠 — 順手修 baseline debt。

8. **`mp.attachIframe` 不包 bridge call**:對齊 [[ADR-0003]] §reversibility「不要在 bridge 上加 third layer」。helper 只 own iframe element create + sandbox=allow-scripts + srcdoc + ready promise。bridge.subscribe / send / requestReply 由 onResult closure 直接呼 — 維持 bridge 是唯一 facade。`mp.registerCleanup(fn)` slot 讓 closure 註冊 unsub callback,ManifestPanel re-attach iframe 前自動 call 所有 registered cleanup — 取代既有 `this._cciBridgeUnsub?.()` ad-hoc pattern。

9. **Panel state 維持 app this 自管**:`_cciResult` / `_composerPhaseTimes` / `_composerCheckedPhases` 等跨 re-render 持久 state 維持掛 `app` this,**不引入 `mp.scope` namespace**。既有 onLocaleChange `panelDispatch[currentPanel]()` re-run pattern 天然 work(只重設 functionPanel.innerHTML,不動 app instance state)。Composer 跨 generate 持久的 `_composerCheckedPhases` Set 行為保留。

10. **`handleMenuAction` switch fold 進 panelDispatch**:9 case switch(`main.js:233-265`)跟 panelDispatch map(`main.js:42-53`)完全 1:1 重複,verified 無 special-case、無 default、無額外行為。改為 `this.currentPanel = action; this.panelDispatch[action]?.();`(2 行取代 32 行) — pre-existing redundancy 但跟本 candidate 5 panel migration 高度耦合(5 entry 都要動,fold 比留 follow-up 更內聚)。

11. **5 panel 同 PR migration(non-pilot)**:CCI + PhaseSync + NormalizedPhaseSync + MuscleRatio + Composer 一次抽出,避免過渡期 maintainer 要記兩種 pattern。既有 `chart_composer_panel.mjs` 形狀(builder function `buildXxxPanelHtml(translator)`)擴展到 spec object,每 panel 一個 file `frontend/src/panels/{cci,phase_sync,normalized_phase_sync,muscle_ratio,chart_composer}_spec.mjs`。

12. **Test surface**:per-panel spec test(沿用既有 `chart_composer_panel.test.mjs` 模板 — fake translator + 無繁中字元洩漏 + 必填欄位斷言)+ ManifestPanel envelope test(`manifestPanel.test.mjs` — rpc 失敗觸發 ShowError / reentrant guard / registerCleanup / attachIframe ready promise)。`package.json` test glob 修為 `node --test src/**/*.test.mjs`(recursive) — 既有 known issue 順手修。

## Why

- **LANGUAGE.md depth 原則 — 5 個 caller**(handoff evidence:`show*Panel` 269-2487 / `loadManifestSubjects` 4 處 / RPC envelope 8 處 / phase-checkbox 2 處 / `phaseOrder` whitelist 4 處 hardcode)各自重寫 panel template / loadManifestSubjects / RPC envelope / phase-checkbox render 是 shallow 反命題;單一 ManifestPanel 持有所有 boilerplate = locality + leverage 雙贏。
- **既有 [[ADR-0003]] 已在 transport 層收乾 CCI/Composer 共用 bridge**,ManifestPanel 是同模式上移一層 panel 層 — pattern consistency 對 maintainer 友善。
- **CCI / Composer 跟其他 3 panel 的 result 形狀本來看起來「太不一樣」**(iframe vs CSV success),cross-check 後發現只是 panel-specific render detail 差異,**boilerplate 真的同形** — Path 3 hybrid 用 closure injection 自然 hold 差異,不需 declarative spec。
- **既有 `frontend/src/panels/chart_composer_panel.mjs` 已部分走「panel module 抽出」的路**(只抽 HTML template builder + i18n coverage test),擴展到「整 spec object + envelope 收乾」是 natural progression,既有 test 模板可直接套到 5 panel。

## Considered Options

- **Fork B(Composer 不進 ManifestPanel,另開 `VisualizationPanel` sibling)**:單 sibling 違反 `one adapter = hypothetical seam`、且 sibling 跟 ManifestPanel 又共用 panel template / subject load / phase-checkbox,要嘛重複要嘛抽第三層。拒。
- **Fork C(Composer panel 維持 inline 不抽)**:5 panel 中只剩 1 個 inline 等於 enforce convention 失效,phase-checkbox 4 處 hardcode 不會被本 candidate 解決。拒。
- **命名 `AnalysisPanel`**:跟 backend [[Analysis pipeline family]] 字面衝突,且 Composer 不在後者。拒。
- **Path 1 Framework(整段 declarative spec)**:`onResult` / `rpc` 大 closure 一坨,locality 沒真正收;formBody string 失 IDE autocomplete + lint。拒。
- **Path 2 Toolkit(純 helper utility)**:每 panel 6+ 行樣板,collapse 力道弱;convention enforcement 靠 review,易 drift。拒。
- **ctx Option A(raw select value caller 自解析)**:Subject 不對稱被推到每 caller,collapse 沒做完。拒。
- **ctx Option C(改 backend 強制統一 idx 或 name)**:擴 candidate scope 動 backend,且 Composer subject string 是 [[ADR-0002]] canonical-key 設計,動之前要驗證。拒。
- **`result.chart` / `result.csvSuccess` orthogonal flags**:cross-check 後不夠表達實際 panel-specific 結果區(CCI 4 段 / Composer chart + PNG / MuscleRatio subjects list)。拒,改 `onResult` closure + helpers。
- **`result` discriminated union(`kind: 'csv-success' | 'iframe-chart-csv' | 'iframe-chart-only'`)**:`iframe-chart-csv` 跟 `iframe-chart-only` 兩 kind 共用 iframeId / onResult 兩欄,重複 spec shape;新增第 4 種組合(modal-only 等)爆 enum。拒。
- **`i18nPrefix` convention(spec 帶 prefix 自動組 status keys)**:跟既有 i18n schema 不對齊(實際 schema 是 running 訊息 panel-specific、done/failed 共通),違反「不擴 schema」原則。拒,改「spec 只暴露 panel-specific 2 個 key,其他 hardcode 共通」。
- **`formBody` string only**:locale change 時 frozen,無法 retranslate;`onLocaleChange` re-run 無效。拒,改 builder function。
- **`mp.attachIframe` 包 bridge.subscribe**:違反 [[ADR-0003]] §reversibility「不要在 bridge 上加 third layer」。拒,改 helper 只 own iframe lifecycle + `mp.registerCleanup` slot,bridge call 由 closure 直接走。
- **`mp.scope.xxx` panel state namespace**:locality 提升但跨 mp.run() 邊界 state(`_cciResult` for download)變難管理,且既有 5 panel 用 app this 已 ship。拒。
- **Pilot migration(先 Composer 後 4 panel)**:spec shape 在 grilling 已完整,無需 pilot 驗 unknown;5 個 PR 過渡期 maintainer 要記兩種 pattern。拒,改一次 5 panel 同 PR。
- **Risk #3(panelDispatch vs switch redundancy)留 follow-up issue**:跟本 candidate migration 5 entry 高度耦合,不 fold 會留下「半動」結果(部分 case 改 ManifestPanel.run、部分留 showXxxPanel)。拒,fold 進本 PR。

## Reversibility

中。

- **不可逆**:
  - 5 panel 從 `show*Panel()` + inline RPC envelope migrate 到 spec object + `ManifestPanel.run` — 動 5 個 spec file + 1 個 main.js,revert 跨 commit 可技術復原但 follow-up code 會在新架構上堆積。
  - `handleMenuAction` switch fold 進 panelDispatch 是單向(switch 整段刪)。
  - CONTEXT.md `ManifestPanel` 條目 + 本 ADR 一旦 commit,future architecture review 會以此為前提。
- **可逆**:
  - spec shape 內欄位細節(`formBody` string vs builder function、`silentSuccess` boolean vs object、ctx 欄位增刪)— 都是 ManifestPanel internal 改動,5 panel migration 後改動只影響 spec definition 5 處,可逐 panel 對齊。
  - `mp.attachIframe` 的 `height` 參數對應方式(Composer 寫 `1300px`、CCI 寫 `620px`)— internal helper detail,可逐用例調整。
  - `mp.registerCleanup` 改名 / 簽名變更 — internal API。
- **格外注意**:
  - 第 2 個 [[ADR-0003]] 模式上移到 panel 層的 deepening — 若未來加第 6 個 manifest panel(例如新分析方法),要對齊 ManifestPanel 既有 spec shape,不能變成 sibling deepening。
  - 跨 manifest 層的 cross-panel state(例如 download path 從 result 區拿 chart dataURL)維持 app this 自管的 contract,不要為 locality 把 state 強遷進 mp instance。
