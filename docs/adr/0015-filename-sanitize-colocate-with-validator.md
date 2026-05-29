# 檔名 sanitize 遷入 internal/validation/filename 與 validate 同居 — 拒絕搬去 internal/security/,共用 primitives、policy 保留分歧

**Status**: accepted + implemented (2026-05-29) · impl 隨本 branch `feat/filename-sanitize-consolidation` 落地(3 commit:patterns free fn → filename.Sanitize 遷移 → i18n/註解收尾)

## Decision

`SanitizeFileName` + 兩個 helper(`truncateAtRuneBoundary` / `isUnsafeFilenameRune`)從 `internal/calculator/emg_statistics.go:213-356` 遷入 `internal/validation/filename`,export 名 rename 為 **`filename.Sanitize`**(去 stutter)。檔名安全的 **validate-face**(`filename.ValidateFilename`,reject)與 **transform-face**(`filename.Sanitize`,replace/drop/truncate)視為同一「檔名安全政策」的兩張臉,co-locate 在同一 package。

共用的是 **primitives(詞彙)**,不是 policy(行為):

1. **rune predicate 收成 package-private 單一份。** `calculator.isUnsafeFilenameRune`(`emg_statistics.go:343-356`)與 validator 的 `checkControlChars` rune 迴圈(`filename_validator.go:162-176`)兩份分類邏輯 byte-identical(`r==0 || unicode.IsControl(r) || unicode.In(r, Cf, Cs)`)。遷入後合成 `validation/filename` 內一個 package-private `isUnsafeFilenameRune(r rune) bool`,`ValidateFilename`(reject-on-true)與 `Sanitize`(drop-on-true)各自套用。
2. **reserved-name 集合單一真相。** 新增 free function `patterns.IsReservedName(name string) bool`(consult `patterns.ReservedNames`,`patterns.go:220-224`);既有 `InjectionDetectorImpl.IsReservedName`(`injection_detector.go:176-187`)改 delegate 過去;`Sanitize` 棄掉自己的 inline `switch`(原 `emg_statistics.go:267-282`)改 consult 它,**保留自己的 first-stem + leading-dot + `_safe` splice policy**。

**不**共用 policy — 以下分歧各自保留,刻意不併:

| 軸 | transform-face(`Sanitize`) | validate-face(`ValidateFilename`) |
| --- | --- | --- |
| 危險字元 | **replace** `→_`(`replacements` map) | **reject**(`patterns.DangerousChars`) |
| 長度 | truncate @ **200 bytes**(rune-boundary) | reject @ **255 chars** |
| reserved-name 範圍 | first stem(leading-dot aware) | 每個 dot-segment(`filename_validator.go:128-138`) |
| 空字串 | → `"untitled"` | → error |

附帶:`calculator.GenerateOutputFileName`(`emg_statistics.go:189-194`,domain naming `{subject}_{start}-{end}_statistics.csv`)留在 `calculator`,內部改呼 `filename.Sanitize`;i18n 衝突訊息(`translations_{zh_tw:106,zh_cn:102,ja_jp:102}.go` 的 `KeyErrorMuscleRatioSubjectCollision`)genericize 為「經檔名安全化後」,移除外洩的函式名。

**明確不放 `internal/security/`**(2026-05-29 architecture review handoff 的原 target)。

## Why

- **原候選的招牌理由(dependency-direction inversion)經開場 cross-check 證偽。** handoff 稱「`io/csv_handler.go` 只為了 `SanitizeFileName` 才 import `calculator`」— 失準。io 還用 `calculator.AnalyzeResult`(型別,`csv_handler.go:698`)與 `calculator.GenerateOutputFileName`(`:763`)。搬走 sanitizer **拆不掉** `io → calculator`,若放 security/ 反而多一條 `io → security`。dependency-direction inversion 不成立,能站住的只剩「語意歸屬 + 去重複」。

- **Deletion test:relocation-only 不過,consolidation 過。** 純搬家(Option B,security/)是 pass-through — 刪掉 3 個函式複雜度只是位移到新 package,不消失。但把 reserved-name set 與 rune predicate 收成單一真相後,刪掉該真相,複雜度會在 validator + sanitizer **兩處重新長出**(各自重抄保留字表 / 控制字元分類)。真實 leverage 在去重複,不在搬家 — 這是本 case 與純語意搬遷的分水嶺。

- **co-locate validation/filename 勝 security/,因為 (c) 的目標是「合臉」。** validate-face 已住 `validation/filename`;把 sanitize 放 security/ 會把同一政策的兩張臉劈到**兩棵不相交的樹**(已驗:security 與 validation 互不 import),且逼出 `security → validation/patterns` 新跨樹邊(共用真相在 patterns)。同居 validation/filename:**新增 package 0、新跨樹邊 0**。cycle-safe 已驗 — `validation/filename` 僅 import `errors` + `patterns` 兩個 leaf(`internal/errors` 對 count_mean 零 import;`patterns` 只 import stdlib),故新邊 `calculator → validation/filename`(經 GenerateOutputFileName)與既有 caller 邊 `io`/`gui`/`muscle_ratio → validation/filename` 皆無環。

- **rune predicate 是 context-specific,只合 filename 兩份;csv/chart/path 三變體不動。** 全 repo 5 處 rune 分類有 **4 套不同規則**:filename drop 全控制 + Cf/Cs(本 case);CSV cell 容 `\t \n \r`(`validation/csv/cell_validator.go:262`);chart JS-string 容 `\t` 且額外處理 U+2028/U+2029 Zl/Zp(`chart/sanitize.go:48`);path 加 `unicode.IsSpace`(`security/lenient_path.go:189`)。只有 calculator 與 validator 兩份是同一 filename context 故 byte-identical → 合一。對其餘三者套 deletion test:它們是**不同 module**,合併會破壞各自語意(naive「dedupe 全部 unicode.IsControl」是 over-merge 陷阱)。

