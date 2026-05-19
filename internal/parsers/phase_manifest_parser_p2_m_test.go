package parsers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPhaseManifestParser_RejectsUnreasonablePhase 釘住 maxReasonablePhase
// 從 1e9 (~31 年) 降到 86400 (1 day),範圍內 phase 通過,範圍外 phase 一律 reject
// (ErrPhaseManifestTimeTooLarge)。
//
// 為何此 boundary 重要:
//   - 真實 EMG/motion trial 時間皆 << 1 day,86400 已是「保守 + 大 N 倍緩衝」上限
//   - 攔截「ms 誤填 s」「epoch timestamp 灌入」「DoS 巨數」三大 input poisoning 來源
//   - 1e9 老上限對 epoch timestamp(2024 ≈ 1.7e9)無感,降到 86400 之後一律擋下
func TestPhaseManifestParser_RejectsUnreasonablePhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		phaseVal  string
		wantErr   bool
		errSentry error // 若 wantErr=true,assertErrIs
	}{
		// 合法值
		{"trial_3_5_seconds_typical", "3.5", false, nil},
		{"long_session_1800_seconds", "1800", false, nil},
		{"boundary_just_below_86400", "86399.9", false, nil},
		{"boundary_exactly_86400_rejected", "86400.1", true, ErrPhaseManifestTimeTooLarge},

		// 新上限攔截舊上限漏網
		{"epoch_timestamp_2024", "1.7e9", true, ErrPhaseManifestTimeTooLarge},
		{"ms_misinput_as_seconds", "1e8", true, ErrPhaseManifestTimeTooLarge},
		{"ten_day_overflow", "864000", true, ErrPhaseManifestTimeTooLarge},
		{"negative_overflow", "-86401", true, ErrPhaseManifestTimeTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 透過完整 ParseFile 路徑驗證:parseFloat 內呼 maxReasonablePhase。
			// P0 = tt.phaseVal,其餘填極小合法 phase 值(避開 monotonicity 與 86400 上限)
			// L 設為很大值,讓某些測試的 tt.phaseVal=86399.9 仍能維持 monotonicity。
			csvContent := "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
			// 注意:若 tt.phaseVal 是負值,後續欄位仍須單調遞增(因為 ParseFloat 失敗於上限
			// 時 monotonicity check 不會跑;但合法值情境必須 monotonic)。
			// 為簡化,後續欄位都填 86400(上限 1 day),確保 monotonicity 在合法區間。
			csvContent += "Subject1,motion.csv,force.anc,emg.csv,100," + tt.phaseVal +
				",86400,86400,86400,86400,1,86400,86400,2,86400"

			tmpFile := filepath.Join(t.TempDir(), "test_phase.csv")
			require.NoError(t, os.WriteFile(tmpFile, []byte(csvContent), 0o600))

			parser := NewPhaseManifestParser()
			_, err := parser.ParseFile(tmpFile)

			if tt.wantErr {
				require.Error(t, err, "phase=%s 應該被 reject", tt.phaseVal)
				if tt.errSentry != nil {
					require.True(t, errors.Is(err, tt.errSentry),
						"err 應 wrap %v,實際為 %v", tt.errSentry, err)
				}
			} else {
				require.NoError(t, err, "phase=%s 是合法值不該被 reject", tt.phaseVal)
			}
		})
	}
}

// TestPhaseManifestParser_MaxReasonablePhase_ConstValue 釘住 const 數值,
// 防止未來不小心改回 1e9 漏掉攔截 epoch / DoS 攻擊面。
func TestPhaseManifestParser_MaxReasonablePhase_ConstValue(t *testing.T) {
	t.Parallel()
	// 1 day = 24 * 3600 = 86400
	if maxReasonablePhase != 86400 {
		t.Errorf("maxReasonablePhase = %v, 應為 86400 (1 day)", maxReasonablePhase)
	}
}
