package chart

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"count_mean/internal/models"
)

// TestGetAvailableColumns_EmptyHeaders 釘住 
//
// 修補前 chart_statistics.go:60 `make([]ColumnInfo, 0, len(dataset.Headers)-1)`,
// 當 Headers 為空 slice 時 cap 算出 -1,runtime.makeslice 噴
// "cap out of range" panic。
//
// 修補後 nil / 空 Headers 都回傳空 slice,沒有 panic。
func TestGetAvailableColumns_EmptyHeaders(t *testing.T) {
	cases := []struct {
		name    string
		dataset *models.EMGDataset
	}{
		{"empty Headers slice", &models.EMGDataset{Headers: []string{}, Data: nil}},
		{"nil Headers slice", &models.EMGDataset{Headers: nil, Data: nil}},
		{"only time column", &models.EMGDataset{Headers: []string{"Time"}, Data: nil}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 任何 panic 都會 fail (test 不能撈住 panic 因為這正是要驗證的修補點)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("GetAvailableColumns panicked on %s: %v", tc.name, r)
				}
			}()

			got := GetAvailableColumns(tc.dataset)
			if got == nil {
				t.Errorf("GetAvailableColumns returned nil slice; want non-nil empty slice")
			}
			if len(got) != 0 {
				t.Errorf("GetAvailableColumns returned %d columns; want 0", len(got))
			}
		})
	}
}

// TestChart_NilDataset 釘住 
//
// 6 個 function 在收到 nil *EMGDataset 時必須 no-op,不能 panic。修補前每一個
// 都會在第一行 deref 處 (`len(dataset.X)` 或 `range dataset.Data`) 噴
// "invalid memory address or nil pointer dereference"。V7 PoC 已重現全部 6 個。
//
// 涵蓋:
//   - GetAvailableColumns          (chart_statistics.go)
//   - GetChartStatistics           (chart_statistics.go)
//   - UniformSubsample             (echarts_generator.go)
//   - ConvertToJSON                (echarts_generator.go)
//   - extractTimeData              (echarts_generator.go, internal)
//   - extractColumnLineData        (echarts_generator.go, internal)
func TestChart_NilDataset(t *testing.T) {
	cases := []struct {
		name string
		// 子測試需在 defer-recover 內呼叫一次 target;recover 命中 → fail。
		run func()
	}{
		{
			name: "GetAvailableColumns",
			run: func() {
				got := GetAvailableColumns(nil)
				if got == nil {
					panic("returned nil slice (want non-nil empty)")
				}
				if len(got) != 0 {
					panic("returned non-empty slice on nil input")
				}
			},
		},
		{
			name: "GetChartStatistics",
			run: func() {
				stats := GetChartStatistics(nil, []int{1})
				if stats == nil {
					panic("returned nil map (want non-nil empty/zero map)")
				}
				// 必要的零值 key 應該存在,讓 caller 統一處理
				if got, ok := stats["total_points"].(int); !ok || got != 0 {
					panic("total_points missing or non-zero on nil dataset")
				}
			},
		},
		{
			name: "UniformSubsample",
			run: func() {
				ds := UniformSubsample(nil, 2)
				if ds == nil {
					panic("returned nil dataset (want non-nil empty)")
				}
				if len(ds.Data) != 0 {
					panic("returned non-empty Data on nil input")
				}
			},
		},
		{
			name: "ConvertToJSON",
			run: func() {
				pts := ConvertToJSON(nil, []int{1})
				if pts == nil {
					panic("returned nil slice (want non-nil empty)")
				}
				if len(pts) != 0 {
					panic("returned non-empty slice on nil input")
				}
			},
		},
		{
			name: "extractTimeData",
			run: func() {
				times := extractTimeData(nil)
				if times == nil {
					panic("returned nil slice (want non-nil empty)")
				}
				if len(times) != 0 {
					panic("returned non-empty slice on nil input")
				}
			},
		},
		{
			name: "extractColumnLineData",
			run: func() {
				ld := extractColumnLineData(nil, 1)
				if ld == nil {
					panic("returned nil slice (want non-nil empty)")
				}
				if len(ld) != 0 {
					panic("returned non-empty slice on nil input")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on nil dataset: %v", tc.name, r)
				}
			}()

			tc.run()
		})
	}
}

