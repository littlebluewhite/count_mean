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
    GenerateChart,
    AnalyzePhases,
    GetCSVHeaders,
    GenerateInteractiveChart,
    LoadPhaseManifest,
    GetAvailablePhases,
    AnalyzePhaseSync,
    AnalyzeNormalizedPhaseSync,
    AnalyzeCCI,
    DownloadCCIChart,
    AnalyzeMuscleRatio,
    GetVersion
} from '../wailsjs/go/gui/App.js';
import { OnFileDrop } from '../wailsjs/runtime/runtime.js';
import { initI18n, t, tHtml, changeLanguage, onLocaleChange, getCurrentLocale } from './i18n.js';

// 應用程序主類
class EMGAnalysisApp {
    constructor() {
        // currentPanel 紀錄目前在哪一頁(由 handleMenuAction 設定)。
        // i18n locale 變更時,onLocaleChange listener 透過此屬性查表
        // 重新呼叫當前 panel 的 show() 函式以觸發 re-render。
        this.currentPanel = null;
        this.panelDispatch = {
            maxMean: () => this.showMaxMeanPanel(),
            normalize: () => this.showNormalizePanel(),
            chart: () => this.showChartPanel(),
            phase: () => this.showPhasePanel(),
            phaseSync: () => this.showPhaseSyncPanel(),
            cci: () => this.showCCIPanel(),
            normalizedPhaseSync: () => this.showNormalizedPhaseSyncPanel(),
            muscleRatio: () => this.showMuscleRatioPanel(),
            config: () => this.showConfigPanel(),
        };
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

        // 綁定事件
        this.bindEvents();

        // 更新狀態
        this.updateStatus(t('status.app_ready'));
    }

