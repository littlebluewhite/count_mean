package gui

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SelectFile opens a file dialog for file selection.
func (a *App) SelectFile(title string, filters []runtime.FileFilter, buttonType string) (path string, err error) {
	defer recoverHandlerPanic("SelectFile", a.logger, &err)

	cfg := a.state.Load().config

	var defaultDir string

	a.logger.Debug("選擇文件對話框", map[string]any{"buttonType": buttonType})

	switch buttonType {
	case FileTypeInput:
		defaultDir = cfg.InputDir
	case FileTypeOutput:
		defaultDir = cfg.OutputDir
	case FileTypeOperate:
		defaultDir = cfg.OperateDir
	}

	a.logger.Debug("預設目錄", map[string]any{"defaultDir": defaultDir})

	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
		Filters:          filters,
	}

	// Wails Startup 設 a.ctx 之前若 frontend 已啟動並呼叫 RPC 會 nil deref;實務
	// 上極少發生(Wails 在 ctx ready 後才暴露 binding),仍加防線回 graceful error。
	ctx := a.loadCtx()
	if ctx == nil {
		return "", ErrAppNotReady
	}

	file, err := runtime.OpenFileDialog(ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟檔案對話框失敗: %w", err)
	}

	return file, nil
}

// SelectDirectory opens a directory dialog.
func (a *App) SelectDirectory(title string) (path string, err error) {
	defer recoverHandlerPanic("SelectDirectory", a.logger, &err)

	s := a.state.Load()
	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: s.config.InputDir,
	}

	ctx := a.loadCtx()
	if ctx == nil {
		return "", ErrAppNotReady
	}

	dir, err := runtime.OpenDirectoryDialog(ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟目錄對話框失敗: %w", err)
	}

	return dir, nil
}

// ShowMessage displays an informational dialog. Fire-and-forget 無 error 回傳,
// panic 必須在 handler 內 swallow 否則 Wails runtime 整個 process 倒;ctx==nil
// 時直接跳過 MessageDialog(Wails runtime 會把 nil ctx 視為 fatal)。
func (a *App) ShowMessage(title, message string) {
	defer recoverHandlerPanicVoid("ShowMessage", a.logger)

	ctx := a.loadCtx()
	if ctx == nil {
		return
	}

	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowError displays an error dialog (same fire-and-forget pattern as ShowMessage).
func (a *App) ShowError(title, message string) {
	defer recoverHandlerPanicVoid("ShowError", a.logger)

	ctx := a.loadCtx()
	if ctx == nil {
		return
	}

	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:    runtime.ErrorDialog,
		Title:   title,
		Message: message,
	})
}
