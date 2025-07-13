package gui

import (
	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/validation"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"image"
	"path/filepath"
	"strings"
)

// App 表示GUI應用程式
type App struct {
	app           fyne.App
	window        fyne.Window
	config        *config.AppConfig
	csvHandler    *io.CSVHandler
	maxMeanCalc   *calculator.MaxMeanCalculator
	normalizer    *calculator.Normalizer
	phaseAnalyzer *calculator.PhaseAnalyzer
	chartGen      *chart.ChartGenerator
	validator     *validation.InputValidator
	logger        *logging.Logger
	statusLabel   *widget.Label
}

// NewApp 創建新的GUI應用程式
func NewApp(cfg *config.AppConfig) *App {
	myApp := app.NewWithID("com.emgtool.analysis")
	myApp.SetIcon(nil) // 可以稍後添加圖標

	window := myApp.NewWindow("EMG 資料分析工具")
	window.SetMainMenu(nil)
	window.Resize(fyne.NewSize(800, 600))

	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
	normalizer := calculator.NewNormalizer(cfg.ScalingFactor)
	phaseAnalyzer := calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels)
	chartGen := chart.NewChartGenerator()
	validator := validation.NewInputValidator()
	logger := logging.GetLogger("gui")

	return &App{
		app:           myApp,
		window:        window,
		config:        cfg,
		csvHandler:    csvHandler,
		maxMeanCalc:   maxMeanCalc,
		normalizer:    normalizer,
		phaseAnalyzer: phaseAnalyzer,
		chartGen:      chartGen,
		validator:     validator,
		logger:        logger,
	}
}

// Run 啟動GUI應用程式
func (a *App) Run() {
	a.setupUI()
	a.window.ShowAndRun()
}

// setupUI 設置用戶界面
func (a *App) setupUI() {
	// 創建主功能按鈕
	maxMeanBtn := widget.NewButton("最大平均值計算", func() {
		a.showMaxMeanCalculationDialog()
	})
	maxMeanBtn.Importance = widget.HighImportance

	normalizationBtn := widget.NewButton("資料標準化", func() {
		a.showNormalizationDialog()
	})
	normalizationBtn.Importance = widget.HighImportance

	phaseAnalysisBtn := widget.NewButton("階段分析", func() {
		a.showPhaseAnalysisDialog()
	})

	configBtn := widget.NewButton("配置設定", func() {
		a.showConfigDialog()
	})

	chartBtn := widget.NewButton("做圖", func() {
		a.showChartDialog()
	})
	chartBtn.Importance = widget.HighImportance

	// 創建狀態標籤
	statusLabel := widget.NewLabel("準備就緒")
	a.statusLabel = statusLabel

	// 創建主佈局
	title := widget.NewLabel("EMG 資料分析工具")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := widget.NewLabel("選擇要執行的分析功能")
	subtitle.Alignment = fyne.TextAlignCenter

	buttonContainer := container.NewGridWithColumns(3,
		maxMeanBtn,
		normalizationBtn,
		chartBtn,
		phaseAnalysisBtn,
		configBtn,
		widget.NewLabel(""), // 空白填充
	)

	content := container.NewVBox(
		widget.NewSeparator(),
		title,
		subtitle,
		widget.NewSeparator(),
		buttonContainer,
		widget.NewSeparator(),
		statusLabel,
	)

	// 添加邊距
	paddedContent := container.NewPadded(content)

	a.window.SetContent(paddedContent)
}

// updateStatus 更新狀態顯示
func (a *App) updateStatus(message string) {
	if a.statusLabel != nil {
		a.statusLabel.SetText(message)
	}
}

// showError 顯示錯誤對話框
func (a *App) showError(message string) {
	a.updateStatus("錯誤: " + message)
	dialog.ShowError(fmt.Errorf("%s", message), a.window)
}

// showInfo 顯示信息對話框
func (a *App) showInfo(message string) {
	dialog.ShowInformation("信息", message, a.window)
}

