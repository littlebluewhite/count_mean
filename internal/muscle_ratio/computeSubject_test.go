package muscle_ratio

// computeSubject_test.go — muscle_ratio compute core(computeSubject,ADR-0052)的 file-free 測試,
// 仿 cci 的 computeCCI_test.go:EMG 與 manifest row 都在記憶體內組出,不碰 disk。
//
// 時間換算(EMGMotionOffset=1):力板時間即 EMG 秒數;motion-index(D/O)→ (idx-1)/250 秒。

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/io"
	"count_mean/internal/models"
)

// mrEMGHeaders 是 8 個右側 EMG 通道 header,與 analyzer_test.go 的 emgHeaderLine(去掉時間欄)
// 逐字相同;順序對應 buildInMemoryEMG 的 chValue。
var mrEMGHeaders = [8]string{
	"R.RA: EMG 1 ->Filter->RMS []",
	"R.ES: EMG 2 ->Filter->RMS []",
	"R.IL: EMG 3 ->Filter->RMS []",
	"R.GMax: EMG 4 ->Filter->RMS []",
	"R.RF: EMG 5 ->Filter->RMS []",
	"R.BF: EMG 6 ->Filter->RMS []",
	"R.TA&IO: EMG 7 ->Filter->RMS []",
	"R.MF: EMG 8 ->Filter->RMS []",
}

// msTimes 回 n 筆 1000Hz 時間軸,第 i 筆為 (first+i)/1000 秒。整數除以 1000 得到的 float64
// 與 EMG parser 讀 "%.4f" 字串的結果相同(都是該十進位值最接近的 float64)。
func msTimes(first, n int) []float64 {
	times := make([]float64, n)
	for i := range times {
		times[i] = float64(first+i) / 1000
	}
	return times
}

// buildInMemoryEMG 在記憶體組出 8 通道 EMG:通道 h 在每筆 sample 都是 chValue[h]
// (順序同 mrEMGHeaders)。
func buildInMemoryEMG(times []float64, chValue [8]float64) *models.PhaseSyncEMGData {
	channels := make(map[string][]float64, len(mrEMGHeaders))
	for h, header := range mrEMGHeaders {
		values := make([]float64, len(times))
		for i := range values {
			values[i] = chValue[h]
		}
		channels[header] = values
	}

	return &models.PhaseSyncEMGData{
		Time:     times,
		Headers:  append([]string(nil), mrEMGHeaders[:]...),
		Channels: channels,
	}
}

// buildInMemoryMRFixture 仿 cci 的 buildInMemoryCCIFixture:
//
//   - EMG:200 筆 1000Hz(0.000~0.199s),8 通道常數 → RA/ES=2、IL/GMax=2、RF/BF=0.5、TAIO/MF=2。
//   - manifest row:10 個分期點填齊,EMGMotionOffset=1 →
//     P0=0.01 P1=0.02 P2=0.03 S=0.04 C=0.05 D=20(→0.076) T0=0.09 T=0.10 O=30(→0.116) L=0.15。
//
// 各測試再拿掉或改動個別分期點。
func buildInMemoryMRFixture() (*models.PhaseSyncEMGData, *models.PhaseManifest) {
	emg := buildInMemoryEMG(msTimes(0, 200), [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	m := &models.PhaseManifest{
		Subject:         "S1",
		EMGMotionOffset: 1,
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(0.01), P1: models.MakeOpt(0.02), P2: models.MakeOpt(0.03),
			S: models.MakeOpt(0.04), C: models.MakeOpt(0.05), D: 20,
			T0: models.MakeOpt(0.09), T: models.MakeOpt(0.10), O: 30, L: models.MakeOpt(0.15),
		},
	}

	return emg, m
}

// countPoints 回 points 中 Name == name 的列數;用於釘住「不重複」契約 — 某 midpoint
// 被誤生兩次時計數會抓到。
func countPoints(points []io.MuscleRatioPhasePoint, name string) int {
	count := 0
	for _, p := range points {
		if p.Name == name {
			count++
		}
	}
	return count
}

// computePoints 跑 computeSubject 並要求 Output 2 有產出(err、warn 皆為 nil),回 Output 2 的列。
func computePoints(t *testing.T, emg *models.PhaseSyncEMGData, m *models.PhaseManifest) []io.MuscleRatioPhasePoint {
	t.Helper()

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err)
	require.NoError(t, warn)

	return computed.points
}

