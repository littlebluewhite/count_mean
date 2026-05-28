// frontend/src/panels/cci_spec.mjs
//
// CCI(Rudolph 共同收縮)panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M3 交付)。CCI 是 ManifestPanel 的「標準案例」:
//   * runBtnLabelKey 省略 → 走 default 'button.start_analyze'(不像 Composer 覆寫)
//   * silentSuccess 省略(預設 false)→ 成功後 envelope 彈 ShowMessage
//   * subject 用 subjectIdx(number)而非 Composer 的 subjectName(string)
//
// 設計:
//   * spec.formBody — CCI 原 panel(main.js showCCIPanel:1233-1273)的表單欄位
//     (manifest / dataFolder / subject)全部由 shell 提供,CCI 沒有額外欄位,
//     故 formBody 回空字串。保留 builder function 形狀以對齊 spec contract。
//   * spec.rpc — 呼 `AnalyzeCCI(CCIParams)` 並把 result.success=false 升為 throw,
//     讓 envelope 統一走 ShowError(對齊既有 executeCCIAnalysis main.js:1352-1357
//     的 success/message 雙檢查路徑)。pre-validation guard(manifestFile /
//     dataFolder / subjectIndex)集中在此 — 既有路徑(main.js:1339-1342)是
//     `await ShowError + return`,改 throw 後 envelope 統一 ShowError 一次。
//   * spec.onResult — 把舊 showCCIResult(main.js:1381-1555)的 4 段結果區整段
//     closure 化:info metadata / iframe chart + phase checkboxes / report /
//     download + open-output-folder buttons。iframe attach 走 mp.attachIframe,
//     phase checkbox 走 mp.bindPhaseCheckboxes(帶 onUpdate 刷新 pct 文字面板)。
//
// **後端 param / result 欄位名(已對 Go struct 驗證,勿臆測)**:
//   CCIParams  { manifestFile, dataFolder, subjectIndex }   ← gui/cci_handlers.go:19
//     (注意:不是 manifestPath / subjectIdx — Go json tag 是 manifestFile /
//      subjectIndex,handoff sketch 寫錯,以 Go struct 為準)
//   CCIResult  { outputCSVPath, subject, pairNames, chartHTML, phasePercents,
//                phaseTimes, report, success, message }      ← gui/cci_handlers.go:26
//
// **不在本 file 內動的事(M5 wiring 處理)**:
//   * `_cciResult` 由 onResult 寫(downloadCCIChart main.js:1638 讀 .subject;
//     bridge.subscribe handler 讀它判斷 result 仍在)。ownership 維持 app this
//     (ADR-0007 §9 panel state 留 app this)。
//   * `_cciIframeReady` promise:downloadCCIChart(main.js:1632)在 await 它後才送
//     PNG requestReply。onResult 把 attachIframe().ready 寫進 `app._cciIframeReady`
//     讓既有 downloadCCIChart 路徑無痛工作。
//   * `_cciCheckedPhases` Set:由 mp.bindPhaseCheckboxes 經 checkedSet 參數 mutate;
//     ownership 仍在 app this。原 main.js 沒有此 Set(每次 render 全勾),M3
//     引入是為了讓 ManifestPanel cross-render persistence 與 Composer 對稱。
//   * download / open-output-folder button onclick="app.downloadCCIChart()" /
//     "app.openOutputFolder()" 由 onResult render 進 #mpResultContent,實作仍在
//     main.js(M5 不動)。
//
// invariant(由 cci_spec.test.mjs 釘):
//   * spec.formBody 為 builder function(CCI 無額外欄位 → 回空字串,無繁中洩漏)
//   * runBtnLabelKey 省略(undefined)、silentSuccess 省略(undefined / falsy)
//   * rpc 在 manifestFile / dataFolder 缺、subjectIndex NaN/<0 時 throw
//   * rpc 在 backend result.success=false 時 throw(升 soft error 為 ShowError)

