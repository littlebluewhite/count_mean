package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"count_mean/internal/security/fsperm"
)

// 預設 rotation 閾值（caller 可在 NewSizeRotatingWriter 覆寫）。
const (
	DefaultMaxLogSizeMB = 100 // 達 100MB → rotate
	DefaultMaxBackups   = 7   // 保留 7 個 .1 ~ .7 backup
	megabyteMultiplier  = 1024 * 1024
)

// ErrLogPathAlreadyOpen 表示同個 process 內已有另一個 SizeRotatingWriter 持有相同
// 的 logPath。第二個 caller 必須選一條: (1) reuse 既有 writer (2) 改用不同 path
// (3) 確認真的要競爭並先 Close 既有 writer。
//
// cross-process safety 僅靠 doc 明示「同 logPath 只允許單 process 持有」是
// 因為 cross-platform flock 引入額外 syscall dependency 與 build tag 複雜度,
// 對 desktop GUI 工具的 ROI 過低 (使用者不會同時跑兩個 EMG 工具)。
// 但同個 process 內 double-open 是 bug (而非合法 use case),用 in-process registry
// 自動偵測,避免「兩個 writer 都認為自己持有 log file → rotation 互相打架 → log
// 內容交錯/截斷」的隱藏 race。
var ErrLogPathAlreadyOpen = errors.New("logPath 已被同 process 內另一個 SizeRotatingWriter 開啟")

// ErrWriterClosed 表示 SizeRotatingWriter 已 Close,或在 rotateLocked 失敗後無法
// reopen 原檔 — 後續 Write 必須 graceful 失敗而非 nil-deref panic。
//
// Codex + Agent 15 cross-confirmed,過去 rotateLocked 內 file.Close 成功但
// 接下來 Rename / OpenFile 失敗時,w.file 已被 nil,後續 Write 直接 nil-deref。
// 修補引入 (1) Write 開頭 nil-guard 回 ErrWriterClosed (2) rotateLocked 失敗時
// best-effort reopen 原檔讓 fallback 能繼續寫入 — docstring 承諾的「fallback 為
// 繼續寫到原檔」第一次有實作對齊。
var ErrWriterClosed = errors.New("SizeRotatingWriter 已 Close 或 rotation fallback 失敗")

// openLogPaths 追蹤 process 內已開啟的 logPath。key 為 absolute path,value 為 *SizeRotatingWriter
// (供 debug,目前只用 key existence)。Close 時負責 unregister。
//
// 為什麼用 map 而非 sync.Map:map+mutex 在「open 同時做 register-or-fail」的 atomic
// CAS-style 操作更直觀 (LoadOrStore 的 OR-fail 語意需要先 Load 再判,但中間有 race window);
// 用 mutex 包住 Lookup + Insert 才能保證 register 的 atomicity。
//
//nolint:gochecknoglobals // process-wide registry is intentional for cross-writer dedup
var (
	openLogPaths   = make(map[string]*SizeRotatingWriter)
	openLogPathsMu sync.Mutex
)

// SizeRotatingWriter 是 io.Writer + size-based rotation。
// 寫入累計到 MaxSizeBytes 時，把目前檔 rename 為 `<base>.1`、舊的 .1 → .2、...
// 超過 MaxBackups 的 backup 自動刪除。
//
// 為何不引入 lumberjack：
//   - 專案是離線 desktop tool，限縮新 dep
//   - 本 writer 只需 size-based + 固定 backup 數，不需要 time-based / compression
//   - ~100 行可控代碼比拉一整個 package 簡單
//
// 對齊 解決 logger 長跑塞爆磁碟問題。
//
// **並發/重複開啟契約**:
//   - **Cross-process**: 同個 logPath 由「同一個 process」獨佔。 第二個 process
//     開啟同一個 logPath 不會被 detect (沒做 flock),但 rotation 的 rename 序列
//     會互相覆蓋,造成 log 內容交錯/丟失。Desktop GUI 工具預期單 instance 執行;
//     若 deploy 場景需多 instance 共用 log,改用 syslog / stdout + journalctl。
//   - **Same-process**: NewSizeRotatingWriter 對同個 logPath 第二次開啟會回
//     ErrLogPathAlreadyOpen。Close 時自動 unregister。
type SizeRotatingWriter struct {
	mu       sync.Mutex
	file     *os.File
	basePath string
	// absPath 在 construction 時凍結 — Close / future-cleanup 必須用 stored 值,
	// 不能再次 filepath.Abs(basePath)。 若 caller 在 writer 生命週期內
	// os.Chdir,重算 abs 會用「當下 cwd」得到錯誤 key,registry delete 變 no-op,
	// stale entry leak 在 process 內,後續對同個 logical path reopen 誤判
	// double-open。 凍結 absPath 切斷對 cwd 的隱性依賴。
	absPath      string
	currentBytes int64
	maxBytes     int64
	maxBackups   int
}