    // 初始化拖曳功能
    initDragAndDrop() {
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

            // 觸發相應的處理邏輯
            if (targetId === 'chartFile') {
                this.loadChartColumns(filePath);
                const btn = document.getElementById('downloadChartBtn');
                if (btn) {
                    btn.style.display = '';
                    btn.disabled = false;
                }
            } else if (targetId === 'phaseSyncManifest') {
                // 分期同步分析：載入主題
                this.loadManifestSubjects(filePath);
            } else if (targetId === 'cciManifest') {
                // 共同收縮分析：載入主題
                this.loadCCIManifestSubjects(filePath);
            } else if (targetId === 'normalizedPhaseSyncManifest') {
                // 標準化分期同步分析：載入主題
                this.loadNormalizedPhaseSyncSubjects(filePath);
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
        this.currentPanel = action;
        switch (action) {
            case 'maxMean':
                this.showMaxMeanPanel();
                break;
            case 'normalize':
                this.showNormalizePanel();
                break;
            case 'chart':
                this.showChartPanel();
                break;
            case 'phase':
                this.showPhasePanel();
                break;
            case 'phaseSync':
                this.showPhaseSyncPanel();
                break;
            case 'cci':
                this.showCCIPanel();
                break;
            case 'normalizedPhaseSync':
                this.showNormalizedPhaseSyncPanel();
                break;
            case 'muscleRatio':
                this.showMuscleRatioPanel();
                break;
            case 'config':
                this.showConfigPanel();
                break;
        }
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

    // 資料做圖面板
    async showChartPanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.chart.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.chart_file')}</label>
                <div class="input-group drop-zone" data-drop-target="chartFile" style="--wails-drop-target: drop;">
                    <input type="text" id="chartFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectChartFile()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.chart_title')}</label>
                <input type="text" id="chartTitle" class="form-control" value="${tHtml('form.default.chart_title')}">
            </div>

            <div class="form-group">
                <label>${tHtml('form.label.select_columns')}</label>
                <div id="columnSelector" class="checkbox-group">
                    <p class="help-text">${tHtml('form.help.select_file_first')}</p>
                </div>
            </div>
            <div id="previewChartContainer" class="chart-preview hidden">
                <h3>${tHtml('chart.preview.title')}</h3>
                <div id="previewChartContent"></div>
            </div>
            <div class="mt-4">
                <button id="downloadChartBtn" class="btn btn-primary"
                    onclick="app.downloadChart()" disabled style="display:none">
                  ${tHtml('button.download_chart')}
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
    // h1/subtitle、9 個 menu button 的 title + description、chartTitle placeholder。
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

        // chartTitle 是 placeholder;chart panel 開啟後會被 user value 覆寫。
        const chartTitle = document.getElementById('chartTitle');
        if (chartTitle) {
            chartTitle.textContent = t('chart.title.placeholder');
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

    async selectChartFile() {
        try {
            const file = await SelectFile('選擇資料檔案', [
                {displayName: 'CSV 檔案', pattern: '*.csv'}
            ], "input");
            if (file) {
                document.getElementById('chartFile').value = file;
                await this.loadChartColumns(file);
                const btn = document.getElementById('downloadChartBtn');
                btn.style.display = '';
                btn.disabled = false;
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

    async generateChart() {
        const file = document.getElementById('chartFile').value;
        if (!file) {
            await ShowError(t('dialog.error'), t('error.msg.select_input_file'));
            return;
        }

        const checked = document.querySelectorAll('#columnSelector input[type="checkbox"]:checked');
        const columns = Array.from(checked).map(cb => parseInt(cb.value));
        if (columns.length === 0) {
            await ShowError(t('dialog.error'), t('error.msg.chart_select_columns'));
            return;
        }

        const title = document.getElementById('chartTitle').value || 'EMG 資料分析圖表';

        try {
            this.updateStatus(t('status.chart_generating'));

            const result = await GenerateChart({
                filePath: file,
                columns: columns,
                title: title
            });

            this.updateStatus(t('status.chart_generated'));
            await ShowMessage(t('dialog.success'), t('success.msg.chart_generated', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.chart_generation_failed'));
            await ShowError(t('dialog.error'), t('error.msg.chart_generation_failed', err));
        }
    }

    async downloadChart() {
        const iframe = document.querySelector('#previewChartContent iframe');
        if (!iframe) {
            await ShowError(t('dialog.error'), t('error.msg.chart_not_found'));
            return;
        }

        const file = document.getElementById('chartFile').value;
        if (!file) {
            await ShowError(t('dialog.error'), t('error.msg.select_input_file'));
            return;
        }

        try {
            this.updateStatus(t('status.chart_downloading'));

            // 等待 iframe 完全加載
            await new Promise(resolve => {
                if (iframe.contentDocument.readyState === 'complete') {
                    resolve();
                } else {
                    iframe.onload = resolve;
                }
            });

            // 獲取 iframe 中的 ECharts 實例
            const iframeWindow = iframe.contentWindow;
            const iframeDocument = iframe.contentDocument;

            if (!iframeWindow.echarts) {
                throw new Error(t('error.msg.echarts_not_found'));
            }

            // 尋找 ECharts 實例
            const chartElement = iframeDocument.querySelector('[_echarts_instance_]');
            if (!chartElement) {
                throw new Error(t('error.msg.chart_element_not_found'));
            }

            const chartInstance = iframeWindow.echarts.getInstanceByDom(chartElement);
            if (!chartInstance) {
                throw new Error(t('error.msg.chart_instance_not_found'));
            }

            // 獲取當前圖表的 PNG 數據
            const dataURL = chartInstance.getDataURL({
                type: 'png',
                pixelRatio: 2,
                backgroundColor: '#fff'
            });

            // 呼叫後端保存
            const result = await GenerateChart({
                filePath: file,
                title: document.getElementById('chartTitle').value || 'EMG圖表',
                imageData: dataURL
            });

            this.updateStatus(t('status.chart_download_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.chart_downloaded', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.chart_download_failed'));
            await ShowError(t('dialog.error'), t('error.msg.chart_download_failed', err.message || err));
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

    displayChart(htmlContent, title) {
        const container = document.getElementById('chartContainer');
        const content = document.getElementById('chartContent');

        document.getElementById('chartTitle').textContent = title;
        content.innerHTML = htmlContent;
        container.classList.remove('hidden');
    }

    closeChart() {
        document.getElementById('chartContainer').classList.add('hidden');
    }

    async loadChartColumns(file) {
        const selector = document.getElementById('columnSelector');
        selector.innerHTML = `<p class="help-text">${tHtml('status.loading_columns')}</p>`;
        try {
            const headers = await GetCSVHeaders({filePath: file});
            // 第一個欄位為時間，必選且禁止取消
            selector.innerHTML = headers.map((col, index) => `
                <div class="checkbox-item">
                    <input type="checkbox"
                           id="col_${index}"
                           value="${index}"
                           ${index === 0 ? 'checked disabled' : 'checked'}>
                    <label for="col_${index}">${col}</label>
                </div>
            `).join('');
            // 綁定預覽更新
            document.querySelectorAll('#columnSelector input[type="checkbox"]').forEach(cb => {
                cb.addEventListener('change', () => this.previewInteractiveChart());
            });
            // 初次顯示預覽
            this.previewInteractiveChart();
        } catch (err) {
            console.error('載入欄位失敗:', err);
            selector.innerHTML = `<p class="help-text text-danger">${tHtml('status.load_columns_failed')}</p>`;
            await ShowError(t('dialog.error'), t('error.msg.load_columns_failed', err));
        }
    }

    // 生成並顯示互動式圖表預覽
    async previewInteractiveChart() {
        const file = document.getElementById('chartFile').value;
        if (!file) return;

        // 取得勾選欄位
        const checked = document.querySelectorAll('#columnSelector input[type="checkbox"]:checked');
        const columns = Array.from(checked).map(cb => parseInt(cb.value));
        if (columns.length === 0) {
            document.getElementById('previewChartContainer').classList.add('hidden');
            return;
        }

        try {
            // 從後端取回完整 HTML
            const html = await GenerateInteractiveChart({
                filePath: file,
                columns,
                title: document.getElementById('chartTitle').value || '',
                width: '900px',
                height: '500px'
            });

            const wrapper = document.getElementById('previewChartContent');
            wrapper.innerHTML = '';          // 清掉上一張圖

            // 建立 iframe 讓 <script> 正常執行
            const iframe = document.createElement('iframe');
            iframe.style.width = '100%';
            iframe.style.height = '520px';
            iframe.style.border = 'none';
            iframe.srcdoc = html;            // 直接塞 srcdoc

            wrapper.appendChild(iframe);
            document.getElementById('previewChartContainer').classList.remove('hidden');
        } catch (err) {
            console.error('預覽生成失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.chart_preview_failed', err));
        }
    }
    // 分期同步分析面板
    showPhaseSyncPanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.phasesync.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>1. ${tHtml('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="phaseSyncManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="phaseSyncManifest" class="form-control" placeholder="${tHtml('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseSyncManifest()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. ${tHtml('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="phaseSyncDataFolder" class="form-control" placeholder="${tHtml('form.placeholder.data_folder')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseSyncDataFolder()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>3. ${tHtml('form.label.subject')}</label>
                <select id="phaseSyncSubject" class="form-control" disabled>
                    <option value="">${tHtml('form.placeholder.load_manifest_first')}</option>
                </select>
            </div>

            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>4. ${tHtml('form.label.start_phase')}</label>
                    <select id="phaseSyncStartPhase" class="form-control">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${tHtml('form.label.end_phase')}</label>
                    <select id="phaseSyncEndPhase" class="form-control">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executePhaseSyncAnalysis()">${tHtml('button.start_analyze')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div id="phaseSyncResult" class="result-section" style="display: none;">
                <h3>${tHtml('result.section.title')}</h3>
                <div id="phaseSyncResultContent"></div>
            </div>
        `;
        
        this.showPanel(panel);
        this.loadAvailablePhases();
    }
    
    // 選擇分期總檔案
    async selectPhaseSyncManifest() {
        try {
            const filters = [{
                displayName: 'CSV Files (*.csv)',
                pattern: '*.csv'
            }];
            const file = await SelectFile('選擇分期總檔案', filters, 'open');
            
            if (file) {
                document.getElementById('phaseSyncManifest').value = file;
                await this.loadManifestSubjects(file);
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }
    
    // 載入分期總檔案的主題
    async loadManifestSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('phaseSyncSubject');
            
            select.innerHTML = `<option value="">${tHtml('form.option.select_subject')}</option>`;
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });
            
            select.disabled = false;
            this.updateStatus(t('status.subjects_loaded', subjects.length));
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.load_subjects_failed', err));
        }
    }
    
    // 選擇數據資料夾
    async selectPhaseSyncDataFolder() {
        try {
            const folder = await SelectDirectory('選擇數據資料夾');
            if (folder) {
                document.getElementById('phaseSyncDataFolder').value = folder;
            }
        } catch (err) {
            console.error('選擇資料夾失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }
    
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
    
    // 執行分期同步分析
    async executePhaseSyncAnalysis() {
        try {
            const manifestFile = document.getElementById('phaseSyncManifest').value;
            const dataFolder = document.getElementById('phaseSyncDataFolder').value;
            const subjectIndex = parseInt(document.getElementById('phaseSyncSubject').value);
            const startPhase = document.getElementById('phaseSyncStartPhase').value;
            const endPhase = document.getElementById('phaseSyncEndPhase').value;
            
            // 驗證輸入
            if (!manifestFile || !dataFolder || isNaN(subjectIndex) || !startPhase || !endPhase) {
                await ShowError(t('dialog.error'), t('error.msg.fill_required_fields'));
                return;
            }
            
            this.updateStatus(t('status.phasesync_running'));
            
            const result = await AnalyzePhaseSync({
                manifestFile,
                dataFolder,
                subjectIndex,
                startPhase,
                endPhase
            });
            
            if (result.success) {
                await this.showPhaseSyncResult(result);
                await ShowMessage(t('dialog.success'), t('success.msg.analysis_done', result.outputPath));
            } else {
                await ShowError(t('dialog.error'), result.message);
            }
            
            this.updateStatus(t('status.analysis_done'));
        } catch (err) {
            console.error('分期同步分析失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.analysis_failed_dynamic', err));
            this.updateStatus(t('status.analysis_failed'));
        }
    }
    
    // 顯示分期同步分析結果
    async showPhaseSyncResult(result) {
        const resultDiv = document.getElementById('phaseSyncResult');
        const contentDiv = document.getElementById('phaseSyncResultContent');
        
        let html = `
            <div class="result-info">
                <p><strong>主題：</strong>${result.subject}</p>
                <p><strong>分析區間：</strong>${result.startPhase} (${result.startTime.toFixed(3)}s) → ${result.endPhase} (${result.endTime.toFixed(3)}s)</p>
                <p><strong>輸出檔案：</strong>${result.outputPath}</p>
            </div>
            
            <div class="result-report">
                <h4>分析報告</h4>
                <pre class="result-pre">${result.report}</pre>
            </div>
            
            <div class="button-group">
                <button class="btn btn-primary" onclick="app.openOutputFolder()">開啟輸出資料夾</button>
            </div>
        `;
        
        contentDiv.innerHTML = html;
        resultDiv.style.display = 'block';
    }
    
    // ==================== 共同收縮分析 (CCI Rudolph) ====================

    // 共同收縮分析面板
    showCCIPanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.cci.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div class="form-group">
                <label>1. ${tHtml('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="cciManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="cciManifest" class="form-control" placeholder="${tHtml('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectCCIManifest()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. ${tHtml('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="cciDataFolder" class="form-control" placeholder="${tHtml('form.placeholder.data_folder_emg')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectCCIDataFolder()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>3. ${tHtml('form.label.subject')}</label>
                <select id="cciSubject" class="form-control" disabled>
                    <option value="">${tHtml('form.placeholder.load_manifest_first')}</option>
                </select>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executeCCIAnalysis()">${tHtml('button.start_analyze')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div id="cciResult" class="result-section" style="display: none;">
                <h3>${tHtml('result.section.title')}</h3>
                <div id="cciResultContent"></div>
            </div>
        `;

        this.showPanel(panel);
    }

    // 選擇 CCI 分期總檔案
    async selectCCIManifest() {
        try {
            const filters = [{
                displayName: 'CSV Files (*.csv)',
                pattern: '*.csv'
            }];
            const file = await SelectFile('選擇分期總檔案', filters, 'open');

            if (file) {
                document.getElementById('cciManifest').value = file;
                await this.loadCCIManifestSubjects(file);
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }

    // 載入 CCI 分期總檔案的主題
    async loadCCIManifestSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('cciSubject');

            select.innerHTML = `<option value="">${tHtml('form.option.select_subject')}</option>`;
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });

            select.disabled = false;
            this.updateStatus(t('status.subjects_loaded', subjects.length));
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.load_subjects_failed', err));
        }
    }

    // 選擇 CCI 數據資料夾
    async selectCCIDataFolder() {
        try {
            const folder = await SelectDirectory('選擇數據資料夾');
            if (folder) {
                document.getElementById('cciDataFolder').value = folder;
            }
        } catch (err) {
            console.error('選擇資料夾失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }

    // 執行 CCI 分析
    async executeCCIAnalysis() {
        try {
            const manifestFile = document.getElementById('cciManifest').value;
            const dataFolder = document.getElementById('cciDataFolder').value;
            const subjectIndex = parseInt(document.getElementById('cciSubject').value);

            if (!manifestFile || !dataFolder || isNaN(subjectIndex)) {
                await ShowError(t('dialog.error'), t('error.msg.fill_required_fields'));
                return;
            }

            this.updateStatus(t('status.cci_running'));

            const result = await AnalyzeCCI({
                manifestFile,
                dataFolder,
                subjectIndex
            });

            if (result.success) {
                this.showCCIResult(result);
                await ShowMessage(t('dialog.success'), t('success.msg.cci_analysis_done', result.outputCSVPath));
            } else {
                await ShowError(t('dialog.error'), result.message);
            }

            this.updateStatus(t('status.analysis_done'));
        } catch (err) {
            console.error('CCI 分析失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.analysis_failed_dynamic', err));
            this.updateStatus(t('status.analysis_failed'));
        }
    }

    // 顯示 CCI 分析結果
    showCCIResult(result) {
        const resultDiv = document.getElementById('cciResult');
        const contentDiv = document.getElementById('cciResultContent');

        // Build phase checkboxes
        let phaseCheckboxes = '';
        if (result.phasePercents) {
            const phaseOrder = ['P0','P1','P2','S','C','D','T0','T','O','L'];
            const available = phaseOrder.filter(p => result.phasePercents[p] !== undefined);
            phaseCheckboxes = `
                <div class="form-group" style="margin-top: 0.5rem;">
                    <label><strong>分期點顯示：</strong></label>
                    <div class="checkbox-group" style="display:flex;flex-wrap:wrap;gap:0.5rem;">
                        ${available.map(p => `
                            <div class="checkbox-item">
                                <input type="checkbox" id="phase_${p}" value="${p}" checked
                                       onchange="app.updateCCIPhaseLines()">
                                <label for="phase_${p}">${p}</label>
                            </div>
                        `).join('')}
                    </div>
                </div>
            `;
        }

        let chartSection = '';
        if (result.chartHTML) {
            chartSection = `
                <div style="margin: 1rem 0;">
                    <div id="cciChartContent"></div>
                    ${phaseCheckboxes}
                    <div id="cciPhasePositions" style="margin-top: 0.5rem;"></div>
                    <div class="button-group" style="margin-top: 0.5rem;">
                        <button class="btn btn-primary" onclick="app.downloadCCIChart()">下載圖表</button>
                    </div>
                </div>
            `;
        }

        contentDiv.innerHTML = `
            <div class="result-info">
                <p><strong>主題：</strong>${result.subject}</p>
                <p><strong>肌肉配對：</strong>${result.pairNames.join(', ')}</p>
                <p><strong>CSV 輸出：</strong>${result.outputCSVPath}</p>
            </div>

            ${chartSection}

            <div class="result-report">
                <h4>分析報告</h4>
                <pre class="result-pre">${result.report}</pre>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.openOutputFolder()">開啟輸出資料夾</button>
            </div>
        `;

        resultDiv.style.display = 'block';

        // Load interactive chart in iframe
        if (result.chartHTML) {
            const wrapper = document.getElementById('cciChartContent');
            const iframe = document.createElement('iframe');
            iframe.style.width = '100%';
            iframe.style.height = '620px';
            iframe.style.border = 'none';
            iframe.srcdoc = result.chartHTML;
            wrapper.appendChild(iframe);

            // Draw phase lines once iframe loads
            iframe.onload = () => this.updateCCIPhaseLines();
        }

        // Listen for restore/legend events from iframe to re-apply phase lines
        window.addEventListener('message', (e) => {
            if (e.data === 'cci-chart-restored' || e.data === 'cci-chart-legend-changed') {
                setTimeout(() => this.updateCCIPhaseLines(), 100);
            }
        });

        // Store for download and phase line updates
        this._cciResult = result;
        this._originalPctLabels = null;
    }

    // 更新 CCI 圖表的分期點垂直線
    updateCCIPhaseLines() {
        const iframe = document.querySelector('#cciChartContent iframe');
        if (!iframe || !iframe.contentWindow || !iframe.contentWindow.echarts) return;

        const iframeDoc = iframe.contentDocument;
        const chartEl = iframeDoc.querySelector('[_echarts_instance_]');
        if (!chartEl) return;

        const chart = iframe.contentWindow.echarts.getInstanceByDom(chartEl);
        if (!chart) return;

        const option = chart.getOption();
        if (!this._originalPctLabels && option.xAxis && option.xAxis[1] && option.xAxis[1].data) {
            this._originalPctLabels = option.xAxis[1].data.slice();
        }
        const xLabels = (option.xAxis && option.xAxis[0] && option.xAxis[0].data) || [];
        const phaseTimes = this._cciResult.phaseTimes || {};

        // Collect checked phases and their times
        const checkedPhases = [];
        const checkboxes = document.querySelectorAll('[id^="phase_"]');
        checkboxes.forEach(cb => {
            if (cb.checked && phaseTimes[cb.value] !== undefined) {
                checkedPhases.push({ name: cb.value, time: phaseTimes[cb.value] });
            }
        });

        // Recalculate percentages: min checked = 0%, max checked = 100%
        let minTime = Infinity, maxTime = -Infinity;
        for (const p of checkedPhases) {
            if (p.time < minTime) minTime = p.time;
            if (p.time > maxTime) maxTime = p.time;
        }
        const duration = maxTime - minTime;

        // Rebuild secondary X-axis percentage labels based on checked phase range
        let newPctLabels;
        const allCheckboxes = document.querySelectorAll('[id^="phase_"]');
        const allChecked = checkedPhases.length === allCheckboxes.length;
        if (allChecked && this._originalPctLabels) {
            newPctLabels = this._originalPctLabels;
        } else if (checkedPhases.length >= 2 && duration > 0 && isFinite(duration)) {
            newPctLabels = xLabels.map(label => {
                const t = parseFloat(label);
                return ((t - minTime) / duration * 100).toFixed(1) + '%';
            });
        } else if (this._originalPctLabels) {
            newPctLabels = this._originalPctLabels;
        } else {
            newPctLabels = xLabels.map(() => '0.0%');
        }

        const recalcPercents = {};
        for (const p of checkedPhases) {
            recalcPercents[p.name] = duration > 0
                ? ((p.time - minTime) / duration * 100)
                : 0;
        }

        // Build markLine data with two-line labels
        const markData = [];
        for (const p of checkedPhases) {
            const pct = recalcPercents[p.name].toFixed(1);
            const nearest = this._findNearestLabel(p.time, xLabels);
            if (nearest) {
                markData.push({
                    name: p.name + '\n(' + pct + '%)',
                    xAxis: nearest,
                    lineStyle: { type: 'dashed', color: '#888', width: 1 },
                    label: { show: true, formatter: '{b}', position: 'end' }
                });
            }
        }

        // Find first visible (legend-selected) mean curve series for markLine attachment
        if (option.series && option.series.length > 0) {
            const legendSelected = (option.legend && option.legend[0] && option.legend[0].selected) || {};
            let targetIdx = 0;
            for (let i = 0; i < option.series.length; i++) {
                const name = option.series[i].name;
                if (name.includes(' -SD') || name.includes(' +SD')) continue;
                if (legendSelected[name] !== false) {
                    targetIdx = i;
                    break;
                }
            }

            // Clear markLine from previous holder, apply to new target
            const seriesUpdate = option.series.map((s, i) => {
                if (i === targetIdx) {
                    return {
                        markLine: {
                            silent: true,
                            symbol: ['none', 'none'],
                            data: markData
                        }
                    };
                }
                if (s.markLine && s.markLine.data && s.markLine.data.length > 0) {
                    return { markLine: { data: [] } };
                }
                return {};
            });
            chart.setOption({ series: seriesUpdate, xAxis: [{}, { data: newPctLabels }] });
        }

        // Update dynamic phase position display
        this._updatePhasePositionDisplay(recalcPercents);
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

    // 找到最接近目標時間的 X 軸標籤
    _findNearestLabel(targetTime, labels) {
        if (!labels || labels.length === 0) return null;
        let nearest = labels[0];
        let minDiff = Math.abs(parseFloat(labels[0]) - targetTime);
        for (const label of labels) {
            const diff = Math.abs(parseFloat(label) - targetTime);
            if (diff < minDiff) {
                minDiff = diff;
                nearest = label;
            }
        }
        return nearest;
    }

    // 下載 CCI 圖表為 PNG
    async downloadCCIChart() {
        const iframe = document.querySelector('#cciChartContent iframe');
        if (!iframe) {
            await ShowError(t('dialog.error'), t('error.msg.cci_chart_not_found'));
            return;
        }

        try {
            this.updateStatus(t('status.cci_chart_downloading'));

            await new Promise(resolve => {
                if (iframe.contentDocument.readyState === 'complete') {
                    resolve();
                } else {
                    iframe.onload = resolve;
                }
            });

            const iframeWindow = iframe.contentWindow;
            const iframeDocument = iframe.contentDocument;

            if (!iframeWindow.echarts) {
                throw new Error(t('error.msg.echarts_not_found'));
            }

            const chartElement = iframeDocument.querySelector('[_echarts_instance_]');
            if (!chartElement) {
                throw new Error(t('error.msg.chart_element_not_found'));
            }

            const chartInstance = iframeWindow.echarts.getInstanceByDom(chartElement);
            if (!chartInstance) {
                throw new Error(t('error.msg.chart_instance_not_found'));
            }

            const dataURL = chartInstance.getDataURL({
                type: 'png',
                pixelRatio: 2,
                backgroundColor: '#fff'
            });

            const result = await DownloadCCIChart({
                imageData: dataURL,
                subject: this._cciResult.subject
            });

            this.updateStatus(t('status.chart_download_done'));
            await ShowMessage(t('dialog.success'), t('success.msg.chart_downloaded', result.outputPath));
        } catch (err) {
            this.updateStatus(t('status.chart_download_failed'));
            await ShowError(t('dialog.error'), t('error.msg.chart_download_failed', err.message || err));
        }
    }

    // ==================== 標準化分期同步分析 ====================

    showNormalizedPhaseSyncPanel() {
        const panel = document.getElementById('functionPanel');
        const html = `
            <div class="panel-header">
                <h2>${tHtml('panel.normalizedphasesync.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>
            <p class="help-text" style="margin-bottom: 1rem;">${tHtml('panel.normalizedphasesync.description')}</p>
            <div class="form-group">
                <label>1. ${tHtml('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="normalizedPhaseSyncManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="normalizedPhaseSyncManifest" class="form-control" placeholder="${tHtml('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectNormalizedPhaseSyncManifest()">${tHtml('button.browse')}</button>
                </div>
            </div>
            <div class="form-group">
                <label>2. ${tHtml('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="normalizedPhaseSyncDataFolder" class="form-control" placeholder="${tHtml('form.placeholder.data_folder_emg')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectNormalizedPhaseSyncDataFolder()">${tHtml('button.browse')}</button>
                </div>
            </div>
            <div class="form-group">
                <label>3. ${tHtml('form.label.subject')}</label>
                <select id="normalizedPhaseSyncSubject" class="form-control" disabled>
                    <option value="">${tHtml('form.placeholder.load_manifest_first')}</option>
                </select>
            </div>
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>4. ${tHtml('form.label.norm_start_phase')}</label>
                    <select id="normalizedPhaseSyncNormStartPhase" class="form-control" data-touched="false">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${tHtml('form.label.norm_end_phase')}</label>
                    <select id="normalizedPhaseSyncNormEndPhase" class="form-control" data-touched="false">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
            </div>
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>5. ${tHtml('form.label.stats_start_phase')}</label>
                    <select id="normalizedPhaseSyncStatsStartPhase" class="form-control" data-touched="false">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${tHtml('form.label.stats_end_phase')}</label>
                    <select id="normalizedPhaseSyncStatsEndPhase" class="form-control" data-touched="false">
                        <option value="">${tHtml('form.option.select')}</option>
                    </select>
                </div>
            </div>
            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executeNormalizedPhaseSyncAnalysis()">${tHtml('button.start_analyze')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>
            <div id="normalizedPhaseSyncResult" class="result-section" style="display: none;">
                <h3>${tHtml('result.section.title')}</h3>
                <div id="normalizedPhaseSyncResultContent"></div>
            </div>`;
        panel.innerHTML = html;
        this.showPanel(panel);
        this.loadNormalizedPhaseSyncPhases();
    }

    async selectNormalizedPhaseSyncManifest() {
        try {
            const filters = [{ displayName: 'CSV Files (*.csv)', pattern: '*.csv' }];
            const file = await SelectFile('選擇分期總檔案', filters, 'open');
            if (file) {
                document.getElementById('normalizedPhaseSyncManifest').value = file;
                await this.loadNormalizedPhaseSyncSubjects(file);
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }

    async loadNormalizedPhaseSyncSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('normalizedPhaseSyncSubject');
            select.innerHTML = '';
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = t('form.option.select_subject');
            select.appendChild(placeholder);
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });
            select.disabled = false;
            this.updateStatus(t('status.subjects_loaded', subjects.length));
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.load_subjects_failed', err));
        }
    }

    async selectNormalizedPhaseSyncDataFolder() {
        try {
            const folder = await SelectDirectory('選擇數據資料夾');
            if (folder) {
                document.getElementById('normalizedPhaseSyncDataFolder').value = folder;
            }
        } catch (err) {
            console.error('選擇資料夾失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
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

    async executeNormalizedPhaseSyncAnalysis() {
        try {
            const manifestFile = document.getElementById('normalizedPhaseSyncManifest').value;
            const dataFolder = document.getElementById('normalizedPhaseSyncDataFolder').value;
            const subjectIndex = parseInt(document.getElementById('normalizedPhaseSyncSubject').value);
            const normStartPhase = document.getElementById('normalizedPhaseSyncNormStartPhase').value;
            const normEndPhase = document.getElementById('normalizedPhaseSyncNormEndPhase').value;
            const statsStartPhase = document.getElementById('normalizedPhaseSyncStatsStartPhase').value;
            const statsEndPhase = document.getElementById('normalizedPhaseSyncStatsEndPhase').value;

            if (!manifestFile || !dataFolder || isNaN(subjectIndex)
                || !normStartPhase || !normEndPhase
                || !statsStartPhase || !statsEndPhase) {
                await ShowError(t('dialog.error'), t('error.msg.fill_required_fields'));
                return;
            }

            this.updateStatus(t('status.normalized_running'));
            const result = await AnalyzeNormalizedPhaseSync({
                manifestFile, dataFolder, subjectIndex,
                normStartPhase, normEndPhase,
                statsStartPhase, statsEndPhase
            });

            if (result.success) {
                this.showNormalizedPhaseSyncResult(result);
                await ShowMessage(
                    t('dialog.success'),
                    t('success.msg.normalized_analysis_done', result.normalizedEMGPath, result.phaseSyncCSVPath)
                );
            } else {
                await ShowError(t('dialog.error'), result.message);
            }
            this.updateStatus(t('status.analysis_done'));
        } catch (err) {
            console.error('標準化分期同步分析失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.analysis_failed_dynamic', err));
            this.updateStatus(t('status.analysis_failed'));
        }
    }

    showNormalizedPhaseSyncResult(result) {
        const resultDiv = document.getElementById('normalizedPhaseSyncResult');
        const contentDiv = document.getElementById('normalizedPhaseSyncResultContent');
        contentDiv.textContent = '';

        const info = document.createElement('div');
        info.className = 'result-info';
        const fmtTime = t => (typeof t === 'number' ? t.toFixed(3) : '—');
        const lines = [
            [t('result.label.subject'), result.subject],
            [t('result.label.norm_range'),
                `${result.normStartPhase} (${fmtTime(result.normStartTime)}s) → ${result.normEndPhase} (${fmtTime(result.normEndTime)}s)`],
            [t('result.label.stats_range'),
                `${result.statsStartPhase} (${fmtTime(result.statsStartTime)}s) → ${result.statsEndPhase} (${fmtTime(result.statsEndTime)}s)`],
            [t('result.label.output_normalized'), result.normalizedEMGPath],
            [t('result.label.output_stats'), result.phaseSyncCSVPath]
        ];
        lines.forEach(([label, value]) => {
            const p = document.createElement('p');
            const strong = document.createElement('strong');
            strong.textContent = label;
            p.appendChild(strong);
            p.appendChild(document.createTextNode(value != null ? String(value) : ''));
            info.appendChild(p);
        });
        contentDiv.appendChild(info);

        if (result.channelNames && result.channelMaxes) {
            const wrap = document.createElement('div');
            wrap.style.marginTop = '1rem';
            const h4 = document.createElement('h4');
            h4.textContent = t('result.label.muscles');
            wrap.appendChild(h4);
            const note = document.createElement('p');
            note.className = 'help-text';
            note.style.marginBottom = '0.5rem';
            note.textContent = t('result.normalized.help_text');
            wrap.appendChild(note);

            const table = document.createElement('table');
            table.className = 'result-table';
            table.style.width = '100%';
            table.style.borderCollapse = 'collapse';
            const thead = document.createElement('thead');
            const headRow = document.createElement('tr');
            [t('table.header.muscle'), t('table.header.norm_max'), t('table.header.norm_mean')].forEach((h, i) => {
                const th = document.createElement('th');
                th.textContent = h;
                th.style.padding = '0.25rem 0.5rem';
                th.style.textAlign = i === 0 ? 'left' : 'right';
                headRow.appendChild(th);
            });
            thead.appendChild(headRow);
            table.appendChild(thead);

            const tbody = document.createElement('tbody');
            result.channelNames.forEach(name => {
                const tr = document.createElement('tr');
                const tdName = document.createElement('td');
                tdName.textContent = name;
                tdName.style.padding = '0.25rem 0.5rem';
                const tdMax = document.createElement('td');
                const maxVal = result.channelMaxes[name];
                tdMax.textContent = typeof maxVal === 'number' ? maxVal.toFixed(6) : '—';
                tdMax.style.textAlign = 'right';
                tdMax.style.fontFamily = 'monospace';
                tdMax.style.padding = '0.25rem 0.5rem';
                const tdMean = document.createElement('td');
                const meanVal = result.channelMeans ? result.channelMeans[name] : undefined;
                tdMean.textContent = typeof meanVal === 'number' ? meanVal.toFixed(6) : '—';
                tdMean.style.textAlign = 'right';
                tdMean.style.fontFamily = 'monospace';
                tdMean.style.padding = '0.25rem 0.5rem';
                tr.appendChild(tdName);
                tr.appendChild(tdMax);
                tr.appendChild(tdMean);
                tbody.appendChild(tr);
            });
            table.appendChild(tbody);
            wrap.appendChild(table);
            contentDiv.appendChild(wrap);
        }

        if (result.report) {
            const reportWrap = document.createElement('div');
            reportWrap.className = 'result-report';
            reportWrap.style.marginTop = '1rem';
            const h4 = document.createElement('h4');
            h4.textContent = t('result.label.analysis_report');
            reportWrap.appendChild(h4);
            const pre = document.createElement('pre');
            pre.className = 'result-pre';
            pre.textContent = result.report;
            reportWrap.appendChild(pre);
            contentDiv.appendChild(reportWrap);
        }

        const btnGroup = document.createElement('div');
        btnGroup.className = 'button-group';
        const btn = document.createElement('button');
        btn.className = 'btn btn-primary';
        btn.textContent = t('button.open_output_folder');
        btn.onclick = () => this.openOutputFolder();
        btnGroup.appendChild(btn);
        contentDiv.appendChild(btnGroup);

        resultDiv.style.display = 'block';
    }

    // ==================== 肌肉比值分析 ====================

    // 肌肉比值分析面板：兩步驟批次處理
    showMuscleRatioPanel() {
        const panel = document.getElementById('functionPanel');
        // 純靜態模板（無 dynamic user input），與既有 showCCIPanel / showPhaseSyncPanel 對稱
        panel.innerHTML = `
            <div class="panel-header">
                <h2>${tHtml('panel.muscleratio.title')}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <p class="help-text">${tHtml('panel.muscleratio.description')}</p>

            <div class="form-group">
                <label>1. ${tHtml('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="muscleRatioManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="muscleRatioManifest" class="form-control" placeholder="${tHtml('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMuscleRatioManifest()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. ${tHtml('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="muscleRatioDataFolder" class="form-control" placeholder="${tHtml('form.placeholder.data_folder_emg')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMuscleRatioDataFolder()">${tHtml('button.browse')}</button>
                </div>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executeMuscleRatioAnalysis()">${tHtml('button.start_batch_analyze')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${tHtml('button.back')}</button>
            </div>

            <div id="muscleRatioResult" class="result-section" style="display: none;">
                <h3>${tHtml('result.section.title')}</h3>
                <div id="muscleRatioResultContent"></div>
            </div>
        `;

        this.showPanel(panel);
    }

    // 選擇肌肉比值分期總檔案
    async selectMuscleRatioManifest() {
        try {
            const filters = [{
                displayName: 'CSV Files (*.csv)',
                pattern: '*.csv'
            }];
            const file = await SelectFile('選擇分期總檔案', filters, 'open');

            if (file) {
                document.getElementById('muscleRatioManifest').value = file;
            }
        } catch (err) {
            console.error('選擇檔案失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }

    // 選擇肌肉比值數據資料夾
    async selectMuscleRatioDataFolder() {
        try {
            const folder = await SelectDirectory('選擇數據資料夾');
            if (folder) {
                document.getElementById('muscleRatioDataFolder').value = folder;
            }
        } catch (err) {
            console.error('選擇資料夾失敗:', err);
            await ShowError(t('dialog.error'), err.toString());
        }
    }

    // 執行肌肉比值批次分析
    async executeMuscleRatioAnalysis() {
        // 找到呼叫此函式的按鈕並 disable，避免雙擊產生兩個並發 RPC（會搶寫同一個輸出檔）
        const btn = document.querySelector('#functionPanel .btn-primary');
        if (btn) btn.disabled = true;

        try {
            const manifestFile = document.getElementById('muscleRatioManifest').value;
            const dataFolder = document.getElementById('muscleRatioDataFolder').value;

            if (!manifestFile || !dataFolder) {
                await ShowError(t('dialog.error'), t('error.msg.muscle_fill_fields'));
                return;
            }

            this.updateStatus(t('status.muscle_running'));

            const result = await AnalyzeMuscleRatio({ manifestFile, dataFolder });

            this.showMuscleRatioResult(result);

            // status 必須與 success 一致；部分失敗時顯示「分析完成」會誤導
            if (result.success) {
                this.updateStatus(t('status.analysis_done'));
                await ShowMessage(t('dialog.title.complete'), result.message);
            } else {
                this.updateStatus(t('dialog.title.partial_failed'));
                await ShowError(t('dialog.title.partial_failed'), result.message);
            }
        } catch (err) {
            console.error('肌肉比值分析失敗:', err);
            await ShowError(t('dialog.error'), t('error.msg.analysis_failed_dynamic', err));
            this.updateStatus(t('status.analysis_failed'));
        } finally {
            if (btn) btn.disabled = false;
        }
    }

    // 顯示肌肉比值批次結果（用 DOM API 避免 XSS：subject / error 可能來自外部 manifest 內容）
    showMuscleRatioResult(result) {
        const resultDiv = document.getElementById('muscleRatioResult');
        const contentDiv = document.getElementById('muscleRatioResultContent');
        contentDiv.textContent = '';

        const summary = document.createElement('p');
        const strong = document.createElement('strong');
        const count = result.subjects ? result.subjects.length : 0;
        strong.textContent = t('result.muscle.subject_count', count);
        summary.appendChild(strong);
        summary.appendChild(document.createTextNode(`：${result.message || ''}`));
        contentDiv.appendChild(summary);

        if (!result.subjects || result.subjects.length === 0) {
            resultDiv.style.display = '';
            return;
        }

        const table = document.createElement('table');
        table.className = 'result-table';
        table.style.width = '100%';
        table.style.borderCollapse = 'collapse';
        table.style.marginTop = '0.5rem';

        const thead = document.createElement('thead');
        const headRow = document.createElement('tr');
        [t('table.header.subject'), t('table.header.status'), t('table.header.output_all'), t('table.header.output_phase'), t('table.header.duration'), t('table.header.message')].forEach(h => {
            const th = document.createElement('th');
            th.textContent = h;
            th.style.padding = '0.25rem 0.5rem';
            th.style.textAlign = 'left';
            th.style.borderBottom = '1px solid #ddd';
            headRow.appendChild(th);
        });
        thead.appendChild(headRow);
        table.appendChild(thead);

        const tbody = document.createElement('tbody');
        result.subjects.forEach(sr => {
            const tr = document.createElement('tr');

            const tdName = document.createElement('td');
            tdName.textContent = sr.subject;
            tdName.style.padding = '0.25rem 0.5rem';
            tr.appendChild(tdName);

            const tdStatus = document.createElement('td');
            tdStatus.textContent = sr.success ? '✓' : '✗';
            tdStatus.style.padding = '0.25rem 0.5rem';
            tdStatus.style.color = sr.success ? '#2a9d3f' : '#c0392b';
            tdStatus.style.fontWeight = 'bold';
            tr.appendChild(tdStatus);

            const tdAll = document.createElement('td');
            tdAll.textContent = sr.outputAllPath || '—';
            tdAll.style.padding = '0.25rem 0.5rem';
            tdAll.style.fontFamily = 'monospace';
            tdAll.style.fontSize = '0.85em';
            tr.appendChild(tdAll);

            const tdPhase = document.createElement('td');
            tdPhase.textContent = sr.outputPhasePath || '—';
            tdPhase.style.padding = '0.25rem 0.5rem';
            tdPhase.style.fontFamily = 'monospace';
            tdPhase.style.fontSize = '0.85em';
            tr.appendChild(tdPhase);

            const tdDuration = document.createElement('td');
            tdDuration.textContent = sr.durationMs > 0 ? sr.durationMs.toString() : '—';
            tdDuration.style.padding = '0.25rem 0.5rem';
            tdDuration.style.fontFamily = 'monospace';
            tdDuration.style.fontSize = '0.85em';
            tdDuration.style.textAlign = 'right';
            tr.appendChild(tdDuration);

            const tdErr = document.createElement('td');
            tdErr.textContent = sr.error || '';
            tdErr.style.padding = '0.25rem 0.5rem';
            tdErr.style.fontSize = '0.85em';
            tdErr.style.color = sr.error ? '#c0392b' : 'inherit';
            tr.appendChild(tdErr);

            tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        contentDiv.appendChild(table);

        resultDiv.style.display = '';
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