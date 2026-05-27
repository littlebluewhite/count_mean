# Format-Aware Write Contract Collapse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `io.CSVHandler` 既有 4 個 format-aware write method 統一為 `(outputPath, error)` return shape，新增 `WriteCCIResult` + `WriteMuscleRatioOutputAll` + `WriteMuscleRatioOutputPhases` 三個 method，把 CCI 與 muscle_ratio 的 row layout 吸進 CSVHandler，落實 ADR-0001 「row layout invariant 100% 覆蓋」結論（同時尊重 ADR-0004 三條 sticky boundary）。

**Architecture:** Format-aware write 深化的最後一哩 — row layout 從 `cci/analyzer.go` 與 `muscle_ratio/analyzer.go` 搬進 `io/csv_handler.go`；business semantics（muscle_ratio sticky-success）與 closure shape（`AnalysisHandler[P, R].WriteCSV`）刻意保留不動。CCI streaming + ctx-aware cancellation 透過 `csvutil.WriteCSVAtomic` 的 Emit callback 維持。Subject-based write 在 CSVHandler 內推導 filename，File-based 走 caller-supplied `req.Filename` — 兩種 ownership 共存（ADR-0004 Boundary 2）。

**Tech Stack:** Go 1.25+ · `csvutil.WriteCSVAtomic` · `internal/io.CSVHandler` · `internal/cci.CCIAnalysisResult` · `internal/muscle_ratio.SubjectResult` · testify/assert

**Reference docs (read these first):**
- `docs/adr/0001-phase-sync-export-via-csvhandler.md` — 統一 write path 的起點
- `docs/adr/0004-format-aware-write-collapse-boundaries.md` — 三條 sticky boundary（本 plan 嚴守）
- `CONTEXT.md` Format-aware write 條目 — 領域語言定義
- GitHub Issue #21 — Q5 (family err channel) follow-up（本 plan **不**動 err channel）

---

## File Structure

新建：
- 無新檔案。所有變更落在現有檔案內。

修改：

| 路徑 | 責任 | Phase |
|---|---|---|
| `internal/io/csv_handler.go` | 4 個現有 method 加 outputPath return；新增 3 個 method；新增 payload struct（CCI / MR）；新增 io→cci import | 1, 2, 4 |
| `internal/io/csv_handler_format_aware_test.go` | 現有 tests 改吃新 return shape；新增 CCI / MR test | 1, 2, 4 |
| `internal/cci/analyzer.go` | 刪除 `ExportToCSV` + `writeCSVFile`；保留 `CCIAnalysisResult` 公開型 | 3 |
| `internal/cci/exporttocsv_test.go` | 對應刪除（覆蓋範圍已轉移到 `csv_handler_format_aware_test.go`） | 3 |
| `internal/muscle_ratio/analyzer.go` | `Params` 加 `CSVHandler` 欄位；`analyzeSubject` 改呼 CSVHandler；刪除 `writeOutputAll` / `writeOutputPhases` | 5, 6 |
| `internal/muscle_ratio/writer_atomic_test.go` | 對應刪除 / 縮減（覆蓋範圍轉移到 `csv_handler_format_aware_test.go`） | 6 |
| `gui/app.go` | `AnalyzePhases` / `CalculateMaxMean` / `NormalizeData` 等 caller 從 `(err)` 改 receive `(path, err)`；移除 `filepath.Join` 重算 | 1 |
| `gui/cci_handlers.go` | `WriteCSV` closure 從 `cciAnalyzer.ExportToCSV` 改呼 `handler.WriteCCIResult`；改 `// Candidate 2 TODO` comment 文案 | 3 |
| `gui/muscle_ratio_handlers.go` | 把 `s.csvHandler` 灌進 `muscle_ratio.Params`；改 `WriteCSV: nil` 旁的 comment 文案（nil 不是 TODO 是設計如此） | 5 |

---

## Task 1: WriteMaxMean / WriteNormalized / WritePhaseAnalysis 三個 method 加 outputPath return

**Why this task matters:** ADR-0001 已把 `WritePhaseSyncResult` 訂為「`(outputPath, error)` shape」。其他三個既有 method 仍是 `error` only — caller 端（`gui/app.go`）得自己 `filepath.Join(s.config.OutputDir, outputFile)` 算 outputPath，這條 join 是 CSVHandler 內部資訊的 leak。本 task 把三個 method 統一拉到 ADR-0001 shape，CSVHandler 負責內部 join，caller 拿 outputPath 不再重算。

**Files:**
- Modify: `internal/io/csv_handler.go:649-717`（WriteMaxMean / WriteNormalized / WritePhaseAnalysis 三個 method 簽章）
- Modify: `internal/io/csv_handler_format_aware_test.go:60-260`（既有 round-trip / empty-input tests 改吃 `(path, err)`）
- Modify: `gui/app.go:428-440`（CalculateMaxMean 內 WriteMaxMean caller）
- Modify: `gui/app.go:582-595`（batch maxmean 內 WriteMaxMean caller）
- Modify: `gui/app.go:869-882`（AnalyzePhases closure 內 WritePhaseAnalysis caller）
- Find via grep then Modify: `gui/app.go` 內 WriteNormalized caller（用 grep 找）

- [ ] **Step 1.1: 找出所有 caller**

Run:
```bash
grep -n "WriteMaxMean\b\|WriteNormalized\b\|WritePhaseAnalysis\b" gui/app.go
```

Expected: 至少 4 個 hits (WriteMaxMean × 2, WritePhaseAnalysis × 1, WriteNormalized × ?)

- [ ] **Step 1.2: 改 csv_handler_format_aware_test.go — TestWriteMaxMean_RoundTrip 預期新 return**

修改 `internal/io/csv_handler_format_aware_test.go:60` 附近的 WriteMaxMean call site：

```go
// before
err := handler.WriteMaxMean(
    io.WriteRequest{Filename: "result.csv"},
    headers, results, 0.0, 1.0,
)
require.NoError(t, err)

// after
outputPath, err := handler.WriteMaxMean(
    io.WriteRequest{Filename: "result.csv"},
    headers, results, 0.0, 1.0,
)
require.NoError(t, err)
require.Equal(t, filepath.Join(handler.OutputDir(), "result.csv"), outputPath)
```

對 file 內所有 WriteMaxMean / WriteNormalized / WritePhaseAnalysis 的 call site（line 60, 87, 141, 173, 213, 228, 242 等）都做相同改動。

`handler.OutputDir()` 是 helper getter；若 CSVHandler 沒有 exposed getter 則改用 test 自己 set 的 outputDir。

- [ ] **Step 1.3: 跑測試確認 RED（compile fail）**

Run:
```bash
go test ./internal/io/... -run 'TestWrite(MaxMean|Normalized|PhaseAnalysis)' 2>&1 | head -30
```

Expected: 編譯失敗 — `outputPath declared but not used` 或 `WriteMaxMean returns 1 value but 2 expected`。RED 確認 test 已對齊新 contract。

- [ ] **Step 1.4: 改 csv_handler.go — 三個 method 加 outputPath return**

`internal/io/csv_handler.go:649-658`（WriteMaxMean）:

