package gui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUIError_ErrorReturnsMessage — tracer bullet。
//
// 為什麼這是 tracer bullet：
// GUI handler 透過 failedXxxResult(runErr.Error()) 把 err 字面值直接放進
// result.Message 送 Wails 前端。整條路徑成立的前提就是 UIError.Error()
// 必須回傳 caller 傳入的 message — 沒這條,後面 Is/sentinel 全部空轉。
func TestUIError_ErrorReturnsMessage(t *testing.T) {
	sentinel := errors.New("test sentinel — never read in this case")
	const userMessage = "分析失敗:/redacted/path"

	err := newUIError(sentinel, userMessage)

	assert.Equal(t, userMessage, err.Error(),
		"Error() 必須回傳 newUIError 傳入的 message — 既有 failedXxxResult(runErr.Error()) 契約依賴這條")
}

// TestUIError_IsMatchesItsSentinel — sentinel pattern 核心能力。
//
// errors.Is 預設沿 Unwrap chain 走或用 == 比對 — UIError 必須提供 Is(target)
// 讓上層能識別「這個 err 屬於哪種失敗類別」(err113 sentinel pattern 的目的)。
func TestUIError_IsMatchesItsSentinel(t *testing.T) {
	sentinel := errors.New("muscle ratio analysis failed")
	err := newUIError(sentinel, "分析失敗:/redacted")

	assert.True(t, errors.Is(err, sentinel),
		"errors.Is(uiErr, sentinel) 必須 true — 沒這條,sentinel 就是空殼,err113 設計目的歸零")
}

// TestUIError_IsDoesNotMatchDifferentSentinel — sentinel 區分能力守門 test。
//
// Cycle 2 證明「sentinel 自己 match 自己」,本 test 證明「sentinel 不 match
// 別人」。負面 case 必須單獨守 — 否則未來若有人把 Is() 改成 always return
// true,Cycle 2 仍綠,但區分能力悄然消失。
func TestUIError_IsDoesNotMatchDifferentSentinel(t *testing.T) {
	cciSentinel := errors.New("CCI analysis failed")
	csvSentinel := errors.New("CCI CSV export failed")

	err := newUIError(cciSentinel, "分析失敗:/redacted")

	assert.False(t, errors.Is(err, csvSentinel),
		"errors.Is 對 different sentinel 必須 false — sentinel 區分能力歸零等於設計失敗")
}
