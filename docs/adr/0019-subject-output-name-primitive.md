# SubjectOutputName primitive — Subject-based 檔名的 sanitize+suffix 單一真相（CSV ownership 不變、兩 PNG handler 對稱化）

**Status**: accepted (2026-06-03, design) · **implementation pending**（後續獨立 `/handoff` 進 worktree） · 同日修訂：cross-check 推翻「Composer = File-based」前提後，納入 Composer-PNG 對稱化（deep option，見 Considered Options）

本 ADR 是 architecture review Candidate 4「give chart-output (PNG) filenames a home」grilling 後的結論。卡片「讓兩個 PNG 命名有個家」的直覺**正確**——只是達成方式不是「合併兩個發散 interface」（那才是 interface-widening），而是讓兩個 PNG handler 共用同一個 Subject-naming primitive。cross-check 證實 CCI-PNG 與 Composer-PNG **都是 Subject-based**（`{subject}_<suffix>.png`）；Composer 過去被誤判為 File-based，源於一句與實際行為矛盾的 stale Go 註解（見 Decision）。

## Decision

抽出純函數 `filename.SubjectOutputName(subject, suffix string) string`，定義為 `Sanitize(subject) + "_" + suffix`，co-locate 在 `internal/validation/filename`（與 `filename.Sanitize` 同居，延續 [[ADR-0015]] 的家）。

七個 Subject-based 命名 site 共用它：

| Site | 檔名 template | 行為變更 |
|---|---|---|
| `csv_handler.go:828` `WriteNormalizedPhaseSyncResult` | `%s_normalized_norm-%s-%s_stats-%s-%s.csv` | 無（逐字不變） |
| `csv_handler.go:872` `WriteCCIResult` | `%s_CCI_Rudolph.csv` | 無 |
| `csv_handler.go:1031` muscle_ratio output1 | `%s_muscle_ratio.csv` | 無 |
| `csv_handler.go:1081` muscle_ratio phases | `%s_muscle_ratio_phases_avg11.csv` | 無 |
| `csv_handler.go:1196` CCI phases | `%s_CCI_Rudolph_phases.csv` | 無 |
| `cci_handlers.go:186` `DownloadCCIChart` (PNG) | `%s_CCI_Rudolph.png` | 無 |
| `chart_composer_handlers.go:353` `DownloadChartComposerImage` (PNG) | `{subject}_chart_composer.png` | **有**：命名落點 frontend→backend（見下） |

**primitive 契約：**

- **raw `subject` 進、內部 `Sanitize`**——caller 不再持有 `safeSubject` local，Subject 只透過此 primitive 碰到檔名。
- **stem-only**：回 `{Sanitize(subject)}_{suffix}`，caller 自接 `.csv` / `.png`。共用單位是 stem，模型化 CCI 的 CSV/PNG 同根。
- **回 `string`、不回 error**：空 / 全 unsafe 的 subject 走 `Sanitize` 既有 `"untitled"` fallback（`sanitize.go:57-59`）。
- **前 6 site = zero behavior change**（純 refactor，檔名逐字不變）；Composer-PNG 是唯一帶行為變更的 site（見下）。

**Composer-PNG 對稱化（本 ADR 的 deep 決定）：**

過去 `DownloadChartComposerImage` 收前端組好的完整 `OutputPath`，後端再 `filepath.Dir` + `Sanitize(filepath.Base())` + `.png`-normalize 拆回來。Go 註解（`chart_composer_handlers.go:80,350`）稱 OutputPath「由前端 file dialog 完整給出」——**此前提不成立**：前端 `main.js:1100-1116` 明載「**鏡像 `downloadCCIChart`：走 config.outputDir + 自動拼檔名，不問 user**」、suffix 為 `'chart_composer'`、pattern 為 `<subject>_<suffix>.png`。即 Composer-PNG 一直是 Subject-based，只是 path 在前端組裝、後端拆回。

本 ADR 把它對稱到 CCI-PNG：前端改傳 `subject`，後端建 `config.OutputDir + SubjectOutputName(subject, "chart_composer") + ".png"` → `downloadValidatedPNG`。一次溶解：

