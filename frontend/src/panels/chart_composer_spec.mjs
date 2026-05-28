// frontend/src/panels/chart_composer_spec.mjs
//
// Chart Composer panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M2 中段交付)。
//
// 設計:
//   * spec.formBody 從原 `buildChartComposerPanelHtml` 抽,但刪去 shell 已提供的
//     panel-header / manifest / dataFolder / subject 三段 form-group + run button
//     + result section(由 shell 透過 #mpResult / #mpResultContent 統一持有)。
//     留下 Composer 獨有的:warning banner、load_emg_channels button + grid info、
//     channel selector、phase selector。
//   * spec.rpc:呼 `GenerateChartComposer` 並把 result.success=false 升為 throw,
//     讓 ManifestPanel envelope 統一走 ShowError(對齊既有 generateComposerChart
//     line 1902-1906 的 silent-fail-to-ShowError 行為)。
//   * spec.onResult:把舊 generateComposerChart line 1908-1967 的「inject iframe →
//     await ready → render phase checkboxes」整段 closure 化,直接呼 `mp.attachIframe`
//     + `mp.bindPhaseCheckboxes`。
//   * silentSuccess: true — 對齊 ADR-0007 §6 + handoff 的鎖定行為(chart 顯現
//     即為成功訊號,不彈 dialog)。
//   * runBtnLabelKey: 'button.generate_chart' — M2 prep API extension,
//     shell #mpRunBtn 是「生成圖表」按鈕(不是「開始分析」)。
//
// **不在本 file 內動的事**:
//   * `_composerSelectedChannels` Set 由 `app.loadComposerEMGChannels()` 管理,
//     channel checkbox 內 onchange add/delete Set。M2 不動,M5 wiring 確認
//     onclick 仍呼 `app.loadComposerEMGChannels()`。
//   * `_composerCheckedPhases` Set 由 `mp.bindPhaseCheckboxes` 經 checkedSet 參數
//     mutate;ownership 仍在 app this(ADR-0007 §9 panel state 留 app this)。
//   * PNG download button onclick="app.downloadComposerChart()" 由 onResult
//     render 進 #mpResultContent,實作仍在 main.js(M5 不動)。
//   * `_composerEMGMotionOffset` / `_composerLoadedSubject` 由
//     `app.loadComposerEMGChannels` 設;rpc closure 只 read。
//   * `_composerIframeReady` promise:downloadComposerChart 在 await 它前面才送
//     PNG request。onResult 把 attachIframe().ready 寫進 `app._composerIframeReady`
//     讓既有 downloadComposerChart 路徑無痛工作。
//
// invariant(由 chart_composer_spec.test.mjs 釘):
//   * spec.formBody 內所有 user-visible text 必經 translator(無繁中字元)
//   * spec.formBody 呼 translator ≥ 5 次(比 chart_composer_panel.mjs 的 ≥9 少 4 處,
//     對應 shell 已包的 panel header / manifest / dataFolder / subject / back-button
//     + result.section.composer_preview / button.download_png + 第二個 back button —
//     扣除這些後 formBody 仍 ≥ 5 個翻譯點)
//   * silentSuccess === true、runBtnLabelKey === 'button.generate_chart'
//   * rpc 在 _composerSelectedChannels 空 / _composerLoadedSubject 不一致時 throw

import { bridge } from '../charts/iframeBridge.mjs';
import { recalcPercents } from '../charts/phaseMarkers.mjs';
import { GenerateChartComposer, LoadChartComposerSubjects } from '../../wailsjs/go/gui/App.js';

/**
 * Chart Composer panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * 跟 chart_composer_panel.mjs 比較,差異:
 *   - 刪除 panel-header(shell 持有)
 *   - 刪除 manifest / dataFolder / subject 三段 form-group(shell 持有,
 *     id 改為 mpManifestPath / mpDataFolder / mpSubject)
 *   - 刪除 run button + back button group(shell 持有,run 為 #mpRunBtn)
 *   - 刪除 result section(shell 提供 #mpResult / #mpResultContent,
 *     onResult 動態填入 chart container + download button)
 *
 * 保留:
 *   - composerWarningBanner(channel reconcile drop 警告,由 _showComposerWarning 寫入)
 *   - load_emg_channels button + composerGridInfo
 *   - composerChannelSelector(EMG 通道勾選)
 *   - composerPhaseSelector(phase checkbox,由 mp.bindPhaseCheckboxes 在 onResult 內動態填)
 *
 * @param {(key: string) => string} t - translator(production: tHtml,test: fake)
 * @returns {string} formBody HTML
 */
