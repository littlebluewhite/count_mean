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

// ---------- spec.hideSubject(M4 prep)----------

test('8b. spec.hideSubject 省略(default):shell render subject select(#mpSubject 存在)', async () => {
    // M4 prep API extension(ADR-0007 §2 入口定義):4 個吃 subject 的 panel
    //(CCI / PhaseSync / NormalizedPhaseSync / Composer)走 default,subject
    // select 必須 render。此 test 釘 default 行為不被 hideSubject 引入破壞。
    const { window, mp } = await setup();
    try {
        mp.run(minimalSpec()); // 不傳 hideSubject → default
        const select = window.document.getElementById('mpSubject');
        assert.ok(select, 'default(hideSubject 省略)時 #mpSubject select 必須 render');
        assert.equal(select.tagName.toLowerCase(), 'select', '#mpSubject 應為 <select>');
    } finally {
        await teardown(window);
    }
});

test('8c. spec.hideSubject=true:shell 省略 subject select(#mpSubject 不存在)', async () => {
    // MuscleRatio 只吃 manifest + dataFolder(無 subject),設 hideSubject=true。
    // 此時 shell 不得 render subject select — 避免出現一個無 caller 讀取、永遠
    // disabled 的孤兒 select。manifest / dataFolder input 仍須存在。
    const { window, mp } = await setup();
    try {
        mp.run(minimalSpec({ hideSubject: true }));
        const select = window.document.getElementById('mpSubject');
        assert.equal(select, null, 'hideSubject=true 時 #mpSubject select 不應 render');
        // 其餘 shell 結構(manifest / dataFolder / run button)仍在。
        assert.ok(window.document.getElementById('mpManifestPath'), 'manifest input 仍須 render');
        assert.ok(window.document.getElementById('mpDataFolder'), 'dataFolder input 仍須 render');
        assert.ok(window.document.getElementById('mpRunBtn'), 'run button 仍須 render');
    } finally {
        await teardown(window);
    }
});

// ---------- bindPhaseCheckboxes onUpdate(M3 prep)----------

test('9. bindPhaseCheckboxes 帶 onUpdate callback:checkbox change 後呼叫 (pcts, checkedPhases)', async () => {
    // M3 prep API extension:CCI 需要 phase checkbox change 觸發後刷新 pct 文字
    // 面板(對齊既有 main.js _updatePhasePositionDisplay)。bindPhaseCheckboxes
    // 新增 optional onUpdate(pcts, checkedPhases) callback,在 bridge.send 完成
    // 後 invoke。本 test 釘:首次 render 一次 + 每次 checkbox change 各一次。
    const { window, mp } = await setup();
    try {
        // container 預備
        const container = window.document.createElement('div');
        container.id = 'phasesForTest';
        window.document.body.appendChild(container);

        // stub bridge.send(避免真的 postMessage)+ recalcPercents
        const sentMessages = [];
        const stubBridge = {
            send: (_iframe, type, payload) => { sentMessages.push({ type, payload }); },
        };
        // recalcPercents fake:每個 phase 一個 pct,name → pct map
        const recalcStub = (checkedPhases) => {
            const out = {};
            checkedPhases.forEach((p, i) => { out[p.name] = i * 10; });
            return out;
        };

        // onUpdate spy:每次呼叫記下 (pcts, checkedPhases)
        const updateCalls = [];
        const onUpdateSpy = (pcts, checkedPhases) => {
            updateCalls.push({ pcts, checkedPhases });
        };

        // 用 fake iframe stub(bindPhaseCheckboxes 只把它 forward 給 bridge.send)
        const fakeIframe = window.document.createElement('iframe');

        mp.bindPhaseCheckboxes({
            phaseTimes: { P0: 0, P1: 100, P2: 200 },
            adapter: 'cci',
            containerId: 'phasesForTest',
            bridge: stubBridge,
            iframe: fakeIframe,
            recalcPercents: recalcStub,
            checkedSet: new Set(),
            onUpdate: onUpdateSpy,
        });

        // 首次 render 後 emitUpdate 應觸發一次
        assert.equal(updateCalls.length, 1, '首次 render 應觸發一次 onUpdate');
        const first = updateCalls[0];
        assert.deepEqual(
            first.checkedPhases.sort(),
            ['P0', 'P1', 'P2'],
            '預設全勾 → checkedPhases 為 [P0, P1, P2]'
        );
        assert.equal(typeof first.pcts, 'object', 'pcts 應為 object(name → pct map)');

        // bridge.send 也該被呼叫一次,payload 含 checkedPhases
        assert.equal(sentMessages.length, 1, 'bridge.send 應呼叫一次');
        assert.equal(sentMessages[0].type, 'cci-update-phase-markers', 'message type 應為 ${adapter}-update-phase-markers');

        // 模擬 user 點 P0 checkbox(取消勾選)
        const cb = window.document.getElementById('cci_phase_P0');
        assert.ok(cb, 'cci_phase_P0 checkbox 應存在於 container');
        cb.checked = false;
        cb.dispatchEvent(new window.Event('change'));

        // 第二次 emit:onUpdate 應再被呼叫一次,checkedPhases 為 [P1, P2]
        assert.equal(updateCalls.length, 2, 'checkbox change 應觸發第二次 onUpdate');
        assert.deepEqual(
            updateCalls[1].checkedPhases.sort(),
            ['P1', 'P2'],
            '取消勾 P0 後 checkedPhases 為 [P1, P2]'
        );
    } finally {
        await teardown(window);
    }
});

