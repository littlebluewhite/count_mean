// frontend/src/iframeBridge.test.mjs
//
// iframeBridge primitives 行為測試(ADR-0003)。
//
// 跑法:`node --test src/iframeBridge.test.mjs`(於 frontend/)
// 也會被 `npm test`(glob `src/*.test.mjs`)抓到。
//
// 為何 happy-dom:bridge 依賴 `window.addEventListener('message')` /
// MessageEvent / iframe.contentWindow.postMessage,純 Node 無法 simulate。

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';

// ----- setup helper -----

async function setup() {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    globalThis.MessageEvent = window.MessageEvent;
    const { bridge } = await import('./charts/iframeBridge.mjs');
    bridge.destroy(); // 從上個 test 殘存 state 清乾淨
    bridge.init();
    return { window, bridge };
}

function makeIframe(window) {
    const iframe = window.document.createElement('iframe');
    window.document.body.appendChild(iframe);
    return iframe;
}

// 模擬 inbound message:在 parent window 上 dispatch MessageEvent,
// 帶 origin + source override(happy-dom 預設不能直接設 origin/source)。
function postInbound(window, { source, origin, data }) {
    const evt = new window.MessageEvent('message', { data });
    Object.defineProperty(evt, 'origin', { value: origin, configurable: true });
    Object.defineProperty(evt, 'source', { value: source, configurable: true });
    window.dispatchEvent(evt);
}

// ----- tests -----

test('subscribe + inbound dispatch: handler 收到 (payload, type)', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    const received = [];
    bridge.subscribe(iframe, 'composer-png-result', (payload, type) => {
        received.push({ payload, type });
    });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: window.location.origin,
        data: { type: 'composer-png-result', payload: { dataURL: 'data:foo' } },
    });
    assert.equal(received.length, 1);
    assert.equal(received[0].type, 'composer-png-result');
    assert.deepEqual(received[0].payload, { dataURL: 'data:foo' });
    bridge.destroy();
});

test('subscribe wildcard "<prefix>-*" match exact prefix-suffix events', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    const received = [];
    bridge.subscribe(iframe, 'cci-chart-*', (_payload, type) => {
        received.push(type);
    });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'cci-chart-restored' },
    });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'cci-chart-legend-changed' },
    });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'composer-png-result' },
    });
    assert.deepEqual(received, ['cci-chart-restored', 'cci-chart-legend-changed']);
    bridge.destroy();
});

test('requestReply happy path: requestId 配對後 resolve payload', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    let outbound = null;
    iframe.contentWindow.postMessage = (msg) => { outbound = msg; };
    const promise = bridge.requestReply(iframe, 'composer-request-png', {});
    assert.equal(outbound.type, 'composer-request-png');
    assert.ok(outbound.requestId, 'bridge 應自動生成 requestId');
    postInbound(window, {
        source: iframe.contentWindow,
        origin: window.location.origin,
        data: {
            type: 'composer-png-result',
            requestId: outbound.requestId,
            payload: { dataURL: 'data:foo' },
        },
    });
    const reply = await promise;
    assert.deepEqual(reply, { dataURL: 'data:foo' });
    bridge.destroy();
});

test('requestReply error envelope: error 字串 → reject Error(error)', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    let outbound = null;
    iframe.contentWindow.postMessage = (msg) => { outbound = msg; };
    const promise = bridge.requestReply(iframe, 'composer-request-png', {});
    postInbound(window, {
        source: iframe.contentWindow,
        origin: window.location.origin,
        data: {
            requestId: outbound.requestId,
            error: 'iframe getDataURL threw',
        },
    });
    await assert.rejects(promise, /iframe getDataURL threw/);
    bridge.destroy();
});

test('requestReply timeout: 無 reply → reject', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    iframe.contentWindow.postMessage = () => {};
    const promise = bridge.requestReply(iframe, 'composer-request-png', {}, { timeout: 30 });
    await assert.rejects(promise, /timeout/i);
    bridge.destroy();
});

test('requestReply: 錯誤 requestId 不 resolve(race-condition guard)', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    iframe.contentWindow.postMessage = () => {};
    const promise = bridge.requestReply(iframe, 'composer-request-png', {}, { timeout: 50 });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: window.location.origin,
        data: {
            requestId: 'WRONG_ID',
            payload: { dataURL: 'data:bar' },
        },
    });
    await assert.rejects(promise, /timeout/i);
    bridge.destroy();
});

test('inbound origin filter: 外來 origin 不觸發 subscribe handler', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    let fired = 0;
    bridge.subscribe(iframe, 'composer-png-result', () => { fired += 1; });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'https://evil.com',
        data: { type: 'composer-png-result', payload: {} },
    });
    assert.equal(fired, 0, 'evil.com origin 應被拒絕,handler 不觸發');
    bridge.destroy();
});

test('inbound source mismatch: 非 subscribed iframe 不觸發 handler', async () => {
    const { window, bridge } = await setup();
    const iframeA = makeIframe(window);
    const iframeB = makeIframe(window);
    let firedA = 0;
    bridge.subscribe(iframeA, 'composer-png-result', () => { firedA += 1; });
    postInbound(window, {
        source: iframeB.contentWindow,
        origin: window.location.origin,
        data: { type: 'composer-png-result', payload: {} },
    });
    assert.equal(firedA, 0, '訊息來自 iframeB,iframeA 上的 subscribe handler 不應觸發');
    bridge.destroy();
});

test('init() 重複呼叫不累積 listener', async () => {
    const { window, bridge } = await setup();
    bridge.init();
    bridge.init();
    const iframe = makeIframe(window);
    let fired = 0;
    bridge.subscribe(iframe, 'composer-png-result', () => { fired += 1; });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'composer-png-result' },
    });
    assert.equal(fired, 1, '重複 init 應 idempotent,inbound 觸發 handler 一次');
    bridge.destroy();
});

test('subscribe 回的 unsubscribe fn 解綁 handler', async () => {
    const { window, bridge } = await setup();
    const iframe = makeIframe(window);
    let fired = 0;
    const unsub = bridge.subscribe(iframe, 'composer-png-result', () => { fired += 1; });
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'composer-png-result' },
    });
    assert.equal(fired, 1);
    unsub();
    postInbound(window, {
        source: iframe.contentWindow,
        origin: 'null',
        data: { type: 'composer-png-result' },
    });
    assert.equal(fired, 1, 'unsubscribe 後不應再觸發');
    bridge.destroy();
});