func TestComputeSubject_MissingChannelFailFast(t *testing.T) {
	emg, m := buildInMemoryMRFixture()
	delete(emg.Channels, emg.Headers[7]) // 拿掉 R.MF
	emg.Headers = emg.Headers[:7]

	computed, warn, err := computeSubject(emg, m)
	require.Error(t, err, "缺通道時應回 core err")
	assert.ErrorContains(t, err, "缺少必要的肌肉通道")
	assert.NoError(t, warn)
	assert.Zero(t, computed, "core err 時不該有任何算好的值")
}

func TestComputeSubject_PhasePoints_FullPhasesYieldRows(t *testing.T) {
	emg, m := buildInMemoryMRFixture()

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err)
	require.NoError(t, warn)

	// Output 1:時間軸即 EMG 時間軸,4 個比值各一條序列
	assert.Equal(t, emg.Time, computed.times)
	require.Len(t, computed.ratios, 4)

	// 10 phases + 9 adjacent-pair midpoints + 2 biomechanical-interval midpoints(mid_S_D, mid_D_T)= 21
	points := computed.points
	require.Len(t, points, 21, "10 phases → 19 adjacent + 2 interval = 21 points expected")

	// 每列 4 個比值:RA/ES = 2/1、IL/GMax = 3/1.5、RF/BF = 1/2、TAIO/MF = 4/2
	for _, p := range points {
		require.Len(t, p.Values, 4)
		assert.InDelta(t, 2.0, p.Values[0], 1e-9, "RA/ES at %s", p.Name)
		assert.InDelta(t, 2.0, p.Values[1], 1e-9, "IL/GMax at %s", p.Name)
		assert.InDelta(t, 0.5, p.Values[2], 1e-9, "RF/BF at %s", p.Name)
		assert.InDelta(t, 2.0, p.Values[3], 1e-9, "TAIO/MF at %s", p.Name)
	}

	// 兩個 biomechanical-interval midpoints 都要生成,且時間正確
	var midSD, midDT *io.MuscleRatioPhasePoint
	for i := range points {
		switch points[i].Name {
		case "mid_S_D":
			midSD = &points[i]
		case "mid_D_T":
			midDT = &points[i]
		}
	}
	require.NotNil(t, midSD, "biomechanical-interval midpoint mid_S_D missing")
	require.NotNil(t, midDT, "biomechanical-interval midpoint mid_D_T missing")
	assert.InDelta(t, 0.058, midSD.Time, 1e-9, "mid_S_D time should be (S=0.04 + D=0.076)/2 = 0.058")
	assert.InDelta(t, 0.088, midDT.Time, 1e-9, "mid_D_T time should be (D=0.076 + T=0.10)/2 = 0.088")

	// 各列依時間嚴格遞增 — 穿插式排序契約(time-sorted interleave)。
	for i := 1; i < len(points); i++ {
		assert.Less(t, points[i-1].Time, points[i].Time,
			"point %d (%s) must be before point %d (%s)", i-1, points[i-1].Name, i, points[i].Name)
	}
}

func TestComputeSubject_PhasePoints_NoIntervalMidpointsWhenSDTAbsent(t *testing.T) {
	// 5 個有效分期 P0–C(無 S/D/T)→ 5 + 4 adjacent-pair midpoints = 9 列。
	// 因 D 缺失,mid_S_D 與 mid_D_T 都無法生成;biomechanical-interval midpoints 為 0。
	// 釘住「S/D/T 任一缺失 → 對應 skip-midpoint 完全不生成」契約。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.D = 0
	m.PhasePoints.T0 = models.NoOpt()
	m.PhasePoints.T = models.NoOpt()
	m.PhasePoints.O = 0
	m.PhasePoints.L = models.NoOpt()

	points := computePoints(t, emg, m)
	assert.Len(t, points, 9, "5 phases without S/D/T → 9 points (2N-1, no interval midpoints)")
	assert.Zero(t, countPoints(points, "mid_S_D"), "mid_S_D should not exist when D is absent")
	assert.Zero(t, countPoints(points, "mid_D_T"), "mid_D_T should not exist when D/T are absent")
}

