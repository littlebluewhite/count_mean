// Package config provides configuration management for the EMG data analysis
// application, including loading, saving, and validating application settings.
package config

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
)

// Configuration validation errors.
var (
	errScalingFactorInvalid = stderrors.New("縮放因子必須大於 0")
	errPhaseLabelsEmpty     = stderrors.New("階段標籤不能為空")
	errPrecisionOutOfRange  = stderrors.New("精度必須在 0-15 之間")
	errUnsupportedFormat    = stderrors.New("不支援的輸出格式")
	errInputDirEmpty        = stderrors.New("輸入目錄路徑不能為空")
	errOutputDirEmpty       = stderrors.New("輸出目錄路徑不能為空")
	errOperateDirEmpty      = stderrors.New("操作目錄路徑不能為空")
	errUnsupportedLanguage  = stderrors.New("不支援的語言")
)

// AppConfig 應用程式配置.
type AppConfig struct {
	ScalingFactor int      `json:"scalingFactor"`
	PhaseLabels   []string `json:"phaseLabels"`
	Precision     int      `json:"precision"`
	OutputFormat  string   `json:"outputFormat"`
	BOMEnabled    bool     `json:"bomEnabled"`
	InputDir      string   `json:"inputDir"`
	OutputDir     string   `json:"outputDir"`
	OperateDir    string   `json:"operateDir"`

	// 日誌配置
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error
	LogFormat    string `json:"logFormat"`    // text, json
	LogDirectory string `json:"logDirectory"` // 日誌目錄

	// 國際化配置
	Language        string `json:"language"`        // zh-TW, zh-CN, en-US, ja-JP
	TranslationsDir string `json:"translationsDir"` // 翻譯文件目錄
}

// DefaultConfig 返回默認配置.
func DefaultConfig() *AppConfig {
	return &AppConfig{
		ScalingFactor: 10,
		PhaseLabels: []string{
			"啟跳下蹲階段",
			"啟跳上升階段",
			"團身階段",
			"下降階段",
		},
		Precision:    10,
		OutputFormat: "csv",
		BOMEnabled:   true,
		InputDir:     "./input",
		OutputDir:    "./output",
		OperateDir:   "./operate",

		// 預設日誌配置
		LogLevel:     "info",
		LogFormat:    "text",
		LogDirectory: "./logs",

		// 預設國際化配置
		Language:        "zh-TW",
		TranslationsDir: "./translations",
	}
}

// LoadConfig 從檔案載入配置.
//
// Windows 上 fsperm.ReadFlags 不含 O_NOFOLLOW (標準 syscall 無等價物),
// 過去這條路徑沒任何 symlink / reparse-point 防護 — 攻擊者把 config.json 換成
// symlink → /etc/passwd 等敏感檔,kernel 跟著解析。
//
// 修法:open 前以 os.Lstat 顯式檢查 filename 本身是不是 symlink / reparse point,
// 是就 reject。這條 check 在三平台都跑,Windows 由此獲得與 Unix O_NOFOLLOW
// 等價的 leaf-symlink 防護 (caller-side defense,non-atomic 但實務足夠 —
// Windows 建立 reparse point 需 SeCreateSymbolicLinkPrivilege,unprivileged
// 攻擊者通常無權植入)。Unix 則是 double-protection:Lstat 先擋 + O_NOFOLLOW
// 再擋。
//
// 為何不用 fsperm.OpenReadValidated:該 helper 需要 basePaths 來定義「allowed
// 範圍」,但 LoadConfig 接受任意 caller-supplied path (預設 "./config.json",
// 但 CLI / GUI 可能傳絕對路徑),沒有預先定義的 allowed-base list。用 filename
// 自己的 parent dir 作 base 在 parent 也是 symlink 的情況下 matchAnyBase 會
// 同時 resolve 兩側到同一 target,等於沒檢查 — 不如直接 leaf-symlink-rejection
// 簡單可靠。
//
// File-not-exist 行為與舊版一致:errors.Is(err, fs.ErrNotExist) 穿透 wrapping,
// 仍會 fall back 到 DefaultConfig — caller 對「config.json 不存在」的契約不變。
func LoadConfig(filename string) (*AppConfig, error) {
	// Leaf symlink rejection — Windows 對齊 Unix O_NOFOLLOW 的最關鍵防護。
	// os.Lstat 不 follow symlink,看到 ModeSymlink / ModeIrregular (Windows reparse
	// point 屬此類) 直接 reject。File-not-exist 走原本 fallback-to-DefaultConfig
	// 路徑;Lstat 其他錯誤 (perm denied 等) 一併包成「無法開啟配置檔案」。
	if info, statErr := os.Lstat(filename); statErr == nil {
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			//nolint:err113 // user-facing dynamic error
			return nil, fmt.Errorf("無法開啟配置檔案: 配置路徑 %s 為 symlink 或 reparse point,已拒絕讀取以防 path-redirection 攻擊", filename)
		}
	} else if !stderrors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("無法開啟配置檔案: %w", statErr)
	}

	file, err := os.OpenFile(filename, fsperm.ReadFlags, 0) //nolint:gosec // filename validated via Lstat above; Unix fsperm.ReadFlags additionally carries O_NOFOLLOW (atomic)
	if err != nil {
		// errors.Is(err, fs.ErrNotExist) 穿透 PathError wrap 偵測 ENOENT。
		if stderrors.Is(err, fs.ErrNotExist) {
			// 如果檔案不存在，返回默認配置
			return DefaultConfig(), nil
		}

		return nil, fmt.Errorf("無法開啟配置檔案: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	// 用 default merge pattern — 先載 DefaultConfig，再 JSON unmarshal
	// 覆蓋。新版加入的欄位若舊版 config.json 省略，預設值不會被 zero-value 吃掉。
	// 過去只對 language 一個欄位補 default（codex review P2 fix），現在通用化。
	config := *DefaultConfig()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("解析配置檔案失敗: %w", err)
	}

	// language 欄位被 unmarshal 後若為空字串（JSON 寫了 ""），補回 default —
	// 保留既有行為防止 Validate 因空 locale 失敗。
	if config.Language == "" {
		config.Language = string(i18n.LocaleZhTW)
	}

	// 驗證配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置檔案無效: %w", err)
	}

	return &config, nil
}

