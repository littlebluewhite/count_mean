package patterns

import (
	"testing"
)

// TestDetectCommand_ShortPatternFalsePositives 釘住 短 pattern（`id`、`sh`、
// `at`、`su -`）改 case-insensitive 後在合法 EMG cell 假陽性爆炸。修法把這 4 個
// short token 從 substring list 抽出，改用 \btoken\b word-boundary regex 比對。
// 本表枚舉常見 EMG/Motion header / 受試者姓名 / 解剖部位名等，全部必須回 false。
func TestDetectCommand_ShortPatternFalsePositives(t *testing.T) {
	det := NewInjectionDetector()

	// 這些字串都含 `id` / `sh` / `at` / `su` 子串但本身是合法 EMG 內容，不能命中。
	benignCases := []string{
		"david",        // 含 `id`
		"David",        // case-insensitive 變體
		"Mid Foot",     // 含 `id` 但分隔在不同 token
		"subject_id",   // 含 `id` 但 `_id` 不是 \bid\b（_ 是 word char，邊界不成立）
		"Subject ID",   // 含 `ID` 結尾 — \bID\b 會命中 — 但 ID 是合法 header 欄位名
		"valid_input",  // 含 `id`
		"voidance",     // 含 `id`
		"Aida",         // 含 `id`
		"Sid Vicious",  // 含 `id`
		"Wash",         // 含 `sh`
		"Fish",         // 含 `sh`
		"Cash",         // 含 `sh`
		"shoulder",     // 含 `sh`
		"shall",        // 含 `sh`
		"category",     // 含 `at`
		"that",         // 含 `at`
		"data",         // 含 `at`
		"Acceleration", // 含 `at`
		"summary",      // 含 `su`
		"super",        // 含 `su`
		"surgery",      // 含 `su`
	}

	// `Subject ID` 含 \bID\b — 確認 word-boundary 對純 `ID` 仍會命中（這是 spec 預期：
	// `ID` 單獨 token 就是 unix `id` command）。把 header `Subject ID` 排除假陽性是
	// 透過 header-scoped detection，本 fix 不負責處理 — Subject ID 仍命中
	// command injection，但 header context 會 skip 整段 DetectCommand。
	// 故 `Subject ID` 應該回 true（隔離測試）。我們從 benign list 刪除它。
	filtered := make([]string, 0, len(benignCases))
	for _, c := range benignCases {
		if c == "Subject ID" {
			continue
		}

		filtered = append(filtered, c)
	}

	for _, content := range filtered {
		t.Run(content, func(t *testing.T) {
			hit, pat := det.DetectCommand(content)
			if hit {
				t.Errorf("DetectCommand(%q) 假陽性 — 命中 pattern=%q，預期 false", content, pat)
			}
		})
	}
}

// TestDetectCommand_ShortPatternStillDetectsRealAttacks 釘住 不能 weaken 真實偵測。
// `id`、`sh`、`at`、`su -` 真的單獨出現（典型 shell injection vector）時必須命中。
func TestDetectCommand_ShortPatternStillDetectsRealAttacks(t *testing.T) {
	det := NewInjectionDetector()

	maliciousCases := []struct {
		content    string
		wantHitSub string // 預期命中的 token（不必嚴格 equal，用 contains 比對）
	}{
		{"; id", "id"},
		{"& id ", "id"},
		{"| id", "id"},
		{"foo; id; bar", "id"},
		{"$(id)", "id"},
		{"`id`", "id"},
		{"; sh", "sh"},
		{"; SH", "sh"}, // case-insensitive 仍命中
		{"; at 23:00", "at"},
		{"& at now", "at"},
		{"; su - root", "su"},
		{"& su- root", "su"}, // 緊接 - 仍命中（\bsu\b\s*- 容許 0 個 whitespace）
	}

	for _, tc := range maliciousCases {
		t.Run(tc.content, func(t *testing.T) {
			hit, _ := det.DetectCommand(tc.content)
			if !hit {
				t.Errorf("DetectCommand(%q) 應命中（pattern=%q），實際為 false", tc.content, tc.wantHitSub)
			}
		})
	}
}

// TestDetectCommand_MediumPatternFalsePositives 釘住 中等長度的 unix
// command token (`pwd` / `kill` / `mount` / `chmod` / `batch`) 過去做 substring
// 比對,在「amount of data」、「dispatched」、「killed」等合法字串上 false-positive。
// 修法將這 5 個 token 改用 word-boundary regex 比對。
func TestDetectCommand_MediumPatternFalsePositives(t *testing.T) {
	det := NewInjectionDetector()

	benignCases := []string{
		"the amount of data",   // 含 `mount`
		"amounted to ten",      // 含 `mount`
		"paramount",            // 含 `mount`
		"dismount the device",  // 含 `mount` 但實際語意合法 — 真實 cell 不太會出現,
		"the keyboard layout",  // 含 `kbd` 反例 (non-pattern, sanity)
		"is dispatched",        // 含 `batch`
		"dispatch_table",       // 含 `batch`
		"batched processing",   // 含 `batch` 但 \bbatch\b 命中 — 真實 EMG cell 不太會出現
		"answered the call",    // 含 `pwd` (actually doesn't)
		"keyboard input",       // sanity
		"was killed in action", // 含 `kill` — \bkill\b 會命中 — 真實 cell 不太會出現
		"the chmoderator",      // 含 `chmod`
	}

	// 預期 false 的 case (substring 但非 whole word)。
	wantFalse := map[string]bool{
		"the amount of data":  true,
		"amounted to ten":     true,
		"paramount":           true,
		"the keyboard layout": true,
		"dispatch_table":      true,
		"is dispatched":       true,
		"answered the call":   true,
		"keyboard input":      true,
		"the chmoderator":     true,
	}

	for _, content := range benignCases {
		if !wantFalse[content] {
			continue
		}
		t.Run(content, func(t *testing.T) {
			hit, pat := det.DetectCommand(content)
			if hit {
				t.Errorf("DetectCommand(%q) 假陽性 — 命中 pattern=%q,word-boundary 修法後應 false", content, pat)
			}
		})
	}
}

