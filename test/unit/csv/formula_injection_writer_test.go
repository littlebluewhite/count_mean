package csv_test

import (
	"strings"
	"testing"

	"count_mean/internal/io"
	"count_mean/internal/models"
)

// TestCSVConverter_SanitizesMaliciousHeaders verifies that all three converter
// entry points pass user-controlled headers through the sanitizer before
// emitting them to CSV. Headers are the highest-risk surface because they
// round-trip from uploaded files into exported CSVs without ever passing
// through the read-side cell validator.
func TestCSVConverter_SanitizesMaliciousHeaders(t *testing.T) {
	t.Parallel()

	maliciousHeaders := []string{"Time", "=cmd|/c calc!A1", "@SUM(1+1)", "+1|cmd"}
	expectedPrefixes := []string{"Time", "'=", "'@", "'+"}

	c := io.NewCSVConverter(1.0, 6)

	cases := []struct {
		name string
		got  func() [][]string
	}{
		{
			name: "ConvertMaxMeanResults",
			got: func() [][]string {
				return c.ConvertMaxMeanResults(maliciousHeaders, nil, 0, 0)
			},
		},
		{
			name: "ConvertNormalizedData",
			got: func() [][]string {
				return c.ConvertNormalizedData(&models.EMGDataset{Headers: maliciousHeaders})
			},
		},
		{
			name: "ConvertPhaseAnalysis",
			got: func() [][]string {
				return c.ConvertPhaseAnalysis(
					maliciousHeaders,
					&models.PhaseAnalysisResult{
						PhaseName:  "Test",
						MaxValues:  map[int]float64{},
						MeanValues: map[int]float64{},
					},
					nil,
				)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := tc.got()
			if len(rows) == 0 {
				t.Fatalf("got no rows")
			}
			header := rows[0]
			if len(header) != len(expectedPrefixes) {
				t.Fatalf("header length: got %d, want %d", len(header), len(expectedPrefixes))
			}
			for i, prefix := range expectedPrefixes {
				if !strings.HasPrefix(header[i], prefix) {
					t.Errorf("header[%d] = %q, want prefix %q", i, header[i], prefix)
				}
			}
		})
	}
}
