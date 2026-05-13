package parsers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

func newWriterTestData() *models.PhaseSyncEMGData {
	return &models.PhaseSyncEMGData{
		Time: []float64{0.000, 0.001, 0.002},
		Channels: map[string][]float64{
			"MuscleA": {0.1, 0.2, 0.3},
			"MuscleB": {0.5, 0.4, 0.3},
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}
}

func TestExportPhaseSyncDataToCSV_WritesBOM(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := parsers.ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 6)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	bom := csvutil.BOMBytes()
	require.True(t, bytes.HasPrefix(content, bom), "output must start with UTF-8 BOM")
}

func TestExportPhaseSyncDataToCSV_HeaderOrderPreserved(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	data := newWriterTestData()
	err := parsers.ExportPhaseSyncDataToCSV(data, outPath, 6)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	// 去掉 BOM 後，第一列應為 Time,MuscleA,MuscleB
	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	firstLine := strings.SplitN(body, "\n", 2)[0]
	require.Equal(t, "Time,MuscleA,MuscleB", strings.TrimSpace(firstLine))
}

func TestExportPhaseSyncDataToCSV_PrecisionApplied(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := parsers.ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 3)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.GreaterOrEqual(t, len(lines), 4) // header + 3 data rows

	// 第二列為第一筆資料：Time=0.000, MuscleA=0.100, MuscleB=0.500
	require.Equal(t, "0.000,0.100,0.500", strings.TrimSpace(lines[1]))
}

func TestExportPhaseSyncDataToCSV_DefaultPrecisionWhenZero(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := parsers.ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 0)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	// 預設精度 6，第一筆值 0.1 應寫成 0.100000
	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.Contains(t, lines[1], "0.100000")
}

func TestExportPhaseSyncDataToCSV_NilDataReturnsError(t *testing.T) {
	err := parsers.ExportPhaseSyncDataToCSV(nil, "/tmp/should-not-be-created.csv", 6)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EMG 數據為空")
}

func TestExportPhaseSyncDataToCSV_RoundTripParseAndWrite(t *testing.T) {
	// 寫出後再用 EMGParser 解析，應該還原相同的資料結構。
	dir := t.TempDir()
	outPath := filepath.Join(dir, "roundtrip.csv")

	original := newWriterTestData()
	require.NoError(t, parsers.ExportPhaseSyncDataToCSV(original, outPath, 6))

	parser := parsers.NewEMGParser()
	parsed, err := parser.ParseFile(outPath)
	require.NoError(t, err)

	require.Equal(t, original.Headers, parsed.Headers)
	require.Len(t, parsed.Time, len(original.Time))

	for i, want := range original.Time {
		require.InDelta(t, want, parsed.Time[i], 1e-9)
	}

	for _, name := range original.Headers {
		wantCh := original.Channels[name]
		gotCh := parsed.Channels[name]
		require.Len(t, gotCh, len(wantCh), "channel %s length", name)

		for i, v := range wantCh {
			require.InDelta(t, v, gotCh[i], 1e-9, "channel %s index %d", name, i)
		}
	}
}
