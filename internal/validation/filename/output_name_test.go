package filename

import (
	"math/rand/v2"
	"strings"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
)

func TestSubjectOutputName(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		suffix  string
		want    string
	}{
		{"normal subject", "NSF1", "CCI_Rudolph", "NSF1_CCI_Rudolph"},
		{"empty subject falls back to untitled", "", "chart_composer", "untitled_chart_composer"},
		{"path separators collapse to single segment", "a/b\\c", "x", "a_b_c_x"},
		{"dynamic suffix (statistics)", "S1", "P0-P1_statistics", "S1_P0-P1_statistics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubjectOutputName(tt.subject, tt.suffix)
			assert.Equal(t, tt.want, got)
			// 不變量:輸出恆為單一 path segment,無法 traverse。
			assert.NotContains(t, got, "/")
			assert.NotContains(t, got, "\\")
			// 契約(load-bearing): = Sanitize(subject) + "_" + suffix。
			assert.Equal(t, Sanitize(tt.subject)+"_"+tt.suffix, got)
		})
	}
}

// TestSubjectOutputName_AlwaysValidName 是 ADR-0054 的 property:任何 subject 經
// SubjectOutputName 接上實際用到的 suffix 與副檔名,必過 ValidateName。io 的
// placeSubjectOutput 與兩個 PNG 下載 handler 因此不必各自再檢查檔名。
func TestSubjectOutputName_AlwaysValidName(t *testing.T) {
	// io placeSubjectOutput 與 gui PNG 下載實際用到的 suffix;分期點取最長的 T0。
	suffixes := []string{
		"CCI_Rudolph", "CCI_Rudolph_phases", "muscle_ratio", "muscle_ratio_phases_avg11",
		"normalized", "T0-T0_statistics", "normalized_norm-T0-T0_stats-T0-T0", "chart_composer",
	}
	exts := []string{".csv", ".png"}

	holds := func(subject string) bool {
		for _, suffix := range suffixes {
			for _, ext := range exts {
				name := SubjectOutputName(subject, suffix) + ext
				if err := ValidateName(name); err != nil {
					t.Logf("ValidateName(%q) = %v", name, err)
					return false
				}
			}
		}
		return true
	}

	// 任意 Unicode 字串(含控制字元、surrogate)。
	if err := quick.Check(holds, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}

	// 邊界:截斷到 200 bytes、保留名在中段 / 尾段、全是點。
	for _, subject := range []string{
		"", "..", "...", "CON", "a.NUL.b", ".CON", "CON.",
		strings.Repeat("CON.", 80),
		strings.Repeat("中", 100),
		strings.Repeat("a", 196) + ".CONX",
		strings.Repeat("a", 196) + ".CON",
	} {
		if !holds(subject) {
			t.Errorf("property 不成立: subject %q", subject)
		}
	}

	// 對抗性組合:保留名、點、分隔符、非法字元、控制 / format 字元、無效 UTF-8、多位元組。
	tokens := []string{
		"CON", "nul", "Lpt9", "com1", "AUX", "prn", ".", "..", " ", "/", `\`, ":", "C:",
		"*", "?", `"`, "<", ">", "|", "\t", "\x00", "\x1f",
		string(rune(0x202E)), string(rune(0xFEFF)), string(rune(0x200B)), string(rune(0x3000)),
		"\xff", "中", "a", "_", "-", "%",
	}
	r := rand.New(rand.NewPCG(54, 54))
	for range 2000 {
		var b strings.Builder
		for range r.IntN(160) {
			b.WriteString(tokens[r.IntN(len(tokens))])
		}
		if subject := b.String(); !holds(subject) {
			t.Fatalf("property 不成立: subject %q", subject)
		}
	}
}

// TestSubjectOutputKey 釘住 ADR-0056 的撞名身分:key 相同 ⇔ SubjectOutputName 在 macOS /
// Windows 上落到同一個檔(Sanitize 後同名、大小寫不同、NFC / NFD)。
func TestSubjectOutputKey(t *testing.T) {
	same := [][2]string{
		{"A/B", "A:B"},              // Sanitize 後都是 A_B
		{"SF8", "sf8"},              // case-insensitive
		{"caf\u00e9", "cafe\u0301"}, // NFC vs NFD
		{"", "untitled"},            // 空 subject 走 Sanitize 的 untitled fallback
	}
	for _, pair := range same {
		assert.Equal(t, SubjectOutputKey(pair[0]), SubjectOutputKey(pair[1]), "%q vs %q", pair[0], pair[1])
	}

	assert.NotEqual(t, SubjectOutputKey("SF8"), SubjectOutputKey("SF9"))
	assert.Equal(t, "a_b", SubjectOutputKey("A/B"), "key = NFC(ToLower(Sanitize(subject)))")
}
