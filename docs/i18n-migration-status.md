# i18n Migration Status (P3-E)

本文件記載 P3-E 後端 i18n 遷移工作的最終狀態：已遷的 key 清單、未遷字串的排除理由、known limitation、與 future work。

**Source of truth**：[TODO_REFACTOR.md](../TODO_REFACTOR.md) §P3-E。
**Plan**：`~/.claude/plans/approach-a-whimsical-porcupine.md`（Approach A — Pilot-first 分階段）。

---

## 工作量總覽

| 統計項 | 值 |
|---|---|
| TODO 原標 | 40 處 |
| Grep 命中（pattern `失敗\|錯誤\|無法\|成功` + 後續補掃）| 36 處 |
| User-facing（reachability trace 後 + (3c) 補的 4 條）| 27 處 |
| 排除（不該 i18n）| 12 處 |
| **實際遷移** | **15 處** |

工作分布跨 5 個 phase：Phase 1 catalog + 守門 → Phase 2 handlers → Phase 3 analyzer → Phase 3c follow-up → Phase 4 lenient_path doc → Phase 5 翻譯落地 + tooling。

---

## 已遷 Key 清單（15 條）

### `gui/muscle_ratio_handlers.go`（3 條）

| 位置 | 原字串 | Catalog Key |
|---|---|---|
| L72 | `"分析失敗: %v"` | `error.muscle_ratio.handler.analysis_failed` |
| L93 | `"已處理 %d 個主題"` | `status.muscle_ratio.processed_count` |
| L95 | `"（部分主題未完成，請查看各 row 的錯誤訊息）"` | `status.muscle_ratio.partial_warning` |

### `internal/muscle_ratio/analyzer.go` — Phase 3（8 條）

| 位置 | 原字串 | Catalog Key | 改造模式 |
|---|---|---|---|
| L86 | `"OutputDir 驗證失敗: %w"` | `error.muscle_ratio.output_dir_invalid` | `fmt.Errorf("%s: %w", i18n.T(key), err)` |
| L91 | `"解析分期總檔案失敗: %w"` | `error.muscle_ratio.parse_manifest_failed` | 同上 |
| L95 | `"分期總檔案沒有任何主題記錄"` | `error.muscle_ratio.empty_manifest` | `errors.New(i18n.T(key))` |
| L103 | `"建立輸出目錄失敗: %w"` | `error.muscle_ratio.mkdir_failed` | caller-wrap |
| L138 | `"Subject 名稱為空"` | `error.muscle_ratio.subject.empty_name` | `result.Error = i18n.T(key)` |
| L150 | `"解析 EMG 檔案失敗: %v"` | `error.muscle_ratio.subject.parse_emg_failed` | `result.Error = i18n.T(key, err)` |
| L171 | `"寫入 Output 1 失敗: %v"` | `error.muscle_ratio.subject.write_output1_failed` | 同上 |
| L190 | `"寫入 Output 2 失敗（Output 1 已產出）: %v"` | `error.muscle_ratio.subject.write_output2_failed` | 同上 |

### `internal/muscle_ratio/analyzer.go` — (3c) follow-up（4 條）

| 位置 | 原字串 | Catalog Key |
|---|---|---|
| L155 | `"EMG 資料為空"` | `error.muscle_ratio.subject.empty_emg` |
| L242 | `"有效分期點不足 2 個，跳過 Output 2"` | `error.muscle_ratio.subject.insufficient_phases` |
| L252 | `"phase %s 時間 %.4f 落在 EMG 範圍 [%.4f, %.4f] 外，跳過 Output 2"` | `error.muscle_ratio.subject.phase_out_of_emg_range` |
| L296 | `"輸出檔名衝突: subject %q 與 %q 經 SanitizeFileName 後同為 %q (case-insensitive)"` | `error.muscle_ratio.subject_collision` |

**遷移 4 條的觸發**：Phase 3 規劃時 Plan agent + Explore agent 用的 grep pattern（`失敗\|錯誤\|無法\|成功`）漏命中含「為空 / 不足 / 落在 / 衝突」的字串。Phase 3 完成後執行手動 grep 全文掃 zh-TW unicode 字符發現，於 (3c) follow-up 補上。

---

## 排除字串（12 條）

### Logger keys（3 條）

