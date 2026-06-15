# 移除 numeric.Validator 層與 synchronizer 三個 production 死碼函式

**Status**: accepted · **implemented** (2026-06-15)

本 ADR 是 2026-06-15 全專案 ultracode code review(34 confirmed findings)修復批次中,兩處「exported 但 production 零呼叫者」死碼的移除決策(review **P3-14** numeric / **P3-15** synchronizer)。user 拍板 **完整移除**(非 janitorial、非保留當 library)。

## Decision

### 1. numeric.Validator 層整層移除(P3-14)

`InputValidator`(`internal/validation`)生產活躍 —— `gui/app.go`、`internal/io/large_file_handler.go`、`internal/io/csv_handler.go` 都持有它做 **filename / CSV / directory** 驗證。但它的 **numeric facade 層零 production 外部 caller**(grep 全樹確認:8 個方法只有 `validator.go` 內部委派 + 一個整合測試呼叫)。移除 numeric 層、**保留 InputValidator 本體**:

- 刪整個 `internal/validation/numeric/` 套件(`numeric_validator.go` + `numeric_validator_test.go`)。
- `validator.go`:移除 8 個 facade 方法(`ValidateInteger`/`ValidateFloat`/`ValidatePositiveInteger`/`ValidatePositiveFloat`/`ValidateWindowSize`/`ValidateTimeRange`/`ValidateScalingFactor`/`ValidatePrecision`)+ `numericValidator` 欄位 + `numeric` import + **連帶孤兒**常數(`maxTimeRangeValue`/`maxScalingFactor`/`maxWindowSize`/`maxPrecision` 及其 default)。
- `interfaces.go`:移除 `NumericValidator` interface + `NumericRange` 型別 + `GetSafeNumericRanges` + `safe{Min,Max}{Int64,Float64}` 常數 —— 移除 8 方法後這些全樹零 consumer(`internal/` 套件外部模組無法 import,確定性死碼)。
- 更新消費者測試:`validator_test.go`(刪 2 個 numeric 方法測試)、`test/integration/integration_test_enhanced.go`(拔 `SecurityAndValidationIntegration` 內 2 段 `ValidateWindowSize` 呼叫,保留同 test 的 filename 驗證)。

P3-5(value 型別)與 P3-13(惡意掃描順序)是同一 numeric 層內的次級 finding,**隨碼刪自動溶解**,不另修。

### 2. synchronizer 三函式移除(P3-15)

`internal/synchronizer/time_sync.go` 三個 exported 函式,grep 確認零 production caller:

- `EMGTimeToForceTime`、`ValidateTimeSync`:除各自的專屬測試外無任何引用 → **乾淨整刪**(函式 + 專屬測試 + 連帶孤兒 sentinel `ErrNegativeOffset`/`ErrOffsetExceedsData`)。
- `EMGTimeToMotionIndex`:零 production caller,但另有 3 個測「其他功能」的測試附帶引用它(`ConcurrentAccess` race 載具、`RoundTripConversions`、`MathematicalRelationships`)。**仍移除**,以外科式拔除這 3 處引用、保留各測試對活方法的覆蓋(見 Considered Options 與 Notes)。

`TimeSynchronizer` 本體(`GetSyncedTimeRange` 及其依賴的 `MotionIndexToTime`/`TimeToMotionIndex`/`MotionIndexToEMGTime`/`ForceTimeToMotionIndex`/`MotionIndexToForceTime`/`ForceTimeToEMGTime`)與 `FindNearestTimeIndex` **全留**。

## Why

- **Deletion test 強(complexity 真實消失)。** 兩處刪除後行為原封消失、無任何 N-callsite 位移(對比 [[ADR-0005]] 拒拔 `*FromRawData` 的「14 callsite 位移」反例)。numeric cluster 自閉:刪後 `validation` 套件仍編譯;synchronizer 三函式刪後 live 端 0 引用。
- **全為確定性死碼。** numeric 方法/型別跨全樹 grep 零 production caller;synchronizer 三函式同。`internal/validation` 是 internal 套件,外部模組無法 import → `interfaces.go` 的 exported numeric 符號無「未來外部 consumer」可能性。
- **`EMGTimeToMotionIndex` 是 dead-by-design,非 missing-wiring。** 唯一對外同步入口 `GetSyncedTimeRange` 只吃 **motion-index / force-time** 兩種輸入,內部從不以 EMG-time 為輸入求 index。它不是「漏接線」的 bug,是設計上多餘的反方向轉換。
- **user 拍板完整移除。** 死碼留著只會在未來 architecture review 被重新挖出 grill 一遍(同 [[ADR-0006]]/[[ADR-0008]] Option C 被拒理由)。
- **同 repo 刪除範式。** 與 [[ADR-0006]](BackpressureController)、[[ADR-0008]](EChartsGenerator)同形:consumer 已消失、只剩 self-referential test 的 surface 整體移除;採同樣 deletion test。

