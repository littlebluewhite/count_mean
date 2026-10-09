package csv

import (
	"strings"
	"testing"
)

// TestValidateCSVData_RejectsStructuralProblems 釘住結構性缺陷：空 records、僅有
// header row、columns 數不一致、以及 header row 本身為空，都必須被拒絕。
func TestValidateCSVData_RejectsStructuralProblems(t *testing.T) {
	v := NewValidator()

	cases := []struct {
		name    string
		records [][]string
		// wantMsg 非空時改斷言錯誤訊息含此子串(而非僅 err!=nil)。用於「empty header
		// row」這種會「同時」觸發欄數不一致的 case:空 header 令 expectedColumns=0,
		// 後續 3 欄 body row 會在欄數檢查報錯 → 若只斷 err!=nil,header-為空 的拒絕
		// 分支被移除後仍因欄數錯誤誤綠。釘住 header 專屬訊息才能鑑別正確的拒絕來源。
		wantMsg string
	}{
		{
			name:    "empty records",
			records: [][]string{},
		},
		{
			name:    "header only",
			records: [][]string{{"Time", "Channel1", "Channel2"}},
		},
		{
			name: "inconsistent columns",
			records: [][]string{
				{"Time", "Channel1", "Channel2"},
				{"0.1", "100", "200"},
				{"0.2", "150"},
			},
		},
		{
			name: "empty header row",
			records: [][]string{
				{},
				{"0.1", "100", "200"},
			},
			wantMsg: "CSV 標題行為空",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.ValidateCSVData(tc.records, "test.csv")
			if err == nil {
				t.Errorf("ValidateCSVData(%q) 必須回傳 error，實際通過", tc.name)
				return
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("ValidateCSVData(%q) 錯誤訊息應含 %q(鑑別正確的拒絕分支),實際=%v",
					tc.name, tc.wantMsg, err)
			}
		})
	}
}

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

// TestValidateCSVData_AcceptsDottedHeaders 釘住讀取側只留 sanity check:
// 真實 EMG header 含 `R.Shoulder` 這類帶點的欄位名,不可被注入偵測誤判。
func TestValidateCSVData_AcceptsDottedHeaders(t *testing.T) {
	v := NewValidator()
	records := [][]string{
		{"Time", "R.Shoulder", "L.Shoulder"},
		{"1", "2", "3"},
	}
	if err := v.ValidateCSVData(records, "emg.csv"); err != nil {
		t.Errorf("ValidateCSVData(R.Shoulder header) 不該 reject,實際 err=%v", err)
	}
}

// TestCheckCells_RejectsOversizeCell 釘住 cell 長度上限(32KB),錯誤只帶 row/col。
func TestCheckCells_RejectsOversizeCell(t *testing.T) {
	big := strings.Repeat("a", 32769)
	err := CheckCells([][]string{{"h"}, {"ok", big}})
	if err == nil {
		t.Fatal("CheckCells 超長 cell 必須回傳 error")
	}
	if !strings.Contains(err.Error(), "第 2 行第 2 欄") {
		t.Errorf("錯誤訊息應含 row/col,實際=%v", err)
	}
}

// TestCheckCells_AllowsNonUTF8 釘住 CheckCells 只檢查長度:Big5 manifest 必須放行。
func TestCheckCells_AllowsNonUTF8(t *testing.T) {
	if err := CheckCells([][]string{{"\xa4\xa4\xa4\xe5"}}); err != nil {
		t.Errorf("CheckCells 不該檢查 UTF-8,實際 err=%v", err)
	}
}

// TestValidateCSVData_RejectsNonUTF8 釘住 user-picked pipeline 保留既有 UTF-8 檢查。
func TestValidateCSVData_RejectsNonUTF8(t *testing.T) {
	v := NewValidator()
	records := [][]string{
		{"Time", "Ch1"},
		{"1", "\xa4\xa4"},
	}
	if err := v.ValidateCSVData(records, "big5.csv"); err == nil {
		t.Error("ValidateCSVData 非 UTF-8 cell 必須拒絕,實際通過")
	}
}
