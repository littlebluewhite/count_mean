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
// headers==records[0], and startRange/endRange equal to what calculator.ResolveTimeRange
// resolves for that file's records and params.
func TestRunBatch_NameAndRangeMapping(t *testing.T) {
	calc := newCalc()
	spy := &spyWriter{}

	rec := goodRecords(1.0)
	params := BatchParams{WindowSize: 2} // StartTime=0, EndTime=0

	source := &memFileSource{files: []BatchFile{
		{Name: "myfile", Read: func() ([][]string, error) { return rec, nil }},
	}}

	_, err := RunBatch(context.Background(), calc, source, spy, params)
	require.NoError(t, err)
	require.Len(t, spy.calls, 1)

	call := spy.calls[0]
	assert.Equal(t, "myfile", call.name, "Write should receive BatchFile.Name")
	assert.Equal(t, rec[0], call.headers, "Write should receive records[0] as headers")

	wantStart, wantEnd := calculator.ResolveTimeRange(rec, params.StartTime, params.EndTime)
	assert.Equal(t, wantStart, call.startRange, "startRange must equal ResolveTimeRange output")
	assert.Equal(t, wantEnd, call.endRange, "endRange must equal ResolveTimeRange output")
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

// TestRunBatch_TimeRangeForwarding verifies that startRange/endRange forwarded
// to Write equals calculator.ResolveTimeRange(records, params.StartTime, params.EndTime).
//
// Scenario A — params (0,0) with data that has non-zero timestamps:
// ResolveTimeRange derives (firstTime, lastTime) from the data, which is
// non-zero, so the resolved pair is non-(0,0). The spy must receive the resolved
// pair, not the raw (0,0) input.
//
// Scenario B — explicit non-zero params: ResolveTimeRange returns them
// unchanged; spy must receive the same values.
//
// NOTE: We do NOT assert "input (0,0) → CalculateFromRawData branch". The
// dispatch in RunBatch keys on the resolved pair, not the raw input. With
// typical fixture data the resolved pair is non-zero, so the runner uses
// CalculateFromRawDataWithRange regardless of whether the caller passed (0,0).
func TestRunBatch_TimeRangeForwarding(t *testing.T) {
	calc := newCalc()

	t.Run("ZeroParamsResolvedFromData", func(t *testing.T) {
		spy := &spyWriter{}
		rec := goodRecords(1.0) // first data time=1.0, last data time=4.0

		params := BatchParams{WindowSize: 2, StartTime: 0, EndTime: 0}
		source := &memFileSource{files: []BatchFile{
			{Name: "f", Read: func() ([][]string, error) { return rec, nil }},
		}}

		_, err := RunBatch(context.Background(), calc, source, spy, params)
		require.NoError(t, err)
		require.Len(t, spy.calls, 1)

		wantStart, wantEnd := calculator.ResolveTimeRange(rec, 0, 0)

		// Sanity-check the fixture: resolved range should be non-zero (1.0, 4.0).
		assert.NotEqual(t, 0.0, wantStart, "fixture must produce non-zero resolved start")
		assert.NotEqual(t, 0.0, wantEnd, "fixture must produce non-zero resolved end")

		// Runner must forward the RESOLVED pair to Write.
		assert.Equal(t, wantStart, spy.calls[0].startRange,
			"startRange to Write must equal ResolveTimeRange output, not raw input 0")
		assert.Equal(t, wantEnd, spy.calls[0].endRange,
			"endRange to Write must equal ResolveTimeRange output, not raw input 0")
	})

	t.Run("ExplicitNonZeroParamsForwardedUnchanged", func(t *testing.T) {
		spy := &spyWriter{}
		rec := goodRecords(1.0)

		params := BatchParams{WindowSize: 2, StartTime: 1.5, EndTime: 3.5}
		source := &memFileSource{files: []BatchFile{
			{Name: "f", Read: func() ([][]string, error) { return rec, nil }},
		}}

		_, err := RunBatch(context.Background(), calc, source, spy, params)
		require.NoError(t, err)
		require.Len(t, spy.calls, 1)

		// ResolveTimeRange returns (startTime, endTime) unchanged when either is non-zero.
		wantStart, wantEnd := calculator.ResolveTimeRange(rec, params.StartTime, params.EndTime)
		assert.Equal(t, params.StartTime, wantStart)
		assert.Equal(t, params.EndTime, wantEnd)

		assert.Equal(t, wantStart, spy.calls[0].startRange,
			"explicit StartTime must be forwarded to Write unchanged")
		assert.Equal(t, wantEnd, spy.calls[0].endRange,
			"explicit EndTime must be forwarded to Write unchanged")
	})
}
