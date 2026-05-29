package patterns

import (
	"testing"
)

// TestInjectionDetector_IsReservedName_HonorsCustomRegistry 釘住 codex P2:
// detector method 必須 honor 注入的 registry(與其餘 Detect* 方法一致),
// 不可硬走 DefaultRegistry。
func TestInjectionDetector_IsReservedName_HonorsCustomRegistry(t *testing.T) {
	// 自訂 registry:ReservedNames 為空 → 該 detector 不該把任何名稱當保留字。
	customReg := &PatternRegistry{patterns: map[PatternCategory][]string{ReservedNames: {}}}
	d := NewInjectionDetectorWithRegistry(customReg)
	if d.IsReservedName("CON") {
		t.Errorf("detector with empty custom ReservedNames must NOT treat CON as reserved (should honor injected registry)")
	}
	// free function 仍走 DefaultRegistry → CON 仍為保留字,證明兩條路徑各自正確。
	if !IsReservedName("CON") {
		t.Errorf("free IsReservedName must still consult DefaultRegistry and treat CON as reserved")
	}
}

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
