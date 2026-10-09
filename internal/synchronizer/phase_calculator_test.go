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

// Benchmark tests.
func BenchmarkPhaseCalculator_ValidatePhaseOrder(b *testing.B) {
	pc := NewPhaseCalculator()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pc.ValidatePhaseOrder(models.PhaseP0, models.PhaseL)
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

	// Run multiple goroutines simultaneously
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				// Test ValidatePhaseOrder
				err := pc.ValidatePhaseOrder(models.PhaseP0, models.PhaseL)
				assert.NoError(t, err)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
