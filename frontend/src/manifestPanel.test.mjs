// frontend/src/manifestPanel.test.mjs
//
// ManifestPanel envelope behaviour tests(ADR-0007 §6 + §7)。
//
// 跑法:`node --test src/manifestPanel.test.mjs`(於 frontend/)
// 也會被 `npm test` glob(`src/*.test.mjs`)抓到。
//
// 測試覆蓋:
//   1. Success path:rpc resolve → status(running→done)+ ShowMessage 一次
//   2. RPC throw:status failed + ShowError + 無 ShowMessage + _running reset
//   3. silentSuccess=true:略過 ShowMessage,onResult 仍呼叫
//   4. Reentrant guard:_runEnvelope 重入 → rpc 只跑一次(防 MuscleRatio doubleclick race)
//   5. registerCleanup:cleanup fn 在下次 run 開頭被 flush 並 call 一次
//   6. registerCleanup:throwing cleanup 不阻擋後續 cleanup(swallow + log)
//   7. attachIframe ready promise:iframe `load` event 觸發後 ready resolve
//   8. spec.runBtnLabelKey:default + override 行為
//
// Mock 策略:不 import main.js(會 trigger Wails runtime IIFE),
// 改 stub globalThis.app / ShowMessage / ShowError / t / tHtml +
// 用 happy-dom Window 提供 DOM。
//
// invariant(由本 file 釘):panel shell 內所有 user-visible text 必經
// translator;render 後 textContent 不可含繁中字元 [一-龥](對齊
// chart_composer_panel.test.mjs 既有 hardcoded 中文洩漏防護模式)。

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';

// ---------- shared setup ----------

// 為何「每 test 一個 Window + happyDOM.close()」:happy-dom iframe element
// 創出 contentWindow 後會掛 internal timer / async pending 任務,跨 test
// 不 close 會讓 node:test 等到 event loop drain → file-level 200s 超時。
// 對齊 jsdom test pattern,test 結束前必呼 `window.happyDOM.close()`。
async function setup() {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    globalThis.MessageEvent = window.MessageEvent;
    globalThis.Event = window.Event;

    // panel shell 預期會看到 #functionPanel 容器(對齊 main.js showPanel())
    const root = window.document.createElement('div');
    root.id = 'functionPanel';
    window.document.body.appendChild(root);
    const mainMenu = window.document.createElement('div');
    mainMenu.id = 'mainMenu';
    window.document.body.appendChild(mainMenu);

    // stub i18n:fake translator 回 ASCII key,讓我們能 assert specific key 觸發
    globalThis.t = (key, ...args) => `__T:${key}__${args.length ? ':' + args.join(',') : ''}`;
    globalThis.tHtml = (key) => `__T:${key}__`;

    // stub dialog APIs:預設 no-op 並記錄 call,test 內覆寫
    const calls = {
        showMessage: [],
        showError: [],
        statusUpdates: [],
    };
    globalThis.ShowMessage = async (title, msg) => {
        calls.showMessage.push({ title, msg });
    };
    globalThis.ShowError = async (title, msg) => {
        calls.showError.push({ title, msg });
    };

    // 最小 app stub(ManifestPanel 透過 constructor 取得 app reference)
    const app = {
        updateStatus(msg) { calls.statusUpdates.push(msg); },
        showPanel() { /* no-op */ },
        openOutputFolder: async () => { calls.openOutputFolder = (calls.openOutputFolder ?? 0) + 1; },
    };

    // 動態 import ManifestPanel(globalThis 已備齊)
    const { ManifestPanel } = await import('./manifestPanel.mjs');
    const mp = new ManifestPanel(app);
    return { window, app, mp, calls };
}

async function teardown(window) {
    // happy-dom Window close 取消所有 pending async + timer(避免 file-level 200s 卡死)
    if (window?.happyDOM?.close) {
        await window.happyDOM.close();
    }
}

