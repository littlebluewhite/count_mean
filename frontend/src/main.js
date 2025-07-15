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
    GenerateInteractiveChart
} from '../wailsjs/go/new_gui/App.js';
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
}

// 創建全局應用實例
window.app = new EMGAnalysisApp();