// NewSizeRotatingWriter 開啟（或創建）log file 並回傳支援 size rotation 的 writer。
// maxSizeMB <= 0 用 default；maxBackups < 0 用 default；filePath 必須是 caller 已驗
// 過的安全路徑。
//
// **重複開啟偵測**: 同個 process 內若已有另一個 SizeRotatingWriter 持有相同
// filePath (用 absolute path 比對),回 ErrLogPathAlreadyOpen — 避免兩個 writer 同
// 步 rotate 時 rename 序列互相覆蓋。caller 應 reuse 既有 writer 或先 Close。
func NewSizeRotatingWriter(filePath string, maxSizeMB, maxBackups int) (*SizeRotatingWriter, error) {
	if maxSizeMB <= 0 {
		maxSizeMB = DefaultMaxLogSizeMB
	}
	if maxBackups < 0 {
		maxBackups = DefaultMaxBackups
	}

	// 用 absolute path 作為 registry key,避免 "./logs/app.log" 與
	// "/Users/.../logs/app.log" 被當作不同 path 重複開啟。
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("無法解析 log file 絕對路徑 %s: %w", filePath, err)
	}

	openLogPathsMu.Lock()
	if _, exists := openLogPaths[absPath]; exists {
		openLogPathsMu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrLogPathAlreadyOpen, absPath)
	}
	// 先佔位再做 IO — 即使後續 OpenFile 失敗,要在 error path 解除佔位。
	openLogPaths[absPath] = nil
	openLogPathsMu.Unlock()

	//nolint:gosec // G304: filePath validated by caller
	file, err := os.OpenFile(filePath, fsperm.AppendFlags, fsperm.FilePerm)
	if err != nil {
		openLogPathsMu.Lock()
		delete(openLogPaths, absPath)
		openLogPathsMu.Unlock()
		return nil, fmt.Errorf("無法開啟 log file %s: %w", filePath, err)
	}

	stat, err := file.Stat()
	if err != nil {
		// best-effort close — 即使 close 也失敗,err path 主要訊息是 stat 失敗。
		if closeErr := file.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "log open: stat 失敗後 close 也失敗(non-fatal): %v\n", closeErr)
		}
		openLogPathsMu.Lock()
		delete(openLogPaths, absPath)
		openLogPathsMu.Unlock()
		return nil, fmt.Errorf("無法取得 log file 狀態 %s: %w", filePath, err)
	}

	w := &SizeRotatingWriter{
		file:         file,
		basePath:     filePath,
		absPath:      absPath,
		currentBytes: stat.Size(),
		maxBytes:     int64(maxSizeMB) * megabyteMultiplier,
		maxBackups:   maxBackups,
	}

	openLogPathsMu.Lock()
	openLogPaths[absPath] = w
	openLogPathsMu.Unlock()

	// maxBackups 下降後,scan dir 收斂遺留下來編號超過上限的舊 backup。
	// best-effort:刪不到不影響 caller (registry / file 都已就緒),失敗只 log 到 stderr。
	w.pruneExcessBackups()

	return w, nil
}

// Write implements io.Writer. 寫前先檢查 rotation 條件，必要時 rotate 再寫。
// 寫入失敗會 propagate err 給 caller，rotate 失敗會 fallback 為「繼續寫到原檔」
// 並不 panic — log path 不應該因 rotation 故障導致應用程序崩潰。
//
// Close 後或 rotateLocked 失敗且 reopen 也失敗時,w.file 會是 nil。此時
// 直接 Write 會 nil-deref panic — 改為回 ErrWriterClosed,caller 可選擇用 errors.Is
// 偵測並降級到 stderr / stdout。
func (w *SizeRotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, ErrWriterClosed
	}

	if w.currentBytes+int64(len(p)) > w.maxBytes {
		if rotErr := w.rotateLocked(); rotErr != nil {
			// rotate 失敗繼續寫原檔；至少不丟 log，再次 boot 時人工處理。
			// rotateLocked 在失敗 path 會 best-effort reopen 原檔讓 w.file 不為 nil;
			// 萬一連 reopen 都失敗(eg parent dir 整個 unwritable),仍可能落到 nil。
			_, _ = fmt.Fprintf(os.Stderr, "log rotation 失敗（繼續寫到原檔）: %v\n", rotErr)
			if w.file == nil {
				return 0, ErrWriterClosed
			}
		}
	}

	n, err := w.file.Write(p)
	w.currentBytes += int64(n)
	if err != nil {
		return n, fmt.Errorf("log file write: %w", err)
	}
	return n, nil
}

