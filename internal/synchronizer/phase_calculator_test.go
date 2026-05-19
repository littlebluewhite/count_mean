package synchronizer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"count_mean/internal/models"
)

func TestNewPhaseCalculator(t *testing.T) {
	pc := NewPhaseCalculator()
	assert.NotNil(t, pc)
}

func TestPhaseCalculator_ValidatePhaseOrder(t *testing.T) {
	pc := NewPhaseCalculator()

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
	pc := NewPhaseCalculator()

	// Batch T：force-time 欄位改 OptFloat，需用 MakeOpt 包裝。D/O 仍是 int。
	phasePoints := models.PhasePoints{
		P0: models.MakeOpt(1.0), // force time
		P1: models.MakeOpt(2.0), // force time
		P2: models.MakeOpt(3.0), // force time
		S:  models.MakeOpt(4.0), // force time
		C:  models.MakeOpt(5.0), // force time
		D:  350,                 // motion index (increased to be later than P0)
		T0: models.MakeOpt(6.0), // force time
		T:  models.MakeOpt(7.0), // force time
		O:  450,                 // motion index (increased)
		L:  models.MakeOpt(8.0), // force time
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
			// Batch T 後語意：P0 未提供 (NoOpt) 仍回 ErrPhaseValueZero。
			// 對應原本 "zero start value" 但用 OptFloat 區分「真實 0」與「未提供」。
			name: "unset start value (NoOpt)",
			phasePoints: models.PhasePoints{
				P0: models.NoOpt(), // 未提供
				P1: models.MakeOpt(2.0),
				P2: models.MakeOpt(3.0),
				S:  models.MakeOpt(4.0),
				C:  models.MakeOpt(5.0),
				D:  350,
				T0: models.MakeOpt(6.0),
				T:  models.MakeOpt(7.0),
				O:  450,
				L:  models.MakeOpt(8.0),
			},
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "phase value is zero or not set",
		},
		{
			name: "unset end value (NoOpt)",
			phasePoints: models.PhasePoints{
				P0: models.MakeOpt(1.0),
				P1: models.NoOpt(), // 未提供
				P2: models.MakeOpt(3.0),
				S:  models.MakeOpt(4.0),
				C:  models.MakeOpt(5.0),
				D:  350,
				T0: models.MakeOpt(6.0),
				T:  models.MakeOpt(7.0),
				O:  450,
				L:  models.MakeOpt(8.0),
			},
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "phase value is zero or not set",
		},
		{
			// D/O 仍是 int 0-sentinel — 任務範圍外保留。
			name: "zero motion index",
			phasePoints: models.PhasePoints{
				P0: models.MakeOpt(1.0),
				P1: models.MakeOpt(2.0),
				P2: models.MakeOpt(3.0),
				S:  models.MakeOpt(4.0),
				C:  models.MakeOpt(5.0),
				D:  0, // motion-index 0 = 未提供 sentinel
				T0: models.MakeOpt(6.0),
				T:  models.MakeOpt(7.0),
				O:  350,
				L:  models.MakeOpt(8.0),
			},
			startPhase:      "P0",
			endPhase:        "D",
			emgMotionOffset: 100,
			expectErr:       true,
			errorMsg:        "phase value is zero or not set",
		},
		{
			// Batch T 新增：Set=true Value=0 應視為合法的「t=0 真實時間」，
			// 不再被誤判為「未提供」。這是 OptFloat 重構的核心測試。
			name: "Set=true Value=0 is legitimate (Batch T regression)",
			phasePoints: models.PhasePoints{
				P0: models.MakeOpt(0.0), // t=0 是真實時間，不是 sentinel
				P1: models.MakeOpt(1.0),
				P2: models.MakeOpt(2.0),
				S:  models.MakeOpt(3.0),
				C:  models.MakeOpt(4.0),
				D:  350,
				T0: models.MakeOpt(5.0),
				T:  models.MakeOpt(6.0),
				O:  450,
				L:  models.MakeOpt(7.0),
			},
			startPhase:      "P0",
			endPhase:        "P1",
			emgMotionOffset: 100,
			expectErr:       false, // OptFloat 之後 t=0 不再是 ErrPhaseValueZero
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
	phases := GetAvailableStartPhases()

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
	phases := GetAvailableEndPhases()

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
	pc := NewPhaseCalculator()

	// Create test phase points with realistic values that maintain proper time ordering
	// Note: EMG time calculation depends on EMGMotionOffset
	// For Force time: EMG_time = Force_time - (offset-1)/250
	// For Motion index: EMG_time = (index - offset) / 250
	// Values must maintain proper ordering after EMG time conversion
	// Example with offset=1:
	//   P0 (force 10.0): EMG_time = 10.0 - 0 = 10.0
	//   D (motion 3000): EMG_time = (3000-1)/250 = 11.996
	phasePoints := models.PhasePoints{
		P0: models.MakeOpt(10.0),    // Force time
		P1: models.MakeOpt(999.999), // Very large force time
		P2: models.MakeOpt(12.0),
		S:  models.MakeOpt(13.0),
		C:  models.MakeOpt(14.0),
		D:  3000, // Motion index (EMG_time = (3000-offset)/250, must be > P0's EMG_time)
		T0: models.MakeOpt(16.0),
		T:  models.MakeOpt(17.0),
		O:  9999, // Large motion index
		L:  models.MakeOpt(18.0),
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
	pc := NewPhaseCalculator()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pc.ValidatePhaseOrder(models.PhaseP0, models.PhaseL)
	}
}

func BenchmarkPhaseCalculator_GetPhaseTimeRange(b *testing.B) {
	pc := NewPhaseCalculator()

	phasePoints := models.PhasePoints{
		P0: models.MakeOpt(1.0),
		P1: models.MakeOpt(2.0),
		P2: models.MakeOpt(3.0),
		S:  models.MakeOpt(4.0),
		C:  models.MakeOpt(5.0),
		D:  250,
		T0: models.MakeOpt(6.0),
		T:  models.MakeOpt(7.0),
		O:  350,
		L:  models.MakeOpt(8.0),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = pc.GetPhaseTimeRange(phasePoints, models.PhaseP0, models.PhaseL, 100)
	}
}

func BenchmarkGetAvailableStartPhases(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = GetAvailableStartPhases()
	}
}

// Test concurrent access.
func TestPhaseCalculator_ConcurrentAccess(t *testing.T) {
	pc := NewPhaseCalculator()

	phasePoints := models.PhasePoints{
		P0: models.MakeOpt(1.0),
		P1: models.MakeOpt(2.0),
		P2: models.MakeOpt(3.0),
		S:  models.MakeOpt(4.0),
		C:  models.MakeOpt(5.0),
		D:  250,
		T0: models.MakeOpt(6.0),
		T:  models.MakeOpt(7.0),
		O:  350,
		L:  models.MakeOpt(8.0),
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
