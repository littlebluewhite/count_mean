# 把三份 byte-identical 遞迴 symlink-fallback resolver 合一為 `fsperm.EvalSymlinksWithFallback`,根因消除重複與合約歧散

**Status**: accepted · **design locked** (2026-06-15)

本 ADR 是 2026-06-15 `/improve-codebase-architecture` Candidate 2 grilling 後的結論。codebase 有三份幾乎 byte-identical 的遞迴 symlink-fallback resolver,各自獨立演化:

1. **`evalSymlinksLenient`**(security/lenient\_path.go) — 深度上限 8,`("", nil)` 回傳空字串成功。
2. **`resolveSymlinksWithFallback`**(security/pathvalidator.go) — 無深度上限,`("", nil)` 回傳空字串成功。
3. **`evalSymlinksWithFallback`**(fsperm/validated\_open.go) — 無深度上限,空字串回 `error`。

三份的演算法核心完全相同:`filepath.EvalSymlinks(path)` 成功則回;失敗則對 `filepath.Dir(path)` 遞迴並在尾部拼回 `filepath.Base(path)`;當 `filepath.Dir(p) == p`(抵達 filesystem root)時 fail-closed 終止。合約歧散的根源是各自獨立複製而非共用。

## Decision

在 `internal/security/fsperm/perm.go` 新增一個 **exported** 函式:

```
EvalSymlinksWithFallback(path string, maxDepth int) (string, error)
```

演算法不變。`maxDepth <= 0` 表示無上限;`maxDepth > 0` 表示深度防禦上限。空字串 `path` 統一回 `error`(fail-closed)。

**定居位置 = `fsperm`**。`security` package 引入了 `fsperm`(`lenient_path.go` 呼叫 `fsperm.OpenReadValidated`);`fsperm` 不可反向引入 `security`。把正典 resolver 放進 `fsperm` 讓 `security/*` 向下委派,進口循環在結構上不可能。正是「為什麼 copy #3 要自己一份」的循環限制,指名了 `fsperm` 是家。

**`maxDepth` 是參數,不是強制性的終止機制**。遞迴終止由 root base case 保證(非上限)。深度上限是**縱深防禦**,服務 lenient 路徑——該路徑走 adversarial manifest 提供的路徑。呼叫點分配:lenient site 傳 `8`;strict validator(`ValidateExternalPath`)與三個 fsperm open site 傳 `0`(無上限)。傳 `0` 無新增 DoS 暴露:strict validator 在 resolver 前已過長度上限 4096(`performBasicSecurityChecks`);三個 fsperm open site 則與舊 copy #3 同為 unbounded(行為保持、無回歸),遞迴仍由 root 終止、實務上受實際路徑長度約束。

**空字串合約 = error**。這是三份分歧合約的統一,行為保持:

- Site #1/#2/#5 在進 resolver 前已構造非空路徑,`("", nil)` 分支從未被觸及(moot)。
- Site #3/#4 由 caller 直接傳入路徑、無前置空字串檢查,但 copy #3 已對空字串回 `error`——完全等價。

**Scope**:5 個 rewire site;刪除 copy #1 和 copy #2;將 copy #3 提升為 `perm.go` 中的 exported 函式;新增直接單元測試(resolver 此前只透過上層 caller 間接覆蓋)。測試包含兩道深度上限驗證:(1) 鑑別性測試——同一條遠超 cap 的路徑,`maxDepth=8` 觸發上限 error、`maxDepth=0`(unbounded)正常解析,證明 cap 真的生效(非「反正在 root 終止」的假通過);(2) off-by-one 邊界測試——同一 `maxDepth=8` 下,cap−1 層成功、cap 層 fail-closed,釘住上限的精確位置。

## Why This Escapes the Evaluated-Not-Adopted Family

此 repo 已三次拒絕整合:

- **ADR-0011**(dual `parseDataRow`) — 拒:interface-widening,兩個 parse 形狀根本不同。
- **ADR-0012**(domain analyzer) — 拒:兩軸命名要求的 cardinality/output-ownership 差異,共用需型別開關。
- **ADR-0023**(CSV reader) — 拒:error-wrap 四種 + `ReuseRecord` + buffer-size 三來源形狀差異,沒有真 seam。

