// Invariant + behavioural tests for phase_sync_spec.mjs(ADR-0007 §6 / M4)。
//
// 跑法:`node --test src/panels/phase_sync_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/**/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位:titleKey / statusRunningKey / formBody / rpc / onResult。
//      PhaseSync 特例:silentSuccess === true(成功訊息帶 outputPath、success=false
//      走業務 ShowError,dialog 由 onResult own);hideSubject 省略(用 subject)。
//   2. formBody 為 builder function 且不洩漏 hardcoded 繁中(start/end phase 下拉)。
//   3. formBody 呼叫 translator ≥ 4 次(2 label + 2 placeholder option)。
//   4. spec.rpc 在 manifestPath 缺時 throw。
//   5. spec.rpc 在 dataFolder 缺時 throw。
//   6. spec.rpc 在 subjectIdx NaN/<0 時 throw。
//   7. spec.rpc 在 startPhase 缺時 throw。
//   8. spec.rpc 在 endPhase 缺時 throw。
//   9. spec.rpc happy path:全必填齊 → 呼到 AnalyzePhaseSync 且 success=false 不升 throw。
//  10. spec.onResult 在 result.success=false(軟失敗)時不 render 結果區、只彈 ShowError
//      (對齊舊 executePhaseSyncAnalysis else branch;codex round-2 P2 回歸守護)。
//
// Mock 策略:
//   * happy-dom 提供 DOM(formBody parse + rpc 讀 #phaseSyncStartPhase/EndPhase value)。
//   * makePhaseSyncSpec(app) factory 接 stub app。rpc throw test 在 startPhase/endPhase
//     缺或 ctx 缺時於 AnalyzePhaseSync binding 觸發前已先擋下,不需 stub window.go。
//     test 9 要走到 AnalyzePhaseSync,故 stub window.go。
//   * rpc 從 DOM 讀 startPhase/endPhase — 每個 test 需先在 document 內備好兩個 select
//     並塞 value(helper setPhases)。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { phaseSyncFormBody, makePhaseSyncSpec } from './phase_sync_spec.mjs';

// 合法 ctx(rpc 不卡在 ctx guard 用)。
function validCtx(overrides = {}) {
    return {
        manifestPath: '/tmp/manifest.csv',
        dataFolder: '/tmp/data',
        subjectIdx: 0,
        subjectName: 'SubjectA',
        subjects: [],
        ...overrides,
    };
}

// 在 happy-dom document 內建立 phaseSyncStartPhase / phaseSyncEndPhase 兩個 select
// 並塞 value，供 rpc 從 DOM 讀。預設給合法 phase。
function setPhases(window, { start = 'P0', end = 'C' } = {}) {
    const make = (id, val) => {
        let el = window.document.getElementById(id);
        if (!el) {
            el = window.document.createElement('select');
            el.id = id;
            const opt = window.document.createElement('option');
            opt.value = val;
            el.appendChild(opt);
            window.document.body.appendChild(el);
        }
        el.value = val;
    };
    make('phaseSyncStartPhase', start);
    make('phaseSyncEndPhase', end);
}

// ---------- spec shape 釘必填欄位 ----------

test('phaseSyncSpec 必須暴露 ADR-0007 §6 spec shape + silentSuccess 特例', () => {
    const stubApp = {};
    const spec = makePhaseSyncSpec(stubApp);

    assert.equal(typeof spec.titleKey, 'string', 'titleKey 應為 string');
    assert.equal(spec.titleKey, 'panel.phasesync.title', 'titleKey 對齊現有 i18n key');

    assert.equal(typeof spec.statusRunningKey, 'string', 'statusRunningKey 應為 string');
    assert.equal(spec.statusRunningKey, 'status.phasesync_running', 'statusRunningKey 對齊現有 i18n key(main.js:1146)');

    // silentSuccess=true:成功訊息帶 outputPath、success=false 走業務 ShowError,
    // dialog 由 onResult own(有別於 CCI rpc-throw + envelope ShowMessage)。
    assert.equal(spec.silentSuccess, true, 'silentSuccess 鎖定為 true(dialog 由 onResult own)');

    // PhaseSync 用 subject(subjectIdx)→ hideSubject 省略走 default(顯示 subject select)。
    assert.ok(
        spec.hideSubject === undefined || spec.hideSubject === false,
        'hideSubject 省略或 false(PhaseSync 用 subject)'
    );

    assert.equal(typeof spec.formBody, 'function', 'formBody 應為 builder function');
    assert.equal(typeof spec.rpc, 'function', 'rpc 應為 async function');
    assert.equal(typeof spec.onResult, 'function', 'onResult 應為 async function');

    // M5:loadSubjects(index-mode)+ afterRender(phase 下拉 load)必填;
    // onSubjectChange 省略(subject change 無特殊行為)。
    assert.equal(typeof spec.loadSubjects, 'function', 'PhaseSync 有 loadSubjects(index-mode)');
    assert.equal(typeof spec.afterRender, 'function', 'PhaseSync 有 afterRender(load phase 下拉)');
    assert.equal(spec.onSubjectChange, undefined, 'PhaseSync 省略 onSubjectChange');
});

