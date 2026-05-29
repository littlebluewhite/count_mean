package patterns

import (
	"testing"
)

// TestIsReservedName 釘住 package-level free function 的語意契約：
// case-insensitive、查 DefaultRegistry ReservedNames set、空字串與非保留名回 false。
func TestIsReservedName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"CON", true},
		{"con", true},  // case-insensitive
		{"COM1", true},
		{"com1", true}, // case-insensitive
		{"hello", false},
		{"", false},
		{"COMX", false},
		{"PRN", true},
		{"AUX", true},
		{"NUL", true},
		{"LPT9", true},
		{"lpt1", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsReservedName(tc.name)
			if got != tc.want {
				t.Errorf("IsReservedName(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
