package maxmean

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
)

// --- in-memory FileSource ---

type memFileSource struct {
	files []BatchFile
}

func (m *memFileSource) Discover() ([]BatchFile, error) {
	return m.files, nil
}

// --- spy ResultWriter ---

type writeCall struct {
	name       string
	headers    []string
	results    []models.MaxMeanResult
	startRange float64
	endRange   float64
}

type spyWriter struct {
	calls    []writeCall
	failName string // if non-empty, return an error when Write is called with this name
}

func (s *spyWriter) Write(
	name string,
	headers []string,
	results []models.MaxMeanResult,
	startRange, endRange float64,
) (string, error) {
	s.calls = append(s.calls, writeCall{
		name:       name,
		headers:    headers,
		results:    results,
		startRange: startRange,
		endRange:   endRange,
	})
	if s.failName != "" && name == s.failName {
		return "", errors.New("spy writer forced error")
	}
	return "/fake/path/" + name + ".csv", nil
}

// --- shared fixture helpers ---

// goodRecords returns a minimal valid CSV fixture: header + 4 data rows.
// scalingFactor=0 means the time column is used verbatim (no 10^N scaling).
// timeStart anchors the first row; subsequent rows increment by 1.
func goodRecords(timeStart float64) [][]string {
	return [][]string{
		{"Time", "Ch1"},
		{fmt.Sprintf("%g", timeStart), "100"},
		{fmt.Sprintf("%g", timeStart+1), "200"},
		{fmt.Sprintf("%g", timeStart+2), "150"},
		{fmt.Sprintf("%g", timeStart+3), "300"},
	}
}

// newCalc returns a real MaxMeanCalculator with scalingFactor=0.
// With scalingFactor=0 the data-parser leaves time values as-is (10^0 == 1),
// so fixture time strings like "1.0", "4.0" are interpreted verbatim.
func newCalc() *calculator.MaxMeanCalculator {
	return calculator.NewMaxMeanCalculator(0)
}

// --- Tests ---

// TestRunBatch_PartialSuccess asserts that SuccessCount/FailCount/Results
// correctly account for three distinct failure modes (Read error, calc error,
// Write error) alongside two successful files.
func TestRunBatch_PartialSuccess(t *testing.T) {
	calc := newCalc()
	spy := &spyWriter{failName: "write-fail"}

	readErr := errors.New("disk read error")
	// calc-fail: only 1 data row with windowSize=2 → dataset too small → calc error.
	calcFailRecords := [][]string{
		{"Time", "Ch1"},
		{"1.0", "100"},
	}

	good1 := goodRecords(1.0)  // times 1,2,3,4
	good2 := goodRecords(10.0) // times 10,11,12,13

	source := &memFileSource{files: []BatchFile{
		{Name: "read-fail", Read: func() ([][]string, error) { return nil, readErr }},
		{Name: "calc-fail", Read: func() ([][]string, error) { return calcFailRecords, nil }},
		{Name: "write-fail", Read: func() ([][]string, error) { return good1, nil }},
		{Name: "good1", Read: func() ([][]string, error) { return good1, nil }},
		{Name: "good2", Read: func() ([][]string, error) { return good2, nil }},
	}}

	res, err := RunBatch(context.Background(), calc, source, spy, BatchParams{WindowSize: 2})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, 2, res.SuccessCount, "SuccessCount should be 2")
	assert.Equal(t, 3, res.FailCount, "FailCount should be 3 (read+calc+write failures)")
}

// TestRunBatch_NameAndRangeMapping asserts that Write receives name==BatchFile.Name,
// headers==records[0], and the range actually used for that file (whole file here:
// first/last sample times 1..4).
func TestRunBatch_NameAndRangeMapping(t *testing.T) {
	calc := newCalc()
	spy := &spyWriter{}

	rec := goodRecords(1.0)
	params := BatchParams{WindowSize: 2} // Range 兩端皆未給 = 整檔

	source := &memFileSource{files: []BatchFile{
		{Name: "myfile", Read: func() ([][]string, error) { return rec, nil }},
	}}

	_, err := RunBatch(context.Background(), calc, source, spy, params)
	require.NoError(t, err)
	require.Len(t, spy.calls, 1)

	call := spy.calls[0]
	assert.Equal(t, "myfile", call.name, "Write should receive BatchFile.Name")
	assert.Equal(t, rec[0], call.headers, "Write should receive records[0] as headers")

	assert.Equal(t, 1.0, call.startRange, "whole file: startRange = first sample time")
	assert.Equal(t, 4.0, call.endRange, "whole file: endRange = last sample time")
}

// TestRunBatch_WriteOrder asserts that Write is called once per successful file,
// in discovery order, with each file's own headers (a leading failed file is skipped).
func TestRunBatch_WriteOrder(t *testing.T) {
	calc := newCalc()
	spy := &spyWriter{}

	headers1 := []string{"Time", "FirstCh"}
	headers2 := []string{"Time", "SecondCh"}

	data1 := goodRecords(1.0)
	data1[0] = headers1
	data2 := goodRecords(10.0)
	data2[0] = headers2

	source := &memFileSource{files: []BatchFile{
		{Name: "fail", Read: func() ([][]string, error) { return nil, errors.New("read err") }},
		{Name: "first-success", Read: func() ([][]string, error) { return data1, nil }},
		{Name: "second-success", Read: func() ([][]string, error) { return data2, nil }},
	}}

	res, err := RunBatch(context.Background(), calc, source, spy, BatchParams{WindowSize: 2})
	require.NoError(t, err)

	assert.Equal(t, 2, res.SuccessCount)
	assert.Equal(t, 1, res.FailCount)

	require.Len(t, spy.calls, 2)
	assert.Equal(t, "first-success", spy.calls[0].name)
	assert.Equal(t, headers1, spy.calls[0].headers)
	assert.Equal(t, "second-success", spy.calls[1].name)
	assert.Equal(t, headers2, spy.calls[1].headers)
}

