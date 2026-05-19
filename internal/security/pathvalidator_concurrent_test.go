package security

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestPathValidator_ConcurrentSetAndValidate exercises the race condition that
// existed when SetAllowedBasePaths and ValidateFilePath were invoked
// concurrently. Each goroutine independently sets the allow-list to its own
// base path, then validates a file path that should be allowed under that
// base path. Run with `go test -race` to detect data races; without the
// mutex this test panics or fails under the race detector.
func TestPathValidator_ConcurrentSetAndValidate(t *testing.T) {
	validator := NewPathValidator(nil)

	const goroutines = 32
	const iterations = 200

	// outer scope 取一次 t.TempDir()，避免 goroutine 內呼叫 t.TempDir() 的不確定性；
	// 子路徑用 filepath.Join 組合，已是 absolute，不需再 filepath.Abs。
	tmpRoot := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			base := filepath.Join(tmpRoot, fmt.Sprintf("race-test-%d", idx))

			for j := 0; j < iterations; j++ {
				// 忽略 error：base 永遠非空，這裡只測 race。
				_ = validator.SetAllowedBasePaths([]string{base})
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
