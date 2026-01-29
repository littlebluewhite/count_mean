package synchronizer

import (
	"errors"
	"fmt"
	"math"
)

// Sampling frequency constants.
const (
	// MotionFrequencyHz is the motion capture sampling frequency.
	MotionFrequencyHz = 250.0
	// EMGFrequencyHz is the EMG sampling frequency.
	EMGFrequencyHz = 1000.0
	// ForceFrequencyHz is the force plate sampling frequency.
	ForceFrequencyHz = 1000.0
)

// Time synchronization errors.
var (
	// ErrStartTimeAfterEnd indicates the start time is after the end time.
	ErrStartTimeAfterEnd = errors.New("start time is after end time")
	// ErrNegativeOffset indicates a negative EMG motion offset.
	ErrNegativeOffset = errors.New("EMG motion offset cannot be negative")
	// ErrOffsetExceedsData indicates the offset exceeds motion data length.
	ErrOffsetExceedsData = errors.New("EMG motion offset exceeds motion data length")
)

// TimeSynchronizer 時間同步器.
type TimeSynchronizer struct {
	motionFreq float64 // Motion 採樣頻率
	emgFreq    float64 // EMG 採樣頻率
	forceFreq  float64 // 力板採樣頻率
}

// NewTimeSynchronizer 創建新的時間同步器.
func NewTimeSynchronizer() *TimeSynchronizer {
	return &TimeSynchronizer{
		motionFreq: MotionFrequencyHz,
		emgFreq:    EMGFrequencyHz,
		forceFreq:  ForceFrequencyHz,
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

// EMGTimeToMotionIndex 將 EMG 時間轉換為對應的 Motion index.
// Formula: Motion_index = EMG_time * MotionFreq + EMGMotionOffset.
func (ts *TimeSynchronizer) EMGTimeToMotionIndex(emgTime float64, emgMotionOffset int) int {
	motionIndex := int(math.Round(emgTime*ts.motionFreq)) + emgMotionOffset
	if motionIndex < 1 {
		motionIndex = 1
	}

	return motionIndex
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

// EMGTimeToForceTime Force 時間 = EMG 時間 + (EMGMotionOffset - 1) / MotionFreq.
func (ts *TimeSynchronizer) EMGTimeToForceTime(emgTime float64, emgMotionOffset int) float64 {
	emgTimeOffset := float64(emgMotionOffset-1) / ts.motionFreq
	return emgTime + emgTimeOffset
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

// FindNearestTimeIndex 在時間序列中找到最接近目標時間的索引.
func FindNearestTimeIndex(times []float64, targetTime float64) int {
	if len(times) == 0 {
		return -1
	}

	// 如果目標時間小於第一個時間
	if targetTime <= times[0] {
		return 0
	}

	// 如果目標時間大於最後一個時間
	if targetTime >= times[len(times)-1] {
		return len(times) - 1
	}

	// 二分查找
	left, right := 0, len(times)-1
	for left <= right {
		mid := (left + right) / 2

		if times[mid] == targetTime {
			return mid
		}

		if times[mid] < targetTime {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	// 找到最接近的值
	if left >= len(times) {
		return len(times) - 1
	}

	if right < 0 {
		return 0
	}

	// 比較 left 和 right 哪個更接近
	if math.Abs(times[left]-targetTime) < math.Abs(times[right]-targetTime) {
		return left
	}

	return right
}

// ValidateTimeSync 驗證時間同步參數.
func ValidateTimeSync(emgMotionOffset, _ /*emgDataLength*/, motionDataLength int) error {
	if emgMotionOffset < 0 {
		return fmt.Errorf("EMG Motion Offset 不能為負數 (%d): %w", emgMotionOffset, ErrNegativeOffset)
	}

	if emgMotionOffset > motionDataLength {
		return fmt.Errorf("EMG Motion Offset (%d) 超過 Motion 數據長度 (%d): %w",
			emgMotionOffset, motionDataLength, ErrOffsetExceedsData)
	}

	return nil
}
