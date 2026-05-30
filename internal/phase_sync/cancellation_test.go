package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"count_mean/internal/models"
)

// TestAnalyzePhaseSync_PreCancelledContextReturnsErr 鎖定 Wave 7 review
// api-designer P2 的修補：phaseSyncAnalyzer.AnalyzePhaseSync 收 ctx 後，預先
// cancel 的 ctx 必須在 file load 之前就 bail，避免長運算被使用者 cancel 後
// 還要等 IO 完成。
func TestAnalyzePhaseSync_PreCancelledContextReturnsErr(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()
	require.NotNil(t, analyzer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 故意給一個不存在的 manifest path — 如果 ctx 檢查沒生效，會先 fail 在 file
	// load 上（不同錯誤訊息）。ctx.Err() 檢查若正常會比 file load 更早 return
	// context.Canceled。
	params := &models.AnalysisParams{
		ManifestFile: "/nonexistent/manifest.csv",
		DataFolder:   "/nonexistent",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP1,
		SubjectIndex: 0,
	}

	_, err := analyzer.AnalyzePhaseSync(ctx, params)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "pre-cancelled ctx 應在 file load 前 bail")
}

// TestAnalyzePhaseSync_InFlightCancelReturnsQuickly 鎖定 修補的
// in-flight 取消契約：一次完整 AnalyzePhaseSync 在 caller cancel 時必須在
// 1 秒內回傳,且必須回 context.Canceled。
//
// 確定性化:用 fake parser 阻塞於 channel,確保「cancel 在 ParseFile
// 中途打進來」這個 path 必被走到 — 不再依賴 fixture 大小 + 硬體速度的競賽。
//
// 設計:
//   - SetParseEMGFileFnForTest 注入 fake parser,fake 走 `<-unblockCh` 阻塞。
//   - test goroutine 啟動 AnalyzePhaseSync,進入 Load → fake parser 卡住。
//   - 100ms 後 test 同時 cancel(ctx) 與 close(unblockCh),fake parser 立即 unblock
//     return 一個 minimal EMG dataset。
//   - Load return 後,AnalyzePhaseSync 下一個 ctx.Err() check (line 414) 命中 →
//     return context.Canceled。
//   - assertion 強制 require.ErrorIs(t, err, context.Canceled),不再允許「err==nil
//     視為退化通過」的軟性 fallback。
//
// 為什麼 fake parser 是必要:phase_sync 的 ctx 檢查只在 step 邊界,ParseFile 本身
// 不接 ctx。原本 test 靠「fixture 夠大讓 Load > 100ms」確保 cancel 打在 Load 中,
// 但「夠大」沒有確定性 — 快速硬體上 Load < 100ms 會讓 cancel 打不到任何 step 邊界,
// 退化為「驗證 no-leak」。fake parser 在 channel 上阻塞則確定性地把 Load 拉長,
// 直到 test 主動 close channel。
func TestAnalyzePhaseSync_InFlightCancelReturnsQuickly(t *testing.T) {
	// 用 goleak.VerifyNone 取代 busy-wait + runtime.NumGoroutine。後者
	// 對「全域 goroutine 數」做比較,容易被 testing framework / parallel test
	// 留下的 transient goroutine 干擾,而且只看數量看不出來「leak 是哪個 stack」。
	// goleak 直接列出當下還活著的 stack,並排除 framework own goroutines,
	// 偵測精度遠高於 ±1 偏差檢查。
	t.Cleanup(func() {
		// 不用 VerifyTestMain (m *testing.M scope) — 受其他 test 影響;
		// 改在這個 test 的 cleanup 點驗 leak,scope 收得最緊。
		goleak.VerifyNone(t)
	})

	manifestFile, dataFolder := buildLargeFixture(t)

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   dataFolder,
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP1,
		SubjectIndex: 0,
	}

	// fake parser — 在 unblockCh 上阻塞,cancel 後由 test 主動 close。
	//
	// Fake EMG dataset 需覆蓋 manifest 中 P0=1.0、P1=2.0 範圍,否則 Load 後的
	// ResolvePhaseRange → validateEMGTimeRange 會先因「EMG 時間範圍不足」失敗
	// 而不是回 context.Canceled。EMG 範圍 0..2.1s 涵蓋 P0..P1 + 一點 margin。
	//
	// 改 SetParseEMGFileFnForTest 注入 hook (atomic.Pointer),避免裸 var
	// 在 t.Parallel() 場景的 data race。
	unblockCh := make(chan struct{})
	cleanup := SetParseEMGFileFnForTest(func(_ io.Reader, _ string) (*models.PhaseSyncEMGData, float64, error) {
		<-unblockCh
		const nSamples = 2100 // 0..2.099s @ 1ms step,涵蓋 P0=1.0 / P1=2.0
		times := make([]float64, nSamples)
		chData := make([]float64, nSamples)
		for i := 0; i < nSamples; i++ {
			times[i] = float64(i) * 0.001
			chData[i] = 0.1
		}
		return &models.PhaseSyncEMGData{
			Time:     times,
			Channels: map[string][]float64{"Ch1": chData},
			Headers:  []string{"Ch1"},
		}, 1000.0, nil
	})
	t.Cleanup(cleanup)

	analyzer := NewPhaseSyncAnalyzer()
	ctx, cancel := context.WithCancel(context.Background())

	type analyzeResult struct {
		err     error
		elapsed time.Duration
	}

	done := make(chan analyzeResult, 1)

	go func() {
		start := time.Now()
		_, err := analyzer.AnalyzePhaseSync(ctx, params)
		done <- analyzeResult{err: err, elapsed: time.Since(start)}
	}()

	// 等 fake parser 進入阻塞 (給 goroutine 一些時間穿過 validation pipeline),
	// 然後同步 cancel + unblock:cancel 先觸發,unblock 讓 ParseFile return,
	// AnalyzePhaseSync 在下一個 ctx.Err() check 命中 context.Canceled。
	time.AfterFunc(100*time.Millisecond, func() {
		cancel()
		close(unblockCh)
	})

	select {
	case result := <-done:
		assert.Less(t, result.elapsed, 1*time.Second,
			"in-flight cancel 應在 1 秒內回傳（實測 %v）", result.elapsed)
		// assertion 從 "non-nil 才檢查 ErrorIs" 改為強制 require.ErrorIs。
		// fake parser 保證 cancel 必走到 step 邊界,err 不應為 nil。
		require.ErrorIs(t, result.err, context.Canceled,
			"err 必須是 context.Canceled: got %v", result.err)
	case <-time.After(2 * time.Second):
		// safety net:若 ParseFile 沒被 cancel/unblock 機制喚醒,主動 unblock 避免 leak
		close(unblockCh)
		cancel()
		<-done
		t.Fatal("AnalyzePhaseSync 在 cancel 後 2 秒仍未回傳 — context plumbing 失效")
	}

	// 不需要 baseline 比對 — t.Cleanup 中的 goleak.VerifyNone 會列出所有殘留 stack。
}

