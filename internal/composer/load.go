// Package composer 把 [[Chart Composer]] 一個 [[Subject]] 的資料組成 chart.ComposerInput:
// [[Manifest]] row → EMG([[Subject source]])、motion(換到 EMG 時間軸)、muscle_ratio
// Output-1(可選)與 [[Phase timeline]] 的 EMG 秒數。
//
// 獨立成套件而非放進 chart:本套件要 import io(讀 Output-1),而 io → cci → chart 的
// import 鏈讓 chart 不能 import io。gui 的 GenerateChartComposer 只剩驗證 → Load →
// chart.RenderComposer → envelope(ADR-0046)。
package composer

import (
	"errors"
	"fmt"
	"strings"

	"count_mean/internal/chart"
	"count_mean/internal/io"
	"count_mean/internal/manifest"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/synchronizer"
)

var (
	// ErrSubjectNotFound 表示 Subject 不在 manifest 內 —— 使用者輸入問題,不帶 Stage;
	// caller 以 errors.Is 辨識。
	ErrSubjectNotFound = errors.New("不存在於分期總檔案")
	// ErrMotionFileEmpty 表示 manifest row 的 MotionFile 為空(motion 是必要 grid)。
	ErrMotionFileEmpty = errors.New("manifest 內 MotionFile 為空")
)

// Stage 標示 Load 在哪個載入步驟失敗,caller 依此選 user-facing 訊息前綴。
type Stage int

const (
	StageManifest    Stage = iota + 1 // manifest.LoadManifests
	StageEMGOpen                      // EMG 路徑解析 / 開檔
	StageEMGParse                     // EMG 解析(*manifest.EMGParseError)
	StageMotion                       // motion 檔名為空、開檔或解析
	StageMuscleRatio                  // muscle_ratio Output-1 開檔或讀取
)

// LoadError 是 Load 的載入失敗:Stage 標示步驟。Error() 與 Err 逐字相同、Unwrap 回 Err
// (同 manifest.EMGParseError 的形狀),不改變任何輸出文字。
type LoadError struct {
	Stage Stage
	Err   error
}

func (e *LoadError) Error() string { return e.Err.Error() }

func (e *LoadError) Unwrap() error { return e.Err }

// Load 讀 manifest、找出 subject 的 row(case-sensitive exact match,取第一筆),組出
// [[Chart Composer]] 的 chart.ComposerInput:
//   - EMG:[[Subject source]](manifest.LoadEMG),columnar 原樣交給 chart
//   - MotionData(必要):motion-index 經 TimeSynchronizer.MotionIndexToEMGTime 換到 EMG 時間軸
//   - MuscleRatioData(可選,row.MuscleRatioFile 非空才載):io.ReadMuscleRatioOutputAll
//   - PhaseTimesEMG:[[Phase timeline]] 的 phase 名 → EMG 秒數(只含已提供的分期點)
//
// 錯誤:Subject 不在 manifest → wrap ErrSubjectNotFound(不帶 Stage);其餘載入失敗都是
// *LoadError。
func Load(manifestPath, dataFolder, subject string) (*chart.ComposerInput, error) {
	manifests, err := manifest.LoadManifests(manifestPath)
	if err != nil {
		return nil, &LoadError{Stage: StageManifest, Err: err}
	}

	row, found := findManifestBySubject(manifests, subject)
	if !found {
		return nil, fmt.Errorf("Subject %q %w", subject, ErrSubjectNotFound)
	}

	emg, err := manifest.LoadEMG(dataFolder, &row)
	if err != nil {
		var parseErr *manifest.EMGParseError
		if errors.As(err, &parseErr) {
			return nil, &LoadError{Stage: StageEMGParse, Err: err}
		}
		return nil, &LoadError{Stage: StageEMGOpen, Err: err}
	}

	motion, err := loadMotion(dataFolder, row.MotionFile, row.EMGMotionOffset)
	if err != nil {
		return nil, &LoadError{Stage: StageMotion, Err: err}
	}

	var muscleRatio *chart.MuscleRatioData
	if strings.TrimSpace(row.MuscleRatioFile) != "" {
		if muscleRatio, err = loadMuscleRatio(dataFolder, row.MuscleRatioFile); err != nil {
			return nil, &LoadError{Stage: StageMuscleRatio, Err: err}
		}
	}

	return &chart.ComposerInput{
		Subject:         row.Subject,
		EMG:             emg,
		MuscleRatioData: muscleRatio,
		MotionData:      motion,
		PhaseTimesEMG:   phaseTimesEMG(&row),
	}, nil
}

// findManifestBySubject 在 manifest list 內找第一個 Subject 等於 target 的 row。
// 第二個回傳值 false 表示沒找到。比對為 case-sensitive exact match。
func findManifestBySubject(
	manifests []models.PhaseManifest, target string,
) (models.PhaseManifest, bool) {
	for i := range manifests {
		if manifests[i].Subject == target {
			return manifests[i], true
		}
	}
	return models.PhaseManifest{}, false
}