export function composerFormBody(t) {
    return `
            <!-- 寫入點:app._showComposerWarning,於 loadComposerEMGChannels 失敗時填充。
                 M3/M4 spec author 若搬動此 banner 必須對齊 caller。 -->
            <div id="composerWarningBanner" class="result-info"
                 style="display:none; background:#fff3cd; border-left:4px solid #ffc107; padding:0.5rem 0.75rem; margin-bottom:0.5rem;">
            </div>

            <div class="form-group">
                <button class="btn btn-secondary" onclick="app.loadComposerEMGChannels()">${t('button.load_emg_channels')}</button>
                <p class="help-text" id="composerGridInfo" style="margin-top:0.5rem;"></p>
            </div>

            <div class="form-group">
                <label>${t('form.label.composer_channels')}</label>
                <div id="composerChannelSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;">
                    <p class="help-text">${t('form.helptext.load_channels_first')}</p>
                </div>
            </div>

            <div class="form-group">
                <label>${t('form.label.composer_phases')}</label>
                <div id="composerPhaseSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;">
                    <p class="help-text">${t('form.helptext.composer_phases_pending')}</p>
                </div>
            </div>
        `;
}

/**
 * Chart Composer spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(不引 globalThis.app):rpc 需讀 `_composerSelectedChannels` /
 * `_composerLoadedSubject` / `_composerEMGMotionOffset`,onResult 需寫
 * `_composerPhaseTimes` / `_composerIframeReady` + 讀 `_composerCheckedPhases`。
 * App-side state 仍由 app this 自管(ADR-0007 §9),spec 不持有 state。
 *
 * 為何不直接 import globalThis.app:test 友善 — happy-dom 沒掛 app,且讓 caller
 * 自由換 stub(rpc throw 路徑測 stubbed app._composerSelectedChannels)。
 *
 * @param {object} app - EMGAnalysisApp 實例(必須帶 _composerSelectedChannels Set 等狀態)
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
         * onSubjectChange(Composer 獨有):subject 切換時 reset state + 清 chart。
         * 走 app.onComposerSubjectChange()(M5 wiring 保留此 method):reset
         * _composerEMGMotionOffset / _composerLoadedSubject + 清 chart container,
         * 強制 user 重按「載入 EMG 欄位」(codex P2#3 guard,避免上個 subject 的
         * offset silent 套到新 subject)。其他 4 panel 省略 onSubjectChange。
         *
         * @param {object} _mp - ManifestPanel 實例(本 hook 不用,走 app.onComposerSubjectChange)
         */
        onSubjectChange: (_mp) => app.onComposerSubjectChange(),

        /**
         * 呼 GenerateChartComposer RPC。在 envelope 包之內 — throw 後 envelope
         * 統一接 ShowError + status failed,對齊既有 generateComposerChart
         * line 1902-1906 silent-fail-to-ShowError 路徑。
         *
         * Pre-validation guards 集中在這 — 既有路徑(main.js:1875-1889)是
         * `await ShowError + return`,改 throw 後 envelope 統一 ShowError 一次。
         */
        rpc: async (ctx) => {
            const selectedChannels = Array.from(app._composerSelectedChannels || []);
            if (selectedChannels.length === 0) {
                // 對齊 main.js:1876 既有錯誤文案(現未走 i18n key,沿用裸字串)。
                // M5 wiring 階段若補 i18n key,改字面值即可。
                throw new Error('請至少選擇一個 EMG 通道');
            }
            // codex P2#3 guard:loadedSubject 必須等於當前 subject(避免上個 subject
            // 的 emgMotionOffset silent 套用到新 subject)。對齊 main.js:1883-1889。
            if (app._composerLoadedSubject !== ctx.subjectName) {
                throw new Error('主題已變更但未重新載入 EMG 欄位 — 請先按「載入 EMG 欄位」再生成圖表');
            }

            const result = await GenerateChartComposer({
                manifestPath: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
                subject: ctx.subjectName,        // ADR-0002 §1 canonical-key:Composer 用 subject string
                selectedChannels,
                emgMotionOffset: app._composerEMGMotionOffset || 0,
            });
            // backend HandlerRun 用 Success=false 表 soft error(non-panic);
            // envelope 需要 throw 才能走 ShowError → 把 soft error 升 throw。
            if (!result.success) {
                throw new Error(result.message || 'Chart Composer 生成失敗');
            }
            return result;
        },

        /**
         * Render chart iframe + download button + phase checkboxes。
         *
         * 流程(對齊 main.js:1908-1967):
         *   1. 在 #mpResultContent 內填 chart container + PNG download button
         *   2. 持久化 phaseTimes 給 _updateComposerPhaseLines / downloadComposerChart
         *   3. attachIframe — sandbox=allow-scripts、height=1300px(image #18 經驗)
         *   4. 記 _composerIframeReady promise(downloadComposerChart 之前 await 它)
         *   5. await ready 後 bindPhaseCheckboxes(M1 review I1 precondition:
         *      必須在 iframe-side listener 掛好之後再送首次 update)
         *
         * @param {object} result - ChartComposerResult {html, phaseTimes, success, message}
         * @param {object} ctx - ManifestPanel _gatherCtx 規範化結果
         * @param {object} mp - ManifestPanel 實例(`attachIframe` / `bindPhaseCheckboxes` 用)
         */
        onResult: async (result, _ctx, mp) => {
            // 1. Build #mpResultContent — chart container + PNG download button。
            //    沿用 innerHTML + tHtml(對齊 ADR-0007 §6 既有 convention,不引 DOM builder)。
            const wrapper = document.getElementById('mpResultContent');
            const tH = globalThis.tHtml;
            wrapper.innerHTML = `
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
