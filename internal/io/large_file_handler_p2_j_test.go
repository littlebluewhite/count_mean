package io

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security/fsperm"
)

// TestProcessStreamingFile_CtxCancel 釘住 streaming 過程中 caller 取消 ctx,
// processStreamingFile 應在下次 reportProgressIfNeeded 邊界(每 progressInterval 筆)
// 偵測到 ctx.Err() 並 wrap return,讓 GUI shutdown / user-cancel 不必等整檔處理完。
func TestProcessStreamingFile_CtxCancel(t *testing.T) {
	// Setup:寫一個夠大的 CSV(>chunkSize 行)讓 streaming 至少跑到一次
	// progress 邊界,確保 ctx cancel 在 hot loop 中被偵測到。
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_ctx_cancel.csv")
	// 寫 ~3000 行(預設 chunkSize=1000),確保至少觸發 3 次 progress check
	var lines []string
	lines = append(lines, "Time,Ch1,Ch2")
	for i := 0; i < 3000; i++ {
		lines = append(lines, fmt.Sprintf("%.3f,%d,%d", float64(i)*0.001, 100+i%50, 50+i%30))
	}
	content := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	// 構造已取消的 ctx — streaming 第一次走到 reportProgressIfNeeded 就應該偵測到。
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消,模擬「caller 還沒給機會走任何 row 就 cancel」

	_, err := handler.ProcessLargeFileInChunksWithContext(ctx, testFile, 100, nil)
	require.Error(t, err, "cancelled ctx 應該讓 streaming 早退並 return err")
	require.True(t, errors.Is(err, context.Canceled),
		"err 應該 wrap context.Canceled,實際 err=%v", err)
}

// TestProcessStreamingFile_CtxCancelMidStream 釘住「中途取消」情境:streaming 跑到
// 一半時 caller 取消,應在下次 progress 邊界 wrap ctx err return。
func TestProcessStreamingFile_CtxCancelMidStream(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_ctx_mid_cancel.csv")
	var lines []string
	lines = append(lines, "Time,Ch1,Ch2")
	for i := 0; i < 5000; i++ {
		lines = append(lines, fmt.Sprintf("%.3f,%d,%d", float64(i)*0.001, 100+i%50, 50+i%30))
	}
	content := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	// 用 callback 觸發 cancel:當 progress 第一次回呼時 cancel ctx,
	// 模擬 GUI 用戶在 progress bar 跳第一次的瞬間按下取消鈕。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callbackCalled := false
	callback := func(_, _ int64, _ float64) {
		if !callbackCalled {
			callbackCalled = true
			cancel() // 第一次 progress callback 後 cancel
		}
	}

	_, err := handler.ProcessLargeFileInChunksWithContext(ctx, testFile, 100, callback)
	if err == nil {
		// 不一定每次都會在 mid-stream 命中 — 若整檔太短(< chunkSize)會直接跑完,
		// 此時不算 regression(行為合法);用 callbackCalled 雙重保險,只有 callback
		// 有跑時才要求 ctx.Err 被偵測。
		if callbackCalled {
			t.Errorf("callback 已觸發 cancel,後續 streaming 應 detect ctx.Err 並 return err")
		}
		return
	}
	require.True(t, errors.Is(err, context.Canceled),
		"mid-stream cancel 後 err 應 wrap context.Canceled,實際=%v", err)
}

// TestProcessStreamingFile_BackgroundCtxCompletes 釘住對偶情境:傳入
// context.Background()(永不取消)時 streaming 必須完整跑完,不可因 ctx 邏輯誤判取消。
func TestProcessStreamingFile_BackgroundCtxCompletes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_bg_ctx.csv")
	lines := []string{
		"Time,Ch1,Ch2",
		"0.1,100,50",
		"0.2,120,55",
		"0.3,110,52",
		"0.4,130,58",
		"0.5,125,56",
	}
	content := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	result, err := handler.ProcessLargeFileInChunksWithContext(
		context.Background(), testFile, 2, nil,
	)
	require.NoError(t, err, "context.Background() 不該讓 streaming 失敗")
	require.NotNil(t, result)
	require.Greater(t, result.ProcessedLines, int64(0))
}

// TestProcessLargeFileInChunks_BackwardCompatible 釘住既有 caller 介面(無 ctx)
// 行為與 前版本一致 — 新 ctx 變體不應 break 既有 API。
func TestProcessLargeFileInChunks_BackwardCompatible(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_backwards.csv")
	lines := []string{
		"Time,Ch1,Ch2",
		"0.1,100,50",
		"0.2,120,55",
		"0.3,110,52",
	}
	content := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	// 用舊 API(無 ctx)— 內部會 fallback 到 context.Background()
	result, err := handler.ProcessLargeFileInChunks(testFile, 2, nil)
	require.NoError(t, err, "後既有 API 不該破壞 — 舊 caller 仍跑 background ctx")
	require.NotNil(t, result)
}