function minimalSpec(overrides = {}) {
    return {
        titleKey: 'panel.test.title',
        statusRunningKey: 'status.test_running',
        formBody: (t) => `<p>${t('form.test.body')}</p>`,
        rpc: async (_ctx) => ({ message: 'ok' }),
        onResult: async (_result, _ctx, _mp) => { /* no-op */ },
        ...overrides,
    };
}

// 從 functionPanel innerHTML 預填一組合法輸入,讓 _gatherCtx 不卡在缺欄位
function fillCtxInputs(window, { manifestPath = '/tmp/m.csv', dataFolder = '/tmp/data', subjectIdx = 0, subjectName = 'SubjectA' } = {}) {
    const get = (id) => window.document.getElementById(id);
    get('mpManifestPath').value = manifestPath;
    get('mpDataFolder').value = dataFolder;
    // subject select 填一個 option + select 它
    const select = get('mpSubject');
    select.innerHTML = '';
    const opt = window.document.createElement('option');
    opt.value = String(subjectIdx);
    opt.textContent = subjectName;
    select.appendChild(opt);
    select.value = String(subjectIdx);
    select.disabled = false;
}

// ---------- tests ----------

test('1. success path:rpc 成功 → status(running→done)+ ShowMessage 一次', async () => {
    const { window, mp, calls } = await setup();
    try {
        const rpcCalls = [];
        const spec = minimalSpec({
            rpc: async (ctx) => {
                rpcCalls.push(ctx);
                return { message: 'analysis-complete-payload' };
            },
            onResult: async (_result, _ctx, _mp) => {
                calls.onResultRan = true;
            },
        });

        mp.run(spec);
        // run() 同步 render shell;_runEnvelope 由 button click 觸發
        fillCtxInputs(window);
        await mp._runEnvelope(spec);

        assert.equal(rpcCalls.length, 1, 'rpc 應呼叫一次');
        assert.equal(calls.statusUpdates.length, 2, 'updateStatus 應呼叫 2 次(running + done)');
        assert.match(calls.statusUpdates[0], /status\.test_running/, '首呼為 spec.statusRunningKey');
        assert.match(calls.statusUpdates[1], /status\.analysis_done/, '次呼為共通 status.analysis_done');
        assert.equal(calls.showMessage.length, 1, 'ShowMessage 應呼叫一次');
        assert.match(calls.showMessage[0].title, /dialog\.title\.complete/, 'ShowMessage 用 dialog.title.complete 標題');
        assert.equal(calls.showMessage[0].msg, 'analysis-complete-payload', 'ShowMessage body 為 result.message');
        assert.equal(calls.showError.length, 0, '成功路徑不應觸發 ShowError');
        assert.equal(calls.onResultRan, true, 'onResult 必呼叫');
        assert.equal(mp._running, false, '_running 應在 finally 重置');
    } finally {
        await teardown(window);
    }
});

test('2. rpc throw:status failed + ShowError + 無 ShowMessage + _running reset', async () => {
    const { window, mp, calls } = await setup();
    try {
        const spec = minimalSpec({
            rpc: async () => { throw new Error('boom'); },
            onResult: async () => { calls.onResultRan = true; },
        });

        mp.run(spec);
        fillCtxInputs(window);
        await mp._runEnvelope(spec);

        assert.equal(calls.showError.length, 1, 'ShowError 應呼叫一次');
        assert.equal(calls.showMessage.length, 0, '失敗路徑不應 ShowMessage');
        // 最後一個 status 是 failed
        const lastStatus = calls.statusUpdates[calls.statusUpdates.length - 1];
        assert.match(lastStatus, /status\.analysis_failed/, '最終 status 為 analysis_failed');
        assert.equal(calls.onResultRan, undefined, 'rpc throw 後不應跑 onResult');
        assert.equal(mp._running, false, '_running 必經 finally 重置(防永久 locked)');
    } finally {
        await teardown(window);
    }
});

