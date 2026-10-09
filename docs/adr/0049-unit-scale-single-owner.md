# 縮放域換算單一 owner calculator.UnitScale（維持 ctor-time scalingFactor）

**Status**: accepted · **implemented** (2026-10-10)

[[Scaled domain]]（縮放域，值已乘 10^scalingFactor）與原域（使用者輸入的秒、CSV 輸出的原單位）之間的換算，原本散落四處各自寫 `math.Pow10`。本 ADR 把它收為 `calculator.UnitScale` 單一 owner，並**關閉 ADR-0029 Notes 的「scaling-domain 反縮放契約」未決註記**。

## Decision

1. 新增 `internal/calculator/unit_scale.go`：`UnitScale{m}`、`NewUnitScale(sf)`、`ToScaled(v) = v * 10^sf`、`FromScaled(v) = v / 10^sf`（除法，不是乘 10^-sf，保證輸出 bytes 與舊路徑 bit-identical）。
2. `MaxMeanCalculator` / `PhaseAnalyzer` 在 **ctor** 以既有 `scalingFactor` 建立 `UnitScale`；`resolveDataRange` 與 `AnalyzeFromRawDataWithRanges` 的 `ToScaled` 取代原 `math.Pow10` 內聯。
3. io `csvConverter` 持有 `calculator.UnitScale`，以 `FromScaled` 取代 `scalingMultiplier` / `scaleValue`（`internal/io` 本就 import `internal/calculator`，無新依賴邊）。
4. `util.Str2Number` 維持自帶 `parsed * math.Pow10(move)`：util 不能 import calculator（calculator 依賴 util）。以 `TestUnitScale_MatchesStr2Number` 釘住 `ToScaled` 與其 bit-for-bit 一致。
5. 零行為變更；golden（MaxMean / Normalize / Phases）輸出無差異。

## Why

- 縮放域換算的方向（乘 / 除）是兩個已發生的靜默回歸之根：`0e9cebb`（秒單位 phase range 未轉入縮放域 → 每個 phase 統計全 0）與 `812ebac`（>922 s 溢位）。單一 owner 讓「哪個值在哪個域」只有一處可查，方向接反由 `TestUnitScale_Direction` 與既有回歸測試（`TestAnalyzePhases_HonorsFrontendPhasesAndNames`、`TestMaxMean_ScaledTimeOverflow_WindowMiscompute`）攔截。
- ADR-0029 把反縮放契約留在「實作層自我說明、未升 ADR；縮放域邊界再起爭議再立」。邊界已再次橫跨 calculator 與 io 兩層，故於此立案並結案。

## Considered Options

- **A. `calculator.UnitScale`，ctor-time 建立（chosen）**：見上。
- **B. 維持散落的 `math.Pow10`（status quo）**：拒。方向約定只存在於註解，已兩度出錯。
- **C. 把 `UnitScale` 放進 util 並讓 Str2Number 也用**：拒。`Str2Number` 的整數化語意與泛型簽章不同，硬併會擴大 util 公開面；bit-for-bit 一致用測試鎖即可。
- **D. ADR-0005 Option D：`scalingFactor` 移出 ctor、per-call 注入 —— 仍拒絕**。本案 `UnitScale` 正是在 ctor 以 ctor-time `scalingFactor` 建立，不改任何方法簽章；ADR-0005 拒絕 Option D 的理由（擴張 method 簽章、破壞 GUI snapshot 與 `appState` 配對的 `ScalingFactor` 一致性）完全不受影響。