- **跟既有 ADR 框架對位。** [[ADR-0006]](不為「讓 anaemic type 看起來 deep」而保留 shallow seam)與 [[ADR-0012]](不為 symmetry 而把 divergent analyzer 收成統一 interface)構成 deletion-test honesty 雙語。本 ADR 是第三個 data point:**relocation-only 才是 pass-through(被拒)、consolidation(去重複)才過 test**;同時對 policy 軸(replace/reject、byte/char、stem/segment)採 [[ADR-0012]] 式 preserve,只併 primitives。

## Considered Options

- **A. 放棄(留在 `calculator`)** — 拒。檔名安全邏輯塞在 `emg_statistics.go`(一個與 EMG 統計毫不相干的檔)是真實 misfiling;且未來 architecture review 會反覆把它挖出來重 grill(本次就是)。不留決策 = 重蹈。
- **B. 純搬家到 `internal/security/`(relocation-only,handoff 原案)** — 拒。deletion test 不過(pass-through);把兩張臉劈到兩棵不相交的樹;多一條 `security → validation/patterns` 跨樹邊;且 `io → calculator` 本來就不是只為 sanitizer(見 Why 第 1 條),搬走解不掉那條邊。security/ 適合 relocation-only framing,不適合 (c) consolidation。
- **C. 同居 `validation/filename` + 去重複 primitives(本次採)** — 見 Decision。新 package 0、新跨樹邊 0、cycle-safe;reserved-name set 與 rune predicate 各從 2 份收成 1 份;policy 分歧保留。
- **D. 新中性 package(例 `internal/fnsafety`)收兩張臉 + primitives** — 拒。語意最純(一套政策兩張臉),但 churn 最大:validator 得離開 `validation/`、orchestrator(`validation/validator.go` 的 `InputValidator`)重接線、`patterns.ReservedNames` 得遷出或重複。純度的邊際收益不抵成本,違反「surgical / 最小變更」。

## Reversibility

中 — package 已遷、~8 個 call site 已 rewire(`calculator.SanitizeFileName` → `filename.Sanitize`)後要再搬成本不低。但 primitives 的單一真相(`patterns.IsReservedName` + package-private rune predicate)讓未來若要拆臉或併臉,都從一處改。本 ADR 主要鎖住的是**方向**:「不回 `internal/security/`」。日後若 security/ 演化出通用的「output-name safety」家族需求,可重啟 grilling — 但屆時應是 fresh deepening,而非把這次的 validate/sanitize 合臉拆開。

## Related

- [[ADR-0006]] — 同 deletion-test honesty(不為 collapse/preserve 而 collapse/preserve)。本 ADR 補一個對照:同樣跑 deletion test,**relocation-only 失敗(pass-through)、consolidation 通過(複雜度真實去重)**。Process note 體例亦沿用 0006。
- [[ADR-0012]] — 同「preserve 分歧、document 落點」。本 ADR 對 policy 軸(replace/reject、200 bytes/255 chars、first-stem/all-segments、untitled/error)採同樣 preserve,只把 primitives 收成單一真相。

## Process note — cross-check / framing-mismatch findings(防未來 reviewer 重蹈)

2026-05-29 grilling session 開場 mandatory cross-check(memory `feedback_cross_check_report_vs_code`)抓到候選 handoff 與 architecture review report 多處 framing 與 code 對不上,記錄於此:

1. **「io imports calculator solely for `SanitizeFileName`」失準。** io 還用 `calculator.AnalyzeResult`(`csv_handler.go:698`)+ `GenerateOutputFileName`(`:763`)。「dependency-direction inversion」為候選招牌理由,證偽後候選改以「語意歸屬 + 去重複」重新立論(否則應直接 Option A 放棄)。
2. **handoff 的 target `internal/security/` 在 (c) consolidation framing 下被推翻。** handoff decision tree 稱 security/「strongest semantic fit」— 那是 relocation-only(Option B)框架下的判斷;選 (c) 合臉後,security/ 反而劈裂兩張臉,co-locate validation/filename 才對。
3. **rune-predicate context-specificity(over-merge 守門)。** 全 repo 5 site / 4 rule-set;只合 filename 兩份,csv(`cell_validator.go:262`)/chart(`chart/sanitize.go:48`)/path(`lenient_path.go:189`)**不動**。防 naive「全 repo dedupe unicode.IsControl」破壞 CSV/chart/path 語意。
4. **ADR 編號實證。** handoff exit criteria 猜 0015;write 當下 `ls docs/adr/`(0001–0014 tracked、git status 無 untracked ADR)+ `git worktree list`(僅 main checkout、無平行 session race)確認 → 0015。ADR-number collision 在本 repo 重演多次(memory `feedback_adr_number_collision`),impl PR 落地前應再驗一次。
5. **impl 階段提醒(漏網寫 note 不 re-grill)。** 本 design session 列出 8 個 `calculator.SanitizeFileName` call site(gui×3 / io×3 / muscle_ratio×1 / calculator-self 經 GenerateOutputFileName)。真實搬遷前 impl 應 re-grep `SanitizeFileName` 與 `GenerateOutputFileName` 補網;若挖到額外 caller,補進本 note,勿回頭重 grill(precedent:[[ADR-0006]] Process note point 5)。