test('10. bindPhaseCheckboxes 未傳 onUpdate:change 觸發不應 throw(向後相容)', async () => {
    // Composer 不需要 onUpdate,呼叫端會省略此參數。?.  guard 必須讓 emitUpdate
    // 在 onUpdate=undefined 時 silently no-op,不能 throw "is not a function"。
    const { window, mp } = await setup();
    try {
        const container = window.document.createElement('div');
        container.id = 'phasesForTestNoOnUpdate';
        window.document.body.appendChild(container);

        const sentMessages = [];
        const stubBridge = {
            send: (_iframe, type, payload) => { sentMessages.push({ type, payload }); },
        };
        const recalcStub = () => ({});
        const fakeIframe = window.document.createElement('iframe');

        // 故意省略 onUpdate
        mp.bindPhaseCheckboxes({
            phaseTimes: { P0: 0, P1: 100 },
            adapter: 'composer',
            containerId: 'phasesForTestNoOnUpdate',
            bridge: stubBridge,
            iframe: fakeIframe,
            recalcPercents: recalcStub,
            checkedSet: new Set(),
            // onUpdate omitted
        });

        // 首次 render 已隱含 emitUpdate;沒 throw 即過
        assert.equal(sentMessages.length, 1, '首次 emit bridge.send 仍正常');

        // 模擬 change,不應 throw
        const cb = window.document.getElementById('composer_phase_P0');
        cb.checked = false;
        assert.doesNotThrow(() => {
            cb.dispatchEvent(new window.Event('change'));
        }, 'change handler 在 onUpdate=undefined 時不應 throw');

        assert.equal(sentMessages.length, 2, 'change 後 bridge.send 仍正常觸發');
    } finally {
        await teardown(window);
    }
});

// ---------- M5 wiring:selectMpManifest / selectMpDataFolder / onMpSubjectChange ----------
//
// Subject load 兩形態(ADR-0007 §4):index-mode(CCI/PhaseSync/Normalized,
// option.value=0-based 索引)vs name-mode(Composer,option.value=subject 字串)。
// 這幾個 test 釘 ManifestPanel 的 subject-load helper 行為,不真正觸發 wails RPC
// (stub window.go.gui.App.{SelectFile,SelectDirectory})。

// stub wails bindings(SelectFile / SelectDirectory)— manifestPanel.mjs 直接 import,
// 呼叫時走 window['go']['gui']['App'][name]。回傳值由各 test 控制。
function stubWails(window, { file = '/tmp/picked.csv', folder = '/tmp/picked-dir' } = {}) {
    window.go = window.go || {};
    window.go.gui = window.go.gui || {};
    window.go.gui.App = window.go.gui.App || {};
    window.go.gui.App.SelectFile = async () => file;
    window.go.gui.App.SelectDirectory = async () => folder;
}

