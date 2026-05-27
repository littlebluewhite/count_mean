// Chart Composer panel HTML builder — 從 main.js showChartComposerPanel() 抽出。
//
// 抽離原因:
//   1. 讓 test 能用 happy-dom render panel HTML + 用 querySelector 釘 DOM,
//      不用 import main.js(會 trigger Wails runtime IIFE 連帶 RPC)。
//   2. Pure function + translator 注入 = test 注入 fake translator,
//      production 注入真實 tHtml,no module-level coupling。
//
// invariant(由 chart_composer_panel.test.mjs 釘):panel 內所有 user-visible
// text 必須經過 translator;render 後 textContent 不可含繁中字元範圍
// [一-龥]。

/**
 * 組 Chart Composer panel 的 HTML 字串。
 * @param {(key: string) => string} translator - 翻譯函式(production: tHtml,test: fake)
 * @returns {string} panel HTML
 */
export function buildChartComposerPanelHtml(translator) {
    return `
            <div class="panel-header">
                <h2>${translator('panel.composer.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${translator('button.back')}</button>
            </div>

            <div id="composerWarningBanner" class="result-info"
                 style="display:none; background:#fff3cd; border-left:4px solid #ffc107; padding:0.5rem 0.75rem; margin-bottom:0.5rem;">
            </div>

            <div class="form-group">
                <label>1. ${translator('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="composerManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="composerManifest" class="form-control" placeholder="${translator('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectComposerManifest()">${translator('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. ${translator('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="composerDataFolder" class="form-control" placeholder="${translator('form.placeholder.data_folder')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectComposerDataFolder()">${translator('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>3. ${translator('form.label.subject')}</label>
                <select id="composerSubject" class="form-control" disabled onchange="app.onComposerSubjectChange()">
                    <option value="">${translator('form.placeholder.load_manifest_first')}</option>
                </select>
            </div>

            <div class="form-group">
                <button class="btn btn-secondary" onclick="app.loadComposerEMGChannels()">${translator('button.load_emg_channels')}</button>
                <p class="help-text" id="composerGridInfo" style="margin-top:0.5rem;"></p>
            </div>

            <div class="form-group">
                <label>${translator('form.label.composer_channels')}</label>
                <div id="composerChannelSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;">
                    <p class="help-text">${translator('form.helptext.load_channels_first')}</p>
                </div>
            </div>

            <div class="form-group">
                <label>${translator('form.label.composer_phases')}</label>
                <div id="composerPhaseSelector" class="checkbox-group" style="display:flex; flex-wrap:wrap; gap:0.5rem;">
                    <p class="help-text">${translator('form.helptext.composer_phases_pending')}</p>
                </div>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.generateComposerChart()">${translator('button.generate_chart')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${translator('button.back')}</button>
            </div>

            <div id="composerResult" class="result-section" style="display:none;">
                <h3>${translator('result.section.composer_preview')}</h3>
                <div id="composerChartContent"></div>
                <div class="button-group" style="margin-top:0.5rem;">
                    <button class="btn btn-primary" onclick="app.downloadComposerChart()">${translator('button.download_png')}</button>
                </div>
            </div>
        `;
}
