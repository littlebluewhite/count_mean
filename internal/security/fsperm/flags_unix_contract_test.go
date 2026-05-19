//go:build !windows

package fsperm_test

import (
	"syscall"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestFsperm_UnixFlagsContainNoFollow pins the platform contract: on Unix
// (darwin / linux / *BSD), all four flag constants MUST include
// `syscall.O_NOFOLLOW`. Removing it would silently regress symlink-TOCTOU
// protection across every callsite (~12 OpenFile sites in the repo).
//
// Companion: `flags_windows_contract_test.go` asserts the opposite (Windows
// has no atomic equivalent and the constants do NOT carry the flag).
func TestFsperm_UnixFlagsContainNoFollow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		flags int
	}{
		{"WriteFlags", fsperm.WriteFlags},
		{"AppendFlags", fsperm.AppendFlags},
		{"ReadFlags", fsperm.ReadFlags},
		{"TmpCreateFlags", fsperm.TmpCreateFlags},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.flags&syscall.O_NOFOLLOW == 0 {
				t.Errorf(
					"%s missing syscall.O_NOFOLLOW — this would silently regress symlink-TOCTOU protection. "+
						"If this change is intentional, update the package doc in flags_unix.go.",
					tc.name,
				)
			}
		})
	}
}