// showFileSelectDialog 顯示文件選擇對話框
func (a *App) showFileSelectDialog(filePath *string, label *widget.Label) {
	// 創建CSV文件過濾器
	csvFilter := storage.NewExtensionFileFilter([]string{".csv"})

	// 創建文件選擇對話框
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			a.showError(fmt.Sprintf("選擇檔案時發生錯誤: %v", err))
			return
		}
		if reader == nil {
			// 用戶取消了選擇
			return
		}
		defer reader.Close()

		uri := reader.URI()
		*filePath = uri.Path()
		fileName := uri.Name()
		label.SetText(fileName)
		a.updateStatus("已選擇檔案: " + fileName)
	}, a.window)

	fileDialog.SetFilter(csvFilter)

	// 設置初始位置為輸入目錄
	if inputURI := storage.NewFileURI(a.config.InputDir); inputURI != nil {
		if listableURI, ok := inputURI.(fyne.ListableURI); ok {
			fileDialog.SetLocation(listableURI)
		}
	}

	fileDialog.Show()
}

// showFileSelectDialogWithDir 顯示文件選擇對話框（指定目錄）
func (a *App) showFileSelectDialogWithDir(filePath *string, label *widget.Label, defaultDir string) {
	// 創建CSV文件過濾器
	csvFilter := storage.NewExtensionFileFilter([]string{".csv"})

	// 創建文件選擇對話框
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			a.showError(fmt.Sprintf("選擇檔案時發生錯誤: %v", err))
			return
		}
		if reader == nil {
			// 用戶取消了選擇
			return
		}
		defer reader.Close()

		uri := reader.URI()
		*filePath = uri.Path()
		fileName := uri.Name()
		label.SetText(fileName)
		a.updateStatus("已選擇檔案: " + fileName)
	}, a.window)

	fileDialog.SetFilter(csvFilter)

	// 設置初始位置為指定目錄
	if defaultDir != "" {
		if dirURI := storage.NewFileURI(defaultDir); dirURI != nil {
			if listableURI, ok := dirURI.(fyne.ListableURI); ok {
				fileDialog.SetLocation(listableURI)
			}
		}
	}

	fileDialog.Show()
}

// showFolderSelectDialog 顯示資料夾選擇對話框
func (a *App) showFolderSelectDialog(dirPath *string, label *widget.Label) {
	// 創建資料夾選擇對話框
	folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			a.showError(fmt.Sprintf("選擇資料夾時發生錯誤: %v", err))
			return
		}
		if uri == nil {
			// 用戶取消了選擇
			return
		}

		*dirPath = uri.Path()
		dirName := uri.Name()
		label.SetText(dirName)
		a.updateStatus("已選擇資料夾: " + dirName)
	}, a.window)

	// 設置初始位置為輸入目錄
	if inputURI := storage.NewFileURI(a.config.InputDir); inputURI != nil {
		if listableURI, ok := inputURI.(fyne.ListableURI); ok {
			folderDialog.SetLocation(listableURI)
		}
	}

	folderDialog.Show()
}

// showDirectorySelectDialog 顯示目錄選擇對話框（用於配置設定）
func (a *App) showDirectorySelectDialog(dirPath *string, entry *widget.Entry) {
	// 創建資料夾選擇對話框
	folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			a.showError(fmt.Sprintf("選擇資料夾時發生錯誤: %v", err))
			return
		}
		if uri == nil {
			// 用戶取消了選擇
			return
		}

		*dirPath = uri.Path()
		entry.SetText(*dirPath)
		a.updateStatus("已選擇資料夾: " + uri.Name())
	}, a.window)

	// 設置初始位置
	if *dirPath != "" {
		if dirURI := storage.NewFileURI(*dirPath); dirURI != nil {
			if listableURI, ok := dirURI.(fyne.ListableURI); ok {
				folderDialog.SetLocation(listableURI)
			}
		}
	}

	folderDialog.Show()
}

