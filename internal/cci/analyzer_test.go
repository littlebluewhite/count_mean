package cci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCCIAnalyzer_AnalyzeCCI_AcceptsLiteralPercentInEMGFilename 釘住 codex review
// post-impl P1：BTS 匯出的 EMG 檔名常含字面 "%"（例 "SF_8_BTS%_*.csv"），原本 CCI loadEMGData
// 用 PathValidator.GetSafePath 會誤拒，整個 CCI 對標準資料不可用。改走 security.ResolveLenientPath
// 後，這條 path 必須能順利通過驗證階段。
//
// 本 test 是 contract 守門：不關心 AnalyzeCCI 是否最後成功（可能因其他原因失敗），只要
// 「EMG 檔案路徑驗證失敗」訊息不出現即視為 fix 持續有效。
func TestCCIAnalyzer_AnalyzeCCI_AcceptsLiteralPercentInEMGFilename(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	emgFile := "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"

	// 最小 8-channel R.* EMG (200 samples × 0.001s = 0~0.199s)
	var emgBuf strings.Builder
	emgBuf.WriteString(`X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4,` +
		`R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&emgBuf, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", float64(i)*0.001)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, emgFile), []byte(emgBuf.String()), 0o600))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	manifestContent := "Subject,motion file,Force Plate file,EMG file," +
		"EMG第一筆時間對應Motion的時間index值,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("SF8,m.csv,f.anc,%s,1,0.01,0.02,0.03,0.04,0.05,20,0.07,0.08,30,0.09\n", emgFile)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	a := NewCCIAnalyzer()
	_, err := a.AnalyzeCCI(context.Background(), &CCIParams{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		SubjectIndex: 0,
	})

	// 強化主張：path-validation 不該因 '%' 失敗（fix 退化的徵兆），同時必須真的穿越到
	// 下游階段——若 err==nil 表示完整跑完，亦合格；若 err!=nil 但訊息不是 path-validation
	// 字串，代表 path 解析確實放行了 '%' 檔名（後續可能因 manifest 時間範圍等其他原因失敗）。
	if err != nil {
		assert.NotContains(t, err.Error(), "EMG 檔案路徑驗證失敗",
			"path validation 不該因 '%%' 失敗，err=%v", err)
		assert.NotContains(t, err.Error(), "URL-encoded 殘留",
			"不應觸發 PathValidator 的 URL-decode 拒絕邏輯，err=%v", err)
		assert.NotContains(t, err.Error(), "EMG 檔案不存在",
			"含 '%%' 的檔案應該被正確開啟，err=%v", err)
	}
}

// TestCCIAnalyzer_ConcurrentCallsNoRace 釘住與 muscle_ratio 對稱的並發守門。
// 後 EMGParser 改 stateless，但 Wails 並行 RPC 仍可能在未來 refactor 引入新 shared
// mutable state — 此 test 用 race detector 守門。
//
// 本 test 必須在 `go test -race` 下跑才會觸發 race detector。
//
// 穩定性：外層 for k 迴圈把整個 concurrent batch 跑 10 次，提升 race detector 在 CI 上的觸發機率
// （單次跑可能因為 goroutine 排程剛好不重疊而 false-pass）。
func TestCCIAnalyzer_ConcurrentCallsNoRace(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	emgFile := "concurrent.csv"

	var emgBuf strings.Builder
	emgBuf.WriteString(`X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4,` +
		`R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&emgBuf, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", float64(i)*0.001)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, emgFile), []byte(emgBuf.String()), 0o600))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	manifestContent := "Subject,motion file,Force Plate file,EMG file," +
		"EMG第一筆時間對應Motion的時間index值,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("S,m.csv,f.anc,%s,1,0.01,0.02,0.03,0.04,0.05,20,0.07,0.08,30,0.09\n", emgFile)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	a := NewCCIAnalyzer()

	for k := 0; k < 10; k++ {
		// 並行 8 個 AnalyzeCCI 呼叫共用同個 Analyzer instance — 模擬 Wails 並行 RPC
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := a.AnalyzeCCI(context.Background(), &CCIParams{
					ManifestFile: manifestPath,
					DataFolder:   dataDir,
					SubjectIndex: 0,
				})
				// 不 assert err — race detector 才是這個 test 的守門
				_ = err
			}()
		}
		wg.Wait()
	}
}

// TestCCIAnalyzer_RejectsInvalidPhaseManifest 釘住 loadAndValidate 新增的分期不變量驗證
// （對齊 phase_sync.validateManifestData）。manifest 帶 P0(0.5) > P1(0.02) 違反 force-time
// 單調序;但 P0/P1/P2 是 pre-gait 事件,calculateGaitCycle 會 continue 跳過、dropOutOfRangePhases
// 也會移除,故下游 gait 計算「看不到」這個違規 → 沒有驗證門時整段會被放行跑完。
// 加門後必須 fail-fast 回 error,且錯誤訊息來自 ValidatePhaseManifest（force-time 順序）。
// 這個 discriminator 確保鎖的是「驗證門」而非任何下游 gait/anchor 守門。
func TestCCIAnalyzer_RejectsInvalidPhaseManifest(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	emgFile := "invalid_phase.csv"

	// 最小 8-channel R.* EMG (200 samples × 0.001s = 0~0.199s)，S/L anchors 落在範圍內，
	// 確保若沒有驗證門,下游 gait 計算會成功 → 凸顯「是驗證門擋下,不是下游」。
	var emgBuf strings.Builder
	emgBuf.WriteString(`X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4,` +
		`R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&emgBuf, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", float64(i)*0.001)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, emgFile), []byte(emgBuf.String()), 0o600))

	// P0=0.5 晚於 P1=0.02 → 違反 force-time 單調序。S=0.04 / L=0.09 anchors 仍合法。
	manifestPath := filepath.Join(tempDir, "manifest.csv")
	manifestContent := "Subject,motion file,Force Plate file,EMG file," +
		"EMG第一筆時間對應Motion的時間index值,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("SF8,m.csv,f.anc,%s,1,0.5,0.02,0.03,0.04,0.05,20,0.07,0.08,30,0.09\n", emgFile)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	a := NewCCIAnalyzer()
	_, err := a.AnalyzeCCI(context.Background(), &CCIParams{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		SubjectIndex: 0,
	})

	require.Error(t, err, "畸形 manifest（P0 晚於 P1）應在 loadAndValidate 被 fail-fast 擋下")
	// 字面鎖住錯誤來自 ValidatePhaseManifest 的 force-time 順序檢查,而非下游 gait 守門。
	assert.Contains(t, err.Error(), "不能早於",
		"錯誤應來自 ValidatePhaseManifest 的 force-time 順序檢查,err=%v", err)
}