```go
// before
func (h *CSVHandler) WriteMaxMean(
    req WriteRequest,
    headers []string,
    results []models.MaxMeanResult,
    startRange, endRange float64,
) error {
    data := h.converter.ConvertMaxMeanResults(headers, results, startRange, endRange)
    return h.writeToTarget(req, data)
}

// after
func (h *CSVHandler) WriteMaxMean(
    req WriteRequest,
    headers []string,
    results []models.MaxMeanResult,
    startRange, endRange float64,
) (string, error) {
    data := h.converter.ConvertMaxMeanResults(headers, results, startRange, endRange)
    if err := h.writeToTarget(req, data); err != nil {
        return "", err
    }
    return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
}
```

`internal/io/csv_handler.go:664-668`（WriteNormalized）:

```go
func (h *CSVHandler) WriteNormalized(req WriteRequest, dataset *models.EMGDataset) (string, error) {
    data := h.converter.ConvertNormalizedData(dataset)
    if err := h.writeToTarget(req, data); err != nil {
        return "", err
    }
    return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
}
```

`internal/io/csv_handler.go:685-717`（WritePhaseAnalysis）— 在 `return h.writeToTarget(req, data)` 之前加：

```go
// before final line
return h.writeToTarget(req, data)

// after final lines
if err := h.writeToTarget(req, data); err != nil {
    return "", err
}
return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
```

並改函式簽章 `error` → `(string, error)`。

- [ ] **Step 1.5: 跑測試確認 GREEN（io 內部 tests）**

Run:
```bash
go test ./internal/io/... -run 'TestWrite(MaxMean|Normalized|PhaseAnalysis)' -v
```

Expected: 全部 PASS。

- [ ] **Step 1.6: 改 gui/app.go callers — 收掉 filepath.Join 重算**

`gui/app.go:428-440`（CalculateMaxMean）:

```go
// before
if err := s.csvHandler.WriteMaxMean(
    io.WriteRequest{Filename: outputFile},
    records[0], results, startRange, endRange,
); err != nil {
    return nil, fmt.Errorf("寫入輸出檔案失敗: %w", err)
}

return &MaxMeanResult{
    OutputPath: filepath.Join(s.config.OutputDir, outputFile),
    Headers:    records[0],
    Results:    convertMaxMeanResultsToArray(results),
}, nil

// after
outputPath, writeErr := s.csvHandler.WriteMaxMean(
    io.WriteRequest{Filename: outputFile},
    records[0], results, startRange, endRange,
)
if writeErr != nil {
    return nil, fmt.Errorf("寫入輸出檔案失敗: %w", writeErr)
}

return &MaxMeanResult{
    OutputPath: outputPath,
    Headers:    records[0],
    Results:    convertMaxMeanResultsToArray(results),
}, nil
```

`gui/app.go:582-595`（batch 變體）:

```go
// before
if writeErr := s.csvHandler.WriteMaxMean(
    io.WriteRequest{Filename: outputFile, SubDir: ctx.outputDirName},
    records[0], results, startRange, endRange,
); writeErr != nil {
    return nil, fmt.Errorf("寫入CSV輸出失敗: %w", writeErr)
}

// after
if _, writeErr := s.csvHandler.WriteMaxMean(
    io.WriteRequest{Filename: outputFile, SubDir: ctx.outputDirName},
    records[0], results, startRange, endRange,
); writeErr != nil {
    return nil, fmt.Errorf("寫入CSV輸出失敗: %w", writeErr)
}
```

（這個 batch 變體不需 outputPath，因為回傳的 batchProcessResult 不含 path 欄位；用 `_` 忽略。）

`gui/app.go:869-882`（AnalyzePhases closure）:

```go
// before
WriteCSV: func(handler *io.CSVHandler, data phaseRunData) (string, error) {
    outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)
    if writeErr := handler.WritePhaseAnalysis(
        io.WriteRequest{Filename: outputName}, data.records[0], data.analysisResult,
    ); writeErr != nil {
        return "", fmt.Errorf("保存結果失敗: %w", writeErr)
    }

    return filepath.Join(s.config.OutputDir, outputName), nil
},

// after
WriteCSV: func(handler *io.CSVHandler, data phaseRunData) (string, error) {
    outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)
    outputPath, writeErr := handler.WritePhaseAnalysis(
        io.WriteRequest{Filename: outputName}, data.records[0], data.analysisResult,
    )
    if writeErr != nil {
        return "", fmt.Errorf("保存結果失敗: %w", writeErr)
    }
    return outputPath, nil
},
```

WriteNormalized caller（由 Step 1.1 找出的位置）做相同改動 — 接 `(outputPath, err)` 兩值。

- [ ] **Step 1.7: 跑全測試 + lint 確認 GREEN**

Run:
```bash
go test ./internal/io/... ./gui/... -count=1
golangci-lint run --timeout=2m ./internal/io/... ./gui/...
```

Expected: 全 PASS, lint clean。

- [ ] **Step 1.8: Commit**

```bash
git add internal/io/csv_handler.go internal/io/csv_handler_format_aware_test.go gui/app.go
git commit -m "$(cat <<'EOF'
refactor(io): WriteMaxMean / WriteNormalized / WritePhaseAnalysis 加 (outputPath, error) return — 對齊 ADR-0001 shape

format-aware write 4 個 method 中 PhaseSync 已採 (outputPath, error)。剩三個既有 method
原本 caller 端得自己 filepath.Join(OutputDir, Filename) 算 path,本 commit 收回 CSVHandler
內部統一 join 後 return。

- WriteMaxMean / WriteNormalized / WritePhaseAnalysis 簽章 error → (string, error)
- gui/app.go: 3 個 caller 端 filepath.Join 重算移除,改為直接接 outputPath
- csv_handler_format_aware_test.go: 7 個 call site 改吃新 return shape

Plan: docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md (Task 1)
EOF
)"
```

---

## Task 2: 新增 io→cci 套件 dep 並 WriteCCIResult method（含 ctx-aware streaming）

**Why this task matters:** Candidate 1 最具實質 depth 增益的一步 — 把 CCI 的 12-pair × N-point row layout（含 NaN/Inf 處理、droppedRowCount log、ctx-aware emit）從 `internal/cci/analyzer.go:540-664` 搬進 `internal/io/csv_handler.go`。注意 ADR-0004 Boundary 2：CSVHandler 內部從 `result.Subject` 推導 filename，`req.Filename` 被忽略。

**Files:**
- Modify: `internal/io/csv_handler.go:1-25`（imports 加 cci）
- Modify: `internal/io/csv_handler.go` 末尾（append WriteCCIResult）
- Modify: `internal/io/csv_handler_format_aware_test.go` 末尾（append CCI tests）

- [ ] **Step 2.1: 加 import**

`internal/io/csv_handler.go:15-25` 區段內 imports 新增：

```go
"count_mean/internal/cci"
"count_mean/internal/csvutil"  // 確認已存在,沒有就補
```

`csvutil` 應該已 import（既有 method 用過 `csvutil.SanitizeAllRows` 等）— 確認即可。`cci` 是新增方向。

- [ ] **Step 2.2: 寫 failing test — TestWriteCCIResult_RoundTrip**

`internal/io/csv_handler_format_aware_test.go` 末尾 append：