## Considered Options

- **A. 完整移除(chosen)。** numeric 層(含 interface/ranges)+ synchronizer 三函式 + self-referential 測試。優點:complexity 真實消失、死碼 surface 收乾。缺點:幾乎沒有 —— 無 production consumer。
- **B. 保留 numeric 當「未來驗證 library」。** 拒:把零 caller 的 facade 留著當 hypothetical 彈性,等於以 dead code 形式偷渡 speculative 設計([[ADR-0008]] Option B 同理由)。若未來真需要數值驗證,從屆時需求 fresh 寫(成本與維護 anaemic facade 等同)。
- **C. Janitorial(只拔 wiring,保留型別)。** 拒:留下 numeric 套件 + interface + self-referential test,未來 review 重挖([[ADR-0006]]/[[ADR-0008]] Option C 同被拒)。
- **`EMGTimeToMotionIndex` 保留當對稱反向。** 拒:它是 live `MotionIndexToEMGTime` 的數學反向,但「對稱完整性」不足以保留 production 死碼(CLAUDE.md「無 speculative 彈性」)。代價極小(若未來出現 caller,重加 8 行)。移除其 3 處附帶測試引用屬「清理自身移除造成的 orphan」,非亂動無關測試。

## Consequences

- **InputValidator 對外契約收窄**:不再實作已刪的 `NumericValidator` interface(該 interface 一併刪),filename/csv/directory 方法不變,三個 production 持有者不受影響。
- **Test surface**:刪 numeric 套件測試 + validator 2 測試 + synchronizer 3 個專屬/附帶測試引用;synchronizer 的 `ConcurrentAccess`/`RoundTrip`/`MathematicalRelationships` 保留對活方法的覆蓋。
- **CONTEXT.md 不動**:numeric.Validator 與 synchronizer 三函式都是 implementation 細節,非 domain term。

## Reversibility

低成本回復(git revert),但本 ADR 鎖住「若未來需數值驗證 / EMG-time→index 轉換,從屆時需求 fresh 寫」而非「restore 舊 facade」—— 殘留形狀不會貼合屆時需求(理由同 [[ADR-0006]]/[[ADR-0008]])。

## Related

- [[ADR-0008]](刪除 EChartsGenerator)—— **同刪除範式 + deletion test**:consumer 消失、self-referential test 整體移除;含「test 檔須獨立符號掃描」紀律(本案 synchronizer 正是靠此抓到 `EMGTimeToMotionIndex` 的跨測試引用)。
- [[ADR-0006]](BackpressureController 拆除)—— 同「刪除 consumer 已消失的 module」。
- [[ADR-0005]](calculator family 拒拔 `*FromRawData`)—— **同 deletion test、結論相反**:該案位移成本高故保留,本案 complexity 真消失故刪。三條合讀防止「總是 collapse / 總是 preserve」誤讀。

## Notes

本 ADR 屬 2026-06-15 ultracode review 34-finding 修復批次(worktree `fix-ultracode-review-34`、基於 main `7ef89c6`;主多 agent 實作 + main agent 整合)。同批次其他修復(PHI redact、MaxMean 縮放溢位、CCI panic、EMG parser、logger、scaling-domain reverse-scale 等)各自 commit,非本 ADR 範圍。

- **`EMGTimeToMotionIndex` 跨測試裁決(main agent 整合層決定)**:Unit I 的 impl agent 依「reachability 要算進 test caller」([[memory:feedback_reachability_includes_handler_tests]])正確地不擅自刪、把決定交還整合層。整合層讀碼確認其為 dead-by-design(`GetSyncedTimeRange` 不吃 EMG-time 輸入)後選擇移除,並親自外科切除 3 處附帶引用(`ConcurrentAccess` 移除 1 行、`MathematicalRelationships` 移除 1 子測、`RoundTripConversions` 移除 EMGTime 往返 + 連帶 unused `emgMotionOffset` 欄位),保留各測試對活方法的覆蓋。**此項對稱性取捨可低成本推翻**(重加函式 8 行 + 對應測試)。
- **scaling-domain 反縮放契約(同批次、無另立 ADR)**:本批 Unit K 把 `*csvConverter.scaleValue` 的反縮放原語以 exported `io.ReverseScale(value, scalingFactor) = value / 10^SF` co-locate,並讓 GUI 陣列輸出比照 CSV 反縮放。屬實作層自我說明(helper + doc),未升 ADR;若未來縮放域邊界再起爭議,再立。
- **GUI smoke 未驗**(native webview headless 跑不了;比照 repo 慣例可授權無 smoke。)
