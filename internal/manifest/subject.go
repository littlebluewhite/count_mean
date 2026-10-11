package manifest

import (
	"errors"
	"fmt"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

var (
	// ErrSubjectNotFound 表示名稱不在 manifest 內 —— 使用者輸入問題。LoadSubject 回
	// `Subject "<name>" 不存在於分期總檔案`,caller 以 errors.Is 辨識。
	ErrSubjectNotFound = i18n.NewError(i18n.KeyErrorManifestSubjectNotFound)
	// ErrSubjectRowInvalid 表示 LoadSubject 找到的 row 沒通過 parsers.ValidatePhaseManifest。
	// 本身不出現在錯誤文字裡:Error() 是驗證錯誤原文,前綴由各 caller 自己的 i18n key 決定。
	ErrSubjectRowInvalid = errors.New("subject row 未通過分期總檔案驗證")
)

// subjectRowError 讓 row 驗證失敗同時命中 ErrSubjectRowInvalid 與驗證錯誤本身,
// Error() 與驗證錯誤逐字相同(同 EMGParseError 的形狀)。
type subjectRowError struct{ err error }

func (e *subjectRowError) Error() string { return e.err.Error() }

func (e *subjectRowError) Unwrap() []error { return []error{ErrSubjectRowInvalid, e.err} }

// LoadSubjectNames 回 manifest 內非空 Subject 的名稱(manifest 順序),給 subject 下拉選單;
// 選定的名稱交給 LoadSubject。名稱唯一:LoadManifests 的撞名檢查已拒絕輸出檔名身分相同的兩列。
//
// 名稱原樣回傳(Big5 manifest 的 Subject 不是合法 UTF-8);webview 收到的是 JSON 換過的版本,
// LoadSubject 的比對涵蓋這個差異。
func LoadSubjectNames(path string) ([]string, error) {
	manifests, err := LoadManifests(path)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(manifests))
	for i := range manifests {
		if manifests[i].Subject != "" {
			names = append(names, manifests[i].Subject)
		}
	}

	return names, nil
}

// LoadSubject 以名稱取一個 [[Subject]] 的 [[Manifest]] row,並驗證這一列(ADR-0057):
// LoadManifests → 找名稱 → parsers.ValidatePhaseManifest(分期點順序、NaN/Inf、必填欄位)。
// 每個以 Subject 為單位的分析都經這裡,不會拿到沒驗證過的 row。
//
// 比對單位是 rune 序列:string([]rune(row.Subject)) == string([]rune(name))。JSON 把無效
// UTF-8 逐 byte 換成 U+FFFD,[]rune 轉換也一樣,所以 Big5 Subject 經 LoadSubjectNames →
// webview → 送回的名稱仍對得上;原始 bytes 的名稱也對得上。rune 序列相同的兩列必然同一個
// filename.SubjectOutputKey,已被 LoadManifests 的撞名檢查拒絕,所以最多一列相符。
//
// 錯誤:LoadManifests 的錯誤原樣;找不到 → ErrSubjectNotFound;row 驗證失敗 →
// errors.Is ErrSubjectRowInvalid,Error() 與驗證錯誤逐字相同。
func LoadSubject(path, name string) (*models.PhaseManifest, error) {
	manifests, err := LoadManifests(path)
	if err != nil {
		return nil, err
	}

	want := string([]rune(name))
	for i := range manifests {
		row := &manifests[i]
		if string([]rune(row.Subject)) != want {
			continue
		}

		if err := parsers.ValidatePhaseManifest(row); err != nil {
			return nil, &subjectRowError{err: err}
		}

		return row, nil
	}

	return nil, fmt.Errorf("Subject %q %w", name, ErrSubjectNotFound)
}
