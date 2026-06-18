package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/errors"
	"count_mean/internal/io"
	"count_mean/internal/maxmean"
	"count_mean/internal/models"
)

// calculateMaxMeanBatch 批次處理資料夾中的所有CSV檔案.
func (a *App) calculateMaxMeanBatch(params MaxMeanParams) (*MaxMeanResult, error) {
	s := a.state.Load()

	if params.InputPath == "" {
		return nil, fmt.Errorf("目錄路徑驗證失敗: %w",
			errors.NewValidationError("directory_path", params.InputPath, "目錄路徑不能為空"))
	}

	source, outputDirName, err := a.buildMaxMeanFileSource(s, params.InputPath)
	if err != nil {
		return nil, err
	}

	writer := &maxMeanResultWriter{csvHandler: s.csvHandler, subDir: outputDirName}

	res, err := maxmean.RunBatch(a.context(), s.maxMeanCalc, source, writer,
		maxmean.BatchParams{WindowSize: params.WindowSize, StartTime: params.StartTime, EndTime: params.EndTime})
	if err != nil {
		return nil, err
	}

	return &MaxMeanResult{
		OutputPath: filepath.Join(s.config.OutputDir, outputDirName),
		Headers:    res.Headers,
		Results:    convertMaxMeanResultsToArray(res.Results, s.config.ScalingFactor),
		Success:    res.SuccessCount > 0,
		Message:    fmt.Sprintf("批次處理完成：成功 %d 個檔案，失敗 %d 個檔案", res.SuccessCount, res.FailCount),
	}, nil
}

// buildMaxMeanFileSource 決定 mode(internal-dir vs external)並建立對應的 FileSource,
// 同時回傳 outputDirName 供 writer SubDir 與 envelope OutputPath 共用。
func (a *App) buildMaxMeanFileSource(s *appState, inputPath string) (maxmean.FileSource, string, error) {
	if !filepath.IsAbs(inputPath) {
		// 相對路徑 → configured InputDir 下
		return &dirFileSource{csvHandler: s.csvHandler, dirName: inputPath}, filepath.Base(inputPath), nil
	}

	relPath, err := filepath.Rel(s.config.InputDir, inputPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		// 外部絕對路徑
		return &externalFileSource{csvHandler: s.csvHandler, dirPath: inputPath}, filepath.Base(inputPath), nil
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
		return nil, fmt.Errorf("列出CSV文件失敗: %w", err)
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

// externalFileSource 是 external absolute-path adapter:用 filepath.Glob + ReadCSVExternal。
type externalFileSource struct {
	csvHandler *io.CSVHandler
	dirPath    string
}

func (e *externalFileSource) Discover() ([]maxmean.BatchFile, error) {
	paths, err := filepath.Glob(filepath.Join(e.dirPath, "*.csv"))
	if err != nil {
		return nil, fmt.Errorf("搜尋CSV文件失敗: %w", err)
	}

	files := make([]maxmean.BatchFile, len(paths))
	for i, p := range paths {
		fp := p
		files[i] = maxmean.BatchFile{
			Name: TrimCSVExtension(filepath.Base(fp)),
			Read: func() ([][]string, error) { return e.csvHandler.ReadCSVExternal(fp) },
		}
	}

	return files, nil
}

// maxMeanResultWriter 是 ResultWriter production adapter:綁 SubDir,包 WriteMaxMean。
type maxMeanResultWriter struct {
	csvHandler *io.CSVHandler
	subDir     string
}

func (w *maxMeanResultWriter) Write(name string, headers []string, results []models.MaxMeanResult, startRange, endRange float64) (string, error) {
	outputFile := buildOutputFilename(name, SuffixMaxMean)
	return w.csvHandler.WriteMaxMean(io.WriteRequest{Filename: outputFile, SubDir: w.subDir}, headers, results, startRange, endRange)
}
