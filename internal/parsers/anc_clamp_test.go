package parsers

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClampANCCapacity covers the Wave 5 PR1 hardening against attacker-controlled
// `Duration(Sec.):` × `PreciseRate:` values in ANC headers. Without the clamp the
// product can be +Inf / NaN / negative / absurdly large — feeding straight into
// `make([]float64, 0, capacity)` triggers either panic or OOM.
func TestClampANCCapacity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   float64
		want int
	}{
		// Malicious inputs — must fall back to default.
		{"positive_infinity", math.Inf(1), defaultANCCapacity},
		{"negative_infinity", math.Inf(-1), defaultANCCapacity},
		{"nan", math.NaN(), defaultANCCapacity},
		{"negative", -1.0, defaultANCCapacity},
		{"over_max", float64(maxANCSampleCapacity) + 1, defaultANCCapacity},
		{"absurd_1e308", 1e308, defaultANCCapacity},

		// Edge values — must pass through.
		{"zero", 0.0, 0},
		{"one", 1.0, 1},
		{"at_max", float64(maxANCSampleCapacity), maxANCSampleCapacity},
		{"typical_60s_1khz", 60.0 * 1000.0, 60_000},
		{"long_practice_1hr_10khz", 3600.0 * 10000.0, 36_000_000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clampANCCapacity(tc.in)
			if got != tc.want {
				t.Errorf("clampANCCapacity(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestClampANCCapacity_NoPanicOnMaliciousProduct guards against the
// `int(+Inf)` undefined-behaviour path that pre-clamp code would hit:
// `make([]float64, 0, capacity)` would either panic with negative-cap or OOM.
func TestClampANCCapacity_NoPanicOnMaliciousProduct(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("clampANCCapacity panicked on malicious input: %v", r)
		}
	}()

	maliciousValues := []float64{
		math.Inf(1) * math.Inf(1), // Inf
		math.Inf(1) - math.Inf(1), // NaN
		-math.Inf(1),              // -Inf
		1e154 * 1e154,             // overflow to +Inf
		1e154 * -1e154,            // overflow to -Inf
	}

	for _, v := range maliciousValues {
		_ = clampANCCapacity(v) // must not panic
	}
}

// TestCheckANCTotalAllocation 釘住 clampANCCapacity 只擋每通道上限，
// 沒擋「總記憶體」。64 ch × 100M sample/ch × 8 byte = 51 GB 直接 OOM。
// 加 checkANCTotalAllocation 對「per-channel capacity × numChannels × sizeof(float64)」
// 做總量檢查；超過 maxANCTotalAllocBytes 視為惡意 header，回 error 拒絕。
//
// 為什麼 reject 而不 silent truncate：truncate 會讓 caller 拿到不完整 dataset
// 仍 NoError，下游做 sliding window / phase sync 會把零值當有效資料 → silent
// miscalculation。Reject + clear error 至少讓 caller 知道 header 不合理。
func TestCheckANCTotalAllocation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		perChannel  int
		numChannels int
		wantErr     bool
	}{
		// Real-world safe scenarios — well under 2 GiB ceiling.
		// 2 GiB / 8 bytes = 256M float64 slots total across all channels.
		{"single_channel_25M", 25_000_000, 1, false},    // 200 MiB
		{"typical_8ch_1khz_1hr", 3600 * 1000, 8, false}, // ~230 MiB
		{"force_plate_6ch_5M", 5_000_000, 6, false},     // 240 MiB
		{"zero_capacity_any_channel", 0, 64, false},     // 0 size never OOMs
		{"default_capacity_64ch", defaultANCCapacity, 64, false},

		// Adversarial — total exceeds ceiling.
		{"force_plate_6ch_at_per_channel_max", maxANCSampleCapacity, 6, true}, // 4.8 GB
		{"64ch_x_10M_samples", 10_000_000, 64, true},                          // ~5 GB
		{"64ch_x_max_capacity", maxANCSampleCapacity, 64, true},               // 51 GB
		{"32ch_x_max_capacity", maxANCSampleCapacity, 32, true},               // 25.6 GB
		{"single_channel_overflow", math.MaxInt64 / 8, 1, true},               // huge
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkANCTotalAllocation(tc.perChannel, tc.numChannels)
			if tc.wantErr && err == nil {
				t.Errorf("checkANCTotalAllocation(%d, %d) expected error, got nil",
					tc.perChannel, tc.numChannels)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("checkANCTotalAllocation(%d, %d) unexpected error: %v",
					tc.perChannel, tc.numChannels, err)
			}
			if tc.wantErr && err != nil {
				if !errors.Is(err, ErrANCTotalCapacityExceeded) {
					t.Errorf("checkANCTotalAllocation(%d, %d) error %v not Is ErrANCTotalCapacityExceeded",
						tc.perChannel, tc.numChannels, err)
				}
			}
		})
	}
}

