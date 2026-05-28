// Regression tests for P2-GUI batch (P2-7, P2-9, P2-10).
//
// 這些 sub-issue 都在 frontend/src/main.js 改動,屬於 JS-only 修法 — 沒辦法
// 用 Go test 釘,所以這支 test 用「main.js source guard」+「行為模擬」
// 兩種策略確保契約不會回退。
//
// 跑法:`node --test src/iframe_security.test.mjs`(於 frontend/ 下)。
// 需要 devDependency `happy-dom`(已存在於 package.json)。
//
// P2-7:iframe.srcdoc 必須帶 sandbox 屬性(allow-scripts 仍 enable echarts,
//        allow-same-origin 必須保留讓 parent 可跨 frame 讀 echarts 實例
//        — 沒它 updateCCIPhaseLines / download chart 會被 SOP 擋)。
//
// P2-9:postMessage / OnFileDrop listener 必須有 idempotent 守護 — 重複 init()
//        不能累積多個 handler。
//
// P2-10:iframe.onload = ... 已全面替換為 addEventListener('load', ...),
//        避免後續寫入覆蓋前一個 handler。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { Window } from 'happy-dom';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const MAIN_JS_PATH = path.join(__dirname, 'main.js');
// ADR-0007 / M5:iframe 建立(srcdoc + sandbox + load listener)從 main.js 5 個
// show*Panel 收乾到 manifestPanel.mjs 的 attachIframe()。iframe 安全 source-guard
// 因此改掃 manifestPanel.mjs(唯一 iframe.srcdoc 賦值點)。
const MANIFEST_PANEL_PATH = path.join(__dirname, 'manifestPanel.mjs');
const COMPOSER_SPEC_PATH = path.join(__dirname, 'panels', 'chart_composer_spec.mjs');

async function readMainJs() {
    return await readFile(MAIN_JS_PATH, 'utf8');
}
async function readManifestPanelJs() {
    return await readFile(MANIFEST_PANEL_PATH, 'utf8');
}
async function readComposerSpecJs() {
    return await readFile(COMPOSER_SPEC_PATH, 'utf8');
}

// -------------------- P2-7:iframe sandbox source guard --------------------
//
// ADR-0007:iframe.srcdoc/sandbox 賦值唯一點現在是 manifestPanel.mjs attachIframe()。
// 所有 5 panel(CCI/Composer/...)走它,故 srcdoc 出現次數 == 1(收乾後不再每
// panel 各自一份)。invariant 不變:srcdoc 賦值前必先設 sandbox=allow-scripts。