// TestRunBatch_EmptySource asserts that an empty file list returns ErrNoCSVFilesInFolder.
func TestRunBatch_EmptySource(t *testing.T) {
	calc := newCalc()
	spy := &spyWriter{}
	source := &memFileSource{files: []BatchFile{}}

	res, err := RunBatch(context.Background(), calc, source, spy, BatchParams{WindowSize: 2})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoCSVFilesInFolder),
		"error should satisfy errors.Is(err, ErrNoCSVFilesInFolder)")
	assert.Nil(t, res)
}

// ptr 回傳 v 的指標,用來表達 MaxMeanRange 有給的那一端。
func ptr(v float64) *float64 { return &v }

// TestRunBatch_TimeRange 釘住 [[Max-mean]] 時間範圍「兩端各自可省略」的語意
// (ADR-0050):沒給的那端從資料實際首/末樣本算起,且寫給 Write 的範圍是實際採用的值。
// fixture 用 scalingFactor=0,時間與通道值即原值;goodRecords(1.0) = 時間 1..4、
// 值 100,200,150,300。
func TestRunBatch_TimeRange(t *testing.T) {
	cases := []struct {
		name               string
		rec                [][]string
		params             BatchParams
		wantStart, wantEnd float64 // 寫出的「開始/結束範圍秒數」
		wantMaxMean        float64
		wantWinStart       float64 // 最佳窗口首樣本時間
		wantWinEnd         float64 // 最佳窗口末樣本時間
	}{
		{
			// 兩端皆未給 = 整檔:範圍列印實際首/末樣本 1..4;最佳窗 (150+300)/2 = 225 @ 3..4。
			name:      "WholeFile",
			rec:       goodRecords(1.0),
			params:    BatchParams{WindowSize: 2},
			wantStart: 1, wantEnd: 4,
			wantMaxMean: 225, wantWinStart: 3, wantWinEnd: 4,
		},
		{
			// 兩端都給 [1.5, 3.5]:原樣轉發;範圍內 200,150 → 唯一窗 175 @ 2..3。
			name:      "BothEnds",
			rec:       goodRecords(1.0),
			params:    BatchParams{WindowSize: 2, Range: calculator.MaxMeanRange{Start: ptr(1.5), End: ptr(3.5)}},
			wantStart: 1.5, wantEnd: 3.5,
			wantMaxMean: 175, wantWinStart: 2, wantWinEnd: 3,
		},
		{
			// 只給起點 2:範圍 [2, 末樣本 4] → 200,150,300 → 最佳窗 (150+300)/2 = 225 @ 3..4。
			name:      "StartOnly",
			rec:       goodRecords(1.0),
			params:    BatchParams{WindowSize: 2, Range: calculator.MaxMeanRange{Start: ptr(2)}},
			wantStart: 2, wantEnd: 4,
			wantMaxMean: 225, wantWinStart: 3, wantWinEnd: 4,
		},
		{
			// 最後一列時間欄空白(parser 會跳過該列):整檔 = 實際首/末樣本 1..4。
			name:      "BlankLastTimeCell",
			rec:       append(goodRecords(1.0), []string{"", "999"}),
			params:    BatchParams{WindowSize: 2},
			wantStart: 1, wantEnd: 4,
			wantMaxMean: 225, wantWinStart: 3, wantWinEnd: 4,
		},
		{
			// 只給終點 2,資料含負時間:起點從首樣本 -2 起算,負時間樣本不得被丟掉。
			// 範圍 -2..2 → 500,400,10,20,30 → 最佳窗 (500+400)/2 = 450 @ -2..-1。
			name: "EndOnlyKeepsNegativeTimes",
			rec: [][]string{
				{"Time", "Ch1"},
				{"-2", "500"},
				{"-1", "400"},
				{"0", "10"},
				{"1", "20"},
				{"2", "30"},
			},
			params:    BatchParams{WindowSize: 2, Range: calculator.MaxMeanRange{End: ptr(2)}},
			wantStart: -2, wantEnd: 2,
			wantMaxMean: 450, wantWinStart: -2, wantWinEnd: -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &spyWriter{}
			source := &memFileSource{files: []BatchFile{
				{Name: "f", Read: func() ([][]string, error) { return tc.rec, nil }},
			}}

			res, err := RunBatch(context.Background(), newCalc(), source, spy, tc.params)
			require.NoError(t, err)
			assert.Equal(t, 1, res.SuccessCount)
			assert.Equal(t, 0, res.FailCount)
			require.Len(t, spy.calls, 1)

			call := spy.calls[0]
			assert.Equal(t, tc.wantStart, call.startRange)
			assert.Equal(t, tc.wantEnd, call.endRange)
			require.Len(t, call.results, 1)
			assert.Equal(t, tc.wantMaxMean, call.results[0].MaxMean)
			assert.Equal(t, tc.wantWinStart, call.results[0].StartTime)
			assert.Equal(t, tc.wantWinEnd, call.results[0].EndTime)
		})
	}
}
