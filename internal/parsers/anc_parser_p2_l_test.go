package parsers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// TestParseANCDataLine_TruncatesExtraFields 釘住 契約:當 ANC data row 的
// fields 數量 > len(channelNames)+1,多餘的 column **一律 truncate**,不寫入
// forceData 也不 panic / OOB。channelNames 對應的前段 column 仍正確被 parse。
//
// 為何重要:某些 capture 軟體在 row 結尾加 schema 外的 quality flag column,
// 或惡意 input 可能塞入超量 column 嘗試讓 parser 越界。truncate 是 defense-in-depth:
//   - 對齊 ANC header `#Channels:` 宣告的 schema(single source of truth)
//   - 避免 OOB 寫入 forceData.Forces map(只會寫入 channelNames 明確列出的 channel)
//   - 不洩漏 schema 外的 attacker-controlled column 到下游 sliding window / phase sync
func TestParseANCDataLine_TruncatesExtraFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fields       []string
		channelNames []string
		wantTime     float64
		wantForces   map[string]float64
	}{
		{
			name:         "exact_match (sanity)",
			fields:       []string{"0.5", "10.0", "20.0"},
			channelNames: []string{"F1X", "F1Y"},
			wantTime:     0.5,
			wantForces:   map[string]float64{"F1X": 10.0, "F1Y": 20.0},
		},
		{
			name:         "extra_field_truncated",
			fields:       []string{"0.5", "10.0", "20.0", "999.0", "888.0"},
			channelNames: []string{"F1X", "F1Y"},
			wantTime:     0.5,
			// 多出的 999.0 / 888.0 必須 truncate,不應出現在 forceData.Forces 任何 key
			wantForces: map[string]float64{"F1X": 10.0, "F1Y": 20.0},
		},
		{
			name: "many_extra_fields_truncated (defense-in-depth boundary)",
			fields: []string{
				"1.0",
				"100.0", "200.0", // 真正 channel
				// 接下來都是 schema-外資料,必須全部 drop
				"7777.0", "8888.0", "9999.0", "11111.0", "22222.0",
			},
			channelNames: []string{"F1X", "F1Y"},
			wantTime:     1.0,
			wantForces:   map[string]float64{"F1X": 100.0, "F1Y": 200.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forceData := &models.ForceData{
				Time:   []float64{},
				Forces: make(map[string][]float64),
			}
			for _, ch := range tt.channelNames {
				forceData.Forces[ch] = []float64{}
			}

			ok := parseANCDataLine(tt.fields, tt.channelNames, forceData)
			require.True(t, ok, "合法 row 應該 return true")

			// time 必然 append
			require.Len(t, forceData.Time, 1, "time slice 應該有 1 個元素")
			require.InDelta(t, tt.wantTime, forceData.Time[0], 1e-9)

			// channelNames 對應的 channel 才應該出現,extra column 不應寫入任何 forces map key
			require.Len(t, forceData.Forces, len(tt.channelNames),
				"forces map key 數量應該等於 channelNames(extra fields 不該創造新 key)")

			for ch, expected := range tt.wantForces {
				require.Len(t, forceData.Forces[ch], 1, "channel %s 應該有 1 個元素", ch)
				require.InDelta(t, expected, forceData.Forces[ch][0], 1e-9,
					"channel %s value mismatch", ch)
			}

			// 額外確認:沒有任何 "schema-外" 的 channel 被意外建立
			for k := range forceData.Forces {
				_, expected := tt.wantForces[k]
				require.True(t, expected,
					"forces map 出現意料外 channel key %q (extra field 應被 truncate 而非建立新 channel)", k)
			}
		})
	}
}

// TestParseANCDataLine_EmptyChannelNames_TruncatesAllExtraFields 釘住邊界:
// channelNames 為空時,fields 全部視為「schema 外」,只有 time 被 append,
// 所有 column data 被 truncate / 拒絕。
func TestParseANCDataLine_EmptyChannelNames_TruncatesAllExtraFields(t *testing.T) {
	t.Parallel()

	forceData := &models.ForceData{
		Time:   []float64{},
		Forces: make(map[string][]float64),
	}

	fields := []string{"2.0", "999.0", "888.0", "777.0"}
	channelNames := []string{} // 完全空 schema

	ok := parseANCDataLine(fields, channelNames, forceData)
	require.True(t, ok, "row 仍合法 (time 可 parse),return true")

	require.Len(t, forceData.Time, 1)
	require.InDelta(t, 2.0, forceData.Time[0], 1e-9)

	// channelNames 為空 → forces map 維持空(無 key 被創建,truncate 所有 column data)
	require.Empty(t, forceData.Forces, "channelNames 空時,所有 fields 都應 truncate")
}
