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

func TestInputValidator_ValidateWindowSize(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name          string
		windowSizeStr string
		want          int
		wantErr       bool
	}{
		{
			name:          "valid window size",
			windowSizeStr: "100",
			want:          100,
			wantErr:       false,
		},
		{
			name:          "empty window size",
			windowSizeStr: "",
			want:          0,
			wantErr:       true,
		},
		{
			name:          "invalid window size",
			windowSizeStr: "abc",
			want:          0,
			wantErr:       true,
		},
		{
			name:          "zero window size",
			windowSizeStr: "0",
			want:          0,
			wantErr:       true,
		},
		{
			name:          "negative window size",
			windowSizeStr: "-10",
			want:          0,
			wantErr:       true,
		},
		{
			name:          "too large window size",
			windowSizeStr: "20000",
			want:          0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validator.ValidateWindowSize(tt.windowSizeStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWindowSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("ValidateWindowSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInputValidator_ValidateTimeRange(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name          string
		startRangeStr string
		endRangeStr   string
		wantStart     float64
		wantEnd       float64
		wantUseCustom bool
		wantErr       bool
	}{
		{
			name:          "valid time range",
			startRangeStr: "1.0",
			endRangeStr:   "5.0",
			wantStart:     1.0,
			wantEnd:       5.0,
			wantUseCustom: true,
			wantErr:       false,
		},
		{
			name:          "empty ranges",
			startRangeStr: "",
			endRangeStr:   "",
			wantStart:     0,
			wantEnd:       0,
			wantUseCustom: false,
			wantErr:       false,
		},
		{
			name:          "invalid start range",
			startRangeStr: "abc",
			endRangeStr:   "5.0",
			wantStart:     0,
			wantEnd:       0,
			wantUseCustom: false,
			wantErr:       true,
		},
		{
			name:          "negative start range",
			startRangeStr: "-1.0",
			endRangeStr:   "5.0",
			wantStart:     0,
			wantEnd:       0,
			wantUseCustom: false,
			wantErr:       true,
		},
		{
			name:          "start greater than end",
			startRangeStr: "10.0",
			endRangeStr:   "5.0",
			wantStart:     0,
			wantEnd:       0,
			wantUseCustom: false,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd, gotUseCustom, err := validator.ValidateTimeRange(tt.startRangeStr, tt.endRangeStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTimeRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotStart != tt.wantStart {
				t.Errorf("ValidateTimeRange() gotStart = %v, want %v", gotStart, tt.wantStart)
			}

			if gotEnd != tt.wantEnd {
				t.Errorf("ValidateTimeRange() gotEnd = %v, want %v", gotEnd, tt.wantEnd)
			}

			if gotUseCustom != tt.wantUseCustom {
				t.Errorf("ValidateTimeRange() gotUseCustom = %v, want %v", gotUseCustom, tt.wantUseCustom)
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
