package cci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteCSVFile_StaleTmp_FinalPathUntouched 驗證 stale tmp 殘留時 atomic write
// 偵測為錯誤、final CCI CSV 不被覆寫（與 muscle_ratio writeOutput* 對稱保護）。
func TestWriteCSVFile_StaleTmp_FinalPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_CCI_Rudolph.csv")

	sentinel := []byte("old\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &CCIAnalysisResult{
		Subject:       "subj",
		GaitStartTime: 0.0,
		GaitEndTime:   1.0,
		TimeValues:    []float64{0.0, 0.5, 1.0},
		PairResults: []CCIResult{
			{PairName: "P1", Values: []float64{0.1, 0.2, 0.3}},
		},
	}

	err := writeCSVFile(path, result, nil)
	if err == nil {
		t.Fatal("expected err on stale tmp, got nil")
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(sentinel) {
		t.Errorf("final path mutated: got %q want %q", got, sentinel)
	}
}

// TestExportToCSV_BadOutputDir_Rejected 驗證 outputDir 含 system-dir prefix 時
// ExportToCSV 入口的 defense-in-depth 早失敗（不依賴後續 os.MkdirAll 的 perm error）。
func TestExportToCSV_BadOutputDir_Rejected(t *testing.T) {
	a := NewCCIAnalyzer()
	result := &CCIAnalysisResult{
		Subject:       "subj",
		GaitStartTime: 0,
		GaitEndTime:   1.0,
		TimeValues:    []float64{0, 1.0},
		PairResults:   []CCIResult{{PairName: "P1", Values: []float64{0.1, 0.2}}},
	}
	_, err := a.ExportToCSV(result, "/etc/subj_out")
	if err == nil {
		t.Fatal("expected err for system-dir outputDir, got nil")
	}
	if !strings.Contains(err.Error(), "OutputDir") {
		t.Errorf("expected err to mention OutputDir, got %v", err)
	}
}
