// 導入 Wails 運行時
import './style.css';
import '../wailsjs/runtime/runtime.js';
import {
    SelectFile,
    SelectDirectory,
    ShowMessage,
    ShowError,
    GetConfig,
    SaveConfig,
    ResetConfig,
    CalculateMaxMean,
    NormalizeData,
    AnalyzePhases,
    GetCSVHeaders,
    GetAvailablePhases,
    DownloadCCIChart,
    GetVersion,
    DownloadChartComposerImage
} from '../wailsjs/go/gui/App.js';
import { OnFileDrop, OnFileDropOff, EventsOn, EventsOff } from '../wailsjs/runtime/runtime.js';
import { initI18n, t, tHtml, changeLanguage, onLocaleChange, getCurrentLocale } from './i18n.js';
import { bridge } from './charts/iframeBridge.mjs';
// ManifestPanel(ADR-0007):5 個 manifest+dataFolder panel 共用 envelope。
// 各 panel 差異由 spec object 注入(makeXxxSpec(this))。
import { ManifestPanel } from './manifestPanel.mjs';
import { makeCciSpec } from './panels/cci_spec.mjs';
import { makeChartComposerSpec } from './panels/chart_composer_spec.mjs';
import { makePhaseSyncSpec } from './panels/phase_sync_spec.mjs';
import { makeNormalizedPhaseSyncSpec } from './panels/normalized_phase_sync_spec.mjs';
import { makeMuscleRatioSpec } from './panels/muscle_ratio_spec.mjs';

// globalThis 暴露(M5 critical fix):ManifestPanel envelope + 5 個 spec module 讀
// globalThis.t / tHtml / ShowMessage / ShowError(它們是獨立 ES module,無法 import
// main.js 的 binding,否則 circular dep)。main.js 把這 4 個 import binding 掛上
// globalThis,production 才有值;test 環境由各 test 自行 stub globalThis.*。
globalThis.t = t;
globalThis.tHtml = tHtml;
globalThis.ShowMessage = ShowMessage;
globalThis.ShowError = ShowError;

// 應用程序主類
class EMGAnalysisApp {
    constructor() {
        // currentPanel 紀錄目前在哪一頁(由 handleMenuAction 設定)。
        // i18n locale 變更時,onLocaleChange listener 透過此屬性查表
        // 重新呼叫當前 panel 的 show() 函式以觸發 re-render。
        this.currentPanel = null;
        // ManifestPanel(ADR-0007):5 個 manifest+dataFolder panel 共用 deep module。
        this._manifestPanel = new ManifestPanel(this);
        this.panelDispatch = {
            maxMean: () => this.showMaxMeanPanel(),
            normalize: () => this.showNormalizePanel(),
            // 5 個 panel 走 ManifestPanel.run(spec)(ADR-0007 §10/§11):各 panel
            // 差異由 makeXxxSpec(this) 注入,共通 boilerplate(shell + subject load +
            // RPC envelope + phase-checkbox)由 ManifestPanel own。
            chart: () => this._manifestPanel.run(makeChartComposerSpec(this)),
            phase: () => this.showPhasePanel(),
            phaseSync: () => this._manifestPanel.run(makePhaseSyncSpec(this)),
            cci: () => this._manifestPanel.run(makeCciSpec(this)),
            normalizedPhaseSync: () => this._manifestPanel.run(makeNormalizedPhaseSyncSpec(this)),
            muscleRatio: () => this._manifestPanel.run(makeMuscleRatioSpec(this)),
            config: () => this.showConfigPanel(),
        };
        // Composer 跨 re-render / re-generate 持久的 phase 勾選 Set(ADR-0007 §9:
        // panel state 留 app this 自管)。在 constructor 初始化確保所有 entry path
        // (onSubjectChange clear / onResult bindPhaseCheckboxes)都不會裸 deref。
        this._composerCheckedPhases = new Set();
        this.config = null;
        this.init();
    }

    async init() {
        // 載入配置
        this.config = await GetConfig();

        // 載入 i18n 字典(以 config.language 為起點)+ 註冊 re-render listener。
        // Phase 2 (C):locale 變更時 snapshot 當前 panel 的 form values,
        // re-render 完成後寫回 — 避免使用者改了 input 但沒按「儲存設定」就切
        // 語言會丟失輸入。language 下拉本身不 snapshot(避免覆蓋使用者剛切換的選項)。
        await initI18n(this.config.language || 'zh-TW');
        onLocaleChange(async () => {
            // 更新主選單 + header 等靜態 HTML 文字(index.html 寫死的字串)
            this.updateStaticTexts();

            // re-render 當前 panel(如果有)
            if (!this.currentPanel || !this.panelDispatch[this.currentPanel]) {
                return;
            }
            // 區分「使用者切換語言」與「resetConfig / importConfig 觸發的程式化 reload」:
            // 後者已經把新 config 載入後端,如果在這裡 snapshot+restore 舊 form 值,
            // 會把舊值蓋回剛載入的新值(codex review P2 fix)。flag 為 true 時略過
            // restore(但仍需 re-render 以套用新 locale),用完即重置避免狀態漏出。
            const shouldRestore = !this._suppressFormRestore;
            this._suppressFormRestore = false;
            const snapshot = shouldRestore ? this.snapshotFormValues() : null;
            try {
                await this.panelDispatch[this.currentPanel]();
                if (shouldRestore) {
                    this.restoreFormValues(snapshot);
                }
            } catch (e) {
                console.error('[i18n] re-render failed:', e);
            }
        });

        // 首次載入時把 index.html 內寫死的中文字串套用當前 locale。
        this.updateStaticTexts();

        // 顯示版本號
        try {
            const version = await GetVersion();
            document.getElementById('versionBadge').textContent = version;
        } catch (e) {
            console.warn('無法取得版本號', e);
        }

        // 初始化拖曳功能
        this.initDragAndDrop();

        // 訂閱後端進度事件(P1-A14-2:取代舊的 polling 模型)
        this.initProgressSubscription();

        // ADR-0003:初始化 chart iframe bridge — singleton window-level
        // postMessage listener,後續 Composer / CCI 通訊都走此 bridge。
        // init() 自身 idempotent,HMR / 重複 init 不會累積 listener。
        // CCI 'cci-chart-*' subscription 改在 showCCIResult iframe 建好後做,
        // 避免在 init() 時 iframe 尚未存在的問題。
        bridge.init();

        // 綁定事件
        this.bindEvents();

        // 更新狀態
        this.updateStatus(t('status.app_ready'));
    }

