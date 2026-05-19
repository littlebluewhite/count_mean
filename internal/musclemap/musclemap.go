// Package musclemap 提供 EMG header → 短肌肉名 channel map 的共用 helper。
//
// 過去 muscle_ratio.BuildRightSideChannelMap 與 cci.BuildChannelMap 都做
// 「header → short name → map」的轉換,但只有前者偵測重複 short name 並 fail-fast,
// 後者 silent overwrite (last-wins by header order)。重複偵測邏輯抽到本套件,
// 兩個 caller 共用同一份契約,確保「同份 EMG 出現兩個 R.RA 被偵測」的行為對稱。
//
// 為什麼放在獨立 package 而非 parsers / cci / muscle_ratio:
//   - cci 與 muscle_ratio 互不 import (對稱 sibling),需要中立的「下層」package。
//   - parsers 已肥,不該再加 domain-specific (muscle) 邏輯。
//   - musclemap 體積極小 (僅一個 helper + error type),引入成本可忽略。
package musclemap

import (
	"fmt"
)

// AssignShort 在 channelMap 中註冊 (short → header)。若 short 已存在,回傳描述
// 「重複肌肉通道」的 error,讓 caller 立刻 fail-fast。
//
// 為什麼用 helper function 而非 method on map:caller 各自管理自己的 map (key
// type / value type 可能差異未來會出現),helper 只做 lookup + insert + dup-check
// 三步,把 dup 偵測收斂到單一實作。
//
// 用法:
//
//	if err := musclemap.AssignShort(cm, "RA", header); err != nil { return nil, err }
//
//nolint:err113 // dynamic error includes header strings for user-facing diagnosis
func AssignShort(channelMap map[string]string, short, header string) error {
	if existing, dup := channelMap[short]; dup {
		return fmt.Errorf(
			"重複的肌肉通道 %s:前者 %q vs 後者 %q", short, existing, header)
	}
	channelMap[short] = header
	return nil
}