- unreachable dead empty-guard（`chart_composer_handlers.go:369-371`，`Sanitize` 永不回 `""`）；
- `filepath.Dir` / `filepath.Base` 拆解 + `.png`-normalize 體操；
- 前端 `(mpSubject.value || "chart_composer")` 與 `_chart_composer.png` 相撞產生的 `chart_composer_chart_composer.png` doubled-name smell；
- 兩句 stale 的「file dialog」Go 註解。

兩個 PNG download handler 對稱後僅差 suffix 常數（`CCI_Rudolph` / `chart_composer`），皆為 `config.OutputDir + SubjectOutputName(subject, suffix) + ".png" → downloadValidatedPNG`。

**[[ADR-0004]] 不 govern 兩個 PNG handler**：其 File-based 清單是 CSVHandler writes（`PhaseAnalysis / MaxMean / Normalized`），PNG download 是獨立 GUI handler，不在 ADR-0004 的 format-aware-write 分類內。對稱化後兩 PNG handler 一致為 Subject-based，與 ADR-0004 的 CSVHandler 軸正交、不衝突。

## Why

1. **安全不變量集中**：「Subject 進 path 前必先 `Sanitize`」是安全邊界。`Sanitize` 已驗證把所有路徑分隔符（`/ \ : * ? " < > |` + 空白）替換為 `_`、空字串回 `"untitled"`，輸出恆為單一 path segment、無法 traverse。此不變量現被各 site 重述；集中進 primitive 後第 N+1 個 Subject-based 輸出**無法繞過 sanitize**。
2. **deletion test pass**：刪掉 primitive，`Sanitize(subject)+"_"+suffix` idiom 在 7 site 重現、安全不變量再度分散——複雜度回流，earns its keep。對照 [[ADR-0015]]：relocation-only=pass-through（拒）、consolidation=過 test（本案）。
3. **interface 剝奪犯錯能力**：收 **raw** subject、內部 sanitize（非收已淨化 `safeSubject`），讓 caller 拿不到「跳過 sanitize」這個選項。同 [[ADR-0015]] 收 `Sanitize` 進 validator 家的理由。
4. **[[ADR-0004]] Boundary 2 reconciliation（CSV 側）**：CSV 5 site 的 ownership（哪個 struct 的 Subject、配哪個 suffix、何時忽略 `req.Filename`）仍 100% 在 CSVHandler；只有 `Sanitize+concat` mechanic 下沉。**mechanic ≠ ownership**，與 Boundary 2 不衝突。看到 CSVHandler `import filename` 呼叫 `SubjectOutputName` **不是** violation，勿 inline 回去。
5. **兩 PNG handler 對稱化兌現卡片原意，且治根非治症**：Composer-PNG 與 CCI-PNG 收斂為同一 Subject-based 形狀，naming 終於「有個家」。這**不是** interface-widening——兩者本就同 camp，過去的發散是「前端建/後端拆」的 artifact，由一句 stale「file dialog」註解憑空撐起。dead-guard 等死碼是該 artifact 的副產物；移除 artifact（治根）一併消滅死碼，優於只刪 dead-guard（治症）。

## Considered Options

- **narrow：只刪 dead-guard**（保留前端建 path / 後端拆回）：拒。treats symptom not root——dead-guard 只是「前端建/後端拆」往返的徵狀；刪掉它，發散結構與 stale 前提仍在，兩 PNG site 仍不共用 home，未來易再長出防禦性死碼。（grilling 當下被否決的選項；user 取 deep。）
- **全名 + ext 參數**（`SubjectOutputName(subject, suffix, ext)`）：拒。`ext` 當 string 易 `".csv"` vs `"csv"` 混淆；CCI 的 CSV/PNG 同根退化為巧合。
- **stem + 共用 suffix 常數**：拒。會讓 convention-agnostic 的 filename pkg 收進 CCI / muscle_ratio 命名知識；suffix 是各 caller 的 business。[[ADR-0012]] 式 preserve：併 primitive、不併 convention。
- **收已淨化的 `safeSubject` 當參數**：拒。退化成單純去重，**無法強制** sanitize。
- **preserve + inline 不抽**：拒。安全不變量分散 7 site；[[ADR-0015]] 已立「集中 load-bearing 不變量」先例。

