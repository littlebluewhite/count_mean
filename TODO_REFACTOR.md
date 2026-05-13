# TODO — 重構與改進清單

本清單由 2026-05-13 的多 agent code review（15 個 specialized agents 從 12 個切面 review 未 commit 改動）整理而成。每項都標註：

- **優先**：P1 應排程修 / P2 應追蹤 / P3 機會性改進
- **涉及範圍**：單檔 / 跨 package / 文件 / 測試
- **來源**：哪個 agent 提出
- **狀態**：`open`（待處理）/ `done`（已完成）/ `wontfix`（決定不修）

當前已修的高優先項見 git log；本檔只列「定義為待辦」的工作。

---

## P1 — 應排程修（影響正確性 / CI / 跨檔一致性）

### P1-A. CSV 寫入改 tmp+rename atomic write
- **狀態**：`open`
- **範圍**：cci + muscle_ratio + csvutil
- **來源**：senior-software-engineer（muscle_ratio batch error model）
- **位置**：
  - `internal/muscle_ratio/analyzer.go:171, 192`（writeOutputAll、writeOutputPhases）
  - `internal/cci/analyzer.go`（writeCSVFile）
- **問題**：目前 `os.OpenFile(path, O_TRUNC, ...)` 直接寫入，若 BOM / header / data row 任何階段中途失敗，磁碟上會留下「截斷且看似有效」的檔；下次 batch 跑時還會被當成既存舊輸出
- **修法草圖**：
  ```go
  tmp := outputPath + ".tmp"
  if err := writeTo(tmp); err != nil { os.Remove(tmp); return err }
  return os.Rename(tmp, outputPath)
  ```
  在 `internal/csvutil/` 新增 `SafeWriter` 包裝 OpenFile+BOM+SanitizeHeader+Flush+Rename，cci 與 muscle_ratio 都改 call
- **驗證**：注入「寫到一半失敗」的測試（os.Pipe + 提前 Close）

### P1-B. OutputDir traversal / jailbreak 驗證
- **狀態**：`open`
- **範圍**：config + cci + muscle_ratio
- **來源**：senior-software-engineer
- **位置**：
  - `gui/muscle_ratio_handlers.go:49`（`s.config.OutputDir` 直接傳入）
  - `internal/muscle_ratio/analyzer.go:101`（os.MkdirAll on user-controlled path）
  - `internal/cci/analyzer.go`（同樣 exposure）
- **問題**：`config.json` 可被編輯成 `"outputDir": "/etc"` 或 `"../../etc"`，`os.MkdirAll` + `O_NOFOLLOW`-only OpenFile 會在那寫 CSV
- **修法草圖**：在 `internal/config/` 載入時 validate 必為 `os.UserHomeDir()` / app data dir 子路徑，並拒絕含 `..` element

### P1-C. analyzer error 改回 `(nil, error)`（待決策）
- **狀態**：`open` — 需與 frontend 一起決定
- **範圍**：gui RPC contract
- **來源**：api-designer
- **位置**：`gui/muscle_ratio_handlers.go:52`
- **問題**：目前 `failedMuscleRatioResult(...)` 製造第三種失敗模式（`result.success=false` + `result.subjects=null`），caller 無法區分「pre-analysis fail」與「all subjects failed」
- **取捨**：knowledge-researcher 確認當前設計仍屬 Wails-idiomatic；改動會與 cci/normalized_phase_sync peer 慣例分歧。**先觀察使用者反饋再決定**
- **若決定改**：把 `analysisResult, err := a.muscleRatioAnalyzer.Analyze(...)` 的 err 直接 `return nil, fmt.Errorf("分析失敗: %w", err)`；JS try/catch 已能接

---

## P2 — 應追蹤（架構債、跨 package 重複、文件缺口）

