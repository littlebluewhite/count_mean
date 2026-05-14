package muscle_ratio

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteOutputAll_StaleTmp_FinalPathUntouched 驗證 stale tmp 殘留時 writeOutputAll 不會覆寫 final。
// 修法依 atomic write plan：tmp+rename 流程的 O_EXCL 開檔會偵測到 stale tmp 並 fail。
func TestWriteOutputAll_StaleTmp_FinalPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_muscle_ratio.csv")

	sentinel := []byte("old,csv\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeOutputAll(path, []float64{0.0, 0.01}, [][]float64{{1.0, 2.0}})
	if err == nil {
		t.Fatal("expected err due to stale tmp, got nil")
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(sentinel) {
		t.Errorf("final path mutated: got %q want %q", got, sentinel)
	}
}

// TestAnalyze_BadOutputDir_Rejected 驗證 OutputDir 含 traversal 時 Analyze 入口
// 的 defense-in-depth 早失敗。檢 err 訊息含 "OutputDir" 以區分非 ManifestFile 錯誤。
func TestAnalyze_BadOutputDir_Rejected(t *testing.T) {
	a := NewAnalyzer()
	_, err := a.Analyze(&Params{
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

// TestWriteOutputPhases_StaleTmp_FinalPathUntouched 同 writeOutputPhases 對稱驗證。
func TestWriteOutputPhases_StaleTmp_FinalPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_muscle_ratio_phases.csv")

	sentinel := []byte("old,csv\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeOutputPhases(
		path,
		[]float64{0.0, 0.01},
		[][]float64{{1.0, 2.0}},
		[]phasePoint{{name: "P0", time: 0.0}},
	)
	if err == nil {
		t.Fatal("expected err due to stale tmp, got nil")
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(sentinel) {
		t.Errorf("final path mutated: got %q want %q", got, sentinel)
	}
}