log message key 是給開發者讀 log 用，i18n 反而破壞 grep-ability。**不 i18n**。

| 位置 | 字串 |
|---|---|
| `gui/muscle_ratio_handlers.go:L58` | `"開始肌肉比值分析"` |
| `gui/muscle_ratio_handlers.go:L98` | `"肌肉比值分析完成"` |
| `internal/muscle_ratio/analyzer.go:L114` | `"肌肉比值批次分析完成"` |

### GUI sentinel errors（2 條）

| 位置 | 字串 |
|---|---|
| `gui/app.go:L37` | `var ErrNoManifestFile = errors.New("請選擇分期總檔案")` |
| `gui/app.go:L38` | `var ErrNoDataFolder = errors.New("請選擇數據資料夾")` |

**排除理由**：package var 在 `main()` 之前 init（Go 規範 var > init() > main 順序），早於 `i18n.InitI18n()` 跑。若改 `errors.New(i18n.T(...))`，evaluate 時 `globalI18n == nil`，`T()` 走 fallback 回 key 字面 — locale 切換無效。長期由 frontend i18n 層接管 user-facing alert message。

### Programmer-precondition error（1 條）

| 位置 | 字串 |
|---|---|
| `internal/muscle_ratio/analyzer.go:L78` | `fmt.Errorf("params 不能為 nil")` |

**排除理由**：入參 `params == nil` 表示**上層 caller bug**，這 error 給開發者讀 panic/log，non-user-facing。

### Path validation errors（11 條，A2 abstraction 策略）

`internal/security/lenient_path.go` L47/L59/L67/L73/L77/L83/L90/L99/L104/L109/L131 全部不 i18n。

**排除理由**（詳見 `internal/security/lenient_path.go` `# i18n strategy` doc section）：
1. Path validation error 對 end-user 無 actionable value（"檔名包含 null byte" / "路徑過長 > 4096"）
2. Security-critical 套件耦合 i18n 過深，每加 validation rule 都得動 catalog
3. 11 × 4 locales = 44 entries 純粹膨脹 catalog 與 binary
4. 上層 `muscle_ratio/analyzer.go:L150` 已 wrap 為 abstracted `"解析 EMG 檔案失敗: %v"` — user 主要看到的就是上層 wrapper 提供的 message，底層 detail 進 log 即可

---

## Known Limitations

### 1. Frontend hard-code 字串未遷（out of backend scope）

`frontend/src/main.js:2099` 表頭仍 hard-code zh-TW：
```js
['主題', '狀態', 'Output 1 (時間序列)', 'Output 2 (分期切片)', '耗時 (ms)', '訊息']
```

`drop-zone` placeholder、alert dialog message、其他 inline label 同此問題。Locale 切非 zh-TW 時 GUI 將呈現**「中文表頭 + 翻譯後 error cell」混合語言**。

**屬 frontend i18n 範圍**，需另開後端→前端 i18n 整合子項。

### 2. 翻譯品質為 best-effort，需 native speaker review

當前 zh-CN / en-US / ja-JP 翻譯由實作者基於 software engineering / clinical EMG context 提供，**未經 native speaker 校對**。建議：
- zh-CN：對齊 simplified Chinese 醫學 / 軟體用語
- en-US：對齊 medical / sports science 慣用語
- ja-JP：對齊「です・ます」formal tone（catalog 用 `です/ます` 句尾，目前一致）

### 3. Test 環境需顯式 i18n init

非 `main.go` 入口（test、CLI 部分子命令）若使用 `i18n.T()`，須自行調用 `i18n.InitI18n(...)`，否則 `T()` fallback 回 key 字面。

範例（`internal/muscle_ratio/analyzer_test.go`）：
```go
func TestMain(m *testing.M) {
    _ = i18n.InitI18n("./nonexistent") // fallback 走內建 translationData
    os.Exit(m.Run())
}
```

未來其他 package 若 caller 引入 `i18n.T()` 且有 test，需套用同模板。

---

## Validation Tooling

### `make i18n-check`

```bash
make i18n-check
```

跑 5 個守門測試：