func TestComputeSubject_PhasePoints_MissingS_OnlyMidDT(t *testing.T) {
	// 9 phases without S → 9 phase rows + 8 adjacent midpoints + 1 interval (mid_D_T) = 18 列。
	// mid_S_D 無法生成（S 缺失）;mid_D_T 仍可生成（D 與 T 都在）。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.S = models.NoOpt()

	points := computePoints(t, emg, m)
	assert.Len(t, points, 18, "9 phases (no S) → 17 adjacent + 1 interval (mid_D_T) = 18 points")
	assert.Zero(t, countPoints(points, "mid_S_D"), "mid_S_D should not exist when S is absent")
	assert.Equal(t, 1, countPoints(points, "mid_D_T"), "mid_D_T should still be generated when D and T are present")
}

func TestComputeSubject_PhasePoints_MissingD_NoIntervalMidpoints(t *testing.T) {
	// 9 phases without D → 9 phase rows + 8 adjacent midpoints + 0 interval = 17 列。
	// 兩個 interval midpoints 都無法生成,因為 D 同時是 mid_S_D 與 mid_D_T 的端點。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.D = 0

	points := computePoints(t, emg, m)
	assert.Len(t, points, 17, "9 phases (no D) → 17 adjacent + 0 interval = 17 points")
	assert.Zero(t, countPoints(points, "mid_S_D"), "mid_S_D should not exist when D is absent")
	assert.Zero(t, countPoints(points, "mid_D_T"), "mid_D_T should not exist when D is absent")
}

func TestComputeSubject_PhasePoints_MissingT_OnlyMidSD(t *testing.T) {
	// 9 phases without T → 9 phase rows + 8 adjacent midpoints + 1 interval (mid_S_D) = 18 列。
	// mid_S_D 仍可生成（S 與 D 都在）;mid_D_T 無法生成（T 缺失）。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.T = models.NoOpt()

	points := computePoints(t, emg, m)
	assert.Len(t, points, 18, "9 phases (no T) → 17 adjacent + 1 interval (mid_S_D) = 18 points")
	assert.Equal(t, 1, countPoints(points, "mid_S_D"), "mid_S_D should still be generated when S and D are present")
	assert.Zero(t, countPoints(points, "mid_D_T"), "mid_D_T should not exist when T is absent")
}

// TestComputeSubject_PhasePoints_MissingC_NoDuplicateMidSD 釘住 codex review (P2) 發現的
// duplicate-row bug:當 C 缺失但 S 與 D 都在,sort 後 S 與 D 變成相鄰,adjacent-pair
// loop 自己會生出 mid_S_D — 此時 biomechanical-interval append 不能再加一次。
//
// Bug 復現前:Output 2 含兩列 mid_S_D,總列數 19;修復後:mid_S_D 只一列,總列數 18。
func TestComputeSubject_PhasePoints_MissingC_NoDuplicateMidSD(t *testing.T) {
	// 9 phases without C: P0, P1, P2, S(0.04), D(0.076), T0, T(0.10), O, L
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.C = models.NoOpt()

	points := computePoints(t, emg, m)
	// 9 phases + 8 adjacent midpoints (含 mid_S_D 由 adjacent loop 產生) + 1 interval (mid_D_T)
	// = 18 列;若 mid_S_D 重複生成則會是 19。
	assert.Len(t, points, 18, "missing C with S+D adjacent should not duplicate mid_S_D")
	assert.Equal(t, 1, countPoints(points, "mid_S_D"), "mid_S_D must appear exactly once")
	assert.Equal(t, 1, countPoints(points, "mid_D_T"), "mid_D_T must appear exactly once")
}