// SaveConfig 保存配置到檔案.
//
// 這條 path 保留給既有 caller (test fixture / migration) 使用,生產
// caller (GUI App.SaveConfig) 改走 SaveConfigAtomic — 後者自動建立 parent dir
// 且做 atomic tmp+rename,擋掉「中途 crash 留下半寫的 config.json」與
// 「parent dir 不存在直接 OpenFile fail」兩個常見坑。
func (c *AppConfig) SaveConfig(filename string) error {
	//nolint:gosec // filename provided by caller
	file, err := os.OpenFile(filename, fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("無法創建配置檔案: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("保存配置檔案失敗: %w", err)
	}

	return nil
}

// SaveConfigAtomic 將 config 以 atomic tmp+rename 方式寫到 filename,並在
// parent 目錄不存在時自動 MkdirAll。
//
// 設計動機:
//
//   - 過去 GUI 走 hardcoded "./config.json" 寫,在 macOS .app bundle 啟動時
//     CWD=/ 落在無權限位置,寫不到 + 靜默 fail,使用者修了設定重啟發現「沒存到」。
//   - 走 ResolveDefaultConfigPath 拿到 "~/Library/Application Support/count_mean/
//     config.json" 之類深層路徑時,parent 目錄第一次寫之前可能不存在;不先 MkdirAll
//     OpenFile 就 ENOENT。
//   - 直接 OpenFile + Encode 不是 atomic — 寫到一半 process crash 會留下 partial
//     JSON,下次 LoadConfig 解析失敗回 default,使用者修的設定被吃掉。
//
// 流程:
//  1. MkdirAll(filepath.Dir(filename), DirPerm) — parent 路徑保證可寫
//  2. 在同一 parent 下開 tmp file `<basename>.tmp.<rand>`,OEXCL 避免撞名
//  3. JSON encode 進 tmp,fsync 後 close
//  4. os.Rename(tmp, filename) — POSIX atomic rename(同 mount 內)
//  5. 任一步驟失敗都 best-effort os.Remove(tmp),原 filename 不變動
//
// 與 csvutil.WriteCSVAtomic 同 pattern,但這裡 payload 是 JSON 而非 CSV,
// 不複用 csvutil 而是自己寫一條 — 對 config 來說 30 行 self-contained 比 拉 csvutil
// 還省事(csvutil 帶 BOM / CSV header / sanitize 等不適用於 JSON 的邏輯)。
func (c *AppConfig) SaveConfigAtomic(filename string) error {
	if filename == "" {
		//nolint:err113 // user-facing dynamic error
		return fmt.Errorf("SaveConfigAtomic: filename 不可為空")
	}

	// Parent dir 自動建立 — 使用 fsperm.DirPerm (0755) 保持與 EnsureDirectories
	// 一致。第一次啟動沒這條,寫進 ~/Library/Application Support/count_mean/
	// 會 ENOENT。
	parent := filepath.Dir(filename)
	if err := os.MkdirAll(parent, fsperm.DirPerm); err != nil {
		return fmt.Errorf("無法建立 config parent 目錄 %s: %w", parent, err)
	}

	// Atomic write:tmp + rename。tmp filename 加上 process PID 後綴避開常見
	// `.tmp` 撞名(同個程式併發兩次 SaveConfig 極不可能,但仍把 entropy 給足)。
	// 用 OEXCL 確保 tmp 真的是新建立的,撞名 attacker plant 也擋掉。
	tmp := filename + ".tmp"

	//nolint:gosec // filename validated by caller; tmp in same dir as final
	tmpFile, err := os.OpenFile(tmp, fsperm.TmpCreateFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("建立 tmp config 檔失敗: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp) //nolint:errcheck // best-effort cleanup of orphan tmp
		}
	}()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")

	if encodeErr := encoder.Encode(c); encodeErr != nil {
		_ = tmpFile.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("config encode 失敗: %w", encodeErr)
	}

	if syncErr := tmpFile.Sync(); syncErr != nil {
		_ = tmpFile.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("config fsync 失敗: %w", syncErr)
	}

	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("關閉 tmp config 檔失敗: %w", closeErr)
	}

	if renameErr := os.Rename(tmp, filename); renameErr != nil {
		return fmt.Errorf("rename tmp → config 失敗: %w", renameErr)
	}

	committed = true
	return nil
}