// Sync 對底層 *os.File 觸發 fsync,把 page cache 內已寫入但尚未落盤的 log 強制
// 寫到 disk。配套:Logger.Fatal 在 os.Exit 之前呼叫此函式,確保最後一筆
// Fatal 訊息 crash-durable。
//
// idempotent: file == nil(已 Close / rotation fallback 失敗)直接 return nil,
// 不報 error — Fatal path 不能因 Sync 失敗 hang。其他狀況呼叫 *os.File.Sync。
// 用 w.mu 短鎖避免 Sync 與 Write / Close / rotateLocked 並發,但 Sync 本身是
// blocking syscall,長鎖會拖慢 hot Write — 此處只 Sync,不做其他 IO,所以鎖
// 區間極短。
func (w *SizeRotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("log file sync: %w", err)
	}
	return nil
}

// Close 關閉底層 file 並解除 process-wide registry 佔位。
// caller 應在程式結束前呼叫;同個 logPath 在 Close 後可被重新 NewSizeRotatingWriter。
//
// idempotent: 多次 Close 不會 panic / 不會回 error,file == nil 直接早退。
func (w *SizeRotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	closeErr := w.file.Close()
	w.file = nil

	// 解除 registry 佔位 — 失敗時不 propagate,因為 Close 的主功能是釋放 file handle,
	// registry 是 best-effort guard;用 construction 時 store 的 absPath,而非
	// 重算 filepath.Abs(w.basePath) — 後者在 caller chdir 後會得到錯誤 key,
	// stale entry 永久 leak。
	if w.absPath != "" {
		openLogPathsMu.Lock()
		delete(openLogPaths, w.absPath)
		openLogPathsMu.Unlock()
	}

	if closeErr != nil {
		return fmt.Errorf("log file close: %w", closeErr)
	}
	return nil
}

