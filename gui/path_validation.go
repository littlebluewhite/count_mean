package gui

import (
	"errors"
	"fmt"
	"strings"

	"count_mean/internal/security"
)

// ErrOddArityValidatorArgs 表示 validateExternalPathInputs 收到 odd-arity caller bug。
//
// 用 static sentinel 而非 fmt.Errorf 動態字串,既滿足 err113 lint 又讓 caller
// 可用 errors.Is 對應(future tooling / audit log 拍照標記)。
var ErrOddArityValidatorArgs = errors.New("validateExternalPathInputs: 參數數量必須為偶數 (programmer error)")

// validateExternalPathInputs runs security.PathValidator.ValidateExternalPath
// against each non-empty path supplied by the caller, returning the first
// validation error wrapped with its human-friendly label. Designed as a single
// boundary check at GUI handler entry so traversal / system-sensitive
// paths (e.g. "../etc/passwd", "/etc/shadow") never reach downstream
// analyzers.
//
// 前端 Wails RPC 是不受信任的邊界 — 即使 file dialog 預期回正常絕對
// 路徑，惡意 / bug client 仍可手動送 ".." 之類字串。Downstream analyzer 內部多
// 半已有 defense-in-depth（cci / muscle_ratio / phase_sync 都已透過
// ResolveLenientPath 或 PathValidator 二次擋），但「邊界提早 reject、回結構化
// 錯誤」對前端 UX 與 audit log 更友善（不會等到中途某個 reader 才報「失敗」）。
//
// 參數設計：variadic (label, path) pair — 各 handler 都有自己的 ManifestFile /
// DataFolder 命名，傳 label 才能讓 error message 對應到具體欄位。空字串路徑
// 直接 skip（由各 handler 自己的「請選擇 XX」哨兵 error 接住）。
//
// 為何用 ValidateExternalPath 而非 ValidateFilePath：GUI file dialog 回來的路徑
// 通常落在 InputDir 白名單外（使用者可選 Desktop / pCloud 等任意目錄）。
// ValidateFilePath 會把它擋掉；ValidateExternalPath 不檢查 allowedBasePaths，
// 只做 traversal / 系統敏感目錄 / 路徑長度等基本檢查，正合需求。
//
// 為何用 DefaultValidator singleton：避免每次 handler 呼叫都建一個新 instance
// （無 base paths 的場景共用即可，內部有 RWMutex 保護）。
//
// 過去版本對 odd-arity 走 panic。雖然在 unit test 階段能抓到 caller bug,
// 但在 production 路徑上 (handlers → recoverHandlerPanic → ErrInternalPanic)
// 會被誤判為「系統 panic 等級事故」並把 panic stack trace 寫進 log,blast radius
// 不必要。改回 fail-fast error,caller 既能拿到結構化錯誤訊息又能讓上游用統一
// failedResult 通道處理 — UI 訊息會清楚指出是「程式 bug」而非「不可預期 crash」,
// 且 audit log 不再含 panic stack。
//
// 若 caller bug 真的把 odd args 帶過來,test (TestValidateExternalPathInputs_OddArityReturnsError)
// 會立即 fail;production 則 fail-safe 退回到「結構化錯誤 + UI 顯示」。
func validateExternalPathInputs(labelPathPairs ...string) error {
	if len(labelPathPairs)%2 != 0 {
		return fmt.Errorf("%w: got %d args", ErrOddArityValidatorArgs, len(labelPathPairs))
	}

	validator := security.DefaultValidator()

	for i := 0; i < len(labelPathPairs); i += 2 {
		label := labelPathPairs[i]
		// gosec G602 false positive：上方已 return error when len%2 != 0，
		// 因此 len 必為偶數、迴圈條件 i < len 確保 i+1 < len 永遠成立。
		path := labelPathPairs[i+1] //nolint:gosec // length is enforced by the odd-arity guard above

		if path == "" {
			continue
		}

		// null byte 注入早於 ValidateExternalPath 攔下。
		// validator 內部的 SanitizePath 會清 \x00 但 ValidateExternalPath 走
		// 的是 validatePathFormat 路徑,沒做 null byte 檢查 — 若 path 是
		// "/legit/foo.csv\x00/etc/passwd",lexical Clean 後變 "/legit/foo.csv"
		// (truncated at null),sensitive prefix 比對失誤,但 caller 後續
		// OpenFile 在某些 OS/locale 下會在 \x00 截斷 → 開到不同檔案。
		//
		// boundary 提早 reject 比 trust 下游 OpenFile 的 null-termination 行為
		// 安全得多;對使用者也是合理的 input contract(GUI file dialog 不可能
		// 回帶 null byte 的合法路徑)。
		if strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("%s 路徑驗證失敗: %w",
				label, security.ErrPathTraversal)
		}

		if err := validator.ValidateExternalPath(path); err != nil {
			return fmt.Errorf("%s 路徑驗證失敗: %w", label, err)
		}
	}

	return nil
}

// validateManifestHandlerParams 是 7 個「manifest + dataFolder」Wails handler 共用
// 的 prelude:先走 strict empty check(manifestFile 先, dataFolder 次, 任一為空
// 即回對應 sentinel),再 delegate 給 validateExternalPathInputs 跑 traversal /
// sensitive prefix / null byte / 超長 path 等 boundary 驗證,label 固定為
// 「分期總檔案」/「資料夾」。
//
// 設計成 pure function(無 *App / *appState receiver) — boundary 驗證本身不依賴
// app state,純函式更好測、caller 也不必持 App instance 才能驗 input。
func validateManifestHandlerParams(manifestFile, dataFolder string) error {
	if manifestFile == "" {
		return ErrNoManifestFile
	}
	if dataFolder == "" {
		return ErrNoDataFolder
	}
	return validateExternalPathInputs(
		"分期總檔案", manifestFile,
		"資料夾", dataFolder,
	)
}