test('P2-7: 所有 iframe.srcdoc 賦值前都有 iframe.sandbox = ...', async () => {
    const src = await readManifestPanelJs();

    // 找出所有 iframe.srcdoc = ... 的位置;前面 30 行應出現 iframe.sandbox 設定
    const srcdocRe = /iframe\.srcdoc\s*=/g;
    const matches = [...src.matchAll(srcdocRe)];
    assert.ok(matches.length >= 1,
        `預期至少 1 個 iframe.srcdoc 賦值(attachIframe 收乾後唯一點),got ${matches.length}`);

    for (const m of matches) {
        const idx = m.index;
        // 取此 srcdoc 前 30 行的內容
        const before = src.slice(0, idx);
        const lastNewlines = before.split('\n').slice(-30).join('\n');
        assert.match(
            lastNewlines,
            /iframe\.sandbox\s*=\s*['"][^'"]*allow-scripts/,
            `iframe.srcdoc 賦值前(line ~${before.split('\n').length})應先設定 iframe.sandbox 含 allow-scripts`
        );
    }
});

test('P2-7: iframe.sandbox 不可包含 allow-top-navigation / allow-popups / allow-forms / allow-modals', async () => {
    const src = await readManifestPanelJs();

    const dangerous = [
        'allow-top-navigation',
        'allow-popups',
        'allow-forms',
        'allow-modals',
        'allow-pointer-lock',
        'allow-presentation',
    ];

    const sandboxRe = /iframe\.sandbox\s*=\s*['"]([^'"]+)['"]/g;
    const sandboxMatches = [...src.matchAll(sandboxRe)];
    assert.ok(sandboxMatches.length >= 1,
        `預期至少 1 個 iframe.sandbox 設定(attachIframe 收乾後唯一點),got ${sandboxMatches.length}`);

    for (const m of sandboxMatches) {
        const value = m[1];
        for (const bad of dangerous) {
            assert.ok(
                !value.includes(bad),
                `iframe.sandbox 不應含 "${bad}" 屬性(過寬):value="${value}"`
            );
        }
        // 仍應該保留 allow-scripts(echarts 需要)
        assert.ok(value.includes('allow-scripts'),
            `iframe.sandbox 必須含 allow-scripts 讓 echarts 渲染:value="${value}"`);
    }
});

// -------------------- P2-9:listener idempotent source guard --------------------
//
// P2-9 「init() 內 _cciMessageHandler 先 remove 再 add」原本對齊舊架構 — 直接
// window.addEventListener('message') + this._cciMessageHandler。ADR-0003 後
// iframeBridge 在 frontend/src/charts/iframeBridge.mjs 統一接管 window-level
// message listener,idempotent 保證收到 bridge.init() 內,既有 source-string
// 比對的 P2-9 invariant 失效 — 等價 behavioural invariant 移到
// iframeBridge.test.mjs(『init() 重複呼叫不累積 listener』)。

test('P2-9: initDragAndDrop 在 OnFileDrop 前先 OnFileDropOff', async () => {
    const src = await readMainJs();

    // initDragAndDrop 函式內必須:OnFileDropOff() 出現於 OnFileDrop(...) 之前
    const fnStart = src.indexOf('initDragAndDrop()');
    assert.ok(fnStart > 0, '找不到 initDragAndDrop()');
    const fnEnd = src.indexOf('\n    findDropTarget(', fnStart);
    const fnBody = src.slice(fnStart, fnEnd);

    const offIdx = fnBody.indexOf('OnFileDropOff(');
    const onIdx = fnBody.indexOf('OnFileDrop(');
    assert.ok(offIdx > 0, 'initDragAndDrop 必須呼叫 OnFileDropOff()');
    assert.ok(onIdx > 0, 'initDragAndDrop 必須呼叫 OnFileDrop()');
    assert.ok(offIdx < onIdx,
        `OnFileDropOff() 必須在 OnFileDrop() 之前呼叫: offIdx=${offIdx}, onIdx=${onIdx}`);
});

test('P2-9: initProgressSubscription 開頭呼叫 teardownProgressSubscription', async () => {
    const src = await readMainJs();

    const fnStart = src.indexOf('initProgressSubscription()');
    assert.ok(fnStart > 0, '找不到 initProgressSubscription()');
    const fnEnd = src.indexOf('\n    teardownProgressSubscription(', fnStart);
    const fnBody = src.slice(fnStart, fnEnd);

    assert.match(fnBody, /this\.teardownProgressSubscription\(\)/,
        'initProgressSubscription 必須先呼叫 teardownProgressSubscription() 清掉舊訂閱再重新 EventsOn');
});

// -------------------- P2-10:iframe.onload race source guard --------------------

test('P2-10: 沒有任何 iframe.onload = ... 賦值留存', async () => {
    const src = await readMainJs();

    // 把所有 // 與 /* */ 註解先剝掉再 grep,避免我們留下說明性 comment 觸發
    // 假警報(此 file 內 P2-10 註解仍有 "iframe.onload" 字樣)。
    const stripped = src
        .replace(/\/\*[\s\S]*?\*\//g, '')   // 移除 block comments
        .replace(/(^|[^:])\/\/[^\n]*/g, '$1'); // 移除 line comments(避免吃到 https://)

    // 只擋 iframe.onload = (賦值寫法);保留 iframe.addEventListener('load', ...) 不受影響
    const onloadAssignRe = /\biframe\.onload\s*=/;
    assert.ok(
        !onloadAssignRe.test(stripped),
        'iframe.onload = ... 全已替換為 addEventListener("load", ..., {once:true}) — P2-10 race fix'
    );
});

test('P2-10: 所有 iframe load 等待都用 addEventListener("load", ..., {once: true})', async () => {
    // ADR-0007:iframe load listener 收乾到 manifestPanel.mjs attachIframe() 內的
    // ready promise(`iframe.addEventListener('load', () => resolve(), {once:true})`)。
    // main.js 不再持有任何 iframe load listener。掃 manifestPanel.mjs。
    const src = await readManifestPanelJs();

    // 切分到每一行 — 用 line-by-line 取代 multiline regex,避免 () 巢狀
    // (e.g. arrow fn 內也有 ())把 regex pattern 拐到錯的位置。
    const lines = src.split('\n');
    const loadLines = lines.filter(l => /iframe\.addEventListener\(\s*['"]load['"]\s*,/.test(l));

    // attachIframe 收乾後唯一一個 load listener(ready promise),5 panel 共用。
    assert.ok(loadLines.length >= 1,
        `應至少 1 個 iframe.addEventListener("load", ...)(attachIframe ready promise),got ${loadLines.length}\n` +
        loadLines.join('\n'));

    // 每個都應該帶 { once: true } 避免重複 fire 累積
    for (const line of loadLines) {
        assert.match(line, /\{\s*once\s*:\s*true\s*\}/,
            `iframe load listener 必須帶 { once: true } 避免重複觸發: ${line.trim()}`);
    }
});

// -------------------- ADR-0003 bridge bypass guard --------------------
//
// ADR-0003 強制所有 chart iframe 通訊走 iframeBridge — main.js 不可再直接
// window.addEventListener('message', ...);否則就回退到「caller 各自寫 origin
// filter / requestId / timeout」shallow 反命題,memory feedback_wails_sandbox_
// iframe_crossframe 的成本又會回頭。
//
// 等價 idempotent 行為驗證移至 frontend/src/iframeBridge.test.mjs 的
// 「init() 重複呼叫不累積 listener」test;此 file 只看 source 防 backslide。

test('bridge bypass guard: main.js 剝註解後不可直接 window.addEventListener("message", ...)', async () => {
    const src = await readMainJs();
    // 移除所有 /* ... */ block comment 與 // line comment(留 string literal 不動)
    const stripped = src
        .replace(/\/\*[\s\S]*?\*\//g, '')
        .replace(/(^|[^:])\/\/[^\n]*/g, '$1');
    assert.ok(
        !/window\.addEventListener\(\s*['"]message['"]/.test(stripped),
        'main.js 不可直接 window.addEventListener("message", ...) — 所有 iframe 訊息走 iframeBridge.subscribe / requestReply(ADR-0003)',
    );
});

// codex review #1 P2:downloadCCIChart / downloadComposerChart 必須在 send
// PNG requestReply 之前 await iframe ready promise。否則 iframe customJS
// (含 external echarts.min.js)還沒 load 完成,listener 尚未註冊,
// requestReply 訊息被 drop → 10s timeout。
test('codex#1 P2: downloadCCIChart 在 requestReply 前 await _cciIframeReady', async () => {
    const src = await readMainJs();
    const fnStart = src.indexOf('async downloadCCIChart()');
    assert.ok(fnStart > 0, '找不到 downloadCCIChart');
    const reqIdx = src.indexOf('cci-request-png', fnStart);
    assert.ok(reqIdx > 0, 'downloadCCIChart 內必須含 cci-request-png');
    const beforeReq = src.slice(fnStart, reqIdx);
    assert.match(beforeReq, /await\s+this\._cciIframeReady/,
        'downloadCCIChart 必須在 bridge.requestReply 之前 await this._cciIframeReady');
});

test('codex#1 P2: downloadComposerChart 在 requestReply 前 await _composerIframeReady', async () => {
    const src = await readMainJs();
    const fnStart = src.indexOf('async downloadComposerChart()');
    assert.ok(fnStart > 0, '找不到 downloadComposerChart');
    const reqIdx = src.indexOf('composer-request-png', fnStart);
    assert.ok(reqIdx > 0, 'downloadComposerChart 內必須含 composer-request-png');
    const beforeReq = src.slice(fnStart, reqIdx);
    assert.match(beforeReq, /await\s+this\._composerIframeReady/,
        'downloadComposerChart 必須在 bridge.requestReply 之前 await this._composerIframeReady');
});

// -------------------- Slice D codex P2 #3 + #4 source guards --------------------

// onComposerSubjectChange 必須 clear 上一個 subject 的 _composerEMGMotionOffset
// + reset _composerLoadedSubject — 避免 user 切 subject 但沒按「載入 EMG 欄位」
// 直接 generateComposerChart 時用到舊 subject 的 offset。
test('Slice D P2#3: onComposerSubjectChange 必須 reset _composerEMGMotionOffset + _composerLoadedSubject', async () => {
    const src = await readMainJs();

    const fnStart = src.indexOf('onComposerSubjectChange()');
    assert.ok(fnStart > 0, '找不到 onComposerSubjectChange()');
    // 抓 fn body 邊界:下一個 method 開頭(loadComposerEMGChannels)
    const fnEnd = src.indexOf('\n    async loadComposerEMGChannels(', fnStart);
    assert.ok(fnEnd > fnStart, '找不到 onComposerSubjectChange 結束邊界');
    const fnBody = src.slice(fnStart, fnEnd);

    assert.match(
        fnBody,
        /this\._composerEMGMotionOffset\s*=\s*0/,
        'onComposerSubjectChange 必須 reset _composerEMGMotionOffset = 0,避免舊 subject offset 漏到新 subject'
    );
    assert.match(
        fnBody,
        /this\._composerLoadedSubject\s*=\s*(null|undefined|''|"")/,
        'onComposerSubjectChange 必須 reset _composerLoadedSubject 來強制 user 重新按「載入 EMG 欄位」'
    );
});

// generateComposerChart 的 _composerLoadedSubject guard:ADR-0007 / M5 後,RPC
// 呼叫從 main.js generateComposerChart 移到 chart_composer_spec.mjs 的 spec.rpc。
// 此 guard(loadedSubject ≠ ctx.subjectName 時 throw)現在掃 spec rpc。
// 行為層另由 chart_composer_spec.test.mjs「rpc 在 _composerLoadedSubject 與
// ctx.subjectName 不一致時 throw」釘死。
test('Slice D P2#3: chart_composer_spec.rpc 必須驗證 _composerLoadedSubject 與當前 subject 一致', async () => {
    const src = await readComposerSpecJs();

    const fnStart = src.indexOf('rpc: async (ctx)');
    assert.ok(fnStart > 0, '找不到 chart_composer_spec rpc');
    // 邊界:下一個 spec 欄位(onResult)
    const fnEnd = src.indexOf('\n        onResult:', fnStart);
    assert.ok(fnEnd > fnStart, '找不到 rpc 結束邊界(onResult)');
    const fnBody = src.slice(fnStart, fnEnd);

    assert.match(
        fnBody,
        /_composerLoadedSubject/,
        'spec.rpc 必須讀 app._composerLoadedSubject 驗證 user 已對當前 subject 重新載入 EMG 欄位'
    );
    // 必須在 GenerateChartComposer 呼叫前 short-circuit(出現順序檢查)
    const guardIdx = fnBody.indexOf('_composerLoadedSubject');
    const rpcIdx = fnBody.indexOf('GenerateChartComposer(');
    assert.ok(guardIdx > 0, 'guard 條件必須存在');
    assert.ok(rpcIdx > guardIdx,
        '_composerLoadedSubject guard 必須在 GenerateChartComposer RPC 呼叫之前(否則 silent 用舊 offset)');
});

// loadComposerEMGChannels 成功後必須記錄 _composerLoadedSubject = 當前 subject。
// ADR-0007 / M5:此 method 仍掛 app this(KEPT),只是 DOM id 改走 #mp*。
test('Slice D P2#3: loadComposerEMGChannels 成功後必須 set _composerLoadedSubject', async () => {
    const src = await readMainJs();

    const fnStart = src.indexOf('async loadComposerEMGChannels()');
    assert.ok(fnStart > 0, '找不到 loadComposerEMGChannels');
    // 邊界:下一個 method signature(M5 後為 downloadComposerChart;比註解字串穩)。
    const fnEnd = src.indexOf('\n    async downloadComposerChart(', fnStart);
    assert.ok(fnEnd > fnStart, '找不到 loadComposerEMGChannels 結束邊界');
    const fnBody = src.slice(fnStart, fnEnd);

    assert.match(
        fnBody,
        /this\._composerLoadedSubject\s*=\s*subject/,
        'loadComposerEMGChannels 成功後必須記錄 _composerLoadedSubject = subject(讓 generate 能驗證一致性)'
    );
});

// downloadComposerChart 不可走 SelectFile('save', ...) — Wails SelectFile 是
// OpenFileDialog 包裝,'save' buttonType 不被認識,實際打開的是「請選現有檔案」
// 對話框,user cancel 會 fallback 寫到 cwd(危險)。鏡像 DownloadCCIChart:
// 走 config.outputDir + 自動拼檔名,backend handler 已支援 outputPath 接收。
test('Slice D P2#4: downloadComposerChart 不可呼叫 SelectFile 開「儲存」對話框', async () => {
    const src = await readMainJs();

    const fnStart = src.indexOf('async downloadComposerChart()');
    assert.ok(fnStart > 0, '找不到 downloadComposerChart');
    // M5:原邊界(// === 標準化分期同步分析 section header)隨 showNormalizedPhaseSyncPanel
    // 刪除而消失;改用下一個 KEPT method signature(loadNormalizedPhaseSyncPhases)作邊界。
    const fnEnd = src.indexOf('\n    async loadNormalizedPhaseSyncPhases(', fnStart);
    assert.ok(fnEnd > fnStart, '找不到 downloadComposerChart 結束邊界');
    const fnBody = src.slice(fnStart, fnEnd);

    // 不可有 SelectFile(..., 'save') — 因為 Wails SelectFile = OpenFileDialog,
    // 'save' 在 backend switch case 不被認識,實際開的還是 OpenFileDialog
    // (user 看到「請選擇現有檔案」很困惑;cancel 走 fallback 寫到 cwd)。
    //
    // 用 split-line + 行內找 SelectFile call 的最後一個 ', 'save'),避免
    // multi-line nested () 干擾 regex。fnBody 內若任一行含 `'save')` 或
    // `"save")`(SelectFile 的 buttonType 收尾)即視為違反。
    const hasSaveButtonType = fnBody.split('\n').some(
        (l) => /['"]save['"]\s*\)/.test(l)
    );
    assert.ok(
        !hasSaveButtonType,
        'downloadComposerChart 不可走 SelectFile(...,...,"save") — Wails SelectFile 不支援 save buttonType,實際 fall through 變成 OpenFileDialog'
    );
});

// downloadComposerChart 必須走 GetConfig().outputDir(鏡像 CCI download 模式)。
test('Slice D P2#4: downloadComposerChart 必須走 config.outputDir(鏡像 DownloadCCIChart)', async () => {
    const src = await readMainJs();

    const fnStart = src.indexOf('async downloadComposerChart()');
    assert.ok(fnStart > 0, '找不到 downloadComposerChart');
    // M5:原邊界(// === 標準化分期同步分析 section header)隨 showNormalizedPhaseSyncPanel
    // 刪除而消失;改用下一個 KEPT method signature(loadNormalizedPhaseSyncPhases)作邊界。
    const fnEnd = src.indexOf('\n    async loadNormalizedPhaseSyncPhases(', fnStart);
    const fnBody = src.slice(fnStart, fnEnd);

    // 必須讀 config (GetConfig 或 this.config)並用 outputDir
    assert.match(
        fnBody,
        /outputDir/,
        'downloadComposerChart 必須讀 config.outputDir 作為輸出目錄(鏡像 CCI baseline)'
    );
});

// 模擬 P2-10 的 race:downloadCCIChart 不可覆蓋 showCCIResult 設的 onload handler。
// 用 addEventListener 之後,兩個 handler 各自獨立 → both fire。
test('P2-10 behavioural: addEventListener("load") 可保留多個 handler 互不覆蓋', () => {
    const window = new Window();
    const doc = window.document;

    const iframe = doc.createElement('iframe');
    doc.body.appendChild(iframe);

    let phaseLineUpdates = 0;
    let downloadReady = 0;

    // showCCIResult 註冊的 handler — 對應 main.js:1812
    iframe.addEventListener('load', () => { phaseLineUpdates += 1; }, { once: true });

    // downloadCCIChart 註冊的 handler — 對應 main.js:2002
    iframe.addEventListener('load', () => { downloadReady += 1; }, { once: true });

    // 模擬 iframe 載入完成
    iframe.dispatchEvent(new window.Event('load'));

    assert.equal(phaseLineUpdates, 1,
        'updateCCIPhaseLines handler 必須被觸發(過去因被 iframe.onload = resolve 覆蓋而消失)');
    assert.equal(downloadReady, 1,
        'downloadCCIChart 的 resolve handler 也必須被觸發');

    // 再 dispatch 一次驗 {once:true}—兩個都不該再 fire
    iframe.dispatchEvent(new window.Event('load'));
    assert.equal(phaseLineUpdates, 1, '{once:true} 應避免第二次觸發');
    assert.equal(downloadReady, 1, '{once:true} 應避免第二次觸發');
});
