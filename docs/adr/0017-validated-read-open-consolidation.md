# 讀側 validated-open consolidation + #34 寫側硬化(整族收斂到單一 hardened 入口)

**Status**: accepted (2026-05-30, design) · **implemented (2026-05-31)**(10 commit `acc46d7..df95a5b`;subagent-driven + 逐階段兩階段審查 + Docker mutation;Phase E 經 impl cross-check 翻案為 doc-only exclusion,見 pt8)· 是 [[ADR-0016]] caveat 點名的 **#34 follow-up** + 其**讀側孿生**。緣起見 memory `project_phasesync_lenient_percent_2026_05_30`、`project_c2_phasesync_atomic_2026_05_29`。

## Context

`%` bug(phase_sync 漏遷 `security.ResolveLenientPath`、與 cci/muscle_ratio 分岔)的 root cause 是 **manifest 讀取路徑沒有單一 hardened 入口**:cci/muscle_ratio 走 `manifest.ResolveEMGFile`,phase_sync 與 chart_composer **各自手刻** `ResolveLenientPath`。codex 在 PR #38 review ×2 兩輪各撈到讀側 symlink TOCTOU(`ResolveLenientPath` 回 lexical + parser 用 `fsperm.ReadFlags` 開,validation-vs-open 之間有縫)。

**開場 cross-check 翻掉 handoff 前提(見 Process note pt1)**:`fsperm.OpenReadValidated` 早已完整存在(Linux `openat2` / Darwin `O_NOFOLLOW_ANY` / Windows fallback + 測試),只是**零 production caller**。所以本案不是「設計 helper」,是「把 read callers 接上既有 kernel + consolidation」。

## Decision

1. **單一讀取門 `manifest.OpenDataFile(baseFolder, file) (*os.File, error)`** 為開啟 manifest-referenced data 檔(EMG/Motion/Force/muscle_ratio)的**唯一入口**。cci / muscle_ratio / phase_sync / chart_composer 全部收斂於此。回傳「已開、已驗」的 `*os.File`,caller `defer Close` 後交 parser。

2. **Compiler-enforced anti-drift invariant**:`security.ResolveLenientPath` → **unexport** 為 `resolveLenientPath`;新增 `security.OpenLenientValidated(baseFolder, file) (*os.File, error)` 為融合「`resolveLenientPath`(lexical 清洗 + advisory boundary)+ `fsperm.OpenReadValidated`(atomic 開 resolved)」的唯一 security 入口。`manifest.OpenDataFile` 是其薄 wrapper,負責映射 domain sentinel。**phase_sync 結構上碰不到 `resolveLenientPath` → 不可能再漂移。**

3. **刪 `manifest.ResolveEMGFile`(單門)**:其唯一非開檔 caller `ValidateAllEMGFiles`(存在性批檢)改走 `OpenDataFile` open+立即 close(順帶取得 boundary + 可開性驗證,比 `os.Stat` 強)。3 個 pin 測試(PartialMissing/AllPresent/OrderPreserved)隨之改走開檔路徑(回傳 `[]MissingRow` 形狀不變)。

4. **Parser 收 reader**:EMG/Motion/ANC 的 `ParseFile(path)` **取代**為 `Parse(r io.Reader, name string)`(`name` 載 extension + 錯訊 context)。門擁有 `*os.File` 生命週期。ANC XLSX 改走 `excelize.OpenReader(f)` —— **順帶補掉 `excelize.OpenFile(path)` 完全繞過 fsperm(連 Unix O_NOFOLLOW 都沒有)的洞**。phase_sync `parseEMGFileFnPtr` test hook 改 reader-based。`ReadCSVDirect`→reader 版 record reader。

5. **io-tier 讀取站點維持現狀,排除於 `OpenReadValidated` 之外(impl-time cross-check 翻案,見 Process note pt8;user 核准 2026-05-31)**:設計階段原規劃把 `csv_handler.go:217`、`large_file_handler.go:238/367` 的 `os.OpenFile(path, fsperm.ReadFlags)` swap 成 `fsperm.OpenReadValidated(path, GetAllowedBasePaths())`(對稱寫側 `OpenWriteValidated`)。impl cross-check 發現此三站點被 `ReadCSVExternal`(`external:true`,file-dialog 使用者選任意檔、gui live caller)共用,swap 會 `ErrPathEscapesBase` 誤擋落在 base 外的合法外部讀取 + 破 `external_user_dir_allowed` 既有測試 → **與 `config.go` 同屬「無天然 base、接受任意 caller path」,套用 pt4 同先例排除**。三站點維持 `os.OpenFile + fsperm.ReadFlags`(已帶 O_NOFOLLOW;strict 讀取另經 allowlist pre-validation 守門)。**`config.go` 亦不動**(原 documented 拒用:無天然 base、改用 Lstat-lite leaf-rejection)。Phase E 收斂為純 doc 修正、無 code 變更;manifest-read 單門(Decision 1-4)不受影響。

