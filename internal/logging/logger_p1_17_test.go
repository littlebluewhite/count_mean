package logging

import (
	"bytes"
	"strings"
	"testing"
)

// TestSanitizeMessage_P1_17_Patterns 釘住 新增的 sensitive pattern:
//
// 過去 sanitizeMessage 只攔常見 keyword (password / token / secret 等),不會
// redact 純 IPv6 / MAC / JWT / user home path。這些都是 EMG 醫療場景常見的
// PII 線索:
//   - IPv6: 內網 / VPN 地址,可追溯辦公網路結構
//   - MAC: 裝置硬體 ID,可追溯特定機台
//   - JWT: 即使過期仍含 user/role/issuer claim
//   - Path: /Users/<name>/, /Volumes/, /home/ 等 user home / 雲端 mount 前綴
//
// 加入這四條 pattern,確保走 logger.sanitizeMessage 的訊息都被遮罩。
// 路徑那條 reuse internal/security/redact.Paths() — single source of truth,
// 避免 logger 跟 gui/recover 兩處各維護一份 regex。
//
// 此 test 用 in-memory logger 餵 fixture message,讀回 output 驗各 pattern 都
// 被 redact 但 non-sensitive 部分保留。
func TestSanitizeMessage_P1_17_Patterns(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustNotLeak  []string
		mustPreserve []string
	}{
		{
			name:  "ipv6_address",
			input: "connection from 2001:0db8:85a3:0000:0000:8a2e:0370:7334 failed",
			mustNotLeak: []string{
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334",
				"2001:0db8:85a3",
			},
			mustPreserve: []string{
				"connection",
				"failed",
			},
		},
		{
			name:  "mac_address",
			input: "client device 00:1A:2B:3C:4D:5E connected",
			mustNotLeak: []string{
				"00:1A:2B:3C:4D:5E",
			},
			mustPreserve: []string{
				"connected",
				"client device",
			},
		},
		{
			name:  "jwt_token",
			input: "Authorization header: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			mustNotLeak: []string{
				"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
				"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			},
			mustPreserve: []string{
				"Authorization header",
			},
		},
		{
			name:  "user_home_path_macos",
			input: "open /Users/alice/patient/emg_001.csv: permission denied",
			mustNotLeak: []string{
				"/Users/alice",
				"/Users/",
			},
			mustPreserve: []string{
				"permission denied",
			},
		},
		{
			name:  "user_home_path_linux",
			input: "failed to read /home/bob/data/recording.csv",
			mustNotLeak: []string{
				"/home/bob",
				"/home/",
			},
			mustPreserve: []string{
				"failed to read",
			},
		},
		{
			name:  "windows_drive_letter",
			input: `cannot open C:\Users\carol\Documents\emg.csv`,
			mustNotLeak: []string{
				`C:\Users\carol`,
				`C:\Users`,
			},
			mustPreserve: []string{
				"cannot open",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(LevelInfo, &buf, false)
			got := logger.sanitizeMessage(tc.input)

			for _, leak := range tc.mustNotLeak {
				if strings.Contains(got, leak) {
					t.Errorf("sensitive fragment %q 仍出現在 sanitized message: %q",
						leak, got)
				}
			}
			for _, want := range tc.mustPreserve {
				if !strings.Contains(got, want) {
					t.Errorf("必要 fragment %q 不在 sanitized message: %q",
						want, got)
				}
			}
		})
	}
}

// TestSanitizeMessage_P1_17_NonMatchPreserved 守 不誤遮:non-sensitive
// 字串(無 IPv6 / MAC / JWT / path 樣)必須 round-trip 不變。
func TestSanitizeMessage_P1_17_NonMatchPreserved(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelInfo, &buf, false)

	cases := []string{
		"normal message no PII",
		"EMG 通道 1 RMS=0.42",
		"phase analysis complete",
		"data points: 12345",
		"version 1.2.3",
	}

	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := logger.sanitizeMessage(in)
			// 內容應該完全不變(除了 \r/\n escape,這幾個 case 都沒含)
			if got != in {
				t.Errorf("non-sensitive message 被誤遮: in=%q got=%q", in, got)
			}
		})
	}
}