// buildLargeFixture 寫出 manifest + EMG csv 到 tmp dir，回傳 (manifestPath, dataFolder)。
// EMG 規模刻意設為 ~12 萬 row × 4 channel，讓 Load 在常見 CI 硬體上跨越 100ms 門檻。
func buildLargeFixture(t *testing.T) (string, string) {
	t.Helper()

	dataFolder := t.TempDir()

	emgPath := filepath.Join(dataFolder, "emg_large.csv")
	writeLargeEMGFile(t, emgPath, 120_000)

	// Motion CSV — 真正的 4-row metadata + header + data 結構（見
	// MotionCategoryRow/SubcatRow/HeaderRow/DataRow constants）。
	motionPath := filepath.Join(dataFolder, "motion_min.csv")
	require.NoError(t, os.WriteFile(motionPath, []byte(
		"Meta1\nMeta2\nMeta3\nIndex,X,Y,Z\n1,0,0,0\n200,0,0,0\n"), 0o600))

	// Force plate — 12 行 ANC text header（line 9 = ChannelNames，必要），
	// 之後接 2 筆 data row。Tab-delimited、line-number prefix 是 ANC parser 認的
	// 標準格式（見 internal/parsers/anc_parser.go parseHeader）。
	forcePath := filepath.Join(dataFolder, "force_min.anc")
	require.NoError(t, os.WriteFile(forcePath, []byte(
		"1\tFile_Type:\tAMTI_FORCE_PLATE\tGeneration#:\t4\n"+
			"2\tBoard_Type:\tOR6-5-1000\n"+
			"3\tTrial_Name:\tCANCEL_TEST\tTrial#:\t1\tDuration(Sec.):\t3.000\t#Channels:\t3\n"+
			"4\tBitDepth:\t16\tPreciseRate:\t1000.000\n"+
			"5\t\n6\t\n7\t\n8\t\n"+
			"9\tName\tFx\tFy\tFz\n"+
			"10\tRate\t1000\t1000\t1000\n"+
			"11\tRange\t2000\t2000\t5000\n"+
			"12\tUnits\tN\tN\tN\n"+
			"0.000000\t0.0\t0.0\t0.0\n"+
			"3.000000\t0.0\t0.0\t0.0\n"), 0o600))

	manifestPath := filepath.Join(dataFolder, "manifest.csv")

	// Batch T：S/C/T0/T/L 用空字串/0 motion-index 表達「未提供」(OptFloat NoOpt /
	// int 0-sentinel)。原本全寫 "0" 在 OptFloat 新契約下會被當成「t=0 有效時間」，
	// 觸發 monotonicity check 把 S 視為早於 P2=3.0 而 fail。
	manifestContent := fmt.Sprintf(
		`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
LoadTest,%s,%s,%s,0,1.0,2.0,3.0,,,0,,,0,
`,
		filepath.Base(motionPath),
		filepath.Base(forcePath),
		filepath.Base(emgPath),
	)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	return manifestPath, dataFolder
}

// writeLargeEMGFile 寫一個 rowCount 行的 EMG CSV：4 個 channel，時間從 0 開始
// 以 0.001s 步長遞增。fixed-format 字串拼接比 fmt.Fprintf 快數倍，避免 fixture
// 建立時間蓋過 test wall clock。
func writeLargeEMGFile(t *testing.T, path string, rowCount int) {
	t.Helper()

	f, err := os.Create(path) //nolint:gosec // test fixture path, controlled by t.TempDir
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	var b strings.Builder

	b.Grow(rowCount * 40)
	b.WriteString("Time,Ch1,Ch2,Ch3,Ch4\n")

	for i := 0; i < rowCount; i++ {
		// 時間 0..119.999s；channel 數值就用 row index 做 deterministic stub
		_, _ = fmt.Fprintf(&b, "%.3f,%d,%d,%d,%d\n", float64(i)*0.001, i, i+1, i+2, i+3)
	}

	_, err = f.WriteString(b.String())
	require.NoError(t, err)
}

// assertNoGoroutineLeak 已移除,改用 go.uber.org/goleak.VerifyNone。
// 舊版用 runtime.NumGoroutine() ±1 偏差不可靠 — testing framework / parallel
// test 的 transient goroutine 會推高 baseline 製造 false positive,且看不出
// leak 是哪個 stack。goleak 直接 dump 殘留 stack,精度與可讀性都更好。