import { bridge } from '../charts/iframeBridge.mjs';
import { recalcPercents } from '../charts/phaseMarkers.mjs';
import { AnalyzeCCI, LoadPhaseManifest } from '../../wailsjs/go/gui/App.js';

/**
 * CCI panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * CCI 原 panel(main.js:1233-1273)只有 manifest / dataFolder / subject 三段
 * form-group + run button + result section,全部由 shell 提供。CCI 沒有 Composer
 * 那種「load EMG channels / channel selector」額外欄位,故 formBody 回空字串。
 *
 * 仍維持 builder function 形狀(而非直接省略)以對齊 ManifestPanel spec contract
 *(`spec.formBody(tH)` 在 _renderShell 內無條件呼叫)+ 對齊其餘 4 panel 一致性。
 * 接 translator 參數即使目前不用,也保留簽名讓 M4/未來新增欄位時無需改 shell。
 *
 * @param {(key: string) => string} _t - translator(目前無欄位 → 未使用)
 * @returns {string} formBody HTML(空字串)
 */
export function cciFormBody(_t) {
    return '';
}

/**
 * CCI spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(不引 globalThis.app):onResult 需寫 `_cciResult` /
 * `_cciIframeReady` / `_cciCheckedPhases`,onUpdate callback 需呼
 * `app._updatePhasePositionDisplay(pcts)`。App-side state 仍由 app this 自管
 *(ADR-0007 §9),spec 不持有 state。
 *
 * 為何不直接 import globalThis.app:test 友善 — happy-dom 沒掛 app,且讓 caller
 * 自由換 stub(rpc throw 路徑測 stubbed ctx,不真正觸發 AnalyzeCCI)。
 *
 * @param {object} app - EMGAnalysisApp 實例(必須帶 _updatePhasePositionDisplay /
 *   downloadCCIChart / openOutputFolder 方法 + 持久 state 欄位)
 * @returns {object} spec for ManifestPanel.run(spec)
 */