// showMaxMeanCalculationDialog 顯示最大平均值計算對話框
func (a *App) showMaxMeanCalculationDialog() {
	a.updateStatus("準備最大平均值計算...")

	// 創建對話框內容
	title := widget.NewLabel("最大平均值計算")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// 處理模式選擇
	modeGroup := widget.NewRadioGroup([]string{"處理單一檔案", "批量處理資料夾"}, nil)
	modeGroup.SetSelected("處理單一檔案")

	// 檔案和資料夾選擇
	var selectedFilePath string
	var selectedDirPath string

	fileSelectLabel := widget.NewLabel("未選擇檔案")
	fileSelectBtn := widget.NewButton("選擇CSV檔案", func() {
		a.showFileSelectDialog(&selectedFilePath, fileSelectLabel)
	})
	fileContainer := container.NewHBox(fileSelectBtn, fileSelectLabel)

	dirSelectLabel := widget.NewLabel("未選擇資料夾")
	dirSelectBtn := widget.NewButton("選擇資料夾", func() {
		a.showFolderSelectDialog(&selectedDirPath, dirSelectLabel)
	})
	dirContainer := container.NewHBox(dirSelectBtn, dirSelectLabel)
	dirContainer.Hide()

	// 參數輸入
	windowSizeEntry := widget.NewEntry()
	windowSizeEntry.SetPlaceHolder("窗口大小（資料點數）")

	startRangeEntry := widget.NewEntry()
	startRangeEntry.SetPlaceHolder("開始範圍秒數（可選）")

	endRangeEntry := widget.NewEntry()
	endRangeEntry.SetPlaceHolder("結束範圍秒數（可選）")

	// 模式切換處理
	modeGroup.OnChanged = func(value string) {
		if value == "批量處理資料夾" {
			fileContainer.Hide()
			dirContainer.Show()
		} else {
			dirContainer.Hide()
			fileContainer.Show()
		}
	}

	// 創建按鈕
	executeBtn := widget.NewButton("執行計算", func() {
		a.executeMaxMeanCalculation(modeGroup.Selected, selectedFilePath, selectedDirPath,
			windowSizeEntry.Text, startRangeEntry.Text, endRangeEntry.Text)
	})
	executeBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("取消", nil)

	// 創建表單佈局
	form := container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("處理模式："),
		modeGroup,
		widget.NewSeparator(),
		widget.NewLabel("檔案選擇："),
		fileContainer,
		dirContainer,
		widget.NewSeparator(),
		widget.NewLabel("計算參數："),
		container.NewGridWithColumns(3,
			widget.NewLabel("窗口大小："),
			widget.NewLabel("開始範圍："),
			widget.NewLabel("結束範圍："),
		),
		container.NewGridWithColumns(3,
			windowSizeEntry,
			startRangeEntry,
			endRangeEntry,
		),
		widget.NewSeparator(),
		container.NewGridWithColumns(2, executeBtn, cancelBtn),
	)

	// 使用Fyne v2現代對話框API
	dlg := dialog.NewCustom("最大平均值計算", "取消", form, a.window)
	dlg.Resize(fyne.NewSize(550, 650))

	// 設置取消按鈕功能
	cancelBtn.OnTapped = func() {
		dlg.Hide()
		a.updateStatus("已取消")
	}

	dlg.Show()
}

// showNormalizationDialog 顯示資料標準化對話框
func (a *App) showNormalizationDialog() {
	a.updateStatus("準備資料標準化...")

	// 創建對話框內容
	title := widget.NewLabel("資料標準化")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// 主要資料檔案選擇
	var mainFilePath string
	mainFileLabel := widget.NewLabel("未選擇主要資料檔案")
	mainFileBtn := widget.NewButton("選擇主要資料檔案", func() {
		a.showFileSelectDialog(&mainFilePath, mainFileLabel)
	})
	mainFileContainer := container.NewHBox(mainFileBtn, mainFileLabel)

	// 參考資料檔案選擇
	var refFilePath string
	refFileLabel := widget.NewLabel("未選擇參考資料檔案")
	refFileBtn := widget.NewButton("選擇參考資料檔案", func() {
		a.showFileSelectDialogWithDir(&refFilePath, refFileLabel, a.config.OperateDir)
	})
	refFileContainer := container.NewHBox(refFileBtn, refFileLabel)

	// 輸出檔名輸入
	outputNameEntry := widget.NewEntry()
	outputNameEntry.SetPlaceHolder("輸出檔案名稱（可選，默認自動生成）")

	// 創建按鈕（先創建，稍後設置功能）
	executeBtn := widget.NewButton("執行標準化", nil)
	executeBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("取消", nil)

	// 創建表單佈局
	form := container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("主要資料檔案："),
		mainFileContainer,
		widget.NewSeparator(),
		widget.NewLabel("參考資料檔案："),
		refFileContainer,
		widget.NewSeparator(),
		widget.NewLabel("輸出設定："),
		outputNameEntry,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, executeBtn, cancelBtn),
	)

	// 使用Fyne v2現代對話框API
	dlg := dialog.NewCustom("資料標準化", "取消", form, a.window)
	dlg.Resize(fyne.NewSize(500, 450))

	// 設置按鈕功能
	executeBtn.OnTapped = func() {
		// 執行標準化，完成後關閉對話框
		a.executeNormalizationWithCallback(mainFilePath, refFilePath, outputNameEntry.Text, func(success bool) {
			if success {
				dlg.Hide()
			}
		})
	}

	cancelBtn.OnTapped = func() {
		dlg.Hide()
		a.updateStatus("已取消")
	}

	dlg.Show()
}

