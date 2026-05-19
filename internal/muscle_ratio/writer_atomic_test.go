package muscle_ratio

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteOutputAll_LegacyStaleTmpNoLongerBlocks 取代舊版
// TestWriteOutputAll_StaleTmp_FinalPathUntouched — 後 csvutil.WriteCSVAtomic
// 已改用 crypto/rand 後綴的 tmp filename,legacy `path + ".tmp"` 不再撞名,
// 因此 stale `.tmp` 不再 block writeOutputAll。對應 atomic-write 屬性的釘住改為:
//   - Final path 仍 atomic 寫成 (random tmp → rename → final)
//   - Stale legacy tmp 不影響 commit (保留在 dir 中作為 noise,屬已知 trade-off)
func TestWriteOutputAll_LegacyStaleTmpNoLongerBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_muscle_ratio.csv")

	// 預先寫 legacy `.tmp` (模擬上一次 crash 殘留)
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeOutputAll(path, []float64{0.0, 0.01}, [][]float64{{1.0, 2.0}})
	if err != nil {
		t.Errorf("後 writeOutputAll 應在 legacy `.tmp` 存在下仍成功 (random suffix),got err: %v", err)
	}

	// final path 必須存在且非 sentinel 內容
	got, _ := os.ReadFile(path)
	if len(got) == 0 {
		t.Errorf("final path 未建立")
	}
	if strings.Contains(string(got), "old,csv") {
		t.Errorf("final path 為舊 sentinel 內容,atomic write 失敗")
	}
}

// TestAnalyze_BadOutputDir_Rejected 驗證 OutputDir 含 traversal 時 Analyze 入口
// 的 defense-in-depth 早失敗。檢 err 訊息含 "OutputDir" 以區分非 ManifestFile 錯誤。
func TestAnalyze_BadOutputDir_Rejected(t *testing.T) {
	a := NewAnalyzer()
	_, err := a.Analyze(context.Background(), &Params{
		ManifestFile: "anyfile.csv",
		DataFolder:   ".",
		OutputDir:    "../../etc",
	})
	if err == nil {
		t.Fatal("expected err for traversal outputDir, got nil")
	}
	if !strings.Contains(err.Error(), "OutputDir") {
		t.Errorf("expected err to mention OutputDir (not ManifestFile), got %v", err)
	}
}

// TestFormatRatioCell_InfWrittenAsEmpty 驗證 NaN/+Inf/-Inf 在 cell 層轉空字串
// （CSV missing-data 慣例），避免 Excel/pandas/R 把 "NaN"/"Inf" 字面誤讀為字串。
func TestFormatRatioCell_InfWrittenAsEmpty(t *testing.T) {
	cells := []float64{math.Inf(1), math.Inf(-1), math.NaN(), 1.5}
	cases := []struct {
		idx  int
		want string
	}{
		{0, ""},
		{1, ""},
		{2, ""},
		{3, "1.500000"},
		{99, ""},
		{-1, ""},
	}
	for _, c := range cases {
		got := formatRatioCell(cells, c.idx)
		if got != c.want {
			t.Errorf("idx=%d got %q want %q", c.idx, got, c.want)
		}
	}
}

// TestWriteOutputAll_NaNAndInfCellsWrittenAsEmpty 驗證 row-level 流程：含 NaN/Inf
// 的 cell 在 CSV 輸出為空字串，不會字面寫 "NaN" / "+Inf"。
func TestWriteOutputAll_NaNAndInfCellsWrittenAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj.csv")

	times := []float64{0.0, 0.01, 0.02}
	ratiosAll := make([][]float64, len(DefaultRatios()))
	for k := range ratiosAll {
		ratiosAll[k] = []float64{1.5, math.NaN(), math.Inf(1)}
	}

	if err := writeOutputAll(path, times, ratiosAll); err != nil {
		t.Fatalf("writeOutputAll err: %v", err)
	}

	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "1.500000") {
		t.Errorf("expected 1.500000 in output, got %q", s)
	}
	if strings.Contains(s, "NaN") || strings.Contains(s, "Inf") {
		t.Errorf("output should not contain NaN/Inf literal, got %q", s)
	}
}

// TestWriteOutputPhases_LegacyStaleTmpNoLongerBlocks 取代舊版
// TestWriteOutputPhases_StaleTmp_FinalPathUntouched — 後 random tmp 不再撞名,
// legacy `.tmp` 不 block。對稱版本見 TestWriteOutputAll_LegacyStaleTmpNoLongerBlocks。
func TestWriteOutputPhases_LegacyStaleTmpNoLongerBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_muscle_ratio_phases.csv")

	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeOutputPhases(
		path,
		[]float64{0.0, 0.01},
		[][]float64{{1.0, 2.0}},
		[]phasePoint{{name: "P0", time: 0.0}},
	)
	if err != nil {
		t.Errorf("後 writeOutputPhases 應在 legacy `.tmp` 存在下仍成功 (random suffix),got err: %v", err)
	}

	got, _ := os.ReadFile(path)
	if len(got) == 0 {
		t.Errorf("final path 未建立")
	}
}