    // 訂閱後端透過 Wails Events 推送的 "progress" 事件。
    // 後端 ProgressManager.UpdateProgress 會在每次 calculator 觸發進度時
    // 呼叫 runtime.EventsEmit(ctx, "progress", ProgressInfo)。前端只需 EventsOn
    // 一次即可在任何分析(CalculateMaxMean / AnalyzeCCI / AnalyzePhaseSync ...)
    // 期間收到 push 訊息,毋需 polling GetCurrentProgress。
    //
    // 目前 UI 還沒有進度條,先把資料保存在 this.latestProgress 並 debug log,
    // 後續若加入 progress bar 元件只要讀 this.latestProgress 即可。
    //
    // P2-9:idempotent 保護 — 若已 subscribed,先 unsub,避免 HMR / 重複 init
    // 累積多個 closure。teardownProgressSubscription 已有同邏輯,這裡直接 reuse。
    initProgressSubscription() {
        this.teardownProgressSubscription();
        this.latestProgress = null;
        // EventsOn 回傳 unsubscribe function;保存起來以便日後 reload 時清理。
        this._unsubscribeProgress = EventsOn('progress', (info) => {
            this.latestProgress = info;
            if (typeof console !== 'undefined' && console.debug) {
                console.debug('[progress]', info);
            }
        });
    }

    // 釋放進度事件訂閱(若未來在熱重啟 / SPA 切換時用到)。
    teardownProgressSubscription() {
        if (typeof this._unsubscribeProgress === 'function') {
            this._unsubscribeProgress();
            this._unsubscribeProgress = null;
        } else {
            // EventsOn 在某些舊版 Wails runtime 不回 unsubscribe — fallback 走 EventsOff
            EventsOff('progress');
        }
    }

    // 初始化拖曳功能。
    //
    // P2-9:Wails OnFileDrop 沒有 callback-level unregister(它是 single-callback
    // listener,內部全域覆寫),所以重複 init 時要先 OnFileDropOff() 清掉前一個
    // 註冊。沒此守護,HMR / re-init 後同一個 drop 事件會觸發多次 — drop 一個
    // 檔案會在多個 handler 中 race(可能跑到舊的 closure 引用了已銷毀的
    // EMGAnalysisApp instance,引發 silent state corruption)。
    initDragAndDrop() {
        // OnFileDropOff 在從未 OnFileDrop 過時也是 no-op-safe(Wails runtime 內部
        // 用 try/catch 把 handler 設成 null),所以可以無條件呼叫。
        if (typeof OnFileDropOff === 'function') {
            OnFileDropOff();
        }

        OnFileDrop((x, y, paths) => {
            // 找到目標元素
            const targetElement = document.elementFromPoint(x, y);
            const dropTarget = this.findDropTarget(targetElement);

            if (dropTarget && paths.length > 0) {
                // 只處理第一個檔案
                const filePath = paths[0];

                // 驗證是否為 CSV 檔案
                if (filePath.toLowerCase().endsWith('.csv')) {
                    this.handleFileDrop(dropTarget, filePath);
                } else {
                    ShowError(t('dialog.error'), t('error.msg.only_csv'));
                }
            }
        }, true);
    }

    // 找到拖曳目標元素
    findDropTarget(element) {
        while (element && element !== document.body) {
            if (element.hasAttribute('data-drop-target')) {
                return element;
            }
            element = element.parentElement;
        }
        return null;
    }

    // 處理檔案拖曳
    handleFileDrop(dropTarget, filePath) {
        const targetId = dropTarget.getAttribute('data-drop-target');
        const inputElement = document.getElementById(targetId);

        if (inputElement) {
            inputElement.value = filePath;

            // 觸發相應的處理邏輯。5 個 manifest panel 統一走 ManifestPanel shell 的
            // #mpManifestPath drop target — drop manifest 後依當前 spec load subject
            //(對齊 selectMpManifest 的 post-pick subject load,ADR-0007)。
            if (targetId === 'mpManifestPath') {
                this._manifestPanel.onMpManifestDropped();
            }
        }
    }

