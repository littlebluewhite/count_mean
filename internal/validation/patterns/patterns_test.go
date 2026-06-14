package patterns

import (
	"reflect"
	"testing"
)

// TestPatternRegistry_GetRef_ContentParityWithGet 釘住 getRef 回傳與 Get 內容相同、
// 但不是同一 backing array(Get 回拷貝、getRef 回 ref)。
// 額外確認:mutate Get 的回傳不影響 registry 內部狀態(公共拷貝語意仍在)。
func TestPatternRegistry_GetRef_ContentParityWithGet(t *testing.T) {
	r := DefaultRegistry()

	for _, cat := range []PatternCategory{
		FormulaInjection, DangerousFunctions, ScriptInjection,
		SQLInjection, CommandInjection, DangerousChars,
		SuspiciousExtensions, ReservedNames, NumericMalicious,
	} {
		ref := r.getRef(cat)
		cp := r.Get(cat)

		// 內容必須相同。
		if !reflect.DeepEqual(ref, cp) {
			t.Errorf("category %q: getRef content != Get content", cat)
		}

		// 修改 Get 回傳不影響 registry 內部。
		if len(cp) > 0 {
			original := cp[0]
			cp[0] = "MUTATED"
			afterMutate := r.Get(cat)
			if afterMutate[0] != original {
				t.Errorf("category %q: mutating Get() return altered registry — 拷貝語意損壞", cat)
			}
		}
	}
}

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