| Test | 守門範圍 |
|---|---|
| `TestI18n_NoCatalogPercentWVerb` | catalog 中無 `%w` verb（須由 caller `fmt.Errorf("%s: %w", i18n.T(key), err)` wrap） |
| `TestI18n_VerbConsistencyAcrossLocales` | 4 locale 同 key 的 verb sequence（`%v %d %s %.4f %q`）必須一致；用 zh-TW 為基準 |
| `TestI18n_AllMuscleRatioKeysCovered` | 15 個 muscle_ratio key 在 4 locale 都有 entry，無 fallback 到 key 本身 |
| `TestT_MuscleRatioKeysVerbCompat` | `%v` / `%d` 經 `i18n.T(key, args...)` 注入動態值後輸出正確 |
| `TestI18n_CallerWrapPatternPreservesErrorsIs` | `fmt.Errorf("%s: %w", i18n.T(key), inner)` pattern 不破壞 `errors.Is` 穿透 wrap chain |

### CI 集成

當前 `make ci` / `make ci-fast` 已含 `lint` 與 `test` target，後者間接跑全部 i18n test。**`make i18n-check` 為 lightweight 獨立 target**，適合 i18n-only diff PR 不需跑全 lint 的場景。

### 手動 GUI E2E（建議於 commit / PR 前跑一次）

```bash
make build-wails && ./build/bin/count_mean
# 1. 觸發 muscle ratio fail-path（傳不存在 manifest 路徑）
#    → 確認 GUI 表格 Error cell 顯示「分析失敗: ...」中文正常（無亂碼、無 escape 問題）
# 2. 觸發 OutputDir traversal（config.json outputDir 改 "../../etc"）
#    → 確認顯示「OutputDir 驗證失敗: ...」
# 3. 切 config.language = "en-US" 重跑
#    → 確認後端字串切英文（"Failed to parse manifest file" 等）
#    → 注意 frontend 表頭仍中文（已知 limitation #1）
```

---

## Future Work

### Frontend i18n（需另開）

- **範圍**：`frontend/src/main.js` 所有 hard-code 字串（表頭、alert、placeholder、tooltip）
- **粗估工作量**：~20-30 條，需引入 frontend i18n library（i18next / vue-i18n / 自寫輕量 dict 三選一）
- **觸發條件**：實際多語使用者需求 / GitHub issue

### 後端 i18n 擴展（其他 analyzer）

其他 analyzer（`cci` / `normalized_phase_sync` / `calculator` / 等）也含 user-facing zh-TW 字串，本次 P3-E migration 僅涵蓋 `muscle_ratio` 流程。如需擴大，套用同一 plan template：

1. Reachability trace 識別真正 user-facing 字串（排除 logger / sentinel / programmer-error / technical detail）
2. Catalog 擴 + caller 替換 + 守門 test 自動 cover
3. 翻譯落地（與 Phase 5 同模板）

### 翻譯品質 review

當有 native speaker 介入時，可針對 `translationData` 中 15 個 muscle_ratio key 的 zh-CN / en-US / ja-JP 翻譯做語言審查，調整 tone / 慣用語 / 標點 / 大小寫。`TestI18n_VerbConsistencyAcrossLocales` 守 verb sequence 一致，純文字微調不會破壞守門。

---

## 完成時程

- **Phase 1**：2026-05-14 — catalog 擴 11 keys + 4 守門測試 + demo bug fix
- **Phase 2**：2026-05-14 — handlers.go 3 處替換 + handler locale switch test
- **Phase 3**：2026-05-14 — analyzer.go 8 處替換含 3 處 `%w` caller-wrap + TestMain i18n init
- **Phase 3c follow-up**：2026-05-14 — analyzer.go 4 條漏項補遷 + 4 keys 擴 catalog
- **Phase 4**：2026-05-14 — lenient_path.go `# i18n strategy` doc
- **Phase 5**：2026-05-14 — zh-CN / en-US / ja-JP 翻譯落地 + `make i18n-check` target + 本文件

**順帶修**：
- `test/demo/i18n_demo/main.go:42` `&i18n.I18n{}` → `i18n.NewI18n()`（zero-value nil map panic fix）
- `test/integration/integration_test.go:240` `originalConfig` 加 `Language: "zh-TW"`（P3-E (1) Language 白名單檢查的 pre-existing fixture follow-up）

**累計工作量**：10 modified files + 2 new files（`gui/muscle_ratio_handlers_test.go`、`docs/i18n-migration-status.md`），diff ~+450 / ~-40 LOC。