// TestANCParser_ParseFile_RejectsMaliciousTotalCapacity 端對端釘住
// 即便每通道 Duration × PreciseRate 在 maxANCSampleCapacity 範圍內，只要
// 通道數膨脹到讓總配置 > 2 GiB，parser 必須回 error 而非靜默吞下。
// 64 channels × ~5M samples/ch（5000s × 1kHz）= 320M float64 ≈ 2.56 GiB > ceiling.
func TestANCParser_ParseFile_RejectsMaliciousTotalCapacity(t *testing.T) {
	t.Parallel()

	// Build header line 3 with Duration that makes per-channel × N channels exceed 2 GiB.
	// perChannel cap = 5000 × 1000 = 5_000_000 (≤ maxANCSampleCapacity = 100_000_000).
	// 64 channels × 5_000_000 × 8 = 2_560_000_000 bytes > 2 GiB.
	numChannels := 64
	channelTokens := make([]string, 0, numChannels)
	rateTokens := make([]string, 0, numChannels)
	rangeTokens := make([]string, 0, numChannels)
	for i := 0; i < numChannels; i++ {
		channelTokens = append(channelTokens, fmt.Sprintf("Ch%d", i+1))
		rateTokens = append(rateTokens, "1000")
		rangeTokens = append(rangeTokens, "2000")
	}

	maliciousANC := fmt.Sprintf(`1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	BIG	Trial#:	1	Duration(Sec.):	5000.000	#Channels:	%d
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	%s
10	Rate	%s
11	Range	%s
12	Units	N
0.000	0.1
`,
		numChannels,
		strings.Join(channelTokens, "\t"),
		strings.Join(rateTokens, "\t"),
		strings.Join(rangeTokens, "\t"),
	)

	tmpFile, err := os.CreateTemp(t.TempDir(), "malicious_anc_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(maliciousANC)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	parser := NewANCParser()
	_, err = parser.Parse(f, tmpFile.Name())
	require.Error(t, err, "expected rejection of malicious total-capacity header")
	assert.True(t, errors.Is(err, ErrANCTotalCapacityExceeded),
		"err must wrap ErrANCTotalCapacityExceeded, got: %v", err)
}

// TestCheckANCTotalAllocation_BoundaryEqualsCeilingAccepted (L11) 釘住:totalBytes
// **正好等於** maxANCTotalAllocBytes 是合法情境(等於上限不該 reject,只有「超出」
// 才該 fail-fast)。原本 code 用 strict `>` 但 error message 寫 `≥` 互相矛盾,
// 註解修正後此邊界 test 鎖住「正好等於」必須回 nil。
//
// 構造:perChannel × numChannels × 8 == 2 GiB → 取 perChannel = 256M / numChannels。
// 用 8 個 channel 簡化:perChannel = 256M / 8 = 32M < maxANCSampleCapacity ✓。
func TestCheckANCTotalAllocation_BoundaryEqualsCeilingAccepted(t *testing.T) {
	t.Parallel()

	// 2 GiB / 8 byte = 256M float64 總 slot 數。8 ch × 32M ch == 256M 總 slot,
	// totalBytes = 2_147_483_648 = maxANCTotalAllocBytes。strict `>` 必須回 nil。
	const totalSlots = 256 * 1024 * 1024 // 2 GiB / 8
	perChannel := totalSlots / 8
	numChannels := 8

	err := checkANCTotalAllocation(perChannel, numChannels)
	if err != nil {
		t.Errorf("totalBytes 正好等於上限應 accept,卻 reject: %v", err)
	}
}

// TestCheckANCTotalAllocation_NegativeInputs 邊界：負值參數不應該 panic 也不
// 應該繞過上限檢查（負 × 負 = 正會誤判合法）。實際上 perChannel 來自
// clampANCCapacity（已 clamp ≥ 0），numChannels 來自 parseHeader（理論上 ≥ 0）。
// 但 caller 路徑可能繞過，加防禦：負值一律 reject。
func TestCheckANCTotalAllocation_NegativeInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		perChannel  int
		numChannels int
	}{
		{"negative_per_channel", -1, 8},
		{"negative_num_channels", 1000, -1},
		{"both_negative", -100, -100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkANCTotalAllocation(tc.perChannel, tc.numChannels)
			if err == nil {
				t.Errorf("checkANCTotalAllocation(%d, %d) expected error for negative input",
					tc.perChannel, tc.numChannels)
			}
		})
	}
}
