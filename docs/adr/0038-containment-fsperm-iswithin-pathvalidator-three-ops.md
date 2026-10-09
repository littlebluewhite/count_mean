# containment 收斂為 fsperm IsWithin / IsWithinResolved；PathValidator 縮為 strict file / external file / external dir

**Status**: accepted · **implemented** (2026-10-09)

本 ADR 同時記錄 W3 計畫 3.1（containment 判定單一來源，該 task 未另寫 ADR）與 3.2（PathValidator 介面縮減）兩個相連的決定。

## Decision

### 3.1 containment 只有一個判定來源：`fsperm.IsWithin` / `fsperm.IsWithinResolved`

1. `internal/security/fsperm/perm.go` 提供兩個函式，**所有** containment 檢查都經由它們：
   - `IsWithin(base, target string) bool`：純字串(lexical)判定；任一輸入為空時 fail-closed。
   - `IsWithinResolved(base, target string) (resolvedBase string, ok bool)`：兩端先經 `EvalSymlinksWithFallback` 解析再比；target 含 `..` element 時 fail-closed。
2. `pathvalidator.isPathWithinBase` 刪除；`PathValidator.ValidateFilePath` 改呼叫 `fsperm.IsWithin`。
3. validated-open gate 入口先以 `absolutizeKeepDotDot`(`validated_open.go`)把相對 path 絕對化(保留 `..` 讓後續 fail-closed 看得到)。
4. 順帶修掉舊實作把 `..foo` 這類合法子項誤拒的 bug(commit 2e44ff3)。

### 3.2 PathValidator 只剩三個驗證操作

| 操作 | 用途 |
|---|---|
| `ValidateFilePath` | strict：受控內部路徑，必須落在 allowedBasePaths 內 |
| `ValidateExternalPath` | 使用者選的外部**檔案**(GUI dialog、CSV 讀寫目標) |
| `ValidateExternalDir`(新) | 使用者選的外部**目錄**(output / data folder / config 目錄) |

1. `ValidateExternalPath` 與 `ValidateExternalDir` 共用 private `validateExternal(path string, isDir bool)`：擋同一組系統敏感位置(`/etc`、`~/.ssh`…)與 symlink 穿透。`isDir` 時在 path 後附內部 sentinel child 再驗，使目錄根本身(結尾無 slash)也命中 sensitive pattern，同時檔名專屬規則(檔名長度、Windows reserved device name)不套到目錄名。
2. **取代 `_validation_marker` 手法**：`internal/config/config.go`(5 個目錄)與 `internal/muscle_ratio/analyzer.go` 不再自行 `filepath.Join(dir, "_validation_marker")`，改呼叫 `ValidateExternalDir`；muscle_ratio 同時改用 `DefaultValidator()`，不再每次呼叫新建 validator。(io 的 `validateMuscleRatioOutputDir` 留待後續 task。)
3. **GUI `dataFolder` 改走目錄檢查**：`gui/path_validation.go` 的 `validateManifestHandlerParams` 對 dataFolder 呼叫 `ValidateExternalDir`。**這是刻意的行為收緊**：原本 `dataFolder = "/etc"` 或 `~/.ssh` 因缺結尾 slash 而通過，現在被擋(紅測試 `TestValidateManifestHandlerParams_RejectsSensitiveDataFolder`)。錯誤仍由 `inputMessage` / err 通道送出，格式 `資料夾 路徑驗證失敗: …` 不變。
4. 刪除 pass-through：`PathValidator.IsCSVFile` / `PathValidator.SanitizePath` method(caller 改用同名 package 函式 `security.IsCSVFile` / `security.SanitizePath`)與 `ValidateDirectoryPath`(= `ValidateFilePath`，`GetSafePath` 直接呼叫後者)。只測 method wrapper 的測試改為直接測 package 函式。

## Why

- containment 判定散在 `isPathWithinBase`、validated-open、各處自寫 `HasPrefix`，語意不一(`..foo` 誤拒、相對 path、symlink)。單一來源讓 fail-closed 規則只需在一處釘住。
- 目錄用「檔案 API + dummy child」是每個 caller 各抄一份的 workaround；漏抄的地方(GUI dataFolder)就是安全缺口。把語意放進 validator，caller 無從寫錯。
- 三個操作對應三種真實信任等級；其餘方法只是同名轉呼叫，增加介面面積而無新行為。

## Considered Options

1. **維持各 caller 附 `_validation_marker`**：已造成 GUI 漏洞，否決。
2. **`ValidateExternalPath` 自動偵測目錄(`os.Stat`)**：路徑尚不存在的 output 目錄無法判定，且讓驗證依賴檔案系統狀態，否決。
3. **`ValidateExternalDir` 另寫一套規則**：兩份敏感清單遲早分歧，否決；採共用 `validateExternal(path, isDir)`。
4. **保留 method wrapper 以減少 caller diff**：wrapper 無行為，否決(caller 僅 io 三處)。