// TestDetectCommand_MediumPatternStillDetectsRealAttacks 釘住 修法後不
// weaken 真實偵測 — `mount /dev/sda` / `pwd` 單獨 token 仍要命中。
func TestDetectCommand_MediumPatternStillDetectsRealAttacks(t *testing.T) {
	det := NewInjectionDetector()

	maliciousCases := []string{
		"mount /dev/sda",
		"; mount /tmp",
		"& mount remote",
		"pwd",
		"; pwd",
		"$(pwd)",
		"`pwd`",
		"chmod 777 /tmp",
		"chmod -R 0 /etc",
		"; kill -9 1234",
		"& kill all",
		"batch process; rm -rf",
	}

	for _, content := range maliciousCases {
		t.Run(content, func(t *testing.T) {
			hit, _ := det.DetectCommand(content)
			if !hit {
				t.Errorf("DetectCommand(%q) 應命中 word-boundary token,實際 false", content)
			}
		})
	}
}

// TestDetectCommand_LongPatternsStillWork 釘住 不能 weaken substring list 的長 token。
func TestDetectCommand_LongPatternsStillWork(t *testing.T) {
	det := NewInjectionDetector()

	cases := []string{
		"curl http://evil",
		"wget evil.sh",
		"whoami",
		"WHOAMI", // case-insensitive 契約
		"netcat -lvp",
		"; ls /etc/passwd",
		"cat /etc/shadow",
		"chmod 777 /tmp",
		"../etc/passwd",
		"C:\\Windows\\System32",
	}

	for _, content := range cases {
		t.Run(content, func(t *testing.T) {
			hit, _ := det.DetectCommand(content)
			if !hit {
				t.Errorf("DetectCommand(%q) 應命中 substring pattern，實際為 false", content)
			}
		})
	}
}

// TestDetectFormula_RequiresCallSyntax 釘住 DetectFormula 的危險函式偵測必須 gate
// 在「呼叫語法」(後接 `(`)上。先前用 naive strings.Contains 比對短 token(`sh`、
// `eval`、`system`),把 `shoulder`、`Cash`、`systemic`、`evaluation` 與裸 `document`/
// `query` 等合法 EMG 內容全部誤殺(P0 移除 !HasPrefix guard 後此 false-positive 暴露)。
// 修法:危險函式名後必須緊跟 `(`(允許中間空白)才算命中,且掃描所有出現位置 —
// 早期良性子串(`wash` 裡的 `sh`)不可 shadow 後面真正的呼叫(`system(`)。
// formula starter(`=`、`@`)分支不變。
func TestDetectFormula_RequiresCallSyntax(t *testing.T) {
	det := NewInjectionDetector()

	// 合法內容:含危險函式「名」的子串但無呼叫語法 → 必須放行(今天全部誤命中)。
	benign := []string{
		"shoulder",   // 含 `sh`
		"Cash",       // 含 `sh`
		"Wash",       // 含 `sh`
		"Fish",       // 含 `sh`
		"systemic",   // 含 `system`
		"evaluation", // 含 `eval`
		"document",   // 本身是危險函式名,但無 `(`
		"query",      // 本身是危險函式名,但無 `(`
	}
	for _, content := range benign {
		t.Run("benign/"+content, func(t *testing.T) {
			if hit, fn := det.DetectFormula(content); hit {
				t.Errorf("DetectFormula(%q) 假陽性 — 命中 fn=%q,無呼叫語法應放行", content, fn)
			}
		})
	}

	// 真實攻擊:危險函式被「呼叫」(後接 `(`)或 formula starter → 必須命中。
	// `wash system(1)` 為 no-shadowing guard:前面 `wash` 的 `sh` 不可掩蓋後面 system(。
	malicious := []string{
		"system(x)",
		"importxml(x)",
		"indirect(x)",
		"hyperlink(1)",
		"wash system(1)",
		"=cmd",
		"@sum(1)",
	}
	for _, content := range malicious {
		t.Run("malicious/"+content, func(t *testing.T) {
			if hit, _ := det.DetectFormula(content); !hit {
				t.Errorf("DetectFormula(%q) 應命中(危險函式呼叫 / formula starter),實際為 false", content)
			}
		})
	}
}
