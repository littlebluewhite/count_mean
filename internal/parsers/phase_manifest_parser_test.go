package parsers

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
)

func TestNewPhaseManifestParser(t *testing.T) {
	parser := NewPhaseManifestParser()
	assert.NotNil(t, parser)
}

func TestPhaseManifestParser_ParseFile(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		wantErr    bool
		checkData  func(*testing.T, []models.PhaseManifest)
	}{
		{
			name: "valid phase manifest file",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000
Subject2,motion2.csv,force2.anc,emg2.csv,120,1.100,2.100,3.100,4.100,5.100,160,6.100,7.100,210,8.100`,
			wantErr: false,
			checkData: func(t *testing.T, manifests []models.PhaseManifest) {
				assert.Len(t, manifests, 2)

				// 檢查第一個記錄（Batch T：float 欄位是 OptFloat）
				m1 := manifests[0]
				assert.Equal(t, "Subject1", m1.Subject)
				assert.Equal(t, "motion1.csv", m1.MotionFile)
				assert.Equal(t, "force1.anc", m1.ForceFile)
				assert.Equal(t, "emg1.csv", m1.EMGFile)
				assert.Equal(t, 100, m1.EMGMotionOffset)

				assert.Equal(t, models.MakeOpt(1.000), m1.PhasePoints.P0)
				assert.Equal(t, models.MakeOpt(5.000), m1.PhasePoints.C)
				assert.Equal(t, 150, m1.PhasePoints.D)
				assert.Equal(t, models.MakeOpt(8.000), m1.PhasePoints.L)

				// 檢查第二個記錄
				m2 := manifests[1]
				assert.Equal(t, "Subject2", m2.Subject)
				assert.Equal(t, 120, m2.EMGMotionOffset)
				assert.Equal(t, 210, m2.PhasePoints.O)
			},
		},
		{
			name: "file with empty values",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,NA,x,4.000,-,150,N/A,7.000,X,8.000`,
			wantErr: false,
			checkData: func(t *testing.T, manifests []models.PhaseManifest) {
				assert.Len(t, manifests, 1)

				// Batch T：「NA / x / - / N/A」→ NoOpt()（與 t=0 區分）。
				m := manifests[0]
				assert.Equal(t, models.MakeOpt(1.000), m.PhasePoints.P0)
				assert.Equal(t, models.NoOpt(), m.PhasePoints.P1) // NA -> NoOpt
				assert.Equal(t, models.NoOpt(), m.PhasePoints.P2) // x -> NoOpt
				assert.Equal(t, models.MakeOpt(4.000), m.PhasePoints.S)
				assert.Equal(t, models.NoOpt(), m.PhasePoints.C) // - -> NoOpt
				assert.Equal(t, 150, m.PhasePoints.D)
				assert.Equal(t, models.NoOpt(), m.PhasePoints.T0) // N/A -> NoOpt
				assert.Equal(t, models.MakeOpt(7.000), m.PhasePoints.T)
				assert.Equal(t, 0, m.PhasePoints.O) // X -> 0 (int sentinel)
				assert.Equal(t, models.MakeOpt(8.000), m.PhasePoints.L)
			},
		},
		{
			name: "file with spaces",
			csvContent: "  Subject  ,  MotionFile  ,  ForceFile  ,  EMGFile  ,  " +
				"EMGMotionOffset  ,  P0  ,  P1  ,  P2  ,  S  ,  C  ,  D  ,  T0  ,  T  ,  O  ,  L  \n" +
				"  Subject1  ,  motion1.csv  ,  force1.anc  ,  emg1.csv  ,  100  ,  " +
				"1.000  ,  2.000  ,  3.000  ,  4.000  ,  5.000  ,  150  ,  6.000  ,  7.000  ,  200  ,  8.000  ",
			wantErr: false,
			checkData: func(t *testing.T, manifests []models.PhaseManifest) {
				assert.Len(t, manifests, 1)
				m := manifests[0]
				assert.Equal(t, "Subject1", m.Subject)
				assert.Equal(t, "motion1.csv", m.MotionFile)
			},
		},
		{
			name:       "empty file",
			csvContent: "",
			wantErr:    true,
		},
		{
			name:       "file with only header",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L`,
			wantErr:    false,
			checkData: func(t *testing.T, manifests []models.PhaseManifest) {
				assert.Len(t, manifests, 0)
			},
		},
		{
			name: "file with insufficient columns",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset
Subject1,motion1.csv,force1.anc,emg1.csv,100`,
			wantErr: true,
		},
		{
			name: "file with invalid EMGMotionOffset",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,invalid,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000`,
			wantErr: true,
		},
		{
			name: "file with invalid phase point values",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,invalid,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000`,
			wantErr: true,
		},
		{
			name: "file with invalid motion index",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,invalid,6.000,7.000,200,8.000`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_manifest_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			// 測試解析
			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tt.checkData != nil {
				tt.checkData(t, manifests)
			}
		})
	}
}

func TestPhaseManifestParser_ParseFile_FileNotFound(t *testing.T) {
	parser := NewPhaseManifestParser()
	_, err := parser.ParseFile("nonexistent_file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "無法開啟檔案")
}

func TestGetPhaseValue(t *testing.T) {
	// Batch T：float 欄位用 OptFloat 包裝，D/O 仍是 int（0=未提供 sentinel）。
	phasePoints := models.PhasePoints{
		P0: models.MakeOpt(1.0),
		P1: models.MakeOpt(2.0),
		P2: models.MakeOpt(3.0),
		S:  models.MakeOpt(4.0),
		C:  models.MakeOpt(5.0),
		D:  150,
		T0: models.MakeOpt(6.0),
		T:  models.MakeOpt(7.0),
		O:  200,
		L:  models.MakeOpt(8.0),
	}

	tests := []struct {
		phaseName     models.PhasePoint
		expectedValue float64
		expectedIsIdx bool
		wantErr       bool
	}{
		{"P0", 1.0, false, false},
		{"P1", 2.0, false, false},
		{"P2", 3.0, false, false},
		{"S", 4.0, false, false},
		{"C", 5.0, false, false},
		{"D", 150.0, true, false}, // motion index
		{"T0", 6.0, false, false},
		{"T", 7.0, false, false},
		{"O", 200.0, true, false}, // motion index
		{"L", 8.0, false, false},
		{"UNKNOWN", 0.0, false, true}, // 未知分期點
	}

	for _, tt := range tests {
		t.Run(string(tt.phaseName), func(t *testing.T) {
			opt, isIdx, err := GetPhaseValue(&phasePoints, tt.phaseName)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			// Batch T：GetPhaseValue 回 OptFloat；已標定的合法值應 Set=true 帶實際值。
			v, ok := opt.Get()
			assert.True(t, ok, "expected Set=true for %s", tt.phaseName)
			assert.Equal(t, tt.expectedValue, v)
			assert.Equal(t, tt.expectedIsIdx, isIdx)
		})
	}
}

func TestValidatePhaseManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest models.PhaseManifest
		wantErr  bool
		errField string
		errMsg   string
	}{
		{
			name: "valid manifest",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(1.0),
					P1: models.MakeOpt(2.0),
					P2: models.MakeOpt(3.0),
					S:  models.MakeOpt(4.0),
					C:  models.MakeOpt(5.0),
					D:  150,
					T0: models.MakeOpt(6.0),
					T:  models.MakeOpt(7.0),
					O:  200,
					L:  models.MakeOpt(8.0),
				},
			},
			wantErr: false,
		},
		{
			name: "empty subject",
			manifest: models.PhaseManifest{
				Subject:    "",
				MotionFile: "motion1.csv",
				ForceFile:  "force1.anc",
				EMGFile:    "emg1.csv",
			},
			wantErr:  true,
			errField: "Subject",
			errMsg:   "主題名稱不能為空",
		},
		{
			name: "empty motion file",
			manifest: models.PhaseManifest{
				Subject:    "Subject1",
				MotionFile: "",
				ForceFile:  "force1.anc",
				EMGFile:    "emg1.csv",
			},
			wantErr:  true,
			errField: "MotionFile",
			errMsg:   "Motion檔案名不能為空",
		},
		{
			name: "empty force file",
			manifest: models.PhaseManifest{
				Subject:    "Subject1",
				MotionFile: "motion1.csv",
				ForceFile:  "",
				EMGFile:    "emg1.csv",
			},
			wantErr:  true,
			errField: "ForceFile",
			errMsg:   "力板檔案名不能為空",
		},
		{
			name: "empty EMG file",
			manifest: models.PhaseManifest{
				Subject:    "Subject1",
				MotionFile: "motion1.csv",
				ForceFile:  "force1.anc",
				EMGFile:    "",
			},
			wantErr:  true,
			errField: "EMGFile",
			errMsg:   "EMG檔案名不能為空",
		},
		{
			name: "negative EMG motion offset",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: -10,
			},
			wantErr:  true,
			errField: "EMGMotionOffset",
			errMsg:   "EMG Motion Offset 不能為負數",
		},
		{
			name: "invalid phase time order",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(3.0), // P0 晚於 P1，順序錯誤
					P1: models.MakeOpt(2.0),
					P2: models.MakeOpt(4.0),
				},
			},
			wantErr:  true,
			errField: "PhasePoints",
			errMsg:   "P1 時間 (2.000) 不能早於 P0 時間 (3.000)",
		},
		{
			// Batch T 後語意：P1 未提供 (NoOpt) 跳過 monotonicity，與舊「P1=0」契約等價。
			name: "valid manifest with unset values",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 0,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(1.0),
					P1: models.NoOpt(), // 未提供，不參與順序檢查
					P2: models.MakeOpt(3.0),
				},
			},
			wantErr: false,
		},
		{
			name: "manifest with only some phase points",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(1.0),
					P2: models.MakeOpt(3.0), // P1 未提供（NoOpt 零值），不參與檢查
					S:  models.MakeOpt(4.0),
				},
			},
			wantErr: false,
		},
		{
			// Batch T 新增：start=0 + divide=1 manifest 是合法的 t=0 真實時間，
			// 不再被誤判為「未提供」。OptFloat 重構的核心 regression test。
			name: "Batch T regression: P0=0 (Set=true) is legitimate t=0 not 'unset'",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(0.0), // start=0
					P1: models.MakeOpt(1.0), // divide=1
					P2: models.MakeOpt(2.0),
				},
			},
			wantErr: false,
		},
		// ValidatePhaseManifest 對 struct-init 的 caller 加 NaN/Inf 雙保險
		// （parseFloat 已 reject，但 caller 可能繞過 parser 直接構造 PhaseManifest）
		// Batch T：caller 須包成 OptFloat — Set=true 才會走 NaN/Inf 檢查。
		{
			name: "NaN in P0 rejected",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					P0: models.MakeOpt(math.NaN()),
				},
			},
			wantErr:  true,
			errField: "PhasePoints.P0",
			errMsg:   "非法數值",
		},
		{
			name: "+Inf in T rejected",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					T: models.MakeOpt(math.Inf(1)),
				},
			},
			wantErr:  true,
			errField: "PhasePoints.T",
			errMsg:   "非法數值",
		},
		// P1-A4-3 (D/O)：motion-index 對 (D=下蹲結束, O=展體轉間) 必須 D < O。
		// D 和 O 都是 int (motion-index)，跟 force-time monotonicity 走獨立 list
		// （單位不同，混在一起比較沒有物理意義）。
		{
			name: "motion index D > O rejected",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					D: 300, // 下蹲結束在 index 300
					O: 200, // 展體轉間在 index 200 — 不可能早於 D
				},
			},
			wantErr:  true,
			errField: "PhasePoints",
			errMsg:   "O index (200) 不能早於 D index (300)",
		},
		{
			name: "motion index D == O accepted (same semantics as force-time: only strict less-than rejected)",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					D: 200,
					O: 200, // 與 force-time monotonicity 一致：< 才 reject
				},
			},
			wantErr: false,
		},
		{
			name: "motion index D < O accepted",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					D: 150,
					O: 200,
				},
			},
			wantErr: false,
		},
		{
			name: "motion index D == 0 (未提供) skips check",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					D: 0,   // 未提供
					O: 200, // 已提供
				},
			},
			wantErr: false,
		},
		{
			name: "motion index O == 0 (未提供) skips check",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					D: 150, // 已提供
					O: 0,   // 未提供
				},
			},
			wantErr: false,
		},
		{
			name: "motion index 大 D 與 force-time 不互相干擾",
			// D=5000 > O=4000 但 force-time S<C<T 完全合法。應該因 D/O 而 reject，
			// 不應該因 force-time list 把 motion-index 混進去而誤判 force-time 有問題。
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 100,
				PhasePoints: models.PhasePoints{
					S: models.MakeOpt(1.0),
					C: models.MakeOpt(2.0),
					T: models.MakeOpt(3.0),
					D: 5000, // larger than O
					O: 4000,
				},
			},
			wantErr:  true,
			errField: "PhasePoints",
			errMsg:   "O index (4000) 不能早於 D index (5000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePhaseManifest(&tt.manifest)

			if tt.wantErr {
				assert.Error(t, err)

				if validationErr, ok := errors.AsType[models.PhaseSyncValidationError](err); ok {
					assert.Equal(t, tt.errField, validationErr.Field)
					assert.Contains(t, validationErr.Message, tt.errMsg)
				} else {
					t.Errorf("Expected PhaseSyncValidationError, got %T", err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseManifestParser_parseFloat(t *testing.T) {
	// Batch T：expectedOpt 取代原本 expected float64。空值類 → NoOpt()，
	// 合法數值 → MakeOpt(v)，包括「0」也是 MakeOpt(0)（合法 t=0），
	// 這就是 OptFloat 引入的核心區分。
	tests := []struct {
		name        string
		value       string
		fieldName   string
		expectedOpt models.OptFloat
		wantErr     bool
	}{
		{"normal value", "123.45", "test", models.MakeOpt(123.45), false},
		// Batch T regression: "0" 是合法 t=0，與「空字串/NA/-」嚴格區分。
		{"zero value is Set=true Value=0", "0", "test", models.MakeOpt(0.0), false},
		// NOT IMPLEMENTED：負值 phase point 為「校準前時間偏移」合法設計（見
		// muscle_ratio analyzer_test.go TestAnalyze_NegativeTime_Handled）。
		{"negative value allowed (校準偏移)", "-123.45", "test", models.MakeOpt(-123.45), false},
		// P1-A10-2 + 超過 maxReasonablePhase（|v| > 86400 = 1 day）拒絕。
		// 1e10 / -1e10 仍然超過上限，行為與 修補後一致。
		{"too large positive value", "1e10", "test", models.NoOpt(), true},
		{"too large negative value", "-1e10", "test", models.NoOpt(), true},
		// Batch T：空值類 → NoOpt（與「t=0」嚴格區分）。
		{"empty string", "", "test", models.NoOpt(), false},
		{"NA value", "NA", "test", models.NoOpt(), false},
		{"N/A value", "N/A", "test", models.NoOpt(), false},
		{"x value", "x", "test", models.NoOpt(), false},
		{"X value", "X", "test", models.NoOpt(), false},
		{"dash value", "-", "test", models.NoOpt(), false},
		{"value with spaces", "  123.45  ", "test", models.MakeOpt(123.45), false},
		{"invalid value", "abc", "test", models.NoOpt(), true},
		{"mixed invalid", "12.3abc", "test", models.NoOpt(), true},
		// NaN/Inf 一律拒絕 — phase manifest 時間欄位含 NaN/Inf 會 bypass
		// monotonicity check（NaN 比較永遠 false），造成下游 silent NaN-poisoning
		{"NaN literal", "NaN", "test", models.NoOpt(), true},
		{"+Inf literal", "+Inf", "test", models.NoOpt(), true},
		{"-Inf literal", "-Inf", "test", models.NoOpt(), true},
		{"Infinity literal", "Infinity", "test", models.NoOpt(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 由於 parseFloat 是私有函數，我們通過創建包含該值的記錄來間接測試
			csvContent := "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
			csvContent += "Subject1,motion1.csv,force1.anc,emg1.csv,100," + tt.value + ",2,3,4,5,150,6,7,200,8"

			tmpFile, err := os.CreateTemp(t.TempDir(), "test_float_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, manifests, 1)
				assert.Equal(t, tt.expectedOpt, manifests[0].PhasePoints.P0)
			}
		})
	}
}

func TestPhaseManifestParser_parseInt(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		expected  int
		wantErr   bool
	}{
		{"normal value", "123", "test", 123, false},
		{"zero value", "0", "test", 0, false},
		{"negative value", "-123", "test", -123, false},
		{"empty string", "", "test", 0, false},
		{"NA value", "NA", "test", 0, false},
		{"N/A value", "N/A", "test", 0, false},
		{"x value", "x", "test", 0, false},
		{"X value", "X", "test", 0, false},
		{"dash value", "-", "test", 0, false},
		{"value with spaces", "  123  ", "test", 123, false},
		{"invalid value", "abc", "test", 0, true},
		{"float value", "123.45", "test", 0, true},
		{"mixed invalid", "12abc", "test", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 由於 parseInt 是私有函數，我們通過創建包含該值的記錄來間接測試
			csvContent := "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
			csvContent += "Subject1,motion1.csv,force1.anc,emg1.csv,100,1,2,3,4,5," + tt.value + ",6,7,200,8"

			tmpFile, err := os.CreateTemp(t.TempDir(), "test_int_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, manifests, 1)
				assert.Equal(t, tt.expected, manifests[0].PhasePoints.D)
			}
		})
	}
}

func TestPhaseManifestParser_Integration(t *testing.T) {
	// 集成測試：創建完整的分期總檔案並測試完整流程
	t.Run("complete phase manifest file parsing and validation", func(t *testing.T) {
		manifestContent := `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject001,motion001.csv,force001.anc,emg001.csv,125,0.500,1.200,2.100,2.850,3.200,780,4.100,4.650,925,5.200
Subject002,motion002.csv,force002.anc,emg002.csv,110,0.480,1.150,2.050,2.800,3.150,760,4.050,4.600,910,5.150
Subject003,motion003.csv,force003.anc,emg003.csv,130,NA,1.250,x,2.900,3.250,780,-,4.700,X,5.250`

		// 創建臨時文件
		tmpFile, err := os.CreateTemp(t.TempDir(), "integration_test_*.csv")
		require.NoError(t, err)

		_, err = tmpFile.WriteString(manifestContent)
		require.NoError(t, err)
		tmpFile.Close()

		// 解析文件
		parser := NewPhaseManifestParser()
		manifests, err := parser.ParseFile(tmpFile.Name())
		require.NoError(t, err)

		// 檢查解析結果
		assert.Len(t, manifests, 3)

		// 驗證第一條記錄（Batch T：float 欄位是 OptFloat）
		m1 := manifests[0]
		assert.Equal(t, "Subject001", m1.Subject)
		assert.Equal(t, 125, m1.EMGMotionOffset)
		assert.Equal(t, models.MakeOpt(0.500), m1.PhasePoints.P0)
		assert.Equal(t, 780, m1.PhasePoints.D)
		assert.Equal(t, 925, m1.PhasePoints.O)

		// 驗證第三條記錄（Batch T：空值 → NoOpt，與 t=0 區分）
		m3 := manifests[2]
		assert.Equal(t, "Subject003", m3.Subject)
		assert.Equal(t, models.NoOpt(), m3.PhasePoints.P0)        // NA -> NoOpt
		assert.Equal(t, models.NoOpt(), m3.PhasePoints.P2)        // x -> NoOpt
		assert.Equal(t, models.NoOpt(), m3.PhasePoints.T0)        // - -> NoOpt
		assert.Equal(t, 0, m3.PhasePoints.O)                      // X -> 0 (int sentinel)
		assert.Equal(t, 780, m3.PhasePoints.D)                    // 正常值
		assert.Equal(t, models.MakeOpt(1.250), m3.PhasePoints.P1) // 正常值
		assert.Equal(t, models.MakeOpt(2.900), m3.PhasePoints.S)  // 正常值

		// 驗證所有記錄
		for i := range manifests {
			err := ValidatePhaseManifest(&manifests[i])
			assert.NoError(t, err, "Validation failed for manifest %d", i+1)
		}

		// 測試分期點值獲取（Batch T：GetPhaseValue 回 OptFloat）
		opt, isIndex, err := GetPhaseValue(&m1.PhasePoints, models.PhaseD)
		require.NoError(t, err)
		v, ok := opt.Get()
		assert.True(t, ok)
		assert.Equal(t, 780.0, v)
		assert.True(t, isIndex)

		opt, isIndex, err = GetPhaseValue(&m1.PhasePoints, models.PhaseS)
		require.NoError(t, err)
		v, ok = opt.Get()
		assert.True(t, ok)
		assert.Equal(t, 2.850, v)
		assert.False(t, isIndex)
	})
}

// 負值 D/O motion-index 不可被 `> 0` sentinel silently skip。
// 動作 frame index 必為正整數;負值來自惡意 / 損毀 manifest,過去
// `parseInt("-150")` 合法但 `validateMotionIndexOrder` 用 `> 0` 過濾跳過 →
// 下游 phase_sync 拿到負 index 算 silent miscalc。修法後負值應 fail-fast。
func TestPhaseManifestParser_ValidateMotionIndex_NegativeRejected(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		errSubstr  string
	}{
		{
			name: "negative D rejected",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,-150,6.000,7.000,200,8.000`,
			errSubstr: "D",
		},
		{
			name: "negative O rejected",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,-50,8.000`,
			errSubstr: "O",
		},
		{
			name: "both D and O negative rejected",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,-150,6.000,7.000,-50,8.000`,
			errSubstr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_neg_motion_*.csv")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())
			require.NoError(t, err, "parse 階段可成功(parseInt 允許負值),validate 階段才 reject")
			require.Len(t, manifests, 1)

			// 真正的 reject 在 ValidatePhaseManifest — 修法後 motion-index 負值
			// 必須 fail-fast,不可被 `> 0` sentinel 當成「未提供」靜悄悄略過。
			err = ValidatePhaseManifest(&manifests[0])
			require.Error(t, err, "motion-index 負值必須 reject,不可 silent skip")

			var validationErr models.PhaseSyncValidationError
			require.True(t, errors.As(err, &validationErr),
				"應該回 PhaseSyncValidationError,目前 err=%v", err)
			assert.Equal(t, "PhasePoints", validationErr.Field)
			if tt.errSubstr != "" {
				assert.Contains(t, validationErr.Message, tt.errSubstr,
					"錯誤訊息要指出哪個欄位 (%s)", tt.errSubstr)
			}
		})
	}
}

