// Invariant + behavioural tests for normalized_phase_sync_spec.mjs(ADR-0007 §6 / M4)。
//
// 跑法:`node --test src/panels/normalized_phase_sync_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/**/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位 + silentSuccess === true(雙輸出路徑訊息、success=false 走業務
//      ShowError,dialog 由 onResult own);hideSubject 省略(用 subject)。
//   2. formBody 為 builder function 且不洩漏 hardcoded 繁中(4 個 phase 下拉)。
//   3. formBody 呼叫 translator ≥ 8 次(4 label + 4 placeholder option)。
//   4-7. spec.rpc 在 manifestPath / dataFolder / subjectIdx / 任一 phase 缺時 throw。
//   8. spec.rpc happy path:全必填齊 → 呼到 AnalyzeNormalizedPhaseSync 且
//      success=false 不升 throw + param 正確 map。
//   9. spec.onResult 在 result.success=false(軟失敗)時不 render 結果區、只彈 ShowError
//      (對齊舊 executeNormalizedPhaseSyncAnalysis else branch;codex round-2 P2 回歸守護)。
//
// Mock 策略(同 phase_sync_spec.test.mjs):rpc 從 DOM 讀 4 個 phase,故每個 test 先
// 在 document 內備好 4 個 select 並塞 value(helper setPhases)。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { normalizedPhaseSyncFormBody, makeNormalizedPhaseSyncSpec } from './normalized_phase_sync_spec.mjs';

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

// 在 happy-dom document 內建立 4 個 phase select 並塞 value。預設給合法 phase；
// 任一參數傳空字串即模擬「該下拉未選」。
function setPhases(window, {
    normStart = 'P0', normEnd = 'C', statsStart = 'P0', statsEnd = 'C',
} = {}) {
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
    make('normalizedPhaseSyncNormStartPhase', normStart);
    make('normalizedPhaseSyncNormEndPhase', normEnd);
    make('normalizedPhaseSyncStatsStartPhase', statsStart);
    make('normalizedPhaseSyncStatsEndPhase', statsEnd);
}

// ---------- spec shape 釘必填欄位 ----------

test('normalizedPhaseSyncSpec 必須暴露 ADR-0007 §6 spec shape + silentSuccess 特例', () => {
    const stubApp = {};
    const spec = makeNormalizedPhaseSyncSpec(stubApp);

    assert.equal(typeof spec.titleKey, 'string', 'titleKey 應為 string');
    assert.equal(spec.titleKey, 'panel.normalizedphasesync.title', 'titleKey 對齊現有 i18n key');

    assert.equal(typeof spec.statusRunningKey, 'string', 'statusRunningKey 應為 string');
    assert.equal(spec.statusRunningKey, 'status.normalized_running', 'statusRunningKey 對齊現有 i18n key(main.js:2312)');

    // silentSuccess=true:雙輸出路徑訊息 + success=false 業務 ShowError,dialog 由 onResult own。
    assert.equal(spec.silentSuccess, true, 'silentSuccess 鎖定為 true(dialog 由 onResult own)');

    // 用 subject(subjectIdx)→ hideSubject 省略走 default。
    assert.ok(
        spec.hideSubject === undefined || spec.hideSubject === false,
        'hideSubject 省略或 false(NormalizedPhaseSync 用 subject)'
    );

    assert.equal(typeof spec.formBody, 'function', 'formBody 應為 builder function');
    assert.equal(typeof spec.rpc, 'function', 'rpc 應為 async function');
    assert.equal(typeof spec.onResult, 'function', 'onResult 應為 async function');

    // M5:loadSubjects(index-mode)+ afterRender(4 個 phase 下拉 load + mirror)必填;
    // onSubjectChange 省略。
    assert.equal(typeof spec.loadSubjects, 'function', 'Normalized 有 loadSubjects(index-mode)');
    assert.equal(typeof spec.afterRender, 'function', 'Normalized 有 afterRender(load 4 phase 下拉)');
    assert.equal(spec.onSubjectChange, undefined, 'Normalized 省略 onSubjectChange');
});

// ---------- spec.loadSubjects + afterRender(M5)----------

