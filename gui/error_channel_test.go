package gui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wave 3 Batch R — 統一錯誤通道契約測試
//
// 契約：AnalyzeCCI / AnalyzeMuscleRatio / AnalyzeNormalizedPhaseSync 三個 handler
// 對「可預期失敗」（參數驗證、路徑驗證、下游 analyzer 錯誤）一律走：
//
//	return failedXxxResult(msg), nil
//
// 不走 `return nil, err`（後者保留給 recoverHandlerPanic 灌入 panic 用）。
// 這組測試守門所有 boundary 失敗都 routed through result.Success=false + result.Message，
// 避免回到 dual-channel state。
//
// path_validation_test.go 已涵蓋 path traversal case；本檔補上「empty params」
// (validateXxxParams) 那條 fail path——cci_handlers.go:47 / muscle_ratio_handlers.go:62
// / normalized_phase_sync_handlers.go:75 三個入口點皆涵蓋。
//
// 重構:三 handler 的 boilerplate 統一抽到 assertUnifiedFailureChannel
// (Go 1.25 泛型);新增 ResultAsserter func 讓 caller-specific 斷言(如 MuscleRatio
// 的 Subjects==nil) 注入,共用 90% 流程。

// unifiedChannelCase 是 assertUnifiedFailureChannel 的 sub-test 描述。
// P / R 對應 handler 的 (params, *result) 型別。expectInMsg 是 result.Message
// 必含的子字串(對應 ErrNo* 哨兵)。extraAssert 是 caller-specific 斷言
// (例:MuscleRatio 對 Subjects==nil 的額外檢查),nil 表示不需要。
type unifiedChannelCase[P, R any] struct {
	name        string
	params      P
	expectInMsg string
	extraAssert func(t *testing.T, result *R)
}

// resultSuccessAccessor 把 R 的 Success / Message 抽成 caller 提供的 accessor —
// 避免在 helper 內用 reflect 或要求所有 result struct 嵌入 base interface,
// 保持各 handler return type 原貌(對 Wails binding 友善)。
type resultSuccessAccessor[R any] struct {
	Success func(*R) bool
	Message func(*R) string
}

// assertUnifiedFailureChannel 是 重構出的泛型 helper:跑同一組「empty
// params → 統一錯誤通道」斷言流程,適用於所有走「失敗回 non-nil result +
// nil err」契約的 handler。
//
// 為何用泛型而非 interface:三個 result 型別(CCIResult / MuscleRatioResult /
// NormalizedPhaseSyncResult)都是 Wails JSON DTO,加 method 會污染前端 binding。
// 走 accessor function 注入,結構保持純資料,helper 仍能型別安全。
//
// 流程不變:
//  1. 呼叫 invoke(handler entry) → (result, err)
//  2. 斷言 err == nil(統一通道契約)
//  3. 斷言 result != nil(failed* helper 必回 non-nil)
//  4. 斷言 access.Success(result) == false
//  5. 斷言 access.Message(result) 含 expectInMsg
//  6. 若 caller 提供 extraAssert,執行(如 Subjects==nil)
//
// 此 helper 把 ~30 行 boilerplate 降到 ~5 行 caller 端,並把契約「失敗回
// non-nil result + nil err」單點維護;若未來契約改變,只此一處需動。
func assertUnifiedFailureChannel[P, R any](
	t *testing.T,
	invoke func(P) (*R, error),
	access resultSuccessAccessor[R],
	cases []unifiedChannelCase[P, R],
) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := invoke(tc.params)
			require.NoError(t, err,
				"統一通道契約：參數驗證失敗不應走 Go err（保留給 panic）")
			require.NotNil(t, result,
				"統一通道契約：參數驗證失敗應回 non-nil failedResult")
			assert.False(t, access.Success(result),
				"參數驗證失敗 result.Success 必為 false")
			assert.Contains(t, access.Message(result), tc.expectInMsg,
				"result.Message 應含驗證失敗哨兵描述")

			if tc.extraAssert != nil {
				tc.extraAssert(t, result)
			}
		})
	}
}

