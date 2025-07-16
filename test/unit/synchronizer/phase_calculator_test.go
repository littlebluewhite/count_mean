package synchronizer

import (
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/synchronizer"

	"github.com/stretchr/testify/assert"
)

func TestNewPhaseCalculator(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()
	assert.NotNil(t, pc)
}

func TestPhaseCalculator_ValidatePhaseOrder(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	tests := []struct {
		name       string
		startPhase string
		endPhase   string
		expectErr  bool
		errorMsg   string
	}{
		{
			name:       "valid order P0 to P1",
			startPhase: "P0",
			endPhase:   "P1",
			expectErr:  false,
		},
		{
			name:       "valid order P1 to P2",
			startPhase: "P1",
			endPhase:   "P2",
			expectErr:  false,
		},
		{
			name:       "valid order P0 to S",
			startPhase: "P0",
			endPhase:   "S",
			expectErr:  false,
		},
		{
			name:       "valid order S to L",
			startPhase: "S",
			endPhase:   "L",
			expectErr:  false,
		},
		{
			name:       "valid order P0 to L (full range)",
			startPhase: "P0",
			endPhase:   "L",
			expectErr:  false,
		},
		{
			name:       "invalid order P1 to P0",
			startPhase: "P1",
			endPhase:   "P0",
			expectErr:  true,
			errorMsg:   "開始分期點 P1 必須在結束分期點 P0 之前",
		},
		{
			name:       "invalid order L to P0",
			startPhase: "L",
			endPhase:   "P0",
			expectErr:  true,
			errorMsg:   "開始分期點 L 必須在結束分期點 P0 之前",
		},
		{
			name:       "same phase",
			startPhase: "P0",
			endPhase:   "P0",
			expectErr:  true,
			errorMsg:   "開始分期點 P0 必須在結束分期點 P0 之前",
		},
		{
			name:       "unknown start phase",
			startPhase: "Unknown",
			endPhase:   "P1",
			expectErr:  true,
			errorMsg:   "未知的開始分期點: Unknown",
		},
		{
			name:       "unknown end phase",
			startPhase: "P0",
			endPhase:   "Unknown",
			expectErr:  true,
			errorMsg:   "未知的結束分期點: Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pc.ValidatePhaseOrder(tt.startPhase, tt.endPhase)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseCalculator_GetPhaseTimeRange(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	// Create test phase points
	phasePoints := models.PhasePoints{
		P0: 1.0, // force time
		P1: 2.0, // force time
		P2: 3.0, // force time
		S:  4.0, // force time
		C:  5.0, // force time
		D:  350, // motion index (increased to be later than P0)
		T0: 6.0, // force time
		T:  7.0, // force time
		O:  450, // motion index (increased)
		L:  8.0, // force time
	}

	tests := []struct {
		name            string
		phasePoints     models.PhasePoints
		startPhase      string
		endPhase        string
		emgMotionOffset int
		expectErr       bool
		errorMsg        string
		checkResult     func(*testing.T, *models.PhaseTimeRange)
	}{
		{
			name:            "P0 to P1 (force to force)",
			phasePoints:     phasePoints,
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       false,
			checkResult: func(t *testing.T, result *models.PhaseTimeRange) {
				assert.Equal(t, "force", result.StartType)
				assert.Equal(t, "force", result.EndType)
				assert.Greater(t, result.EndTime, result.StartTime)
			},
		},
		{
			name:            "P0 to D (force to motion)",
			phasePoints:     phasePoints,
			startPhase:      "P0",
			endPhase:        "D",
			emgMotionOffset: 100,
			expectErr:       false,
			checkResult: func(t *testing.T, result *models.PhaseTimeRange) {
				assert.Equal(t, "force", result.StartType)
				assert.Equal(t, "motion", result.EndType)
				assert.Greater(t, result.EndTime, result.StartTime)
			},
		},
		{
			name:            "D to O (motion to motion)",
			phasePoints:     phasePoints,
			startPhase:      "D",
			endPhase:        "O",
			emgMotionOffset: 100,
			expectErr:       false,
			checkResult: func(t *testing.T, result *models.PhaseTimeRange) {
				assert.Equal(t, "motion", result.StartType)
				assert.Equal(t, "motion", result.EndType)
				assert.Greater(t, result.EndTime, result.StartTime)
			},
		},
		{
			name:            "O to L (motion to force)",
			phasePoints:     phasePoints,
			startPhase:      "O",
			endPhase:        "L",
			emgMotionOffset: 100,
			expectErr:       false,
			checkResult: func(t *testing.T, result *models.PhaseTimeRange) {
				assert.Equal(t, "motion", result.StartType)
				assert.Equal(t, "force", result.EndType)
				assert.Greater(t, result.EndTime, result.StartTime)
			},
		},
		{
			name: "zero start value",
			phasePoints: models.PhasePoints{
				P0: 0.0, // Invalid: zero value
				P1: 2.0,
				P2: 3.0,
				S:  4.0,
				C:  5.0,
				D:  350,
				T0: 6.0,
				T:  7.0,
				O:  450,
				L:  8.0,
			},
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "開始分期點 P0 的值為 0 或未設置",
		},
		{
			name: "zero end value",
			phasePoints: models.PhasePoints{
				P0: 1.0,
				P1: 0.0, // Invalid: zero value
				P2: 3.0,
				S:  4.0,
				C:  5.0,
				D:  350,
				T0: 6.0,
				T:  7.0,
				O:  450,
				L:  8.0,
			},
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "結束分期點 P1 的值為 0 或未設置",
		},
		{
			name: "zero motion index",
			phasePoints: models.PhasePoints{
				P0: 1.0,
				P1: 2.0,
				P2: 3.0,
				S:  4.0,
				C:  5.0,
				D:  0, // Invalid: zero motion index
				T0: 6.0,
				T:  7.0,
				O:  350,
				L:  8.0,
			},
			startPhase:      "P0",
			endPhase:        "D",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "結束分期點 D 的值為 0 或未設置",
		},
		{
			name:            "invalid phase order",
			phasePoints:     phasePoints,
			startPhase:      "P1",
			endPhase:        "P0",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "開始時間", // The actual error comes from time synchronizer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pc.GetPhaseTimeRange(
				tt.phasePoints,
				tt.startPhase,
				tt.endPhase,
				tt.emgMotionOffset,
			)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetAvailableStartPhases(t *testing.T) {
	phases := synchronizer.GetAvailableStartPhases()

	expected := []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
	assert.Equal(t, expected, phases)

	// Verify length
	assert.Len(t, phases, 10)

	// Verify specific phases are present
	assert.Contains(t, phases, "P0")
	assert.Contains(t, phases, "S")
	assert.Contains(t, phases, "L")
}

func TestGetAvailableEndPhases(t *testing.T) {
	phases := synchronizer.GetAvailableEndPhases()

	expected := []string{"P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
	assert.Equal(t, expected, phases)

	// Verify length
	assert.Len(t, phases, 9)

	// Verify P0 is not included (cannot be end phase)
	assert.NotContains(t, phases, "P0")

	// Verify other phases are present
	assert.Contains(t, phases, "P1")
	assert.Contains(t, phases, "S")
	assert.Contains(t, phases, "L")
}

func TestGetPhaseInfo(t *testing.T) {
	phaseInfo := synchronizer.GetPhaseInfo()

	// Verify length
	assert.Len(t, phaseInfo, 10)

	// Create a map for easier lookup
	infoMap := make(map[string]synchronizer.PhaseInfo)
	for _, info := range phaseInfo {
		infoMap[info.Name] = info
	}

	// Verify specific phase info
	tests := []struct {
		name         string
		expectedType string
		hasDesc      bool
	}{
		{"P0", "force", true},
		{"P1", "force", true},
		{"P2", "force", true},
		{"S", "force", true},
		{"C", "force", true},
		{"D", "motion", true},
		{"T0", "force", true},
		{"T", "force", true},
		{"O", "motion", true},
		{"L", "force", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, exists := infoMap[tt.name]
			assert.True(t, exists, "Phase %s should exist", tt.name)
			assert.Equal(t, tt.expectedType, info.Type)
			assert.Equal(t, tt.name, info.Name)

			if tt.hasDesc {
				assert.NotEmpty(t, info.Description)
			}
		})
	}
}

func TestFormatPhaseTime(t *testing.T) {
	tests := []struct {
		name             string
		value            float64
		isMotionIndex    bool
		expectedContains string
	}{
		{
			name:             "force time",
			value:            1.234,
			isMotionIndex:    false,
			expectedContains: "1.234 秒",
		},
		{
			name:             "motion index",
			value:            250.0,
			isMotionIndex:    true,
			expectedContains: "Index: 250",
		},
		{
			name:             "decimal motion index",
			value:            250.7,
			isMotionIndex:    true,
			expectedContains: "Index: 250", // Should be converted to int
		},
		{
			name:             "zero force time",
			value:            0.0,
			isMotionIndex:    false,
			expectedContains: "0.000 秒",
		},
		{
			name:             "zero motion index",
			value:            0.0,
			isMotionIndex:    true,
			expectedContains: "Index: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := synchronizer.FormatPhaseTime(tt.value, tt.isMotionIndex)
			assert.Contains(t, result, tt.expectedContains)
		})
	}
}

func TestPhaseCalculator_GetPhaseTimeRange_EdgeCases(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	// Create test phase points with extreme values
	phasePoints := models.PhasePoints{
		P0: 0.001,   // Very small force time
		P1: 999.999, // Very large force time
		P2: 3.0,
		S:  4.0,
		C:  5.0,
		D:  1, // Minimum motion index
		T0: 6.0,
		T:  7.0,
		O:  9999, // Large motion index
		L:  8.0,
	}

	tests := []struct {
		name            string
		startPhase      string
		endPhase        string
		emgMotionOffset int
		expectErr       bool
	}{
		{
			name:            "minimum motion index",
			startPhase:      "P0",
			endPhase:        "D",
			emgMotionOffset: 1,
			expectErr:       false,
		},
		{
			name:            "large motion index",
			startPhase:      "P0",
			endPhase:        "O",
			emgMotionOffset: 100,
			expectErr:       false,
		},
		{
			name:            "large force time range",
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       false,
		},
		{
			name:            "negative EMG offset",
			startPhase:      "P0",
			endPhase:        "P2",
			emgMotionOffset: -50,
			expectErr:       false, // Should be handled by time synchronizer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pc.GetPhaseTimeRange(
				phasePoints,
				tt.startPhase,
				tt.endPhase,
				tt.emgMotionOffset,
			)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				// Basic validation
				assert.LessOrEqual(t, result.StartTime, result.EndTime)
				assert.NotEmpty(t, result.StartType)
				assert.NotEmpty(t, result.EndType)
				assert.Contains(t, []string{"force", "motion"}, result.StartType)
				assert.Contains(t, []string{"force", "motion"}, result.EndType)
			}
		})
	}
}