// TestComputeSubject_PhasePoints_MissingT0_NoDuplicateMidDT 釘住 codex review (P2) 的對稱情境:
// T0 缺失但 D 與 T 都在,adjacent-pair loop 自己會生出 mid_D_T — 此時 biomechanical-interval
// append 不能再加一次。
func TestComputeSubject_PhasePoints_MissingT0_NoDuplicateMidDT(t *testing.T) {
	// 9 phases without T0: P0, P1, P2, S, C, D(0.076), T(0.10), O, L
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.T0 = models.NoOpt()

	points := computePoints(t, emg, m)
	// 9 phases + 8 adjacent midpoints (含 mid_D_T 由 adjacent loop 產生) + 1 interval (mid_S_D)
	// = 18 列;若 mid_D_T 重複生成則會是 19。
	assert.Len(t, points, 18, "missing T0 with D+T adjacent should not duplicate mid_D_T")
	assert.Equal(t, 1, countPoints(points, "mid_D_T"), "mid_D_T must appear exactly once")
	assert.Equal(t, 1, countPoints(points, "mid_S_D"), "mid_S_D must appear exactly once")
}

// TestComputeSubject_PhasePoints_MissingC_ReversedSDOrder_NoDuplicate 釘住 codex 第二輪 (P2)
// 發現的「反向命名 dedup 失效」bug:當 C 缺失且 D 的 motion-index → EMG time 換算後
// 比 S 還早,sort 後相鄰兩點是 D, S(反向),adjacent-pair loop 產生 `mid_D_S`(非
// canonical 的 `mid_S_D`)。dedup 必須同時比對反向名稱,否則 biomechanical-interval
// 會再加 `mid_S_D`,兩列 label 不同但 time 與 ratios 完全相同 — 重複資料的偽裝。
//
// 端點對(endpoint pair){S, D} 在輸出中應只出現一次,**不論 label 方向**。
func TestComputeSubject_PhasePoints_MissingC_ReversedSDOrder_NoDuplicate(t *testing.T) {
	// D=10 motion → 0.036 EMG; S=0.04 force → 0.04 EMG → D < S → 反序。C 缺失讓
	// D 與 S 在 sort 後相鄰,adjacent-pair 產 mid_D_S。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints.P0 = models.MakeOpt(0.001)
	m.PhasePoints.P1 = models.MakeOpt(0.002)
	m.PhasePoints.P2 = models.MakeOpt(0.003)
	m.PhasePoints.C = models.NoOpt()
	m.PhasePoints.D = 10

	points := computePoints(t, emg, m)
	// 9 phases + 8 adjacent midpoints (含 mid_D_S 反序產生) + 1 interval (mid_D_T)
	// = 18 列。若反向 dedup 失效,mid_S_D 也會被加 → 19 列。
	assert.Len(t, points, 18, "endpoint pair {S,D} must appear once regardless of label direction")
	totalSDPair := countPoints(points, "mid_S_D") + countPoints(points, "mid_D_S")
	assert.Equal(t, 1, totalSDPair, "endpoint pair {S,D} must appear exactly once total (mid_S_D + mid_D_S)")
	// mid_D_T 此情境下不涉及反序,正常生成一次
	assert.Equal(t, 1, countPoints(points, "mid_D_T"), "mid_D_T must appear exactly once")
}

// TestComputeSubject_PhasePoints_MissingT0_ReversedDTOrder_NoDuplicate 是 P2 修復的對稱
// 情境:T0 缺失且 T 的 force-time 比 D 的 motion-index 換算時間還早(T.emg < D.emg),
// sort 後 T 在 D 之前,adjacent-pair 產 `mid_T_D`(反向)。biomechanical-interval
// 想加 `mid_D_T` 必須被反向 dedup 攔住。
func TestComputeSubject_PhasePoints_MissingT0_ReversedDTOrder_NoDuplicate(t *testing.T) {
	// D=20 motion → 0.076 EMG; T=0.01 force → 0.01 EMG → T < D → 反序。T0 缺失讓
	// T 與 D 在 sort 後相鄰,adjacent-pair 產 mid_T_D。
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints = models.PhasePoints{
		P0: models.MakeOpt(0.001), P1: models.MakeOpt(0.002), P2: models.MakeOpt(0.003),
		S: models.MakeOpt(0.005), C: models.MakeOpt(0.006), D: 20,
		T: models.MakeOpt(0.01), O: 30, L: models.MakeOpt(0.15),
	}

	points := computePoints(t, emg, m)
	// 9 phases + 8 adjacent midpoints (含 mid_T_D 反序產生) + 1 interval (mid_S_D)
	// = 18 列。若反向 dedup 失效,mid_D_T 也會被加 → 19 列。
	assert.Len(t, points, 18, "endpoint pair {D,T} must appear once regardless of label direction")
	totalDTPair := countPoints(points, "mid_D_T") + countPoints(points, "mid_T_D")
	assert.Equal(t, 1, totalDTPair, "endpoint pair {D,T} must appear exactly once total (mid_D_T + mid_T_D)")
	assert.Equal(t, 1, countPoints(points, "mid_S_D"), "mid_S_D must appear exactly once")
}

