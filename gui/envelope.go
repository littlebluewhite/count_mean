package gui

import (
	"count_mean/internal/i18n"
	"count_mean/internal/security/redact"
)

// [[Webview envelope]] 的 Message 通道(ADR-0036):handler 把可預期失敗包進
// result 字串欄位(Message、MR SubjectDTO.Error、Composer MissingFileDTO.ErrMessage)
// 時,只能經這三個 helper 產生文字。err 通道的出口在 recover.go
// (recoverHandlerPanic → redactForWebview)。
//
// 兩條通道都只做 sink-side redact.Paths:目錄段換成 `<redacted-path>/`,**末段
// (basename)保留**。錯誤若以病患資料夾名結尾(例如 DataFolder 本身不存在),
// 那一段仍會出現 —— source-side 的 fsperm redactBasePaths 因此保留。
//
// i18n 規則:handler 層 localize(failMessage 的前綴),analyzer 只回 error /
// sentinel;cci / muscle_ratio / phase_sync 內部既有的 i18n 字串留待後續 wave 遷移。
//
// AST 守門(app_panic_ast_test.go):failed*Result 的引數只能是字串字面值、
// failMessage(...) 或 inputMessage(...);只有本檔與 recover.go 能 import redact。

// failMessage 建構可預期失敗(下游 analyzer / IO / 計算錯誤)的 Message:
// i18n.T(key) + ": " + redact 後的 err 文字,並以 a.logger.Error 記一次
// (handler 分支不另打 Error log)。log 以 key 為訊息,不隨 locale 變動。
func (a *App) failMessage(key string, err error) string {
	a.logger.Error(key, err)

	return i18n.T(key) + ": " + redact.RedactForMessage(err)
}

// inputMessage 建構驗證失敗(使用者輸入 sentinel,例如 ErrNoManifestFile、
// 路徑驗證失敗)的 Message:只 redact、不加前綴、不 log —— 使用者輸入問題不是
// 系統錯誤。
func inputMessage(err error) string {
	return redact.RedactForMessage(err)
}

// redactText 給 Message 以外的 webview 文字欄位過 redact、不 log:muscle_ratio
// SubjectResult.Error(analyzer 回的字串)、Composer MissingFileDTO.ErrMessage
// (non-blocking 的缺檔清單,不是失敗)。
func redactText(s string) string {
	return redact.Paths(s)
}