test('Normalized spec.loadSubjects 走 LoadPhaseManifest 且 valueMode=index', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        window.go = {
            gui: { App: { LoadPhaseManifest: async () => ['N1', 'N2'] } },
        };
        const spec = makeNormalizedPhaseSyncSpec({});
        const out = await spec.loadSubjects('/tmp/m.csv', '');
        assert.deepEqual(out.subjects, ['N1', 'N2']);
        assert.equal(out.valueMode, 'index', 'Normalized valueMode=index');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('Normalized spec.afterRender 呼叫 app.loadNormalizedPhaseSyncPhases()', () => {
    // afterRender 對齊舊 showNormalizedPhaseSyncPanel:2189(內部會綁 mirror)。
    let loadCalled = 0;
    const app = { loadNormalizedPhaseSyncPhases: () => { loadCalled += 1; } };
    const spec = makeNormalizedPhaseSyncSpec(app);
    spec.afterRender(/* mp */ {});
    assert.equal(loadCalled, 1, 'afterRender 應呼叫 app.loadNormalizedPhaseSyncPhases 一次');
});

// ---------- formBody i18n 防回歸 ----------

test('NormalizedPhaseSync spec.formBody 內 user-visible text 均來自 translator(無 hardcoded 繁中)', () => {
    const fakeT = (key) => `__T:${key}__`;
    const html = normalizedPhaseSyncFormBody(fakeT);

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

test('NormalizedPhaseSync spec.formBody 必呼叫 translator 至少 8 次(4 label + 4 placeholder option)', () => {
    // 8 = 當前 formBody 內 translator call 數的下界:4 個 phase label(norm_start /
    // norm_end / stats_start / stats_end)+ 4 個 form.option.select placeholder。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    normalizedPhaseSyncFormBody(trackingT);

    assert.ok(
        calls.length >= 8,
        `formBody 至少應呼叫 translator 8 次,實際呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});

// ---------- spec.rpc pre-validation throw ----------

// 共用:備好合法 4 phase + 在指定 ctx 下斷言 rpc throw「必要欄位」語意。
async function assertRpcThrows(ctxOverrides, phaseOverrides) {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window, phaseOverrides);
        const spec = makeNormalizedPhaseSyncSpec({});
        await assert.rejects(
            async () => { await spec.rpc(validCtx(ctxOverrides)); },
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
}

test('spec.rpc 在 manifestPath 缺時 throw(對齊 main.js:2305-2310)', async () => {
    await assertRpcThrows({ manifestPath: '' }, {});
});

test('spec.rpc 在 dataFolder 缺時 throw', async () => {
    await assertRpcThrows({ dataFolder: '' }, {});
});

test('spec.rpc 在 subjectIdx 為 NaN 時 throw(未選 subject)', async () => {
    await assertRpcThrows({ subjectIdx: NaN }, {});
});

test('spec.rpc 在 normStartPhase 缺時 throw', async () => {
    await assertRpcThrows({}, { normStart: '' });
});

test('spec.rpc 在 normEndPhase 缺時 throw', async () => {
    await assertRpcThrows({}, { normEnd: '' });
});

test('spec.rpc 在 statsStartPhase 缺時 throw', async () => {
    await assertRpcThrows({}, { statsStart: '' });
});

test('spec.rpc 在 statsEndPhase 缺時 throw', async () => {
    await assertRpcThrows({}, { statsEnd: '' });
});

// ---------- spec.rpc happy path:success=false 不升 throw + param map ----------

test('spec.rpc 全必填齊 → 呼到 AnalyzeNormalizedPhaseSync 且 success=false 不升 throw + param 正確 map', async () => {
    // NormalizedPhaseSync 後端所有可預期失敗走 result.success=false;rpc 不升 throw
    //(對比 CCI),原樣回傳讓 onResult 判 + 彈業務 ShowError。此 test 同時釘 4 個 phase
    // 從 DOM 讀 + ctx.subjectIdx 正確 map 到後端 param。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    try {
        setPhases(window, { normStart: 'P0', normEnd: 'S', statsStart: 'C', statsEnd: 'D' });
        let analyzeParams = null;
        const fakeResult = { success: false, message: '標準化視窗時間越界' };
        window.go = {
            gui: {
                App: {
                    AnalyzeNormalizedPhaseSync: async (params) => {
                        analyzeParams = params;
                        return fakeResult;
                    },
                },
            },
        };

        const spec = makeNormalizedPhaseSyncSpec({});
        const result = await spec.rpc(validCtx({ subjectIdx: 3 }));

        assert.equal(result, fakeResult, 'success=false 時 rpc 應原樣回傳 result(不升 throw)');
        assert.ok(analyzeParams, 'rpc 應呼到 AnalyzeNormalizedPhaseSync');
        assert.equal(analyzeParams.manifestFile, '/tmp/manifest.csv', 'manifestFile 走 ctx.manifestPath');
        assert.equal(analyzeParams.dataFolder, '/tmp/data', 'dataFolder 走 ctx.dataFolder');
        assert.equal(analyzeParams.subjectIndex, 3, 'subjectIndex 走 ctx.subjectIdx');
        assert.equal(analyzeParams.normStartPhase, 'P0', 'normStartPhase 走 #normalizedPhaseSyncNormStartPhase value');
        assert.equal(analyzeParams.normEndPhase, 'S', 'normEndPhase 走 #normalizedPhaseSyncNormEndPhase value');
        assert.equal(analyzeParams.statsStartPhase, 'C', 'statsStartPhase 走 #normalizedPhaseSyncStatsStartPhase value');
        assert.equal(analyzeParams.statsEndPhase, 'D', 'statsEndPhase 走 #normalizedPhaseSyncStatsEndPhase value');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
    }
});

// ---------- spec.onResult 軟失敗:不 render 結果區、只彈 ShowError ----------

test('spec.onResult 在 result.success=false 時不 render 結果區、只彈 ShowError(codex round-2 P2)', async () => {
    // 軟失敗 result 只帶 success+message(無 subject/phase/路徑)。修正前 onResult 在
    // check success 前就 render rows → 顯示 undefined rows + 無用 open-folder 按鈕
    //(對齊舊 executeNormalizedPhaseSyncAnalysis else branch:只 ShowError、不 render)。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    window.document.body.innerHTML =
        '<div id="mpResult" style="display:none"><div id="mpResultContent"></div></div>';
    const errCalls = [];
    const msgCalls = [];
    globalThis.t = (key) => key;
    globalThis.ShowError = async (title, body) => { errCalls.push([title, body]); };
    globalThis.ShowMessage = async (title, body) => { msgCalls.push([title, body]); };
    try {
        const spec = makeNormalizedPhaseSyncSpec({});
        const mp = { openOutputFolder: () => { throw new Error('不該被呼叫'); } };
        await spec.onResult({ success: false, message: '標準化視窗時間越界' }, validCtx(), mp);

        const contentDiv = window.document.getElementById('mpResultContent');
        assert.equal(contentDiv.children.length, 0, '軟失敗時結果區應維持空(不 render row)');
        assert.equal(contentDiv.querySelector('.result-info'), null, '不應有 .result-info 區塊');

        assert.equal(errCalls.length, 1, 'ShowError 應被呼叫一次');
        assert.equal(errCalls[0][1], '標準化視窗時間越界', 'ShowError body 為 result.message');
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
