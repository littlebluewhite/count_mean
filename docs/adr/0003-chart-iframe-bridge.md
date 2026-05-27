# Chart iframe 通訊統一收乾為 iframeBridge

**Status**: accepted (2026-05-28)

## Decision

新增 `frontend/src/charts/iframeBridge.mjs` 作為 parent ↔ chart iframe 之間**唯一**的 postMessage 通訊進出點,把現有兩個 chart adapter(CCI、[[Chart Composer]])散落的 listener 統一遷上來。核心架構選擇:

1. **postMessage 唯一協定,禁止 cross-frame chart access**:bridge 強制 callers 不直接呼 `iframe.contentWindow.echarts.*`。`phaseLines.mjs` 既有 `category` mode(CCI 在用)的 cross-frame `chart.setOption` 路徑整段廢除,改走 postMessage,iframe 內 customJS 自己 setOption。理由:wails dev webview 在 sandbox + opaque origin 下會 silent block cross-frame access(見 `memory/feedback_wails_sandbox_iframe_crossframe`),CCI 那條路徑屬於 latent bomb,bridge 收乾時順手拆掉。

2. **3 個 primitive**:
   - `subscribe(iframe, typeOrPrefix, handler) → unsubscribe` — iframe → parent notification,支援 exact 或 `'<prefix>-*'` wildcard,handler 收 `(payload, type)`
   - `send(iframe, type, payload)` — parent → iframe one-way 推送
   - `requestReply(iframe, type, payload, {timeout?}) → Promise<reply>` — round-trip 含 requestId 配對與 timeout

3. **單一 window-level message listener、iframe + type 多路分發**:bridge 內部維護 `Map<iframe, Map<typePrefix, handler[]>>`,在 `init()` 一次性 `window.addEventListener('message')` 並做 idempotent 重啟保護(對齊 P2-9 既有 hardening)。callers 不接觸 window listener API。

4. **origin policy 100% hidden**:bridge 內部 hardcode inbound(`['null', window.location.origin]` + `e.source === window.parent` fallback)與 outbound(`'*'`)。`subscribe` / `send` / `requestReply` 不收任何 origin-related opts。Caller 永遠看不到 sandbox / opaque origin trap。

5. **uniform error envelope**:requestReply reply message shape `{requestId, payload?, error?}`,`error: string` 時 bridge auto-reject Promise(英文 message,callers 自負 i18n)。iframe customJS 必須 try/catch 包 throwable 後 post error,**不可** silent swallow(否則 caller 拿到的會是 10s timeout 而非 root cause)。

6. **symmetric [[phase marker]] protocol**:CCI + Composer 兩 adapter 收到的 phase-markers payload shape 一致:`{type: '<adapter>-update-phase-markers', checkedPhases: [{name, time, pct}]}`。parent 算 `recalcPercents` 後送出、iframe customJS 自己依各自 chart axis 類型組裝 markData(CCI category 走 `findNearestLabel`、Composer value 走 numeric)並 setOption。parent 不持有 chart-internal 知識(targetIdx、xAxis 形狀)。

7. **`phaseLines.mjs` 整個刪除**:parent 側只剩「讀 checkbox state → `recalcPercents` → `bridge.send`」三步,薄到不值得獨立 module;CCI / Composer adapter 各自 ~10 行 inline 呼叫 `phaseMarkers.recalcPercents` + `bridge.send`。

8. **新增 `frontend/src/charts/phaseMarkers.mjs` pure helpers**:`recalcPercents` + `findNearestLabel`,parent 跟 iframe customJS 共用 — Go 端用 `//go:embed` 把 source 塞進 chart HTML `<script>` 讓 customJS body 可呼叫。

9. **test discipline(hybrid)**:
   - bridge primitives:`iframeBridge.test.mjs` happy-dom 行為測試
   - phaseMarkers pure helpers:`phaseMarkers.test.mjs` happy-dom 單元測試
   - iframe customJS listener 存在性:`internal/cci/chart_origin_test.go` + `internal/chart/composer_test.go` source-string 斷言
   - iframe customJS setOption assembly:接受 coverage regression(原 `phaseLines.test.mjs` category 6 個 test 的 chart-state 邏輯不再有 unit cover,但 ~25 行範圍小、ECharts API mechanical,manual smoke 風險可接受)

