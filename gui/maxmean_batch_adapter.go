package gui

import (
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/maxmean"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// calculateMaxMeanBatch 批次處理資料夾中的所有CSV檔案.
func (a *App) calculateMaxMeanBatch(params MaxMeanParams) (*MaxMeanResult, error) {
	s := a.state.Load()

	if params.InputPath == "" {
		return nil, i18n.WrapError(i18n.NewError(i18n.KeyErrorHandlerDirPathEmpty), i18n.KeyErrorHandlerDirPathInvalid)
	}

	source, outputDirName, err := a.buildMaxMeanFileSource(s, params.InputPath)
	if err != nil {
		return nil, err
	}

	writer := &maxMeanResultWriter{csvHandler: s.csvHandler, subDir: outputDirName}

	res, err := maxmean.RunBatch(a.context(), s.maxMeanCalc, source, writer,
		maxmean.BatchParams{
			WindowSize: params.WindowSize,
			Range:      calculator.MaxMeanRange{Start: params.StartTime, End: params.EndTime},
		})
	if err != nil {
		return nil, err
	}

	// OutputPath 取自最後一次實際寫入的路徑(placement 的回傳值),不在 gui 重組;
	// 全部失敗時沒有寫出任何檔,給空字串。
	outputPath := ""
	if res.SuccessCount > 0 {
		outputPath = filepath.Dir(writer.last)
	}

	return &MaxMeanResult{
		OutputPath: outputPath,
		Success:    res.SuccessCount > 0,
		Message:    i18n.T(i18n.KeyStatusMaxMeanBatchSummary, res.SuccessCount, res.FailCount),
	}, nil
}

// buildMaxMeanFileSource 決定 mode(internal-dir vs external)並建立對應的 FileSource,
// 同時回傳 outputDirName 供 writer 的 SubDir(envelope OutputPath 取自實際寫入路徑)。
func (a *App) buildMaxMeanFileSource(s *appState, inputPath string) (maxmean.FileSource, string, error) {
	if !filepath.IsAbs(inputPath) {
		// 相對路徑 → configured InputDir 下
		return &dirFileSource{csvHandler: s.csvHandler, dirName: inputPath}, filepath.Base(inputPath), nil
	}

	if !fsperm.IsWithin(s.config.InputDir, inputPath) {
		// 外部絕對路徑
		return &externalFileSource{csvHandler: s.csvHandler, dirPath: inputPath}, filepath.Base(inputPath), nil
	}

	// IsWithin 為 true 蘊含 Rel 成功。
	relPath, err := filepath.Rel(s.config.InputDir, inputPath)
	if err != nil {
		return nil, "", i18n.WrapError(err, i18n.KeyErrorHandlerRelPathFailed)
	}

	// 絕對路徑但在 InputDir 下
	return &dirFileSource{csvHandler: s.csvHandler, dirName: relPath}, filepath.Base(relPath), nil
}

// dirFileSource 是 configured input-dir adapter:用 ListCSVFilesInDirectory + ReadCSVFromDirectory。
type dirFileSource struct {
	csvHandler *io.CSVHandler
	dirName    string
}

func (d *dirFileSource) Discover() ([]maxmean.BatchFile, error) {
	names, err := d.csvHandler.ListCSVFilesInDirectory(d.dirName)
	if err != nil {
		return nil, i18n.WrapError(err, i18n.KeyErrorHandlerListCSVFailed)
	}

	files := make([]maxmean.BatchFile, len(names))
	for i, name := range names {
		n := name
		files[i] = maxmean.BatchFile{
			Name: TrimCSVExtension(n),
			Read: func() ([][]string, error) { return d.csvHandler.ReadCSVFromDirectory(d.dirName, n) },
		}
	}

	return files, nil
}

// externalFileSource 是 external absolute-path adapter:用 filepath.Glob + ReadCSV。
type externalFileSource struct {
	csvHandler *io.CSVHandler
	dirPath    string
}

func (e *externalFileSource) Discover() ([]maxmean.BatchFile, error) {
	paths, err := filepath.Glob(filepath.Join(e.dirPath, "*.csv"))
	if err != nil {
		return nil, i18n.WrapError(err, i18n.KeyErrorHandlerGlobCSVFailed)
	}

	files := make([]maxmean.BatchFile, len(paths))
	for i, p := range paths {
		fp := p
		files[i] = maxmean.BatchFile{
			Name: TrimCSVExtension(filepath.Base(fp)),
			Read: func() ([][]string, error) { return e.csvHandler.ReadCSV(fp) },
		}
	}

	return files, nil
}

// maxMeanResultWriter 是 ResultWriter production adapter:綁 SubDir,包 WriteMaxMean。
// last 記住最後一次成功寫入的路徑,供 envelope 的 OutputPath(RunBatch 逐檔循序呼叫 Write)。
type maxMeanResultWriter struct {
	csvHandler *io.CSVHandler
	subDir     string
	last       string
}

func (w *maxMeanResultWriter) Write(name string, headers []string, results []models.MaxMeanResult, startRange, endRange float64) (string, error) {
	outputFile := buildOutputFilename(name, SuffixMaxMean)
	path, err := w.csvHandler.WriteMaxMean(io.WriteRequest{Filename: outputFile, SubDir: w.subDir}, headers, results, startRange, endRange)
	if err != nil {
		return "", err
	}
	w.last = path

	return path, nil
}
