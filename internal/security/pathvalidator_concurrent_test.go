package security

import (
	"path/filepath"
	"testing"
)

// TestPathValidator_GetAllowedBasePathsReturnsCopy verifies that the slice
// returned by GetAllowedBasePaths is an independent copy. Mutating it must
// not affect the validator's internal state.
func TestPathValidator_GetAllowedBasePathsReturnsCopy(t *testing.T) {
	tmpDir := t.TempDir()
	expected := filepath.Join(tmpDir, "copy-test")

	validator := NewPathValidator([]string{expected})

	got := validator.GetAllowedBasePaths()
	if len(got) != 1 || got[0] != expected {
		t.Fatalf("unexpected initial allow-list: %v", got)
	}

	got[0] = filepath.Join(tmpDir, "tampered")

	again := validator.GetAllowedBasePaths()
	if len(again) != 1 || again[0] != expected {
		t.Fatalf("validator state was mutated via returned slice: got %v, want [%s]", again, expected)
	}
}