// loadMotion 從 motion CSV 載入後轉成 chart.MotionData
// (time-series + per-channel slice + 可預測 channel 順序)。
//
// motion-index 透過 TimeSynchronizer.MotionIndexToEMGTime 換算為 EMG 時間軸,
// 讓 motion grid 與 EMG grid 在同一條 X 軸上對齊。
//
// 開檔走 manifest.OpenDataFile 硬化讀檔門(內部 lenient resolve + atomic
// validated-open)。
func loadMotion(dataFolder, motionFile string, emgMotionOffset int) (*chart.MotionData, error) {
	if strings.TrimSpace(motionFile) == "" {
		// motion 是必要 grid;空檔名直接 fail。
		return nil, ErrMotionFileEmpty
	}

	motionFileHandle, err := manifest.OpenDataFile(dataFolder, motionFile)
	if err != nil {
		return nil, fmt.Errorf("Motion 路徑解析失敗: %w", err)
	}
	defer func() { _ = motionFileHandle.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	motion, err := parsers.NewMotionParser().Parse(motionFileHandle, motionFile)
	if err != nil {
		return nil, fmt.Errorf("解析 Motion 失敗: %w", err)
	}

	ts := synchronizer.NewTimeSynchronizer()
	times := make([]float64, len(motion.Indices))
	for i, idx := range motion.Indices {
		times[i] = ts.MotionIndexToEMGTime(idx, emgMotionOffset)
	}

	// motion.Headers 已是純資料 channel 名 — parsers.MotionParser.initializeMotionData
	// 已透過 `Headers: headers[1:]` 把 Index 欄剝掉。過去這段對 `k == 0 continue` 是錯認
	// Index 仍在 Headers 內,結果把第一個真資料 channel 永遠不渲染(單 channel motion 整
	// grid 空白)。直接 iterate 即可。
	order := make([]string, 0, len(motion.Headers))
	series := make(map[string][]float64, len(motion.Headers))
	for _, name := range motion.Headers {
		vals := motion.Data[name]
		// 防呆:若 parser 因 jagged row 略 row 而 vals 不齊,直接 skip 該 series
		// 而非 panic;motion 資料量大,degraded mode 比 hard fail 對使用者好。
		if len(vals) != len(times) {
			continue
		}
		order = append(order, name)
		series[name] = vals
	}

	return &chart.MotionData{
		Time:   times,
		Series: series,
		Order:  order,
	}, nil
}

// loadMuscleRatio 讀 muscle_ratio Output-1 CSV 成 chart.MuscleRatioData(Order = pair 欄位序)。
//
// 開檔走 manifest.OpenDataFile 硬化讀檔門(支援 BTS 匯出含字面 "%" 的檔名);CSV 解析委派
// io.ReadMuscleRatioOutputAll(WriteMuscleRatioOutputAll 的反函式,空 cell 讀成 NaN)。
// 空檔 / 標題不足兩個 sentinel 原樣回傳,其餘讀取失敗加「讀取 muscle_ratio CSV 失敗」前綴。
func loadMuscleRatio(dataFolder, muscleRatioFile string) (*chart.MuscleRatioData, error) {
	mrFile, err := manifest.OpenDataFile(dataFolder, muscleRatioFile)
	if err != nil {
		return nil, fmt.Errorf("muscle_ratio 路徑解析失敗: %w", err)
	}
	payload, err := io.ReadMuscleRatioOutputAll(mrFile)
	_ = mrFile.Close() //nolint:errcheck // read-only fd; close error not actionable
	if err != nil {
		if !errors.Is(err, io.ErrMuscleRatioCSVEmpty) && !errors.Is(err, io.ErrMuscleRatioCSVNoHeader) {
			err = fmt.Errorf("讀取 muscle_ratio CSV 失敗: %w", err)
		}
		return nil, err
	}

	series := make(map[string][]float64, len(payload.PairLabels))
	for k, name := range payload.PairLabels {
		series[name] = payload.Ratios[k]
	}
	return &chart.MuscleRatioData{
		Time:   payload.Times,
		Series: series,
		Order:  payload.PairLabels,
	}, nil
}

// phaseTimesEMG 把 manifest row 的 [[Phase timeline]] 轉成「phase 名 → EMG 秒數」map
// (ADR-0042)。Composer 所有 grid 的 X 軸都是 EMG 時間;直接拿 manifest 原值畫 markLine
// 會整個偏一個 sync offset。同一份 map 供 chart 預設 markLine(ComposerInput.PhaseTimesEMG)
// 與 gui 回給前端的 phaseTimes 共用。未提供的分期點不在 map 內 — 不會 inject 偽 markLine,
// 前端也不渲染 checkbox。
func phaseTimesEMG(m *models.PhaseManifest) map[string]float64 {
	timeline := synchronizer.NewPhaseTimeline(m)
	out := make(map[string]float64, len(timeline))
	for _, pt := range timeline {
		out[string(pt.Phase)] = pt.EMGTime
	}
	return out
}
