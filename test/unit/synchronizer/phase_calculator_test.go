package synchronizer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
)

func TestNewPhaseCalculator(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()
	assert.NotNil(t, pc)
}

func TestPhaseCalculator_ValidatePhaseOrder(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	tests := []struct {
		name       string
		startPhase models.PhasePoint
		endPhase   models.PhasePoint
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
			errorMsg:   "start phase must be before end phase",
		},
		{
			name:       "invalid order L to P0",
			startPhase: "L",
			endPhase:   "P0",
			expectErr:  true,
			errorMsg:   "start phase must be before end phase",
		},
		{
			name:       "same phase",
			startPhase: "P0",
			endPhase:   "P0",
			expectErr:  true,
			errorMsg:   "start phase must be before end phase",
		},
		{
			name:       "unknown start phase",
			startPhase: "Unknown",
			endPhase:   "P1",
			expectErr:  true,
			errorMsg:   "unknown phase point",
		},
		{
			name:       "unknown end phase",
			startPhase: "P0",
			endPhase:   "Unknown",
			expectErr:  true,
			errorMsg:   "unknown phase point",
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
		startPhase      models.PhasePoint
		endPhase        models.PhasePoint
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
			errorMsg:        "phase value is zero or not set",
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
			errorMsg:        "phase value is zero or not set",
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
			errorMsg:        "phase value is zero or not set",
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

	expected := []models.PhasePoint{
		models.PhaseP0, models.PhaseP1, models.PhaseP2, models.PhaseS, models.PhaseC,
		models.PhaseD, models.PhaseT0, models.PhaseT, models.PhaseO, models.PhaseL,
	}
	assert.Equal(t, expected, phases)

	// Verify length
	assert.Len(t, phases, 10)

	// Verify specific phases are present
	assert.Contains(t, phases, models.PhaseP0)
	assert.Contains(t, phases, models.PhaseS)
	assert.Contains(t, phases, models.PhaseL)
}

func TestGetAvailableEndPhases(t *testing.T) {
	phases := synchronizer.GetAvailableEndPhases()

	expected := []models.PhasePoint{
		models.PhaseP1, models.PhaseP2, models.PhaseS, models.PhaseC, models.PhaseD,
		models.PhaseT0, models.PhaseT, models.PhaseO, models.PhaseL,
	}
	assert.Equal(t, expected, phases)

	// Verify length
	assert.Len(t, phases, 9)

	// Verify P0 is not included (cannot be end phase)
	assert.NotContains(t, phases, models.PhaseP0)

	// Verify other phases are present
	assert.Contains(t, phases, models.PhaseP1)
	assert.Contains(t, phases, models.PhaseS)
	assert.Contains(t, phases, models.PhaseL)
}

func TestPhaseCalculator_GetPhaseTimeRange_EdgeCases(t *testing.T) {
	pc := synchronizer.NewPhaseCalculator()

	// Create test phase points with realistic values that maintain proper time ordering
	// Note: EMG time calculation depends on EMGMotionOffset
	// For Force time: EMG_time = Force_time - (offset-1)/250
	// For Motion index: EMG_time = (index - offset) / 250
	// Values must maintain proper ordering after EMG time conversion
	// Example with offset=1:
	//   P0 (force 10.0): EMG_time = 10.0 - 0 = 10.0
	//   D (motion 3000): EMG_time = (3000-1)/250 = 11.996
	phasePoints := models.PhasePoints{
		P0: 10.0,    // Force time
		P1: 999.999, // Very large force time
		P2: 12.0,
		S:  13.0,
		C:  14.0,
		D:  3000, // Motion index (EMG_time = (3000-offset)/250, must be > P0's EMG_time)
		T0: 16.0,
		T:  17.0,
		O:  9999, // Large motion index
		L:  18.0,
	}

	tests := []struct {
		name            string
		startPhase      models.PhasePoint
		endPhase        models.PhasePoint
		emgMotionOffset int
		expectErr       bool
	}{
		{
			name:            "small EMG offset with valid range",
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

// Benchmark tests.
func BenchmarkPhaseCalculator_ValidatePhaseOrder(b *testing.B) {
	pc := synchronizer.NewPhaseCalculator()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pc.ValidatePhaseOrder(models.PhaseP0, models.PhaseL)
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
		_, _ = pc.GetPhaseTimeRange(phasePoints, models.PhaseP0, models.PhaseL, 100)
	}
}

func BenchmarkGetAvailableStartPhases(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = synchronizer.GetAvailableStartPhases()
	}
}

// Test concurrent access.
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
				err := pc.ValidatePhaseOrder(models.PhaseP0, models.PhaseL)
				assert.NoError(t, err)

				// Test GetPhaseTimeRange
				result, err := pc.GetPhaseTimeRange(phasePoints, models.PhaseP0, models.PhaseL, 100)
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
