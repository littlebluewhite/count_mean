package gui

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/logging"
)

// validPNGBase64 回傳一個最小但合法 PNG 的 base64 字串（不含 dataURL 前綴），
// reuse png_validation_test.go 的 validPNGBytes（同 package 可見）。caller 自行
// 視需要在前面拼 pngDataURLPrefix。
func validPNGBase64(width, height uint32) string {
	return base64.StdEncoding.EncodeToString(validPNGBytes(width, height))
}

// setupDownloadValidatedPNGTestApp 構造僅含 logger 的最小 App。
// downloadValidatedPNG 不讀 app.state（OutputPath 由 caller 直接給），只需 logger
// 不為 nil 以免後續若加入 log 行造成 nil-deref。對齊 sibling
// setupChartComposerTestApp 的最小化精神。
func setupDownloadValidatedPNGTestApp(t *testing.T) *App {
	t.Helper()

	app := &App{
		logger: logging.GetLogger("png_download_test"),
	}
	return app
}

// TestDownloadValidatedPNG_RejectsBadPrefix 釘住第一道守門:imageData 缺
// "data:image/png;base64," 前綴時，直接回 ErrInvalidImageFormat（不 wrap），
// 對齊 DownloadCCIChart / DownloadChartComposerImage 既有行為。
func TestDownloadValidatedPNG_RejectsBadPrefix(t *testing.T) {
	app := setupDownloadValidatedPNGTestApp(t)

	result, err := app.downloadValidatedPNG("not-a-data-url", "/tmp/whatever.png")

	require.Error(t, err, "缺 PNG dataURL 前綴必須 reject")
	assert.Nil(t, result, "格式錯誤時不應回 result")
	assert.True(t, errors.Is(err, ErrInvalidImageFormat),
		"reject reason 應為 ErrInvalidImageFormat（不 wrap），got %v", err)
}

// TestDownloadValidatedPNG_RejectsInvalidPNGPayload 釘住第二道守門:前綴正確
// 但 strip 後 payload 不是合法 base64 / PNG 時，回的 error 必須帶
// "PNG 驗證失敗" 前綴（DecodeAndValidatePNG 錯誤被 fmt.Errorf wrap）。
func TestDownloadValidatedPNG_RejectsInvalidPNGPayload(t *testing.T) {
	app := setupDownloadValidatedPNGTestApp(t)

	// 前綴正確，但後面接的是不可解析的 base64 — DecodeAndValidatePNG 必失敗。
	result, err := app.downloadValidatedPNG(pngDataURLPrefix+"!!! not base64 !!!", "/tmp/whatever.png")

	require.Error(t, err, "前綴正確但 payload 非法 PNG 必須 reject")
	assert.Nil(t, result, "PNG 驗證失敗時不應回 result")
	assert.Contains(t, err.Error(), "PNG 驗證失敗",
		"PNG decode/validate 失敗應 wrap 成「PNG 驗證失敗」，got %v", err)
}

// TestDownloadValidatedPNG_RejectsSensitivePath_NoDoubleLabel 釘住第三道守門:
// 輸出路徑落在系統敏感目錄時，由 validateExternalPathInputs reject，且 helper
// 不再外層 re-wrap → label "PNG 輸出路徑" 在錯誤訊息中最多出現一次。
//
// 跨平台規則:用「字串樣本」/etc/passwd（normalizePath 後在所有 OS 都命中
// "/etc/" 敏感樣式）而非 t.TempDir()，確保 mac/linux/windows CI 都驗得到
// sensitive-path rejection。
func TestDownloadValidatedPNG_RejectsSensitivePath_NoDoubleLabel(t *testing.T) {
	app := setupDownloadValidatedPNGTestApp(t)

	// 合法 PNG dataURL 走過 decode → 讓 reject reason 一定是 path validation。
	imageData := pngDataURLPrefix + validPNGBase64(1, 1)

	result, err := app.downloadValidatedPNG(imageData, "/etc/passwd")

	require.Error(t, err, "系統敏感路徑必須被 boundary 擋下")
	assert.Nil(t, result, "path validation 失敗時不應回 result")

	msg := err.Error()
	t.Logf("downloadValidatedPNG 敏感路徑錯誤訊息: %q", msg)

	// label "PNG 輸出路徑" 不可重複（雙重 wrap 會產生重複 label 的 bug）。
	occurrences := strings.Count(msg, "PNG 輸出路徑")
	assert.LessOrEqual(t, occurrences, 1,
		"「PNG 輸出路徑」label 最多出現一次（不可雙重 wrap），got %d 次: %q", occurrences, msg)

	// 仍須帶 path validation 語義 token，audit log 可定位。
	assert.Contains(t, msg, "路徑驗證失敗", "錯誤仍須指出失敗類別")
}