export function makeCciSpec(app) {
    return {
        titleKey: 'panel.cci.title',
        statusRunningKey: 'status.cci_running',
        // runBtnLabelKey 省略 → ManifestPanel shell fallback 'button.start_analyze'
        //   (CCI 是「開始分析」按鈕,不像 Composer 是「生成圖表」)。
        // silentSuccess 省略 → 預設 false → envelope 成功後彈 ShowMessage
        //   (對齊 executeCCIAnalysis main.js:1354 既有 ShowMessage(success) 行為)。

        formBody: cciFormBody,

        /**
         * Subject load(ADR-0007 §4 index-mode):manifest 選好後 ManifestPanel
         * selectMpManifest 呼此 fn 取 subject 列表填 #mpSubject。CCI 走
         * LoadPhaseManifest(主題字串陣列),valueMode='index' → option.value 寫
         * 0-based 索引(對齊舊 loadCCIManifestSubjects main.js:1304-1308 `option.value
         * = index`)。_gatherCtx 的 subjectIdx = parseInt(value) 對應後端 subjectIndex。
         *
         * @param {string} manifestPath - #mpManifestPath value
         * @returns {Promise<{subjects: string[], valueMode: 'index'}>}
         */
        loadSubjects: async (manifestPath) => ({
            subjects: await LoadPhaseManifest(manifestPath),
            valueMode: 'index',
        }),

        /**
         * 呼 AnalyzeCCI RPC。在 envelope 包之內 — throw 後 envelope 統一接
         * ShowError + status failed,對齊既有 executeCCIAnalysis(main.js:1352-1357)
         * 的 `if (result.success) {...} else { ShowError }` 路徑。
         *
         * Pre-validation guards 集中在這 — 既有路徑(main.js:1339-1342)是
         * `if (!manifestFile || !dataFolder || isNaN(subjectIndex)) { ShowError + return }`,
         * 改 throw 後 envelope 統一 ShowError 一次。throw 訊息沿用裸字串(原因見
         * guard 內註解),soft-error 升 throw 帶 backend message。
         *
         * @param {object} ctx - ManifestPanel _gatherCtx 結果
         *   { manifestPath, dataFolder, subjectIdx, subjectName, subjects }
         * @returns {Promise<object>} CCIResult(success / message / chartHTML / ...)
         */
        rpc: async (ctx) => {
            // ctx.manifestPath / ctx.dataFolder 來自 shell 的 #mpManifestPath /
            // #mpDataFolder input(_gatherCtx 規範化命名),對應後端 CCIParams 的
            // manifestFile / dataFolder。ctx.subjectIdx 是 parseInt(option.value),
            // CCI option.value 寫的是 index(main.js:1306 `option.value = index`),
            // 故 subjectIdx 直接對應後端 subjectIndex。
            if (!ctx.manifestPath || !ctx.dataFolder || Number.isNaN(ctx.subjectIdx) || ctx.subjectIdx < 0) {
                // 對齊 main.js:1340 既有錯誤文案(error.msg.fill_required_fields →
                // 「請填寫所有必要欄位」)。沿用裸字串而非 globalThis.t(key):對齊 M2
                // chart_composer_spec.mjs:140 rpc throw 裸字串慣例,且 rpc 不該依賴
                // globalThis.t 在 test 環境存在。envelope catch 後會以
                // error.msg.analysis_failed_dynamic 包裝此訊息顯示(manifestPanel.mjs:150)。
                // M5 i18n sweep 若要 rpc 直走 i18n,需先確保 t 在 spec scope 可達。
                throw new Error('請填寫所有必要欄位');
            }

            const result = await AnalyzeCCI({
                // 後端 CCIParams json tag:manifestFile / dataFolder / subjectIndex
                //(gui/cci_handlers.go:19,以 Go struct 為準勿臆測)。
                manifestFile: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
                subjectIndex: ctx.subjectIdx,
            });

            // backend HandlerRun 用 Success=false 表 soft error(non-panic,見
            // cci_handlers.go:114 failedCCIResult);envelope 需要 throw 才能走
            // ShowError → 把 soft error 升 throw(對齊 main.js:1356 ShowError 路徑)。
            if (!result.success) {
                throw new Error(result.message || 'CCI 分析失敗');
            }
            return result;
        },

        /**
         * Render CCI 結果區(對齊舊 showCCIResult main.js:1381-1555 的 4 段):
         *   1. info metadata(主題 / 肌肉配對 / CSV 輸出)— textContent 防 XSS
         *      (subject / pairNames / outputCSVPath 來自後端 CSV,user-controlled)
         *   2. iframe chart + phase checkboxes(走 mp.attachIframe + bindPhaseCheckboxes)
         *   3. 分析報告 textarea(report 來自後端拼接含 subject,readonly textarea)
         *   4. button row(下載圖表 / 開啟輸出資料夾)
         *
         * 流程(對齊 main.js + ADR-0007 §8 + M1 review I1):
         *   1. 持久化 _cciResult(downloadCCIChart 讀 .subject + bridge handler 讀它)
         *   2. 在 #mpResultContent 內 render 4 段 skeleton(info list / chart wrapper /
         *      report / phase selector / pct display / button row)
         *   3. populate info list + report(textContent,XSS-safe)
         *   4. attachIframe(height 620px,CCI default)→ 記 _cciIframeReady
         *   5. await ready 後:先 bindPhaseCheckboxes(M1 I1 precondition:iframe-side
         *      listener 掛好後才送首次 update)→ 再 bridge.subscribe(re-draw on chart
         *      restore/legend)經 registerCleanup 註冊。順序見 code 內 :270 註解。
         *
         * @param {object} result - CCIResult(欄位見 file header,已對 Go struct 驗證)
         * @param {object} _ctx - ManifestPanel _gatherCtx 結果(本 onResult 不用)
         * @param {object} mp - ManifestPanel 實例(attachIframe / bindPhaseCheckboxes /
         *   registerCleanup 用)
         */
        onResult: async (result, _ctx, mp) => {
            // 1. 持久化 result 給 downloadCCIChart(讀 .subject)+ bridge handler。
            app._cciResult = result;

            // 2. Render #mpResultContent skeleton。
            //    沿用 innerHTML + tHtml(對齊 ADR-0007 §6 既有 convention,不引 DOM
            //    builder)。tHtml 對 static i18n key 已做 escapeHtml;user-controlled
            //    欄位(subject / pairNames / outputCSVPath / report)走後續 textContent
            //    填入,不進 innerHTML(對齊舊 showCCIResult P0-A H2 XSS hardening)。
            const wrapper = document.getElementById('mpResultContent');
            const tH = globalThis.tHtml;
            // i18n:phase_display / analysis_report / open_output_folder 三個 key
            // 後端字典已有(internal/i18n/i18n.go:354/363/300),直接走 tH 不擴
            // schema(ADR-0007 §6 對齊)。「下載圖表」M5 i18n sweep 已補
            // button.download_chart key(語意「下載圖表」≠ button.download_png「下載 PNG」),
            // 改走 tH。對齊 M2 chart_composer_spec.mjs onResult 用 tH(key) 於 result 區的慣例。
            wrapper.innerHTML = `
                <div class="result-info" id="cciInfoList"></div>
                ${result.chartHTML ? `
                <div style="margin:1rem 0;">
                    <div id="cciChartContent"></div>
                    <div class="form-group" id="cciPhaseSelectorGroup" style="margin-top:0.5rem;">
                        <label><strong>${tH('result.label.phase_display')}</strong></label>
                        <div id="cciPhaseSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;"></div>
                    </div>
                    <div id="cciPhasePositions" style="margin-top:0.5rem;"></div>
                    <div class="button-group" style="margin-top:0.5rem;">
                        <button class="btn btn-primary" onclick="app.downloadCCIChart()">${tH('button.download_chart')}</button>
                    </div>
                </div>` : ''}
                ${result.report ? `
                <div class="result-report">
                    <h4>${tH('result.label.analysis_report')}</h4>
                    <pre class="result-pre" id="cciReportText"></pre>
                </div>` : ''}
                <div class="button-group">
                    <button class="btn btn-primary" onclick="app.openOutputFolder()">${tH('button.open_output_folder')}</button>
                </div>
            `;
            document.getElementById('mpResult').style.display = 'block';

            // 3. populate info list(user-controlled value → textContent,XSS-safe)。
            //    對齊舊 showCCIResult main.js:1386-1403 的 infoRows 結構。label 走
            //    globalThis.t(plain translator,非 tHtml — 因為寫進 strong.textContent
            //    不需 HTML escape)。subject / muscle_pairs / csv_output 三個 key
            //    後端字典已有(internal/i18n/i18n.go:351/357/358,文案完全一致),
            //    直接走 i18n 不擴 schema。對齊 _updatePhasePositionDisplay:1607 既有
            //    `t('result.label.phase_positions')` textContent 慣例。
            const tt = globalThis.t;
            const infoList = document.getElementById('cciInfoList');
            const pairNamesText = Array.isArray(result.pairNames) ? result.pairNames.join(', ') : '';
            const infoRows = [
                [tt('result.label.subject'), result.subject],
                [tt('result.label.muscle_pairs'), pairNamesText],
                [tt('result.label.csv_output'), result.outputCSVPath],
            ];
            infoRows.forEach(([label, value]) => {
                const p = document.createElement('p');
                const strong = document.createElement('strong');
                strong.textContent = label;
                p.appendChild(strong);
                p.appendChild(document.createTextNode(value != null ? String(value) : ''));
                infoList.appendChild(p);
            });

            // report textContent(後端拼接含 subject;textContent 防 XSS)。
            if (result.report) {
                const reportEl = document.getElementById('cciReportText');
                if (reportEl) reportEl.textContent = result.report;
            }

            // 無 chartHTML 時(極少數 — 例如 chart render 失敗但分析成功)無 iframe /
            // phase checkbox 可綁,直接結束 onResult(info + report + open-folder 已 render)。
            if (!result.chartHTML) return;

            // 4. attachIframe — height 620px(CCI default,對齊 main.js:1504)。
            const { iframe, ready } = mp.attachIframe({
                containerId: 'cciChartContent',
                html: result.chartHTML,
                height: '620px',
            });

            // 寫入 _cciIframeReady — 既有 downloadCCIChart(main.js:1632)會 await 它
            // 再送 PNG requestReply,避免 iframe customJS listener 尚未掛好就 timeout。
            // M5 wiring 不動 downloadCCIChart,本欄位需保留。
            app._cciIframeReady = ready;

            // 5. await ready 後再 subscribe + bindPhaseCheckboxes(M1 review I1
            //    precondition:iframe-side message listener 在 load 後才掛,首次
            //    emitUpdate 的 bridge.send 必須等到 listener 在線才不會 silent drop)。
            await ready;

            // _cciCheckedPhases 跨 render 持久(對齊 Composer _composerCheckedPhases;
            // 原 main.js 每次全勾,M3 引入 Set 為 ManifestPanel 對稱)。
            if (!app._cciCheckedPhases) app._cciCheckedPhases = new Set();

            // bindPhaseCheckboxes:phase checkbox render + change → bridge.send
            // 'cci-update-phase-markers' + onUpdate 刷新 pct 文字面板。收成 named
            // function 讓下方 bridge.subscribe handler 的 re-draw 能重用(chart
            // restore / legend change 後重綁同一組 checkbox + 觸發首次 emitUpdate)。
            // 先定義 + 先呼一次,再 subscribe — 避免 handler 引用尚未初始化的 const。
            const rebindCciPhaseCheckboxes = () => {
                mp.bindPhaseCheckboxes({
                    phaseTimes: result.phaseTimes,
                    adapter: 'cci',
                    containerId: 'cciPhaseSelector',
                    bridge,
                    iframe,
                    recalcPercents,
                    checkedSet: app._cciCheckedPhases,
                    // onUpdate(M3 prep):bridge.send 後刷新 pct 文字面板。對齊
                    // 舊 updateCCIPhaseLines:1577 `this._updatePhasePositionDisplay(pcts)`。
                    onUpdate: (pcts) => app._updatePhasePositionDisplay(pcts),
                });
            };
            rebindCciPhaseCheckboxes();

            // bridge.subscribe:CCI chart restore / legend change 時 re-draw phase
            // markers(對齊 main.js:1541-1546)。經 registerCleanup 註冊,下次 run
            // 開頭自動 flush(取代既有 `this._cciBridgeUnsub?.()` ad-hoc pattern,
            // ADR-0007 §8)。handler 內讀 app._cciResult 判 result 仍在;type 為
            // 'cci-chart-restored' / 'cci-chart-legend-changed' 時 100ms 後重畫。
            //
            // 與舊 main.js 差異:舊路徑 restore 時呼 updateCCIPhaseLines()(只重算 +
            // re-send,不動 checkbox DOM);本版改呼 rebindCciPhaseCheckboxes()(會
            // 清空 + 重建 checkbox DOM)。因 _cciCheckedPhases Set 跨 render 持久,
            // 重建後勾選狀態一致,且 bindPhaseCheckboxes 尾端 emitUpdate 同樣 re-send
            // markers + 刷新 pct,net effect 等價。多一次 DOM rebuild 屬可接受成本
            //(避免在 helper 另開 standalone emitUpdate 公開 API 擴 scope)。
            const unsub = bridge.subscribe(iframe, 'cci-chart-*', (_payload, type) => {
                if (!app._cciResult) return;
                if (type === 'cci-chart-restored' || type === 'cci-chart-legend-changed') {
                    setTimeout(() => rebindCciPhaseCheckboxes(), 100);
                }
            });
            mp.registerCleanup(unsub);
        },
    };
}