    bindEvents() {
        // 主選單按鈕事件
        document.querySelectorAll('.menu-button').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const action = e.currentTarget.dataset.action;
                this.handleMenuAction(action);
            });
        });
    }

    handleMenuAction(action) {
        // 紀錄 currentPanel,讓 i18n locale 變更時知道要重 render 哪個 panel。
        // ADR-0007 §10:原 9-case switch 與 panelDispatch map 完全 1:1 重複
        //(無 special-case / 無 default),fold 成 panelDispatch lookup。未知 action
        // 經 `?.()` no-op(對齊舊 switch 無 default 的 fall-through 行為)。
        this.currentPanel = action;
        this.panelDispatch[action]?.();
    }

    // 最大平均值計算面板
    showMaxMeanPanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.maxmean.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.process_mode')}</label>
                <select id="processMode" class="form-control" onchange="app.toggleProcessMode()">
                    <option value="single">${tHtml('form.option.single_file')}</option>
                    <option value="batch">${tHtml('form.option.batch_folder')}</option>
                </select>
            </div>

            <div id="singleFileSection">
                <div class="form-group">
                    <label>${tHtml('form.label.input_file')}</label>
                    <div class="input-group drop-zone" data-drop-target="inputFile" style="--wails-drop-target: drop;">
                        <input type="text" id="inputFile" class="form-control" readonly>
                        <button class="btn btn-secondary" onclick="app.selectInputFile()">${tHtml('button.browse')}</button>
                    </div>
                </div>
            </div>

            <div id="batchFolderSection" class="hidden">
                <div class="form-group">
                    <label>${tHtml('form.label.input_folder')}</label>
                    <div class="input-group">
                        <input type="text" id="inputFolder" class="form-control" readonly>
                        <button class="btn btn-secondary" onclick="app.selectInputFolder()">${tHtml('button.browse')}</button>
                    </div>
                </div>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.window_size')}</label>
                <input type="number" id="windowSize" class="form-control" value="1000" min="1">
                <p class="help-text">${tHtml('form.help.window_size')}</p>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.time_range')}</label>
                <div class="flex gap-2">
                    <div style="flex: 1;">
                        <input type="number" id="startTime" class="form-control" placeholder="${tHtml('form.placeholder.start_time')}" step="0.1">
                    </div>
                    <div style="flex: 1;">
                        <input type="number" id="endTime" class="form-control" placeholder="${tHtml('form.placeholder.end_time')}" step="0.1">
                    </div>
                </div>
                <p class="help-text">${tHtml('form.help.time_range')}</p>
            </div>

            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.calculateMaxMean()">
                    ${tHtml('button.start_calculate')}
                </button>
            </div>
        `;

        this.showPanel();
    }

    // 資料標準化面板
    showNormalizePanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.normalize.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.main_file')}</label>
                <div class="input-group drop-zone" data-drop-target="mainFile" style="--wails-drop-target: drop;">
                    <input type="text" id="mainFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMainFile()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.reference_file')}</label>
                <div class="input-group drop-zone" data-drop-target="referenceFile" style="--wails-drop-target: drop;">
                    <input type="text" id="referenceFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectReferenceFile()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.output_name')}</label>
                <input type="text" id="outputName" class="form-control" placeholder="${tHtml('form.placeholder.output_name')}">
            </div>

            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.normalizeData()">
                    ${tHtml('button.start_normalize')}
                </button>
            </div>
        `;

        this.showPanel();
    }

    // 階段分析面板
    showPhasePanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.phase.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.phase_file')}</label>
                <div class="input-group drop-zone" data-drop-target="phaseFile" style="--wails-drop-target: drop;">
                    <input type="text" id="phaseFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseFile()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.phase_points')}</label>
                <input type="text" id="phasePoints" class="form-control" placeholder="${tHtml('form.placeholder.phase_points')}">
                <p class="help-text">${tHtml('form.help.phase_points')}</p>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.phase_labels')}</label>
                <textarea id="phaseLabels" class="form-control" rows="4" placeholder="${tHtml('form.placeholder.phase_labels')}">啟跳下蹲階段