// showPhaseAnalysisDialog 顯示階段分析對話框
func (a *App) showPhaseAnalysisDialog() {
	a.updateStatus("準備階段分析...")

	// 創建對話框內容
	title := widget.NewLabel("階段分析")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// 資料檔案選擇
	var dataFilePath string
	dataFileLabel := widget.NewLabel("未選擇資料檔案")
	dataFileBtn := widget.NewButton("選擇資料檔案", func() {
		a.showFileSelectDialog(&dataFilePath, dataFileLabel)
	})
	dataFileContainer := container.NewHBox(dataFileBtn, dataFileLabel)

	// 階段時間點檔案選擇
	var phaseFilePath string
	phaseFileLabel := widget.NewLabel("未選擇階段時間點檔案")
	phaseFileBtn := widget.NewButton("選擇階段時間點檔案", func() {
		a.showFileSelectDialog(&phaseFilePath, phaseFileLabel)
	})
	phaseFileContainer := container.NewHBox(phaseFileBtn, phaseFileLabel)

	// 階段標籤輸入
	phaseLabelsEntry := widget.NewMultiLineEntry()
	phaseLabelsEntry.SetPlaceHolder("輸入階段標籤，每行一個（例如：\n階段1\n階段2\n階段3）")
	phaseLabelsEntry.Resize(fyne.NewSize(400, 100))

	// 預設階段標籤
	defaultLabels := strings.Join(a.config.PhaseLabels, "\n")
	phaseLabelsEntry.SetText(defaultLabels)

	// 輸出檔名輸入
	outputNameEntry := widget.NewEntry()
	outputNameEntry.SetPlaceHolder("輸出檔案名稱（可選，默認自動生成）")

	// 創建按鈕
	executeBtn := widget.NewButton("執行分析", func() {
		labels := strings.Split(strings.TrimSpace(phaseLabelsEntry.Text), "\n")
		a.executePhaseAnalysis(dataFilePath, phaseFilePath, labels, outputNameEntry.Text)
	})
	executeBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("取消", nil)

	// 創建表單佈局
	form := container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("資料檔案："),
		dataFileContainer,
		widget.NewSeparator(),
		widget.NewLabel("階段時間點檔案："),
		phaseFileContainer,
		widget.NewSeparator(),
		widget.NewLabel("階段標籤："),
		phaseLabelsEntry,
		widget.NewSeparator(),
		widget.NewLabel("輸出設定："),
		outputNameEntry,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, executeBtn, cancelBtn),
	)

	// 使用Fyne v2現代對話框API
	dlg := dialog.NewCustom("階段分析", "取消", form, a.window)
	dlg.Resize(fyne.NewSize(500, 600))

	// 設置取消按鈕功能
	cancelBtn.OnTapped = func() {
		dlg.Hide()
		a.updateStatus("已取消")
	}

	dlg.Show()
}

