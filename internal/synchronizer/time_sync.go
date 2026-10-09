package synchronizer

import (
	"errors"
	"fmt"
	"math"

	"count_mean/internal/models"
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

// OutsideEMG 回報 t 是否落在 [[EMG time axis]] [times[0], times[len-1]](含 ±emgTimeEpsilon
// 邊界容差)之外,以及在哪一側。times 須升冪排序。這是「t 在不在 EMG 資料內」的
// 唯一規則:ResolveTimeIndex 的 inRange 即 !before && !after(ADR-0030、ADR-0043)。
//
//	t < times[0]−ε           → (true, false)
//	t > times[len−1]+ε       → (false, true)
//	在範圍內(或邊界 ±ε)     → (false, false)
//	len(times)==0 或 t 為 NaN → (true, true):空軸沒有「內」,NaN 無從比較
//
// 比較寫成 !(t >= lo) / !(t <= hi):不能證明在範圍內就算越界(首 / 末筆為 NaN 時,
// 該側也回 true)。
func OutsideEMG(times []float64, t float64) (before, after bool) {
	if len(times) == 0 {
		return true, true
	}

	before = !(t >= times[0]-emgTimeEpsilon)
	after = !(t <= times[len(times)-1]+emgTimeEpsilon)

	return before, after
}

// ErrTimeRangeNotFound 表示 SliceEMG 的區間(毫秒取整後)內沒有任何 EMG sample。
var ErrTimeRangeNotFound = errors.New("no data found in time range")

// EMGSlice 是 SliceEMG 的結果。Data 的 Time 與各通道是原資料的子切片(共用底層
// 陣列、不複製),Headers 沿用原 slice。
type EMGSlice struct {
	Data            *models.PhaseSyncEMGData
	ActualStartTime float64 // 實際選取的第一筆 sample 時間
	ActualEndTime   float64 // 實際選取的最後一筆 sample 時間
}

// msPerSecond 是 SliceEMG 毫秒取整的換算係數。
const msPerSecond = 1000

// SliceEMG 是 [[EMG time axis]] 唯一的切片規則(ADR-0043):start、end 與每筆 sample
// 都先 math.Round 到整數毫秒,再取含端點的 [startMs, endMs]。掃描在第一筆取整後
// 超過 endMs 的 sample 停止;d.Time 須升冪排序。
//
// 切片刻意不用 OutsideEMG 的 ±emgTimeEpsilon:manifest 的力板時間是毫秒精度的值,
// 以 float32 匯出、印到 6 位小數後帶 1e-6 起跳的雜訊(16.780001、17.809999),且隨
// 時間變大(32–64 s 可達 2e-6),超過 ε 時 ±ε 會靜默少切一筆邊界 sample;以毫秒
// 取整則與資料的有效精度一致。math.Round 單調,所以通過 OutsideEMG 的端點,其
// 邊界 sample 一定被切入。限制:假設 sample interval ≥ 1 ms;> 1 kHz 時相鄰 sample
// 可能取整到同一毫秒,區間兩端各可能多切入一筆(距端點 < 0.5 ms)。
//
//	d 為 nil 或 Time 為空 → parsers.ErrNilData
//	start > end           → 錯誤(無 sentinel)
//	區間內沒有 sample      → ErrTimeRangeNotFound
//
//nolint:err113 // start > end 的動態錯誤沿用既有 user-facing 字樣
func SliceEMG(d *models.PhaseSyncEMGData, start, end float64) (*EMGSlice, error) {
	// 空 Time slice 也視為空資料一併 reject,避免下方索引存取 panic。
	if d == nil || len(d.Time) == 0 {
		return nil, fmt.Errorf("EMG 數據為空: %w", parsers.ErrNilData)
	}

	if start > end {
		return nil, fmt.Errorf("開始時間 %.3f 不能大於結束時間 %.3f", start, end)
	}

	startMs := int64(math.Round(start * msPerSecond))
	endMs := int64(math.Round(end * msPerSecond))

	startIdx, endIdx := -1, -1
	for i, t := range d.Time {
		tMs := int64(math.Round(t * msPerSecond))
		if startIdx == -1 && tMs >= startMs {
			startIdx = i
		}

		if tMs <= endMs {
			endIdx = i
		} else if endIdx != -1 {
			break
		}
	}

	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return nil, fmt.Errorf("找不到有效的時間範圍數據: %w", ErrTimeRangeNotFound)
	}

	sliced := &models.PhaseSyncEMGData{
		Time:     d.Time[startIdx : endIdx+1],
		Channels: make(map[string][]float64, len(d.Channels)),
		Headers:  d.Headers,
	}
	for name, values := range d.Channels {
		sliced.Channels[name] = values[startIdx : endIdx+1]
	}

	return &EMGSlice{
		Data:            sliced,
		ActualStartTime: d.Time[startIdx],
		ActualEndTime:   d.Time[endIdx],
	}, nil
}

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

	before, after := OutsideEMG(times, target)
	inRange = !before && !after

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