test('3. silentSuccess=true:略過 ShowMessage,onResult 仍呼叫', async () => {
    const { window, mp, calls } = await setup();
    try {
        const spec = minimalSpec({
            rpc: async () => ({ message: 'irrelevant' }),
            onResult: async () => { calls.onResultRan = true; },
            silentSuccess: true,
        });

        mp.run(spec);
        fillCtxInputs(window);
        await mp._runEnvelope(spec);

        assert.equal(calls.showMessage.length, 0, 'silentSuccess=true → 不 ShowMessage');
        assert.equal(calls.onResultRan, true, 'onResult 仍跑');
        // status 仍正常 running → done
        assert.equal(calls.statusUpdates.length, 2);
    } finally {
        await teardown(window);
    }
});

test('4. reentrant guard:_runEnvelope 重入 → rpc 只跑一次', async () => {
    const { window, mp } = await setup();
    try {
        let rpcCount = 0;
        let resolveRpc;
        const rpcGate = new Promise((res) => { resolveRpc = res; });
        const spec = minimalSpec({
            rpc: async () => {
                rpcCount += 1;
                await rpcGate; // hang
                return { message: 'ok' };
            },
        });

        mp.run(spec);
        fillCtxInputs(window);

        // 觸發兩次 — 第二次應被 reentrant guard 擋下
        const first = mp._runEnvelope(spec);
        const second = mp._runEnvelope(spec);

        // 給 microtask 機會跑到 rpc 內 await rpcGate(讓 rpcCount 自增)
        await Promise.resolve();
        await Promise.resolve();

        assert.equal(rpcCount, 1, '重入呼叫應被 guard 擋下,rpc 只跑一次');
        assert.equal(mp._running, true, '首呼仍 in-flight,_running 應為 true');

        // 收尾:釋放 first 並等兩個 promise 結束
        resolveRpc();
        await first;
        await second;
        assert.equal(mp._running, false, '結束後 _running 必 reset');
    } finally {
        await teardown(window);
    }
});

test('5. registerCleanup:cleanup fn 在下次 run 開頭被 flush 並 call 一次', async () => {
    const { window, mp } = await setup();
    try {
        let cleanupCalls = 0;
        const spec = minimalSpec({
            onResult: (_result, _ctx, mpInst) => {
                mpInst.registerCleanup(() => { cleanupCalls += 1; });
            },
        });

        mp.run(spec);
        fillCtxInputs(window);

        // 首次 run:onResult 註冊 cleanup;cleanup 此時不應被觸發
        await mp._runEnvelope(spec);
        assert.equal(cleanupCalls, 0, '首次 run 結束後 cleanup 尚未 flush');

        // 第二次 run:flush 應在開頭被觸發,cleanup 跑一次
        await mp._runEnvelope(spec);
        assert.equal(cleanupCalls, 1, '第二次 run 開頭應 flush cleanup');

        // 第三次 run:第一次註冊的 cleanup 已被消耗,第二次 run 又重新註冊一個新的;
        // 第三次 run 開頭只 flush 第二次註冊的那一個。
        await mp._runEnvelope(spec);
        assert.equal(cleanupCalls, 2, '第三次 run 再 flush 一個新註冊的 cleanup');
    } finally {
        await teardown(window);
    }
});

test('6. registerCleanup:throwing cleanup 不會阻擋後續 cleanup 執行', async () => {
    // _flushCleanups 用 try/catch + console.error swallow,讓後續 cleanup 仍跑。
    // 此 invariant 鎖定「一個 bridge.unsubscribe throw 不能讓其他 unsubscribe 漏跑」。
    const { window, mp } = await setup();
    try {
        // 暫 swallow console.error,避免 test output noise(_flushCleanups 內 swallow + log)
        const origError = console.error;
        console.error = () => {};
        try {
            let counter = 0;
            mp.registerCleanup(() => { throw new Error('boom'); });
            mp.registerCleanup(() => { counter += 1; });
            // 觸發 flush — 跑下一次 envelope 就會 flush
            mp.run(minimalSpec());
            fillCtxInputs(window);
            await mp._runEnvelope(minimalSpec());
            assert.equal(counter, 1, 'second cleanup should run despite first throwing');
        } finally {
            console.error = origError;
        }
    } finally {
        await teardown(window);
    }
});

