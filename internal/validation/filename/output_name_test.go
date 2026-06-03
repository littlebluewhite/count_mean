package filename

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubjectOutputName(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		suffix  string
		want    string
	}{
		{"normal subject", "NSF1", "CCI_Rudolph", "NSF1_CCI_Rudolph"},
		{"empty subject falls back to untitled", "", "chart_composer", "untitled_chart_composer"},
		{"path separators collapse to single segment", "a/b\\c", "x", "a_b_c_x"},
		{"dynamic suffix (statistics)", "S1", "P0-P1_statistics", "S1_P0-P1_statistics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubjectOutputName(tt.subject, tt.suffix)
			assert.Equal(t, tt.want, got)
			// 不變量:輸出恆為單一 path segment,無法 traverse。
			assert.NotContains(t, got, "/")
			assert.NotContains(t, got, "\\")
			// 契約(load-bearing): = Sanitize(subject) + "_" + suffix。
			assert.Equal(t, Sanitize(tt.subject)+"_"+tt.suffix, got)
		})
	}
}