// 補強:motionIndexOpt 對負值的契約 — 經 ValidatePhaseManifest reject 後
// 不會走到 motionIndexOpt,但對於繞過 parser 直接 struct-init 的 caller(例如
// 測試 helper),motionIndexOpt 對負值也回 NoOpt 做防禦性兜底,避免下游
// motionIndexOpt(-150) → MakeOpt(-150.0) 把負值當合法時間流入 phase_sync。
func TestPhaseManifestParser_MotionIndexOpt_NegativeIsNoOpt(t *testing.T) {
	// 0 = 未提供 → NoOpt;正值 → MakeOpt;負值(非法) → NoOpt(defense-in-depth)
	points := models.PhasePoints{D: -150}
	opt, isIdx, err := GetPhaseValue(&points, models.PhaseD)
	require.NoError(t, err)
	require.True(t, isIdx, "D 是 motion-index")
	_, ok := opt.Get()
	assert.False(t, ok, "負 motion-index 必須回 NoOpt,而非 MakeOpt(-150)")
}

// absurd motion-index 必須在 parse 階段就被 reject,不可進到
// validateMotionIndexOrder 後依賴 monotonicity 抓。
//
// 攻擊向量(V5):`D=1e15` + `O=0`(sentinel) — 因 O=0 被當「未提供」跳過
// monotonicity check,只有 D 一個 motion-index 點，single-element list
// monotonicity 永遠 pass。下游 phase_sync sliding window / chart axis 拿到
// 1e15 frame index 直接 OOM / hang(DoS)。
//
// 修法:parseInt 對 motion-index path 加 MaxReasonableMotionIndex = 1e9 上限
// (≈ 250Hz 取樣下 46 天連續錄製,涵蓋任何合理動作 capture session)。
func TestParseManifest_AbsurdMotionIndex_Rejected(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		field      string // 預期錯誤訊息含哪個欄位名
	}{
		{
			// V5 重現:D=1e15 + O=0(sentinel)組合
			// monotonicity 不會抓 — O=0 視為「未提供」,單元素 list 永遠單調。
			name: "D=1e15 with O=0 sentinel (V5 攻擊向量)",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,1000000000000000,6.000,7.000,0,8.000`,
			field: "D",
		},
		{
			name: "O=1e15 with D=0 sentinel",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,0,6.000,7.000,1000000000000000,8.000`,
			field: "O",
		},
		{
			// 邊界 + 1 — 剛剛好超過 1e9 必須 reject
			name: "D just above MaxReasonableMotionIndex (1e9+1)",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,1000000001,6.000,7.000,0,8.000`,
			field: "D",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_absurd_motion_*.csv")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			parser := NewPhaseManifestParser()
			_, err = parser.ParseFile(tmpFile.Name())
			require.Error(t, err, "absurd motion-index 必須在 parse 階段被 reject,不可依賴 monotonicity")
			assert.Contains(t, err.Error(), tt.field,
				"錯誤訊息要指出哪個 motion-index 欄位 (%s)", tt.field)
		})
	}
}

// 邊界:正常值 + 上限值 (1e9 整數) 必須通過,不可誤殺合法輸入。
func TestParseManifest_MotionIndex_BoundaryAccepted(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
	}{
		{
			name: "D = MaxReasonableMotionIndex (1e9) accepted",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,1000000000,6.000,7.000,0,8.000`,
		},
		{
			name: "typical values (D=150, O=200) accepted",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_boundary_motion_*.csv")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())
			require.NoError(t, err, "上限內的合法 motion-index 不可被誤殺")
			require.Len(t, manifests, 1)
		})
	}
}