// rotateLocked 必須在持有 w.mu 時呼叫。
// Rotation 流程：
//  1. 關閉目前 file
//  2. <base>.N → 刪（超過 maxBackups）
//  3. <base>.(N-1) → <base>.N
//  4. ...
//  5. <base> → <base>.1
//  6. 重新開新 <base>
//
// 任一步失敗都會 best-effort reopen 原檔(reopenFallbackLocked),
// 讓 caller 的 Write 不會 nil-deref。失敗 path 仍會 return 包裝過的 error,
// caller (Write) 把它 log 到 stderr 並繼續寫到剛 reopen 的 file。
func (w *SizeRotatingWriter) rotateLocked() error {
	if w.file != nil {
		// rename 前先 fsync 確保 page cache 內已寫入的 log 落盤。
		// 沒此 fsync,系統 crash 發生在 rename 與下次 fsync 之間時,雖然 rename
		// 本身 atomic(POSIX),但 backup 檔的 content 可能還在 page cache 沒寫
		// 到 disk,crash recovery 後出現「rename 完成但 backup 內容空白 / 殘缺」。
		// 對 audit log / compliance 場景 (EMG 醫療資料) 損失不可接受。
		// Sync 失敗仍嘗試 Close+reopen — 至少保證 caller Write 有 fd 可寫。
		if err := w.file.Sync(); err != nil {
			// fsync 失敗但 file 還在 — 走 Close 流程繼續 rotation。fsync 失敗
			// 不致命,僅犧牲此次 rotation 的 durability,後續 Write 仍工作。
			_, _ = fmt.Fprintf(os.Stderr, "log rotation: fsync 舊 log 失敗(non-fatal): %v\n", err)
		}
		if err := w.file.Close(); err != nil {
			// w.file 已不可用;嘗試 reopen 讓後續 Write 有 file 可寫。
			w.file = nil
			w.reopenFallbackLocked()
			return fmt.Errorf("關閉舊 log 失敗: %w", err)
		}
		w.file = nil
	}

	// 刪除超過 backup 上限的最舊一個。Remove 失敗不致命 — 接下來的 rename 仍可
	// 推進(.N → 已存在的 oldest 會被 OS 覆寫,unix 上 rename atomic 覆蓋),只
	// 是磁碟回收延遲一輪。但仍 log 出來避免靜默吞 IO 問題。
	oldest := backupPath(w.basePath, w.maxBackups)
	if _, err := os.Stat(oldest); err == nil {
		if rmErr := os.Remove(oldest); rmErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "log rotation: 刪除最舊 backup %s 失敗(non-fatal): %v\n", oldest, rmErr)
		}
	}

	// 依序往後 rename：.(N-1) → .N
	for i := w.maxBackups - 1; i >= 1; i-- {
		from := backupPath(w.basePath, i)
		to := backupPath(w.basePath, i+1)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			w.reopenFallbackLocked()
			return fmt.Errorf("rename %s → %s 失敗: %w", from, to, err)
		}
	}

	// 主檔 → .1
	if w.maxBackups >= 1 {
		if err := os.Rename(w.basePath, backupPath(w.basePath, 1)); err != nil {
			w.reopenFallbackLocked()
			return fmt.Errorf("rename %s → .1 失敗: %w", w.basePath, err)
		}
	} else {
		// maxBackups == 0：直接刪除主檔。Remove 失敗仍 reopen — fallback 路徑會
		// 把現有檔當原檔繼續寫(O_APPEND),log 不會丟。
		if rmErr := os.Remove(w.basePath); rmErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "log rotation: 刪除主檔 %s 失敗(non-fatal): %v\n", w.basePath, rmErr)
		}
	}

	// rename 後 fsync parent dir,讓「rename 完成」這個目錄變更
	// 落盤。POSIX 規範 — rename atomicity 限於 in-flight 期間;crash recovery
	// 後是否能看到 rename 結果取決於 parent dir 的 metadata 是否已 fsync。
	// 沒此呼叫,系統 crash 發生在 rename 與下次自然 fsync 之間,recovery 可能
	// 看到 backup file 已存在但 directory entry 仍指向舊位置 → log content
	// inaccessible。對 audit log 場景不可接受。
	//
	// fsync parent dir 失敗 non-fatal — 後續 rotation 仍會跑,但此次 rotation
	// 的 durability 弱化。stderr log 留 audit trail。
	if syncErr := fsperm.SyncParentDir(w.basePath); syncErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "log rotation: fsync parent dir 失敗(non-fatal): %v\n", syncErr)
	}

	// 重新開新主檔
	//nolint:gosec // G304: basePath validated by caller; same path as initial open
	file, err := os.OpenFile(w.basePath, fsperm.AppendFlags, fsperm.FilePerm)
	if err != nil {
		w.reopenFallbackLocked()
		return fmt.Errorf("重新開新 log 失敗: %w", err)
	}
	w.file = file
	w.currentBytes = 0
	return nil
}

// reopenFallbackLocked 在 rotateLocked 失敗 path 嘗試「reopen 原檔」(可能是
// 還沒被 rename 走的 basePath,或 rename 後 basePath 不存在 → OpenFile 用
// AppendFlags 帶 O_CREATE 會建一個空的)。讓 caller Write 至少有 *os.File 可寫,
// 對齊 Write 的 docstring「fallback 為繼續寫到原檔」。
//
// 必須在持有 w.mu 時呼叫。失敗(連 reopen 都失敗)時 w.file 保持 nil,Write 會
// 回 ErrWriterClosed 而非 panic。currentBytes 重設為現有檔案大小(若 stat 失敗
// 則歸零)避免 rotation 條件持續被誤判為 already past threshold。
func (w *SizeRotatingWriter) reopenFallbackLocked() {
	//nolint:gosec // G304: basePath validated at NewSizeRotatingWriter; same path
	file, err := os.OpenFile(w.basePath, fsperm.AppendFlags, fsperm.FilePerm)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "log rotation fallback reopen 失敗: %v\n", err)
		return
	}
	w.file = file
	if stat, statErr := file.Stat(); statErr == nil {
		w.currentBytes = stat.Size()
	} else {
		w.currentBytes = 0
	}
}

// backupPath 組合 backup 路徑。為何 .1 是「最新」backup（非 .N）：lumberjack 慣例
// 一致，使用者較熟悉。
func backupPath(base string, n int) string {
	return filepath.Join(filepath.Dir(base), fmt.Sprintf("%s.%d", filepath.Base(base), n))
}