6. **#34 寫側折入,dirfd-anchored(maximal)**:`WriteCSVAtomic` 經 `SafeWriteOptions.BasePaths` 穿入 basePaths;把 validated parent 開成 `O_DIRECTORY` dirfd,`openat2` tmp(`TmpCreateFlags`)+ `renameat` 全 relative to dirfd —— **連 parent-swap race 都關**。平台感知(Linux/Darwin dirfd,Windows fallback)。#34 目前**不可達**(所有 caller `SubDir=""`),屬 precautionary defense-in-depth。寫側是 **twin 非新 door**:輸出檔名由 Subject 推導、不經 lenient resolution,無漂移可治。

7. **跨平台 symlink 政策一致**:Linux `openat2` 加 `RESOLVE_NO_SYMLINKS`(read ReadFlags / write WriteFlags / tmp-create TmpCreateFlags **三個 openat2 站點共吃**),對齊 Darwin `O_NOFOLLOW_ANY` 在 TOCTOU-race window 的嚴格度。**test-compatible**:合法 in-base symlink 在 open 前已被 `evalSymlinksWithFallback` 解析掉,openat2 看不到 symlink(見 Process note pt3)。

8. **Windows kernel-atomic 明確 deferred**:讀寫在 Windows 仍是 caller-side EvalSymlinks + 普通 open(殘餘 swap-window TOCTOU)。真正 closure 需 `windows.CreateFile + FILE_FLAG_OPEN_REPARSE_POINT` + Windows CI runner —— **本專案不立 Windows CI**,沿用 `flags_windows.go` package doc 的 "Future work" 追蹤。

## Why