func TestComputeSubject_PhaseTimeOutOfRange(t *testing.T) {
	// EMG 範圍 0~0.099,offset=10000 → ForceTimeToEMGTime(0.01, 10000) = 0.01 - 9999/250 = -39.986
	// → 落在 EMG [0, 0.099] 外
	emg := buildInMemoryEMG(msTimes(0, 100), [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	m := &models.PhaseManifest{
		Subject:         "S1",
		EMGMotionOffset: 10000,
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(0.01), P1: models.MakeOpt(0.02), P2: models.MakeOpt(0.03),
			S: models.MakeOpt(0.04), C: models.MakeOpt(0.05), D: 100,
			T0: models.MakeOpt(0.07), T: models.MakeOpt(0.08), O: 200, L: models.MakeOpt(0.10),
		},
	}

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err, "分期點越界只影響 Output 2,不是 core err")
	assert.ErrorContains(t, warn, "落在 EMG 範圍")
	assert.ErrorContains(t, warn, "外")
	assert.Nil(t, computed.points, "Output 2 應被跳過")
	assert.Len(t, computed.times, 100, "Output 1 的值仍應算好")
	assert.Len(t, computed.ratios, 4, "Output 1 的值仍應算好")
}

// TestComputeSubject_PhaseTimeAtBoundary 釘住 collectPhasePoints bounds check 為 inclusive:
// 邊界(== emgStart / == emgEnd)應該被接受。Refactor 若收緊為 `<=/>=`,gait 開始正好落在
// t=0 的真實資料會被誤拒。
func TestComputeSubject_PhaseTimeAtBoundary(t *testing.T) {
	// 200 samples × 0.001s = EMG time 0.000 ~ 0.199s;P0=0.000(恰為 emgStart);L=0.199(恰為 emgEnd)
	emg, m := buildInMemoryMRFixture()
	m.PhasePoints = models.PhasePoints{
		P0: models.MakeOpt(0.000), P1: models.MakeOpt(0.05), P2: models.MakeOpt(0.06),
		S: models.MakeOpt(0.07), C: models.MakeOpt(0.08), D: 20,
		T0: models.MakeOpt(0.10), T: models.MakeOpt(0.11), O: 30, L: models.MakeOpt(0.199),
	}

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err)
	assert.NoError(t, warn, "邊界 phase 不該被拒絕")
	assert.NotEmpty(t, computed.points, "Output 2 應該產出")
}

// TestComputeSubject_PhaseTimeWithinEpsilon 釘住 ADR-0030 的行為變更:phase EMG time 落在 emgEnd
// 之外但在 1e-6 ULP 容差帶內(force-plate↔EMG 同步飄移),collectPhasePoints 經
// ResolveTimeIndex 的 inRange 旗標應「接受」,而非舊 strict-0 的「拒收+跳 Output 2」。
// 對照 TestComputeSubject_PhaseTimeAtBoundary(測 == 邊界);此處測 (emgEnd, emgEnd+1e-6) 開區間。
func TestComputeSubject_PhaseTimeWithinEpsilon(t *testing.T) {
	// 200 samples × 0.001s → 末筆 emgEnd = 0.199。
	//
	// Precondition:把 in-band 浮點假設釘進 test,一破即大聲 fail(不藏在 runtime 算式)。
	// offset=1 → ForceTimeToEMGTime(L,1)=L。L=0.1990005 落在 (emgEnd, emgEnd+1e-6) 開區間。
	const emgEnd = 0.199
	const phaseL = 0.1990005
	require.Greater(t, phaseL, emgEnd, "L 必須嚴格超出 emgEnd(否則測的是 boundary 非 epsilon)")
	require.Less(t, phaseL, emgEnd+1e-6, "L 必須在 1e-6 容差帶內(否則會被當真 typo 拒收)")

	emg, m := buildInMemoryMRFixture()
	require.Equal(t, emgEnd, emg.Time[len(emg.Time)-1], "fixture 末筆須恰為 emgEnd")
	m.PhasePoints = models.PhasePoints{
		P0: models.MakeOpt(0.000), P1: models.MakeOpt(0.05), P2: models.MakeOpt(0.06),
		S: models.MakeOpt(0.07), C: models.MakeOpt(0.08), D: 20,
		T0: models.MakeOpt(0.10), T: models.MakeOpt(0.11), O: 30, L: models.MakeOpt(phaseL),
	}

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err)
	assert.NoError(t, warn, "容差內 phase 不該被拒")
	assert.NotEmpty(t, computed.points, "Output 2 應產出(舊 strict-0 會跳過→此處空)")
}

