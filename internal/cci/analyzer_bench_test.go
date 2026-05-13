package cci

import (
	"testing"

	"count_mean/internal/models"
)

// generateCCIBenchData 產生合成的 CCI benchmark 輸入：所有 DefaultMusclePairs 涵蓋
// 的肌肉通道 × n 個時間點，channelMap 直接以肌肉短名作為 header。
func generateCCIBenchData(n int) (*models.PhaseSyncEMGData, map[string]string) {
	pairs := DefaultMusclePairs()

	muscleSet := make(map[string]struct{})
	for _, p := range pairs {
		muscleSet[p.Muscle1] = struct{}{}
		muscleSet[p.Muscle2] = struct{}{}
	}

	headers := make([]string, 0, len(muscleSet))
	channels := make(map[string][]float64, len(muscleSet))
	channelMap := make(map[string]string, len(muscleSet))

	for muscle := range muscleSet {
		headers = append(headers, muscle)
		channelMap[muscle] = muscle

		series := make([]float64, n)
		for i := range series {
			series[i] = float64(i%1000) + 1.0
		}

		channels[muscle] = series
	}

	times := make([]float64, n)
	for i := range times {
		times[i] = float64(i) * 0.001
	}

	return &models.PhaseSyncEMGData{
		Time:     times,
		Channels: channels,
		Headers:  headers,
	}, channelMap
}

// BenchmarkCCI_ComputeAllPairs_Parallel 量現行 errgroup 平行版本吞吐量。
func BenchmarkCCI_ComputeAllPairs_Parallel(b *testing.B) {
	const points = 300_000

	emgData, channelMap := generateCCIBenchData(points)
	analyzer := NewCCIAnalyzer()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result := analyzer.computeAllPairs("bench", emgData, channelMap, nil, nil)
		if len(result.PairResults) == 0 {
			b.Fatal("expected non-empty results")
		}
	}
}

// BenchmarkCCI_ComputeAllPairs_Sequential 對照組：舊版序列實作，用來量化平行版加速比。
// computeAllPairsSequential 只存在於 _test.go，不進 prod binary。
func BenchmarkCCI_ComputeAllPairs_Sequential(b *testing.B) {
	const points = 300_000

	emgData, channelMap := generateCCIBenchData(points)
	analyzer := NewCCIAnalyzer()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result := analyzer.computeAllPairsSequential("bench", emgData, channelMap, nil, nil)
		if len(result.PairResults) == 0 {
			b.Fatal("expected non-empty results")
		}
	}
}

// computeAllPairsSequential 是 computeAllPairs 平行化前的序列版本，
// 保留在測試碼內僅作為 benchmark 對照組，不參與 prod 邏輯。
func (a *CCIAnalyzer) computeAllPairsSequential(
	subject string,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
	phasePercents map[string]float64,
	phaseTimes map[string]float64,
) *CCIAnalysisResult {
	pairs := DefaultMusclePairs()
	pairResults := make([]CCIResult, 0, len(pairs))
	meanCurves := make(map[string][]float64, len(pairs))

	for _, pair := range pairs {
		cciValues := a.computeSinglePair(pair, emgData, channelMap)
		if cciValues == nil {
			continue
		}

		pairResults = append(pairResults, CCIResult{
			PairName: pair.Name,
			Values:   cciValues,
		})

		meanCurves[pair.Name] = cciValues
	}

	return &CCIAnalysisResult{
		Subject:       subject,
		PairResults:   pairResults,
		TimeValues:    emgData.Time,
		PhasePercents: phasePercents,
		PhaseTimes:    phaseTimes,
		MeanCurves:    meanCurves,
	}
}