```go
// TestWriteCCIResult_RoundTrip 驗證 WriteCCIResult 寫出的檔可被讀回,
// row layout (Time / Gait Cycle / N pair columns) 對齊.
func TestWriteCCIResult_RoundTrip(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    result := &cci.CCIAnalysisResult{
        Subject:       "subj_A",
        GaitStartTime: 0.0,
        GaitEndTime:   1.0,
        TimeValues:    []float64{0.0, 0.5, 1.0},
        PairResults: []cci.PairResult{
            {PairName: "P1_P2", Values: []float64{0.1, 0.2, 0.3}},
            {PairName: "P3_P4", Values: []float64{0.4, 0.5, 0.6}},
        },
    }

    outputPath, err := handler.WriteCCIResult(context.Background(), io.WriteRequest{}, result)
    require.NoError(t, err)
    assert.Equal(t, filepath.Join(outputDir, "subj_A_CCI_Rudolph.csv"), outputPath)

    rows := readCSV(t, outputPath)
    require.Len(t, rows, 4) // 1 header + 3 data rows
    assert.Equal(t, []string{"Time (s)", "Gait Cycle (%)", "P1_P2", "P3_P4"}, rows[0])
    assert.Equal(t, []string{"0.0000", "0.00", "0.100000", "0.400000"}, rows[1])
}

// TestWriteCCIResult_NaNRowSkip 驗證全 NaN/Inf 的 row 被跳過 + droppedRowCount log.
func TestWriteCCIResult_NaNRowSkip(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    nan := math.NaN()
    result := &cci.CCIAnalysisResult{
        Subject:       "subj_B",
        GaitStartTime: 0.0,
        GaitEndTime:   1.0,
        TimeValues:    []float64{0.0, 0.5, 1.0},
        PairResults: []cci.PairResult{
            {PairName: "P1", Values: []float64{0.1, nan, 0.3}},
            {PairName: "P2", Values: []float64{0.2, nan, 0.4}},
        },
    }

    outputPath, err := handler.WriteCCIResult(context.Background(), io.WriteRequest{}, result)
    require.NoError(t, err)

    rows := readCSV(t, outputPath)
    require.Len(t, rows, 3) // header + 2 surviving rows (中間 row 全 NaN 被 skip)
}

// TestWriteCCIResult_CtxCancel 驗證 pre-cancel 不寫檔.
func TestWriteCCIResult_CtxCancel(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    ctx, cancel := context.WithCancel(context.Background())
    cancel() // pre-cancel

    result := &cci.CCIAnalysisResult{
        Subject:       "subj_C",
        GaitStartTime: 0.0,
        GaitEndTime:   1.0,
        TimeValues:    []float64{0.0},
        PairResults:   []cci.PairResult{{PairName: "P1", Values: []float64{0.1}}},
    }

    _, err := handler.WriteCCIResult(ctx, io.WriteRequest{}, result)
    require.ErrorIs(t, err, context.Canceled)

    _, statErr := os.Stat(filepath.Join(outputDir, "subj_C_CCI_Rudolph.csv"))
    require.True(t, os.IsNotExist(statErr), "pre-cancel 不應留下檔案")
}
```

註：`newTestCSVHandler` / `readCSV` 是既有 helper（看現有 test file 上下文確認）；若沒有則複用 ADR-0001 PhaseSync test 同名 helper。

- [ ] **Step 2.3: 跑測試確認 RED**

Run:
```bash
go test ./internal/io/... -run 'TestWriteCCIResult' -v
```

Expected: 編譯失敗 — `handler.WriteCCIResult undefined`。

- [ ] **Step 2.4: 實作 WriteCCIResult**

`internal/io/csv_handler.go` 末尾 append（在 `WritePhaseSyncResult` 之後）：

```go
// WriteCCIResult 把 CCI 分析結果寫成 CSV。
//
// Filename 由 result.Subject 經 SanitizeFileName 後 + "_CCI_Rudolph.csv" suffix
// 推導 — req.Filename 被忽略,僅 req.SubDir 生效(空字串 → OutputDir 根)。
// 回傳實際 outputPath 與錯誤。
//
// row layout: 1 header row ["Time (s)", "Gait Cycle (%)", PairName...]
//             + N data rows,每 row 含 [time, gait_pct, pair_value...]
// NaN/Inf cell → 空字串;整 row 所有 pair 都 NaN/Inf → skip 整 row(計入 droppedRowCount)。
//
// ctx 為第一個參數,sample loop 中每 cciChartCtxCheckInterval 點檢查一次 ctx.Done()
// — caller cancel 後立即停寫並回 ctx.Err。csvutil.WriteCSVAtomic 對 emit 回 error 走
// tmp file abort 路徑,不留下半成品。
//
// ADR-0001 invariant 延伸到 CCI: SanitizePath / SanitizeAllRows / OpenWriteValidated
// 三道 defense-in-depth 守門 同步覆蓋(原 cci.ExportToCSV 走 csvutil.WriteCSVAtomic
// 是 Atomic 但缺 path validator;由本 method 統一補上)。
func (h *CSVHandler) WriteCCIResult(
    ctx context.Context, req WriteRequest, result *cci.CCIAnalysisResult,
) (string, error) {
    if err := ctx.Err(); err != nil {
        return "", err
    }
    if result == nil {
        return "", errEmptyCCIResult
    }

    duration := result.GaitEndTime - result.GaitStartTime
    if duration <= 0 {
        return "", fmt.Errorf("%w: start=%v end=%v",
            cci.ErrInvalidGaitCycle, result.GaitStartTime, result.GaitEndTime)
    }

    safeSubject := calculator.SanitizeFileName(result.Subject)
    filename := fmt.Sprintf("%s_CCI_Rudolph.csv", safeSubject)
    outputPath := filepath.Join(h.config.OutputDir, req.SubDir, filename)

    // Defense-in-depth: 對齊 muscle_ratio.Analyzer / WritePhaseSyncResult 同款守門
    checkPath := filepath.Join(h.config.OutputDir, req.SubDir, "_validation_marker")
    if err := h.pathValidator.ValidateExternalPath(checkPath); err != nil {
        return "", fmt.Errorf("CCI 輸出路徑無效: %w", err)
    }

    if err := os.MkdirAll(filepath.Dir(outputPath), fsperm.DirPerm); err != nil {
        return "", fmt.Errorf("CCI 輸出目錄建立失敗: %w", err)
    }

    header := []string{"Time (s)", "Gait Cycle (%)"}
    for _, pr := range result.PairResults {
        header = append(header, pr.PairName)
    }

    var droppedRowCount int
    numPoints := len(result.TimeValues)

    err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
        Header: header,
        Emit: func(emit func([]string) error) error {
            for i := 0; i < numPoints; i++ {
                if i > 0 && i%cciStreamCtxCheckInterval == 0 {
                    select {
                    case <-ctx.Done():
                        return ctx.Err()
                    default:
                    }
                }

                t := result.TimeValues[i]
                pct := (t - result.GaitStartTime) / duration * 100

                pairCells := make([]string, 0, len(result.PairResults))
                allNonFinite := true

                for _, pr := range result.PairResults {
                    if i >= len(pr.Values) {
                        pairCells = append(pairCells, "")
                        continue
                    }
                    v := pr.Values[i]
                    if math.IsNaN(v) || math.IsInf(v, 0) {
                        pairCells = append(pairCells, "")
                        continue
                    }
                    pairCells = append(pairCells, fmt.Sprintf("%.6f", v))
                    allNonFinite = false
                }

                if len(result.PairResults) > 0 && allNonFinite {
                    droppedRowCount++
                    continue
                }

                row := []string{
                    fmt.Sprintf("%.4f", t),
                    fmt.Sprintf("%.2f", pct),
                }
                row = append(row, pairCells...)

                if err := emit(row); err != nil {
                    return err
                }
            }
            return nil
        },
    })
    if err != nil {
        return "", err
    }

    if droppedRowCount > 0 && h.logger != nil {
        h.logger.Warn("CCI 匯出 CSV 時跳過全 NaN/Inf 的 row", map[string]any{
            "dropped_rows": droppedRowCount,
            "total_rows":   numPoints,
            "output_path":  outputPath,
        })
    }

    return outputPath, nil
}

// cciStreamCtxCheckInterval 是 CCI 寫檔 emit loop 內 ctx 取消檢查間隔,
// 跟原 cci 套件常數 (cciChartCtxCheckInterval) 對齊,避免每點 select 過熱。
const cciStreamCtxCheckInterval = 64

// errEmptyCCIResult 標示 WriteCCIResult 收到 nil result.
var errEmptyCCIResult = stderrors.New("WriteCCIResult: result is nil")
```

