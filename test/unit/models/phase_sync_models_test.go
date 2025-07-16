package models_test

import (
	"count_mean/internal/models"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestPhaseManifest 測試分期總檔案記錄結構
func TestPhaseManifest(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestPhaseManifestCreation", testPhaseManifestCreation},
		{"TestPhaseManifestJSONSerialization", testPhaseManifestJSONSerialization},
		{"TestPhaseManifestJSONDeserialization", testPhaseManifestJSONDeserialization},
		{"TestPhaseManifestValidation", testPhaseManifestValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testPhaseManifestCreation(t *testing.T) {
	phasePoints := models.PhasePoints{
		P0: 0.1,
		P1: 0.2,
		P2: 0.3,
		S:  0.4,
		C:  0.5,
		D:  100,
		T0: 0.6,
		T:  0.7,
		O:  200,
		L:  0.8,
	}

	manifest := models.PhaseManifest{
		Subject:         "Subject001",
		MotionFile:      "motion.csv",
		ForceFile:       "force.csv",
		EMGFile:         "emg.csv",
		EMGMotionOffset: 50,
		PhasePoints:     phasePoints,
	}

	if manifest.Subject != "Subject001" {
		t.Errorf("PhaseManifest.Subject = %s, expected 'Subject001'", manifest.Subject)
	}

	if manifest.MotionFile != "motion.csv" {
		t.Errorf("PhaseManifest.MotionFile = %s, expected 'motion.csv'", manifest.MotionFile)
	}

	if manifest.ForceFile != "force.csv" {
		t.Errorf("PhaseManifest.ForceFile = %s, expected 'force.csv'", manifest.ForceFile)
	}

	if manifest.EMGFile != "emg.csv" {
		t.Errorf("PhaseManifest.EMGFile = %s, expected 'emg.csv'", manifest.EMGFile)
	}

	if manifest.EMGMotionOffset != 50 {
		t.Errorf("PhaseManifest.EMGMotionOffset = %d, expected 50", manifest.EMGMotionOffset)
	}

	if !reflect.DeepEqual(manifest.PhasePoints, phasePoints) {
		t.Errorf("PhaseManifest.PhasePoints = %v, expected %v", manifest.PhasePoints, phasePoints)
	}
}

func testPhaseManifestJSONSerialization(t *testing.T) {
	manifest := models.PhaseManifest{
		Subject:         "Subject002",
		MotionFile:      "motion2.csv",
		ForceFile:       "force2.csv",
		EMGFile:         "emg2.csv",
		EMGMotionOffset: 25,
		PhasePoints: models.PhasePoints{
			P0: 0.15,
			P1: 0.25,
			P2: 0.35,
			S:  0.45,
			C:  0.55,
			D:  150,
			T0: 0.65,
			T:  0.75,
			O:  250,
			L:  0.85,
		},
	}

	jsonData, err := json.Marshal(manifest)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "Subject002") {
		t.Error("JSON does not contain subject name")
	}

	if !strings.Contains(jsonString, "motion2.csv") {
		t.Error("JSON does not contain motion file name")
	}
}

func testPhaseManifestJSONDeserialization(t *testing.T) {
	jsonString := `{
		"Subject": "Subject003",
		"MotionFile": "motion3.csv",
		"ForceFile": "force3.csv",
		"EMGFile": "emg3.csv",
		"EMGMotionOffset": 75,
		"PhasePoints": {
			"P0": 0.12,
			"P1": 0.22,
			"P2": 0.32,
			"S": 0.42,
			"C": 0.52,
			"D": 125,
			"T0": 0.62,
			"T": 0.72,
			"O": 225,
			"L": 0.82
		}
	}`

	var manifest models.PhaseManifest
	err := json.Unmarshal([]byte(jsonString), &manifest)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if manifest.Subject != "Subject003" {
		t.Errorf("Deserialized Subject = %s, expected 'Subject003'", manifest.Subject)
	}

	if manifest.EMGMotionOffset != 75 {
		t.Errorf("Deserialized EMGMotionOffset = %d, expected 75", manifest.EMGMotionOffset)
	}

	if manifest.PhasePoints.P0 != 0.12 {
		t.Errorf("Deserialized PhasePoints.P0 = %f, expected 0.12", manifest.PhasePoints.P0)
	}
}

func testPhaseManifestValidation(t *testing.T) {
	// 測試有效的 manifest
	validManifest := models.PhaseManifest{
		Subject:         "ValidSubject",
		MotionFile:      "valid_motion.csv",
		ForceFile:       "valid_force.csv",
		EMGFile:         "valid_emg.csv",
		EMGMotionOffset: 0,
		PhasePoints: models.PhasePoints{
			P0: 0.0,
			P1: 0.1,
			P2: 0.2,
			S:  0.3,
			C:  0.4,
			D:  100,
			T0: 0.5,
			T:  0.6,
			O:  200,
			L:  0.7,
		},
	}

	if validManifest.Subject == "" {
		t.Error("Valid manifest should have non-empty subject")
	}

	if validManifest.EMGMotionOffset < 0 {
		t.Error("Valid manifest should have non-negative EMGMotionOffset")
	}

	// 測試時間順序
	points := validManifest.PhasePoints
	if points.P0 > points.P1 || points.P1 > points.P2 {
		t.Error("Phase points P0, P1, P2 should be in ascending order")
	}
}

// TestPhasePoints 測試分期點結構
func TestPhasePoints(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestPhasePointsCreation", testPhasePointsCreation},
		{"TestPhasePointsJSONSerialization", testPhasePointsJSONSerialization},
		{"TestPhasePointsJSONDeserialization", testPhasePointsJSONDeserialization},
		{"TestPhasePointsValidation", testPhasePointsValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testPhasePointsCreation(t *testing.T) {
	points := models.PhasePoints{
		P0: 0.1,
		P1: 0.2,
		P2: 0.3,
		S:  0.4,
		C:  0.5,
		D:  100,
		T0: 0.6,
		T:  0.7,
		O:  200,
		L:  0.8,
	}

	if points.P0 != 0.1 {
		t.Errorf("PhasePoints.P0 = %f, expected 0.1", points.P0)
	}

	if points.D != 100 {
		t.Errorf("PhasePoints.D = %d, expected 100", points.D)
	}

	if points.O != 200 {
		t.Errorf("PhasePoints.O = %d, expected 200", points.O)
	}
}

func testPhasePointsJSONSerialization(t *testing.T) {
	points := models.PhasePoints{
		P0: 0.11,
		P1: 0.21,
		P2: 0.31,
		S:  0.41,
		C:  0.51,
		D:  110,
		T0: 0.61,
		T:  0.71,
		O:  210,
		L:  0.81,
	}

	jsonData, err := json.Marshal(points)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "P0") {
		t.Error("JSON does not contain P0 field")
	}

	if !strings.Contains(jsonString, "110") {
		t.Error("JSON does not contain D field value")
	}
}

func testPhasePointsJSONDeserialization(t *testing.T) {
	jsonString := `{
		"P0": 0.13,
		"P1": 0.23,
		"P2": 0.33,
		"S": 0.43,
		"C": 0.53,
		"D": 130,
		"T0": 0.63,
		"T": 0.73,
		"O": 230,
		"L": 0.83
	}`

	var points models.PhasePoints
	err := json.Unmarshal([]byte(jsonString), &points)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if points.P0 != 0.13 {
		t.Errorf("Deserialized P0 = %f, expected 0.13", points.P0)
	}

	if points.D != 130 {
		t.Errorf("Deserialized D = %d, expected 130", points.D)
	}
}

func testPhasePointsValidation(t *testing.T) {
	// 測試有效的分期點
	validPoints := models.PhasePoints{
		P0: 0.0,
		P1: 0.1,
		P2: 0.2,
		S:  0.3,
		C:  0.4,
		D:  100,
		T0: 0.5,
		T:  0.6,
		O:  200,
		L:  0.7,
	}

	// 檢查時間順序
	if validPoints.P0 > validPoints.P1 {
		t.Error("P0 should be <= P1")
	}

	if validPoints.P1 > validPoints.P2 {
		t.Error("P1 should be <= P2")
	}

	// 檢查索引值
	if validPoints.D < 0 {
		t.Error("D should be non-negative")
	}

	if validPoints.O < 0 {
		t.Error("O should be non-negative")
	}
}

// TestAnalysisParams 測試分析參數結構
func TestAnalysisParams(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestAnalysisParamsCreation", testAnalysisParamsCreation},
		{"TestAnalysisParamsJSONSerialization", testAnalysisParamsJSONSerialization},
		{"TestAnalysisParamsJSONDeserialization", testAnalysisParamsJSONDeserialization},
		{"TestAnalysisParamsValidation", testAnalysisParamsValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testAnalysisParamsCreation(t *testing.T) {
	params := models.AnalysisParams{
		ManifestFile: "manifest.csv",
		DataFolder:   "/data",
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	if params.ManifestFile != "manifest.csv" {
		t.Errorf("AnalysisParams.ManifestFile = %s, expected 'manifest.csv'", params.ManifestFile)
	}

	if params.DataFolder != "/data" {
		t.Errorf("AnalysisParams.DataFolder = %s, expected '/data'", params.DataFolder)
	}

	if params.StartPhase != "P0" {
		t.Errorf("AnalysisParams.StartPhase = %s, expected 'P0'", params.StartPhase)
	}

	if params.EndPhase != "P2" {
		t.Errorf("AnalysisParams.EndPhase = %s, expected 'P2'", params.EndPhase)
	}

	if params.SubjectIndex != 0 {
		t.Errorf("AnalysisParams.SubjectIndex = %d, expected 0", params.SubjectIndex)
	}
}

func testAnalysisParamsJSONSerialization(t *testing.T) {
	params := models.AnalysisParams{
		ManifestFile: "test_manifest.csv",
		DataFolder:   "/test_data",
		StartPhase:   "S",
		EndPhase:     "T",
		SubjectIndex: 1,
	}

	jsonData, err := json.Marshal(params)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "test_manifest.csv") {
		t.Error("JSON does not contain manifest file name")
	}

	if !strings.Contains(jsonString, "/test_data") {
		t.Error("JSON does not contain data folder path")
	}
}

func testAnalysisParamsJSONDeserialization(t *testing.T) {
	jsonString := `{
		"ManifestFile": "deserial_manifest.csv",
		"DataFolder": "/deserial_data",
		"StartPhase": "C",
		"EndPhase": "L",
		"SubjectIndex": 2
	}`

	var params models.AnalysisParams
	err := json.Unmarshal([]byte(jsonString), &params)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if params.ManifestFile != "deserial_manifest.csv" {
		t.Errorf("Deserialized ManifestFile = %s, expected 'deserial_manifest.csv'", params.ManifestFile)
	}

	if params.SubjectIndex != 2 {
		t.Errorf("Deserialized SubjectIndex = %d, expected 2", params.SubjectIndex)
	}
}

func testAnalysisParamsValidation(t *testing.T) {
	// 測試有效參數
	validParams := models.AnalysisParams{
		ManifestFile: "valid.csv",
		DataFolder:   "/valid",
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	if validParams.ManifestFile == "" {
		t.Error("Valid params should have non-empty manifest file")
	}

	if validParams.DataFolder == "" {
		t.Error("Valid params should have non-empty data folder")
	}

	if validParams.SubjectIndex < 0 {
		t.Error("Valid params should have non-negative subject index")
	}
}

// TestEMGStatistics 測試 EMG 統計結果結構
func TestEMGStatistics(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestEMGStatisticsCreation", testEMGStatisticsCreation},
		{"TestEMGStatisticsJSONSerialization", testEMGStatisticsJSONSerialization},
		{"TestEMGStatisticsJSONDeserialization", testEMGStatisticsJSONDeserialization},
		{"TestEMGStatisticsValidation", testEMGStatisticsValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testEMGStatisticsCreation(t *testing.T) {
	channelNames := []string{"Channel1", "Channel2", "Channel3"}
	channelMeans := map[string]float64{
		"Channel1": 0.1,
		"Channel2": 0.2,
		"Channel3": 0.3,
	}
	channelMaxes := map[string]float64{
		"Channel1": 0.5,
		"Channel2": 0.6,
		"Channel3": 0.7,
	}

	stats := models.EMGStatistics{
		Subject:      "TestSubject",
		StartPhase:   "P0",
		StartTime:    0.0,
		EndPhase:     "P2",
		EndTime:      1.0,
		ChannelNames: channelNames,
		ChannelMeans: channelMeans,
		ChannelMaxes: channelMaxes,
	}

	if stats.Subject != "TestSubject" {
		t.Errorf("EMGStatistics.Subject = %s, expected 'TestSubject'", stats.Subject)
	}

	if stats.StartTime != 0.0 {
		t.Errorf("EMGStatistics.StartTime = %f, expected 0.0", stats.StartTime)
	}

	if stats.EndTime != 1.0 {
		t.Errorf("EMGStatistics.EndTime = %f, expected 1.0", stats.EndTime)
	}

	if !reflect.DeepEqual(stats.ChannelNames, channelNames) {
		t.Errorf("EMGStatistics.ChannelNames = %v, expected %v", stats.ChannelNames, channelNames)
	}

	if !reflect.DeepEqual(stats.ChannelMeans, channelMeans) {
		t.Errorf("EMGStatistics.ChannelMeans = %v, expected %v", stats.ChannelMeans, channelMeans)
	}

	if !reflect.DeepEqual(stats.ChannelMaxes, channelMaxes) {
		t.Errorf("EMGStatistics.ChannelMaxes = %v, expected %v", stats.ChannelMaxes, channelMaxes)
	}
}

func testEMGStatisticsJSONSerialization(t *testing.T) {
	stats := models.EMGStatistics{
		Subject:      "SerializationTest",
		StartPhase:   "S",
		StartTime:    0.5,
		EndPhase:     "T",
		EndTime:      1.5,
		ChannelNames: []string{"Ch1", "Ch2"},
		ChannelMeans: map[string]float64{"Ch1": 0.1, "Ch2": 0.2},
		ChannelMaxes: map[string]float64{"Ch1": 0.3, "Ch2": 0.4},
	}

	jsonData, err := json.Marshal(stats)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "SerializationTest") {
		t.Error("JSON does not contain subject name")
	}

	if !strings.Contains(jsonString, "Ch1") {
		t.Error("JSON does not contain channel names")
	}
}

func testEMGStatisticsJSONDeserialization(t *testing.T) {
	jsonString := `{
		"Subject": "DeserializationTest",
		"StartPhase": "C",
		"StartTime": 0.25,
		"EndPhase": "L",
		"EndTime": 0.75,
		"ChannelNames": ["ChA", "ChB"],
		"ChannelMeans": {"ChA": 0.15, "ChB": 0.25},
		"ChannelMaxes": {"ChA": 0.35, "ChB": 0.45}
	}`

	var stats models.EMGStatistics
	err := json.Unmarshal([]byte(jsonString), &stats)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if stats.Subject != "DeserializationTest" {
		t.Errorf("Deserialized Subject = %s, expected 'DeserializationTest'", stats.Subject)
	}

	if stats.StartTime != 0.25 {
		t.Errorf("Deserialized StartTime = %f, expected 0.25", stats.StartTime)
	}

	if len(stats.ChannelNames) != 2 {
		t.Errorf("Deserialized ChannelNames length = %d, expected 2", len(stats.ChannelNames))
	}
}

func testEMGStatisticsValidation(t *testing.T) {
	// 測試有效統計
	validStats := models.EMGStatistics{
		Subject:      "ValidStats",
		StartPhase:   "P0",
		StartTime:    0.0,
		EndPhase:     "P1",
		EndTime:      1.0,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 0.1},
		ChannelMaxes: map[string]float64{"Ch1": 0.2},
	}

	if validStats.EndTime <= validStats.StartTime {
		t.Error("End time should be greater than start time")
	}

	if len(validStats.ChannelNames) != len(validStats.ChannelMeans) {
		t.Error("Channel names and means should have same length")
	}

	if len(validStats.ChannelNames) != len(validStats.ChannelMaxes) {
		t.Error("Channel names and maxes should have same length")
	}

	// 檢查均值和最大值的關係
	for channel, mean := range validStats.ChannelMeans {
		max, exists := validStats.ChannelMaxes[channel]
		if !exists {
			t.Errorf("Channel %s missing in maxes", channel)
		}
		if mean > max {
			t.Errorf("Channel %s mean (%f) > max (%f)", channel, mean, max)
		}
	}
}

// TestPhaseSyncEMGData 測試分期同步 EMG 數據結構
func TestPhaseSyncEMGData(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestPhaseSyncEMGDataCreation", testPhaseSyncEMGDataCreation},
		{"TestPhaseSyncEMGDataJSONSerialization", testPhaseSyncEMGDataJSONSerialization},
		{"TestPhaseSyncEMGDataJSONDeserialization", testPhaseSyncEMGDataJSONDeserialization},
		{"TestPhaseSyncEMGDataValidation", testPhaseSyncEMGDataValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testPhaseSyncEMGDataCreation(t *testing.T) {
	time := []float64{0.0, 0.1, 0.2, 0.3}
	channels := map[string][]float64{
		"Channel1": {0.1, 0.2, 0.3, 0.4},
		"Channel2": {0.2, 0.3, 0.4, 0.5},
		"Channel3": {0.3, 0.4, 0.5, 0.6},
	}
	headers := []string{"Channel1", "Channel2", "Channel3"}

	data := models.PhaseSyncEMGData{
		Time:     time,
		Channels: channels,
		Headers:  headers,
	}

	if !reflect.DeepEqual(data.Time, time) {
		t.Errorf("PhaseSyncEMGData.Time = %v, expected %v", data.Time, time)
	}

	if !reflect.DeepEqual(data.Channels, channels) {
		t.Errorf("PhaseSyncEMGData.Channels = %v, expected %v", data.Channels, channels)
	}

	if !reflect.DeepEqual(data.Headers, headers) {
		t.Errorf("PhaseSyncEMGData.Headers = %v, expected %v", data.Headers, headers)
	}
}

func testPhaseSyncEMGDataJSONSerialization(t *testing.T) {
	data := models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1},
		Channels: map[string][]float64{
			"Ch1": {0.1, 0.2},
			"Ch2": {0.2, 0.3},
		},
		Headers: []string{"Ch1", "Ch2"},
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "Time") {
		t.Error("JSON does not contain Time field")
	}

	if !strings.Contains(jsonString, "Channels") {
		t.Error("JSON does not contain Channels field")
	}

	if !strings.Contains(jsonString, "Headers") {
		t.Error("JSON does not contain Headers field")
	}
}

func testPhaseSyncEMGDataJSONDeserialization(t *testing.T) {
	jsonString := `{
		"Time": [0.0, 0.1, 0.2],
		"Channels": {
			"ChA": [0.1, 0.2, 0.3],
			"ChB": [0.2, 0.3, 0.4]
		},
		"Headers": ["ChA", "ChB"]
	}`

	var data models.PhaseSyncEMGData
	err := json.Unmarshal([]byte(jsonString), &data)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if len(data.Time) != 3 {
		t.Errorf("Deserialized Time length = %d, expected 3", len(data.Time))
	}

	if len(data.Channels) != 2 {
		t.Errorf("Deserialized Channels length = %d, expected 2", len(data.Channels))
	}

	if len(data.Headers) != 2 {
		t.Errorf("Deserialized Headers length = %d, expected 2", len(data.Headers))
	}
}

func testPhaseSyncEMGDataValidation(t *testing.T) {
	// 測試有效數據
	validData := models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2},
		Channels: map[string][]float64{
			"Ch1": {0.1, 0.2, 0.3},
			"Ch2": {0.2, 0.3, 0.4},
		},
		Headers: []string{"Ch1", "Ch2"},
	}

	// 檢查時間和通道數據長度一致
	timeLength := len(validData.Time)
	for channel, values := range validData.Channels {
		if len(values) != timeLength {
			t.Errorf("Channel %s has %d values, expected %d", channel, len(values), timeLength)
		}
	}

	// 檢查標題與通道的一致性
	for _, header := range validData.Headers {
		if _, exists := validData.Channels[header]; !exists {
			t.Errorf("Header %s not found in channels", header)
		}
	}
}

// TestMotionData 測試 Motion 數據結構
func TestMotionData(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestMotionDataCreation", testMotionDataCreation},
		{"TestMotionDataJSONSerialization", testMotionDataJSONSerialization},
		{"TestMotionDataJSONDeserialization", testMotionDataJSONDeserialization},
		{"TestMotionDataValidation", testMotionDataValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testMotionDataCreation(t *testing.T) {
	indices := []int{0, 1, 2, 3}
	data := map[string][]float64{
		"X": {1.0, 2.0, 3.0, 4.0},
		"Y": {2.0, 3.0, 4.0, 5.0},
		"Z": {3.0, 4.0, 5.0, 6.0},
	}
	headers := []string{"X", "Y", "Z"}

	motionData := models.MotionData{
		Indices: indices,
		Data:    data,
		Headers: headers,
	}

	if !reflect.DeepEqual(motionData.Indices, indices) {
		t.Errorf("MotionData.Indices = %v, expected %v", motionData.Indices, indices)
	}

	if !reflect.DeepEqual(motionData.Data, data) {
		t.Errorf("MotionData.Data = %v, expected %v", motionData.Data, data)
	}

	if !reflect.DeepEqual(motionData.Headers, headers) {
		t.Errorf("MotionData.Headers = %v, expected %v", motionData.Headers, headers)
	}
}

func testMotionDataJSONSerialization(t *testing.T) {
	motionData := models.MotionData{
		Indices: []int{0, 1, 2},
		Data: map[string][]float64{
			"X": {1.0, 2.0, 3.0},
			"Y": {2.0, 3.0, 4.0},
		},
		Headers: []string{"X", "Y"},
	}

	jsonData, err := json.Marshal(motionData)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "Indices") {
		t.Error("JSON does not contain Indices field")
	}

	if !strings.Contains(jsonString, "Data") {
		t.Error("JSON does not contain Data field")
	}

	if !strings.Contains(jsonString, "Headers") {
		t.Error("JSON does not contain Headers field")
	}
}

func testMotionDataJSONDeserialization(t *testing.T) {
	jsonString := `{
		"Indices": [0, 1, 2, 3],
		"Data": {
			"X": [1.0, 2.0, 3.0, 4.0],
			"Y": [2.0, 3.0, 4.0, 5.0]
		},
		"Headers": ["X", "Y"]
	}`

	var motionData models.MotionData
	err := json.Unmarshal([]byte(jsonString), &motionData)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if len(motionData.Indices) != 4 {
		t.Errorf("Deserialized Indices length = %d, expected 4", len(motionData.Indices))
	}

	if len(motionData.Data) != 2 {
		t.Errorf("Deserialized Data length = %d, expected 2", len(motionData.Data))
	}

	if len(motionData.Headers) != 2 {
		t.Errorf("Deserialized Headers length = %d, expected 2", len(motionData.Headers))
	}
}

func testMotionDataValidation(t *testing.T) {
	// 測試有效數據
	validData := models.MotionData{
		Indices: []int{0, 1, 2},
		Data: map[string][]float64{
			"X": {1.0, 2.0, 3.0},
			"Y": {2.0, 3.0, 4.0},
		},
		Headers: []string{"X", "Y"},
	}

	// 檢查索引和數據長度一致
	indicesLength := len(validData.Indices)
	for column, values := range validData.Data {
		if len(values) != indicesLength {
			t.Errorf("Column %s has %d values, expected %d", column, len(values), indicesLength)
		}
	}

	// 檢查標題與數據的一致性
	for _, header := range validData.Headers {
		if _, exists := validData.Data[header]; !exists {
			t.Errorf("Header %s not found in data", header)
		}
	}
}

// TestValidationError 測試驗證錯誤結構
func TestValidationError(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestValidationErrorCreation", testValidationErrorCreation},
		{"TestValidationErrorString", testValidationErrorString},
		{"TestValidationErrorInterface", testValidationErrorInterface},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testValidationErrorCreation(t *testing.T) {
	err := models.ValidationError{
		Field:   "TestField",
		Message: "Test error message",
	}

	if err.Field != "TestField" {
		t.Errorf("ValidationError.Field = %s, expected 'TestField'", err.Field)
	}

	if err.Message != "Test error message" {
		t.Errorf("ValidationError.Message = %s, expected 'Test error message'", err.Message)
	}
}

func testValidationErrorString(t *testing.T) {
	err := models.ValidationError{
		Field:   "Subject",
		Message: "cannot be empty",
	}

	expectedString := "Subject: cannot be empty"
	if err.Error() != expectedString {
		t.Errorf("ValidationError.Error() = %s, expected '%s'", err.Error(), expectedString)
	}
}

func testValidationErrorInterface(t *testing.T) {
	var err error = models.ValidationError{
		Field:   "StartTime",
		Message: "must be positive",
	}

	// 測試是否實現了 error 接口
	if err == nil {
		t.Error("ValidationError should implement error interface")
	}

	expectedString := "StartTime: must be positive"
	if err.Error() != expectedString {
		t.Errorf("ValidationError as error interface = %s, expected '%s'", err.Error(), expectedString)
	}
}

// TestSyncTime 測試同步時間信息結構
func TestSyncTime(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestSyncTimeCreation", testSyncTimeCreation},
		{"TestSyncTimeJSONSerialization", testSyncTimeJSONSerialization},
		{"TestSyncTimeJSONDeserialization", testSyncTimeJSONDeserialization},
		{"TestSyncTimeValidation", testSyncTimeValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testSyncTimeCreation(t *testing.T) {
	now := time.Now()
	syncTime := models.SyncTime{
		EMGTime:   1.234,
		ForceTime: 2.345,
		MotionIdx: 100,
		ValidAt:   now,
	}

	if syncTime.EMGTime != 1.234 {
		t.Errorf("SyncTime.EMGTime = %f, expected 1.234", syncTime.EMGTime)
	}

	if syncTime.ForceTime != 2.345 {
		t.Errorf("SyncTime.ForceTime = %f, expected 2.345", syncTime.ForceTime)
	}

	if syncTime.MotionIdx != 100 {
		t.Errorf("SyncTime.MotionIdx = %d, expected 100", syncTime.MotionIdx)
	}

	if !syncTime.ValidAt.Equal(now) {
		t.Errorf("SyncTime.ValidAt = %v, expected %v", syncTime.ValidAt, now)
	}
}

func testSyncTimeJSONSerialization(t *testing.T) {
	syncTime := models.SyncTime{
		EMGTime:   3.456,
		ForceTime: 4.567,
		MotionIdx: 200,
		ValidAt:   time.Now(),
	}

	jsonData, err := json.Marshal(syncTime)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "EMGTime") {
		t.Error("JSON does not contain EMGTime field")
	}

	if !strings.Contains(jsonString, "ForceTime") {
		t.Error("JSON does not contain ForceTime field")
	}

	if !strings.Contains(jsonString, "MotionIdx") {
		t.Error("JSON does not contain MotionIdx field")
	}

	if !strings.Contains(jsonString, "ValidAt") {
		t.Error("JSON does not contain ValidAt field")
	}
}

func testSyncTimeJSONDeserialization(t *testing.T) {
	jsonString := `{
		"EMGTime": 5.678,
		"ForceTime": 6.789,
		"MotionIdx": 300,
		"ValidAt": "2023-01-01T00:00:00Z"
	}`

	var syncTime models.SyncTime
	err := json.Unmarshal([]byte(jsonString), &syncTime)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if syncTime.EMGTime != 5.678 {
		t.Errorf("Deserialized EMGTime = %f, expected 5.678", syncTime.EMGTime)
	}

	if syncTime.ForceTime != 6.789 {
		t.Errorf("Deserialized ForceTime = %f, expected 6.789", syncTime.ForceTime)
	}

	if syncTime.MotionIdx != 300 {
		t.Errorf("Deserialized MotionIdx = %d, expected 300", syncTime.MotionIdx)
	}
}

func testSyncTimeValidation(t *testing.T) {
	// 測試有效同步時間
	validSyncTime := models.SyncTime{
		EMGTime:   1.0,
		ForceTime: 2.0,
		MotionIdx: 100,
		ValidAt:   time.Now(),
	}

	if validSyncTime.EMGTime < 0 {
		t.Error("EMGTime should be non-negative")
	}

	if validSyncTime.ForceTime < 0 {
		t.Error("ForceTime should be non-negative")
	}

	if validSyncTime.MotionIdx < 0 {
		t.Error("MotionIdx should be non-negative")
	}

	if validSyncTime.ValidAt.IsZero() {
		t.Error("ValidAt should not be zero time")
	}
}

// 基準測試
func BenchmarkPhaseManifestJSONSerialization(b *testing.B) {
	manifest := models.PhaseManifest{
		Subject:         "BenchmarkSubject",
		MotionFile:      "motion.csv",
		ForceFile:       "force.csv",
		EMGFile:         "emg.csv",
		EMGMotionOffset: 50,
		PhasePoints: models.PhasePoints{
			P0: 0.1, P1: 0.2, P2: 0.3, S: 0.4, C: 0.5,
			D: 100, T0: 0.6, T: 0.7, O: 200, L: 0.8,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(manifest)
		if err != nil {
			b.Errorf("JSON serialization failed: %v", err)
		}
	}
}

func BenchmarkEMGStatisticsJSONSerialization(b *testing.B) {
	stats := models.EMGStatistics{
		Subject:      "BenchmarkSubject",
		StartPhase:   "P0",
		StartTime:    0.0,
		EndPhase:     "P2",
		EndTime:      1.0,
		ChannelNames: []string{"Ch1", "Ch2", "Ch3"},
		ChannelMeans: map[string]float64{"Ch1": 0.1, "Ch2": 0.2, "Ch3": 0.3},
		ChannelMaxes: map[string]float64{"Ch1": 0.5, "Ch2": 0.6, "Ch3": 0.7},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(stats)
		if err != nil {
			b.Errorf("JSON serialization failed: %v", err)
		}
	}
}

func BenchmarkPhaseSyncEMGDataJSONSerialization(b *testing.B) {
	data := models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2, 0.3, 0.4},
		Channels: map[string][]float64{
			"Ch1": {0.1, 0.2, 0.3, 0.4, 0.5},
			"Ch2": {0.2, 0.3, 0.4, 0.5, 0.6},
			"Ch3": {0.3, 0.4, 0.5, 0.6, 0.7},
		},
		Headers: []string{"Ch1", "Ch2", "Ch3"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(data)
		if err != nil {
			b.Errorf("JSON serialization failed: %v", err)
		}
	}
}

// 測試完整的數據流程
func TestCompleteDataFlow(t *testing.T) {
	// 創建完整的分析流程數據
	manifest := models.PhaseManifest{
		Subject:         "CompleteFlowSubject",
		MotionFile:      "motion.csv",
		ForceFile:       "force.csv",
		EMGFile:         "emg.csv",
		EMGMotionOffset: 25,
		PhasePoints: models.PhasePoints{
			P0: 0.1, P1: 0.2, P2: 0.3, S: 0.4, C: 0.5,
			D: 100, T0: 0.6, T: 0.7, O: 200, L: 0.8,
		},
	}

	params := models.AnalysisParams{
		ManifestFile: "manifest.csv",
		DataFolder:   "/data",
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	// 序列化
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Manifest serialization failed: %v", err)
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Params serialization failed: %v", err)
	}

	// 反序列化
	var deserializedManifest models.PhaseManifest
	err = json.Unmarshal(manifestJSON, &deserializedManifest)
	if err != nil {
		t.Fatalf("Manifest deserialization failed: %v", err)
	}

	var deserializedParams models.AnalysisParams
	err = json.Unmarshal(paramsJSON, &deserializedParams)
	if err != nil {
		t.Fatalf("Params deserialization failed: %v", err)
	}

	// 驗證數據完整性
	if !reflect.DeepEqual(manifest, deserializedManifest) {
		t.Error("Manifest data flow failed")
	}

	if !reflect.DeepEqual(params, deserializedParams) {
		t.Error("Params data flow failed")
	}
}
