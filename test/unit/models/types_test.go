package models_test

import (
	"count_mean/internal/models"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestEMGData 測試 EMG 數據結構
func TestEMGData(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestEMGDataCreation", testEMGDataCreation},
		{"TestEMGDataJSONSerialization", testEMGDataJSONSerialization},
		{"TestEMGDataJSONDeserialization", testEMGDataJSONDeserialization},
		{"TestEMGDataValidation", testEMGDataValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testEMGDataCreation(t *testing.T) {
	// 測試正常創建
	emgData := models.EMGData{
		Time:     1.234,
		Channels: []float64{0.1, 0.2, 0.3, 0.4},
	}

	if emgData.Time != 1.234 {
		t.Errorf("EMGData.Time = %f, expected 1.234", emgData.Time)
	}

	if len(emgData.Channels) != 4 {
		t.Errorf("EMGData.Channels length = %d, expected 4", len(emgData.Channels))
	}

	expectedChannels := []float64{0.1, 0.2, 0.3, 0.4}
	if !reflect.DeepEqual(emgData.Channels, expectedChannels) {
		t.Errorf("EMGData.Channels = %v, expected %v", emgData.Channels, expectedChannels)
	}
}

func testEMGDataJSONSerialization(t *testing.T) {
	emgData := models.EMGData{
		Time:     2.567,
		Channels: []float64{0.5, 0.6, 0.7},
	}

	jsonData, err := json.Marshal(emgData)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	// 檢查 JSON 是否包含預期的字段
	jsonString := string(jsonData)
	if !containsField(jsonString, "time") {
		t.Error("JSON does not contain 'time' field")
	}

	if !containsField(jsonString, "channels") {
		t.Error("JSON does not contain 'channels' field")
	}
}

func testEMGDataJSONDeserialization(t *testing.T) {
	jsonString := `{"time": 3.789, "channels": [0.8, 0.9, 1.0]}`

	var emgData models.EMGData
	err := json.Unmarshal([]byte(jsonString), &emgData)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if emgData.Time != 3.789 {
		t.Errorf("Deserialized EMGData.Time = %f, expected 3.789", emgData.Time)
	}

	expectedChannels := []float64{0.8, 0.9, 1.0}
	if !reflect.DeepEqual(emgData.Channels, expectedChannels) {
		t.Errorf("Deserialized EMGData.Channels = %v, expected %v", emgData.Channels, expectedChannels)
	}
}

func testEMGDataValidation(t *testing.T) {
	// 測試空通道
	emptyChannels := models.EMGData{
		Time:     1.0,
		Channels: []float64{},
	}

	if len(emptyChannels.Channels) != 0 {
		t.Error("Empty channels should be valid")
	}

	// 測試 nil 通道
	nilChannels := models.EMGData{
		Time:     1.0,
		Channels: nil,
	}

	if nilChannels.Channels != nil {
		t.Error("Nil channels should remain nil")
	}
}

// TestEMGDataset 測試 EMG 數據集結構
func TestEMGDataset(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestEMGDatasetCreation", testEMGDatasetCreation},
		{"TestEMGDatasetJSONSerialization", testEMGDatasetJSONSerialization},
		{"TestEMGDatasetJSONDeserialization", testEMGDatasetJSONDeserialization},
		{"TestEMGDatasetValidation", testEMGDatasetValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testEMGDatasetCreation(t *testing.T) {
	headers := []string{"Time", "Channel1", "Channel2", "Channel3"}
	data := []models.EMGData{
		{Time: 0.0, Channels: []float64{0.1, 0.2, 0.3}},
		{Time: 0.1, Channels: []float64{0.4, 0.5, 0.6}},
		{Time: 0.2, Channels: []float64{0.7, 0.8, 0.9}},
	}

	dataset := models.EMGDataset{
		Headers:               headers,
		Data:                  data,
		OriginalTimePrecision: 3,
	}

	if !reflect.DeepEqual(dataset.Headers, headers) {
		t.Errorf("EMGDataset.Headers = %v, expected %v", dataset.Headers, headers)
	}

	if len(dataset.Data) != len(data) {
		t.Errorf("EMGDataset.Data length = %d, expected %d", len(dataset.Data), len(data))
	}

	if dataset.OriginalTimePrecision != 3 {
		t.Errorf("EMGDataset.OriginalTimePrecision = %d, expected 3", dataset.OriginalTimePrecision)
	}
}

func testEMGDatasetJSONSerialization(t *testing.T) {
	dataset := models.EMGDataset{
		Headers: []string{"Time", "Channel1"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0.1}},
			{Time: 0.1, Channels: []float64{0.2}},
		},
		OriginalTimePrecision: 2,
	}

	jsonData, err := json.Marshal(dataset)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "headers") {
		t.Error("JSON does not contain 'headers' field")
	}

	if !containsField(jsonString, "data") {
		t.Error("JSON does not contain 'data' field")
	}

	if !containsField(jsonString, "original_time_precision") {
		t.Error("JSON does not contain 'original_time_precision' field")
	}
}

func testEMGDatasetJSONDeserialization(t *testing.T) {
	jsonString := `{
		"headers": ["Time", "Channel1", "Channel2"],
		"data": [
			{"time": 0.0, "channels": [0.1, 0.2]},
			{"time": 0.1, "channels": [0.3, 0.4]}
		],
		"original_time_precision": 2
	}`

	var dataset models.EMGDataset
	err := json.Unmarshal([]byte(jsonString), &dataset)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	expectedHeaders := []string{"Time", "Channel1", "Channel2"}
	if !reflect.DeepEqual(dataset.Headers, expectedHeaders) {
		t.Errorf("Deserialized EMGDataset.Headers = %v, expected %v", dataset.Headers, expectedHeaders)
	}

	if len(dataset.Data) != 2 {
		t.Errorf("Deserialized EMGDataset.Data length = %d, expected 2", len(dataset.Data))
	}

	if dataset.OriginalTimePrecision != 2 {
		t.Errorf("Deserialized EMGDataset.OriginalTimePrecision = %d, expected 2", dataset.OriginalTimePrecision)
	}
}

func testEMGDatasetValidation(t *testing.T) {
	// 測試空數據集
	emptyDataset := models.EMGDataset{}

	if len(emptyDataset.Headers) != 0 {
		t.Error("Empty dataset should have no headers")
	}

	if len(emptyDataset.Data) != 0 {
		t.Error("Empty dataset should have no data")
	}

	// 測試數據一致性
	dataset := models.EMGDataset{
		Headers: []string{"Time", "Channel1", "Channel2"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0.1, 0.2}},
			{Time: 0.1, Channels: []float64{0.3, 0.4}},
		},
	}

	// 驗證數據通道數與標題數是否一致
	expectedChannels := len(dataset.Headers) - 1 // 減去時間列
	for i, data := range dataset.Data {
		if len(data.Channels) != expectedChannels {
			t.Errorf("Data[%d] has %d channels, expected %d", i, len(data.Channels), expectedChannels)
		}
	}
}

