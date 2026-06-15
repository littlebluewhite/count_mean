# 把 atomic-write orchestration 收進 payload-agnostic deep module `fsperm.AtomicWriteFile`,CSV/JSON 變 thin adapter,根因消除 config stale-`.tmp` P2

**Status**: accepted · **implemented** (2026-06-15)

本 ADR 是 2026-06-15 `/improve-codebase-architecture` Candidate 1 grilling 後的結論。atomic `tmp → fsync → rename` 寫入在 codebase **三層深**,而最上層 consumer 自行 roll 了一份壞掉的副本:

1. **深原語(好)**:`fsperm.OpenAtomicWriteValidated` — dirfd / openat2 / `O_NOFOLLOW`,回 `AtomicWriteHandle{File,Commit,Abort}`,已 payload-agnostic。
2. **CSV orchestrator(好但與 CSV 糾纏)**:`csvutil.WriteCSVAtomic` — crypto-random tmp + open(validated-or-legacy)+ sync + commit/abort + parent-fsync,但焊死在 CSV concern(BOM/header/sanitize/mid-write probe)上,非 CSV caller 無法複用。
3. **config 副本(壞)**:`config.SaveConfigAtomic` — **繞過**原語、把整套舞蹈**重寫得更差**:固定 `tmp := filename + ".tmp"`(config.go:228)配 O_EXCL,前次 crash 留下的 `config.json.tmp` orphan 會讓下次 open 回 `EEXIST` → **存檔被永久 block** 直到手動刪 orphan(`CODE_REVIEW_2026-06-15.md` 的 P2);更有一段 **lying comment**(宣稱加 PID/entropy 後綴,程式碼一個都沒加)。

本案把 **payload-agnostic 的編排**抽進單一 deep module `fsperm.AtomicWriteFile`,`WriteCSVAtomic` 與 `SaveConfigAtomic` 退化為只描述自己位元組的 thin adapter。硬化(crypto-tmp + 整套耐久放置協定)寫一次;stale-`.tmp` bug class 變**結構上不可能**——config 不再擁有任何 tmp/rename 邏輯可寫錯。

## Decision

新增 `internal/security/fsperm/atomic_write_file.go`,package-level 函式:

```
AtomicWriteFile(path string, basePaths []string, write func(io.Writer) error) error
```

它擁有完整耐久放置協定:`makeTmpPath` → branch(`len(basePaths)>0` 走 `OpenAtomicWriteValidated`,否則 legacy `os.OpenFile(TmpCreateFlags)`)→ `write(file)` → `file.Sync()` → `file.Close()` → commit(dirfd 走 `handle.Commit()`,legacy 走 `os.Rename` + `SyncParentDir`)。`makeTmpPath`/`truncateToBytes` 連同 `tmpRandSuffixLen`/`fsNameMaxBytes` 兩 const 從 csvutil **逐字下移**進此檔(保持 private)。

兩條 grilling crux 的結論:

**1. seam shape = Option A(抽 orchestrator),非 Option B 最小 in-place fix。** 「config 副本是刻意 self-contained」的反對被 lying comment 證偽:它是 **drifted-and-wrong**,不是 deliberate-and-clean。deletion test 過:恰 2 個 adapter(CSV + JSON)= 真 seam。

**2. config dirfd-anchoring = NO;`basePaths=nil` 走 fallback 分支。** `config.json` 是使用者自有檔、落在 app-support 目錄;parent-swap dirfd 防的 TOCTOU 不在單機桌面威脅模型內,且 anchoring 等於把 config 自己的 parent 對自己 whitelist。fallback(crypto-tmp + `TmpCreateFlags` 的 `O_NOFOLLOW` + atomic `os.Rename` + parent fsync)是恰當層級——且嚴格優於今日。config 仍保留 `MkdirAll`(path 前置條件、非 orchestration)。

## Why