// Benchmark tests
func BenchmarkPhaseCalculator_ValidatePhaseOrder(b *testing.B) {
	pc := synchronizer.NewPhaseCalculator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pc.ValidatePhaseOrder("P0", "L")
	}
}

func BenchmarkPhaseCalculator_GetPhaseTimeRange(b *testing.B) {
	pc := synchronizer.NewPhaseCalculator()

	phasePoints := models.PhasePoints{
		P0: 1.0,
		P1: 2.0,
		P2: 3.0,
		S:  4.0,
		C:  5.0,
		D:  250,
		T0: 6.0,
		T:  7.0,
		O:  350,
		L:  8.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pc.GetPhaseTimeRange(phasePoints, "P0", "L", 100)
	}
}

func BenchmarkGetAvailableStartPhases(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = synchronizer.GetAvailableStartPhases()
	}
}

func BenchmarkGetPhaseInfo(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = synchronizer.GetPhaseInfo()
	}
}

func BenchmarkFormatPhaseTime(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = synchronizer.FormatPhaseTime(123.456, false)
		_ = synchronizer.FormatPhaseTime(250.0, true)
	}
}

// Test concurrent access
func TestPhaseCalculator_ConcurrentAccess(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	phasePoints := models.PhasePoints{
		P0: 1.0,
		P1: 2.0,
		P2: 3.0,
		S:  4.0,
		C:  5.0,
		D:  250,
		T0: 6.0,
		T:  7.0,
		O:  350,
		L:  8.0,
	}

	// Run multiple goroutines simultaneously
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				// Test ValidatePhaseOrder
				err := pc.ValidatePhaseOrder("P0", "L")
				assert.NoError(t, err)

				// Test GetPhaseTimeRange
				result, err := pc.GetPhaseTimeRange(phasePoints, "P0", "L", 100)
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