### P2-A. 抽 `internal/csvutil/SafeWriter`（解 P1-A 的依賴）
- **狀態**：`open`
- **範圍**：跨 cci + muscle_ratio + 未來分析模組
- **來源**：refactor-specialist
- **證據**：`internal/cci/analyzer.go:writeCSVFile` 與 `internal/muscle_ratio/analyzer.go:writeOutputAll/writeOutputPhases` 共享 25+ 行 OpenFile + BOM + SanitizeHeaderRow + Flush + close-with-error-capture scaffolding。**已實際飄移**：cci 有 dropped-row warning logging，muscle_ratio 沒有
- **修法草圖**：
  ```go
  // internal/csvutil/safe_writer.go
  func WriteFile(path string, write func(*csv.Writer) error) (err error)
  func FormatFloatCell(v float64) string  // NaN/Inf → ""
  ```
- **連動**：完成後 P1-A 自然成為「在 SafeWriter 內加 tmp+rename」一行改動

### P2-B. 抽 `internal/manifest/loader.go`
- **狀態**：`open`
- **範圍**：cci + muscle_ratio + 未來分析模組
- **來源**：refactor-specialist
- **證據**：
  - `internal/cci/analyzer.go:103-148` 與 `internal/muscle_ratio/analyzer.go:83-99, 128-148` 共享 `ParseFile` → `EvalSymlinks` → `ResolveLenientPath` → `os.Stat IsNotExist` → `NewEMGParser().ParseFile` 序列
  - 路徑驗證已飄移一次（codex P1 修補時 cci 與 muscle_ratio 分別改）
- **修法草圖**：
  ```go
  // internal/manifest/loader.go
  func LoadManifests(file string) ([]models.PhaseManifest, error)
  func ResolveEMGFile(baseFolder, emgFile string) (resolvedPath string, err error)
  ```
  **不要** bundle 整個 loop / error 處理——cci 是 fail-fast，muscle_ratio 是 per-subject batch
- **連動**：完成後 `security.ResolveBaseFolder` (EvalSymlinks 三行 idiom) 可一併消失

### P2-C. EMGParser 變成 pure（消除 per-call new 模式）
- **狀態**：`open`
- **範圍**：parsers 套件
- **來源**：tech-lead-architect、senior-software-engineer、code-simplifier
- **位置**：`internal/parsers/emg_parser.go`，`ParseFile` 寫 `p.frequency`
- **問題**：兩個 analyzer 各自帶 5 行 `// per-call new EMGParser 因為 ParseFile 寫 frequency 欄位...` 註解。第 4、5 個分析模組來時，這個註解會繼續複製
- **修法草圖**：
  ```go
  // 方案 A：ParseFile 回傳 (data, frequency, error)，移除 instance field
  func (p *EMGParser) ParseFile(path string) (*PhaseSyncEMGData, float64, error)
  // 方案 B：接受 *ParseOpts，保持 parser stateless
  ```
- **影響**：兩處 analyzer 註解可移除，未來模組不需要再學這個 footgun

### P2-D. cci 的 `parsers.NewEMGParser().GetDataInTimeRange(...)` cargo-cult
- **狀態**：`open`
- **範圍**：parsers + cci
- **來源**：senior-software-engineer、code-simplifier
- **位置**：`internal/cci/analyzer.go:84`
- **問題**：`GetDataInTimeRange` 標 `unused-receiver`（純函式），為了「API 一致」每次 new 一個 parser instance 純粹當 noop receiver
- **修法草圖**：把 `GetDataInTimeRange` 提升為 package function `parsers.GetDataInTimeRange(emgData, start, end)`，cci 直接呼叫

### P2-E. ValidatePhaseManifest 死碼移除或啟用
- **狀態**：`open`
- **範圍**：parsers
- **來源**：senior-software-engineer
- **位置**：`internal/parsers/phase_manifest_parser.go:248`（函式存在但 `ParseFile` 沒呼叫）
- **問題**：空 Subject / 必填欄位欄空白等 validation 邏輯寫了但沒人 call，導致 muscle_ratio 必須在 analyzer 層自己擋（見已修的 empty-Subject case）
- **修法草圖**：在 `ParseFile` 內接到每筆 record 後立即 call `ValidatePhaseManifest`，failed records 收集為 error list 或 fail-fast（決策）