test('7. attachIframe ready promise:iframe load event 觸發後 ready resolve', async () => {
    const { window, mp } = await setup();
    try {
        // 預備一個 container — attachIframe 會把 iframe append 進去
        const container = window.document.createElement('div');
        container.id = 'iframeHostForTest';
        window.document.body.appendChild(container);

        const { iframe, ready } = mp.attachIframe({
            containerId: 'iframeHostForTest',
            html: '<html><body>hello</body></html>',
            height: '500px',
        });

        assert.ok(iframe, 'attachIframe 必回傳 iframe');
        // happy-dom 把 iframe.sandbox expose 為 DOMTokenList 而非 string,
        // 跟 production webview 行為一致(DOMTokenList toString → token list);
        // 用 String(...) coerce 比對單一 token 字面值。
        assert.equal(String(iframe.sandbox), 'allow-scripts', 'sandbox 必為 allow-scripts(ADR-0003 + P1-12)');
        assert.equal(iframe.style.height, '500px', '高度套用 spec');
        assert.equal(container.querySelector('iframe'), iframe, 'iframe append 進 containerId 容器');

        // 模擬 iframe load 完成 — 直接 await ready 是最 honest 的 behavior assertion:
        // 若 ready 永遠不 resolve,test 會 hang 被 node:test timeout 抓到,
        // 而不是靠 resolved flag 微秒級 microtask 計數來偽證。
        iframe.dispatchEvent(new window.Event('load'));
        await ready;
        assert.ok(true, 'attachIframe ready promise 在 iframe load event 後 resolve');
    } finally {
        await teardown(window);
    }
});

test('8. spec.runBtnLabelKey:default 為 button.start_analyze、override 走 spec 覆蓋值', async () => {
    // M2 prep API extension(ADR-0007 §6 spec shape):
    //   - Composer 的 run button label 是「生成圖表」而非「開始分析」,
    //     需要 spec 端可 override 而不擴 i18n schema。
    //   - 其他 4 panel 維持 default `button.start_analyze`。
    //
    // 此 test 釘 default + override 兩種行為,確保未來 spec 改動不會破默契。
    const { window: w1, mp: mp1 } = await setup();
    try {
        // 不傳 runBtnLabelKey → fallback default
        mp1.run(minimalSpec());
        const btn = w1.document.getElementById('mpRunBtn');
        assert.ok(btn, 'run button 必須存在');
        assert.match(
            btn.textContent,
            /button\.start_analyze/,
            'default 為 button.start_analyze(fake translator 回 __T:<key>__,assert 含 key)'
        );
    } finally {
        await teardown(w1);
    }

    const { window: w2, mp: mp2 } = await setup();
    try {
        mp2.run(minimalSpec({ runBtnLabelKey: 'button.generate_chart' }));
        const btn = w2.document.getElementById('mpRunBtn');
        assert.ok(btn, 'run button 必須存在');
        assert.match(
            btn.textContent,
            /button\.generate_chart/,
            'override 後 button label 走 spec.runBtnLabelKey'
        );
        assert.doesNotMatch(
            btn.textContent,
            /button\.start_analyze/,
            'override 後 default key 不應出現'
        );
    } finally {
        await teardown(w2);
    }
});

// ---------- panel shell hardcoded 中文回歸測試 ----------

test('panel shell 內 user-visible text 均來自 translator(無 hardcoded 繁中)', async () => {
    const { window, mp } = await setup();
    try {
        const spec = minimalSpec();
        mp.run(spec);

        const panel = globalThis.document.getElementById('functionPanel');
        const text = panel.textContent;

        const cjkRange = /[一-龥]/g;
        const chineseChars = text.match(cjkRange) || [];
        assert.equal(
            chineseChars.length,
            0,
            `ManifestPanel shell 內仍有 hardcoded 繁中字元(共 ${chineseChars.length} 個):` +
            `${[...new Set(chineseChars)].join('')}\n` +
            `任何未經 translator 包裹的字串都會洩漏到 textContent。`
        );
    } finally {
        await teardown(window);
    }
});