// showConfigDialog 顯示配置設定對話框
func (a *App) showConfigDialog() {
	a.updateStatus("準備配置設定...")

	// 創建對話框內容
	title := widget.NewLabel("配置設定")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// 縮放因子設定
	scalingFactorEntry := widget.NewEntry()
	scalingFactorEntry.SetText(fmt.Sprintf("%d", a.config.ScalingFactor))
	scalingFactorEntry.SetPlaceHolder("縮放因子")

	// 精度設定
	precisionEntry := widget.NewEntry()
	precisionEntry.SetText(fmt.Sprintf("%d", a.config.Precision))
	precisionEntry.SetPlaceHolder("精度 (0-15)")

	// 輸出格式設定
	outputFormatRadio := widget.NewRadioGroup([]string{"csv", "json", "xlsx"}, nil)
	outputFormatRadio.SetSelected(a.config.OutputFormat)

	// BOM設定
	bomCheck := widget.NewCheck("啟用 UTF-8 BOM", nil)
	bomCheck.SetChecked(a.config.BOMEnabled)

	// 階段標籤設定
	phaseLabelsEntry := widget.NewMultiLineEntry()
	phaseLabelsEntry.SetText(strings.Join(a.config.PhaseLabels, "\n"))
	phaseLabelsEntry.SetPlaceHolder("階段標籤，每行一個")
	phaseLabelsEntry.Resize(fyne.NewSize(580, 80))

	// 目錄設定 - 添加資料夾選擇按鈕
	inputDirEntry := widget.NewEntry()
	inputDirEntry.SetText(a.config.InputDir)
	inputDirEntry.SetPlaceHolder("輸入目錄")

	inputDirBtn := widget.NewButton("瀏覽", func() {
		a.showDirectorySelectDialog(&a.config.InputDir, inputDirEntry)
	})
	inputDirContainer := container.NewBorder(nil, nil, nil, inputDirBtn, inputDirEntry)

	outputDirEntry := widget.NewEntry()
	outputDirEntry.SetText(a.config.OutputDir)
	outputDirEntry.SetPlaceHolder("輸出目錄")

	outputDirBtn := widget.NewButton("瀏覽", func() {
		a.showDirectorySelectDialog(&a.config.OutputDir, outputDirEntry)
	})
	outputDirContainer := container.NewBorder(nil, nil, nil, outputDirBtn, outputDirEntry)

	operateDirEntry := widget.NewEntry()
	operateDirEntry.SetText(a.config.OperateDir)
	operateDirEntry.SetPlaceHolder("操作目錄")

	operateDirBtn := widget.NewButton("瀏覽", func() {
		a.showDirectorySelectDialog(&a.config.OperateDir, operateDirEntry)
	})
	operateDirContainer := container.NewBorder(nil, nil, nil, operateDirBtn, operateDirEntry)

	// 創建按鈕
	saveBtn := widget.NewButton("保存配置", func() {
		a.saveConfiguration(scalingFactorEntry.Text, precisionEntry.Text,
			outputFormatRadio.Selected, bomCheck.Checked, phaseLabelsEntry.Text,
			inputDirEntry.Text, outputDirEntry.Text, operateDirEntry.Text)
	})
	saveBtn.Importance = widget.HighImportance

	resetBtn := widget.NewButton("重置為默認", func() {
		a.resetToDefaults(scalingFactorEntry, precisionEntry,
			outputFormatRadio, bomCheck, phaseLabelsEntry,
			inputDirEntry, outputDirEntry, operateDirEntry)
	})

	cancelBtn := widget.NewButton("取消", nil)

	// 創建滾動容器來解決版面問題
	basicSettings := container.NewVBox(
		widget.NewLabel("基本設定："),
		container.NewGridWithColumns(2,
			widget.NewLabel("縮放因子："), scalingFactorEntry,
			widget.NewLabel("精度："), precisionEntry,
		),
		widget.NewSeparator(),
		widget.NewLabel("輸出格式："),
		outputFormatRadio,
		bomCheck,
	)

	phaseSettings := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabel("階段標籤："),
		phaseLabelsEntry,
	)

	directorySettings := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabel("目錄設定："),
		container.NewVBox(
			container.NewVBox(
				widget.NewLabel("輸入目錄："),
				inputDirContainer,
			),
			container.NewVBox(
				widget.NewLabel("輸出目錄："),
				outputDirContainer,
			),
			container.NewVBox(
				widget.NewLabel("操作目錄："),
				operateDirContainer,
			),
		),
	)

	// 按鈕區域固定在底部
	buttonArea := container.NewVBox(
		widget.NewSeparator(),
		container.NewGridWithColumns(3, saveBtn, resetBtn, cancelBtn),
	)

	// 主要內容區域
	content := container.NewVBox(
		title,
		widget.NewSeparator(),
		basicSettings,
		phaseSettings,
		directorySettings,
	)

	// 創建滾動容器
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(600, 500))

	// 完整佈局
	form := container.NewBorder(nil, buttonArea, nil, nil, scroll)

	// 使用Fyne v2現代對話框API，增大視窗尺寸
	dlg := dialog.NewCustom("配置設定", "關閉", form, a.window)
	dlg.Resize(fyne.NewSize(650, 750))

	// 設置取消按鈕功能
	cancelBtn.OnTapped = func() {
		dlg.Hide()
		a.updateStatus("已取消")
	}

	dlg.Show()
}

