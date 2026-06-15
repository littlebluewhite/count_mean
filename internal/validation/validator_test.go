package validation

import (
	"testing"

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
}

func TestInputValidator_ValidateFilename(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{
			name:     "valid filename",
			filename: "test.csv",
			wantErr:  false,
		},
		{
			name:     "empty filename",
			filename: "",
			wantErr:  true,
		},
		{
			name:     "filename with null byte",
			filename: "test\x00.csv",
			wantErr:  true,
		},
		{
			name:     "filename with dangerous char",
			filename: "test<.csv",
			wantErr:  true,
		},
		{
			name:     "reserved name",
			filename: "CON.csv",
			wantErr:  true,
		},
		{
			name:     "too long filename",
			filename: string(make([]byte, 300)) + ".csv",
			wantErr:  true,
		},
		{
			name:     "invalid extension",
			filename: "test.txt",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilename() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInputValidator_ValidateCSVData(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name     string
		records  [][]string
		filename string
		wantErr  bool
	}{
		{
			name: "valid CSV data",
			records: [][]string{
				{"Time", "Channel1", "Channel2"},
				{"0.1", "100", "200"},
				{"0.2", "150", "250"},
			},
			filename: "test.csv",
			wantErr:  false,
		},
		{
			name:     "empty CSV data",
			records:  [][]string{},
			filename: "test.csv",
			wantErr:  true,
		},
		{
			name: "CSV with only header",
			records: [][]string{
				{"Time", "Channel1", "Channel2"},
			},
			filename: "test.csv",
			wantErr:  true,
		},
		{
			name: "CSV with inconsistent columns",
			records: [][]string{
				{"Time", "Channel1", "Channel2"},
				{"0.1", "100", "200"},
				{"0.2", "150"}, // Missing column
			},
			filename: "test.csv",
			wantErr:  true,
		},
		{
			name: "CSV with empty header",
			records: [][]string{
				{},
				{"0.1", "100", "200"},
			},
			filename: "test.csv",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCSVData(tt.records, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCSVData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