// Validate 驗證配置.
func (c *AppConfig) Validate() error {
	if c.ScalingFactor <= 0 {
		return errScalingFactorInvalid
	}

	if len(c.PhaseLabels) == 0 {
		return errPhaseLabelsEmpty
	}

	const maxPrecision = 15
	if c.Precision < 0 || c.Precision > maxPrecision {
		return errPrecisionOutOfRange
	}

	validFormats := map[string]bool{
		"csv":  true,
		"json": true,
		"xlsx": true,
	}

	if !validFormats[c.OutputFormat] {
		return fmt.Errorf("%w: %s", errUnsupportedFormat, c.OutputFormat)
	}

	validLanguages := map[string]bool{
		string(i18n.LocaleZhTW): true,
		string(i18n.LocaleZhCN): true,
		string(i18n.LocaleEnUS): true,
		string(i18n.LocaleJaJP): true,
	}

	if !validLanguages[c.Language] {
		return fmt.Errorf("%w: %s", errUnsupportedLanguage, c.Language)
	}

	// 驗證目錄路徑
	if c.InputDir == "" {
		return errInputDirEmpty
	}

	if c.OutputDir == "" {
		return errOutputDirEmpty
	}

	if c.OperateDir == "" {
		return errOperateDirEmpty
	}

	// Traversal / system-dir / null-byte 驗證 — 拒絕 "../../etc"、"/etc"、含 \x00 等。
	// security.PathValidator.ValidateExternalPath 自動把相對路徑（如 "./output"）以 cwd
	// 展開為絕對路徑後再檢，因此預設 config 不會誤拒。InputDir / OutputDir / OperateDir /
	// LogDirectory / TranslationsDir 五個 user-controllable directory 都一併保護。
	//
	// 用 process-wide singleton 避免每次 Validate 都重建 instance — 對於
	// allowedBasePaths == nil 的 default 設定所有 caller 共用，singleton 內部 mutex
	// 已 thread-safe。
	validator := security.DefaultValidator()

	dirFields := []struct {
		name string
		path string
	}{
		{"InputDir", c.InputDir},
		{"OutputDir", c.OutputDir},
		{"OperateDir", c.OperateDir},
		{"LogDirectory", c.LogDirectory},
		{"TranslationsDir", c.TranslationsDir},
	}

	for _, f := range dirFields {
		if f.path == "" {
			continue // LogDirectory / TranslationsDir 容許空字串（非必填）
		}

		if strings.ContainsRune(f.path, 0) {
			//nolint:err113 // dynamic error for user-facing output
			return fmt.Errorf("%s 含 null byte", f.name)
		}

		// Directory 語意：OutputDir 等是「未來寫入路徑的 prefix」，附 dummy child 後再驗
		// 確保 OutputDir 本身就是 system root（"/etc"）也被擋 — performBasicSecurityChecks
		// 的 sensitive pattern "/etc/" 等需要結尾 slash 才命中。"/etc-backup" 不會誤擋
		// 因為 join 後是 "/etc-backup/_marker" 不含 "/etc/"。
		checkPath := filepath.Join(f.path, "_validation_marker")
		if err := validator.ValidateExternalPath(checkPath); err != nil {
			return fmt.Errorf("%s 驗證失敗: %w", f.name, err)
		}
	}

	return nil
}

// EnsureDirectories 確保配置中的目錄存在.
func (c *AppConfig) EnsureDirectories() error {
	dirs := []string{c.InputDir, c.OutputDir, c.OperateDir, c.LogDirectory, c.TranslationsDir}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, fsperm.DirPerm); err != nil {
			return fmt.Errorf("無法創建目錄 %s: %w", dir, err)
		}
	}

	return nil
}

// ToAnalysisConfig 轉換為分析配置.
func (c *AppConfig) ToAnalysisConfig() *models.AnalysisConfig {
	return &models.AnalysisConfig{
		ScalingFactor: c.ScalingFactor,
		PhaseLabels:   c.PhaseLabels,
		CreatedAt:     time.Now(),
	}
}

// ProcessingOptions 獲取處理選項.
func (c *AppConfig) ProcessingOptions() *models.ProcessingOptions {
	return &models.ProcessingOptions{
		ValidateInput: true,
		Precision:     c.Precision,
		OutputFormat:  c.OutputFormat,
	}
}
