# Chart Composer 採 single-instance multi-grid 並取代舊 chart panel

**Status**: accepted (2026-05-27)

## Decision

新增 [[Chart Composer]] —— 一個 visualization-only feature —— 取代舊「資料做圖」(showChartPanel + `GenerateChart` / `GenerateInteractiveChart` Go handlers + 對應 i18n)。核心架構選擇：

1. **不進 [[Analysis pipeline family]]**：不計算、不寫 CSV、不產 result struct。輸入是 [[Manifest]] + 數據資料夾 + 選定 [[Subject]] + user-selected EMG channel subset，輸出是 chart HTML（live preview + on-demand PNG download）。
2. **三圖渲染走 single ECharts instance + N stacked grids**（N=2 或 3，視 manifest 是否含 `MuscleRatioFile` 動態決定）：共用 bottom time x-axis、共用 top percent x-axis、底部 slider zoom 控全部 grid、inside zoom 各 grid 內。
3. **Phase multi-select recalc percent**：重用 `frontend/src/main.js:1859` `updateCCIPhaseLines` 的 `min checked = 0%, max checked = 100%` 機制 — 抽 phase-checkbox + percent recalc 為 frontend 共用 helper，CCI 與 Chart Composer 兩處消費同一份實作。
4. **Motion data 不 resample**：motion 20Hz sample 點直接畫在 `motion_index × (1/FrequencyMotion) + EMGMotionOffset 換算` 後的 EMG 秒數軸上。共用 x-axis 走 value type，ECharts 自動以連線方式視覺 interpolate sparse 點。
5. **`MuscleRatioFile` 為 manifest 可選欄位**：`PhaseManifestMinFields` 保持 15（V.10/V.13 manifest 仍可被 parser 接受），新欄位由 parser 在 `len(record) >= 16` 時 conditionally 解析。Chart Composer 在 `MuscleRatioFile == ""` 時退化為「EMG + motion 兩 grid」並在 UI 顯示說明。

## Why

- 既有四個 [[Analysis pipeline family]] 成員（CCI / MuscleRatio / PhaseSync / Phases）形狀統一為「manifest → 計算 → 結果 struct → CSV」；Chart Composer 沒有計算、沒結果、沒 CSV，硬塞進家族會逼出 `EmptyResult` 反模式或讓 csvHandler 寫 HTML，違背 ADR-0001 收乾的「單一明確管線」原則。
- CCI 既有 `updateCCIPhaseLines` 已實作「min/max-of-checked recalc percent」UX，與本次需求**字面對齊**；重複實作會分裂演算法，未來 bug-fix 需雙改。
- Slider zoom 一次控三圖：multi-instance 需 hand-wire `dataZoom` event 跨 instance broadcast + checkbox 分發到三個 listener — 增加 ~150 行同步邏輯與 reload race-condition 風險；single-instance 是 ECharts 原生 grid array pattern，零協同成本。
- Motion 20Hz vs EMG 2000Hz：resample 到 EMG 軸增加 100× 點數同時喪失原始採樣點可追溯性；ECharts value-axis 在 sparse 點之間自動連線視覺已足夠 smooth（motion 圖在 zoom-out 視圖看不出採樣率差異）。
- 舊 chart panel 的「單檔 → 一張圖」實際支援 ad-hoc 任意 CSV 畫圖，但無測試覆蓋此用途；保留會撐出兩個用詞極相似的 menu entry（「資料做圖」vs「Chart Composer」），混淆成本大於彈性收益。

## Considered Options

- **加入 [[Analysis pipeline family]]**：強制 Chart Composer 走 [[AnalysisHandler[P, R]]]。拒絕：產出端是 chart HTML 不是 CSV，會逼出 `EmptyResult` 或 `ChartHTMLResult` 反模式 — 前者污染家族契約、後者讓 csvHandler 寫 HTML 邏輯爆走。
- **保留舊 chart panel 並存**：menu 加一個「Chart Composer」按鈕、與「資料做圖」並列。拒絕：menu label / i18n key 命名衝突、user habits 雙重學習、舊 panel 的 ad-hoc 任意 CSV 能力沒有測試覆蓋（實際使用頻率不明）。
- **三個獨立 ECharts instance（每張一個 iframe）**：hand-wire zoom 同步 + 跨 instance event broadcast。拒絕：~150 行同步邏輯、reload race condition 難 trace、PNG 下載要 stitch 三張圖。
- **Motion resample 到 EMG 軸**：linear interp 補密到 2000Hz。拒絕：100× 點數膨脹、原始 motion 採樣點可追溯性喪失、視覺改善為零。
- **強制 `MuscleRatioFile` 必填**：parse 階段 reject V.10 manifest。拒絕：使用者手上有大量 V.10 manifest，強制升級 high friction；「動態 2-3 grid」實作成本接近零（grid array length 是 1 個 number）。

## Reversibility

中至低。
- **不可逆**：刪除舊 `showChartPanel` / `GenerateChart` / `GenerateInteractiveChart` Go handler / 對應 i18n key 後，git revert 跨 commit 可技術復原，但 follow-up code 會在新架構上堆積，回頭代價 ≥ 0.5 工作天。
- **可逆**：single-instance multi-grid 換成 multi-instance、sparse motion 換成 resample、`MuscleRatioFile` 從可選變必填 — 都是 internal 重寫，不影響 manifest schema 或外部契約。