- **深模組 + 真 seam。** 抽出的介面(`path` + `basePaths` + 一個 `func(io.Writer) error` closure)遠小於它藏住的實作(crypto-tmp 熵預算截斷 + validated/legacy 雙分支 open + sync/close 順序 + commit/abort + parent-fsync + dirfd 生命週期)。CSV 全部 concern(BOM/header/sanitize/mid-write probe/writerWrapHook)留在 closure → **byte-identical**;JSON 只交 `json.NewEncoder(w).SetIndent("", "  ").Encode(c)`。
- **deletion test 強(concentrate 非 move)。** 刪 `AtomicWriteFile` → 同一段 tmp/open/sync/rename 協定原封回到 **兩個** caller(且 config 那份會再次寫錯)。對照 [[ADR-0005]]/[[ADR-0011]]/[[ADR-0012]] 的「relocation-only = pass-through 被拒」判準:本案是真行為的集中(兩 caller 都跨同一介面),故過 test。
- **bug class 從「靠註解祈禱」變「結構不可能」。** 與 [[ADR-0026]] 的 snapshot 不變式升級同範式:config 失去自有 tmp 命名後,固定名 O_EXCL 撞名永久 block 的 P2 在結構上無法重現。
- **lying comment 是決定性洞察。** config 的「30 行 self-contained 比拉 csvutil 還省事」+「加 PID 後綴 entropy 給足」皆與程式碼矛盾(無 PID、固定 `.tmp`)。這證明它是漂移後的錯誤、非刻意的乾淨副本,deepening 勝過純 bug-fix。

## Considered Options

- **Option B — config 直接用 `OpenAtomicWriteValidated` handle API。** 拒:config 仍須自建 crypto-tmp 路徑(等於把 `makeTmpPath` 提出 csvutil — 那已是 Option A 的迷你版),且 config 無 basePaths 會落到 legacy 分支,只得 crypto-tmp 不得 dirfd,徒增耦合不解問題本質。
- **給 config dirfd-anchoring(basePaths=config 自己的 parent)。** 拒:見 Decision §2,self-whitelist 循環、單機威脅模型外。
- **留 config 副本 + 記「deliberate self-contained」。** 拒:lying comment 證偽前提;留著 = P2 永存 + 兩份漂移的耐久寫入邏輯。
- **把 CSV/JSON 都改去呼 `WriteCSVAtomic`。** 拒(這是 grill handoff 點明的誠實 objection):`WriteCSVAtomic` 對 JSON 是錯形狀(帶 BOM/header/sanitize)。正解是在**兩者下方**抽 payload-agnostic 層,非讓 JSON 套 CSV。

## Consequences

- **不改 [[ADR-0016]] / [[ADR-0020]] 的「Subject-based write ⟹ atomic」不變式。** 那條講的是 CSVHandler 的**輸出所有權**(PhaseSync / NormalizedPhaseSync / CCI / MuscleRatioOutput* 走 `WriteCSVAtomic`);`config.json` 不是 subject-based CSV writer,從不在那條 arc 上。ADR-0027 抽的是 `WriteCSVAtomic` **底下**的共用**機制**,該不變式原封不動。
- **byte-identical 輸出**(by construction:CSV closure 與 JSON encoder 設定皆未動,唯一變動是 transient tmp 檔名 固定 → crypto-random)。`internal/io/csv_handler.go` 的 5 個 CSV writer(`phaseSyncAtomicWrite`/`WriteCCIResult`/`WriteMuscleRatioOutputAll`/`WriteMuscleRatioOutputPhases`/`WriteCCIPhasesResult`)全經 `WriteCSVAtomic`,輸出零變動。
- **Test surface**:
  - **新增** `internal/config/config_atomic_test.go` — round-trip、parent auto-create、**鑑別性 P2 回歸**(預植固定名 `config.json.tmp` orphan → `SaveConfigAtomic` 必須成功;b32eabf 上 RED=EEXIST、本案後 GREEN)。config 此前**無** package 內直接單元測試(只有 `gui/app_save_config_test.go` 間接覆蓋)。
  - **新增** `internal/security/fsperm/atomic_write_file_test.go` — fallback / validated 輸出、write-callback error 清理(fallback + dirfd 兩分支)、`ErrPathEscapesBase` 經 `%w` 的 sentinel 保留、`[]string{}` 走 fallback 非 `ErrBasePathsEmpty`、overwrite。
  - **新增** byte-identity golden:`config_byteid_test.go`(交叉用 `json.MarshalIndent`+`\n` 鎖 JSON bytes,補 round-trip 正規化的盲點)、`safe_writer_byteid_test.go`(對固定 golden 鎖 CSV bytes,補跨分支對比「兩分支同錯」的盲點)。