// TestAnalyzeCCI_EmptyParamsUnifiedChannel 驗證：CCI handler 收到空參數應走
// 統一錯誤通道，回 non-nil failedResult + nil err，而非舊版 (nil, ErrNoManifestFile)。
func TestAnalyzeCCI_EmptyParamsUnifiedChannel(t *testing.T) {
	app := setupHandlerTestApp(t)

	assertUnifiedFailureChannel(t,
		app.AnalyzeCCI,
		resultSuccessAccessor[CCIResult]{
			Success: func(r *CCIResult) bool { return r.Success },
			Message: func(r *CCIResult) string { return r.Message },
		},
		[]unifiedChannelCase[CCIParams, CCIResult]{
			{
				name:        "no_manifest",
				params:      CCIParams{ManifestFile: "", DataFolder: "/tmp/legit", SubjectIndex: 0},
				expectInMsg: ErrNoManifestFile.Error(),
			},
			{
				name:        "no_data_folder",
				params:      CCIParams{ManifestFile: "/tmp/legit/m.csv", DataFolder: "", SubjectIndex: 0},
				expectInMsg: ErrNoDataFolder.Error(),
			},
		},
	)
}

// TestAnalyzeMuscleRatio_EmptyParamsUnifiedChannel 同 CCI — 守 MuscleRatio
// 入口 validateMuscleRatioParams fail path 走統一通道。
//
// extraAssert 補上 Subjects==nil 斷言(pre-analysis fail 與 partial-fail 區分)。
func TestAnalyzeMuscleRatio_EmptyParamsUnifiedChannel(t *testing.T) {
	app := setupHandlerTestApp(t)

	subjectsNilCheck := func(t *testing.T, r *MuscleRatioResult) {
		t.Helper()
		assert.Nil(t, r.Subjects,
			"pre-analysis fail Subjects 必為 nil（與 partial-fail 區分）")
	}

	assertUnifiedFailureChannel(t,
		app.AnalyzeMuscleRatio,
		resultSuccessAccessor[MuscleRatioResult]{
			Success: func(r *MuscleRatioResult) bool { return r.Success },
			Message: func(r *MuscleRatioResult) string { return r.Message },
		},
		[]unifiedChannelCase[MuscleRatioParams, MuscleRatioResult]{
			{
				name:        "no_manifest",
				params:      MuscleRatioParams{ManifestFile: "", DataFolder: "/tmp/legit"},
				expectInMsg: ErrNoManifestFile.Error(),
				extraAssert: subjectsNilCheck,
			},
			{
				name:        "no_data_folder",
				params:      MuscleRatioParams{ManifestFile: "/tmp/legit/m.csv", DataFolder: ""},
				expectInMsg: ErrNoDataFolder.Error(),
				extraAssert: subjectsNilCheck,
			},
		},
	)
}

// TestAnalyzeNormalizedPhaseSync_EmptyParamsUnifiedChannel 同 CCI — 守
// NormalizedPhaseSync 入口三段驗證 (manifest / folder / 兩組 phase) 皆走統一通道。
func TestAnalyzeNormalizedPhaseSync_EmptyParamsUnifiedChannel(t *testing.T) {
	app := setupHandlerTestApp(t)

	assertUnifiedFailureChannel(t,
		app.AnalyzeNormalizedPhaseSync,
		resultSuccessAccessor[NormalizedPhaseSyncResult]{
			Success: func(r *NormalizedPhaseSyncResult) bool { return r.Success },
			Message: func(r *NormalizedPhaseSyncResult) string { return r.Message },
		},
		[]unifiedChannelCase[NormalizedPhaseSyncParams, NormalizedPhaseSyncResult]{
			{
				name: "no_manifest",
				params: NormalizedPhaseSyncParams{
					ManifestFile: "", DataFolder: "/tmp/legit",
					NormStartPhase: "P0", NormEndPhase: "P2",
					StatsStartPhase: "P0", StatsEndPhase: "P2",
				},
				expectInMsg: ErrNoManifestFile.Error(),
			},
			{
				name: "no_data_folder",
				params: NormalizedPhaseSyncParams{
					ManifestFile: "/tmp/legit/m.csv", DataFolder: "",
					NormStartPhase: "P0", NormEndPhase: "P2",
					StatsStartPhase: "P0", StatsEndPhase: "P2",
				},
				expectInMsg: ErrNoDataFolder.Error(),
			},
			{
				name: "no_norm_phase",
				params: NormalizedPhaseSyncParams{
					ManifestFile: "/tmp/legit/m.csv", DataFolder: "/tmp/legit",
					NormStartPhase: "", NormEndPhase: "P2",
					StatsStartPhase: "P0", StatsEndPhase: "P2",
				},
				expectInMsg: ErrNoPhaseSelection.Error(),
			},
			{
				name: "no_stats_phase",
				params: NormalizedPhaseSyncParams{
					ManifestFile: "/tmp/legit/m.csv", DataFolder: "/tmp/legit",
					NormStartPhase: "P0", NormEndPhase: "P2",
					StatsStartPhase: "P0", StatsEndPhase: "",
				},
				expectInMsg: ErrNoPhaseSelection.Error(),
			},
		},
	)
}