注意：`cci.ErrInvalidGaitCycle` 必須是 cci 套件已 export 的 sentinel — 確認 `internal/cci/errors.go`（或同等位置）有；沒有的話本 task 順手在 cci 套件 export 之。

- [ ] **Step 2.5: 跑測試確認 GREEN**

Run:
```bash
go test ./internal/io/... -run 'TestWriteCCIResult' -v
```

Expected: 3 個 sub-test 全 PASS。

- [ ] **Step 2.6: Commit**

```bash
git add internal/io/csv_handler.go internal/io/csv_handler_format_aware_test.go
git commit -m "$(cat <<'EOF'
feat(io): WriteCCIResult — CSVHandler 吸 CCI row layout(含 ctx-aware streaming)

ADR-0004 Boundary 2: Subject-based write,CSVHandler 內部從 result.Subject 推導
filename ({safeSubject}_CCI_Rudolph.csv),req.Filename 被忽略。

實作沿用 csvutil.WriteCSVAtomic streaming Emit pattern,維持原 cci.ExportToCSV 的:
- ctx-aware cancellation (每 cciStreamCtxCheckInterval 點檢查)
- NaN/Inf cell → 空字串
- 整 row 全 NaN/Inf → skip + droppedRowCount log
- atomic tmp+rename write

同時補上 ADR-0001 既有 invariant: SanitizePath / pathValidator 守門,
原 cci 內 csvutil call 缺的 defense-in-depth 一併齊。

Plan: docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md (Task 2)
EOF
)"
```

---

## Task 3: 把 gui/cci_handlers.go 切換到 WriteCCIResult，刪除 cci.ExportToCSV + writeCSVFile

**Why this task matters:** 收掉 `// Candidate 2 TODO` 那條 source-code comment（`gui/cci_handlers.go:87-91`），closure 換成呼 CSVHandler。Dead code 即刻清除以免之後 architecture review 再被當 leak 提報。

**Files:**
- Modify: `gui/cci_handlers.go:87-103`（WriteCSV closure 切換）
- Modify: `internal/cci/analyzer.go:533-664`（刪除 ExportToCSV + writeCSVFile）
- Delete: `internal/cci/exporttocsv_test.go`（測試覆蓋已轉移）

- [ ] **Step 3.1: 切換 cci_handlers.go closure**

`gui/cci_handlers.go:87-103` 整段改寫：

```go
// before
// WriteCSV 暫保留 cciAnalyzer.ExportToCSV — Candidate 2 推進時把 closure
// 內容換成 csvHandler.WriteCCIResult(...) 即可，handler 本身不再動。
// `*io.CSVHandler` 參數此版本還用不到（走 cciAnalyzer 內部 export），
// 是 Candidate 2 前的暫保留設計。outputDir 透過 closure capture caller
// 端的 local var（state.Load 已在 Run 外做）。
WriteCSV: func(_ *io.CSVHandler, analysisResult *cci.CCIAnalysisResult) (string, error) {
    csvPath, exportErr := a.cciAnalyzer.ExportToCSV(a.context(), analysisResult, outputDir)
    if exportErr != nil {
        return "", newUIError(ErrCCICSVExportFailed,
            fmt.Sprintf("CSV 導出失敗: %s", redact.RedactForMessage(exportErr)))
    }
    return csvPath, nil
},

// after
// WriteCSV: ADR-0004 Boundary 2 — Subject-based write,CSVHandler 內部從
// analysisResult.Subject 推導 filename;req.Filename 被忽略。SubDir 用 ""
// (寫到 OutputDir 根)。outputDir capture 由 CSVHandler 自身的 h.config.OutputDir
// 替代(state.Load 在 Run 外做時 csvHandler 已綁定當時 config)。
WriteCSV: func(handler *io.CSVHandler, analysisResult *cci.CCIAnalysisResult) (string, error) {
    csvPath, exportErr := handler.WriteCCIResult(a.context(), io.WriteRequest{}, analysisResult)
    if exportErr != nil {
        return "", newUIError(ErrCCICSVExportFailed,
            fmt.Sprintf("CSV 導出失敗: %s", redact.RedactForMessage(exportErr)))
    }
    return csvPath, nil
},
```

注意：`outputDir` 這條 local var 在 closure 內不再用 — 檢查 `gui/cci_handlers.go:55-58` 附近若 outputDir 只被本 closure 使用，順手刪掉 declaration；若還有別處用就留著。

- [ ] **Step 3.2: 跑既有 cci handler integration test 確認 GREEN**

Run:
```bash
go test ./gui/... -run 'TestAnalyzeCCI' -v -count=1
```

Expected: 全 PASS — closure 內部換實作但對外契約（CCIResult.OutputCSVPath, Subject, PairNames 等）不變。

- [ ] **Step 3.3: 刪除 cci.ExportToCSV + writeCSVFile**

`internal/cci/analyzer.go:533-664` 整段刪除（包含 godoc）。確認 import 中若 `csvutil`、`logging`、`math` 等只被被刪函式使用，順手刪 import（Go compiler 會提示 unused import error）。

- [ ] **Step 3.4: 跑 cci 內部 test 確認 GREEN**

Run:
```bash
go test ./internal/cci/... -v -count=1
```

Expected: 全 PASS（ExportToCSV 相關 test 已刪 → 應該沒 reference 留著）。若 fail，看是否有 test 還 reference ExportToCSV — 對應刪掉那 test 或改測 `csv_handler.WriteCCIResult` 那邊。

- [ ] **Step 3.5: 刪除 cci/exporttocsv_test.go**

```bash
git rm internal/cci/exporttocsv_test.go
```

該檔覆蓋的測試（subject sanitize / NaN row skip / ctx cancel）已在 `csv_handler_format_aware_test.go` 重新覆蓋。

- [ ] **Step 3.6: 跑全套 test 確認 GREEN**

Run:
```bash
go test ./... -count=1
```

Expected: 全 PASS。

- [ ] **Step 3.7: Commit**

```bash
git add gui/cci_handlers.go internal/cci/analyzer.go
git rm internal/cci/exporttocsv_test.go
git commit -m "$(cat <<'EOF'
refactor(cci): 切換 cci_handlers.go WriteCSV closure 至 handler.WriteCCIResult,刪除 ExportToCSV + writeCSVFile

收掉 gui/cci_handlers.go:87 那條 "// Candidate 2 TODO" comment 與 cci_handlers.go
內部對 cciAnalyzer.ExportToCSV 的 fallback path。closure 改呼 CSVHandler.WriteCCIResult,
outputPath / err 契約不變。

- 刪除 internal/cci/analyzer.go ExportToCSV + writeCSVFile (~130 行)
- 刪除 internal/cci/exporttocsv_test.go (覆蓋轉移到 csv_handler_format_aware_test.go)
- gui/cci_handlers.go: closure 改吃 handler arg (本 commit 前是 _),outputDir local var 順手清

Plan: docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md (Task 3)
EOF
)"
```

