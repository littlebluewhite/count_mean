package validation_test

import (
	"testing"

	"count_mean/internal/validation"
)

func TestInputValidator_ValidateFilename(t *testing.T) {
	validator := validation.NewInputValidator()

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
	validator := validation.NewInputValidator()

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
	validator := validation.NewInputValidator()

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

func TestInputValidator_ValidatePhaseLabels(t *testing.T) {
	validator := validation.NewInputValidator()

	tests := []struct {
		name            string
		phaseLabelsText string
		wantLen         int
		wantErr         bool
	}{
		{
			name:            "valid phase labels",
			phaseLabelsText: "Phase 1\nPhase 2\nPhase 3",
			wantLen:         3,
			wantErr:         false,
		},
		{
			name:            "empty phase labels",
			phaseLabelsText: "",
			wantLen:         0,
			wantErr:         true,
		},
		{
			name:            "phase labels with empty lines",
			phaseLabelsText: "Phase 1\n\nPhase 2\n\n",
			wantLen:         2,
			wantErr:         false,
		},
		{
			name:            "only whitespace",
			phaseLabelsText: "   \n\n   ",
			wantLen:         0,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validator.ValidatePhaseLabels(tt.phaseLabelsText)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePhaseLabels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("ValidatePhaseLabels() length = %v, want %v", len(got), tt.wantLen)
			}
		})
	}
}

func TestInputValidator_SanitizeString(t *testing.T) {
	validator := validation.NewInputValidator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean string",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "string with null byte",
			input: "hello\x00world",
			want:  "helloworld",
		},
		{
			name:  "string with control chars",
			input: "hello\x01\x02world",
			want:  "helloworld",
		},
		{
			name:  "string with allowed control chars",
			input: "hello\tworld\n",
			want:  "hello\tworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validator.SanitizeString(tt.input); got != tt.want {
				t.Errorf("SanitizeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInputValidator_ValidateCSVData(t *testing.T) {
	validator := validation.NewInputValidator()

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