// TestMaxMeanResult 測試最大平均值結果結構
func TestMaxMeanResult(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestMaxMeanResultCreation", testMaxMeanResultCreation},
		{"TestMaxMeanResultJSONSerialization", testMaxMeanResultJSONSerialization},
		{"TestMaxMeanResultJSONDeserialization", testMaxMeanResultJSONDeserialization},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testMaxMeanResultCreation(t *testing.T) {
	result := models.MaxMeanResult{
		ColumnIndex: 2,
		StartTime:   1.5,
		EndTime:     2.5,
		MaxMean:     0.567,
	}

	if result.ColumnIndex != 2 {
		t.Errorf("MaxMeanResult.ColumnIndex = %d, expected 2", result.ColumnIndex)
	}

	if result.StartTime != 1.5 {
		t.Errorf("MaxMeanResult.StartTime = %f, expected 1.5", result.StartTime)
	}

	if result.EndTime != 2.5 {
		t.Errorf("MaxMeanResult.EndTime = %f, expected 2.5", result.EndTime)
	}

	if result.MaxMean != 0.567 {
		t.Errorf("MaxMeanResult.MaxMean = %f, expected 0.567", result.MaxMean)
	}
}

func testMaxMeanResultJSONSerialization(t *testing.T) {
	result := models.MaxMeanResult{
		ColumnIndex: 1,
		StartTime:   0.5,
		EndTime:     1.5,
		MaxMean:     0.789,
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "column_index") {
		t.Error("JSON does not contain 'column_index' field")
	}

	if !containsField(jsonString, "start_time") {
		t.Error("JSON does not contain 'start_time' field")
	}

	if !containsField(jsonString, "end_time") {
		t.Error("JSON does not contain 'end_time' field")
	}

	if !containsField(jsonString, "max_mean") {
		t.Error("JSON does not contain 'max_mean' field")
	}
}

func testMaxMeanResultJSONDeserialization(t *testing.T) {
	jsonString := `{
		"column_index": 3,
		"start_time": 2.0,
		"end_time": 3.0,
		"max_mean": 0.456
	}`

	var result models.MaxMeanResult
	err := json.Unmarshal([]byte(jsonString), &result)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if result.ColumnIndex != 3 {
		t.Errorf("Deserialized MaxMeanResult.ColumnIndex = %d, expected 3", result.ColumnIndex)
	}

	if result.StartTime != 2.0 {
		t.Errorf("Deserialized MaxMeanResult.StartTime = %f, expected 2.0", result.StartTime)
	}

	if result.EndTime != 3.0 {
		t.Errorf("Deserialized MaxMeanResult.EndTime = %f, expected 3.0", result.EndTime)
	}

	if result.MaxMean != 0.456 {
		t.Errorf("Deserialized MaxMeanResult.MaxMean = %f, expected 0.456", result.MaxMean)
	}
}

// TestPhaseAnalysisResult 測試階段分析結果結構
func TestPhaseAnalysisResult(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestPhaseAnalysisResultCreation", testPhaseAnalysisResultCreation},
		{"TestPhaseAnalysisResultJSONSerialization", testPhaseAnalysisResultJSONSerialization},
		{"TestPhaseAnalysisResultJSONDeserialization", testPhaseAnalysisResultJSONDeserialization},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testPhaseAnalysisResultCreation(t *testing.T) {
	maxValues := map[int]float64{1: 0.5, 2: 0.7, 3: 0.3}
	meanValues := map[int]float64{1: 0.3, 2: 0.4, 3: 0.2}

	result := models.PhaseAnalysisResult{
		PhaseName:  "Phase1",
		MaxValues:  maxValues,
		MeanValues: meanValues,
	}

	if result.PhaseName != "Phase1" {
		t.Errorf("PhaseAnalysisResult.PhaseName = %s, expected 'Phase1'", result.PhaseName)
	}

	if !reflect.DeepEqual(result.MaxValues, maxValues) {
		t.Errorf("PhaseAnalysisResult.MaxValues = %v, expected %v", result.MaxValues, maxValues)
	}

	if !reflect.DeepEqual(result.MeanValues, meanValues) {
		t.Errorf("PhaseAnalysisResult.MeanValues = %v, expected %v", result.MeanValues, meanValues)
	}
}

func testPhaseAnalysisResultJSONSerialization(t *testing.T) {
	result := models.PhaseAnalysisResult{
		PhaseName:  "Phase2",
		MaxValues:  map[int]float64{1: 0.8, 2: 0.9},
		MeanValues: map[int]float64{1: 0.6, 2: 0.7},
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "phase_name") {
		t.Error("JSON does not contain 'phase_name' field")
	}

	if !containsField(jsonString, "max_values") {
		t.Error("JSON does not contain 'max_values' field")
	}

	if !containsField(jsonString, "mean_values") {
		t.Error("JSON does not contain 'mean_values' field")
	}
}

func testPhaseAnalysisResultJSONDeserialization(t *testing.T) {
	jsonString := `{
		"phase_name": "Phase3",
		"max_values": {"1": 0.6, "2": 0.8},
		"mean_values": {"1": 0.4, "2": 0.5}
	}`

	var result models.PhaseAnalysisResult
	err := json.Unmarshal([]byte(jsonString), &result)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if result.PhaseName != "Phase3" {
		t.Errorf("Deserialized PhaseAnalysisResult.PhaseName = %s, expected 'Phase3'", result.PhaseName)
	}

	if len(result.MaxValues) != 2 {
		t.Errorf("Deserialized MaxValues length = %d, expected 2", len(result.MaxValues))
	}

	if len(result.MeanValues) != 2 {
		t.Errorf("Deserialized MeanValues length = %d, expected 2", len(result.MeanValues))
	}
}

// TestAnalysisConfig 測試分析配置結構
func TestAnalysisConfig(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestAnalysisConfigCreation", testAnalysisConfigCreation},
		{"TestAnalysisConfigJSONSerialization", testAnalysisConfigJSONSerialization},
		{"TestAnalysisConfigJSONDeserialization", testAnalysisConfigJSONDeserialization},
		{"TestAnalysisConfigValidation", testAnalysisConfigValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testAnalysisConfigCreation(t *testing.T) {
	now := time.Now()
	phases := []models.TimeRange{
		{Start: 0.0, End: 1.0},
		{Start: 1.0, End: 2.0},
	}

	config := models.AnalysisConfig{
		ScalingFactor: 1000,
		WindowSize:    50,
		PhaseLabels:   []string{"Phase1", "Phase2"},
		Phases:        phases,
		CreatedAt:     now,
	}

	if config.ScalingFactor != 1000 {
		t.Errorf("AnalysisConfig.ScalingFactor = %d, expected 1000", config.ScalingFactor)
	}

	if config.WindowSize != 50 {
		t.Errorf("AnalysisConfig.WindowSize = %d, expected 50", config.WindowSize)
	}

	if len(config.PhaseLabels) != 2 {
		t.Errorf("AnalysisConfig.PhaseLabels length = %d, expected 2", len(config.PhaseLabels))
	}

	if !reflect.DeepEqual(config.Phases, phases) {
		t.Errorf("AnalysisConfig.Phases = %v, expected %v", config.Phases, phases)
	}

	if !config.CreatedAt.Equal(now) {
		t.Errorf("AnalysisConfig.CreatedAt = %v, expected %v", config.CreatedAt, now)
	}
}

func testAnalysisConfigJSONSerialization(t *testing.T) {
	config := models.AnalysisConfig{
		ScalingFactor: 500,
		WindowSize:    25,
		PhaseLabels:   []string{"Start", "End"},
		Phases: []models.TimeRange{
			{Start: 0.0, End: 0.5},
		},
		CreatedAt: time.Now(),
	}

	jsonData, err := json.Marshal(config)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "scaling_factor") {
		t.Error("JSON does not contain 'scaling_factor' field")
	}

	if !containsField(jsonString, "window_size") {
		t.Error("JSON does not contain 'window_size' field")
	}

	if !containsField(jsonString, "phase_labels") {
		t.Error("JSON does not contain 'phase_labels' field")
	}

	if !containsField(jsonString, "phases") {
		t.Error("JSON does not contain 'phases' field")
	}

	if !containsField(jsonString, "created_at") {
		t.Error("JSON does not contain 'created_at' field")
	}
}

func testAnalysisConfigJSONDeserialization(t *testing.T) {
	jsonString := `{
		"scaling_factor": 2000,
		"window_size": 100,
		"phase_labels": ["Phase1", "Phase2", "Phase3"],
		"phases": [
			{"start": 0.0, "end": 1.0},
			{"start": 1.0, "end": 2.0}
		],
		"created_at": "2023-01-01T00:00:00Z"
	}`

	var config models.AnalysisConfig
	err := json.Unmarshal([]byte(jsonString), &config)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if config.ScalingFactor != 2000 {
		t.Errorf("Deserialized AnalysisConfig.ScalingFactor = %d, expected 2000", config.ScalingFactor)
	}

	if config.WindowSize != 100 {
		t.Errorf("Deserialized AnalysisConfig.WindowSize = %d, expected 100", config.WindowSize)
	}

	if len(config.PhaseLabels) != 3 {
		t.Errorf("Deserialized AnalysisConfig.PhaseLabels length = %d, expected 3", len(config.PhaseLabels))
	}

	if len(config.Phases) != 2 {
		t.Errorf("Deserialized AnalysisConfig.Phases length = %d, expected 2", len(config.Phases))
	}
}

func testAnalysisConfigValidation(t *testing.T) {
	// 測試有效配置
	validConfig := models.AnalysisConfig{
		ScalingFactor: 1000,
		WindowSize:    50,
		PhaseLabels:   []string{"Phase1"},
		Phases: []models.TimeRange{
			{Start: 0.0, End: 1.0},
		},
		CreatedAt: time.Now(),
	}

	if validConfig.ScalingFactor <= 0 {
		t.Error("Valid config should have positive scaling factor")
	}

	if validConfig.WindowSize <= 0 {
		t.Error("Valid config should have positive window size")
	}

	if len(validConfig.PhaseLabels) != len(validConfig.Phases) {
		t.Error("Phase labels and phases should have same length")
	}
}

// TestTimeRange 測試時間範圍結構
func TestTimeRange(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestTimeRangeCreation", testTimeRangeCreation},
		{"TestTimeRangeJSONSerialization", testTimeRangeJSONSerialization},
		{"TestTimeRangeJSONDeserialization", testTimeRangeJSONDeserialization},
		{"TestTimeRangeValidation", testTimeRangeValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testTimeRangeCreation(t *testing.T) {
	timeRange := models.TimeRange{
		Start: 1.5,
		End:   2.5,
	}

	if timeRange.Start != 1.5 {
		t.Errorf("TimeRange.Start = %f, expected 1.5", timeRange.Start)
	}

	if timeRange.End != 2.5 {
		t.Errorf("TimeRange.End = %f, expected 2.5", timeRange.End)
	}
}

func testTimeRangeJSONSerialization(t *testing.T) {
	timeRange := models.TimeRange{
		Start: 0.5,
		End:   1.5,
	}

	jsonData, err := json.Marshal(timeRange)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "start") {
		t.Error("JSON does not contain 'start' field")
	}

	if !containsField(jsonString, "end") {
		t.Error("JSON does not contain 'end' field")
	}
}

func testTimeRangeJSONDeserialization(t *testing.T) {
	jsonString := `{"start": 2.0, "end": 3.0}`

	var timeRange models.TimeRange
	err := json.Unmarshal([]byte(jsonString), &timeRange)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if timeRange.Start != 2.0 {
		t.Errorf("Deserialized TimeRange.Start = %f, expected 2.0", timeRange.Start)
	}

	if timeRange.End != 3.0 {
		t.Errorf("Deserialized TimeRange.End = %f, expected 3.0", timeRange.End)
	}
}

func testTimeRangeValidation(t *testing.T) {
	// 測試有效範圍
	validRange := models.TimeRange{
		Start: 0.0,
		End:   1.0,
	}

	if validRange.Start >= validRange.End {
		t.Error("Valid range should have Start < End")
	}

	// 測試無效範圍
	invalidRange := models.TimeRange{
		Start: 2.0,
		End:   1.0,
	}

	if invalidRange.Start < invalidRange.End {
		t.Error("Invalid range should have Start >= End")
	}
}

// TestProcessingOptions 測試處理選項結構
func TestProcessingOptions(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestProcessingOptionsCreation", testProcessingOptionsCreation},
		{"TestProcessingOptionsJSONSerialization", testProcessingOptionsJSONSerialization},
		{"TestProcessingOptionsJSONDeserialization", testProcessingOptionsJSONDeserialization},
		{"TestProcessingOptionsDefaults", testProcessingOptionsDefaults},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testProcessingOptionsCreation(t *testing.T) {
	options := models.ProcessingOptions{
		ValidateInput: true,
		Precision:     3,
		OutputFormat:  "json",
	}

	if !options.ValidateInput {
		t.Error("ProcessingOptions.ValidateInput should be true")
	}

	if options.Precision != 3 {
		t.Errorf("ProcessingOptions.Precision = %d, expected 3", options.Precision)
	}

	if options.OutputFormat != "json" {
		t.Errorf("ProcessingOptions.OutputFormat = %s, expected 'json'", options.OutputFormat)
	}
}

func testProcessingOptionsJSONSerialization(t *testing.T) {
	options := models.ProcessingOptions{
		ValidateInput: false,
		Precision:     2,
		OutputFormat:  "csv",
	}

	jsonData, err := json.Marshal(options)
	if err != nil {
		t.Errorf("JSON serialization failed: %v", err)
	}

	jsonString := string(jsonData)
	if !containsField(jsonString, "validate_input") {
		t.Error("JSON does not contain 'validate_input' field")
	}

	if !containsField(jsonString, "precision") {
		t.Error("JSON does not contain 'precision' field")
	}

	if !containsField(jsonString, "output_format") {
		t.Error("JSON does not contain 'output_format' field")
	}
}

func testProcessingOptionsJSONDeserialization(t *testing.T) {
	jsonString := `{
		"validate_input": true,
		"precision": 4,
		"output_format": "xml"
	}`

	var options models.ProcessingOptions
	err := json.Unmarshal([]byte(jsonString), &options)
	if err != nil {
		t.Errorf("JSON deserialization failed: %v", err)
	}

	if !options.ValidateInput {
		t.Error("Deserialized ProcessingOptions.ValidateInput should be true")
	}

	if options.Precision != 4 {
		t.Errorf("Deserialized ProcessingOptions.Precision = %d, expected 4", options.Precision)
	}

	if options.OutputFormat != "xml" {
		t.Errorf("Deserialized ProcessingOptions.OutputFormat = %s, expected 'xml'", options.OutputFormat)
	}
}

func testProcessingOptionsDefaults(t *testing.T) {
	// 測試零值
	var options models.ProcessingOptions

	if options.ValidateInput {
		t.Error("Default ProcessingOptions.ValidateInput should be false")
	}

	if options.Precision != 0 {
		t.Errorf("Default ProcessingOptions.Precision = %d, expected 0", options.Precision)
	}

	if options.OutputFormat != "" {
		t.Errorf("Default ProcessingOptions.OutputFormat = %s, expected empty string", options.OutputFormat)
	}
}

// 輔助函數：檢查 JSON 字符串是否包含字段
func containsField(jsonString, fieldName string) bool {
	return strings.Contains(jsonString, `"`+fieldName+`"`)
}

// 基準測試
func BenchmarkEMGDataJSONSerialization(b *testing.B) {
	emgData := models.EMGData{
		Time:     1.234,
		Channels: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(emgData)
		if err != nil {
			b.Errorf("JSON serialization failed: %v", err)
		}
	}
}

func BenchmarkEMGDatasetJSONSerialization(b *testing.B) {
	dataset := models.EMGDataset{
		Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0.1, 0.2, 0.3}},
			{Time: 0.1, Channels: []float64{0.4, 0.5, 0.6}},
			{Time: 0.2, Channels: []float64{0.7, 0.8, 0.9}},
		},
		OriginalTimePrecision: 3,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(dataset)
		if err != nil {
			b.Errorf("JSON serialization failed: %v", err)
		}
	}
}

func BenchmarkEMGDataJSONDeserialization(b *testing.B) {
	jsonString := `{"time": 1.234, "channels": [0.1, 0.2, 0.3, 0.4, 0.5]}`
	jsonData := []byte(jsonString)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var emgData models.EMGData
		err := json.Unmarshal(jsonData, &emgData)
		if err != nil {
			b.Errorf("JSON deserialization failed: %v", err)
		}
	}
}

// 測試完整的數據流
func TestDataFlow(t *testing.T) {
	// 創建原始數據
	originalData := []models.EMGData{
		{Time: 0.0, Channels: []float64{0.1, 0.2, 0.3}},
		{Time: 0.1, Channels: []float64{0.4, 0.5, 0.6}},
		{Time: 0.2, Channels: []float64{0.7, 0.8, 0.9}},
	}

	// 創建數據集
	dataset := models.EMGDataset{
		Headers:               []string{"Time", "Channel1", "Channel2", "Channel3"},
		Data:                  originalData,
		OriginalTimePrecision: 3,
	}

	// 序列化
	jsonData, err := json.Marshal(dataset)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// 反序列化
	var deserializedDataset models.EMGDataset
	err = json.Unmarshal(jsonData, &deserializedDataset)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// 驗證數據完整性
	if !reflect.DeepEqual(dataset, deserializedDataset) {
		t.Error("Data flow test failed: serialized and deserialized data are not equal")
	}
}

// 測試邊界情況
func TestEdgeCases(t *testing.T) {
	// 測試極小值
	smallData := models.EMGData{
		Time:     -1e-10,
		Channels: []float64{1e-15, -1e-15},
	}

	jsonData, err := json.Marshal(smallData)
	if err != nil {
		t.Errorf("Small values serialization failed: %v", err)
	}

	var deserializedSmall models.EMGData
	err = json.Unmarshal(jsonData, &deserializedSmall)
	if err != nil {
		t.Errorf("Small values deserialization failed: %v", err)
	}

	// 測試極大值
	largeData := models.EMGData{
		Time:     1e10,
		Channels: []float64{1e15, -1e15},
	}

	jsonData, err = json.Marshal(largeData)
	if err != nil {
		t.Errorf("Large values serialization failed: %v", err)
	}

	var deserializedLarge models.EMGData
	err = json.Unmarshal(jsonData, &deserializedLarge)
	if err != nil {
		t.Errorf("Large values deserialization failed: %v", err)
	}
}
