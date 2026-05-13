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

// 應用程序主類
class EMGAnalysisApp {
    constructor() {
        this.currentPanel = null;
        this.config = null;
        this.init();
    }

    async init() {
        // 載入配置
        this.config = await GetConfig();

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
        this.updateStatus('應用程序已就緒');
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
                    ShowError('錯誤', '只支援 CSV 檔案');
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
                <h2>最大平均值計算</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="form-group">
                <label>處理模式</label>
                <select id="processMode" class="form-control" onchange="app.toggleProcessMode()">
                    <option value="single">單檔案處理</option>
                    <option value="batch">批次處理資料夾</option>
                </select>
            </div>
            
            <div id="singleFileSection">
                <div class="form-group">
                    <label>選擇資料檔案</label>
                    <div class="input-group drop-zone" data-drop-target="inputFile" style="--wails-drop-target: drop;">
                        <input type="text" id="inputFile" class="form-control" readonly>
                        <button class="btn btn-secondary" onclick="app.selectInputFile()">瀏覽</button>
                    </div>
                </div>
            </div>
            
            <div id="batchFolderSection" class="hidden">
                <div class="form-group">
                    <label>選擇資料夾</label>
                    <div class="input-group">
                        <input type="text" id="inputFolder" class="form-control" readonly>
                        <button class="btn btn-secondary" onclick="app.selectInputFolder()">瀏覽</button>
                    </div>
                </div>
            </div>
            
            <div class="form-group">
                <label>視窗大小（資料點數）</label>
                <input type="number" id="windowSize" class="form-control" value="1000" min="1">
                <p class="help-text">用於計算移動平均值的視窗大小</p>
            </div>
            
            <div class="form-group">
                <label>時間範圍（選填）</label>
                <div class="flex gap-2">
                    <div style="flex: 1;">
                        <input type="number" id="startTime" class="form-control" placeholder="開始時間（秒）" step="0.1">
                    </div>
                    <div style="flex: 1;">
                        <input type="number" id="endTime" class="form-control" placeholder="結束時間（秒）" step="0.1">
                    </div>
                </div>
                <p class="help-text">留空表示處理整個檔案</p>
            </div>
            
            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.calculateMaxMean()">
                    開始計算
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
                <h2>資料標準化</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="form-group">
                <label>主要資料檔案</label>
                <div class="input-group drop-zone" data-drop-target="mainFile" style="--wails-drop-target: drop;">
                    <input type="text" id="mainFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMainFile()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>參考資料檔案</label>
                <div class="input-group drop-zone" data-drop-target="referenceFile" style="--wails-drop-target: drop;">
                    <input type="text" id="referenceFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectReferenceFile()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>輸出檔名（選填）</label>
                <input type="text" id="outputName" class="form-control" placeholder="留空使用預設名稱">
            </div>
            
            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.normalizeData()">
                    開始標準化
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
                <h2>資料做圖</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="form-group">
                <label>選擇資料檔案</label>
                <div class="input-group drop-zone" data-drop-target="chartFile" style="--wails-drop-target: drop;">
                    <input type="text" id="chartFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectChartFile()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>圖表標題</label>
                <input type="text" id="chartTitle" class="form-control" value="EMG 資料分析圖表">
            </div>
            
            <div class="form-group">
                <label>選擇要顯示的欄位</label>
                <div id="columnSelector" class="checkbox-group">
                    <p class="help-text">請先選擇檔案</p>
                </div>
            </div>
            <div id="previewChartContainer" class="chart-preview hidden">
                <h3>即時圖表預覽</h3>
                <div id="previewChartContent"></div>
            </div>
            <div class="mt-4">
                <button id="downloadChartBtn" class="btn btn-primary"
                    onclick="app.downloadChart()" disabled style="display:none">
                  下載圖表
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
                <h2>階段分析</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="form-group">
                <label>選擇資料檔案</label>
                <div class="input-group drop-zone" data-drop-target="phaseFile" style="--wails-drop-target: drop;">
                    <input type="text" id="phaseFile" class="form-control" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseFile()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>階段時間點</label>
                <input type="text" id="phasePoints" class="form-control" placeholder="例如: 0.5, 1.0, 1.5, 2.0">
                <p class="help-text">輸入各階段的時間點（秒），用逗號分隔</p>
            </div>
            
            <div class="form-group">
                <label>階段標籤</label>
                <textarea id="phaseLabels" class="form-control" rows="4" placeholder="每行一個標籤">啟跳下蹲階段
啟跳上升階段
團身階段
下降階段</textarea>
            </div>
            
            <div class="mt-4">
                <button class="btn btn-primary" onclick="app.analyzePhases()">
                    開始分析
                </button>
            </div>
        `;

        this.showPanel();
    }

    // 系統配置面板
    async showConfigPanel() {
        const config = await GetConfig();

        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>系統配置</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="config-sections">
                <div class="config-section">
                    <h3 class="section-title">📊 數據處理設定</h3>
                    
                    <div class="form-group">
                        <label>縮放因子</label>
                        <input type="number" id="scalingFactor" class="form-control" value="${config.scalingFactor || 10}" min="1">
                        <p class="help-text">數據縮放倍數，用於放大微小信號</p>
                    </div>
                    
                    <div class="form-group">
                        <label>精度（小數位數）</label>
                        <input type="number" id="precision" class="form-control" value="${config.precision || 10}" min="0" max="15">
                        <p class="help-text">輸出數據的小數位數</p>
                    </div>
                    
                    <div class="form-group">
                        <label>輸出格式</label>
                        <select id="outputFormat" class="form-control">
                            <option value="csv" ${config.outputFormat === 'csv' ? 'selected' : ''}>CSV（逗號分隔值）</option>
                            <option value="json" ${config.outputFormat === 'json' ? 'selected' : ''}>JSON（JavaScript 對象表示法）</option>
                            <option value="xlsx" ${config.outputFormat === 'xlsx' ? 'selected' : ''}>XLSX（Excel 檔案）</option>
                        </select>
                        <p class="help-text">輸出檔案的格式</p>
                    </div>
                    
                    <div class="form-group">
                        <label>
                            <input type="checkbox" id="bomEnabled" ${config.bomEnabled ? 'checked' : ''}>
                            啟用 BOM（字節順序標記）
                        </label>
                        <p class="help-text">在 CSV 檔案開頭添加 BOM，改善 Excel 相容性</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">📁 目錄設定</h3>
                    
                    <div class="form-group">
                        <label>預設輸入目錄</label>
                        <div class="input-group">
                            <input type="text" id="inputDir" class="form-control" value="${config.inputDir || './input'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectInputDir()">瀏覽</button>
                        </div>
                        <p class="help-text">預設的資料檔案來源目錄</p>
                    </div>
                    
                    <div class="form-group">
                        <label>預設輸出目錄</label>
                        <div class="input-group">
                            <input type="text" id="outputDir" class="form-control" value="${config.outputDir || './output'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectOutputDir()">瀏覽</button>
                        </div>
                        <p class="help-text">處理結果的儲存目錄</p>
                    </div>
                    
                    <div class="form-group">
                        <label>參考資料目錄</label>
                        <div class="input-group">
                            <input type="text" id="operateDir" class="form-control" value="${config.operateDir || './value_operate'}" readonly>
                            <button class="btn btn-secondary" onclick="app.selectOperateDir()">瀏覽</button>
                        </div>
                        <p class="help-text">存放參考檔案的目錄</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">🏷️ 階段標籤設定</h3>
                    
                    <div class="form-group">
                        <label>階段標籤（每行一個）</label>
                        <textarea id="phaseLabels" class="form-control" rows="4">${(config.phaseLabels || []).join('\n')}</textarea>
                        <p class="help-text">定義階段分析時使用的標籤名稱</p>
                    </div>
                </div>

                <div class="config-section">
                    <h3 class="section-title">🔧 進階設定</h3>
                    
                    <div class="form-group">
                        <label>日誌級別</label>
                        <select id="logLevel" class="form-control">
                            <option value="debug" ${config.logLevel === 'debug' ? 'selected' : ''}>Debug（除錯）</option>
                            <option value="info" ${config.logLevel === 'info' ? 'selected' : ''}>Info（資訊）</option>
                            <option value="warn" ${config.logLevel === 'warn' ? 'selected' : ''}>Warn（警告）</option>
                            <option value="error" ${config.logLevel === 'error' ? 'selected' : ''}>Error（錯誤）</option>
                        </select>
                        <p class="help-text">控制日誌輸出的詳細程度</p>
                    </div>
                    
                    <div class="form-group">
                        <label>介面語言</label>
                        <select id="language" class="form-control">
                            <option value="zh-TW" ${config.language === 'zh-TW' ? 'selected' : ''}>繁體中文</option>
                            <option value="zh-CN" ${config.language === 'zh-CN' ? 'selected' : ''}>简体中文</option>
                            <option value="en-US" ${config.language === 'en-US' ? 'selected' : ''}>English</option>
                            <option value="ja-JP" ${config.language === 'ja-JP' ? 'selected' : ''}>日本語</option>
                        </select>
                        <p class="help-text">應用程序的顯示語言</p>
                    </div>
                </div>
            </div>
            
            <div class="mt-4 flex gap-2">
                <button class="btn btn-primary" onclick="app.saveConfig()">
                    <span class="icon">💾</span> 儲存設定
                </button>
                <button class="btn btn-secondary" onclick="app.resetConfig()">
                    <span class="icon">🔄</span> 重設為預設值
                </button>
                <button class="btn btn-info" onclick="app.importConfig()">
                    <span class="icon">📥</span> 匯入設定
                </button>
            </div>
        `;

        this.showPanel();
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
                await ShowError('錯誤', '請選擇資料檔案');
                return;
            }
        } else {
            inputPath = document.getElementById('inputFolder').value;
            if (!inputPath) {
                await ShowError('錯誤', '請選擇資料夾');
                return;
            }
        }

        try {
            this.updateStatus('正在計算最大平均值...');
            const result = await CalculateMaxMean({
                inputPath: inputPath,
                windowSize: windowSize,
                startTime: startTime,
                endTime: endTime,
                isBatch: mode === 'batch'
            });

            this.updateStatus('計算完成');
            await ShowMessage('成功', `計算完成！結果已儲存至：\n${result.outputPath}`);
        } catch (err) {
            this.updateStatus('計算失敗');
            await ShowError('錯誤', `計算失敗：${err}`);
        }
    }

    async normalizeData() {
        const mainFile = document.getElementById('mainFile').value;
        const referenceFile = document.getElementById('referenceFile').value;
        const outputName = document.getElementById('outputName').value;

        if (!mainFile || !referenceFile) {
            await ShowError('錯誤', '請選擇主要資料檔案和參考資料檔案');
            return;
        }

        try {
            this.updateStatus('正在進行資料標準化...');
            const result = await NormalizeData({
                mainFile: mainFile,
                referenceFile: referenceFile,
                outputPath: outputName
            });

            this.updateStatus('標準化完成');
            await ShowMessage('成功', `標準化完成！結果已儲存至：\n${result.outputPath}`);
        } catch (err) {
            this.updateStatus('標準化失敗');
            await ShowError('錯誤', `標準化失敗：${err}`);
        }
    }

    async generateChart() {
        const file = document.getElementById('chartFile').value;
        if (!file) {
            await ShowError('錯誤', '請選擇資料檔案');
            return;
        }

        const checked = document.querySelectorAll('#columnSelector input[type="checkbox"]:checked');
        const columns = Array.from(checked).map(cb => parseInt(cb.value));
        if (columns.length === 0) {
            await ShowError('錯誤', '請選擇至少一個欄位');
            return;
        }

        const title = document.getElementById('chartTitle').value || 'EMG 資料分析圖表';

        try {
            this.updateStatus('正在生成圖表...');

            const result = await GenerateChart({
                filePath: file,
                columns: columns,
                title: title
            });

            this.updateStatus('圖表生成完成');
            await ShowMessage('成功', `圖表已生成並保存至：\n${result.outputPath}`);
        } catch (err) {
            this.updateStatus('圖表生成失敗');
            await ShowError('錯誤', `圖表生成失敗：${err}`);
        }
    }

    async downloadChart() {
        const iframe = document.querySelector('#previewChartContent iframe');
        if (!iframe) {
            await ShowError('錯誤', '請先預覽圖表');
            return;
        }

        const file = document.getElementById('chartFile').value;
        if (!file) {
            await ShowError('錯誤', '請選擇資料檔案');
            return;
        }

        try {
            this.updateStatus('正在下載圖表...');

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
                throw new Error('ECharts 未找到');
            }

            // 尋找 ECharts 實例
            const chartElement = iframeDocument.querySelector('[_echarts_instance_]');
            if (!chartElement) {
                throw new Error('找不到圖表元素');
            }

            const chartInstance = iframeWindow.echarts.getInstanceByDom(chartElement);
            if (!chartInstance) {
                throw new Error('找不到圖表實例');
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

            this.updateStatus('圖表下載完成');
            await ShowMessage('成功', `圖表已下載至：${result.outputPath}`);
        } catch (err) {
            this.updateStatus('圖表下載失敗');
            await ShowError('錯誤', `下載失敗：${err.message || err}`);
        }
    }

    async analyzePhases() {
        const inputFile = document.getElementById('phaseFile').value;
        const phasePoints = document.getElementById('phasePoints').value;
        const phaseLabels = document.getElementById('phaseLabels').value;

        if (!inputFile || !phasePoints) {
            await ShowError('錯誤', '請選擇資料檔案並輸入階段時間點');
            return;
        }

        // 解析時間點和標籤
        const points = phasePoints.split(',').map(p => parseFloat(p.trim()));
        const labels = phaseLabels.split('\n').filter(l => l.trim());

        if (points.length !== labels.length + 1) {
            await ShowError('錯誤', '時間點數量應該比標籤數量多1');
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
            this.updateStatus('正在進行階段分析...');
            const result = await AnalyzePhases({
                inputFile: inputFile,
                phases: phases
            });

            this.updateStatus('階段分析完成');
            await ShowMessage('成功', `階段分析完成！結果已儲存至：\n${result.outputPath}`);
        } catch (err) {
            this.updateStatus('階段分析失敗');
            await ShowError('錯誤', `階段分析失敗：${err}`);
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
            await ShowMessage('成功', '配置已儲存');
        } catch (err) {
            await ShowError('錯誤', `儲存配置失敗：${err}`);
        }
    }

    async resetConfig() {
        try {
            const config = await ResetConfig();
            this.config = config;
            await this.showConfigPanel();
            await ShowMessage('成功', '配置已重設為預設值');
        } catch (err) {
            await ShowError('錯誤', `重設配置失敗：${err}`);
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
                        throw new Error('無效的配置檔案格式');
                    }

                    await SaveConfig(config);
                    this.config = config;
                    await this.showConfigPanel();
                    await ShowMessage('成功', '配置已匯入並儲存');
                } catch (err) {
                    await ShowError('錯誤', `匯入配置失敗：${err.message}`);
                }
            };

            input.click();
        } catch (err) {
            await ShowError('錯誤', `開啟檔案選擇器失敗：${err}`);
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
        this.updateStatus('就緒');
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
        selector.innerHTML = '<p class="help-text">載入欄位中...</p>';
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
            selector.innerHTML = '<p class="help-text text-danger">載入欄位失敗</p>';
            await ShowError('錯誤', `讀取欄位失敗：${err}`);
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
            await ShowError('錯誤', `即時預覽失敗：${err}`);
        }
    }
    // 分期同步分析面板
    showPhaseSyncPanel() {
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = `
            <div class="panel-header">
                <h2>分期同步分析</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div class="form-group">
                <label>1. 選擇分期總檔案</label>
                <div class="input-group drop-zone" data-drop-target="phaseSyncManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="phaseSyncManifest" class="form-control" placeholder="選擇或拖放分期總檔案 (.csv)" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseSyncManifest()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>2. 選擇數據資料夾</label>
                <div class="input-group">
                    <input type="text" id="phaseSyncDataFolder" class="form-control" placeholder="選擇包含所有數據檔案的資料夾" readonly>
                    <button class="btn btn-secondary" onclick="app.selectPhaseSyncDataFolder()">瀏覽</button>
                </div>
            </div>
            
            <div class="form-group">
                <label>3. 選擇分析主題</label>
                <select id="phaseSyncSubject" class="form-control" disabled>
                    <option value="">請先載入分期總檔案</option>
                </select>
            </div>
            
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>4. 開始分期點</label>
                    <select id="phaseSyncStartPhase" class="form-control">
                        <option value="">請選擇</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>結束分期點</label>
                    <select id="phaseSyncEndPhase" class="form-control">
                        <option value="">請選擇</option>
                    </select>
                </div>
            </div>
            
            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executePhaseSyncAnalysis()">開始分析</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">返回</button>
            </div>
            
            <div id="phaseSyncResult" class="result-section" style="display: none;">
                <h3>分析結果</h3>
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
            await ShowError('錯誤', err.toString());
        }
    }
    
    // 載入分期總檔案的主題
    async loadManifestSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('phaseSyncSubject');
            
            select.innerHTML = '<option value="">請選擇主題</option>';
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });
            
            select.disabled = false;
            this.updateStatus(`已載入 ${subjects.length} 個主題`);
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError('錯誤', `載入主題失敗: ${err}`);
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
            await ShowError('錯誤', err.toString());
        }
    }
    
    // 載入可用的分期點
    async loadAvailablePhases() {
        try {
            const phases = await GetAvailablePhases();
            
            // 填充開始分期點
            const startSelect = document.getElementById('phaseSyncStartPhase');
            startSelect.innerHTML = '<option value="">請選擇</option>';
            phases.start.forEach(phase => {
                const option = document.createElement('option');
                option.value = phase;
                option.textContent = phase;
                startSelect.appendChild(option);
            });
            
            // 填充結束分期點
            const endSelect = document.getElementById('phaseSyncEndPhase');
            endSelect.innerHTML = '<option value="">請選擇</option>';
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
                await ShowError('錯誤', '請填寫所有必要欄位');
                return;
            }
            
            this.updateStatus('正在進行分期同步分析...');
            
            const result = await AnalyzePhaseSync({
                manifestFile,
                dataFolder,
                subjectIndex,
                startPhase,
                endPhase
            });
            
            if (result.success) {
                await this.showPhaseSyncResult(result);
                await ShowMessage('成功', `分析完成！結果已保存至：${result.outputPath}`);
            } else {
                await ShowError('錯誤', result.message);
            }
            
            this.updateStatus('分析完成');
        } catch (err) {
            console.error('分期同步分析失敗:', err);
            await ShowError('錯誤', `分析失敗: ${err}`);
            this.updateStatus('分析失敗');
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
                <h2>共同收縮分析 (CCI Rudolph)</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>

            <div class="form-group">
                <label>1. 選擇分期總檔案</label>
                <div class="input-group drop-zone" data-drop-target="cciManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="cciManifest" class="form-control" placeholder="選擇或拖放分期總檔案 (.csv)" readonly>
                    <button class="btn btn-secondary" onclick="app.selectCCIManifest()">瀏覽</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. 選擇數據資料夾</label>
                <div class="input-group">
                    <input type="text" id="cciDataFolder" class="form-control" placeholder="選擇包含 EMG 數據檔案的資料夾" readonly>
                    <button class="btn btn-secondary" onclick="app.selectCCIDataFolder()">瀏覽</button>
                </div>
            </div>

            <div class="form-group">
                <label>3. 選擇分析主題</label>
                <select id="cciSubject" class="form-control" disabled>
                    <option value="">請先載入分期總檔案</option>
                </select>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executeCCIAnalysis()">開始分析</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">返回</button>
            </div>

            <div id="cciResult" class="result-section" style="display: none;">
                <h3>分析結果</h3>
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
            await ShowError('錯誤', err.toString());
        }
    }

    // 載入 CCI 分期總檔案的主題
    async loadCCIManifestSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('cciSubject');

            select.innerHTML = '<option value="">請選擇主題</option>';
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });

            select.disabled = false;
            this.updateStatus(`已載入 ${subjects.length} 個主題`);
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError('錯誤', `載入主題失敗: ${err}`);
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
            await ShowError('錯誤', err.toString());
        }
    }

    // 執行 CCI 分析
    async executeCCIAnalysis() {
        try {
            const manifestFile = document.getElementById('cciManifest').value;
            const dataFolder = document.getElementById('cciDataFolder').value;
            const subjectIndex = parseInt(document.getElementById('cciSubject').value);

            if (!manifestFile || !dataFolder || isNaN(subjectIndex)) {
                await ShowError('錯誤', '請填寫所有必要欄位');
                return;
            }

            this.updateStatus('正在進行 CCI Rudolph 分析...');

            const result = await AnalyzeCCI({
                manifestFile,
                dataFolder,
                subjectIndex
            });

            if (result.success) {
                this.showCCIResult(result);
                await ShowMessage('成功', `分析完成！\nCSV: ${result.outputCSVPath}`);
            } else {
                await ShowError('錯誤', result.message);
            }

            this.updateStatus('分析完成');
        } catch (err) {
            console.error('CCI 分析失敗:', err);
            await ShowError('錯誤', `分析失敗: ${err}`);
            this.updateStatus('分析失敗');
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
        strong.textContent = '分期點位置 (步態週期 %)：';
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
            await ShowError('錯誤', '找不到圖表');
            return;
        }

        try {
            this.updateStatus('正在下載 CCI 圖表...');

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
                throw new Error('ECharts 未找到');
            }

            const chartElement = iframeDocument.querySelector('[_echarts_instance_]');
            if (!chartElement) {
                throw new Error('找不到圖表元素');
            }

            const chartInstance = iframeWindow.echarts.getInstanceByDom(chartElement);
            if (!chartInstance) {
                throw new Error('找不到圖表實例');
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

            this.updateStatus('圖表下載完成');
            await ShowMessage('成功', `圖表已下載至：${result.outputPath}`);
        } catch (err) {
            this.updateStatus('圖表下載失敗');
            await ShowError('錯誤', `下載失敗：${err.message || err}`);
        }
    }

    // ==================== 標準化分期同步分析 ====================

    showNormalizedPhaseSyncPanel() {
        const panel = document.getElementById('functionPanel');
        const html = [
            '<div class="panel-header">',
            '<h2>標準化分期同步分析</h2>',
            '<button class="btn-back" onclick="app.showMainMenu()">返回</button>',
            '</div>',
            '<p class="help-text" style="margin-bottom: 1rem;">',
            '先以每條肌肉在「標準化區間」內的最大值做標準化（除數），',
            '再對標準化後的資料於「統計區間」內計算分期同步統計。一次產生兩個檔案。',
            '標準化區間與統計區間可獨立選擇（預設兩者相同，可分別調整）。',
            '</p>',
            '<div class="form-group">',
            '<label>1. 選擇分期總檔案</label>',
            '<div class="input-group drop-zone" data-drop-target="normalizedPhaseSyncManifest" style="--wails-drop-target: drop;">',
            '<input type="text" id="normalizedPhaseSyncManifest" class="form-control" placeholder="選擇或拖放分期總檔案 (.csv)" readonly>',
            '<button class="btn btn-secondary" onclick="app.selectNormalizedPhaseSyncManifest()">瀏覽</button>',
            '</div></div>',
            '<div class="form-group">',
            '<label>2. 選擇數據資料夾</label>',
            '<div class="input-group">',
            '<input type="text" id="normalizedPhaseSyncDataFolder" class="form-control" placeholder="選擇包含 EMG 數據檔案的資料夾" readonly>',
            '<button class="btn btn-secondary" onclick="app.selectNormalizedPhaseSyncDataFolder()">瀏覽</button>',
            '</div></div>',
            '<div class="form-group">',
            '<label>3. 選擇分析主題</label>',
            '<select id="normalizedPhaseSyncSubject" class="form-control" disabled>',
            '<option value="">請先載入分期總檔案</option>',
            '</select></div>',
            '<div class="form-row">',
            '<div class="form-group col-md-6">',
            '<label>4. 標準化開始分期點</label>',
            '<select id="normalizedPhaseSyncNormStartPhase" class="form-control" data-touched="false">',
            '<option value="">請選擇</option></select></div>',
            '<div class="form-group col-md-6">',
            '<label>標準化結束分期點</label>',
            '<select id="normalizedPhaseSyncNormEndPhase" class="form-control" data-touched="false">',
            '<option value="">請選擇</option></select></div>',
            '</div>',
            '<div class="form-row">',
            '<div class="form-group col-md-6">',
            '<label>5. 統計開始分期點</label>',
            '<select id="normalizedPhaseSyncStatsStartPhase" class="form-control" data-touched="false">',
            '<option value="">請選擇</option></select></div>',
            '<div class="form-group col-md-6">',
            '<label>統計結束分期點</label>',
            '<select id="normalizedPhaseSyncStatsEndPhase" class="form-control" data-touched="false">',
            '<option value="">請選擇</option></select></div>',
            '</div>',
            '<div class="button-group">',
            '<button class="btn btn-primary" onclick="app.executeNormalizedPhaseSyncAnalysis()">開始分析</button>',
            '<button class="btn btn-secondary" onclick="app.showMainMenu()">返回</button>',
            '</div>',
            '<div id="normalizedPhaseSyncResult" class="result-section" style="display: none;">',
            '<h3>分析結果</h3>',
            '<div id="normalizedPhaseSyncResultContent"></div>',
            '</div>'
        ].join('');
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
            await ShowError('錯誤', err.toString());
        }
    }

    async loadNormalizedPhaseSyncSubjects(manifestPath) {
        try {
            const subjects = await LoadPhaseManifest(manifestPath);
            const select = document.getElementById('normalizedPhaseSyncSubject');
            select.innerHTML = '';
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = '請選擇主題';
            select.appendChild(placeholder);
            subjects.forEach((subject, index) => {
                const option = document.createElement('option');
                option.value = index;
                option.textContent = subject;
                select.appendChild(option);
            });
            select.disabled = false;
            this.updateStatus(`已載入 ${subjects.length} 個主題`);
        } catch (err) {
            console.error('載入主題失敗:', err);
            await ShowError('錯誤', `載入主題失敗: ${err}`);
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
            await ShowError('錯誤', err.toString());
        }
    }

    async loadNormalizedPhaseSyncPhases() {
        try {
            const phases = await GetAvailablePhases();
            const populate = (selectEl, list) => {
                selectEl.replaceChildren();
                const placeholder = document.createElement('option');
                placeholder.value = '';
                placeholder.textContent = '請選擇';
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
                await ShowError('錯誤', '請填寫所有必要欄位');
                return;
            }

            this.updateStatus('正在進行標準化分期同步分析...');
            const result = await AnalyzeNormalizedPhaseSync({
                manifestFile, dataFolder, subjectIndex,
                normStartPhase, normEndPhase,
                statsStartPhase, statsEndPhase
            });

            if (result.success) {
                this.showNormalizedPhaseSyncResult(result);
                await ShowMessage(
                    '成功',
                    `分析完成！\n標準化 EMG: ${result.normalizedEMGPath}\n分期統計: ${result.phaseSyncCSVPath}`
                );
            } else {
                await ShowError('錯誤', result.message);
            }
            this.updateStatus('分析完成');
        } catch (err) {
            console.error('標準化分期同步分析失敗:', err);
            await ShowError('錯誤', `分析失敗: ${err}`);
            this.updateStatus('分析失敗');
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
            ['主題：', result.subject],
            ['標準化區間：',
                `${result.normStartPhase} (${fmtTime(result.normStartTime)}s) → ${result.normEndPhase} (${fmtTime(result.normEndTime)}s)`],
            ['統計區間：',
                `${result.statsStartPhase} (${fmtTime(result.statsStartTime)}s) → ${result.statsEndPhase} (${fmtTime(result.statsEndTime)}s)`],
            ['Output 1（標準化 EMG）：', result.normalizedEMGPath],
            ['Output 2（分期統計）：', result.phaseSyncCSVPath]
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
            h4.textContent = '各肌肉數值';
            wrap.appendChild(h4);
            const note = document.createElement('p');
            note.className = 'help-text';
            note.style.marginBottom = '0.5rem';
            note.textContent = '「標準化區間最大值」為標準化前在標準化區間內的原始最大值（用作除數）；「標準化後區間平均」為標準化後在統計區間內的平均值。Output 2 中各肌肉最大值在「標準化區間與統計區間相同時」會列為 1.000000（區間內 max 除以自己 = 1）；當兩區間不同時，該數值為 max(統計區間) / max(標準化區間)，可能小於或大於 1。';
            wrap.appendChild(note);

            const table = document.createElement('table');
            table.className = 'result-table';
            table.style.width = '100%';
            table.style.borderCollapse = 'collapse';
            const thead = document.createElement('thead');
            const headRow = document.createElement('tr');
            ['肌肉', '標準化區間最大值', '標準化後區間平均'].forEach((h, i) => {
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
            h4.textContent = '分析報告';
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
        btn.textContent = '開啟輸出資料夾';
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
                <h2>肌肉比值分析</h2>
                <button class="btn-back" onclick="app.showMainMenu()">返回</button>
            </div>

            <p class="help-text">
                批次計算 manifest 中所有 subject 的 4 對右側肌肉比值：
                <strong>R.RA/R.ES</strong>、<strong>R.IL/R.GMax</strong>、<strong>R.RF/R.BF</strong>、<strong>R.TA&amp;IO/R.MF</strong>。
                每個 subject 產出兩個 CSV：完整時間序列 + 分期點切片（10 個分期 + 9 個中間時間點 = 19 列）。
            </p>

            <div class="form-group">
                <label>1. 選擇分期總檔案</label>
                <div class="input-group drop-zone" data-drop-target="muscleRatioManifest" style="--wails-drop-target: drop;">
                    <input type="text" id="muscleRatioManifest" class="form-control" placeholder="選擇或拖放分期總檔案 (.csv)" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMuscleRatioManifest()">瀏覽</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. 選擇數據資料夾</label>
                <div class="input-group">
                    <input type="text" id="muscleRatioDataFolder" class="form-control" placeholder="選擇包含 EMG 數據檔案的資料夾" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMuscleRatioDataFolder()">瀏覽</button>
                </div>
            </div>

            <div class="button-group">
                <button class="btn btn-primary" onclick="app.executeMuscleRatioAnalysis()">開始批次分析</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">返回</button>
            </div>

            <div id="muscleRatioResult" class="result-section" style="display: none;">
                <h3>分析結果</h3>
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
            await ShowError('錯誤', err.toString());
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
            await ShowError('錯誤', err.toString());
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
                await ShowError('錯誤', '請選擇分期總檔案與數據資料夾');
                return;
            }

            this.updateStatus('正在批次計算肌肉比值...');

            const result = await AnalyzeMuscleRatio({ manifestFile, dataFolder });

            this.showMuscleRatioResult(result);

            // status 必須與 success 一致；部分失敗時顯示「分析完成」會誤導
            if (result.success) {
                this.updateStatus('分析完成');
                await ShowMessage('完成', result.message);
            } else {
                this.updateStatus('部分失敗');
                await ShowError('部分失敗', result.message);
            }
        } catch (err) {
            console.error('肌肉比值分析失敗:', err);
            await ShowError('錯誤', `分析失敗: ${err}`);
            this.updateStatus('分析失敗');
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
        strong.textContent = `共 ${count} 個主題`;
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
        ['主題', '狀態', 'Output 1 (時間序列)', 'Output 2 (分期切片)', '訊息'].forEach(h => {
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
                await ShowMessage('提示', `輸出檔案已保存至：${config.outputDir}`);
            }
        } catch (err) {
            console.error('無法開啟輸出資料夾:', err);
            await ShowError('錯誤', '無法開啟輸出資料夾');
        } 
    }
}

// 創建全局應用實例
window.app = new EMGAnalysisApp();