test('11. selectMpManifest:index-mode loadSubjects 填 #mpSubject(option.value=索引)', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { file: '/tmp/manifest.csv' });

        let loadArgs = null;
        const spec = minimalSpec({
            loadSubjects: async (manifestPath, dataFolder) => {
                loadArgs = { manifestPath, dataFolder };
                return { subjects: ['SubjA', 'SubjB', 'SubjC'], valueMode: 'index' };
            },
        });
        mp.run(spec);

        await mp.selectMpManifest();

        // manifest 寫入 input
        assert.equal(window.document.getElementById('mpManifestPath').value, '/tmp/manifest.csv');
        // loadSubjects 被呼叫,manifestPath forward 正確
        assert.equal(loadArgs.manifestPath, '/tmp/manifest.csv', 'loadSubjects 收到 manifestPath');

        // #mpSubject 填了 placeholder + 3 個 subject,index-mode option.value 為索引
        const select = window.document.getElementById('mpSubject');
        assert.equal(select.disabled, false, 'subject select 應 enable');
        const opts = Array.from(select.options);
        assert.equal(opts.length, 4, 'placeholder + 3 subject = 4 option');
        assert.equal(opts[1].value, '0', 'index-mode:第一個 subject option.value=0');
        assert.equal(opts[1].textContent, 'SubjA', 'option text 顯示 subject 名');
        assert.equal(opts[3].value, '2', 'index-mode:第三個 subject option.value=2');
    } finally {
        await teardown(window);
    }
});

test('12. selectMpManifest:name-mode loadSubjects 填 #mpSubject(option.value=subject 字串)', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { file: '/tmp/composer-manifest.csv' });

        const spec = minimalSpec({
            loadSubjects: async () => ({ subjects: ['Alice', 'Bob'], valueMode: 'name' }),
        });
        mp.run(spec);

        await mp.selectMpManifest();

        const select = window.document.getElementById('mpSubject');
        const opts = Array.from(select.options);
        assert.equal(opts.length, 3, 'placeholder + 2 subject');
        // name-mode:option.value 為 subject 字串(ADR-0002 canonical-key),非索引
        assert.equal(opts[1].value, 'Alice', 'name-mode:option.value=subject 字串');
        assert.equal(opts[2].value, 'Bob', 'name-mode:option.value=subject 字串');
        assert.equal(opts[1].textContent, 'Alice');
    } finally {
        await teardown(window);
    }
});

test('13. selectMpManifest:hideSubject=true 時不呼 loadSubjects(MuscleRatio)', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { file: '/tmp/m.csv' });

        let loadCalled = false;
        const spec = minimalSpec({
            hideSubject: true,
            loadSubjects: async () => { loadCalled = true; return { subjects: [], valueMode: 'index' }; },
        });
        mp.run(spec);

        await mp.selectMpManifest();

        // manifest 仍寫入,但 loadSubjects 不該被呼(hideSubject → 無 subject select)
        assert.equal(window.document.getElementById('mpManifestPath').value, '/tmp/m.csv');
        assert.equal(loadCalled, false, 'hideSubject=true 時不應 load subjects');
        // hideSubject → shell 無 #mpSubject
        assert.equal(window.document.getElementById('mpSubject'), null, 'hideSubject 時無 subject select');
    } finally {
        await teardown(window);
    }
});

test('14. selectMpManifest:user cancel(SelectFile 回 falsy)→ 不動 input、不 load', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { file: '' }); // cancel

        let loadCalled = false;
        const spec = minimalSpec({
            loadSubjects: async () => { loadCalled = true; return { subjects: [], valueMode: 'index' }; },
        });
        mp.run(spec);
        // 預填一個既有值,cancel 後不應被覆蓋
        window.document.getElementById('mpManifestPath').value = '/existing.csv';

        await mp.selectMpManifest();

        assert.equal(window.document.getElementById('mpManifestPath').value, '/existing.csv', 'cancel 不動既有值');
        assert.equal(loadCalled, false, 'cancel 後不 load subjects');
    } finally {
        await teardown(window);
    }
});

test('15. selectMpManifest:loadSubjects throw → ShowError(冒泡到 catch)', async () => {
    const { window, mp, calls } = await setup();
    try {
        stubWails(window, { file: '/tmp/m.csv' });

        const spec = minimalSpec({
            loadSubjects: async () => { throw new Error('後端載入失敗'); },
        });
        mp.run(spec);

        // 暫 swallow console.error noise
        const origError = console.error;
        console.error = () => {};
        try {
            await mp.selectMpManifest();
        } finally {
            console.error = origError;
        }

        assert.equal(calls.showError.length, 1, 'loadSubjects throw 應觸發 ShowError 一次');
    } finally {
        await teardown(window);
    }
});

