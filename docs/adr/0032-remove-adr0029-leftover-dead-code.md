# 移除 ADR-0029 / b48a481 遺留死碼（validation / security / io）

**Status**: accepted · **implemented** (2026-10-09)

ADR-0029 與 commit `b48a481`（`InputValidator` facade 收合）之後，`validation` / `security` / `io` 仍留有「exported（或 `nolint:unused` 保留）但 production 零呼叫者」的符號。本 ADR 為架構重構 W1「清場」的第一部分，記錄這批純刪除（zero behaviour change）。本 ADR 依 area 分節；後續 W1 任務（parsers / calculator / models）以新增 `### Area N` 節的方式 append 於此（見 Area 4）。

## Decision

### Area 1. validation（`internal/validation`）

grep 全樹確認每個符號零 non-test caller 後移除，連同專屬測試：

- 刪 `internal/validation/interfaces.go` 與 `interfaces_test.go`：整檔 0 importer（`InjectionDetector` / `MutableFilenameValidator` 等 interface 在 `b48a481` 之後無人使用）。
- `patterns`：刪 `InjectionDetectorImpl.DetectAll`、`DetectMaliciousNumeric`、`NumericMalicious` 分類（含其 registry 初始化 pattern 表）、`NewInjectionDetectorWithRegistry`、`CommandInjectionWordTokens()`（`DetectCommand` 本來就直接遍歷包級 `commandInjectionWordTokens`）。「detector 遵守注入 registry」測試改以 struct literal 注入 registry，保留該覆蓋。
- `filename`：刪 `Validator.WithAllowedExtensions` / `GetAllowedExtensions`（唯一 caller 是已刪的 `interfaces_test.go`）。
- **保留** `csvutil.UnsanitizeCell`（後續任務會用）。

### Area 2. security（`internal/security`）

- `pathvalidator.go`：刪 `SetAllowedBasePaths`、`markFrozen` / `frozen` 欄位、`ErrValidatorFrozen`、`ErrAllowedBasePathsEmpty`、`filterTraversalElements`，以及 `PathValidator` 的 `sync.RWMutex`。`allowedBasePaths` 只在 `NewPathValidator` 設定，之後只讀；`GetAllowedBasePaths` 直接回傳副本、`ValidateFilePath` 直接讀欄位，不再加鎖。`DefaultValidator()` 不再 freeze（無 mutator 可凍結）。
- `fsperm`：刪 `isPathWithinAnyBase`（`matchAnyBase` 的 `nolint:unused` boolean wrapper）。
- 移除專屬測試：`ConcurrentSetAndValidate`、`TestDefaultValidator_IsImmutable`、`TestNewPathValidator_StillMutable`、`SetAllowedBasePaths_RejectsEmpty`；保留 `GetAllowedBasePathsReturnsCopy`。
- 修正描述已刪元件的註解（`csv_handler.go` 的 `GetAllowedBasePaths` 說明、`SanitizePath` 內提及 `filterTraversalElements` 處、`fsperm` 歷史說明改指 `matchAnyBase`、`docs/api.md` PathValidator 段）。

### Area 3. io（`CSVHandler`）

- 刪 `CSVHandler.ReadCSVFromInput`（`docs/usage_patterns.md` 範例改用 `ReadCSVFromDirectory(cfg.InputDir, fileName)`）。
- 刪 `CSVHandler.GetFileInfo` wrapper。**保留** `LargeFileHandler.GetFileInfo`（`CSVHandler` 內部與 streaming 路徑仍用；後續 wave 處理）。

### Area 4. parsers / calculator / models

grep 全樹（含 `gui/`、`test/`）確認 production 零呼叫者後移除，連同只測它們的測試：

- `parsers`：刪 `GetANCDataInTimeRange`、`ValidateForceData`、`GetMotionDataAtIndex`、`GetMotionDataInIndexRange`、`FindIndexRangeIndices`、`ValidateMotionData`；刪 `MotionParser.IndexToTime` / `TimeToIndex` / `GetSampleInterval` 與 `ANCParser.GetSampleInterval`。
- 被上述連帶孤立的符號一併移除：`MotionParser.frequency` 欄位；`ANCParser.frequency` 欄位及三處 `computeFrequencyFromTime` 賦值（`ANCParser` 變為無狀態 `struct{}`）；`ErrIndexRangeNotFound`、`RoundingOffset`。`ValidateTimeSeries` 仍被 EMG 驗證使用，保留。
- `parsers.ErrPhaseManifestNegativeTime`：宣告但從未回傳（`parseFloat` 明文允許負時間），刪除。
- `calculator.ValidateStatisticsParams` 及其 4 個 sentinel（`ErrNegativeStartTime` / `ErrNegativeEndTime` / `ErrStartTimeNotBeforeEnd` / `ErrEmptySubject`）：0 caller（含測試），刪除。
- `models.SyncTime`：僅被自己的測試引用，刪除。
- `DataParser.GetScalingFactor`：僅測試使用，刪除。
- **保留** `MaxMeanCalculator.ScalingFactor()`：雖僅測試使用，但 `gui/wails_binding_test.go` 的 `TestApplyConfig_RebuildsComponents` 與 `TestApp_SnapshotConsistency_UnderConcurrentApply` 靠它斷言「重建後的 calculator 吃進新 config」與「snapshot 不撕裂」；刪除會使這兩個回歸測試失去行為斷言。
- **不動**：`chart.ComposerInput` 的死欄位（W5）、synchronizer 的反向換算 `MotionIndexToTime` / `TimeToMotionIndex` / `MotionIndexToForceTime` / `ForceTimeToMotionIndex`（W4，與 phase timeline 重構一併處理）。

## Why

- 死碼會誤導 reviewer 以為存在「可變 allow-list」「凍結 singleton」等安全合約；實際 production 全程只在建構時設定 allow-list。移除 mutator 後，不可變性由結構保證，RWMutex 與 frozen flag 成為純成本。
- 這些符號多為 ADR-0029 / `b48a481` 移除 facade 後的孤兒，保留只靠 `nolint:unused` 或自身測試維持「有人用」的假象。
- 純刪除，行為不變；`filterTraversalElements` 的守門角色早已由 `HasTraversalElement` 與 `SanitizePath` 的 reject 契約取代。

## Considered Options

### A. 全部移除（chosen）

grep 證實零 production caller；專屬測試一併刪除，不留「只測死碼」的測試。

### B. 保留 `SetAllowedBasePaths` + frozen 機制作為 future-proof（rejected）

無任何 production 需求；保留需維持 RWMutex、兩段 frozen 檢查與 4 個測試，且與「allow-list 於建構時決定」的實際信任邊界重複。若未來真需要可變 allow-list，應以新建構 instance 表達（`NewPathValidator`）。

### C. 保留 `UnsanitizeCell`、`LargeFileHandler.GetFileInfo`（chosen exception）

前者後續任務使用；後者仍被 `CSVHandler` 內部呼叫，留待後續 wave 決定。

## Consequences

- `PathValidator` 變為建構後不可變、無鎖；並發讀取安全。
- `ErrValidatorFrozen` / `ErrAllowedBasePathsEmpty` 不再存在（無外部 `errors.Is` caller）。
- 使用者面 zh-TW 訊息文字不變。

## Related

- [[ADR-0029]]（numeric validator / synchronizer 死碼移除；本 ADR 清其後續遺留）