// TestPhaseManifestParser_ParseFile_StripsBOM 釘住 Wave 6 PR1 BOM 對稱補完:
// ParseFile 過去直接 csv.NewReader(file) — 不包 bufio + 不剝 BOM。Excel 匯出的
// UTF-8 manifest CSV 帶 0xEF 0xBB 0xBF 前綴時,records[1][0] 會帶 U+FEFF,
// Subject 比對失敗(整個 LoadManifestSubjects 流程因為「找不到 subject」中斷)。
// 修法:bufio.NewReaderSize + csvutil.PeekBOM,對稱於 csv_handler.go / large_file_handler.go。
func TestPhaseManifestParser_ParseFile_StripsBOM(t *testing.T) {
	parser := NewPhaseManifestParser()

	bom := []byte{0xEF, 0xBB, 0xBF}
	csvBody := "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"SubjectBOM,motion.csv,force.anc,emg.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000\n"
	content := append(append([]byte{}, bom...), []byte(csvBody)...)

	tmpFile, err := os.CreateTemp(t.TempDir(), "manifest_bom_*.csv")
	require.NoError(t, err)
	_, err = tmpFile.Write(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	manifests, err := parser.ParseFile(tmpFile.Name())
	require.NoError(t, err, "ParseFile 失敗 — BOM 未被剝除可能造成欄位數錯誤")
	require.Len(t, manifests, 1)
	assert.Equal(t, "SubjectBOM", manifests[0].Subject,
		"Subject 不能含 U+FEFF — manifest 必須在剝除 BOM 後 parse")
}

// TestPhaseManifestParser_MuscleRatioFile_VersionMatrix 釘住 V.14 manifest schema
// 升級的核心契約 — 新增第 16 欄 MuscleRatioFile（filename only、相對 DataFolder、可空）
// 必須對既有 V.10 / V.13 manifest（15 欄）保持 happy path:
//
//	1) V.10 fixture（15 欄,header 不含 MuscleRatioFile）→ 解析成功,MuscleRatioFile == ""
//	2) V.14 fixture（16 欄,header 含 MuscleRatioFile）  → 解析成功,MuscleRatioFile == "muscle_ratio_subject1.csv"
//	3) V.14 fixture with empty MuscleRatioFile（16 欄但第 16 欄為空字串）→ MuscleRatioFile == ""
//
// 同時:PhaseManifestMinFields 必須維持 15(新欄位純加值,不收緊既有契約)。
func TestPhaseManifestParser_MuscleRatioFile_VersionMatrix(t *testing.T) {
	// 釘住常數值 — PhaseManifestMinFields 必須維持 15,V.10 / V.13 仍 happy path。
	require.Equal(t, 15, PhaseManifestMinFields,
		"PhaseManifestMinFields 必須維持 15;V.14 新增第 16 欄是純加值,不可收緊既有 V.10/V.13 契約")

	tests := []struct {
		name                    string
		csvContent              string
		expectedMuscleRatioFile string
	}{
		{
			name: "V.10 manifest (15 cols, no MuscleRatioFile header) -> empty",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000`,
			expectedMuscleRatioFile: "",
		},
		{
			name: "V.14 manifest (16 cols) -> populated",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000,muscle_ratio_subject1.csv`,
			expectedMuscleRatioFile: "muscle_ratio_subject1.csv",
		},
		{
			name: "V.14 manifest with empty MuscleRatioFile cell -> empty",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000,`,
			expectedMuscleRatioFile: "",
		},
		{
			name: "V.14 manifest with whitespace around MuscleRatioFile -> trimmed",
			csvContent: `Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile
Subject1,motion1.csv,force1.anc,emg1.csv,100,1.000,2.000,3.000,4.000,5.000,150,6.000,7.000,200,8.000,  muscle_ratio_subject1.csv  `,
			expectedMuscleRatioFile: "muscle_ratio_subject1.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_v14_*.csv")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			parser := NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())
			require.NoError(t, err, "V.10 / V.14 manifest 皆必須解析成功")
			require.Len(t, manifests, 1)

			assert.Equal(t, tt.expectedMuscleRatioFile, manifests[0].MuscleRatioFile,
				"MuscleRatioFile 解析結果不符預期")

			// V.10 / V.14 都應保留既有 happy path — 隨手驗一下其他欄位沒被破壞。
			assert.Equal(t, "Subject1", manifests[0].Subject)
			assert.Equal(t, "motion1.csv", manifests[0].MotionFile)
			assert.Equal(t, models.MakeOpt(1.000), manifests[0].PhasePoints.P0)
			assert.Equal(t, 150, manifests[0].PhasePoints.D)
		})
	}
}

// TestPhaseManifest_StripsBOM對齊 ANC 版本 TestANCParser_StripsBOM 的契約:
// (1) BOM-prefixed manifest CSV 必須能順利 ParseFile,不報錯;
// (2) Subject / MotionFile / ForceFile / EMGFile 都不能殘留 BOM bytes;
// (3) 多列 BOM 檔案 — 只有 file 起點有 BOM,中間任何欄位都不該帶 0xEF 0xBB 0xBF。
//
// 與既有的 *_ParseFile_StripsBOM(單列、單欄驗證)互補:這份明確列出 ANC 對應的
// 「不含 BOM bytes」契約,日後若 PeekBOM / BOMBytes 改動,此 test 會與
// TestANCParser_StripsBOM 同步亮紅燈而非只有單側炸掉。
func TestPhaseManifest_StripsBOM(t *testing.T) {
	parser := NewPhaseManifestParser()

	const csvBody = "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"Subject1,motion1.csv,force1.anc,emg1.csv,100,1.0,2.0,3.0,4.0,5.0,150,6.0,7.0,200,8.0\n" +
		"Subject2,motion2.csv,force2.anc,emg2.csv,120,1.1,2.1,3.1,4.1,5.1,160,6.1,7.1,210,8.1\n"

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest_bom_multi.csv")
	f, err := os.Create(path) //nolint:gosec // t.TempDir
	require.NoError(t, err)
	_, err = f.Write(csvutil.BOMBytes())
	require.NoError(t, err)
	_, err = f.WriteString(csvBody)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	manifests, err := parser.ParseFile(path)
	require.NoError(t, err)
	require.Len(t, manifests, 2)

	bom := csvutil.BOMBytes()
	for i, m := range manifests {
		for _, field := range []struct {
			name  string
			value string
		}{
			{"Subject", m.Subject},
			{"MotionFile", m.MotionFile},
			{"ForceFile", m.ForceFile},
			{"EMGFile", m.EMGFile},
		} {
			assert.Falsef(t, bytes.Contains([]byte(field.value), bom),
				"manifest[%d].%s = %q 不可含 UTF-8 BOM", i, field.name, field.value)
		}
	}

	// Spot-check Subject 字面 — 對齊 TestANCParser_StripsBOM 用 `[]string{"Fx","Fy"}` 比對。
	assert.Equal(t, "Subject1", manifests[0].Subject)
	assert.Equal(t, "Subject2", manifests[1].Subject)
}