### P2-F. lenient_path 補 length cap
- **狀態**：`open`
- **範圍**：security
- **來源**：senior-software-engineer（CCI race fix）
- **位置**：`internal/security/lenient_path.go`
- **問題**：取代 `PathValidator.GetSafePath` 後丟掉了 `maxPathLength=4096` / `maxFilenameLength=255` 防護；超長 filename 不再在 path-validation 階段被拒
- **修法草圖**：
  ```go
  if len(filename) > 255 { return "", fmt.Errorf("檔名過長") }
  if len(joined) > 4096 { return "", fmt.Errorf("路徑過長") }
  ```
  位於 IsAbs 檢查之後、HasTraversalElement 之前

### P2-G. lenient_path 補 null byte / `.` / whitespace 守門
- **狀態**：`open`
- **範圍**：security
- **來源**：security-specialist
- **位置**：`internal/security/lenient_path.go:29-31, 37`
- **問題**：
  - `ResolveLenientPath(base, ".")` 回傳 `base` 本身（caller 後續 OpenFile 在 Unix 對目錄成功，行為未定）
  - `"  "`（whitespace）通過所有檢查
  - `"a\x00b.csv"` null byte 通過驗證；`os.OpenFile` 會拒，但 error path 把 byte 寫進 log
- **修法草圖**：
  ```go
  if strings.ContainsRune(normalized, 0) { return "", fmt.Errorf("null byte") }
  cleaned := filepath.Clean(strings.TrimSpace(normalized))
  if cleaned == "" || cleaned == "." { return "", fmt.Errorf("檔名無效") }
  ```

### P2-H. lenient_path TOCTOU 文件化
- **狀態**：`open`（可能 `wontfix`）
- **範圍**：security 文件
- **來源**：security-specialist
- **位置**：`internal/security/lenient_path.go:56-69`
- **問題**：`EvalSymlinks(joined)` 與後續 `os.OpenFile` 之間存在 TOCTOU window；attacker 在 baseFolder 中間目錄有寫權限時可 swap symlink
- **取捨**：Go 標準庫無 portable `openat2(RESOLVE_BENEATH)`；專案目標 macOS/Win/Linux 跨平台
- **修法**：先在 doc comment 文件化已知 limitation 與 threat model 假設（baseFolder 為 user-selected 可信路徑）；長期可考慮 platform-specific implementation 提升保證

### P2-I. cci vs muscle_ratio channel-map 對 L./R. 處理不對稱
- **狀態**：`open`（需 EMG 領域決策）
- **範圍**：cci + muscle_ratio
- **來源**：quality-assurance-manager
- **位置**：
  - `internal/cci/calculator.go:114-140` `MapHeaderToShortName` 把 R. 與 L. 都 strip → `L.RA` 與 `R.RA` 都映射成 `RA`（last-wins）
  - `internal/muscle_ratio/calculator.go:80-99` `BuildRightSideChannelMap` 僅接受 `R.*`
- **問題**：同份 EMG 在兩個 analyzer 得不同肌肉 assignment
- **修法草圖**：
  - 方案 A：CCI 限定 R. 對齊 muscle_ratio（領域決策）
  - 方案 B：保留 CCI 既有行為，在 doc comment 明白寫差異與適用場景
- **必須伴隨**：新增 deterministic L-vs-R-priority test

### P2-J. Manifest 契約集中文件化
- **狀態**：`open`
- **範圍**：models / parsers
- **來源**：tech-lead-architect
- **位置**：`internal/models/phase_sync_models.go`（`PhaseManifest` struct）
- **問題**：4 個 consumer (cci / phase_sync / normalized phase_sync / muscle_ratio) 共讀此 manifest，但「`0 == empty`」「`EMGMotionOffset` 語意」「`D` / `O` 是 motion index」等專案級不變式散在 4 處 comment 中
- **修法草圖**：在 `PhaseManifest` doc comment 上方加一段，列：(a) 欄位語意，(b) 0 即空的契約，(c) EMGMotionOffset 定義，(d) 目前 consumer 清單

