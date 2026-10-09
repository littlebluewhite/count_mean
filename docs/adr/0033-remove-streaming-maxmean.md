# 刪除 streaming Max-mean — ADR-0011 moot 而非推翻

**Status**: accepted · **implemented** (2026-10-09)

`LargeFileHandler` 的 streaming 滑動窗口 Max-mean 路徑（`ProcessLargeFileInChunks` 及其 `slidingWindowState`、`processStreamingFile`、`executeStreamingLoop`）、`ProgressCallback` / `StreamingResult`、`BufferPool`、`MemoryStats`、`CSVHandler.ProcessLargeFile` 全部刪除。本 ADR 為架構重構 W1「清場」的一部分，記錄此決策，並說明它與 [[ADR-0011]] 的關係。

## Decision

- 刪 `internal/io/large_file_handler.go` 中 streaming 區段（約 L330–1062）：`streamingContext` / `recordProcessor` / `processStreamingFile` / `executeStreamingLoop` / 進度回報 / `slidingWindowState` 全套 / `ProcessLargeFileInChunks(WithContext)` / `GetBufferPoolStats` / `GetMemoryStats` / `ResetBufferPool` / 第二份 `parseDataRow`，以及僅服務它們的欄位（`chunkSize`、`memoryLimit`、`bufferPool`）、`errDataRowTooShort`、`closeFileWithLog`。
- 刪 `internal/io/buffer_pool.go`、`memory_stats.go` 與對應測試；刪 streaming 專屬測試（`large_file_handler_streaming_test.go`、`large_file_handler_p2_j_test.go`，以及 `large_file_handler_test.go` 中只測 streaming 的案例）。
- 刪 benchmark：`internal/benchmark` 的 `BenchmarkLargeFileProcessing`、`test/benchmark` 的 `BenchmarkLargeFileHandler_SlidingWindow` / `BenchmarkLargeFileProcessing`（含僅供它們使用的 CSV 產生 helper）。
- 刪 orphan i18n key `KeyErrorFileTooLarge`（常數、四個語系 catalog、`test/demo/i18n_demo` 的用法）。
- **保留** `LargeFileHandler.GetFileInfo` / `scanFileStructure` / `isLargeFileThreshold`：`CSVHandler.checkFileSizeAndFormat` 仍在讀取路徑上呼叫，留待 W3 處理。
- 超過 100 MB 的使用者訊息由「文件過大，請使用大文件處理功能」改為「檔案過大（上限 100 MB），請分割檔案後再試」（interim；讀取入口在後續 wave 定稿）。
- 同步更新 `README.md`、`docs/api.md`、`docs/usage_patterns.md`、`docs/testing_automation.md` 中描述 streaming / 大檔處理 / buffer pool 的段落。

## Why

- streaming 路徑**零 production caller**：GUI 與所有分析流程走 `CSVHandler.ReadCSV` + `MaxMeanCalculator`；`ProcessLargeFile` 只被 benchmark 呼叫。約 1200 行（含 ring buffer、遞迴校準、BufferPool、記憶體統計）只為維持一條無人使用的路徑。
- 該路徑還使「>100MB 請使用大文件處理功能」成為對使用者的誤導：GUI 根本沒有可用的大檔入口。
- 兩條路徑各自維護 Max-mean 語意（-Inf channel skip、non-finite row reset 等），是潛在的語意分叉來源；刪除後唯一實作是 `MaxMeanCalculator`。

## 與 ADR-0011 的關係

[[ADR-0011]]「維持 dual `parseDataRow`、不抽共用 kernel」的前提是兩份實作（`parsers.DataParser.parseDataRow` 與 `LargeFileHandler.parseDataRow`）並存。streaming 刪除後第二份消失，該決策變 **moot（失去適用對象）而非被推翻**：其論證（兩邊 allocation 策略 / tolerance policy 分歧，不值得抽 kernel）沒有被反駁，只是不再有第二個 caller。ADR-0011 不編輯，維持原文為歷史紀錄。

## Considered Options

### A. 刪除 streaming Max-mean（chosen）

零 production caller、無 GUI 入口，保留只增加維護面與語意分叉風險。

### B. 保留並接上 GUI 作為真正的大檔入口（rejected）

沒有需求：典型 EMG CSV 1–50 MB，100 MB 已是極端 outlier。若未來真需要，應以現行 `MaxMeanCalculator` 為核心重新設計 streaming 輸入，而非復活這份獨立實作。

### C. 只刪 benchmark、保留實作（rejected）

實作的唯一 caller 就是 benchmark；留下實作等於留下無測量的死碼。

## Consequences

- 超過 100 MB 的 CSV 一律被讀取路徑以 `ErrCodeFileTooLarge` 拒絕；使用者需先分割檔案。
- `test/benchmark` 與 `internal/benchmark` 仍保留其餘 benchmark，兩個 package 皆未被清空。
- 遺留（已於同一 wave 後續 commit 清理）：`validation/csv/csv_validator.go`（`ValidateRow` / `ValidateHeaderRow` 註解）、`parsers/emg_parser.go`、`util/str2number.go` 內提及 `processStreamingFile` / `executeStreamingLoop` / `large_file_handler` 為 caller 的過時註解已改寫；`LargeFileHandler.csvValidator` 欄位（streaming 刪除後無人讀取）已移除；`csv_handler.go` package doc、`docs/usage_patterns.md`「大文件處理模式」章節（改名「檔案大小限制」）與 `MaxMeanCalculator` worker pool / backpressure 描述、`README.md` 架構圖的 `BackpressureController` 同步修正。`KeyStatusLargeFileProc` i18n key 仍在，留待後續 wave。

## Related

- [[ADR-0011]]（dual parseDataRow；本 ADR 使其 moot）
- [[ADR-0032]]（同一 wave 的其他死碼清除）
