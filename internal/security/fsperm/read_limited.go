package fsperm

import (
	"errors"
	"fmt"
	"os"
)

// MaxReadBytes 是整檔讀進記憶體的輸入檔(使用者選取 CSV、分期總檔案、manifest 資料檔)的大小上限(100 MB)。
//
// csv.Reader.ReadAll 把整檔 materialize 成 [][]string,實測 memory 約為 source bytes 的
// 4-10x;200MB source → 800MB-2GB heap 對 GUI process 是 OOM 風險。典型 EMG CSV 1-50MB,
// 100MB 已是極端 outlier;超過一律拒絕(streaming 路徑已刪,見 ADR-0033)。
const MaxReadBytes = 100 * 1024 * 1024

// 讀檔門的 sentinel(ADR-0056)。fsperm 不 import i18n(i18n 依賴 fsperm),所以只是一般
// error;user-facing 訊息由 caller(io.ReadCSV、manifest)轉換。
var (
	// ErrNotRegular 表示開啟的路徑不是一般檔案(目錄、FIFO、device 等)。
	ErrNotRegular = errors.New("非一般檔案")
	// ErrTooLarge 表示檔案超過讀取上限;具體錯誤是 *TooLargeError。
	ErrTooLarge = errors.New("檔案過大")
)

// TooLargeError 是超過讀取上限的具體錯誤,errors.Is(err, ErrTooLarge) 成立。
//
// Size 是看到的大小:開檔時是 fstat 的 st_size;開檔後檔案才變大、讀到第 Limit+1 byte 時
// 只知道「至少 Limit+1」,Size 即為 Limit+1。
type TooLargeError struct {
	Size  int64
	Limit int64
}

func (e *TooLargeError) Error() string {
	const mb = 1024 * 1024

	return fmt.Sprintf("檔案過大（%d MB，上限 %d MB）", (e.Size+mb-1)/mb, e.Limit/mb)
}

// Unwrap 回 ErrTooLarge,讓 caller 以 errors.Is 判斷。
func (*TooLargeError) Unwrap() error { return ErrTooLarge }

// CheckRegularSize 對已開啟的 f 做 fstat:必須是一般檔案(ErrNotRegular)且不超過 limit
// bytes(*TooLargeError)。作用在 fd 上,檢查與讀取是同一個 inode,沒有 stat-then-open 的縫隙。
// 失敗時不關 f,由 caller 決定。
func CheckRegularSize(f *os.File, limit int64) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("fstat: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w (mode %s)", ErrNotRegular, info.Mode().Type())
	}

	if info.Size() > limit {
		return &TooLargeError{Size: info.Size(), Limit: limit}
	}

	return nil
}

// LimitedFile 是 OpenRegularLimited 開好的檔:讀到第 limit+1 byte 時回 *TooLargeError,
// 開檔之後檔案才變大也不會無界讀進記憶體。caller 必須 Close。
type LimitedFile struct {
	f         *os.File
	limit     int64
	remaining int64 // 還能讀幾個 byte 才到第 limit+1 byte
}

// OpenRegularLimited 是整檔讀取的輸入檔(使用者選取 CSV、分期總檔案)的開檔門,依序:
//
//  1. pre-stat:非一般檔案直接回 ErrNotRegular,FIFO 不必開(Windows 的 ReadFlags 沒有
//     O_NONBLOCK)。stat 失敗不在此報錯,交給下一步產生原本的開檔錯誤。
//  2. 以 ReadFlags 開檔(Unix 含 O_NOFOLLOW、O_NONBLOCK)。開檔錯誤原樣回傳(*os.PathError),
//     caller 既有的錯誤對應(errors.Is(err, os.ErrNotExist) 等)與訊息不變。
//  3. CheckRegularSize:fstat 必須是一般檔案且 ≤ limit。
//  4. 回傳的 LimitedFile 讀到第 limit+1 byte 時回 *TooLargeError。
//
// path 的位置檢查(traversal、敏感位置)由 caller 先做;本函式只管「開到的是不超過上限的一般檔案」。
func OpenRegularLimited(path string, limit int64) (*LimitedFile, error) {
	if pre, statErr := os.Stat(path); statErr == nil && !pre.Mode().IsRegular() {
		return nil, fmt.Errorf("%w (mode %s)", ErrNotRegular, pre.Mode().Type())
	}

	f, err := os.OpenFile(path, ReadFlags, 0) //nolint:gosec // caller 已驗證 path 位置;ReadFlags 含 O_NOFOLLOW
	if err != nil {
		return nil, err //nolint:wrapcheck // 開檔錯誤原樣回傳,caller 的 errors.Is 對應與訊息不變
	}

	if err := CheckRegularSize(f, limit); err != nil {
		_ = f.Close() //nolint:errcheck // 已在錯誤路徑;read-only fd 的 close 錯誤不影響結果
		return nil, err
	}

	return &LimitedFile{f: f, limit: limit, remaining: limit + 1}, nil
}

// Read 實作 io.Reader;累計讀到第 limit+1 byte 時回 (n, *TooLargeError)。
func (l *LimitedFile) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, &TooLargeError{Size: l.limit + 1, Limit: l.limit}
	}

	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}

	n, err := l.f.Read(p)
	l.remaining -= int64(n)
	if l.remaining <= 0 {
		return n, &TooLargeError{Size: l.limit + 1, Limit: l.limit}
	}

	return n, err //nolint:wrapcheck // io.Reader 契約:io.EOF 必須原樣回傳
}

// Close 關閉底層檔案。
func (l *LimitedFile) Close() error {
	return l.f.Close() //nolint:wrapcheck // 與 *os.File.Close 相同語意
}
