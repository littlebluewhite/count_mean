// chart hardening 回歸測試:取代先前只 t.Logf 不 assert 的 zzz_probe_test.go
// 「probe」—— 那三個 probe 把 nil-panic / NaN%-label / PairName XSS 三個真實缺陷
// 偽裝成綠燈。本檔三個測試各自 pin 一個已修復的行為,真正會 fail。
package cci

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// TestGenerateCCIInteractiveChart_NilResultReturnsError 釘住:nil result 必須回
// ErrNilResult。先前 validateCCIResultForChart 宣告 nil 為合法(return nil)→
// 下游 setCCIGlobalOptions 解參 result.Subject → nil-pointer panic。
func TestGenerateCCIInteractiveChart_NilResultReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateCCIInteractiveChart(context.Background(), nil, &buf)
	if err == nil {
		t.Fatal("nil result 必須回 error(先前會 nil-pointer panic),實際 err=nil")
	}
	if !errors.Is(err, ErrNilResult) {
		t.Errorf("nil result 應回 ErrNilResult,got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("nil result 不應寫出任何 HTML,實際寫了 %d bytes", buf.Len())
	}
}

// TestGenerateCCIInteractiveChart_NonFiniteGaitTimeRejected 釘住:GaitStartTime /
// GaitEndTime 為 NaN / ±Inf 時必須被拒。先前 `d := end - start; d <= 0` 對 NaN 與
// +Inf 恆為 false → 通過驗證 → 產出含 "NaN%" 軸標的圖。
func TestGenerateCCIInteractiveChart_NonFiniteGaitTimeRejected(t *testing.T) {
	for _, gait := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		result := &CCIAnalysisResult{
			Subject:       "s",
			TimeValues:    []float64{0, 1, 2},
			PairResults:   []CCIResult{{PairName: "RA/ES", Values: []float64{0.1, 0.2, 0.3}}},
			GaitStartTime: 0,
			GaitEndTime:   gait,
		}
		var buf bytes.Buffer
		err := GenerateCCIInteractiveChart(context.Background(), result, &buf)
		if !errors.Is(err, ErrInvalidGaitCycle) {
			t.Errorf("GaitEndTime=%v(非有限)應回 ErrInvalidGaitCycle,got %v(產出 %d bytes)",
				gait, err, buf.Len())
		}
		if strings.Contains(buf.String(), "NaN%") {
			t.Errorf("GaitEndTime=%v 產出含 'NaN%%' 軸標的 HTML", gait)
		}
	}
}

// TestGenerateCCIInteractiveChart_PairNameSanitized 釘住:user-controlled PairName
// 嵌進 echarts series 名前必須過 SanitizeChartString(H3 安全契約明文涵蓋 channel /
// label)。先前 line.AddSeries(pr.PairName, ...) 未 sanitize → 惡意 `</script>` 逸出
// 進 HTML <script> 區塊 = Wails WebView RPC 任意執行。
func TestGenerateCCIInteractiveChart_PairNameSanitized(t *testing.T) {
	t.Run("malicious_pairname_no_script_escape", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:       "safe",
			TimeValues:    []float64{0, 1, 2},
			PairResults:   []CCIResult{{PairName: `</script><script>alert(1)</script>`, Values: []float64{0.1, 0.2, 0.3}}},
			GaitStartTime: 0,
			GaitEndTime:   2.0,
		}
		var buf bytes.Buffer
		if err := GenerateCCIInteractiveChart(context.Background(), result, &buf); err != nil {
			t.Fatalf("合法 result(僅 PairName 惡意)不應 error: %v", err)
		}
		if strings.Contains(buf.String(), "</script><script>") {
			t.Error("PairName 未 sanitize,raw </script><script> 逸出進 HTML(XSS)")
		}
	})

	t.Run("benign_pairname_preserved", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:       "safe",
			TimeValues:    []float64{0, 1, 2},
			PairResults:   []CCIResult{{PairName: "RA/ES", Values: []float64{0.1, 0.2, 0.3}}},
			GaitStartTime: 0,
			GaitEndTime:   2.0,
		}
		var buf bytes.Buffer
		if err := GenerateCCIInteractiveChart(context.Background(), result, &buf); err != nil {
			t.Fatalf("合法 result 不應 error: %v", err)
		}
		if !strings.Contains(buf.String(), "RA/ES") {
			t.Error("正常 pair 名 RA/ES 不應被 sanitize mangle")
		}
	})
}