10. **`iframe_security.test.mjs` 防 bypass guard**:新增負向源碼斷言 — `main.js` 剝註解後不得直接出現 `window.addEventListener('message', ...)`(防 backslide);原 P2-9 idempotent 行為驗證移到 `iframeBridge.test.mjs`。

## Why

- 兩個 chart adapter 既有 3 處 inline origin filter(`main.js:139`、`main.js:2182`、Composer phase-markers fallback)、各自 ad-hoc listener lifecycle(CCI persistent vs Composer per-call)、Composer PNG `downloadComposerChart:2174-2202` 那 14 行 requestId/timeout/cleanup boilerplate — sandbox / postMessage / opaque origin 陷阱重複出現在多處,memory 累積 2 個專門筆記就是這個 leak 的成本。
- `phaseLines.mjs` 已分裂成 `category` mode(cross-frame)+ `value` mode(postMessage)兩條路徑,前者是 latent bomb;不收乾遲早再出 phase-render bug。
- ADR-0002 §3 已 explicitly 點出 CCI/Composer 共享 phase-line 邏輯的方向(目前透過 `phaseLines.mjs` helper);但 helper 只統一了 parent 側計算、沒統一傳輸層 — bridge 把共享範圍從「helper function」擴張到「protocol」,順手解決 helper `axisMode` 參數的技術債(見 `phaseLines.mjs:38-48` 註解)。
- LANGUAGE.md depth 原則:3 個 callers(CCI subscribe、CCI/Composer send、Composer requestReply)各自重寫 origin filter / requestId / timeout 是 shallow 反命題;單一 bridge 持有所有陷阱 = locality + leverage 雙贏。

## Considered Options

- **不抽 bridge,各 caller 自寫**:現狀,sandbox trap 每個新 adapter 重學一次。拒絕:LANGUAGE.md「two adapters = real seam」threshold 已過、memory 已記載重複 cost。
- **薄 bridge(僅 subscribe/send,callers 自寫 requestReply Promise wrapper)**:Composer 那 14 行 boilerplate 留在 caller、第二個 requestReply 用者複製。拒絕:違反 depth 原則。
- **每 adapter 自定 protocol shape(不對稱 markData / checkedPhases)**:Composer 維持 `markData` payload、CCI 走新 `checkedPhases`。拒絕:bridge 退化為 transport、leverage = 0、CONTEXT.md 無法收乾 `phase marker` domain 概念。
- **計算留 parent,bridge 純 transport**:parent 仍持有 chart-internal 知識(targetIdx、xAxis 形狀);Composer 已 ship 的 deep 路線(parent 不碰 chart-internal)退化向後對齊 CCI shallow shape。拒絕:倒退已有的好設計。
- **保留 CCI category mode cross-frame access**:bridge 只收 Composer 那側、CCI 仍 cross-frame。拒絕:latent bomb 留著、seam adapter 數退回 borderline 不過 threshold。
- **錯誤訊號改走「不回」走 timeout fallback**:iframe 遇 error silent swallow,parent 等到 10s timeout 視為失敗。拒絕:debug 困難 — caller 拿到 timeout 但 root cause 是 iframe 內 200ms 就 throw、trace 不到。

## Reversibility

中。

- **不可逆**:`phaseLines.mjs` 刪除 + iframe customJS 注入 `phaseMarkers.mjs` source 兩條形狀變化跨 Go + frontend,revert 需同步改兩語言;`iframeBridge.test.mjs` 一旦建立,後續 test infra 依賴它。
- **可逆**:bridge primitive 數量(3 vs 2)、subscribe match 語義(exact + wildcard vs exact only)、timeout default 值、error envelope `error: string` vs `{code, message}` — 都是 bridge internal 改動、不影響 CCI / Composer adapter 行為。
- **格外注意**:Q9 「`phaseLines.mjs` 刪掉」是 high-locality 決策,若未來加第 3 個 chart adapter 發現「每個 adapter 10 行 inline 寫得很煩」、想再抽 facade,語意要與既有 bridge + phaseMarkers 對齊,不能變成新的 third layer。
