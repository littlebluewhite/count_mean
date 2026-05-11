package security_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"count_mean/internal/security"
)

// TestPathValidator_ConcurrentSetAndValidate exercises the race condition that
// existed when SetAllowedBasePaths and ValidateFilePath were invoked
// concurrently. Each goroutine independently sets the allow-list to its own
// base path, then validates a file path that should be allowed under that
// base path. Run with `go test -race` to detect data races; without the
// mutex this test panics or fails under the race detector.
func TestPathValidator_ConcurrentSetAndValidate(t *testing.T) {
	validator := security.NewPathValidator(nil)

	const goroutines = 32
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			base, err := filepath.Abs(fmt.Sprintf("/tmp/race-test-%d", idx))
			if err != nil {
				t.Errorf("goroutine %d: failed to compute absolute path: %v", idx, err)
				return
			}

			for j := 0; j < iterations; j++ {
				validator.SetAllowedBasePaths([]string{base})
				_ = validator.GetAllowedBasePaths()
				// Errors here are tolerated: another goroutine may have
				// overwritten the allow-list. We only care about preventing
				// data races and ensuring the validator stays consistent.
				_ = validator.ValidateFilePath(filepath.Join(base, "data.csv"))
			}
		}(i)
	}

	wg.Wait()
}

// TestPathValidator_GetAllowedBasePathsReturnsCopy verifies that the slice
// returned by GetAllowedBasePaths is an independent copy. Mutating it must
// not affect the validator's internal state.
func TestPathValidator_GetAllowedBasePathsReturnsCopy(t *testing.T) {
	expected, err := filepath.Abs("/tmp/copy-test")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	validator := security.NewPathValidator([]string{expected})

	got := validator.GetAllowedBasePaths()
	if len(got) != 1 || got[0] != expected {
		t.Fatalf("unexpected initial allow-list: %v", got)
	}

	got[0] = "/tmp/tampered"

	again := validator.GetAllowedBasePaths()
	if len(again) != 1 || again[0] != expected {
		t.Fatalf("validator state was mutated via returned slice: got %v, want [%s]", again, expected)
	}
}
