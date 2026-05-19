// Package manifest 集中 phase manifest 載入與 EMG 檔案路徑解析的共用流程。
//
// cci 與 muscle_ratio 兩個 analyzer 共用：
//   - 載入分期總檔（manifest CSV）
//   - 把 manifest 內相對檔名解析為 baseFolder 下的絕對路徑、檢查存在性
//
// 路徑解析整合 security.ResolveLenientPath（允許 BTS 匯出含字面 "%" 的檔名），
// 不要 bundle 後續 ParseFile / 迭代 / 錯誤處理 — caller 行為刻意不同（cci 是 fail-fast，
// muscle_ratio 是 per-subject batch）。
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
)

// sentinel errors 讓 caller 可用 errors.Is 區分原因。原本 fmt.Errorf
// 動態字串 caller 只能 strings.Contains 比對，重構時 message 一改就 break。
var (
	ErrManifestEMGPathInvalid = errors.New("EMG 檔案路徑驗證失敗")
	ErrManifestEMGFileMissing = errors.New("EMG 檔案不存在")
)

// LoadManifests 解析分期總檔案，回傳所有 manifest 紀錄。
func LoadManifests(filepath string) ([]models.PhaseManifest, error) {
	return parsers.NewPhaseManifestParser().ParseFile(filepath)
}

// ResolveEMGFile 把 manifest.EMGFile 相對檔名解析為 baseFolder 下的絕對路徑。
// 整合：EvalSymlinks(baseFolder) → security.ResolveLenientPath → os.Stat IsNotExist。
// 錯誤 wraps sentinel ErrManifestEMGPathInvalid / ErrManifestEMGFileMissing，
// caller 可用 errors.Is 區分（路徑驗證失敗 vs. 檔案不存在）。
func ResolveEMGFile(baseFolder, emgFile string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolved
	}

	emgPath, err := security.ResolveLenientPath(baseFolder, emgFile)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrManifestEMGPathInvalid, err)
	}

	if _, err := os.Stat(emgPath); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrManifestEMGFileMissing, emgPath)
	}

	return emgPath, nil
}
