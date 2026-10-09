# musclemap 擁有右側 EMG header → 標準肌肉 → 必要通道表

**Status**: accepted · **implemented** (2026-10-09)

`internal/musclemap` 新增 `RightSideChannels(headers) (map[string]string, error)` 與 `*MissingMuscleError`，成為「右側 EMG header → 標準肌肉短名 → 8 條必要通道」的唯一擁有者。`cci` 與 `muscle_ratio` 各自攜帶的 `shortNameMap`、"R." header 解析、必要肌肉清單全部刪除。本 ADR 為架構重構 W1「清場」的一部分。

## Decision

- `musclemap.RightSideChannels` 接手全部規則：只認 "R." 前綴且含 ":" 的 header（L.* 與其他格式略過，不論排序）；`R.<名稱>` 後綴轉大寫查 `shortNameMap`（`TA&IO` 與 `TAIO` → `TAIO`、`GMAX` → `GMax`）；同一短名出現兩次即 fail-fast（`重複的肌肉通道 …`）；RA/ES/IL/GMax/RF/BF/TAIO/MF 任一缺失回 `*MissingMuscleError{Muscle}`（`Error()` = `缺少必要的肌肉通道: <名>`）。
- 回傳方向為 **short → header**：兩個 caller 都是「以肌肉名查 `PhaseSyncEMGData.Channels` 的 header」（`ComputeAllRatios`、CCI pair 抽取），下游零調整。
- `AssignShort` 改為 unexported `assignShort`（package 外已無使用者）。
- `muscle_ratio.BuildRightSideChannelMap`、`muscle_ratio.mapHeaderToRightShortName` / `lookupShortName`、`cci.MapHeaderToShortName` 刪除；`muscle_ratio` analyzer 直接呼叫 `musclemap.RightSideChannels`（錯誤文字與舊版逐字相同）。
- `cci.BuildChannelMap` 保留為薄轉接：`errors.As(err, *MissingMuscleError)` → `i18n.T(KeyErrorCCIMissingMuscleChannel, muscle)`（訊息不變、仍隨 locale）；其他錯誤（重複）原樣回傳。
- 修正兩個 package 中互相描述錯誤的註解（過去宣稱 cci「silent overwrite」、muscle_ratio「與 cci 不同」，兩者實際行為早已對齊）。
- 測試：`musclemap_test.go` 以表格涵蓋 CanonicalNames / IgnoresNonRight / RightWinsInterleaved / DuplicateFailFast / MissingIsTyped；刪除被取代的舊映射測試（cci 8、muscle_ratio 3、musclemap `AssignShort` 4）；cci 保留 en-US i18n 測試並補一則 zh-TW 文字測試。

## Why

- 兩份複本靠註解互相宣告「對稱」，沒有任何機制保證；要改一條肌肉規則得動兩處且測試也重複兩處。
- 兩 caller 的映射、重複偵測、interleaving（R 優先於 L）、必要集合經核對完全一致，唯一差異是缺失錯誤文字（cci 走 i18n、muscle_ratio 固定 zh-TW）—— 這正是 typed error 可吸收的部分。
- 抽出後 interface 小（一個函式 + 一個 error type）、實作深（解析、正規化、去重、必要性檢查），符合 deep module 取向；測試集中在該 interface。

## Considered Options

### A. musclemap 擁有整張表，caller 轉譯 typed error（chosen）

見上。

### B. 只共用 `AssignShort`（現狀，rejected）

重複偵測已共用，但 `shortNameMap`、header 解析、必要清單仍兩份，是真正會分岔的部分。

### C. 回傳 header → short 方向（rejected）

兩 caller 的下游都以肌肉名查 header，反向需要處處反查或轉置，沒有收益。

## Consequences

- 行為與輸出逐位元組不變（cci / muscle_ratio 的 golden 基線不受影響）。
- 日後新增肌肉或別名只改 `musclemap`。
- `cci.BuildChannelMap` 仍為 exported（目前 caller 為 `AnalyzeCCI` 與測試），是否內聯留待後續 wave。

## Related

- [[ADR-0033]]（同一 wave 的 streaming 清除）
