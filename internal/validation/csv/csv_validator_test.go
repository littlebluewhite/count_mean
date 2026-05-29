package csv

import (
	"strings"
	"testing"
)

// TestValidateCSVData_RejectsDuplicateHeader 釘住 CSV header 不允許出現重複
// 的欄位名。下游 EMG / motion parser 依賴「header → column index」單射映射
// （map[string]int），重複的 header 會 silently 覆寫對應 index，後續資料解讀時
// 把不同 channel 的數值對到同一個邏輯 channel，造成完全錯誤的分析結果而沒有
// 任何錯誤訊息。validator 必須擋在資料進來的第一關。
func TestValidateCSVData_RejectsDuplicateHeader(t *testing.T) {
	v := NewValidator()

	cases := []struct {
		name    string
		records [][]string
	}{
		{
			name: "兩個欄位完全相同",
			records: [][]string{
				{"Time", "Channel1", "Channel1"},
				{"0.1", "100", "200"},
			},
		},
		{
			name: "case-insensitive 視為重複（CSV 工具通常 case-insensitive）",
			records: [][]string{
				{"Time", "channel1", "Channel1"},
				{"0.1", "100", "200"},
			},
		},
		{
			name: "前後空白後仍相同",
			records: [][]string{
				{"Time", "Channel1", " Channel1 "},
				{"0.1", "100", "200"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := v.ValidateCSVData(c.records, "dup.csv")
			if err == nil {
				t.Errorf("ValidateCSVData header=%v 必須擋下重複 header，實際通過", c.records[0])
			}
		})
	}
}

// TestValidateCSVData_RejectsEmptyHeaderCell 釘住 header 中的「空字串」欄位
// 同樣是 silent 風險來源 — 下游用 strings.Index 找欄位時會抓到第一個空 header,
// 後續資料對應到錯誤 column。validator 必須擋。
func TestValidateCSVData_RejectsEmptyHeaderCell(t *testing.T) {
	v := NewValidator()

	cases := []struct {
		name    string
		records [][]string
	}{
		{
			name: "中間空字串 header",
			records: [][]string{
				{"Time", "", "Channel1"},
				{"0.1", "100", "200"},
			},
		},
		{
			name: "尾端空字串 header",
			records: [][]string{
				{"Time", "Channel1", ""},
				{"0.1", "100", "200"},
			},
		},
		{
			name: "純空白 header (TrimSpace 後為空)",
			records: [][]string{
				{"Time", "   ", "Channel1"},
				{"0.1", "100", "200"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := v.ValidateCSVData(c.records, "empty.csv")
			if err == nil {
				t.Errorf("ValidateCSVData header=%v 必須擋下空 header cell，實際通過", c.records[0])
			}
		})
	}
}

// TestValidateCSVData_AllowsLegitimateUniqueHeader 是 sanity check：合法
// 不重複 header 必須仍通過。
func TestValidateCSVData_AllowsLegitimateUniqueHeader(t *testing.T) {
	v := NewValidator()

	records := [][]string{
		{"Time", "Channel1", "Channel2", "Subject ID"},
		{"0.1", "100", "200", "S1"},
		{"0.2", "150", "250", "S1"},
	}

	if err := v.ValidateCSVData(records, "ok.csv"); err != nil {
		t.Errorf("ValidateCSVData 合法 unique header 不該 reject，實際 err=%v", err)
	}
}

// TestValidateCSVData_EMGHeaderFalsePositive 釘住 EMG header `Subject ID` /
// `Frame ID` / `Mid Foot` / `=Channel1` 等合法欄位名先前被 cell-level SQL/Command/
// DangerousFunctions 比對誤判（且 formula starter `=` 對 channel 名稱也假陽性）。
//
// 修法：records[0] 由 ValidateCSVData 標 IsHeader=true 並 scoped 跳過 SQL/Command/
// DangerousFunctions 比對。**注意**：formula starter（`=`）仍會擋 header — `=Channel1`
// 在現實 EMG 檔幾乎不出現（Excel BTS 等工具不會用 `=` 開頭命名 channel），本契約假設
// 真 EMG header 不會以 `=` 開頭。若未來出現此種命名需求,需另行處理 formula starter 對
// header 的 scoping。
//
// 本 test 只 cover「不以 `=`/`@` 開頭」的 header — 那是 EMG 工具的真實 header 樣貌。
func TestValidateCSVData_EMGHeaderPassesWithFalsePositiveTerms(t *testing.T) {
	v := NewValidator()

	// EMG / Motion / ANC 真實 header 樣貌（從 input/SF8、test/testdata 採樣 + 合成）
	headerCases := [][]string{
		{"Subject ID", "Frame ID", "RA"},
		{"Time", "Mid Foot", "Forefoot"},
		{"Frame", "subject_id", "trial_id"},
		{"david", "Mid Foot", "Acceleration"},
		{"Wash", "Sid Vicious", "Aida"},
	}

	for _, header := range headerCases {
		t.Run(strings.Join(header, ","), func(t *testing.T) {
			records := [][]string{
				header,
				{"1", "2", "3"},
				{"1.5", "2.5", "3.5"},
			}

			err := v.ValidateCSVData(records, "emg.csv")
			if err != nil {
				t.Errorf("ValidateCSVData(header=%v) 不該 reject 合法 EMG header，實際 err=%v",
					header, err)
			}
		})
	}
}

// TestValidateCell_BodyRejectsDangerousFunctionPrefix 釘住 CSV injection P0:
// body cell 以危險函式名「開頭」(system(...)、importxml(...) 等)先前因
// checkDangerousFunctions 的 `!strings.HasPrefix(cell, fn)` guard 被 silently
// 放行(return nil)。移除 guard 後,DetectFormula 命中危險函式即拒絕。
func TestValidateCell_BodyRejectsDangerousFunctionPrefix(t *testing.T) {
	v := NewCellValidator()

	// 全部小寫且「以」危險函式名開頭 — 觸發 HasPrefix-guard 繞過。刻意避開會被
	// formula starter / script / sql / command / extension 攔截的字串,確保這些 cell
	// 的唯一守門點就是 checkDangerousFunctions。
	cases := []string{
		"system(x)",
		"importxml(x)",
		"indirect(x)",
	}

	for _, cell := range cases {
		t.Run(cell, func(t *testing.T) {
			ctx := NewCellContext(1, 1, "evil.csv") // body cell(IsHeader=false)
			if err := v.ValidateCell(cell, ctx); err == nil {
				t.Errorf("body cell %q 以危險函式開頭,必須被拒絕(CSV injection),實際通過", cell)
			}
		})
	}
}

// TestValidateCSVData_HeaderStillBlocksFormulaInjection 釘住 不能 weaken header
// 的 formula starter 守門 — `=cmd|/c calc!A1` 在 header 仍要被擋下（Excel 開啟 header
// 仍會 trigger formula 計算）。
func TestValidateCSVData_HeaderStillBlocksFormulaInjection(t *testing.T) {
	v := NewValidator()

	maliciousHeaders := [][]string{
		{"Time", "=cmd|/c calc", "LA"},
		{"Time", "@SUM(1+1)", "LA"},
		{"=cmd|/c calc!A1", "RA", "LA"},
	}

	for _, header := range maliciousHeaders {
		t.Run(strings.Join(header, ","), func(t *testing.T) {
			records := [][]string{
				header,
				{"1", "2", "3"},
			}

			err := v.ValidateCSVData(records, "evil.csv")
			if err == nil {
				t.Errorf("ValidateCSVData(header=%v) 必須擋下 formula injection，實際通過", header)
			}
		})
	}
}

// TestValidateCSVData_HeaderStillBlocksControlChars 釘住 header 的 null byte /
// 控制字元仍要擋（這些在 Excel 開啟仍會 trigger render bug / parser confusion）。
func TestValidateCSVData_HeaderStillBlocksControlChars(t *testing.T) {
	v := NewValidator()

	records := [][]string{
		{"Time", "Channel1\x00", "LA"}, // null byte in header
		{"1", "2", "3"},
	}

	err := v.ValidateCSVData(records, "evil.csv")
	if err == nil {
		t.Error("ValidateCSVData header 含 null byte 必須擋下，實際通過")
	}
}

// TestValidateCSVData_HeaderStillBlocksScript 釘住 header 的 script injection
// 仍要擋（Excel 預覽 / web preview 會 render header）。
func TestValidateCSVData_HeaderStillBlocksScript(t *testing.T) {
	v := NewValidator()

	records := [][]string{
		{"Time", "<script>alert(1)</script>", "LA"},
		{"1", "2", "3"},
	}

	err := v.ValidateCSVData(records, "evil.csv")
	if err == nil {
		t.Error("ValidateCSVData header 含 script injection 必須擋下，實際通過")
	}
}

// TestValidateCSVData_BodyStillBlocksSQLAndCommand 釘住 沒 weaken body 守門：
// SQL / Command injection 在 body row 必須繼續被擋。
func TestValidateCSVData_BodyStillBlocksSQLAndCommand(t *testing.T) {
	v := NewValidator()

	cases := []struct {
		name  string
		cells []string
	}{
		{"sql union select", []string{"1", "union select * from users", "3"}},
		{"sql drop table", []string{"1", "drop table users", "3"}},
		{"command whoami", []string{"1", "whoami", "3"}},
		{"command curl", []string{"1", "curl http://evil.com", "3"}},
		{"path traversal", []string{"1", "../etc/passwd", "3"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := [][]string{
				{"Time", "RA", "LA"},
				tc.cells,
			}

			err := v.ValidateCSVData(records, "evil.csv")
			if err == nil {
				t.Errorf("ValidateCSVData body=%v 必須擋下 injection，實際通過", tc.cells)
			}
		})
	}
}

// TestValidateHeaderRow_SmokeForStreamingCaller 釘住 streaming 路徑契約：
// large_file_handler 跑 header 時呼 ValidateHeaderRow，必須對 EMG header 放行
// 但對 formula injection header 擋下。
func TestValidateHeaderRow_SmokeForStreamingCaller(t *testing.T) {
	v := NewValidator()

	// 放行：合法 EMG header
	if err := v.ValidateHeaderRow([]string{"Subject ID", "Frame ID", "Mid Foot"}, 1, -1,
		"emg.csv"); err != nil {
		t.Errorf("ValidateHeaderRow(EMG header) 不該 reject，實際 err=%v", err)
	}

	// 擋下：formula injection header
	if err := v.ValidateHeaderRow([]string{"Time", "=cmd|/c calc", "LA"}, 1, -1,
		"evil.csv"); err == nil {
		t.Error("ValidateHeaderRow(formula injection) 必須擋下，實際通過")
	}
}

// TestValidateCell_BodyAllowsBenignFunctionSubstrings 釘住 P2 回歸:body cell 含
// 危險函式「名」的子串但非呼叫(`shoulder`⊃`sh`、`systemic`⊃`system`、
// `evaluation`⊃`eval`、`Cash`/`Wash`⊃`sh`)必須放行。先前 checkDangerousFunctions 的
// DetectFormula 用 naive substring 比對(移除 !HasPrefix guard 後暴露),把這些合法
// EMG 內容全誤殺。修法把危險函式偵測 gate 在呼叫語法(後接 `(`)上。
func TestValidateCell_BodyAllowsBenignFunctionSubstrings(t *testing.T) {
	v := NewCellValidator()

	cases := []string{
		"shoulder",   // 含 `sh`
		"systemic",   // 含 `system`
		"Cash",       // 含 `sh`
		"evaluation", // 含 `eval`
		"Wash",       // 含 `sh`
	}
	for _, cell := range cases {
		t.Run(cell, func(t *testing.T) {
			ctx := NewCellContext(1, 1, "data.csv") // body cell(IsHeader=false)
			if err := v.ValidateCell(cell, ctx); err != nil {
				t.Errorf("body cell %q 為合法 EMG 內容(危險函式名子串但非呼叫),必須放行,實際 err=%v", cell, err)
			}
		})
	}
}

// TestValidateCell_BodyBlocksShadowedFunctionCall 釘住 no-shadowing 端到端契約:
// body cell 內早期良性子串(`wash` 的 `sh`)不可掩蓋後面真正的危險函式呼叫
// (`system(`)。確保 call-syntax gate 掃描所有出現位置,而非第一個子串就放行。
func TestValidateCell_BodyBlocksShadowedFunctionCall(t *testing.T) {
	v := NewCellValidator()
	ctx := NewCellContext(1, 1, "evil.csv") // body cell(IsHeader=false）
	if err := v.ValidateCell("wash system(1)", ctx); err == nil {
		t.Error(`body cell "wash system(1)" 含真實函式呼叫 system(,必須被拒(no-shadowing),實際通過`)
	}
}

// TestValidateCell_BodyBlocksMultilineFunctionCall 釘住 no-bypass 端到端:body cell
// 內危險函式名與 `(` 之間夾換行(quoted CSV multiline cell — checkControlChars 放行
// `\n`/`\r`,且 checkDangerousFunctions 先於 checkControlChars 跑)仍須被拒。
func TestValidateCell_BodyBlocksMultilineFunctionCall(t *testing.T) {
	v := NewCellValidator()

	cases := map[string]string{
		"lf":   "system\n(1)",
		"crlf": "system\r\n(1)",
	}
	for name, cell := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := NewCellContext(1, 1, "evil.csv")
			if err := v.ValidateCell(cell, ctx); err == nil {
				t.Errorf("body cell %q 跨行函式呼叫須被拒,實際通過(換行繞過)", cell)
			}
		})
	}
}