// TestDownloadValidatedPNG_RejectsLeafSymlink 釘住第四道守門:輸出路徑本身是
// 一個 symlink 時，fsperm.WriteFileNoFollow（O_NOFOLLOW）必須 reject，且
// symlink 指向的目標檔案不可被覆寫。鏡像 fsperm/symlink_test.go 的設定，含
// Windows symlink 權限 skip guard。
func TestDownloadValidatedPNG_RejectsLeafSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires Developer Mode / admin; skipping leaf-symlink boundary test")
	}

	app := setupDownloadValidatedPNGTestApp(t)

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "victim.png")
	require.NoError(t, os.WriteFile(target, []byte("untouched"), 0o600), "setup: 寫 victim 檔失敗")

	symlinkPath := filepath.Join(tempDir, "out.png")
	require.NoError(t, os.Symlink(target, symlinkPath), "setup: 建 symlink 失敗")

	imageData := pngDataURLPrefix + validPNGBase64(1, 1)

	result, err := app.downloadValidatedPNG(imageData, symlinkPath)

	require.Error(t, err, "leaf symlink 輸出路徑必須被 WriteFileNoFollow 擋下")
	assert.Nil(t, result, "symlink 寫入失敗時不應回 result")
	assert.Contains(t, err.Error(), "保存圖片失敗",
		"WriteFileNoFollow 失敗應 wrap 成「保存圖片失敗」，got %v", err)

	// 核心安全保證:symlink 指向的目標檔案不可被覆寫。
	got, readErr := os.ReadFile(target)
	require.NoError(t, readErr, "verify: 讀 victim 檔失敗")
	assert.Equal(t, "untouched", string(got),
		"symlink 目標檔被覆寫了 — O_NOFOLLOW 未生效")
}

// TestDownloadValidatedPNG_HappyPath 釘住成功路徑:合法 PNG dataURL + 合法輸出
// 路徑 → 把 decode 後的 PNG bytes 寫到該路徑，回 ChartResult{OutputPath, Success,
// Message}。檔案內容必須等於 decode 後的 PNG bytes（reuse validPNGBytes fixture）。
func TestDownloadValidatedPNG_HappyPath(t *testing.T) {
	app := setupDownloadValidatedPNGTestApp(t)

	outputPath := filepath.Join(t.TempDir(), "chart.png")
	pngBytes := validPNGBytes(1, 1)
	imageData := pngDataURLPrefix + base64.StdEncoding.EncodeToString(pngBytes)

	result, err := app.downloadValidatedPNG(imageData, outputPath)

	require.NoError(t, err, "合法 PNG + 合法路徑必須成功")
	require.NotNil(t, result)
	assert.Equal(t, outputPath, result.OutputPath)
	assert.True(t, result.Success)
	assert.Equal(t, "圖表已下載至: "+outputPath, result.Message)

	// 檔案真的被寫出，且內容等於 decode 後的 PNG bytes。
	written, readErr := os.ReadFile(outputPath)
	require.NoError(t, readErr, "PNG 必須真的被寫入 disk")
	assert.Equal(t, pngBytes, written, "寫入的 bytes 必須等於 decode 後的 PNG bytes")
}