### P2-K. 路徑驗證政策（lenient vs strict）的選擇規則
- **狀態**：`open`
- **範圍**：security 文件
- **來源**：tech-lead-architect
- **位置**：`internal/security/lenient_path.go` doc
- **問題**：cci / muscle_ratio 用 `ResolveLenientPath`；phase_sync 仍用 `PathValidator.GetSafePath`。**何時挑哪個** 未被文件化
- **修法草圖**：在 `ResolveLenientPath` doc 上方加：
  > lenient：manifest-driven user files 且檔名可能含 vendor encoding（BTS `%`）
  > strict（PathValidator）：internal / config / user-input 直接輸入的 path

### P2-L. Unicode NFC/NFD normalization on subject collision
- **狀態**：`open`
- **範圍**：muscle_ratio
- **來源**：senior-software-engineer
- **位置**：`internal/muscle_ratio/analyzer.go:285-302` `assertUniqueSanitizedSubjects`
- **問題**：macOS APFS / HFS+（case-insensitive 預設）對 `café` (NFC) 與 `café` (NFD) hash 同 on-disk name，但 `strings.ToLower` 視為相異；現行檢查會放行兩筆然後第二筆覆寫第一筆
- **修法草圖**：
  ```go
  import "golang.org/x/text/unicode/norm"
  key := norm.NFC.String(strings.ToLower(safe))
  ```

### P2-M. Mutable globals（DefaultRatios, shortNameMap）
- **狀態**：`open`
- **範圍**：muscle_ratio
- **來源**：code-debugger
- **位置**：`internal/muscle_ratio/calculator.go:28, 43`
- **問題**：slice / map global 可被任何 importer mutate；`//nolint:gochecknoglobals` 註解寫「treated as immutable」但沒語言層強制
- **修法草圖**：改 accessor function 回傳 copy
  ```go
  func DefaultRatios() []MuscleRatio { return []MuscleRatio{{...}, ...} }
  ```
  **影響**：`TestDefaultRatios_OrderLocked` 與所有 `for _, r := range DefaultRatios` 都要改

### P2-N. `Ratio` subnormal-denominator → Inf 違反契約
- **狀態**：`open`（邊緣案例）
- **範圍**：muscle_ratio
- **來源**：code-debugger
- **位置**：`internal/muscle_ratio/calculator.go:62`
- **問題**：`Ratio(1.0, 5e-324)` 通過所有 guard，回 `+Inf`；契約「den==0 → NaN」隱含「any underflow → NaN」但實際沒覆蓋
- **修法草圖**：尾段 `r := num/den; if math.IsInf(r, 0) { return math.NaN() } return r`
- **必須伴隨**：在 `calculator_test.go` 加 `{"subnormal denominator", 1.0, 5e-324, 0, true}`

---

## P3 — 機會性改進

### P3-A. `AnalysisResult` 包裝薄
- **位置**：`internal/muscle_ratio/analyzer.go:43-46`
- **問題**：struct 只 re-export `Subjects []SubjectResult`；唯一 caller 立即 `.Subjects`
- **修法**：改回 `([]SubjectResult, error)`；wrapper 沒 carry weight

### P3-B. `evalSymlinksLenient` parent-dir fallback 死碼
- **位置**：`internal/security/lenient_path.go:81-94`
- **問題**：當前 caller 只讀既存檔案（`os.Stat IsNotExist` 早就擋下不存在路徑）；fallback 只為「先建檔路徑後寫檔」場景，無 caller 使用
- **修法**：刪除 `evalSymlinksLenient`，改直接 `filepath.EvalSymlinks(joined)`