---

## Task 4: 新增 WriteMuscleRatioOutputAll + WriteMuscleRatioOutputPhases 與 payload struct

**Why this task matters:** ADR-0004 Boundary 1 — 拆兩個 method 每個只負責一個檔的 row layout（不是合 single method 內部 sticky）。Payload struct 定義在 io 套件（避免 muscle_ratio → io 與 io → muscle_ratio 撞 cycle）。

**Files:**
- Modify: `internal/io/csv_handler.go` 末尾（append payload struct + 2 methods）
- Modify: `internal/io/csv_handler_format_aware_test.go` 末尾（append MR tests）

- [ ] **Step 4.1: 寫 failing test**

`internal/io/csv_handler_format_aware_test.go` append：

```go
// TestWriteMuscleRatioOutputAll_RoundTrip 驗證 Output 1 (per-subject full
// time-series ratio) 的 round-trip + filename derivation.
func TestWriteMuscleRatioOutputAll_RoundTrip(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    payload := io.MuscleRatioOutputAllPayload{
        Subject:    "s1",
        PairLabels: []string{"R1", "R2"},
        Times:      []float64{0.0, 0.5, 1.0},
        Ratios:     [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
    }

    outputPath, err := handler.WriteMuscleRatioOutputAll(io.WriteRequest{}, payload)
    require.NoError(t, err)
    assert.Equal(t, filepath.Join(outputDir, "s1_muscle_ratio.csv"), outputPath)

    rows := readCSV(t, outputPath)
    require.Len(t, rows, 4)
    assert.Equal(t, []string{"Time (s)", "R1", "R2"}, rows[0])
    assert.Equal(t, []string{"0.0000", "0.100000", "0.400000"}, rows[1])
}

// TestWriteMuscleRatioOutputPhases_RoundTrip 驗證 Output 2 (per-subject phase
// + midpoint slice) 的 round-trip.
func TestWriteMuscleRatioOutputPhases_RoundTrip(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    payload := io.MuscleRatioOutputPhasesPayload{
        Subject:    "s1",
        PairLabels: []string{"R1"},
        Times:      []float64{0.0, 0.5, 1.0},
        Ratios:     [][]float64{{0.1, 0.2, 0.3}},
        Points: []io.MuscleRatioPhasePoint{
            {Name: "P1", Time: 0.5},
        },
    }

    outputPath, err := handler.WriteMuscleRatioOutputPhases(io.WriteRequest{}, payload)
    require.NoError(t, err)
    assert.Equal(t, filepath.Join(outputDir, "s1_muscle_ratio_phases.csv"), outputPath)

    rows := readCSV(t, outputPath)
    require.Len(t, rows, 2)
    assert.Equal(t, []string{"Phase", "Time (s)", "R1"}, rows[0])
    assert.Equal(t, []string{"P1", "0.5000", "0.200000"}, rows[1])
}

// TestWriteMuscleRatioOutputAll_NaNInfCell 驗證 NaN/Inf cell → 空字串.
func TestWriteMuscleRatioOutputAll_NaNInfCell(t *testing.T) {
    outputDir := t.TempDir()
    handler := newTestCSVHandler(t, outputDir)

    payload := io.MuscleRatioOutputAllPayload{
        Subject:    "s2",
        PairLabels: []string{"R1"},
        Times:      []float64{0.0, 1.0},
        Ratios:     [][]float64{{math.NaN(), math.Inf(1)}},
    }

    outputPath, err := handler.WriteMuscleRatioOutputAll(io.WriteRequest{}, payload)
    require.NoError(t, err)

    rows := readCSV(t, outputPath)
    require.Len(t, rows, 3)
    assert.Equal(t, []string{"0.0000", ""}, rows[1])
    assert.Equal(t, []string{"1.0000", ""}, rows[2])
}
```

- [ ] **Step 4.2: 跑測試確認 RED**

Run:
```bash
go test ./internal/io/... -run 'TestWriteMuscleRatio' -v
```

Expected: 編譯失敗 — `io.MuscleRatioOutputAllPayload undefined`。

- [ ] **Step 4.3: 實作 payload struct + 2 methods**

`internal/io/csv_handler.go` 末尾 append：