test('16. selectMpDataFolder:寫 #mpDataFolder + 若 manifest 已選則 re-load subjects(Composer)', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { folder: '/tmp/data-dir' });

        let loadArgs = null;
        const spec = minimalSpec({
            loadSubjects: async (manifestPath, dataFolder) => {
                loadArgs = { manifestPath, dataFolder };
                return { subjects: ['X'], valueMode: 'name' };
            },
        });
        mp.run(spec);
        // 先有 manifest(模擬 user 已選 manifest 再選 dataFolder)
        window.document.getElementById('mpManifestPath').value = '/tmp/manifest.csv';

        await mp.selectMpDataFolder();

        assert.equal(window.document.getElementById('mpDataFolder').value, '/tmp/data-dir');
        // manifest 已在 → 應 re-load subjects 且 dataFolder forward 正確(Composer 需要)
        assert.ok(loadArgs, 'manifest 已選 → selectMpDataFolder 應 re-load subjects');
        assert.equal(loadArgs.dataFolder, '/tmp/data-dir', 'loadSubjects 收到 dataFolder');
        assert.equal(loadArgs.manifestPath, '/tmp/manifest.csv', 'loadSubjects 收到 manifestPath');
    } finally {
        await teardown(window);
    }
});

test('17. selectMpDataFolder:manifest 未選時不 re-load subjects', async () => {
    const { window, mp } = await setup();
    try {
        stubWails(window, { folder: '/tmp/data-dir' });

        let loadCalled = false;
        const spec = minimalSpec({
            loadSubjects: async () => { loadCalled = true; return { subjects: [], valueMode: 'index' }; },
        });
        mp.run(spec);
        // manifest 留空

        await mp.selectMpDataFolder();

        assert.equal(window.document.getElementById('mpDataFolder').value, '/tmp/data-dir');
        assert.equal(loadCalled, false, 'manifest 未選 → 不 load subjects');
    } finally {
        await teardown(window);
    }
});

test('18. afterRender:run() render + showPanel 後呼叫一次,收到 mp', async () => {
    const { window, mp } = await setup();
    try {
        let afterRenderArg = 'NOT_CALLED';
        let formBodyExistedAtAfterRender = false;
        const spec = minimalSpec({
            formBody: (t) => `<div id="afterRenderProbe">${t('x')}</div>`,
            afterRender: (mpInst) => {
                afterRenderArg = mpInst;
                // afterRender 必須在 formBody 已 render 進 DOM 後才跑(PhaseSync 需要
                // 找到 phase 下拉)。
                formBodyExistedAtAfterRender = !!window.document.getElementById('afterRenderProbe');
            },
        });

        mp.run(spec);

        assert.equal(afterRenderArg, mp, 'afterRender 收到 ManifestPanel 實例');
        assert.equal(formBodyExistedAtAfterRender, true, 'afterRender 時 formBody 已在 DOM');
    } finally {
        await teardown(window);
    }
});

test('19. afterRender 省略:run() 不 throw(向後相容)', async () => {
    const { window, mp } = await setup();
    try {
        // 不傳 afterRender
        assert.doesNotThrow(() => {
            mp.run(minimalSpec());
        }, 'afterRender=undefined 時 run() 不應 throw');
    } finally {
        await teardown(window);
    }
});

test('20. onMpSubjectChange:forward 到 spec.onSubjectChange(收到 mp)', async () => {
    const { window, mp } = await setup();
    try {
        let changeArg = 'NOT_CALLED';
        const spec = minimalSpec({
            onSubjectChange: (mpInst) => { changeArg = mpInst; },
        });
        mp.run(spec);

        mp.onMpSubjectChange();

        assert.equal(changeArg, mp, 'onSubjectChange 收到 ManifestPanel 實例');
    } finally {
        await teardown(window);
    }
});

test('21. onMpSubjectChange:spec.onSubjectChange 省略 → no-op 不 throw', async () => {
    const { window, mp } = await setup();
    try {
        mp.run(minimalSpec()); // 無 onSubjectChange
        assert.doesNotThrow(() => {
            mp.onMpSubjectChange();
        }, 'onSubjectChange=undefined 時 onMpSubjectChange() 不應 throw');
    } finally {
        await teardown(window);
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
