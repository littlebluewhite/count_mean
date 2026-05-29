// frontend/src/panels/chart_composer_spec.mjs
//
// Chart Composer panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M2 中段交付)。
//
// 設計:
//   * spec.formBody 刪去 shell 已提供的 panel-header / manifest / dataFolder /
//     subject 三段 form-group + run button + result section(由 shell 透過
//     #mpResult / #mpResultContent 統一持有)。ADR-0013(D1/D6)後 formBody
//     **收斂為空** —— load_emg_channels button / channel selector / warning banner
//     已隨一鍵生成流程(D1)刪除;分期點 selector 則搬到結果區(D6,onResult 內
//     由 bindPhaseCheckboxes 動態填),不再留在表單區。
//   * spec.rpc:呼 `GenerateChartComposer`(僅 manifest / dataFolder / subject
//     三參數,ADR-0013)並把 result.success=false 升為 throw,讓 ManifestPanel
//     envelope 統一走 ShowError。
//   * spec.onResult:「inject phase selector + iframe → await ready → render phase
//     checkboxes」整段 closure 化,直接呼 `mp.attachIframe` + `mp.bindPhaseCheckboxes`。
//   * silentSuccess: true — 對齊 ADR-0007 §6 + handoff 的鎖定行為(chart 顯現
//     即為成功訊號,不彈 dialog)。
//   * runBtnLabelKey: 'button.generate_chart' — shell #mpRunBtn 是「生成圖表」
//     按鈕(不是「開始分析」)。
//
// **不在本 file 內動的事**:
//   * `_composerCheckedPhases` Set 由 `mp.bindPhaseCheckboxes` 經 checkedSet 參數
//     mutate;ownership 仍在 app this(ADR-0007 §9 panel state 留 app this)。
//   * PNG download button onclick="app.downloadComposerChart()" 由 onResult
//     render 進 #mpResultContent,實作仍在 main.js。
//   * `_composerIframeReady` promise:downloadComposerChart 在 await 它前面才送
//     PNG request。onResult 把 attachIframe().ready 寫進 `app._composerIframeReady`
//     讓既有 downloadComposerChart 路徑無痛工作。
//
// invariant(由 chart_composer_spec.test.mjs 釘):
//   * spec.formBody 收斂為空(D6 後分期點移結果區):無 user-visible text、
//     不呼 translator(舊「≥ 2 次」下界已隨 phase selector 搬移取消)
//   * spec.onResult render 後 #composerPhaseSelector 容器存在於結果區
//     (bindPhaseCheckboxes 找得到容器,非 silent no-op)
//   * silentSuccess === true、runBtnLabelKey === 'button.generate_chart'

import { bridge } from '../charts/iframeBridge.mjs';
import { recalcPercents } from '../charts/phaseMarkers.mjs';
import { GenerateChartComposer, LoadChartComposerSubjects } from '../../wailsjs/go/gui/App.js';

/**
 * Chart Composer panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * ADR-0013(D1/D6)後 **收斂為空字串**:
 *   - D1 一鍵生成:刪除「載入 EMG 欄位」按鈕 / channel 勾選 UI / channel
 *     reconcile warning banner — Generate 直接預設全通道。
 *   - D6 版面:分期點 selector 從表單區搬到結果區(onResult 在 #mpResultContent
 *     的 chart container 之前注入 #composerPhaseSelector,再由 bindPhaseCheckboxes
 *     動態填),故表單區不再持有任何 Composer 獨有欄位。
 *
 * shell 已持有 panel-header / manifest / dataFolder / subject 三段 form-group /
 * run button / result section(#mpResult / #mpResultContent),formBody 無內容可加。
 *
 * 仍保留為 builder function(spec.formBody 契約是 `(t) => string`,shell 會呼
 * `spec.formBody(tH)`),回空字串。
 *
 * @param {(key: string) => string} _t - translator(production: tHtml,test: fake);
 *   formBody 收斂為空後不再使用,保留簽名以符合 spec.formBody 契約。
 * @returns {string} formBody HTML(空字串)
 */
export function composerFormBody(_t) {
    return '';
}

/**
 * Chart Composer spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(不引 globalThis.app):onSubjectChange 呼
 * `app.onComposerSubjectChange()`,onResult 需寫 `_composerPhaseTimes` /
 * `_composerIframeReady` + 讀 `_composerCheckedPhases`。App-side state 仍由
 * app this 自管(ADR-0007 §9),spec 不持有 state。
 *
 * 為何不直接 import globalThis.app:test 友善 — happy-dom 沒掛 app,且讓 caller
 * 自由換 stub。
 *
 * @param {object} app - EMGAnalysisApp 實例
 * @returns {object} spec for ManifestPanel.run(spec)
 */