啟跳上升階段
團身階段
下降階段</textarea>
            </div>

            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.analyzePhases()">
                    ${tHtml('button.start_analyze')}
                </button>
            </div>
        `;

        this.showPanel();
    }

    // 系統配置面板 — Phase 1 frontend i18n MVP catalog 化版本。
    // 字串透過 t('config.xxx') 從 i18n.js in-memory 字典查;切換語言時
    // onLocaleChange listener 會重呼此函式重 render。語言下拉的 selected 採
    // getCurrentLocale() 而非 config.language —— 使用者切完語言但尚未按
    // 「儲存設定」前,以 i18n 當前 locale 為視覺基準才一致。
    async showConfigPanel() {
        const config = await GetConfig();
        const uiLocale = getCurrentLocale();

        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('config.panel.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('config.button.back')}</button>
            </div>

            <div class="config-sections">
                <div class="config-section">
                    <h3 class="section-title">${tHtml('config.section.data_processing')}</h3>

                    <div class="form-group">
                        <label>${tHtml('config.label.scaling_factor')}</label>
                        <input type="number" id="scalingFactor" class="form-control" value="${config.scalingFactor || 10}" min="1">
                        <p class="help-text">${tHtml('config.help.scaling_factor')}</p>
                    </div>

                    <div class="form-group">
                        <label>${tHtml('config.label.precision')}</label>
                        <input type="number" id="precision" class="form-control" value="${config.precision || 10}" min="0" max="15">
                        <p class="help-text">${tHtml('config.help.precision')}</p>
                    </div>

                    <div class="form-group">
                        <label>${tHtml('config.label.output_format')}</label>
                        <select id="outputFormat" class="form-control">
                            <option value="csv" ${config.outputFormat === 'csv' ? 'selected' : ''}>${tHtml('config.option.output_csv')}</option>
                            <option value="json" ${config.outputFormat === 'json' ? 'selected' : ''}>${tHtml('config.option.output_json')}</option>
                            <option value="xlsx" ${config.outputFormat === 'xlsx' ? 'selected' : ''}>${tHtml('config.option.output_xlsx')}</option>
                        </select>
                        <p class="help-text">${tHtml('config.help.output_format')}</p>
                    </div>

                    <div class="form-group">
                        <label>
                            <input type="checkbox" id="bomEnabled" ${config.bomEnabled ? 'checked' : ''}>
                            ${tHtml('config.label.bom_enabled')}
                        </label>
                        <p class="help-text">${tHtml('config.help.bom_enabled')}</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">${tHtml('config.section.directories')}</h3>

                    <div class="form-group">
                        <label>${tHtml('config.label.input_dir')}</label>
                        <div class="input-group">
                            <input type="text" id="inputDir" class="form-control" value="${config.inputDir || './input'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectInputDir()">${tHtml('config.button.browse')}</button>
                        </div>
                        <p class="help-text">${tHtml('config.help.input_dir')}</p>
                    </div>

                    <div class="form-group">
                        <label>${tHtml('config.label.output_dir')}</label>
                        <div class="input-group">
                            <input type="text" id="outputDir" class="form-control" value="${config.outputDir || './output'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectOutputDir()">${tHtml('config.button.browse')}</button>
                        </div>
                        <p class="help-text">${tHtml('config.help.output_dir')}</p>
                    </div>

                    <div class="form-group">
                        <label>${tHtml('config.label.operate_dir')}</label>
                        <div class="input-group">
                            <input type="text" id="operateDir" class="form-control" value="${config.operateDir || './value_operate'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectOperateDir()">${tHtml('config.button.browse')}</button>
                        </div>
                        <p class="help-text">${tHtml('config.help.operate_dir')}</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">${tHtml('config.section.phase_labels')}</h3>

                    <div class="form-group">
                        <label>${tHtml('config.label.phase_labels')}</label>
                        <textarea id="phaseLabels" class="form-control" rows="4">${(config.phaseLabels || []).join('\n')}</textarea>
                        <p class="help-text">${tHtml('config.help.phase_labels')}</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">${tHtml('config.section.advanced')}</h3>

                    <div class="form-group">
                        <label>${tHtml('config.label.log_level')}</label>
                        <select id="logLevel" class="form-control">
                            <option value="debug" ${config.logLevel === 'debug' ? 'selected' : ''}>${tHtml('config.option.log_level_debug')}</option>
                            <option value="info" ${config.logLevel === 'info' ? 'selected' : ''}>${tHtml('config.option.log_level_info')}</option>
                            <option value="warn" ${config.logLevel === 'warn' ? 'selected' : ''}>${tHtml('config.option.log_level_warn')}</option>
                            <option value="error" ${config.logLevel === 'error' ? 'selected' : ''}>${tHtml('config.option.log_level_error')}</option>
                        </select>
                        <p class="help-text">${tHtml('config.help.log_level')}</p>
                    </div>

                    <div class="form-group">
                        <label>${tHtml('config.label.ui_language')}</label>
                        <select id="language" class="form-control" onchange="app.handleLanguageChange(this.value)">
                            <option value="zh-TW" ${uiLocale === 'zh-TW' ? 'selected' : ''}>繁體中文</option>
                            <option value="zh-CN" ${uiLocale === 'zh-CN' ? 'selected' : ''}>简体中文</option>
                            <option value="en-US" ${uiLocale === 'en-US' ? 'selected' : ''}>English</option>
                            <option value="ja-JP" ${uiLocale === 'ja-JP' ? 'selected' : ''}>日本語</option>
                        </select>
                        <p class="help-text">${tHtml('config.help.ui_language')}</p>
                    </div>
                </div>
            </div>

            <div class="mt-4 flex gap-2">
                <button class="btn btn-primary" onclick="app.saveConfig()">
                    <span class="icon">💾</span> ${tHtml('config.button.save')}
                </button>
                <button class="btn btn-secondary" onclick="app.resetConfig()">
                    <span class="icon">🔄</span> ${tHtml('config.button.reset')}
                </button>
                <button class="btn btn-info" onclick="app.importConfig()">
                    <span class="icon">📥</span> ${tHtml('config.button.import')}
                </button>
            </div>
        `;

        this.showPanel();
    }

    // 語言下拉 onchange handler。立刻切換 backend i18n locale + 重抓字典,
    // onLocaleChange listener 會自動重 render 當前 panel(typically ConfigPanel)。
    // 不寫入 config.json — 使用者按「儲存設定」才持久化。
    async handleLanguageChange(locale) {
        try {
            await changeLanguage(locale);
        } catch (err) {
            console.error('[i18n] changeLanguage failed:', err);
            ShowError(t('dialog.error'), t('error.msg.language_switch_failed', String(err)));
        }
    }

    // 用 i18n 字典更新 index.html 內寫死的中文字串:document.title、header
    // h1/subtitle、9 個 menu button 的 title + description。
    // 在 init() 與 onLocaleChange 內呼叫。statusText 不在此處理 — 它由
    // updateStatus() 動態寫入,locale 變更後保留當前狀態(Task 15 將進一步處理)。
    updateStaticTexts() {
        document.title = t('header.app_title');

        // header h1 包含 versionBadge <span>,要保留 — 重設 textContent 後 re-append。
        const h1 = document.querySelector('header h1');
        if (h1) {
            const versionBadge = h1.querySelector('#versionBadge');
            h1.textContent = t('header.app_title') + ' ';
            if (versionBadge) {
                h1.appendChild(versionBadge);
            }
        }

        const subtitle = document.querySelector('header .subtitle');
        if (subtitle) {
            subtitle.textContent = t('header.subtitle');
        }

        // 9 個 menu button.
        const menuMappings = [
            { action: 'maxMean', titleKey: 'menu.button.maxmean.title', descKey: 'menu.button.maxmean.description' },
            { action: 'normalize', titleKey: 'menu.button.normalize.title', descKey: 'menu.button.normalize.description' },
            { action: 'chart', titleKey: 'menu.button.chart.title', descKey: 'menu.button.chart.description' },
            { action: 'phase', titleKey: 'menu.button.phase.title', descKey: 'menu.button.phase.description' },
            { action: 'phaseSync', titleKey: 'menu.button.phasesync.title', descKey: 'menu.button.phasesync.description' },
            { action: 'cci', titleKey: 'menu.button.cci.title', descKey: 'menu.button.cci.description' },
            { action: 'normalizedPhaseSync', titleKey: 'menu.button.normalizedphasesync.title', descKey: 'menu.button.normalizedphasesync.description' },
            { action: 'muscleRatio', titleKey: 'menu.button.muscleratio.title', descKey: 'menu.button.muscleratio.description' },
            { action: 'config', titleKey: 'menu.button.config.title', descKey: 'menu.button.config.description' },
        ];
        menuMappings.forEach(({ action, titleKey, descKey }) => {
            const btn = document.querySelector(`[data-action="${action}"]`);
            if (!btn) {
                return;
            }
            const titleSpan = btn.querySelector('.title');
            const descSpan = btn.querySelector('.description');
            if (titleSpan) {
                titleSpan.textContent = t(titleKey);
            }
            if (descSpan) {
                descSpan.textContent = t(descKey);
            }
        });
    }

    // 抓 functionPanel 內所有有 id 的 input/select/textarea 當前值,給
    // onLocaleChange re-render 後 restoreFormValues 使用,避免使用者輸入
    // 被 panel.innerHTML 重設清掉。skip 'language' 下拉以免蓋掉剛切換的 locale。
    snapshotFormValues() {
        const panel = document.getElementById('functionPanel');
        if (!panel) {
            return new Map();
        }
        const snapshot = new Map();
        panel.querySelectorAll('input, select, textarea').forEach((el) => {
            if (!el.id || el.id === 'language') {
                return;
            }
            if (el.type === 'checkbox' || el.type === 'radio') {
                snapshot.set(el.id, { kind: 'checked', value: el.checked });
            } else if (el.type === 'file') {
                // file input 無法以 JS 設值,略過
            } else {
                snapshot.set(el.id, { kind: 'value', value: el.value });
            }
        });
        return snapshot;
    }

    // 把 snapshot 寫回對應 id 的元素。re-render 後元素可能 id 不存在(panel
    // 結構變更等)時 safely 略過。配對 snapshotFormValues 使用。
    restoreFormValues(snapshot) {
        if (!snapshot || snapshot.size === 0) {
            return;
        }
        snapshot.forEach((entry, id) => {
            const el = document.getElementById(id);
            if (!el) {
                return;
            }
            if (entry.kind === 'checked') {
                el.checked = entry.value;
            } else {
                el.value = entry.value;
            }
        });
    }

    // 檔案選擇功能
    async selectInputFile() {
        try {
            const file = await SelectFile('選擇資料檔案', [
                {displayName: 'CSV 檔案', pattern: '*.csv'}
            ], "input");
            if (file) {
                document.getElementById('inputFile').value = file;
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
        }
    }

    async selectInputFolder() {
        try {
            const folder = await SelectDirectory('選擇資料夾');
            if (folder) {
                document.getElementById('inputFolder').value = folder;
            }
        } catch (err) {
            console.error('選擇資料夾失敗:', err);
        }
    }

    async selectMainFile() {
        try {
            const file = await SelectFile('選擇主要資料檔案', [
                {displayName: 'CSV 檔案', pattern: '*.csv'}
            ], "input");
            if (file) {
                document.getElementById('mainFile').value = file;
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
        }
    }

    async selectReferenceFile() {
        try {
            const file = await SelectFile('選擇參考資料檔案', [
                {displayName: 'CSV 檔案', pattern: '*.csv'}
            ], "operate");
            if (file) {
                document.getElementById('referenceFile').value = file;
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
        }
    }

    async selectPhaseFile() {
        try {
            const file = await SelectFile('選擇資料檔案', [
                {displayName: 'CSV 檔案', pattern: '*.csv'}
            ], "operate");
            if (file) {
                document.getElementById('phaseFile').value = file;
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
        }
    }

    // 功能執行
    async calculateMaxMean() {
        const mode = document.getElementById('processMode').value;
        const windowSize = parseInt(document.getElementById('windowSize').value);
        const startTime = parseFloat(document.getElementById('startTime').value) || 0;
        const endTime = parseFloat(document.getElementById('endTime').value) || 0;

        let inputPath;
        if (mode === 'single') {
            inputPath = document.getElementById('inputFile').value;
            if (!inputPath) {
                await ShowError(t('dialog.error'), t('error.msg.select_input_file'));
                return;
            }
        } else {
            inputPath = document.getElementById('inputFolder').value;
            if (!inputPath) {
                await ShowError(t('dialog.error'), t('error.msg.select_input_folder'));
                return;
            }
        }

        try {
            this.updateStatus(t('status.calculation_running'));
            const result = await CalculateMaxMean({
                inputPath: inputPath,
                windowSize: windowSize,
                startTime: startTime,
                endTime: endTime,
                isBatch: mode === 'batch'
            });

            this.updateStatus(t('status.calculation_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.calculation_done', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.calculation_failed'));
            await ShowError(t('dialog.error'), t('error.msg.calculation_failed', err));
        }
    }

    async normalizeData() {
        const mainFile = document.getElementById('mainFile').value;
        const referenceFile = document.getElementById('referenceFile').value;
        const outputName = document.getElementById('outputName').value;

        if (!mainFile || !referenceFile) {
            await ShowError(t('dialog.error'), t('error.msg.select_both_files'));
            return;
        }

        try {
            this.updateStatus(t('status.normalization_running'));
            const result = await NormalizeData({
                mainFile: mainFile,
                referenceFile: referenceFile,
                outputPath: outputName
            });

            this.updateStatus(t('status.normalization_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.normalization_done', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.normalization_failed'));
            await ShowError(t('dialog.error'), t('error.msg.normalization_failed', err));
        }
    }

    async analyzePhases() {
        const inputFile = document.getElementById('phaseFile').value;
        const phasePoints = document.getElementById('phasePoints').value;
        const phaseLabels = document.getElementById('phaseLabels').value;

        if (!inputFile || !phasePoints) {
            await ShowError(t('dialog.error'), t('error.msg.phase_inputs'));
            return;
        }

        // 解析時間點和標籤
        const points = phasePoints.split(',').map(p => parseFloat(p.trim()));
        const labels = phaseLabels.split('\n').filter(l => l.trim());

        if (points.length !== labels.length + 1) {
            await ShowError(t('dialog.error'), t('error.msg.phase_points_count'));
            return;
        }

        // 構建階段數據
        const phases = [];
        for (let i = 0; i < labels.length; i++) {
            phases.push({
                name: labels[i].trim(),
                startTime: points[i],
                endTime: points[i + 1]
            });
        }

        try {
            this.updateStatus(t('status.phase_analysis_running'));
            const result = await AnalyzePhases({
                inputFile: inputFile,
                phases: phases
            });

            this.updateStatus(t('status.phase_analysis_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.phase_analysis_done', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.phase_analysis_failed'));
            await ShowError(t('dialog.error'), t('error.msg.phase_analysis_failed', err));
        }
    }

    // 配置管理
    async selectInputDir() {
        try {
            const dir = await SelectDirectory('選擇預設輸入目錄');
            if (dir) {
                document.getElementById('inputDir').value = dir;
            }
        } catch (err) {
            console.error('選擇目錄失敗:', err);
        }
    }

    async selectOutputDir() {
        try {
            const dir = await SelectDirectory('選擇預設輸出目錄');
            if (dir) {
                document.getElementById('outputDir').value = dir;
            }
        } catch (err) {
            console.error('選擇目錄失敗:', err);
        }
    }

    async selectOperateDir() {
        try {
            const dir = await SelectDirectory('選擇參考資料目錄');
            if (dir) {
                document.getElementById('operateDir').value = dir;
            }
        } catch (err) {
            console.error('選擇目錄失敗:', err);
        }
    }

    async saveConfig() {
        const config = {
            scalingFactor: parseInt(document.getElementById('scalingFactor').value),
            precision: parseInt(document.getElementById('precision').value),
            outputFormat: document.getElementById('outputFormat').value,
            bomEnabled: document.getElementById('bomEnabled').checked,
            inputDir: document.getElementById('inputDir').value,
            outputDir: document.getElementById('outputDir').value,
            operateDir: document.getElementById('operateDir').value,
            phaseLabels: document.getElementById('phaseLabels').value.split('\n').filter(label => label.trim()),
            logLevel: document.getElementById('logLevel').value,
            language: document.getElementById('language').value,
            // 保留其他必要的配置
            logFormat: this.config.logFormat || 'text',
            logDirectory: this.config.logDirectory || './logs',
            translationsDir: this.config.translationsDir || './translations'
        };

        try {
            await SaveConfig(config);
            this.config = config;
            await ShowMessage(t('dialog.success'), t('success.msg.config_saved'));
        } catch (err) {
            await ShowError(t('dialog.error'), t('error.msg.config_save_failed', err));
        }
    }

    async resetConfig() {
        try {
            const config = await ResetConfig();
            this.config = config;
            // 同步前端 i18n locale 到新 cfg.language — 否則 dropdown 仍會顯示
            // 預覽態的舊 locale,使用者下次按「儲存設定」會把 reset 的 language
            // 改回舊值(codex review P2 fix)。changeLanguage 會 trigger
            // onLocaleChange listener 自動重 render panel,所以不必額外呼 showConfigPanel。
            // 設 _suppressFormRestore 讓 listener 略過 snapshot+restore — 否則 reset 後
            // 舊 form 值會蓋回剛載入的 default config(codex review P2 fix #2)。
            if (config.language && config.language !== getCurrentLocale()) {
                this._suppressFormRestore = true;
                await changeLanguage(config.language);
            } else {
                await this.showConfigPanel();
            }
            await ShowMessage(t('dialog.success'), t('success.msg.config_reset'));
        } catch (err) {
            await ShowError(t('dialog.error'), t('error.msg.config_reset_failed', err));
        }
    }

    // 匯入配置
    async importConfig() {
        try {
            const input = document.createElement('input');
            input.type = 'file';
            input.accept = '.json';

            input.onchange = async (e) => {
                const file = e.target.files[0];
                if (!file) return;

                try {
                    const text = await file.text();
                    const config = JSON.parse(text);

                    // 驗證配置結構
                    if (!config.scalingFactor || !config.inputDir || !config.outputDir) {
                        throw new Error(t('error.msg.invalid_config_format'));
                    }

                    await SaveConfig(config);
                    this.config = config;
                    // 同步前端 i18n locale 到 imported cfg.language(同 resetConfig 邏輯 —
                    // codex review P2 fix)。changeLanguage 自動 re-render panel。
                    // 同樣設 _suppressFormRestore 避免舊 form 值蓋回 imported config
                    // (codex review P2 fix #2)。
                    if (config.language && config.language !== getCurrentLocale()) {
                        this._suppressFormRestore = true;
                        await changeLanguage(config.language);
                    } else {
                        await this.showConfigPanel();
                    }
                    await ShowMessage(t('dialog.success'), t('success.msg.config_imported'));
                } catch (err) {
                    await ShowError(t('dialog.error'), t('error.msg.config_import_failed', err.message));
                }
            };

            input.click();
        } catch (err) {
            await ShowError(t('dialog.error'), t('error.msg.file_picker_failed', err));
        }
    }

    // UI 輔助功能
    showPanel() {
        document.getElementById('mainMenu').classList.add('hidden');
        document.getElementById('functionPanel').classList.remove('hidden');
    }

    showMainMenu() {
        document.getElementById('functionPanel').classList.add('hidden');
        document.getElementById('mainMenu').classList.remove('hidden');
        // 回主選單後沒有任何 panel 在 view,清掉 currentPanel 避免 locale 變更時
        // 誤觸 re-render(主選單本身的字串切換留待 Phase 2 catalog 化處理)。
        this.currentPanel = null;
        this.updateStatus(t('status.ready'));
    }

    toggleProcessMode() {
        const mode = document.getElementById('processMode').value;
        if (mode === 'single') {
            document.getElementById('singleFileSection').classList.remove('hidden');
            document.getElementById('batchFolderSection').classList.add('hidden');
        } else {
            document.getElementById('singleFileSection').classList.add('hidden');
            document.getElementById('batchFolderSection').classList.remove('hidden');
        }
    }

    updateStatus(message) {
        document.getElementById('statusText').textContent = message;
    }

    // ManifestPanel delegators(ADR-0007 §M5 wiring):shell 的
    // onclick="app.selectMpManifest()" / "app.selectMpDataFolder()" /
    // onchange="app.onMpSubjectChange()" 走這三個 thin forwarder 到
    // this._manifestPanel.*,維持既有 `app.xxx()` template convention 不動 shell。
    selectMpManifest() { return this._manifestPanel.selectMpManifest(); }
    selectMpDataFolder() { return this._manifestPanel.selectMpDataFolder(); }
    onMpSubjectChange() { return this._manifestPanel.onMpSubjectChange(); }

    // 載入可用的分期點
    async loadAvailablePhases() {
        try {
            const phases = await GetAvailablePhases();
            
            // 填充開始分期點
            const startSelect = document.getElementById('phaseSyncStartPhase');
            startSelect.innerHTML = `<option value="">${tHtml('form.option.select')}</option>`;
            phases.start.forEach(phase => {
                const option = document.createElement('option');
                option.value = phase;
                option.textContent = phase;
                startSelect.appendChild(option);
            });
            
            // 填充結束分期點
            const endSelect = document.getElementById('phaseSyncEndPhase');
            endSelect.innerHTML = `<option value="">${tHtml('form.option.select')}</option>`;
            phases.end.forEach(phase => {
                const option = document.createElement('option');
                option.value = phase;
                option.textContent = phase;
                endSelect.appendChild(option);
            });
        } catch (err) {
            console.error('載入分期點失敗:', err);
        }
    }

    // 更新分期點位置顯示
    _updatePhasePositionDisplay(recalcPercents) {
        const container = document.getElementById('cciPhasePositions');
        if (!container || !this._cciResult) return;

        const phaseOrder = ['P0','P1','P2','S','C','D','T0','T','O','L'];
        const available = phaseOrder.filter(p =>
            this._cciResult.phaseTimes && this._cciResult.phaseTimes[p] !== undefined
        );

        const lines = available.map(p => {
            const pct = recalcPercents[p];
            const display = pct !== undefined ? pct.toFixed(1) + '%' : 'Null';
            return '  ' + p + ': ' + display;
        });

        const pre = document.createElement('pre');
        pre.className = 'result-pre';
        pre.style.margin = '0.25rem 0 0 0';
        pre.textContent = lines.join('\n');

        container.textContent = '';
        const wrapper = document.createElement('div');
        wrapper.className = 'result-info';
        wrapper.style.marginTop = '0.5rem';
        const label = document.createElement('p');
        const strong = document.createElement('strong');
        strong.textContent = t('result.label.phase_positions');
        label.appendChild(strong);
        wrapper.appendChild(label);
        wrapper.appendChild(pre);
        container.appendChild(wrapper);
    }

    // 下載 CCI 圖表為 PNG (ADR-0003 latent bomb #2 拆除):
    // 改走 bridge.requestReply,iframe 端 myChart.getDataURL → 回 reply。
    // 不再 cross-frame 讀 iframe.contentWindow.echarts(在 wails dev opaque-origin
    // sandbox 下 silent fail)。Bridge 內建 10s timeout + requestId 配對,
    // caller 不再寫 30+ 行 readiness/instance/getDataURL boilerplate。
    async downloadCCIChart() {
        const iframe = document.querySelector('#cciChartContent iframe');
        if (!iframe) {
            await ShowError(t('dialog.error'), t('error.msg.cci_chart_not_found'));
            return;
        }

        try {
            this.updateStatus(t('status.cci_chart_downloading'));
            // codex review #1 P2:iframe customJS message listener 在 iframe 完整 load
            // (含 echarts.min.js external script)後才註冊。在 ready 前送 requestReply
            // 會被 drop → 10s timeout。先 await _cciIframeReady promise(若不存在
            // — 例如 showCCIResult 還沒被 call — 直接走 requestReply 走 timeout 路徑)。
            if (this._cciIframeReady) {
                await this._cciIframeReady;
            }
            const reply = await bridge.requestReply(iframe, 'cci-request-png', {});
            const result = await DownloadCCIChart({
                imageData: reply.dataURL,
                subject: this._cciResult.subject,
            });
            this.updateStatus(t('status.chart_download_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.chart_downloaded', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.chart_download_failed'));
            await ShowError(t('dialog.error'), t('error.msg.chart_download_failed', err.message || err));
        }
    }

    // 切 subject 時清掉前一個 subject 的殘留(ADR-0013):清 chart container +
    // 清勾選分期 Set。一鍵生成流程無 channel checkbox / EMGMotionOffset state,
    // 故不再需要 reset offset / loadedSubject guard — Generate 自己從 manifest row
    // 讀 offset、channel 預設全選。
    //
    // 由 chart_composer_spec.mjs 的 onSubjectChange hook 經 ManifestPanel
    // onMpSubjectChange 委派呼叫。結果區/圖表容器走 ManifestPanel shell 的
    // #mpResult;#composerChartContent 由 spec.onResult render 進 #mpResultContent。
    onComposerSubjectChange() {
        const resultDiv = document.getElementById('mpResult');
        if (resultDiv) {
            resultDiv.style.display = 'none';
            const wrap = document.getElementById('composerChartContent');
            if (wrap) wrap.textContent = '';
        }
        // 清掉舊 subject 的勾選分期,避免殘留到新 subject(下次 generate 後重新 render)。
        if (this._composerCheckedPhases) this._composerCheckedPhases.clear();
        // D4:切 subject 後到下次生成前,標準化視圖按鈕回灰(視覺一致;下次 onResult
        // 的 bindPhaseCheckboxes onUpdate 會依勾選數重新 enable)。
        const stdBtn = document.getElementById('composerStandardizeBtn');
        if (stdBtn) stdBtn.disabled = true;
    }

    // 下載 PNG — 從 iframe 抓 ECharts dataURL(反映當下 zoom / phase / legend 狀態)
    // → 拼 outputDir + 自動檔名 → 呼叫 DownloadChartComposerImage。
    //
    // codex P2 #4 修補:不走 `SelectFile('save', ...)`。Wails `SelectFile` 是
    // `runtime.OpenFileDialog` 包裝(見 gui/app.go:316),`buttonType` switch case
    // 只認 'input' / 'output' / 'operate' 設預設目錄;'save' 不在 switch case 內
    // 會 fall through,**實際打開的仍然是 OpenFileDialog**(請選擇現有檔案)。
    // user cancel 後舊版 fallback 寫 `<subject>.png` 到 app cwd — 是 silent 寫到
    // 不可預期目錄(macOS 是 app bundle 內、Windows 是 install dir),屬危險。
    //
    // 鏡像 `downloadCCIChart` baseline(gui/cci_handlers.go:159):走 config.outputDir
    // + 自動拼檔名,不問 user。Composer backend `DownloadChartComposerImage`
    // (gui/chart_composer_handlers.go:451)已接受 outputPath,前端只負責拼好 PATH。
    //
    // 檔名 pattern 沿用 CCI 風格(`<subject>_<suffix>.png`),Composer suffix 是
    // 'chart_composer';backend 已 SanitizeFileName(filepath.Base(outputPath))
    // 二次防 traversal,再加 validateExternalPathInputs 防 sensitive dir。
    async downloadComposerChart() {
        const iframe = document.querySelector('#composerChartContent iframe');
        if (!iframe) {
            await ShowError(t('dialog.error'), '找不到圖表 iframe');
            return;
        }
        try {
            // 先驗 outputDir 存在 — 沒設定的話直接 ShowError 而非 silent 寫到 cwd。
            // GetConfig 在 init() 時就存到 this.config,但 user 可能進過設定 panel 改;
            // 重新呼叫 GetConfig 拿最新值。
            const cfg = await GetConfig();
            const outputDir = (cfg && cfg.outputDir) ? String(cfg.outputDir).trim() : '';
            if (!outputDir) {
                await ShowError(
                    t('dialog.error'),
                    '輸出目錄未設定 — 請先到「設定」設定輸出資料夾'
                );
                return;
            }

            // codex review #1 P2:iframe customJS message listener 在完整 load 後
            // (含 external echarts.min.js)才註冊;在 ready 前送 requestReply 會
            // 被 drop → 10s timeout。先 await _composerIframeReady promise。
            if (this._composerIframeReady) {
                await this._composerIframeReady;
            }
            // ADR-0003:走 bridge.requestReply 拿當下 chart dataURL。
            // 內部包 requestId 配對 + 預設 10s timeout + opaque-origin '*' 退路,
            // caller 不再寫 14 行 promise/listener boilerplate。
            const reply = await bridge.requestReply(iframe, 'composer-request-png', {});
            const dataURL = reply.dataURL;

            // 拼 outputDir + `<subject>_chart_composer.png`(對齊 CCI 用 `_CCI_Rudolph.png`)。
            // backend 會再 SanitizeFileName(filepath.Base(...)) 做最終 sanitize +
            // path validation(prefix '/' 視 OS 而定,前端只負責 join,不裸接受 user input)。
            // M5:#composerSubject → 共用 #mpSubject(name-mode value=subject 字串)。
            const subject = document.getElementById('mpSubject').value || 'chart_composer';
            const sep = outputDir.endsWith('/') || outputDir.endsWith('\\') ? '' : '/';
            const outputPath = outputDir + sep + subject + '_chart_composer.png';

            const result = await DownloadChartComposerImage({
                base64Data: dataURL,
                outputPath,
            });
            if (!result.success) {
                await ShowError(t('dialog.error'), result.message);
                return;
            }
            await ShowMessage(t('dialog.success'), result.message);
        } catch (err) {
            console.error('下載 PNG 失敗:', err);
            await ShowError(t('dialog.error'), err.message || String(err));
        }
    }

    // 標準化視圖(ADR-0013 D4/D5):把 chart zoom 到「當下勾選分期點的 min/max 秒」
    // 兩側各留 5% buffer 的區間。由結果區「標準化視圖」按鈕(chart_composer_spec
    // extraRunButtons 注入)的 onclick 觸發。
    //
    // 讀勾選分期 Set(_composerCheckedPhases:phase 名)+ phase 秒數 map
    // (_composerPhaseTimes:phase 名→秒),取勾選 phase 對應秒數;< 2 個無法定義
    // 區間 → no-op return(按鈕本就灰,雙重保險)。算出 [t_first - buf, t_last + buf]
    // 後走 bridge 送 composer-standardize-zoom 給 iframe(iframe 端 customJS 把
    // 秒值換算成 dataZoom 百分比 start/end)。
    standardizeComposerView() {
        const checked = this._composerCheckedPhases;
        const times = this._composerPhaseTimes;
        if (!checked || !times) return;

        // 勾選 phase 對應的秒數(過濾掉 times 內不存在的,防殘留)。
        const secs = Array.from(checked)
            .map((name) => times[name])
            .filter((s) => typeof s === 'number');
        if (secs.length < 2) return; // < 2 → no-op(按鈕本就 disabled)

        const tFirst = Math.min(...secs);
        const tLast = Math.max(...secs);
        const buf = (tLast - tFirst) * 0.05;

        const iframe = this._composerIframe;
        if (!iframe) return;
        bridge.send(iframe, 'composer-standardize-zoom', {
            startSec: tFirst - buf,
            endSec: tLast + buf,
        });
    }

    async loadNormalizedPhaseSyncPhases() {
        try {
            const phases = await GetAvailablePhases();
            const populate = (selectEl, list) => {
                selectEl.replaceChildren();
                const placeholder = document.createElement('option');
                placeholder.value = '';
                placeholder.textContent = t('form.option.select');
                selectEl.appendChild(placeholder);
                list.forEach(phase => {
                    const option = document.createElement('option');
                    option.value = phase;
                    option.textContent = phase;
                    selectEl.appendChild(option);
                });
            };
            const fills = [
                ['normalizedPhaseSyncNormStartPhase', phases.start],
                ['normalizedPhaseSyncNormEndPhase', phases.end],
                ['normalizedPhaseSyncStatsStartPhase', phases.start],
                ['normalizedPhaseSyncStatsEndPhase', phases.end],
            ];
            fills.forEach(([id, list]) => populate(document.getElementById(id), list));

            this._bindNormalizedPhaseSyncMirror();
        } catch (err) {
            console.error('載入分期點失敗:', err);
        }
    }

    // _bindNormalizedPhaseSyncMirror 綁定第四點 → 第五點的「同步直到使用者觸碰」邏輯。
    // 第五點 select 的 data-touched 屬性初始為 "false"，使用者一旦動過任一第五點
    // select，該 select 的 data-touched 變 "true"，之後第四點變動不再 mirror 過去。
    // 目的：常見情境（兩組區間相同）零學習成本，分歧情境（兩組區間不同）使用者可
    // 自由獨立設定。
    _bindNormalizedPhaseSyncMirror() {
        const normStart = document.getElementById('normalizedPhaseSyncNormStartPhase');
        const normEnd = document.getElementById('normalizedPhaseSyncNormEndPhase');
        const statsStart = document.getElementById('normalizedPhaseSyncStatsStartPhase');
        const statsEnd = document.getElementById('normalizedPhaseSyncStatsEndPhase');

        const mirror = (src, dst) => () => {
            if (dst.dataset.touched === 'false') {
                dst.value = src.value;
            }
        };
        normStart.addEventListener('change', mirror(normStart, statsStart));
        normEnd.addEventListener('change', mirror(normEnd, statsEnd));

        const markTouched = el => () => { el.dataset.touched = 'true'; };
        statsStart.addEventListener('change', markTouched(statsStart));
        statsEnd.addEventListener('change', markTouched(statsEnd));
    }

    // 開啟輸出資料夾
    async openOutputFolder() {
        try {
            const config = await GetConfig();
            if (config && config.outputDir) {
                // 使用系統預設程式開啟資料夾
                // 注意：這裡需要通過後端 API 來執行
                await ShowMessage(t('dialog.title.hint'), t('info.msg.output_folder', config.outputDir));
            }
        } catch (err) {
            console.error('無法開啟輸出資料夾:', err);
            await ShowError(t('dialog.error'), t('error.msg.open_output_folder'));
        } 
    }
}

// 創建全局應用實例
window.app = new EMGAnalysisApp();