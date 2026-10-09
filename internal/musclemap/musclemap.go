// Package musclemap 擁有「右側 EMG header → 標準肌肉短名 → 必要通道」這張表。
//
// cci 與 muscle_ratio 過去各自帶一份 shortNameMap、"R." header 解析與 8 條必要肌肉清單;
// 現在由 RightSideChannels 單一入口提供,兩個 caller 對同份 EMG 必取同一組通道
// (規則見 ADR-0034)。
//
// 為什麼放在獨立 package 而非 parsers / cci / muscle_ratio:
//   - cci 與 muscle_ratio 互不 import (對稱 sibling),需要中立的「下層」package。
//   - parsers 已肥,不該再加 domain-specific (muscle) 邏輯。
package musclemap

import (
	"fmt"
	"strings"
)

// shortNameMap 把 header 的 "R." 後綴 (大寫) 正規化成標準肌肉短名。
//
//nolint:gochecknoglobals // domain constants
var shortNameMap = map[string]string{
	"RA":    "RA",
	"ES":    "ES",
	"IL":    "IL",
	"GMAX":  "GMax",
	"RF":    "RF",
	"BF":    "BF",
	"TAIO":  "TAIO",
	"TA&IO": "TAIO",
	"MF":    "MF",
}

// requiredMuscles 是 map 中必須全部出現的 8 個標準短名 (檢查順序即回報順序)。
//
//nolint:gochecknoglobals // domain constants
var requiredMuscles = []string{"RA", "ES", "IL", "GMax", "RF", "BF", "TAIO", "MF"}

// MissingMuscleError 表示缺少必要肌肉通道;caller 以 errors.As 取出 Muscle,
// 自行決定訊息 (例如 cci 走 i18n)。
type MissingMuscleError struct {
	Muscle string // 標準短名,例如 "GMax"
}

func (e *MissingMuscleError) Error() string {
	return "缺少必要的肌肉通道: " + e.Muscle
}

// RightSideChannels 回傳 (標準短名 → 完整 header) 的右側 8 通道表。
//
// 規則:
//   - 只認 "R." 前綴且含 ":" 的 header;L.* 與其他格式一律略過 (不論排在 R.* 前或後),
//     避免 "L.RA" 與 "R.RA" 互相覆蓋。
//   - 同一短名出現兩次即 fail-fast (含兩個 header 的「重複的肌肉通道」錯誤),不 silent overwrite。
//   - 8 個必要肌肉 (RA/ES/IL/GMax/RF/BF/TAIO/MF) 任一缺失回 *MissingMuscleError。
//
// 回傳 map 方向取 short → header:兩個 caller 都是「以肌肉名查 EMG channel header」。
func RightSideChannels(headers []string) (map[string]string, error) {
	channelMap := make(map[string]string, len(headers))

	for _, header := range headers {
		short := rightShortName(header)
		if short == "" {
			continue
		}

		if err := assignShort(channelMap, short, header); err != nil {
			return nil, err
		}
	}

	for _, name := range requiredMuscles {
		if _, ok := channelMap[name]; !ok {
			return nil, &MissingMuscleError{Muscle: name}
		}
	}

	return channelMap, nil
}

// rightShortName 取出右側 header 的標準短名;非 "R." 前綴或未知肌肉回 ""。
//
//	"R.RA: EMG 1 (from ...) ->Filter->RMS []" → "RA"
//	"R.TA&IO: EMG 7 (...)"                    → "TAIO"
//	"L.RA: EMG 1 ..."                         → "" (左側略過)
//	"R RECTUS ABDOMINIS: ..."                 → "" (無 "R." 點前綴)
func rightShortName(header string) string {
	colonIdx := strings.Index(header, ":")
	if colonIdx < 0 {
		return ""
	}

	prefix := strings.TrimSpace(header[:colonIdx])
	if !strings.HasPrefix(prefix, "R.") {
		return ""
	}

	return shortNameMap[strings.ToUpper(prefix[2:])]
}

// assignShort 註冊 (short → header);short 已存在則回「重複肌肉通道」error 且不改寫 map。
//
//nolint:err113 // dynamic error includes header strings for user-facing diagnosis
func assignShort(channelMap map[string]string, short, header string) error {
	if existing, dup := channelMap[short]; dup {
		return fmt.Errorf(
			"重複的肌肉通道 %s:前者 %q vs 後者 %q", short, existing, header)
	}

	channelMap[short] = header

	return nil
}
