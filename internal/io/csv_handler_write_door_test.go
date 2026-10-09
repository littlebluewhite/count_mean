package io

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/models"
)

// writeMaxMeanFor 以 req 呼叫 WriteMaxMean,供 File-based write 單一寫門的測試共用。
func writeMaxMeanFor(h *CSVHandler, req WriteRequest) (string, error) {
	results := []models.MaxMeanResult{
		{ColumnIndex: 1, StartTime: 1.0, EndTime: 2.0, MaxMean: 100.0},
	}

	return h.WriteMaxMean(req, []string{"Time", "Ch1"}, results, 0.5, 3.0)
}

// 檔名中的 `+` 是字面字元,不得被當成 URL-encoded 空白。
func TestWriteMaxMean_PlusFilenameWritesExactName(t *testing.T) {
	t.Parallel()

	handler, dir := newFormatAwareTestHandler(t)

	got, err := writeMaxMeanFor(handler, WriteRequest{Filename: "a+b.csv"})
	require.NoError(t, err)

	want := filepath.Join(dir, "a+b.csv")
	require.Equal(t, want, got)
	require.FileExists(t, want)
	require.NoFileExists(t, filepath.Join(dir, "a b.csv"))
}

// 檔名中的 `%` 是字面字元(BTS 匯出檔名),不得被 URL-decode 或拒寫。
func TestWriteMaxMean_LiteralPercentFilename(t *testing.T) {
	t.Parallel()

	handler, dir := newFormatAwareTestHandler(t)

	for _, name := range []string{"SF_8_BTS%_6.10.csv", "50%.csv", "%41.csv", "a%2Fb.csv"} {
		got, err := writeMaxMeanFor(handler, WriteRequest{Filename: name})
		require.NoError(t, err, name)
		require.Equal(t, filepath.Join(dir, name), got)
		require.FileExists(t, filepath.Join(dir, name), name)
	}
}

// 回傳值必須是「實際寫入的路徑」:該檔存在,且等於 join 後的結果。
func TestFileBasedWriters_ReturnWrittenPath(t *testing.T) {
	t.Parallel()

	handler, dir := newFormatAwareTestHandler(t)
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    []models.EMGData{{Time: 0.1, Channels: []float64{1.0}}},
	}

	for _, sub := range []string{"", "sub"} {
		writers := map[string]func(name string) (string, error){
			"WriteMaxMean": func(name string) (string, error) {
				return writeMaxMeanFor(handler, WriteRequest{Filename: name, SubDir: sub})
			},
			"WriteNormalized": func(name string) (string, error) {
				return handler.WriteNormalized(WriteRequest{Filename: name, SubDir: sub}, dataset)
			},
		}
		for label, write := range writers {
			name := label + "+x%.csv"
			got, err := write(name)
			require.NoError(t, err, label)

			want := filepath.Join(dir, sub, name)
			require.Equal(t, want, got, label)

			_, statErr := os.Stat(got)
			require.NoError(t, statErr, "%s 回傳的路徑必須是實際存在的檔案", label)
		}
	}
}

// OutputDir 本身含 `+` 時,不得被改寫成含空白的另一個目錄。
func TestWriteMaxMean_OutputDirWithPlus(t *testing.T) {
	t.Parallel()

	outDir := filepath.Join(t.TempDir(), "out+dir")
	cfg := config.DefaultConfig()
	cfg.InputDir = outDir
	cfg.OutputDir = outDir
	cfg.OperateDir = outDir
	handler := NewCSVHandler(cfg)

	got, err := writeMaxMeanFor(handler, WriteRequest{Filename: "r.csv"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(outDir, "r.csv"), got)
	require.FileExists(t, got)
	require.NoDirExists(t, filepath.Join(filepath.Dir(outDir), "out dir"))
}

// SubDir 含 `+` 時同理。
func TestWriteMaxMean_SubDirWithPlus(t *testing.T) {
	t.Parallel()

	handler, dir := newFormatAwareTestHandler(t)

	got, err := writeMaxMeanFor(handler, WriteRequest{Filename: "r.csv", SubDir: "S+1"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "S+1", "r.csv"), got)
	require.FileExists(t, got)
	require.NoDirExists(t, filepath.Join(dir, "S 1"))
}