- **單門 + unexport = root cause 結構性根治**。`%` bug 來自手刻分岔;唯一入口 + compiler 擋外呼,讓「下一個 phase_sync 漂移」不可能,而非再寫一條 convention(convention 已失敗過一次 = 本案存在的原因)。
- **kernel 設計已付清**:`OpenReadValidated`(openat2/O_NOFOLLOW_ANY/*os.File 契約)早寫好且測過,只缺接線。consolidation 是 headline、Linux/mac kernel-atomic symlink 安全是順帶(memory Q1)。
- **讀寫一次做完 + 共用 kernel**:折入 #34 寫側 twin,讓 `RESOLVE_NO_SYMLINKS` 一次改、三 openat2 站點(讀/寫/tmp)受惠;讀側先立 pattern(`OpenReadValidated` 門),寫側 dirfd-anchored 鏡像,kernel 對稱。
- **ANC XLSX hole 順帶補**:`excelize.OpenFile` 是所有讀路徑保護最弱者(無 fsperm),改 `OpenReader(validatedFile)` 零成本納入守門。

## Considered Options(每個分叉的選擇 vs 拒案)

- **Seam 形狀**:✅ fused open 門(parser 收 reader)｜拒「原地換 open primitive(resolve 留兩步)」｜拒「只收斂 resolve、hardening 全 defer」。選最強 consolidation。
- **門數量**:✅ 單門 `OpenDataFile`(刪 ResolveEMGFile、pre-check 改 open+close)｜拒「兩門一核」｜拒「保留 EMG 舊名」。
- **Invariant 強制**:✅ compiler-enforced(unexport `resolveLenientPath`、door 落 security)｜拒「CI grep guard-test」｜拒「convention/doc」。**「搬進 manifest 做 compiler 強制」被 helper census 否決**(`lenient_path.go` 拖 3 helper + 依賴 security 常數,搬走碎裂跨包);改原地 unexport + door 落 security,零碎裂。
- **Parser 簽章**:✅ `Parse(io.Reader, name)`(最可測、脫鉤檔生命週期)｜拒 `Parse(*os.File)`(耦合、難 fake hook)｜拒「並存 ParseFile」(死路徑 + 雙入口 drift)。
- **Symlink 不對稱**:✅ 強制一致(Linux 加 `RESOLVE_NO_SYMLINKS`)｜拒「接受不對稱」。不對稱本是 security-equivalent,但既然 #34 已動 kernel,順手對齊 race-edge 廉價且 strictly safer。
- **#34 範圍**:✅ 折入本案(讀寫一次做)｜拒「defer 成獨立寫側 follow-up」。
- **#34 深度**:✅ maximal dirfd-anchored(連 parent-swap race 關)｜拒「pragmatic(tmp-create validated + rename within resolved parent,留窄殘餘)」。

## Reversibility

**中-低**。unexport `resolveLenientPath` + 刪 `ResolveEMGFile` + parser `ParseFile→Parse(reader,name)` + `WriteCSVAtomic` 簽章(+BasePaths)散落多 caller;回頭要 re-export / re-add / re-thread。但 kernel(`OpenReadValidated`/`OpenWriteValidated`)預先存在、設計內聚,invariant 單一。Windows kernel-atomic 是獨立可疊的 future layer(不需回退本案)。

## Related

- [[ADR-0016]] — 本案是其結尾點名的 **#34 follow-up** + 讀側孿生;0016 把寫側降為 lenient posture 並留 caveat,本案把讀寫雙側拉回 validated-open kernel。
- [[ADR-0004]] — Subject-based(輸出檔名由 Subject 推導)vs File-based 軸;#34 寫側 twin 在 Subject-based 家族,故無 manifest-resolution 漂移。
- [issue #34](https://github.com/littlebluewhite/count_mean/issues/34) — 寫側 `WriteCSVAtomic → validated-open` 硬化;本案折入並升級為 dirfd-anchored。
- memory `feedback_cross_check_report_vs_code`(開場 cross-check 翻掉 handoff)、`feedback_adr_number_collision`(write 當下再驗 0017 空號)、`feedback_handoff_after_design`(設計完 handoff fresh agent 做 impl)。

## Process note — cross-check / framing 修正

grill-with-docs 開場 mandatory cross-check(memory `feedback_cross_check_report_vs_code`)+ 過程親讀測試,抓到:

1. **handoff 前提作廢**:handoff 框成「設計新 validated-read helper、攻克 Windows O_NOFOLLOW 等價」;親讀 `internal/security/fsperm/validated_open.go` 發現 `OpenReadValidated` **已完整存在**(讀寫雙胞胎、含平台 dispatch + 測試),零 production caller。handoff mental model 落後 code(它知 `OpenWriteValidated`、漏了同檔的 `OpenReadValidated`)。專案從「設計」收斂為「接線 + consolidation」。

2. **handoff headline 不可達**:「關掉讀側 Windows TOCTOU」做不到 —— Windows kernel-atomic 結構性 deferred(需 `windows.CreateFile` + Windows CI),與 #34 同一 blocker。改框為「Linux/mac kernel-atomic + consolidation;Windows defense-in-depth deferred」。

3. **Q7 framing 自我修正**:我初框 Linux/Darwin symlink 不對稱「有 usability 代價(破壞 symlink-subdir 用例)」。親讀 `TestOpenWriteValidated_AcceptsInternalSymlink`/`OpensResolvedPath` 後修正 —— 合法 in-base symlink 在 open 前已被 `evalSymlinksWithFallback` 解析,openat2 看到的是 real path,故 `RESOLVE_NO_SYMLINKS` **不破壞**這些測試;不對稱**只在**對抗性 TOCTOU-race-harmless-swap edge(逃逸 base 兩平台本來就拒)。原「niche usability 代價」是錯的;user「強制一致」選擇 cross-check 證實成立且廉價。

4. **config.go 拒用先例不適用**:config 拒 `OpenReadValidated` 是因「無天然 base、接受任意 caller path」;analyzer 手上握 `baseFolder`(天然 allowed-base)→ 那個拒用反而是綠燈。

5. **ValidateAllEMGFiles 是活的 existence-only caller**(chart_composer:193,3 pin 測試)→ 單門選擇把它改 open+close 批檢。

6. **ADR 編號**:write 當下 re-`ls docs/adr/` 確認 0016 最高 → 0017;`origin/docs/adr-0009-png-download-seam` 是已 merge 孤兒 branch、非 0017 競爭者;無平行 worktree。

7. **chart_composer 是第二漂移站**(`chart_composer_handlers.go:524` 自刻 `ResolveLenientPath`)—— 漂移非 phase_sync 獨有,強化單門價值。

8. **Phase E io-tier swap 被 impl-time cross-check 翻案**(user 核准 2026-05-31):Decision 5 原規劃假設 io 讀取路徑 base-bound。impl 親讀 `csv_handler.go` 的 `readOptions{external}` 雙 regime 發現:`external:true`(`ReadCSVExternal` → `readCSVCore`:302 走 lenient `ValidateExternalPath` → `readAndParseCSV`:217)服務 **file-dialog 使用者選的任意路徑**(live caller `gui/app.go:685`、`gui/file_helpers.go:41`),而 `ValidateExternalPath` 的本意正是**放行 base 外路徑**(只擋 NUL/traversal/敏感目錄/symlink-escape)。把 :217/:238/:367 swap 成 `OpenReadValidated(GetAllowedBasePaths())` 會 `ErrPathEscapesBase` 誤擋這些合法外部讀取,並破既有 pin 測試 `csv_handler_path_validation_test.go:67 external_user_dir_allowed`(以第二個 `t.TempDir` 模擬外部選檔、`require.NoError`)。此情境與 pt4 的 config.go 拒用理由**同構**(無天然 base、接受任意 caller path)—— handoff/設計把 io 站點誤判為 base-bound。**Resolution**:io-tier 讀取排除於 `OpenReadValidated`(同 config.go 先例),三站點維持 `os.OpenFile + fsperm.ReadFlags`;Phase E 收斂為本 doc 修正、零 code 變更。headline 的 manifest-read consolidation(Decision 1-4 / Phases A-D2b)完全不受影響。

9. **muscle_ratio 在 chart_composer 不 "tolerate missing"**(plan-vs-code 修正):plan 框 chart_composer(~:523)對缺 muscle_ratio 檔「容忍」,實際 code **hard-fail 整張圖**("optional" 僅是 empty-field gate,非缺檔容忍)。行為 preserved(hard-fail);加容忍被當 unrequested scope creep 拒。

10. **EMG 存在性改 validate-time 檢查**(behavior delta):單門 atomic 開檔,舊 lexical resolver 不檢存在性。deliberate、arguably-better;兩個 phase_sync test fixture 校正到更嚴格的現實(verified legitimate、非掩蓋 regression)。

11. **Commit 結構偏離 plan**(intentional):plan 要 fewer/bigger commit,實作拆細為可靠/可審 —— B 獨立於 C、C→C1/C2、D→D1a/D1b/D2a/D2b、F→F-kernel/F-wire。每 commit `go build` 綠。10 code commit `acc46d7..df95a5b`。

12. **舊 `security.ResolveLenientPath` 字面殘留註解**:unexport 成 `resolveLenientPath`(D2a)後,fsperm / manifest / gui 數處 DO-NOT-TOUCH 檔的 prose 註解仍提舊 exported 名(歷史/概念 context、非 code ref)。留待 final doc-sweep / codex 收;避免 scope creep 進無關檔。

13. **#34 寫側 impl 註記**(Phase F):(a) cross-check 確認「atomic writer caller 全 `SubDir=""`」premise 成立 —— `gui/app.go:596` 的 `SubDir!=""` 走非-atomic `WriteCSV` 路徑、不觸 dirfd writer,故 #34 維持 precautionary。(b) `WriteCSVAtomic` 對 `BasePaths` 採 graceful branch:空→legacy `os.OpenFile`+`os.Rename`(byte-identical)、非空→`fsperm.OpenAtomicWriteValidated` dirfd 門;只有 4 個 Subject-based csv_handler writer opt-in(`writePhaseSyncAtomic`/`WriteCCIResult`/`WriteMuscleRatioOutputAll`/`WriteMuscleRatioOutputPhases`),`emg_writer.go:111`(第 5 個 caller、parser-tier 無 base set)留 legacy。(c) `TmpCreateFlags` 帶 `O_EXCL` → tmp 撞名 leaf-symlink 回 EEXIST(非 ELOOP)、target 不被寫穿、安全;crypto-rand tmp 名使撞名近乎不可能。Linux openat2 RESOLVE_NO_SYMLINKS mutation-proven(Docker golang:1.26)。