```go
// MuscleRatioOutputAllPayload 是 WriteMuscleRatioOutputAll 的輸入承載結構。
//
// 把 muscle_ratio.Analyzer.analyzeSubject 內部計算出的 ratios 與時間軸打包傳給
// CSVHandler;Subject 給 filename derivation 用,PairLabels 是 ratio pair 的
// header 名稱 (對齊 muscle_ratio.DefaultRatios() Name 欄位)。
type MuscleRatioOutputAllPayload struct {
    Subject    string
    PairLabels []string
    Times      []float64
    Ratios     [][]float64 // 每個 pair 一個 inner slice,長度與 Times 對齊
}

// MuscleRatioOutputPhasesPayload 是 WriteMuscleRatioOutputPhases 的輸入承載。
//
// Points 與 Ratios 由 muscle_ratio.Analyzer 預先 collectPhasePoints 計算得;
// CSVHandler 只負責照 Point.Time 找 Ratios 切片的對應 cell 並 emit row。
type MuscleRatioOutputPhasesPayload struct {
    Subject    string
    PairLabels []string
    Times      []float64
    Ratios     [][]float64
    Points     []MuscleRatioPhasePoint
}

// MuscleRatioPhasePoint 是 muscle_ratio.Analyzer 在 collectPhasePoints 階段
// 算出的 phase / midpoint 條目,Name 為顯示名稱,Time 為 EMG-time-aligned 時間值。
type MuscleRatioPhasePoint struct {
    Name string
    Time float64
}

// WriteMuscleRatioOutputAll 寫 per-subject Output 1 — full time-series ratio CSV。
//
// Filename 由 Subject 經 SanitizeFileName 後 + "_muscle_ratio.csv" 推導;
// req.Filename 被忽略,僅 req.SubDir 生效。
//
// row layout: 1 header ["Time (s)", PairLabels...] + N data rows。
// NaN/Inf cell → 空字串。
func (h *CSVHandler) WriteMuscleRatioOutputAll(
    req WriteRequest, p MuscleRatioOutputAllPayload,
) (string, error) {
    if len(p.Times) == 0 {
        return "", errEmptyMuscleRatioPayload
    }

    safeSubject := calculator.SanitizeFileName(p.Subject)
    filename := fmt.Sprintf("%s_muscle_ratio.csv", safeSubject)
    outputPath := filepath.Join(h.config.OutputDir, req.SubDir, filename)

    if err := h.validateOutputDir(req.SubDir); err != nil {
        return "", err
    }

    header := make([]string, 0, 1+len(p.PairLabels))
    header = append(header, "Time (s)")
    header = append(header, p.PairLabels...)

    err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
        Header: header,
        Emit: func(emit func([]string) error) error {
            for i, t := range p.Times {
                row := make([]string, 0, 1+len(p.Ratios))
                row = append(row, fmt.Sprintf("%.4f", t))
                for k := range p.Ratios {
                    row = append(row, formatMuscleRatioCell(p.Ratios[k], i))
                }
                if err := emit(row); err != nil {
                    return err
                }
            }
            return nil
        },
    })
    if err != nil {
        return "", err
    }
    return outputPath, nil
}

// WriteMuscleRatioOutputPhases 寫 per-subject Output 2 — phase+midpoint slice CSV。
//
// Filename 由 Subject 推導 + "_muscle_ratio_phases.csv";req.Filename 被忽略。
// Row 數量由 caller (muscle_ratio.Analyzer) 預先決定的 Points 切片長度決定。
func (h *CSVHandler) WriteMuscleRatioOutputPhases(
    req WriteRequest, p MuscleRatioOutputPhasesPayload,
) (string, error) {
    if len(p.Points) == 0 {
        return "", errEmptyMuscleRatioPayload
    }

    safeSubject := calculator.SanitizeFileName(p.Subject)
    filename := fmt.Sprintf("%s_muscle_ratio_phases.csv", safeSubject)
    outputPath := filepath.Join(h.config.OutputDir, req.SubDir, filename)

    if err := h.validateOutputDir(req.SubDir); err != nil {
        return "", err
    }

    header := make([]string, 0, 2+len(p.PairLabels))
    header = append(header, "Phase", "Time (s)")
    header = append(header, p.PairLabels...)

    err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
        Header: header,
        Emit: func(emit func([]string) error) error {
            for _, point := range p.Points {
                idx := nearestTimeIndex(p.Times, point.Time)
                row := make([]string, 0, 2+len(p.Ratios))
                row = append(row, point.Name, fmt.Sprintf("%.4f", p.Times[idx]))
                for k := range p.Ratios {
                    row = append(row, formatMuscleRatioCell(p.Ratios[k], idx))
                }
                if err := emit(row); err != nil {
                    return err
                }
            }
            return nil
        },
    })
    if err != nil {
        return "", err
    }
    return outputPath, nil
}

// formatMuscleRatioCell formats one ratio value (與 cci.ExportToCSV row-format
// 同款規則,NaN/Inf → 空字串、否則 %.6f)。
func formatMuscleRatioCell(values []float64, idx int) string {
    if idx < 0 || idx >= len(values) {
        return ""
    }
    v := values[idx]
    if math.IsNaN(v) || math.IsInf(v, 0) {
        return ""
    }
    return fmt.Sprintf("%.6f", v)
}

// nearestTimeIndex 是 muscle_ratio.Analyzer 內 synchronizer.FindNearestTimeIndex
// 的純函式 mirror — 取出最接近 target 的 times slice index。time series 假設
// 已 ascending sorted (muscle_ratio.Analyzer 上游已保證)。
func nearestTimeIndex(times []float64, target float64) int {
    if len(times) == 0 {
        return 0
    }
    best := 0
    bestDelta := math.Abs(times[0] - target)
    for i, t := range times[1:] {
        delta := math.Abs(t - target)
        if delta < bestDelta {
            best = i + 1
            bestDelta = delta
        }
    }
    return best
}

// validateOutputDir 是 muscle_ratio / CCI 兩條 write path 共用的 defense-in-depth
// 守門 — 對齊既有 muscle_ratio.Analyzer 內 ValidateExternalPath 的位置。
func (h *CSVHandler) validateOutputDir(subDir string) error {
    checkPath := filepath.Join(h.config.OutputDir, subDir, "_validation_marker")
    if err := h.pathValidator.ValidateExternalPath(checkPath); err != nil {
        return fmt.Errorf("輸出路徑無效: %w", err)
    }
    if err := os.MkdirAll(filepath.Dir(checkPath), fsperm.DirPerm); err != nil {
        return fmt.Errorf("輸出目錄建立失敗: %w", err)
    }
    return nil
}

var errEmptyMuscleRatioPayload = stderrors.New("WriteMuscleRatio*: payload 缺 Times/Points")
```

注意 `nearestTimeIndex` 跟 `synchronizer.FindNearestTimeIndex` 等價 — 若 synchronizer 已 export 且 io 套件方便 import，就直接用 `synchronizer.FindNearestTimeIndex(p.Times, point.Time)`，刪掉本地 helper。本 plan 預設不 import 以免動 io 套件 dep graph 過大；reviewer 可酌情調整。

- [ ] **Step 4.4: 跑測試確認 GREEN**

Run:
```bash
go test ./internal/io/... -run 'TestWriteMuscleRatio' -v
```

Expected: 3 個 sub-test 全 PASS。

- [ ] **Step 4.5: Commit**

```bash
git add internal/io/csv_handler.go internal/io/csv_handler_format_aware_test.go
git commit -m "$(cat <<'EOF'
feat(io): WriteMuscleRatioOutputAll/OutputPhases — CSVHandler 吸 MR row layout

ADR-0004 Boundary 1: 拆兩 method 各自負責 Output 1 / Output 2 row layout,
sticky-success 規則仍留在 muscle_ratio.Analyzer (本 task 不動 Analyzer)。
ADR-0004 Boundary 2: Subject-based,filename 由 Subject 內部推導。

- MuscleRatioOutputAllPayload / MuscleRatioOutputPhasesPayload / MuscleRatioPhasePoint
  payload struct 全部 in io 套件 (避免 muscle_ratio → io vs io → muscle_ratio cycle)
- nearestTimeIndex 本地 mirror synchronizer.FindNearestTimeIndex 純函式
- validateOutputDir helper 對齊 muscle_ratio.Analyzer 既有 defense-in-depth
- formatMuscleRatioCell NaN/Inf → 空字串

Plan: docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md (Task 4)
EOF
)"
```

---

## Task 5: muscle_ratio.Analyzer 改呼 CSVHandler，刪除 writeOutputAll/writeOutputPhases

**Why this task matters:** ADR-0004 Boundary 1 落實 — Analyzer 持有 sticky-success 規則與 collectPhasePoints warn-path，row layout 透過 CSVHandler 兩個新 method 落實。`gui/muscle_ratio_handlers.go` 的 `WriteCSV: nil` 維持（ADR-0004 Boundary 3），但 comment 從「TODO」改為「設計如此」。

**Files:**
- Modify: `internal/muscle_ratio/analyzer.go:28-33`（Params 加 CSVHandler 欄位）
- Modify: `internal/muscle_ratio/analyzer.go:180-224`（analyzeSubject 改呼 CSVHandler）
- Modify: `internal/muscle_ratio/analyzer.go:446-505`（刪除 writeOutputAll/writeOutputPhases）
- Modify: `gui/muscle_ratio_handlers.go:80-110`（Params 多塞 CSVHandler；comment 改文案）
- Delete: `internal/muscle_ratio/writer_atomic_test.go`（覆蓋轉移）

- [ ] **Step 5.1: 改 muscle_ratio.Params 加 CSVHandler 欄位**

`internal/muscle_ratio/analyzer.go:28-33`：

```go
// before
type Params struct {
    ManifestFile string
    DataFolder   string
    OutputDir    string
}

// after
type Params struct {
    ManifestFile string
    DataFolder   string
    OutputDir    string
    CSVHandler   *io.CSVHandler // ADR-0004 Boundary 1: row layout 透過 CSVHandler 落實
}
```

import 加 `"count_mean/internal/io"` — 這是新增的 muscle_ratio → io dep 方向（io 套件不會 import muscle_ratio，所以無 cycle 風險）。

- [ ] **Step 5.2: 改 analyzeSubject 呼叫位置**