三次拒絕的共同根因是:模組是**發散形狀**,共用等於 **interface-widening**——需要型別開關或 `interface{}`。

本案相反:**三份 body byte-identical**;唯一差異是 `maxDepth` int 參數和空字串的小差異。統一後的簽章 `(path, maxDepth)` 比任何一份副本都更**窄**,不是更寬——無型別開關、無 `interface{}`、無行為聯集。這是 **dedup**,不是 interface-widening。deletion test:刪除 `EvalSymlinksWithFallback` → 同一套 try-parent-rejoin-or-fail-closed 協定完整回到至少 3 個 caller。真 seam,過 test。

## Considered Options

- **Option A — `maxDepth` 參數化(選定)。** lenient 傳 `8`(adversarial manifest 縱深防禦);strict validator 與 fsperm open site 傳 `0`(strict 上游有 4096 length cap;fsperm open site 與舊 copy #3 同為 unbounded、root 終止)。cap 是縱深防禦而非終止保證,與 signature narrower 並行。
- **Option B — 強制 cap,所有呼叫點統一上限。** 拒:strict 路徑的長度 cap 已存在於 resolver 上游,加第二層上限為重複;更重要的是上限並非終止所需,強制等於在文件上謊報終止機制。
- **把正典 resolver 放進 `security/`。** 拒:會引入循環(`security` import `fsperm`、`fsperm` import `security`)。進口循環是 copy #3 存在的根本原因——不解決它就解決不了問題。
- **保持三份各自獨立,只統一合約(用個別改動)。** 拒:解決合約歧散卻保留演算法漂移風險;未來任何一份獨立演化就又回到 P2 狀態。

## Consequences

- **`pathvalidator.go` 獲得 file-level `fsperm` import**:外觀上新增,但該 package 已間接依賴 `fsperm`——只是依賴現在顯式化。
- **error 訊息前綴一般化**:從 `"evalSymlinksLenient:"` 改為 `"evalSymlinks:"`。無測試斷言此前綴(鑑別/參數值測試僅斷言 cap 值如 `(8)`/`(3)`、不依賴前綴),故前綴可自由變更,接受。
- **新增直接單元測試**(`fsperm/eval_symlinks_test.go`):resolver 合約從此有直接 pin——成功解析(existing dir / non-existent leaf fallback / parent symlink / existing symlink leaf)、深度上限鑑別 + off-by-one 邊界 + 參數值釘定(maxDepth=3)、空字串 error 等子測試。root-unresolvable 分支為防禦性程式碼(正常系統不可達)、刻意不強求脆弱的非可攜測試。
- **5 個 rewire site**:lenient\_path.go、pathvalidator.go、validated\_open.go(2 site)、atomic\_write.go。
- **與 [[ADR-0027]] 的檔案重疊**:`atomic_write.go` 的 site #5 落在 ADR-0027 修改區;實作時須在 ADR-0027 落地後 rebase 或基於含 0027 的 main。

## Reversibility

中。回頭要把 `EvalSymlinksWithFallback` body 重新內聯回三個呼叫點(或保持兩份,讓循環限制逼出 copy)、rewire 5 個 site、刪直接測試、還原空字串 `("", nil)` 合約至 site #1/#2。重識別動機需重走 grilling。

## Related

- [[ADR-0017]] — validated-read/open consolidation;整合了「開門」seam 與寫側 dirfd atomic,但 resolver 仍三份。ADR-0028 完成 0017 未能到達的部分,不衝突。
- [[ADR-0027]] — atomic-write file deep module;主要修改區域的 `atomic_write.go` 含本案 rewire site #5;兩案正交,實作時注意落地順序。
- [[ADR-0011]] / [[ADR-0012]] / [[ADR-0023]] — evaluated-not-adopted 家族;本案明確區隔於此三案(byte-identical → dedup 而非 interface-widening)。
