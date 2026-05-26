package cci

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteCSVFile_LegacyStaleTmpNoLongerBlocks 取代舊版
// TestWriteCSVFile_StaleTmp_FinalPathUntouched — 後 csvutil.WriteCSVAtomic
// 改 random tmp suffix,legacy `.tmp` 不再撞名,新寫入不再 block。
//
// 對應 atomic-write 契約釘住改為:legacy `.tmp` 存在下仍能成功 commit final path。
func TestWriteCSVFile_LegacyStaleTmpNoLongerBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subj_CCI_Rudolph.csv")

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

	err := writeCSVFile(context.Background(), path, result, nil)
	if err != nil {
		t.Errorf("後 writeCSVFile 應在 legacy `.tmp` 存在下仍成功 (random suffix),got err: %v", err)
	}

	got, _ := os.ReadFile(path)
	if len(got) == 0 {
		t.Errorf("final CSV 未建立")
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
	_, err := a.ExportToCSV(context.Background(), result, "/etc/subj_out")
	if err == nil {
		t.Fatal("expected err for system-dir outputDir, got nil")
	}
	if !strings.Contains(err.Error(), "OutputDir") {
		t.Errorf("expected err to mention OutputDir, got %v", err)
	}
}

// TestExportToCSV_ReturnsEmptyPathOnError 守護
//
// 過去版本對 writeCSVFile error 仍回 outputPath — caller 看到 "(non-empty path, err)"
// 容易誤把 path 當「寫進去了但有 warn」處理 (例如 logging path 給 user 顯示)。
// 實際上 WriteCSVAtomic 失敗時 tmp 檔已被 abort,outputPath 上不會有檔案
// (或為前次留下的舊版本 — 更糟,caller 可能不知道讀到的是 stale data)。
//
// 修法後:寫檔 / 路徑驗證失敗時 return "" + err,讓 caller 無法誤用 path。
//
// 此 test 守住兩條 invariant:
//
//  1. validator 失敗 (system-dir outputDir) → path == ""
//  2. writeCSVFile 失敗 (invalid gait duration) → path == ""
//  3. 控制組:成功路徑仍回 non-empty path
func TestExportToCSV_ReturnsEmptyPathOnError(t *testing.T) {
	a := NewCCIAnalyzer()

	t.Run("validator_failure_returns_empty_path", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:       "subj",
			GaitStartTime: 0,
			GaitEndTime:   1.0,
			TimeValues:    []float64{0, 1.0},
			PairResults:   []CCIResult{{PairName: "P1", Values: []float64{0.1, 0.2}}},
		}
		path, err := a.ExportToCSV(context.Background(), result, "/etc/subj_out")
		if err == nil {
			t.Fatal("expected err for system-dir outputDir, got nil")
		}
		if path != "" {
			t.Errorf("error path should return empty string; got %q (caller may误用為合法路徑)", path)
		}
	})

	t.Run("writeCSV_failure_returns_empty_path", func(t *testing.T) {
		tempDir := t.TempDir()
		// gait start == end 觸發 ErrInvalidGaitCycle 在 writeCSVFile 內
		result := &CCIAnalysisResult{
			Subject:       "subj",
			GaitStartTime: 1.0,
			GaitEndTime:   1.0, // invalid duration
			TimeValues:    []float64{1.0},
			PairResults:   []CCIResult{{PairName: "P1", Values: []float64{0.1}}},
		}
		path, err := a.ExportToCSV(context.Background(), result, tempDir)
		if err == nil {
			t.Fatal("expected err for invalid gait duration, got nil")
		}
		if path != "" {
			t.Errorf("writeCSV error path should return empty string; got %q", path)
		}
	})

	t.Run("success_returns_non_empty_path", func(t *testing.T) {
		tempDir := t.TempDir()
		result := &CCIAnalysisResult{
			Subject:       "success_subj",
			GaitStartTime: 0,
			GaitEndTime:   1.0,
			TimeValues:    []float64{0.0, 1.0},
			PairResults: []CCIResult{
				{PairName: "RA/ES", Values: []float64{0.42, 0.51}},
			},
		}
		path, err := a.ExportToCSV(context.Background(), result, tempDir)
		if err != nil {
			t.Fatalf("expected success, got err: %v", err)
		}
		if path == "" {
			t.Error("success path should return non-empty path")
		}
	})
}
