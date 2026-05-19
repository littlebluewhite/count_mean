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

async function readMainJs() {
    return await readFile(MAIN_JS_PATH, 'utf8');
}

// -------------------- P2-7:iframe sandbox source guard --------------------

test('P2-7: 所有 iframe.srcdoc 賦值前都有 iframe.sandbox = ...', async () => {
    const src = await readMainJs();

    // 找出所有 iframe.srcdoc = ... 的位置;前面 30 行應出現 iframe.sandbox 設定
    const srcdocRe = /iframe\.srcdoc\s*=/g;
    const matches = [...src.matchAll(srcdocRe)];
    assert.ok(matches.length >= 2,
        `預期至少 2 個 iframe.srcdoc 賦值(preview + CCI),got ${matches.length}`);

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
    const src = await readMainJs();

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
    assert.ok(sandboxMatches.length >= 2,
        `預期至少 2 個 iframe.sandbox 設定,got ${sandboxMatches.length}`);

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

test('P2-9: init() 內 postMessage listener 在 add 之前先 removeEventListener', async () => {
    const src = await readMainJs();

    // 在 init() async 函式內找 addEventListener('message', ...);前 5 行應出現
    // removeEventListener('message', this._cciMessageHandler)
    const initStart = src.indexOf('async init()');
    assert.ok(initStart > 0, '找不到 async init()');

    // init body 邊界以下一個 method 開頭抓
    const nextMethod = src.indexOf('\n    initProgressSubscription(', initStart);
    const initBody = src.slice(initStart, nextMethod);

    // 必須有先 remove 再 add 的 pattern
    const removeBeforeAdd = /removeEventListener\(['"]message['"]\s*,\s*this\._cciMessageHandler\)[\s\S]*?addEventListener\(['"]message['"]\s*,\s*this\._cciMessageHandler\)/;
    assert.match(initBody, removeBeforeAdd,
        'init() 必須先 removeEventListener("message", this._cciMessageHandler) 再重新 add — 確保 HMR / 重複 init 不會累積 listener');
});

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
    const src = await readMainJs();

    // 切分到每一行 — 用 line-by-line 取代 multiline regex,避免 () 巢狀
    // (e.g. arrow fn 內也有 ())把 regex pattern 拐到錯的位置。
    const lines = src.split('\n');
    const loadLines = lines.filter(l => /iframe\.addEventListener\(\s*['"]load['"]\s*,/.test(l));
    assert.ok(loadLines.length >= 3,
        `應至少 3 個 iframe.addEventListener("load", ...)(downloadChart + showCCIResult + downloadCCIChart),got ${loadLines.length}\n` +
        loadLines.join('\n'));

    // 每個都應該帶 { once: true } 避免重複 fire 累積
    for (const line of loadLines) {
        assert.match(line, /\{\s*once\s*:\s*true\s*\}/,
            `iframe load listener 必須帶 { once: true } 避免重複觸發: ${line.trim()}`);
    }
});

// -------------------- P2-9 + P2-10:行為驗證(happy-dom) --------------------

// 模擬 init() 註冊的「先 remove 再 add」pattern,確保多次呼叫不會累積 listener。
// 跑兩次 simulateInit,然後在 window 上 dispatchEvent,僅應觸發一次 handler。
test('P2-9 behavioural: 重複 init 後 message handler 只觸發一次', () => {
    const window = new Window();
    const doc = window.document;

    // 模擬 EMGAnalysisApp 物件
    const app = { _cciMessageHandler: undefined, fired: 0 };

    // 對應 main.js init() 的 listener 註冊 pattern
    function simulateInit(handler) {
        if (typeof app._cciMessageHandler === 'function') {
            window.removeEventListener('message', app._cciMessageHandler);
        }
        app._cciMessageHandler = handler;
        window.addEventListener('message', app._cciMessageHandler);
    }

    const handler = () => { app.fired += 1; };

    // 跑 3 次 init(模擬 HMR / 重複載入)
    simulateInit(handler);
    simulateInit(handler);
    simulateInit(handler);

    // 觸發一次 message event
    const evt = new window.MessageEvent('message', { data: 'cci-chart-restored' });
    window.dispatchEvent(evt);

    assert.equal(app.fired, 1,
        `3 次 simulateInit 後僅應有一個 handler — 收到一次 message 應觸發 1 次,實際 ${app.fired}`);

    // 必要的 hygiene:解除全部
    window.removeEventListener('message', app._cciMessageHandler);
    window.dispatchEvent(evt);
    assert.equal(app.fired, 1, '解除後再 dispatch 不可觸發');
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