// showChartDialog 顯示做圖對話框
func (a *App) showChartDialog() {
	a.updateStatus("準備做圖...")

	// 創建對話框內容
	title := widget.NewLabel("資料做圖")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// CSV檔案選擇
	var filePath string
	fileLabel := widget.NewLabel("未選擇CSV檔案")

	// 列選擇區域（初始為空）
	columnContainer := container.NewVBox()
	columnLabel := widget.NewLabel("請先選擇CSV檔案以顯示可用的列")
	columnContainer.Add(columnLabel)

	// 更新列選擇的函數
	var selectedColumns []string
	updateColumns := func() {
		if filePath == "" {
			return
		}

		// 讀取CSV檔案獲取headers
		records, err := a.csvHandler.ReadCSV(filePath)
		if err != nil {
			a.showError(fmt.Sprintf("讀取CSV檔案失敗: %v", err))
			return
		}

		if len(records) < 1 {
			a.showError("CSV檔案格式無效")
			return
		}

		// 清空之前的內容
		columnContainer.RemoveAll()
		selectedColumns = nil

		// 添加說明
		instruction := widget.NewLabel("選擇要繪製的Y軸數據列（第一列為X軸時間）:")
		columnContainer.Add(instruction)

		// 為每個非時間列創建複選框
		headers := records[0]
		for i := 1; i < len(headers); i++ { // 跳過第一列（時間）
			columnName := headers[i]
			checkBox := widget.NewCheck(columnName, func(checked bool) {
				if checked {
					selectedColumns = append(selectedColumns, columnName)
				} else {
					// 移除取消選中的列
					for j, col := range selectedColumns {
						if col == columnName {
							selectedColumns = append(selectedColumns[:j], selectedColumns[j+1:]...)
							break
						}
					}
				}
			})
			columnContainer.Add(checkBox)
		}

		columnContainer.Refresh()
	}

	selectFileBtn := widget.NewButton("選擇CSV檔案", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			defer reader.Close()

			filePath = reader.URI().Path()
			fileLabel.SetText(filePath)
			a.updateStatus("已選擇檔案: " + filePath)

			// 更新列選擇
			updateColumns()
		}, a.window)

		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))

		if a.config.InputDir != "" {
			inputURI := storage.NewFileURI(a.config.InputDir)
			if listableURI, ok := inputURI.(fyne.ListableURI); ok {
				fileDialog.SetLocation(listableURI)
			}
		}

		fileDialog.Show()
	})

	// 圖表標題輸入
	titleEntry := widget.NewEntry()
	titleEntry.SetPlaceHolder("輸入圖表標題（可選）")

	// 執行按鈕
	previewBtn := widget.NewButton("預覽圖表", func() {
		if filePath == "" {
			a.showError("請選擇CSV檔案")
			return
		}
		if len(selectedColumns) == 0 {
			a.showError("請至少選擇一個數據列")
			return
		}

		chartTitle := titleEntry.Text
		if chartTitle == "" {
			chartTitle = "EMG資料圖表"
		}

		a.executeChartPreview(filePath, selectedColumns, chartTitle)
	})
	previewBtn.Importance = widget.HighImportance

	saveBtn := widget.NewButton("直接保存", func() {
		if filePath == "" {
			a.showError("請選擇CSV檔案")
			return
		}
		if len(selectedColumns) == 0 {
			a.showError("請至少選擇一個數據列")
			return
		}

		chartTitle := titleEntry.Text
		if chartTitle == "" {
			chartTitle = "EMG資料圖表"
		}

		a.executeChartGeneration(filePath, selectedColumns, chartTitle)
	})

	cancelBtn := widget.NewButton("取消", nil)

	// 創建滾動容器用於列選擇
	columnScroll := container.NewScroll(columnContainer)
	columnScroll.SetMinSize(fyne.NewSize(400, 200))

	// 組合表單
	// 創建按鈕區域
	buttonArea := container.NewHBox(
		previewBtn,
		widget.NewLabel(""), // 空白分隔
		saveBtn,
		widget.NewLabel(""), // 空白分隔
		cancelBtn,
	)

	form := container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("CSV檔案:"),
		container.NewBorder(nil, nil, nil, selectFileBtn, fileLabel),
		widget.NewSeparator(),
		widget.NewLabel("數據列選擇:"),
		columnScroll,
		widget.NewSeparator(),
		widget.NewLabel("圖表標題:"),
		titleEntry,
		widget.NewSeparator(),
		container.NewPadded(buttonArea), // 添加邊距避免重疊
	)

	// 創建對話框
	dlg := dialog.NewCustom("做圖", "取消", form, a.window)
	dlg.Resize(fyne.NewSize(600, 500))

	// 設置取消按鈕功能
	cancelBtn.OnTapped = func() {
		dlg.Hide()
		a.updateStatus("已取消")
	}

	dlg.Show()
}