`internal/muscle_ratio/analyzer.go:195-220` 那段 writeOutputAll / writeOutputPhases 整段改：

```go
// before
outAllPath := filepath.Join(outputDir, fmt.Sprintf("%s_muscle_ratio.csv", safeSubject))
if err := writeOutputAll(outAllPath, emg.Time, ratiosAll); err != nil {
    result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput1Failed, err)
    return result
}

result.OutputAllPath = outAllPath

// Output 2 — phases + midpoints
points, warn := a.collectPhasePoints(m, emg)
if warn != "" {
    result.Success = true
    result.Error = warn
    return result
}

outPhasePath := filepath.Join(outputDir, fmt.Sprintf("%s_muscle_ratio_phases.csv", safeSubject))
if err := writeOutputPhases(outPhasePath, emg.Time, ratiosAll, points); err != nil {
    result.Success = true
    result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput2Failed, err)
    return result
}

result.OutputPhasePath = outPhasePath
result.Success = true

// after
pairLabels := defaultRatioLabels()

outAllPath, writeAllErr := params.CSVHandler.WriteMuscleRatioOutputAll(
    io.WriteRequest{},
    io.MuscleRatioOutputAllPayload{
        Subject:    m.Subject,
        PairLabels: pairLabels,
        Times:      emg.Time,
        Ratios:     ratiosAll,
    },
)
if writeAllErr != nil {
    result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput1Failed, writeAllErr)
    return result
}
result.OutputAllPath = outAllPath

// Output 2 — phases + midpoints (ADR-0004 Boundary 1: sticky-success 規則留在
// Analyzer。collectPhasePoints warn-path 與 Output 2 寫檔失敗都讓 Output 1
// 視為 sticky-success)。
points, warn := a.collectPhasePoints(m, emg)
if warn != "" {
    result.Success = true
    result.Error = warn
    return result
}

ioPhasePoints := make([]io.MuscleRatioPhasePoint, len(points))
for i, p := range points {
    ioPhasePoints[i] = io.MuscleRatioPhasePoint{Name: p.name, Time: p.time}
}

outPhasePath, writePhaseErr := params.CSVHandler.WriteMuscleRatioOutputPhases(
    io.WriteRequest{},
    io.MuscleRatioOutputPhasesPayload{
        Subject:    m.Subject,
        PairLabels: pairLabels,
        Times:      emg.Time,
        Ratios:     ratiosAll,
        Points:     ioPhasePoints,
    },
)
if writePhaseErr != nil {
    result.Success = true
    result.Error = i18n.T(i18n.KeyErrorMuscleRatioSubjectWriteOutput2Failed, writePhaseErr)
    return result
}

result.OutputPhasePath = outPhasePath
result.Success = true
```

注意 `defaultRatioLabels()` 是 helper 抽出 `DefaultRatios()` 的 `.Name` slice — 加在 analyzer.go 末尾：

```go
// defaultRatioLabels 提取 DefaultRatios() 的 pair name slice,供 CSVHandler payload 用。
func defaultRatioLabels() []string {
    pairs := DefaultRatios()
    labels := make([]string, len(pairs))
    for i, r := range pairs {
        labels[i] = r.Name
    }
    return labels
}
```

`outputDir` local var 若只在被刪段落用，順手清除。

- [ ] **Step 5.3: 刪除 writeOutputAll/writeOutputPhases/formatRatioCell**

`internal/muscle_ratio/analyzer.go:446-525` 區段（包含三個函式）整段刪除。

- [ ] **Step 5.4: 改 gui/muscle_ratio_handlers.go — 灌 CSVHandler 進 Params + 改 comment**

`gui/muscle_ratio_handlers.go:87-110` 那段：

```go
// before
subjectResults, analyzeErr := a.muscleRatioAnalyzer.Analyze(ctx, &muscle_ratio.Params{
    ManifestFile: p.ManifestFile,
    DataFolder:   p.DataFolder,
    OutputDir:    outputDir,
})

// after
subjectResults, analyzeErr := a.muscleRatioAnalyzer.Analyze(ctx, &muscle_ratio.Params{
    ManifestFile: p.ManifestFile,
    DataFolder:   p.DataFolder,
    OutputDir:    outputDir,
    CSVHandler:   s.csvHandler,
})
```

並改 `WriteCSV: nil` 上方 comment（`:105-110` 區段）：

```go
// before
// WriteCSV = nil:per-subject CSV write 摺在 muscle_ratio.Analyzer 內
// (batch 變體)。樣板看到 nil 後 outputPath 回空字串、跳過寫檔步驟;
// per-subject 寫檔的 outputPath 由 analyzer 自己回填到
// SubjectResult.OutputAllPath / OutputPhasePath。Candidate 2 推進時
// 把 per-subject write 上移到 csvHandler 後,closure 從 nil 補回實作。
WriteCSV: nil,

// after
// WriteCSV = nil:batch unit-of-work 不適用 single-path closure (ADR-0004 Boundary 3)。
// per-subject row layout 已透過 muscle_ratio.Analyzer 內呼叫 csvHandler.WriteMuscleRatioOutputAll
// 與 WriteMuscleRatioOutputPhases 落實;outputPath 由 analyzer 回填到
// SubjectResult.OutputAllPath / OutputPhasePath。
// 詳見 docs/adr/0004-format-aware-write-collapse-boundaries.md。
WriteCSV: nil,
```

- [ ] **Step 5.5: 跑 muscle_ratio 全測試確認 GREEN**

Run:
```bash
go test ./internal/muscle_ratio/... ./gui/... -run 'MuscleRatio' -v -count=1
```

Expected: 全 PASS。若 `internal/muscle_ratio/analyzer_test.go` 有 test 自己建 Params 並呼叫 Analyze，要記得加上 CSVHandler 欄位（用 test helper 建一個或 reuse newTestCSVHandler）。

- [ ] **Step 5.6: 刪除 writer_atomic_test.go**

該檔內既有 4 個 test (`TestWriteOutputAll_LegacyStaleTmpNoLongerBlocks`, `TestWriteOutputAll_NaNAndInfCellsWrittenAsEmpty`, `TestWriteOutputPhases_LegacyStaleTmpNoLongerBlocks`, 等) 全部 reference 已刪函式。覆蓋範圍已轉移到 Task 4 的 csv_handler_format_aware_test.go MR tests。

```bash
git rm internal/muscle_ratio/writer_atomic_test.go
```

如 reviewer 認為某條 legacy 行為（例如 stale tmp 清理）值得保留 — 在 csv_handler_format_aware_test.go 補測。

- [ ] **Step 5.7: 跑全套確認 GREEN**

Run:
```bash
go test ./... -count=1
```

Expected: 全 PASS。

- [ ] **Step 5.8: Commit**

```bash
git add internal/muscle_ratio/analyzer.go gui/muscle_ratio_handlers.go
git rm internal/muscle_ratio/writer_atomic_test.go
git commit -m "$(cat <<'EOF'
refactor(muscle_ratio): Analyzer 改呼 CSVHandler.WriteMuscleRatioOutput* — ADR-0004 落實

ADR-0004 Boundary 1: row layout 搬進 CSVHandler,sticky-success 規則 (Output 1
保留 + Output 2 跳過 / 寫檔失敗 → result.Success=true) 仍留在 Analyzer。
ADR-0004 Boundary 3: gui/muscle_ratio_handlers.go WriteCSV: nil 維持,
comment 從 "Candidate 2 TODO" 改寫為 "設計如此" narrative。

- muscle_ratio.Params 加 CSVHandler *io.CSVHandler 欄位
- analyzeSubject 改呼 csvHandler.WriteMuscleRatioOutputAll/OutputPhases
- 刪除 writeOutputAll/writeOutputPhases/formatRatioCell (~80 行)
- 刪除 internal/muscle_ratio/writer_atomic_test.go (覆蓋轉移到 io 套件)
- gui/muscle_ratio_handlers.go 灌 csvHandler 進 Params

Plan: docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md (Task 5)
EOF
)"
```