### P3-C. `parsers.NewEMGParser` 「per-call 自動 release race-detector pressure」doc
- **位置**：`internal/parsers/emg_parser.go:NewEMGParser` doc comment（待加）
- **修法**：在 NewEMGParser 上方加 doc 段，解釋 `ParseFile` 寫 frequency field 的 race 含義；analyzer 端則簡化為 `// see NewEMGParser doc`
- **連動**：與 P2-C 二選一

### P3-D. SubjectResult 增 `DurationMs` 欄位
- **來源**：knowledge-researcher
- **位置**：`internal/muscle_ratio/analyzer.go:SubjectResult`
- **問題**：partial-success batch 沒有 per-subject 耗時，diagnostic 困難
- **修法**：`Success` 上加 `DurationMs int64 \`json:"durationMs"\``；analyzer 用 `time.Since(start).Milliseconds()` 填入；frontend 表格多一欄

### P3-E. i18n / 訊息字串 tag
- **來源**：quality-assurance-manager
- **位置**：`internal/muscle_ratio/analyzer.go`, `gui/muscle_ratio_handlers.go`, `internal/security/lenient_path.go`（多處 zh-TW 字面）
- **問題**：`config.language` 支援 zh-TW/zh-CN/en-US/ja-JP，但專案無 message catalog；新訊息也照舊 hard-code zh-TW
- **修法**：兩階段——(1) 先把 language config 標 `unused`，或 (2) 引入 `internal/i18n` 訊息表並逐步遷移

---

## 測試補洞 TODO

由 software-test-engineer 與其他 agent 整理：

| 缺口 | 位置 | 優先 |
|---|---|---|
| 半寫 CSV simulation（os.Pipe + 提前 Close） | muscle_ratio + cci | P2 |
| `Ratio(0, 0)` 顯式 case | calculator_test.go | P3 |
| Inf cell 經 `formatRatioCell` 寫成空字串 | muscle_ratio analyzer_test.go | P2 |
| EMG 含空 cell（NaN-in）整合測試 | muscle_ratio analyzer_test.go | P2 |
| Subject = `=SUM(...)` → 檔名 sanitize 顯式 assert | muscle_ratio analyzer_test.go | P3 |
| 負時間 `Time (s)` 格式 | muscle_ratio analyzer_test.go | P3 |
| race test wrap 內 `for k := 0; k < 10; k++` 增穩定性 | cci + muscle_ratio | P3 |
| Lenient path：bare `..` / `./` / `null byte` / 長路徑 | security/lenient_path_test.go | P3 |
| 內部 symlink target 是 base 自己 | security/lenient_path_test.go | P3 |
| `TwoSubjects_BothExported` 改實際讀 4 個 CSV 內容 | muscle_ratio analyzer_test.go | P2 |
| `CaseOnlySubjectCollision_FailFast` 補 `outDir empty` 斷言 | muscle_ratio analyzer_test.go | P2 |

---

## 共同盲點（沒有任何 agent 提到的議題）

- **batch RPC 沒 `context.Context` 用於取消**：muscle_ratio batch 跑到一半使用者按返回，goroutine 仍會跑完整 batch。Wails v2 已知限制，v3 規劃中。**追蹤**：當 batch 跨幾百 subject 時實際變痛點再處理
- **OnFileDrop 對 `muscleRatioManifest` 的 binding**：drop zone marker 存在但 `handleFileDrop` 沒對應分支（peer panel 也未啟用所有 drop target）；目前無 functional impact

---

## 處理流程建議

1. 從 P1-A（atomic write）開始——它解 P1-A、P2-A 兩個關聯項
2. P1-B（OutputDir validation）+ P1-A 可在同 PR
3. P2-C（EMGParser pure）值得排程，會自然清掉多處 comment 重複
4. P2-B（manifest loader）等 P2-C 完成再做，避免 refactor stack-up
5. 測試補洞 P2 級可獨立 PR，影響範圍小
6. 文件項（P2-J、P2-K）可在 next CCI / phase_sync 改動時順便補