## Reversibility

中。primitive 本身 ~5 行、易 inline 回。Composer-PNG 對稱化動到 Wails RPC 契約（`DownloadChartComposerImageParams` `OutputPath`→`Subject`）+ 前端 `downloadComposerChart` + dist rebuild，回退成本中等；重識安全動機需重走 grilling。

## Related

- [[ADR-0015]] — `filename.Sanitize` 與 validator 同居；本 ADR 直系續集（同家再加 primitive、只併 primitive、preserve policy）。
- [[ADR-0004]] — filename ownership 隨 unit-of-work 形狀分（CSVHandler writes 的 Subject/File 軸）；本 ADR 的 CSV 側 lens，Boundary 2 在此 reconcile；**不涵蓋 PNG download handler**。
- [[ADR-0009]] — PNG download 安全管線（`downloadValidatedPNG`）已抽；naming 是 leftover，本 ADR 收乾，兩 PNG handler 對稱後皆走此管線。
- [[ADR-0020]] — 平行 session：normalized-phase-sync Output1 收回 CSVHandler，其 fork A 讓該輸出成為 `SubjectOutputName` 的另一個 consumer。complementary（0019=primitive、0020=ownership migration）；0019/0020 為 live 撞號於 write-moment 捕獲後分配的編號。

## Notes

- **Design → implemented（2026-06-03）**：設計與實作同日完成；impl 走 subagent-driven（5 task + 兩階段審查 + codex），見下方 catch-net process note。
- **impl 注意（Composer-PNG，唯一行為變更 site）**：(a) empty-subject 行為**改變**——現況 `chart_composer_chart_composer.png`（degenerate doubled-name）→ 對稱後 `untitled_chart_composer.png`；以 characterization test 標記、確認 degenerate-case 可接受。(b) 前端「outputDir 未設定 → ShowError」UX guard（`main.js:1129`）需保留（前端 pre-check 或後端回明確 error）。(c) 修 `chart_composer_handlers.go:80,350` 的 stale「file dialog」註解。(d) 改 `frontend/src` 後 `npm run build` rebuild dist（[[feedback_wails_frontend_dist_rebuild]]）。
- **impl 注意（CSV / CCI 6 site）**：re-grep `_CCI_Rudolph` / `_muscle_ratio` / `_normalized` / `safeSubject` 補網；TDD characterization test 鎖死「檔名逐字不變」。若挖到表外額外 Subject-based site，補進此 Notes，勿回頭重 grill（precedent：[[ADR-0015]] Process note）。
- **Impl process note — catch-net 表外 site（2026-06-03 impl sweep）**：`safeSubject`/`filename.Sanitize` re-grep 挖到兩個 7-site 表外 Subject-based site。(a) `calculator.GenerateOutputFileName`（`{subject}_{startPhase}-{endPhase}_statistics.csv`）——無 ADR 認領、相同 idiom，**本案納入**為內部 consumer（zero-behavior）。(b) `gui/normalized_phase_sync_handlers.go:149`（`{subject}_normalized.csv`）——ADR-0020 明文認領的遷移目標，**本案不碰**，待 0020 收回 CSVHandler 時委派 primitive。依 [[ADR-0015]] precedent 補記於此、未回頭重 grill。**計數修正**：本 note supersede 上方 Decision 的「七 site / 前 6 site = zero behavior」措辭——實際遷移 **8 site（7 zero-behavior + Composer-PNG 1 behavior-change）**；原 Decision 表反映 grilling 當下的盤點，GenerateOutputFileName 為事後 catch-net 補入。
- **Impl process note — CCI/Composer package & inline doc 對稱（2026-06-03）**：對稱化時順手修正 `cci_handlers.go` 與 `chart_composer_handlers.go` 兩處 `{safeSubject}`/「filename.Sanitize + .png normalization」stale doc（兩階段審查 spec/quality reviewer 各抓一條），使兩 PNG handler 的 package-doc / 推導敘述一致。`csv_handler.go` doc 內的 `{safeSubject}` template 標記屬 catch-net 明示忽略清單、未動（template 仍準確描述輸出）。