export function makeChartComposerSpec(app) {
    return {
        titleKey: 'panel.composer.title',
        // status.composer_running 已於 M5 wiring 補進 4 locale(取代舊 hardcoded
        // 'Chart Composer 生成中...' main.js debt)。
        statusRunningKey: 'status.composer_running',
        runBtnLabelKey: 'button.generate_chart',
        // Composer 的成功訊號是「chart 出現」— ADR-0007 §6 + handoff 鎖定:
        // 不彈 ShowMessage dialog,避免 user 流程被打斷。
        silentSuccess: true,
        // Composer subject 依賴 dataFolder(LoadChartComposerSubjects 帶 dataFolder),
        // 故 selectMpDataFolder 寫完 dataFolder 後需 re-load subjects。僅 Composer 設此
        // 旗標 — index-mode 3 panel(CCI/PhaseSync/Normalized)subject 只依賴 manifest,
        // 設 true 會讓選 dataFolder 清空已選 subject(codex round-1 P2)。
        subjectsDependOnDataFolder: true,

        formBody: composerFormBody,

        /**
         * Subject load(ADR-0007 §4 **name-mode**):Composer 走
         * LoadChartComposerSubjects({manifestPath, dataFolder}) → result.subjects,
         * valueMode='name' → option.value 寫 subject **字串**(ADR-0002 §1
         * canonical-key:manifest 升版時 idx 位移、name 不變,故 Composer 用 name)。
         * 對齊舊 loadComposerSubjects(main.js:1746-1750 `opt.value = subject`)。
         *
         * Composer 的 loadSubjects 需要 dataFolder(LoadChartComposerSubjects 帶它),
         * 故 ManifestPanel selectMpDataFolder 寫完 dataFolder 後會 re-load(見
         * manifestPanel.mjs selectMpDataFolder 註解)。result.success=false 時 throw,
         * 讓 ManifestPanel selectMpManifest/DataFolder 的 catch 統一 ShowError
         *(對齊舊 loadComposerSubjects:1736-1738 的 `if (!success) ShowError + return`)。
         *
         * @param {string} manifestPath - #mpManifestPath value
         * @param {string} dataFolder - #mpDataFolder value
         * @returns {Promise<{subjects: string[], valueMode: 'name'}>}
         */
        loadSubjects: async (manifestPath, dataFolder) => {
            // 對齊舊 selectComposerManifest/DataFolder(main.js:1700/1715):只有在
            // manifest + dataFolder 都齊時才打 RPC(backend LoadChartComposerSubjects
            // RejectsEmptyDataFolder)。dataFolder 缺時回空 subjects(不 throw、不打
            // RPC)— ManifestPanel 此時保留 placeholder + 不 enable,等使用者選完
            // dataFolder 後 selectMpDataFolder 會 re-load。index-mode 3 panel 無此
            // 限制(LoadPhaseManifest 不需 dataFolder),故只 Composer loadSubjects 設此 guard。
            if (!dataFolder) {
                return { subjects: [], valueMode: 'name' };
            }
            const result = await LoadChartComposerSubjects({ manifestPath, dataFolder });
            if (!result.success) {
                // 升 throw 讓 ManifestPanel caller catch → ShowError(對齊舊路徑語意)。
                throw new Error(result.message || '載入主題失敗');
            }
            return { subjects: result.subjects || [], valueMode: 'name' };
        },

        /**
         * onSubjectChange(Composer 獨有):subject 切換時清 chart container +
         * 清勾選分期 Set。走 app.onComposerSubjectChange()。ADR-0013 後一鍵生成
         * 流程無 channel / offset state 需 reset。其他 4 panel 省略 onSubjectChange。
         *
         * @param {object} _mp - ManifestPanel 實例(本 hook 不用,走 app.onComposerSubjectChange)
         */
        onSubjectChange: (_mp) => app.onComposerSubjectChange(),

        /**
         * 呼 GenerateChartComposer RPC。在 envelope 包之內 — throw 後 envelope
         * 統一接 ShowError + status failed。
         *
         * ADR-0013 一鍵生成:不再有 channel-empty / loadedSubject pre-validation
         * guard(channel 預設全選、offset 由 backend 從 manifest row 讀),只傳
         * manifest / dataFolder / subject 三參數。
         */
        rpc: async (ctx) => {
            const result = await GenerateChartComposer({
                manifestPath: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
                subject: ctx.subjectName,        // ADR-0002 §1 canonical-key:Composer 用 subject string
            });
            // backend HandlerRun 用 Success=false 表 soft error(non-panic);
            // envelope 需要 throw 才能走 ShowError → 把 soft error 升 throw。
            if (!result.success) {
                throw new Error(result.message || 'Chart Composer 生成失敗');
            }
            return result;
        },

        /**
         * Render phase selector + chart iframe + download button + phase checkboxes。
         *
         * 流程(對齊 main.js:1908-1967):
         *   1. 在 #mpResultContent 內填(由上而下)分期點容器 #composerPhaseSelector
         *      → chart container → PNG download button。分期點容器排在 chart 之前
         *      (D6:從表單區搬到結果區),且必須在 bindPhaseCheckboxes(步驟 5)之前
         *      就存在於 DOM,否則 bindPhaseCheckboxes 會 silent no-op
         *      (manifestPanel.mjs:`if (!container) return`)→ checkbox 不 render。
         *   2. 持久化 phaseTimes 給 _updateComposerPhaseLines / downloadComposerChart
         *   3. attachIframe — sandbox=allow-scripts、height=1300px(image #18 經驗)
         *   4. 記 _composerIframeReady promise(downloadComposerChart 之前 await 它)
         *   5. await ready 後 bindPhaseCheckboxes(containerId='composerPhaseSelector',
         *      指向步驟 1 注入的結果區容器;M1 review I1 precondition:必須在 iframe-side
         *      listener 掛好之後再送首次 update)
         *
         * @param {object} result - ChartComposerResult {html, phaseTimes, success, message}
         * @param {object} ctx - ManifestPanel _gatherCtx 規範化結果
         * @param {object} mp - ManifestPanel 實例(`attachIframe` / `bindPhaseCheckboxes` 用)
         */
        onResult: async (result, _ctx, mp) => {
            // 1. Build #mpResultContent — 分期點容器(chart 之前)+ chart container +
            //    PNG download button。沿用 innerHTML + tHtml(對齊 ADR-0007 §6 既有
            //    convention,不引 DOM builder)。#composerPhaseSelector 必須在此就位,
            //    步驟 5 的 bindPhaseCheckboxes 才找得到容器(否則 silent no-op)。
            const wrapper = document.getElementById('mpResultContent');
            const tH = globalThis.tHtml;
            wrapper.innerHTML = `
                <div class="form-group">
                    <label>${tH('form.label.composer_phases')}</label>
                    <div id="composerPhaseSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;"></div>
                </div>
                <div id="composerChartContent"></div>
                <div class="button-group" style="margin-top:0.5rem;">
                    <button class="btn btn-primary" onclick="app.downloadComposerChart()">${tH('button.download_png')}</button>
                </div>
            `;
            document.getElementById('mpResult').style.display = 'block';

            // 2. 持久化 phaseTimes(downloadComposerChart 不用,但 _updateComposerPhaseLines
            //    M5 wiring 後若被 mp.bindPhaseCheckboxes 取代,這欄位仍是 phase-line
            //    handler 的真實來源)。對齊 main.js:1952。
            app._composerPhaseTimes = result.phaseTimes || {};

            // 3. attachIframe — height 1300px(經驗值,見 main.js:1924-1928 註解)。
            //    Composer 是 ManifestPanel 5 panel 中唯一覆寫 default 620px 的 caller。
            const { iframe, ready } = mp.attachIframe({
                containerId: 'composerChartContent',
                html: result.html,
                height: '1300px',
            });

            // 4. 寫入 _composerIframeReady — 既有 downloadComposerChart line 2089-2092
            //    會 await 它再送 PNG request,避免 iframe customJS listener 尚未掛好
            //    就 timeout。M5 wiring 不動 downloadComposerChart,本欄位需保留。
            app._composerIframeReady = ready;

            // 5. await ready,然後 bindPhaseCheckboxes — M1 review I1 precondition:
            //    iframe-side message listener 在 load 後才掛,首次 emitUpdate 的
            //    bridge.send 必須等到 listener 在線才不會 silent drop。
            await ready;

            if (!app._composerCheckedPhases) app._composerCheckedPhases = new Set();
            mp.bindPhaseCheckboxes({
                phaseTimes: result.phaseTimes,
                adapter: 'composer',
                containerId: 'composerPhaseSelector',
                bridge,
                iframe,
                recalcPercents,
                checkedSet: app._composerCheckedPhases,
            });
        },
    };
}