---

## Task 6: 全套驗證 + lint + (option) PR-ready

**Why this task matters:** 把累積五個 commits 的成果統一驗證 — `make ci-fast` 跑 test-unit, bench-std, coverage-check, lint。任何 regression 在這時揪出。

- [ ] **Step 6.1: 跑 make ci-fast**

Run:
```bash
make ci-fast
```

Expected: 全綠。若 fail：
- `coverage-check` fail：可能新 method 沒打到 90% 覆蓋線 — 補測在 csv_handler_format_aware_test.go。
- `lint` fail：通常是 unused import / unused var / cyclic complexity — 對應修。
- `test-unit` fail：通常是漏改 caller 的測試 — 找出 callsite 加 `_, err :=` 或 `path, err :=`。

- [ ] **Step 6.2: 跑 race detection**

Run:
```bash
make test-race
```

Expected: race-free。CCI streaming Emit 是單 goroutine，MR loop 也是單 goroutine，理論上不會踩 race。

- [ ] **Step 6.3: 跑 integration test 確認 end-to-end 不退**

Run:
```bash
make test-int
```

Expected: 全 PASS。`test/integration/` 內若有 fixture-based CCI / MR 整合 test，會驗到 outputPath / file content 都對。

- [ ] **Step 6.4: (option) 用真實 manifest fixture 跑一次 GUI smoke**

如果 grilling 時的 ADR-0004 narrative 還沒入心，建議跑一次 GUI smoke 驗 CCI / MR 兩條 path 的 outputPath 仍正確回給前端：

Run:
```bash
go run main.go
# 手動 trigger CCI 分析 (任一 manifest fixture) 與 MR 分析,確認 OutputCSVPath / OutputAllPath / OutputPhasePath 都顯示且檔案存在
```

Expected: 兩個 panel 都跑成功，輸出檔現身在 OutputDir。

如果你不想跑 GUI（或 `Wails frontend dist rebuild trap` 那條 memory feedback 適用），可以略過此 step — make ci-fast 通過已涵蓋 unit + integration。

- [ ] **Step 6.5: Final tidy commit (如果有額外 lint fix 或 docstring 修)**

```bash
git status
git diff
# 若有未 commit 的 lint fix
git add -p
git commit -m "chore(io): lint cleanup post-collapse"
```

- [ ] **Step 6.6: 開 PR**

```bash
gh pr create --title "refactor(io): Candidate 1 — format-aware write contract collapse" --body "$(cat <<'EOF'
## Summary

- 收乾 ADR-0001 的延續工程：4 個既有 method 統一 `(outputPath, error)` shape + 新增 `WriteCCIResult` / `WriteMuscleRatioOutputAll` / `WriteMuscleRatioOutputPhases`
- 落實 ADR-0004 三條 sticky boundary：muscle_ratio sticky-success 留在 Analyzer、filename ownership 沿 unit-of-work 形狀分、`AnalysisHandler[P, R].WriteCSV` closure 簽章不變（MR nil 是設計如此）
- 刪除 `internal/cci/analyzer.go` ExportToCSV (~130 行) 與 `internal/muscle_ratio/analyzer.go` writeOutputAll/writeOutputPhases (~80 行)

## Architecture narrative

注意：本 PR **沒有**把 4 個 family member 的 closure shape 收成同一形 — `WriteCSV: nil` 在 muscle_ratio 是 semantic-correct 的設計（batch unit-of-work），不是未收乾的 TODO。HTML report 的「after · one contract」圖視覺上整齊四欄是粗略表達；本 PR 落實的是「row layout invariant 100% 覆蓋」而非「closure shape 100% 統一」。詳見 `docs/adr/0004-format-aware-write-collapse-boundaries.md`。

## Out of scope（追在 #21）

family err channel 三種 philosophy 並存（CCI/MR 走 failedResult+nil，PhaseAnalysis 走 nil+err，PhaseSync 走 sentinel）— 獨立 follow-up 處理。

## Test plan

- [x] `make ci-fast` 全綠
- [x] `make test-race` race-free
- [x] `make test-int` integration 全綠
- [ ] (optional) GUI smoke：CCI / MR / PhaseSync / PhaseAnalysis / MaxMean / Normalized 6 條 write path 都跑通

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Checklist

跑完所有 task 後 self-review 這幾條（plan 寫作者已自驗一輪）：

**1. Spec coverage:**
- ✓ Q1 (CCI 單檔) → Task 2/3
- ✓ Q2 (per-subject method, loop 在 Analyzer) → Task 4/5
- ✓ Q2.1 (兩 methods, sticky 留 Analyzer) → Task 4/5
- ✓ Q3 (Subject-based 內推, File-based 外給) → Task 1 (PhaseAnalysis 維持 caller-supplied)、Task 2 (CCI 內推)、Task 4 (MR 內推)
- ✓ Q4 (Headers 參數保留) → Task 1 (WriteMaxMean / WritePhaseAnalysis sig 不動 headers slot)
- ✓ Q5 (err channel out-of-scope) → Plan 序文明示 + PR description 重申 + issue #21 追

**2. Placeholder scan:** 無 "TBD" / "implement later" / 沒有「add appropriate error handling」泛述 — 每一步都有 exact code 或 exact command。

**3. Type consistency:**
- `MuscleRatioOutputAllPayload` / `MuscleRatioOutputPhasesPayload` / `MuscleRatioPhasePoint` 在 Task 4 定義、Task 5 消費 — 名稱一致
- `WriteCCIResult(ctx, req, *cci.CCIAnalysisResult) (string, error)` 在 Task 2 定義、Task 3 呼叫 — 簽章對齊
- `defaultRatioLabels()` 在 Task 5 新增於 muscle_ratio/analyzer.go — 範圍局部
- `cciStreamCtxCheckInterval` 在 Task 2 定義於 io/csv_handler.go (避免 cross-package 引 `cci.cciChartCtxCheckInterval` private constant)

---

## Execution Choice

**Plan complete and saved to `docs/superpowers/plans/2026-05-28-format-aware-write-collapse.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — 我每個 task 派一個 fresh subagent，task 之間我做 review 把關。對本 plan 特別合適，因為：
- 每個 task 有獨立的 commit boundary
- Task 2-5 涉及 io ↔ cci / io ↔ muscle_ratio 套件邊界改動，每 task 結束後我可以驗證 dep graph 還乾淨
- 6 個 task 預估累計 1.5–2 小時 wall time，subagent fan-out 不會省，但每 task 有 fresh context 比較不容易混淆 ADR-0001 vs ADR-0004 的 narrative

**2. Inline Execution** — 在當前 session 內以 executing-plans 跑，batch 執行 + checkpoint review。優點是省 context-switch；缺點是中間有 lint / test fail 時 debug 路徑會卡住 6 個 task 全部。

**Which approach?**
