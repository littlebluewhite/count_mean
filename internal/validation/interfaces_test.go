package validation

import (
	"testing"

	csvvalidator "count_mean/internal/validation/csv"
	"count_mean/internal/validation/filename"
)

// TestFilenameValidator_InterfaceSplit 釘住 L16 的 interface 拆分契約：
//
//  1. FilenameValidator (read-only) 必須由 filename.Validator 滿足
//  2. MutableFilenameValidator (含 WithAllowedExtensions mutator) 也必須滿足
//  3. 兩者之間有 is-a 關係（MutableFilenameValidator 內嵌 FilenameValidator）
//
// 編譯期靜態 assert 不過就直接 compile error,test 是 runtime smoke 防止
// 未來 refactor 把方法簽名改錯。
func TestFilenameValidator_InterfaceSplit(t *testing.T) {
	t.Parallel()

	v := filename.NewValidator()

	// FilenameValidator: read-only
	var _ FilenameValidator = v
	// MutableFilenameValidator: read + mutate
	var _ MutableFilenameValidator = v

	// 確認 read-only API 仍 work
	if err := (FilenameValidator)(v).ValidateFilename("data.csv"); err != nil {
		t.Errorf("ValidateFilename via read-only interface 不該失敗: %v", err)
	}

	// 確認 mutator interface 能改 extension whitelist
	m := MutableFilenameValidator(v)
	m.WithAllowedExtensions([]string{".tsv"})
	if err := m.ValidateFilename("data.csv"); err == nil {
		t.Error("WithAllowedExtensions 改成 [.tsv] 後，.csv 不該再通過")
	}
	if err := m.ValidateFilename("data.tsv"); err != nil {
		t.Errorf("WithAllowedExtensions 改成 [.tsv] 後，.tsv 應通過，實際 err=%v", err)
	}

	// CSVValidator interface satisfaction
	var _ CSVValidator = csvvalidator.NewValidator()
}
