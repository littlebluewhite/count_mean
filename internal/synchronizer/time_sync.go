package synchronizer

import (
	"math"

	"count_mean/internal/parsers"
)

// TimeSynchronizer 時間同步器.
type TimeSynchronizer struct {
	motionFreq float64 // Motion 採樣頻率
}

// NewTimeSynchronizer 創建新的時間同步器.
func NewTimeSynchronizer() *TimeSynchronizer {
	return &TimeSynchronizer{
		motionFreq: parsers.FrequencyMotion,
	}
}

// MotionIndexToEMGTime 使用 EMGMotionOffset 進行偏移計算.
func (ts *TimeSynchronizer) MotionIndexToEMGTime(motionIndex, emgMotionOffset int) float64 {
	// EMG 時間 = (Motion_index - EMGMotionOffset) * (1/250)
	// 因為 EMGMotionOffset 表示 EMG 第一筆對應的 Motion index
	return float64(motionIndex-emgMotionOffset) / ts.motionFreq
}

// ForceTimeToEMGTime 因此：EMG時間 = ForceTime - (EMGMotionOffset - 1) / MotionFreq.
func (ts *TimeSynchronizer) ForceTimeToEMGTime(forceTime float64, emgMotionOffset int) float64 {
	// EMGMotionOffset 是 EMG 0秒對應的 Motion index
	// Motion index 1 對應 Motion 時間 0 秒
	// 所以 Motion 時間 = (index - 1) / 250
	// EMG 0 秒對應 Motion 時間 = (EMGMotionOffset - 1) / 250
	// EMG 時間 = Force 時間 - (EMGMotionOffset - 1) / 250
	emgTimeOffset := float64(emgMotionOffset-1) / ts.motionFreq
	return forceTime - emgTimeOffset
}

// emgTimeEpsilon 吸收 force-plate↔EMG 時間同步後的 ULP 飄移(~1e-7),設為比
// 1000Hz sample interval(1e-3)細 3 個量級的安全餘量:真實 out-of-range
// (>= 1ms ≈ 1e-3)仍被判越界,浮點 ULP 飄移(<= 1e-6)被吸收。
const emgTimeEpsilon = 1e-6

// ResolveTimeIndex 解析 target 到 times 中最接近的 sample 索引,並回報 target 是否
// 落在 [times[0], times[len-1]](含 ±emgTimeEpsilon 邊界容差)內。times 須升冪排序。
//
//	len(times)==0                          → (-1, false)
//	target 超出範圍+容差(idx clamp 到邊界) → (0 或 len-1, false)
//	target 在範圍內(或邊界 ±容差)          → (nearest, true)
//
// idx 與 inRange 正交:越界時 idx 仍 clamp 到邊界索引(0 / len-1)—— 純粹保留舊
// FindNearestTimeIndex 的既有行為(零成本)。caller 有兩種合法消費 idx 的方式:
// (a) 先看 inRange、僅在 true 時用 idx(CCI phaseAt/dropOutOfRangePhases、
// muscle_ratio collectPhasePoints 越界攔截);(b) in-range 已由 precondition 確立後
// 丟棄 inRange(CCI computeRow 凸中點、muscle_ratio buildPhasePoints 已驗證點)。
// 無 caller 在 in-range 未經確立時消費越界 clamp 的 idx。
func ResolveTimeIndex(times []float64, target float64) (idx int, inRange bool) {
	if len(times) == 0 {
		return -1, false
	}

	inRange = target >= times[0]-emgTimeEpsilon && target <= times[len(times)-1]+emgTimeEpsilon

	// idx:沿用既有 clamp(越界 snap 到邊界索引)+ 既有二分查找 kernel,邏輯一字不改,
	// 差別只是每個 return 多帶 inRange。
	if target <= times[0] {
		return 0, inRange
	}
	if target >= times[len(times)-1] {
		return len(times) - 1, inRange
	}

	left, right := 0, len(times)-1
	for left <= right {
		mid := (left + right) / 2
		if times[mid] == target {
			return mid, inRange
		}
		if times[mid] < target {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	if left >= len(times) {
		return len(times) - 1, inRange
	}
	if right < 0 {
		return 0, inRange
	}
	if math.Abs(times[left]-target) < math.Abs(times[right]-target) {
		return left, inRange
	}
	return right, inRange
}
