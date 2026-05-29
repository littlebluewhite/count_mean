# PhaseSync 寫入完成遷移 +「Subject-based write ⟹ atomic」invariant 補完

**Status**: accepted (2026-05-29) · impl 本 PR · 與 [[ADR-0015]] 解耦(無硬排序)—— 收尾 rebase 時 C1(ADR-0015 / PR #33)已先落地、`calculator.SanitizeFileName` 整族遷為 `filename.Sanitize`;C2 遂於 rebase 直接消費 `filename.Sanitize`(原預期的「C1 日後 rewire 收編第 9 site」由本次 rebase 一併完成)。詳見 Process note pt7。

## Decision

完成 [[ADR-0001]] 的 PhaseSync export 遷移,並把「Subject-based write 一律走 `csvutil.WriteCSVAtomic`」收成 invariant:

1. **新增 `io.CSVHandler.WriteNormalizedPhaseSyncResult(req WriteRequest, stats *models.EMGStatistics, normStart, normEnd models.PhasePoint) (string, error)`** —— Subject-based write。內部推 filename `{filename.Sanitize(stats.Subject)}_normalized_norm-{normStart}-{normEnd}_stats-{stats.StartPhase}-{stats.EndPhase}.csv`,body 用既有 `csvConverter.ConvertPhaseSyncResult(stats)`(8-row,與 regular PhaseSync 逐列相同),走 `WriteCSVAtomic` + `safeJoinOutput` + lenient `ValidateExternalPath` + `MkdirAll`(CCI/MR 食譜)。**不**動 `WriteRequest`(不引入 filename-override)→ [[ADR-0004]] Boundary 2 完整保留。

2. **migrate 既有 `WritePhaseSyncResult` 從 `writeToTarget→WriteCSV` 改走 `WriteCSVAtomic`(同食譜,經共用私有 `writePhaseSyncAtomic` helper)。** PhaseSync 是 Subject-based 家族最後一個非 atomic 成員(CCI / MuscleRatioOutput* 早已 atomic);補上後 invariant 成立:**Subject-based write(PhaseSync / NormalizedPhaseSync / CCI / MuscleRatioOutput*)⟹ `WriteCSVAtomic`;File-based write(PhaseAnalysis / MaxMean / Normalized)⟹ `WriteCSV`(strict guard,非 atomic)。** 簽章不變,`gui/app.go` 的 regular phase sync caller 不動。

3. **刪除 residual + vestigial state(L1+L2)**:`calculator.ExportToCSV` + `writeCSVWithBOM` + `buildUniformRow` + `buildChannelValueRow` + `formatFloat`(L1,全 ExportToCSV-only),連帶 `EMGStatisticsCalculator.precision` 欄位 + `NewEMGStatisticsCalculator` precision 參數(L2:`formatFloat` 死後 precision 變 write-only dead field,`unused` linter 會擋)。`EMGStatisticsCalculator` 變純 compute 物件,輸出格式化真相單獨歸 `csvConverter`(`phaseSyncPrecision=6`)。

## Why

- **filename-override(原 (A) 案)會 reopen [[ADR-0004]] Boundary 2 已拒的「全 caller」替代** —— 新 Subject-based method 繞開:normalized Output 2 是 single-subject unit-of-work,依 Boundary 2 原則本就該 Subject-based;新 method 是**套用**而非 reopen。
- **precision tension 為假(cross-check 證偽)**:converter `phaseSyncPrecision=6`(固定)與 `gui.normalizedPhaseSyncPrecision=6` 相同,遷移零精度差。原框的「filename-override + optional precision」只剩 filename,而 filename 走 Subject-based 即解。
- **PhaseSync 是 Subject-based 唯一非 atomic 異類**:把 regular + normalized 兩個 PhaseSync 寫入一起 atomic 化,是對齊 CCI/MR,真正的異常是「過去非 atomic」。沿 [[ADR-0004]] Subject/File 軸,write-backend 也跟著分。
- **BOM 忠實度**:`WriteCSVAtomic` 無條件寫 BOM,與被刪的 `ExportToCSV`(無條件 BOM)對齊;`WriteCSV` 的 BOM 是 `config.BOMEnabled`-gated 可能漂移。atomic 是更忠實的遷移。
- **deletion 划算**:`ExportToCSV` 的 8-row body 與 `ConvertPhaseSyncResult` 逐列相同 → 新 method 的 body 免費複用;新增 ≈ 十幾行 + 共用 helper,換掉 ~70 行 residual + 一個 pre-existing 半死 precision field。複雜度被吸進 CSVHandler,非平移。

## Considered Options

- **A. WriteRequest 加 filename-override(+ optional precision)** — 拒。reopen [[ADR-0004]] Boundary 2 拒過的「全 caller」方向、稀釋 Subject-based depth;且 precision 同為 6,override 無必要。
- **B. 留 ExportToCSV residual + 記「evaluated-not-adopted」** — 拒。PhaseSync 永久 straddle(math 模組持有 CSV 寫入)、dead code 留存、Subject-based 家族 atomic 不齊。friction 已足以 justify 完成。
- **C. 新 Subject-based method 但走非 atomic `WriteCSV`(對齊直系 sibling)** — 部分採。原推此案(strict guard 升級),user 改選 atomic;遂連 regular 一起 atomic 化,改以「Subject-based ⟹ atomic」立論。trade-off:atomic 放棄 `WriteCSV` 的 strict allowlist `ValidateFilePath` + `OpenWriteValidated` parent-symlink kernel guard,降為 lenient `ValidateExternalPath` + leaf `O_NOFOLLOW`(`TmpCreateFlags`,unix)。接受,因輸出恆在 OutputDir 內 + Subject 經 `filename.Sanitize` + 這正是 CCI/MR 既有姿態。

## Reversibility

中 — 兩寫入已 atomic 化、residual + precision 已刪、新 method + test 已立;回頭分裂要重發明 internal export + 重加 precision + migrate test。invariant 單一,未來若要把 File-based 也 atomic 化,從一處 grilling 重啟即可。

## Related

- [[ADR-0001]] — PhaseSync 走 CSVHandler;本 ADR 完成其最後 25%(normalized Output 2)並刪 calculator 側 residual。
- [[ADR-0004]] — Subject/File filename 軸;本 ADR 在同軸加 write-backend(atomic vs WriteCSV)維度,**不** reopen Boundary 2。
- [[ADR-0015]] — `filename.Sanitize` 遷 `validation/filename`(PR #33,已先落地)。本 ADR 新 method 的 filename 衍生於收尾 rebase 直接消費 `filename.Sanitize`;原計畫的「第 9 個 `calculator.SanitizeFileName` call site 待 C1 rewire 收編」由 rebase 一併完成。**解耦成立(無硬排序)** —— C2 不依賴 C1 先落地,實際落地順序為 C1-先(decision 階段 C1 尚未實作,見 Process note pt6/pt7)。

## Process note — cross-check / framing-mismatch findings

2026-05-29 C2 grilling 開場 mandatory cross-check + impl 計畫階段複驗(memory `feedback_cross_check_report_vs_code`)抓到:

1. 原 handoff 框的「optional precision」tension 假 —— converter 與 gui 兩端 precision 同為固定 6,遷移零精度差。
2. 原 handoff (A)/(B) 二元漏第三路 (C) —— Subject-based 新 method 繞開 [[ADR-0004]] 張力。
3. ADR 編號:handoff 猜 0015;發現 0015 已被平行 C1 session 佔 → C2 用 **0016**(impl 落地前再驗 `ls docs/adr/`,確認 0016 空號;memory `feedback_adr_number_collision`)。
4. branch 實證:grilling 期間 working tree 在 `fix/code-review-p0-p1-batch`(非 main),C1 ADR-0015 untracked;平行 session 共用目錄。
5. impl re-grep 補網:動手前 re-grep `ExportToCSV` / `WritePhaseSyncResult` / `SanitizeFileName` / `NewEMGStatisticsCalculator`;`ExportToCSV` 唯一生產 caller 為 normalized Output 2(已遷),無額外漏網 caller(對照 [[ADR-0006]] impl 常多挖 1-2 caller 的先例,此次乾淨)。
6. **C1 依賴分岔(impl 計畫階段拍板 Option B)**:查驗發現 C1(ADR-0015)尚未實作 —— `calculator.SanitizeFileName` 仍在 `internal/calculator/emg_statistics.go`、`filename.Sanitize` 全 repo 不存在、ADR-0015 仍 untracked(平行 worktree)。user 拍板 C2 獨立先做、沿用現有 `calculator.SanitizeFileName`(與兄弟 `WriteCCIResult` / `WriteMuscleRatioOutputPhases` 逐字一致)、與 C1 解耦;ADR 措辭隨之從「依賴 C1 先落地」改為「解耦 + C1 日後 rewire 收編第 9 site」。

7. **C1 落地順序反轉(2026-05-30 收尾 rebase)**:pt6 decision-time 前提「C1 尚未實作」於收尾時已不成立 —— C1(ADR-0015 / PR #33)已 merge 進 main(`d210f95`),把 `calculator.SanitizeFileName` 整族遷為 `filename.Sanitize` 並移除舊函式。C2 branch 原 base `d600b14` rebase 到含 PR #33 的 main 後,新 call site 的 `calculator.SanitizeFileName` 變 undefined symbol(rebase 文字三方合併不報、`go build` 才抓到);遂把 `csv_handler.go:WriteNormalizedPhaseSyncResult` 的新 site 改為 `filename.Sanitize`(local var `filename`→`fname` 避免遮蔽 package,對齊 sibling `WriteCCIResult`/`WriteMuscleRatioOutput*`),其餘 pre-existing site 由 rebase 取 C1 版本。`emg_statistics.go` import 衝突手動解(留 `filename`、丟 `fsperm`——唯一用戶 `ExportToCSV` 已由本 PR 刪除)。**decoupling 假設成立,只是落地順序反轉為 C1-先**;memory `feedback_cross_check_report_vs_code` / `feedback_scan_parallel_forks_before_finish` 的收尾開場 cross-check(親跑 `git rev-parse main`)攔截了 handoff「rebase = no-op」的失效前提。
