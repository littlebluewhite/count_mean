package fsperm

import "os"

// WriteFileNoFollow writes data to path with WriteFlags + FilePerm — the
// OpenFile / Write / Close pattern needed to apply O_NOFOLLOW from a callsite
// that would otherwise use os.WriteFile (which has no flag knob).
//
// 抽出統一 helper 因為 chart PNG / cci PNG / i18n JSON 三處的 pattern 重複；
// read / append 各只有 1 個 callsite，不額外封裝（避免過度抽象）。
func WriteFileNoFollow(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, WriteFlags, FilePerm)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	_, err = f.Write(data)
	return err
}