// backupSuffixRegexp 從 backup file name 抽出 ".N" 後面的純數字後綴。
//
// 預編譯 regex 取代原本「strings.HasPrefix + strconv.Atoi」兩段拼裝。
// 預編譯 (package-level var) 確保 regex.Compile 只跑一次(package init),
// 而不是每次 pruneExcessBackups 都重新 compile;對長跑 process 內 Init/重啟
// 多次的場景,compile 成本可累積。
//
// Pattern 解釋:
//   - $ 強制 end-of-string,拒絕「.4.bak」之類 mixed suffix 形式
//   - (\d+) 捕獲純數字後綴,給 strconv.Atoi 用
//
// 此 regex 不嵌 baseName(基名)— 因為基名在 NewSizeRotatingWriter 是動態值,
// caller 各自不同;pruneExcessBackups 內每次傳 baseName 構造完整 prefix-strip
// 後再餵這個 regex 比對 "純數字後綴" 部分。這樣 regex 預編譯 + 與 baseName 解耦
// 兩個性質都拿到。
//
//nolint:gochecknoglobals // compile-once regex; immutable after package init
var backupSuffixRegexp = regexp.MustCompile(`^(\d+)$`)

// pruneExcessBackups 掃描 basePath 所在 dir,刪除編號 > maxBackups 的舊 backup。
//
// 過去 rotateLocked 只在每輪 rotation 刪一個 (`backupPath(base, maxBackups)`),
// 假設「過去的 backup 數量 ≤ 當前 maxBackups」。 一旦 caller 把 maxBackups 從 10
// 降到 3,.4 ~ .10 永遠沒人清,長期累積在磁碟上 — 違反「保留 maxBackups 個」契約。
//
// 修補:Open 入口 (NewSizeRotatingWriter) 收斂遺留檔。 不在 rotateLocked 入口再
// 跑一次的理由:rotation 過程已持有 w.mu,額外 scan dir 引入 IO 拖累 hot path;
// Open 是一次性事件,適合做收斂。 若 caller 長跑 process 並動態調 maxBackups
// (目前 API 不支援),需要額外 hook;當前需求只覆蓋「重啟時 maxBackups 改小」場景。
//
// best-effort:scan / delete 任一步失敗都不致命 — caller 仍取得 functional writer,
// 失敗訊息走 stderr。 不在 mu 下執行,因為:
//
//	(1) Open 時 caller 還沒拿到 w reference,沒有並發 Write 來爭 mu
//	(2) pruneExcessBackups 只動 .N (N > maxBackups) 的舊檔,跟 hot rotation
//	    (.1 ~ .maxBackups) 無交集
//
// 不對 .N (N ≤ maxBackups) 動手 — 那些是 valid backup,rotateLocked 自己會推進。
//
// 用 backupSuffixRegexp (package-level 預編譯 regex) 取代「strings.HasPrefix
// + strconv.Atoi」拼裝。 regex compile 只跑一次(package init), prune 函式只負責
// regex.FindStringSubmatch + Atoi 抽數值,語意更清楚也省 compile 成本。
func (w *SizeRotatingWriter) pruneExcessBackups() {
	dir := filepath.Dir(w.basePath)
	baseName := filepath.Base(w.basePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "log rotation prune: 讀取 dir %s 失敗(non-fatal): %v\n", dir, err)
		return
	}

	prefix := baseName + "."
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Prefix 比對先做(O(len(prefix)) 字符比對,比 regex 啟動成本低)
		// 不符 prefix 直接跳過,避免對 unrelated file 跑 regex。
		if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		suffix := name[len(prefix):]
		// 預編譯 regex 比對純數字後綴,拒絕「.4.bak」「.tmp」等 mixed suffix
		// 避免誤刪使用者放在同 dir 的 unrelated file。
		matches := backupSuffixRegexp.FindStringSubmatch(suffix)
		if matches == nil {
			continue
		}
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			// regex 已限制 \d+,理論上不可能失敗;defensive 跳過。
			continue
		}
		// 只刪超過 maxBackups 上限的舊 backup;.1 ~ .maxBackups 由 rotation logic 自管。
		if n <= w.maxBackups {
			continue
		}
		stalePath := filepath.Join(dir, name)
		if rmErr := os.Remove(stalePath); rmErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "log rotation prune: 刪除過量 backup %s 失敗(non-fatal): %v\n",
				stalePath, rmErr)
		}
	}
}