// showChartPreview 顯示圖表預覽視窗
func (a *App) showChartPreview(img image.Image, filePath string, selectedColumns []string, chartTitle string, dataset *models.EMGDataset, config chart.ChartConfig) {
	// 創建交互式圖表小工具
	interactiveChart := NewInteractiveChartWithGenerator(img, dataset, config, a.chartGen)

	// 設置雙擊事件處理（用於縮放）
	interactiveChart.SetOnDoubleClick(func(x, y float64) {
		a.updateStatus(fmt.Sprintf("雙擊位置: 時間 %.3f 秒", x))
	})

	// 創建控制按鈕
	zoomInBtn := widget.NewButton("放大", func() {
		// 使用圖表中心作為縮放點
		size := interactiveChart.Size()
		centerX := size.Width / 2
		interactiveChart.ZoomIn(centerX)
	})

	zoomOutBtn := widget.NewButton("縮小", func() {
		// 使用圖表中心作為縮放點
		size := interactiveChart.Size()
		centerX := size.Width / 2
		interactiveChart.ZoomOut(centerX)
	})

	resetZoomBtn := widget.NewButton("重置縮放", func() {
		interactiveChart.ResetZoom()
		a.updateStatus("已重置縮放")
	})

	// 創建下載按鈕
	downloadBtn := widget.NewButton("下載圖表", func() {
		// 生成輸出檔案名
		baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		outputFileName := fmt.Sprintf("%s_圖表.png", baseName)
		outputPath := filepath.Join(a.config.OutputDir, outputFileName)

		// 保存圖表
		err := a.chartGen.GenerateLineChart(dataset, config, outputPath)
		if err != nil {
			a.showError(fmt.Sprintf("保存圖表失敗: %v", err))
			return
		}

		a.updateStatus("圖表已下載！")
		a.showInfo(fmt.Sprintf("圖表已保存到: %s", outputPath))
	})
	downloadBtn.Importance = widget.HighImportance

	closeBtn := widget.NewButton("關閉", nil)

	// 創建控制按鈕容器
	zoomControls := container.NewGridWithColumns(3, zoomInBtn, zoomOutBtn, resetZoomBtn)
	mainControls := container.NewGridWithColumns(2, downloadBtn, closeBtn)

	// 創建說明標籤
	instructionLabel := widget.NewLabel("操作說明: 雙擊縮放到該點 | 滾輪縮放")
	instructionLabel.Alignment = fyne.TextAlignCenter
	instructionLabel.TextStyle = fyne.TextStyle{Italic: true}

	// 創建圖表容器，直接使用InteractiveChart不包裝在滾動容器中
	chartContainer := container.NewVBox(
		instructionLabel,
		widget.NewSeparator(),
		interactiveChart,
	)

	// 添加控制按鈕區域
	controlArea := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabel("縮放控制:"),
		zoomControls,
		widget.NewSeparator(),
		mainControls,
	)

	// 創建主容器，確保按鈕不會覆蓋圖表
	content := container.NewBorder(nil, controlArea, nil, nil, chartContainer)

	// 創建預覽視窗
	previewWindow := a.app.NewWindow("圖表預覽 - " + chartTitle)
	previewWindow.SetContent(content)
	previewWindow.Resize(fyne.NewSize(900, 700))
	previewWindow.CenterOnScreen()

	// 設置關閉按鈕功能
	closeBtn.OnTapped = func() {
		previewWindow.Close()
	}

	// 當視窗關閉時的處理
	previewWindow.SetOnClosed(func() {
		a.updateStatus("預覽已關閉")
	})

	a.updateStatus("圖表預覽已生成 - 可懸停查看數值，雙擊或滾輪縮放")
	previewWindow.Show()
}
