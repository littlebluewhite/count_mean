package parsers

import (
	"os"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/parsers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPhaseManifestParser(t *testing.T) {
	parser := parsers.NewPhaseManifestParser()
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

				// 檢查第一個記錄
				m1 := manifests[0]
				assert.Equal(t, "Subject1", m1.Subject)
				assert.Equal(t, "motion1.csv", m1.MotionFile)
				assert.Equal(t, "force1.anc", m1.ForceFile)
				assert.Equal(t, "emg1.csv", m1.EMGFile)
				assert.Equal(t, 100, m1.EMGMotionOffset)

				assert.Equal(t, 1.000, m1.PhasePoints.P0)
				assert.Equal(t, 5.000, m1.PhasePoints.C)
				assert.Equal(t, 150, m1.PhasePoints.D)
				assert.Equal(t, 8.000, m1.PhasePoints.L)

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

				m := manifests[0]
				assert.Equal(t, 1.000, m.PhasePoints.P0)
				assert.Equal(t, 0.0, m.PhasePoints.P1) // NA -> 0
				assert.Equal(t, 0.0, m.PhasePoints.P2) // x -> 0
				assert.Equal(t, 4.000, m.PhasePoints.S)
				assert.Equal(t, 0.0, m.PhasePoints.C) // - -> 0
				assert.Equal(t, 150, m.PhasePoints.D)
				assert.Equal(t, 0.0, m.PhasePoints.T0) // N/A -> 0
				assert.Equal(t, 7.000, m.PhasePoints.T)
				assert.Equal(t, 0, m.PhasePoints.O) // X -> 0
				assert.Equal(t, 8.000, m.PhasePoints.L)
			},
		},
		{
			name: "file with spaces",
			csvContent: `  Subject  ,  MotionFile  ,  ForceFile  ,  EMGFile  ,  EMGMotionOffset  ,  P0  ,  P1  ,  P2  ,  S  ,  C  ,  D  ,  T0  ,  T  ,  O  ,  L  
  Subject1  ,  motion1.csv  ,  force1.anc  ,  emg1.csv  ,  100  ,  1.000  ,  2.000  ,  3.000  ,  4.000  ,  5.000  ,  150  ,  6.000  ,  7.000  ,  200  ,  8.000  `,
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
			tmpFile, err := os.CreateTemp("", "test_manifest_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			// 測試解析
			parser := parsers.NewPhaseManifestParser()
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
	parser := parsers.NewPhaseManifestParser()
	_, err := parser.ParseFile("nonexistent_file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "無法開啟檔案")
}

func TestGetPhaseValue(t *testing.T) {
	phasePoints := models.PhasePoints{
		P0: 1.0,
		P1: 2.0,
		P2: 3.0,
		S:  4.0,
		C:  5.0,
		D:  150,
		T0: 6.0,
		T:  7.0,
		O:  200,
		L:  8.0,
	}

	tests := []struct {
		phaseName     string
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
		t.Run(tt.phaseName, func(t *testing.T) {
			value, isIdx, err := parsers.GetPhaseValue(phasePoints, tt.phaseName)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedValue, value)
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
					P0: 1.0,
					P1: 2.0,
					P2: 3.0,
					S:  4.0,
					C:  5.0,
					D:  150,
					T0: 6.0,
					T:  7.0,
					O:  200,
					L:  8.0,
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
					P0: 3.0, // P0 晚於 P1，順序錯誤
					P1: 2.0,
					P2: 4.0,
				},
			},
			wantErr:  true,
			errField: "PhasePoints",
			errMsg:   "P1 時間 (2.000) 不能早於 P0 時間 (3.000)",
		},
		{
			name: "valid manifest with zero values",
			manifest: models.PhaseManifest{
				Subject:         "Subject1",
				MotionFile:      "motion1.csv",
				ForceFile:       "force1.anc",
				EMGFile:         "emg1.csv",
				EMGMotionOffset: 0,
				PhasePoints: models.PhasePoints{
					P0: 1.0,
					P1: 0.0, // 零值會被忽略，不參與順序檢查
					P2: 3.0,
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
					P0: 1.0,
					P2: 3.0, // P1 為 0，不參與檢查
					S:  4.0,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parsers.ValidatePhaseManifest(tt.manifest)

			if tt.wantErr {
				assert.Error(t, err)
				if validationErr, ok := err.(models.ValidationError); ok {
					assert.Equal(t, tt.errField, validationErr.Field)
					assert.Contains(t, validationErr.Message, tt.errMsg)
				} else {
					t.Errorf("Expected ValidationError, got %T", err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseManifestParser_parseFloat(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		expected  float64
		wantErr   bool
	}{
		{"normal value", "123.45", "test", 123.45, false},
		{"zero value", "0", "test", 0.0, false},
		{"negative value", "-123.45", "test", -123.45, false},
		{"empty string", "", "test", 0.0, false},
		{"NA value", "NA", "test", 0.0, false},
		{"N/A value", "N/A", "test", 0.0, false},
		{"x value", "x", "test", 0.0, false},
		{"X value", "X", "test", 0.0, false},
		{"dash value", "-", "test", 0.0, false},
		{"value with spaces", "  123.45  ", "test", 123.45, false},
		{"invalid value", "abc", "test", 0.0, true},
		{"mixed invalid", "12.3abc", "test", 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 由於 parseFloat 是私有函數，我們通過創建包含該值的記錄來間接測試
			csvContent := "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
			csvContent += "Subject1,motion1.csv,force1.anc,emg1.csv,100," + tt.value + ",2,3,4,5,150,6,7,200,8"

			tmpFile, err := os.CreateTemp("", "test_float_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			parser := parsers.NewPhaseManifestParser()
			manifests, err := parser.ParseFile(tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, manifests, 1)
				assert.Equal(t, tt.expected, manifests[0].PhasePoints.P0)
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

			tmpFile, err := os.CreateTemp("", "test_int_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			parser := parsers.NewPhaseManifestParser()
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
		tmpFile, err := os.CreateTemp("", "integration_test_*.csv")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(manifestContent)
		require.NoError(t, err)
		tmpFile.Close()

		// 解析文件
		parser := parsers.NewPhaseManifestParser()
		manifests, err := parser.ParseFile(tmpFile.Name())
		require.NoError(t, err)

		// 檢查解析結果
		assert.Len(t, manifests, 3)

		// 驗證第一條記錄
		m1 := manifests[0]
		assert.Equal(t, "Subject001", m1.Subject)
		assert.Equal(t, 125, m1.EMGMotionOffset)
		assert.Equal(t, 0.500, m1.PhasePoints.P0)
		assert.Equal(t, 780, m1.PhasePoints.D)
		assert.Equal(t, 925, m1.PhasePoints.O)

		// 驗證第三條記錄（包含空值）
		m3 := manifests[2]
		assert.Equal(t, "Subject003", m3.Subject)
		assert.Equal(t, 0.0, m3.PhasePoints.P0)   // NA -> 0
		assert.Equal(t, 0.0, m3.PhasePoints.P2)   // x -> 0
		assert.Equal(t, 0.0, m3.PhasePoints.T0)   // - -> 0
		assert.Equal(t, 0, m3.PhasePoints.O)      // X -> 0
		assert.Equal(t, 780, m3.PhasePoints.D)    // 正常值
		assert.Equal(t, 1.250, m3.PhasePoints.P1) // 正常值
		assert.Equal(t, 2.900, m3.PhasePoints.S)  // 正常值

		// 驗證所有記錄
		for i, manifest := range manifests {
			err := parsers.ValidatePhaseManifest(manifest)
			assert.NoError(t, err, "Validation failed for manifest %d", i+1)
		}

		// 測試分期點值獲取
		value, isIndex, err := parsers.GetPhaseValue(m1.PhasePoints, "D")
		require.NoError(t, err)
		assert.Equal(t, 780.0, value)
		assert.True(t, isIndex)

		value, isIndex, err = parsers.GetPhaseValue(m1.PhasePoints, "S")
		require.NoError(t, err)
		assert.Equal(t, 2.850, value)
		assert.False(t, isIndex)
	})
}