- **stale 註解 reword**(makeTmpPath 移家後):`atomic_write.go`(2 處)、`flags_unix.go`、`atomic_write_windows.go`、`atomic_write_test.go`。
- **import hygiene**:`safe_writer.go` 移除 `crypto/rand`/`encoding/hex`/`os`/`path/filepath`/`unicode/utf8`;`config.go` 加 `io`。
- **`error` 訊息位移**(非 user-facing 行為):open/sync/close/rename 的中文 `%w` wrap 移進 orchestrator;`ErrPathEscapesBase`/`ErrBasePathsEmpty` sentinel chain 經 `%w` 保留。

## Reversibility

中。回頭要把 `AtomicWriteFile` body 重新內聯回 `WriteCSVAtomic`、config 重寫自有 tmp/rename(並重新引入 P2)、`makeTmpPath`/`truncateToBytes` 移回 csvutil、reword 註解回去、刪新測試。重識別動機需重走 grilling。

## Related

- [[ADR-0016]] — 「Subject-based write ⟹ atomic」invariant;本案抽其底層**機制**為 deep module,**不**動該輸出所有權不變式。
- [[ADR-0017]] — validated-read/open consolidation;本案的 validated 分支複用其 `OpenAtomicWriteValidated` dirfd 原語,未改。
- [[ADR-0020]] — normalized-EMG write via CSVHandler;其 atomic 寫入經 `WriteCSVAtomic`,byte-identical 保持。
- [[ADR-0026]] — Max-mean batch runner;同範式(把不變式從「註解維持」升級為「結構保證」),本案是寫入層的對應。

## Notes

實作 as-built(2026-06-15,branch `worktree-atomic-write-deepening-adr0027`、基於 main `b32eabf`;統合 main agent + 多 subagent waves):

- **與設計一致**:`AtomicWriteFile` 簽章、`makeTmpPath`/`truncateToBytes`/2 const 下移、CSV/JSON thin adapter、config `basePaths=nil`。`safe_writer.go` 淨 −247 行、`config.go` 淨 −40 行,orchestration 收斂為單一 deep module。
- **多 agent 編排**:Wave 1 sonnet 寫 config RED baseline(觀察到 P2 RED 才動 config.go,TDD fail-first)→ Wave 2 sonnet 核心重構(coherent 編譯單元,單一 agent)→ Wave 3 sonnet 寫 fsperm orchestrator 測試 → Wave 4 opus 唯讀對抗審查(byte-identity / 行為對等 / cleanup / sentinel 全 HOLDS)+ 統合者親跑全套 gate。
- **opus review 抓 1 個 CI-failing lint**:重構**改善型別**反而引入 staticcheck **ST1023**(`var underlying io.Writer = w` — main 上 RHS 是具體 `*os.File` 故標註必要,抽出後 `w` 已是 `io.Writer` 故標註冗餘)→ 改 `var underlying = w`。opus 並點名最有價值缺測(validated 分支 callback-error 清理),已補。
- **codex×3 不重疊切角**:R1 default(clean)/ R2 security+cleanup(clean,fd/dirfd 洩漏、TOCTOU、sentinel 全保持)/ R3 byte-identity+測試品質(3×P3:config byte golden + CSV byte golden 已補;1024-probe 有 `safe_writer_p1_14_test.go` 覆蓋故 dismiss;**fdNone fallback + 非空 basePaths 缺測 dismiss**——orchestrator 無 fdNone 專屬分支、邏輯在未改的 primitive 內、平台閘控不可攜強制,列 follow-up)。
- **gate**:`go build ./...`、`make test-unit`/`test-int`/`test-race`(33 套件、零 race)、`make lint`(0 issues,musclemap err113 為已刪 sibling worktree 的陳舊快取雜訊,`golangci-lint cache clean` 後消失)全綠。
- **CONTEXT.md 不動**:`AtomicWriteFile` 是 architecture-layer(seam/adapter/depth)概念,非 domain 詞;`Format-aware write` 講輸出所有權,與本案正交。
- **GUI smoke 未驗**(config save 經 `gui/app.go` `App.SaveConfig` → native webview,headless 跑不了;比照慣例可授權無 smoke 直接 merge)。