// TestComputeSubject_NegativeTime_Handled 釘住:EMG 時間軸為負值(如校準前的時間軸偏移)時,
// compute core 不該 panic 或把它當成 invalid —— Output 1 的值照算。
func TestComputeSubject_NegativeTime_Handled(t *testing.T) {
	// EMG 時間 -0.500 ~ -0.301;力板分期點落在此負時間範圍內。D/O 是 motion-index,
	// 換算後是正秒數、落在 EMG 範圍外,所以 Output 2 會被 warn 跳過 —— 那不是本測試關心的事
	// (原 Analyze 版也只要求 Output 1 產出),warn 不斷言。
	emg := buildInMemoryEMG(msTimes(-500, 200), [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	m := &models.PhaseManifest{
		Subject:         "NegT",
		EMGMotionOffset: 1,
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(-0.49), P1: models.MakeOpt(-0.48), P2: models.MakeOpt(-0.47),
			S: models.MakeOpt(-0.46), C: models.MakeOpt(-0.45), D: 20,
			T0: models.MakeOpt(-0.43), T: models.MakeOpt(-0.42), O: 30, L: models.MakeOpt(-0.41),
		},
	}

	computed, _, err := computeSubject(emg, m)
	require.NoError(t, err, "負時間 EMG 不應讓 compute core 失敗")
	// Output 1(時間序列 dump)不依賴分期點範圍
	require.Len(t, computed.times, 200)
	assert.InDelta(t, -0.5, computed.times[0], 1e-12)
	require.Len(t, computed.ratios, 4)
	assert.InDelta(t, 2.0, computed.ratios[0][0], 1e-9, "RA/ES = 2/1")
}

// TestComputeSubject_ZeroDenominatorYieldsNaN 是 core 層的 NaN 斷言:R.ES = 0 → RA/ES 每筆都是
// NaN(Output 1 寫成空格),Output 2 的 RA/ES 整窗皆 NaN → NaN(也寫成空格);其餘比值不受影響。
// 寫成空格那一段由端到端的 TestAnalyze_NaNCellWrittenAsEmpty 釘住。
func TestComputeSubject_ZeroDenominatorYieldsNaN(t *testing.T) {
	_, m := buildInMemoryMRFixture()
	emg := buildInMemoryEMG(msTimes(0, 200), [8]float64{2, 0, 3, 1.5, 1, 2, 4, 2})

	computed, warn, err := computeSubject(emg, m)
	require.NoError(t, err)
	require.NoError(t, warn)

	require.Len(t, computed.ratios, 4)
	for i, v := range computed.ratios[0] {
		require.True(t, math.IsNaN(v), "Output 1 RA/ES[%d] 應為 NaN(R.ES=0),實際 %v", i, v)
	}
	assert.InDelta(t, 2.0, computed.ratios[1][0], 1e-9, "IL/GMax 不受 RA/ES 的 NaN 影響")

	require.NotEmpty(t, computed.points)
	for _, p := range computed.points {
		assert.True(t, math.IsNaN(p.Values[0]), "Output 2 %s 的 RA/ES 應為 NaN", p.Name)
		assert.InDelta(t, 2.0, p.Values[1], 1e-9, "Output 2 %s 的 IL/GMax 不受影響", p.Name)
	}
}
