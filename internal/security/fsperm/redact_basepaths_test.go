package fsperm

import (
	"strings"
	"testing"
)

// TestRedactBasePaths_RedactsDirectoryNameIncludingLastSegment 釘住 codex R2 [P2]:
// base path 是目錄,其末段(可能是 patient 目錄名)必須被脫敏。直接
// redact.Paths(fmt.Sprintf("%v", basePaths)) 會因前導 "[" 與「保留 basename」雙重
// 漏洞洩漏目錄名;redactBasePaths 對每個 base append "/" 後 redact,強制連末段一併
// 脫成 "<redacted-path>/"。
//
// 用字串樣本(含 Windows 盤符)確保所有 OS 都驗到(redact 正則平台無關,字串測試
// 在 darwin/linux 也能驗 Windows 路徑分支;見 memory feedback_cross_platform_test_design)。
func TestRedactBasePaths_RedactsDirectoryNameIncludingLastSegment(t *testing.T) {
	cases := []struct {
		name   string
		bases  []string
		leaked []string // 這些子字串不可出現在輸出
	}{
		{"posix root single segment", []string{"/Patient123"}, []string{"Patient123"}},
		{
			"posix nested patient dir",
			[]string{"/Volumes/clinic/patient_Smith"},
			[]string{"patient_Smith", "clinic", "Volumes"},
		},
		{"windows drive patient dir", []string{`C:\clinic\patient_Jones`}, []string{"patient_Jones", "clinic"}},
		{"multiple bases", []string{"/Volumes/a/patientA", "/data/patientB"}, []string{"patientA", "patientB"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := redactBasePaths(tc.bases)
			for _, leak := range tc.leaked {
				if strings.Contains(out, leak) {
					t.Errorf("redactBasePaths(%v) = %q 洩漏了敏感段 %q", tc.bases, out, leak)
				}
			}
			if !strings.Contains(out, "<redacted-path>") {
				t.Errorf("redactBasePaths(%v) = %q 缺脫敏標記 <redacted-path>", tc.bases, out)
			}
		})
	}
}