// ---------- spec.loadSubjects + afterRender(M5)----------

test('PhaseSync spec.loadSubjects 走 LoadPhaseManifest 且 valueMode=index', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        window.go = {
            gui: { App: { LoadPhaseManifest: async () => ['A', 'B', 'C'] } },
        };
        const spec = makePhaseSyncSpec({});
        const out = await spec.loadSubjects('/tmp/m.csv', '');
        assert.deepEqual(out.subjects, ['A', 'B', 'C']);
        assert.equal(out.valueMode, 'index', 'PhaseSync valueMode=index');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('PhaseSync spec.afterRender 呼叫 app.loadAvailablePhases()', () => {
    // afterRender 對齊舊 showPhaseSyncPanel:1045 的 loadAvailablePhases() 呼叫時機。
    let loadCalled = 0;
    const app = { loadAvailablePhases: () => { loadCalled += 1; } };
    const spec = makePhaseSyncSpec(app);
    spec.afterRender(/* mp */ {});
    assert.equal(loadCalled, 1, 'afterRender 應呼叫 app.loadAvailablePhases 一次');
});

// ---------- formBody i18n 防回歸 ----------

test('PhaseSync spec.formBody 內 user-visible text 均來自 translator(無 hardcoded 繁中)', () => {
    const fakeT = (key) => `__T:${key}__`;
    const html = phaseSyncFormBody(fakeT);

    const window = new Window();
    const parser = new window.DOMParser();
    const doc = parser.parseFromString(`<!doctype html><body>${html}</body>`, 'text/html');
    const visibleText = doc.body.textContent;

    const cjkRange = /[一-龥]/g;
    const chineseChars = visibleText.match(cjkRange) || [];

    assert.equal(
        chineseChars.length,
        0,
        `formBody 內仍有 hardcoded 繁中字元(共 ${chineseChars.length} 個):` +
        `${[...new Set(chineseChars)].join('')}\n` +
        `任何未經 translator 包裹的字串都會洩漏到 textContent。`
    );
});

test('PhaseSync spec.formBody 必呼叫 translator 至少 4 次(2 label + 2 placeholder option)', () => {
    // 4 = 當前 formBody 內 translator call 數的下界:form.label.start_phase /
    // form.label.end_phase + 兩個 form.option.select placeholder。抓「翻譯被整段砍光」regression。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    phaseSyncFormBody(trackingT);

    assert.ok(
        calls.length >= 4,
        `formBody 至少應呼叫 translator 4 次,實際呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});

// ---------- spec.rpc pre-validation throw ----------

test('spec.rpc 在 manifestPath 缺時 throw(對齊 main.js:1141-1144)', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window);
        const spec = makePhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx({ manifestPath: '' })); },
            (err) => {
                assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
                return true;
            },
            'rpc 應 throw 而非 return falsy'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

test('spec.rpc 在 dataFolder 缺時 throw', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window);
        const spec = makePhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx({ dataFolder: '' })); },
            (err) => {
                assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
                return true;
            },
            'rpc 應 throw 而非 return falsy'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

test('spec.rpc 在 subjectIdx 為 NaN 時 throw(未選 subject)', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window);
        const spec = makePhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx({ subjectIdx: NaN })); },
            (err) => {
                assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
                return true;
            },
            'rpc 應 throw 而非 return falsy'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

test('spec.rpc 在 startPhase 缺時 throw', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window, { start: '', end: 'C' }); // startPhase 空
        const spec = makePhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx()); },
            (err) => {
                assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
                return true;
            },
            'startPhase 空時 rpc 應 throw'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

test('spec.rpc 在 endPhase 缺時 throw', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window, { start: 'P0', end: '' }); // endPhase 空
        const spec = makePhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx()); },
            (err) => {
                assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
                return true;
            },
            'endPhase 空時 rpc 應 throw'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

// ---------- spec.rpc happy path:success=false 不升 throw ----------

test('spec.rpc 全必填齊 → 呼到 AnalyzePhaseSync 且 success=false 不升 throw(dual 通道,onResult own dialog)', async () => {
    // PhaseSync 後端 dual 通道:Execute/Write 軟失敗走 result.success=false。rpc 不升
    // throw(對比 CCI),原樣回傳讓 onResult 判 + 彈業務 ShowError。此 test 同時釘
    // rpc 把 DOM 的 startPhase/endPhase + ctx.subjectIdx 正確 map 到後端 param。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window, { start: 'P0', end: 'C' });
        let analyzeParams = null;
        const fakeResult = { success: false, message: '分期時間越界', outputPath: '' };
        window.go = {
            gui: {
                App: {
                    AnalyzePhaseSync: async (params) => {
                        analyzeParams = params;
                        return fakeResult;
                    },
                },
            },
        };

        const spec = makePhaseSyncSpec({});
        const result = await spec.rpc(validCtx({ subjectIdx: 2 }));

        assert.equal(result, fakeResult, 'success=false 時 rpc 應原樣回傳 result(不升 throw)');
        assert.ok(analyzeParams, 'rpc 應呼到 AnalyzePhaseSync');
        assert.equal(analyzeParams.manifestFile, '/tmp/manifest.csv', 'manifestFile 走 ctx.manifestPath');
        assert.equal(analyzeParams.dataFolder, '/tmp/data', 'dataFolder 走 ctx.dataFolder');
        assert.equal(analyzeParams.subjectIndex, 2, 'subjectIndex 走 ctx.subjectIdx');
        assert.equal(analyzeParams.startPhase, 'P0', 'startPhase 走 #phaseSyncStartPhase value');
        assert.equal(analyzeParams.endPhase, 'C', 'endPhase 走 #phaseSyncEndPhase value');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

// ---------- spec.onResult 軟失敗:不 render 結果區、只彈 ShowError ----------

test('spec.onResult 在 result.success=false 時不 render 結果區、只彈 ShowError(codex round-2 P2)', async () => {
    // 軟失敗 result 只帶 success+message(無 subject/startPhase/outputPath)。
    // 修正前 onResult 在 check success 前就 render rows → 顯示 undefined (—s) → undefined
    //(對齊舊 executePhaseSyncAnalysis else branch:只 ShowError、不 render)。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    // onResult shell DOM(#mpResultContent 結果區容器 + #mpResult 外層);本 test 斷言
    // content 維持空(無 .result-info row)。
    window.document.body.innerHTML =
        '<div id="mpResult" style="display:none"><div id="mpResultContent"></div></div>';
    // i18n + dialog stub:t 回傳 key 本身;ShowError/ShowMessage 記錄呼叫。
    const errCalls = [];
    const msgCalls = [];
    globalThis.t = (key) => key;
    globalThis.ShowError = async (title, body) => { errCalls.push([title, body]); };
    globalThis.ShowMessage = async (title, body) => { msgCalls.push([title, body]); };
    try {
        const spec = makePhaseSyncSpec({});
        const mp = { openOutputFolder: () => { throw new Error('不該被呼叫'); } };
        await spec.onResult({ success: false, message: '分期時間越界' }, validCtx(), mp);

        const contentDiv = window.document.getElementById('mpResultContent');
        assert.equal(contentDiv.children.length, 0, '軟失敗時結果區應維持空(不 render row)');
        assert.equal(contentDiv.querySelector('.result-info'), null, '不應有 .result-info 區塊');

        assert.equal(errCalls.length, 1, 'ShowError 應被呼叫一次');
        assert.equal(errCalls[0][1], '分期時間越界', 'ShowError body 為 result.message');
        assert.equal(msgCalls.length, 0, '軟失敗不應呼叫 ShowMessage');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
        delete globalThis.t;
        delete globalThis.ShowError;
        delete globalThis.ShowMessage;
    }
});