// TestChart_RendererSentinelErrors 確認 sentinel errors (ErrDatasetNil /
// ErrDatasetNoHeaders / ErrDatasetNoData) 真的被 chart-generator 入口使用,
// 並且 caller 透過 errors.Is 能識別 — 也就是 V7 報告所說「declared-but-unused」
// 狀態已被打破。
//
// 涵蓋三個 method (gui/chart_helpers.go 的真實 caller path):
//   - GenerateInteractiveChart
//   - RenderChartToWriter
//   - GenerateComparisonChart
func TestChart_RendererSentinelErrors(t *testing.T) {
	gen := NewEChartsGenerator()
	cfg := &InteractiveChartConfig{
		Title:           "sentinel test",
		SelectedColumns: []int{1},
		Width:           "800px",
		Height:          "600px",
	}

	tmpOut := filepath.Join(t.TempDir(), "out.html")

	// nil dataset → ErrDatasetNil
	t.Run("GenerateInteractiveChart_NilDataset", func(t *testing.T) {
		err := gen.GenerateInteractiveChart(nil, cfg, tmpOut)
		if err == nil {
			t.Fatalf("expected error on nil dataset")
		}
		if !errors.Is(err, ErrDatasetNil) {
			t.Errorf("expected ErrDatasetNil; got %v", err)
		}
	})

	t.Run("RenderChartToWriter_NilDataset", func(t *testing.T) {
		var buf bytes.Buffer
		err := gen.RenderChartToWriter(nil, *cfg, &buf)
		if err == nil {
			t.Fatalf("expected error on nil dataset")
		}
		if !errors.Is(err, ErrDatasetNil) {
			t.Errorf("expected ErrDatasetNil; got %v", err)
		}
	})

	t.Run("GenerateComparisonChart_NilDatasetInList", func(t *testing.T) {
		datasets := []*models.EMGDataset{nil}
		err := gen.GenerateComparisonChart(datasets, []string{"a"}, cfg, tmpOut)
		if err == nil {
			t.Fatalf("expected error on nil dataset in list")
		}
		if !errors.Is(err, ErrDatasetNil) {
			t.Errorf("expected ErrDatasetNil; got %v", err)
		}
	})

	t.Run("GenerateComparisonChart_EmptyDatasetsSlice", func(t *testing.T) {
		err := gen.GenerateComparisonChart(nil, nil, cfg, tmpOut)
		if err == nil {
			t.Fatalf("expected error on empty datasets slice")
		}
		if !errors.Is(err, ErrDatasetNoData) {
			t.Errorf("expected ErrDatasetNoData; got %v", err)
		}
	})

	// 空 Headers → ErrDatasetNoHeaders
	t.Run("RenderChartToWriter_EmptyHeaders", func(t *testing.T) {
		ds := &models.EMGDataset{Headers: []string{}, Data: nil}
		var buf bytes.Buffer
		err := gen.RenderChartToWriter(ds, *cfg, &buf)
		if err == nil {
			t.Fatalf("expected error on empty headers")
		}
		if !errors.Is(err, ErrDatasetNoHeaders) {
			t.Errorf("expected ErrDatasetNoHeaders; got %v", err)
		}
	})

	// 有 Headers 但無 Data → ErrDatasetNoData
	t.Run("RenderChartToWriter_NoData", func(t *testing.T) {
		ds := &models.EMGDataset{Headers: []string{"Time", "Ch1"}, Data: nil}
		var buf bytes.Buffer
		err := gen.RenderChartToWriter(ds, *cfg, &buf)
		if err == nil {
			t.Fatalf("expected error on no data")
		}
		if !errors.Is(err, ErrDatasetNoData) {
			t.Errorf("expected ErrDatasetNoData; got %v", err)
		}
	})
}
