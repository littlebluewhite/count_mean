package synchronizer

import (
	"errors"
	"fmt"
	"math"

	"count_mean/internal/parsers"
)

// Time synchronization errors.
var (
	// ErrStartTimeAfterEnd indicates the start time is after the end time.
	ErrStartTimeAfterEnd = errors.New("start time is after end time")
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

// MotionIndexToTime Motion index 從 1 開始，時間從 0 開始.
func (ts *TimeSynchronizer) MotionIndexToTime(index int) float64 {
	if index < 1 {
		return 0
	}

	return float64(index-1) / ts.motionFreq
}

// TimeToMotionIndex 將時間轉換為最接近的 Motion index.
func (ts *TimeSynchronizer) TimeToMotionIndex(time float64) int {
	if time < 0 {
		return 1
	}
	// 四捨五入到最接近的 index
	index := int(math.Round(time*ts.motionFreq)) + 1
	if index < 1 {
		index = 1
	}

	return index
}

// MotionIndexToEMGTime 使用 EMGMotionOffset 進行偏移計算.
func (ts *TimeSynchronizer) MotionIndexToEMGTime(motionIndex, emgMotionOffset int) float64 {
	// EMG 時間 = (Motion_index - EMGMotionOffset) * (1/250)
	// 因為 EMGMotionOffset 表示 EMG 第一筆對應的 Motion index
	return float64(motionIndex-emgMotionOffset) / ts.motionFreq
}

// ForceTimeToMotionIndex 力板和 Motion 同步開始.
func (ts *TimeSynchronizer) ForceTimeToMotionIndex(forceTime float64) int {
	return ts.TimeToMotionIndex(forceTime)
}

// MotionIndexToForceTime 力板和 Motion 同步開始.
func (ts *TimeSynchronizer) MotionIndexToForceTime(motionIndex int) float64 {
	return ts.MotionIndexToTime(motionIndex)
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

// GetSyncedTimeRange 根據分期點類型和值，計算出三個系統的對應時間.
//
//nolint:revive // flag-parameter: bool params needed to distinguish motion index from force time
func (ts *TimeSynchronizer) GetSyncedTimeRange(
	startValue float64,
	startIsMotionIndex bool,
	endValue float64,
	endIsMotionIndex bool,
	emgMotionOffset int,
) (*SyncedTimeRange, error) {
	result := &SyncedTimeRange{}

	// 處理開始時間
	if startIsMotionIndex {
		// Motion index 類型
		startIndex := int(startValue)
		result.StartMotionIndex = startIndex
		result.StartForceTime = ts.MotionIndexToForceTime(startIndex)
		result.StartEMGTime = ts.MotionIndexToEMGTime(startIndex, emgMotionOffset)
	} else {
		// 力板時間類型
		result.StartForceTime = startValue
		result.StartMotionIndex = ts.ForceTimeToMotionIndex(startValue)
		result.StartEMGTime = ts.ForceTimeToEMGTime(startValue, emgMotionOffset)
	}

	// 處理結束時間
	if endIsMotionIndex {
		// Motion index 類型
		endIndex := int(endValue)
		result.EndMotionIndex = endIndex
		result.EndForceTime = ts.MotionIndexToForceTime(endIndex)
		result.EndEMGTime = ts.MotionIndexToEMGTime(endIndex, emgMotionOffset)
	} else {
		// 力板時間類型
		result.EndForceTime = endValue
		result.EndMotionIndex = ts.ForceTimeToMotionIndex(endValue)
		result.EndEMGTime = ts.ForceTimeToEMGTime(endValue, emgMotionOffset)
	}

	// 驗證時間範圍
	if result.StartEMGTime > result.EndEMGTime {
		return nil, fmt.Errorf("開始時間 (%.3f) 大於結束時間 (%.3f): %w",
			result.StartEMGTime, result.EndEMGTime, ErrStartTimeAfterEnd)
	}

	return result, nil
}

// SyncedTimeRange 同步的時間範圍.
type SyncedTimeRange struct {
	StartMotionIndex int     // 開始 Motion index
	EndMotionIndex   int     // 結束 Motion index
	StartForceTime   float64 // 開始力板時間
	EndForceTime     float64 // 結束力板時間
	StartEMGTime     float64 // 開始 EMG 時間
	EndEMGTime       float64 // 結束 EMG 時間
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
// FindNearestTimeIndex 的既有行為(零成本),目前無 caller 在 inRange=false 時用它。
// 所有 caller 一律先看 inRange;idx 只在 inRange=true 時被消費。
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
