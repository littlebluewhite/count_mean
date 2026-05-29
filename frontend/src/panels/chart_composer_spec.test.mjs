// Invariant + behavioural tests for chart_composer_spec.mjs(ADR-0007 §6 / M2)。
//
// 跑法:`node --test src/panels/chart_composer_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/panels/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位:titleKey / statusRunningKey / runBtnLabelKey /
//      silentSuccess / formBody / rpc / onResult
//   2. formBody 內 user-visible text 均來自 translator(無繁中字元洩漏)
//   3. formBody 呼叫 translator ≥ 2 次(ADR-0013 後僅 phase selector,sanity bound)
//   4. spec.rpc 只傳 manifest/dataFolder/subject 三參數給 GenerateChartComposer
//      (ADR-0013:不再有 channel-empty / loadedSubject pre-validation guard)
//   5. spec.rpc 在 result.success=false 時 throw
//
// Mock 策略:
//   * happy-dom 提供 DOM(formBody 內 textContent 抽繁中字元)
//   * fake translator 回 ASCII key — 任何未經 translator 的字串會以繁中字元洩漏到
//     textContent,被 cjkRange regex 抓到
//   * makeChartComposerSpec(app) factory 接 stub app;rpc test 透過 window.go stub
//     GenerateChartComposer 觀察 forward 的參數 / success=false throw
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { composerFormBody, makeChartComposerSpec } from './chart_composer_spec.mjs';

// ---------- spec shape 釘必填欄位 ----------

test('chartComposerSpec 必須暴露 ADR-0007 §6 spec shape 所有必填欄位', () => {
    // 因應 ADR-0007 §6 + M2 prep:Composer 額外需要 runBtnLabelKey('button.generate_chart')
    // 與 silentSuccess: true(其他 4 panel 省略)。
    const stubApp = {};
    const spec = makeChartComposerSpec(stubApp);

    assert.equal(typeof spec.titleKey, 'string', 'titleKey 應為 string');
    assert.equal(spec.titleKey, 'panel.composer.title', 'titleKey 對齊現有 i18n key');

    assert.equal(typeof spec.statusRunningKey, 'string', 'statusRunningKey 應為 string');

    assert.equal(typeof spec.runBtnLabelKey, 'string', 'runBtnLabelKey 應為 string');
    assert.equal(
        spec.runBtnLabelKey,
        'button.generate_chart',
        'runBtnLabelKey 鎖定為 button.generate_chart(M2 prep API extension 鎖定)'
    );

    assert.equal(spec.silentSuccess, true, 'silentSuccess 鎖定為 true(ADR-0007 §6 + handoff)');

    assert.equal(typeof spec.formBody, 'function', 'formBody 應為 builder function');
    assert.equal(typeof spec.rpc, 'function', 'rpc 應為 async function');
    assert.equal(typeof spec.onResult, 'function', 'onResult 應為 async function');

    // M5:loadSubjects(**name-mode**)+ onSubjectChange(reset state)必填;
    // afterRender 省略(Composer 無 phase 下拉,phase checkbox 在 onResult 內 render)。
    assert.equal(typeof spec.loadSubjects, 'function', 'Composer 有 loadSubjects(name-mode)');
    assert.equal(typeof spec.onSubjectChange, 'function', 'Composer 有 onSubjectChange(reset state)');
    assert.equal(spec.afterRender, undefined, 'Composer 省略 afterRender(無 phase 下拉)');
});

// ---------- spec.loadSubjects(M5,name-mode)+ onSubjectChange ----------

test('Composer spec.loadSubjects 走 LoadChartComposerSubjects 且 valueMode=name', async () => {
    // Composer subject 用 string(ADR-0002 canonical-key / ADR-0007 §4 name-mode)。
    // loadSubjects 呼 LoadChartComposerSubjects({manifestPath, dataFolder}) → result.subjects。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        let argObj = null;
        window.go = {
            gui: {
                App: {
                    LoadChartComposerSubjects: async (o) => {
                        argObj = o;
                        return { success: true, subjects: ['Rudolph', 'Donner'] };
                    },
                },
            },
        };
        const spec = makeChartComposerSpec({});
        const out = await spec.loadSubjects('/tmp/manifest.csv', '/tmp/data');

        assert.equal(argObj.manifestPath, '/tmp/manifest.csv', 'loadSubjects forward manifestPath');
        assert.equal(argObj.dataFolder, '/tmp/data', 'loadSubjects forward dataFolder(Composer 需要)');
        assert.deepEqual(out.subjects, ['Rudolph', 'Donner']);
        assert.equal(out.valueMode, 'name', 'Composer valueMode=name(subject 用字串)');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('Composer spec.loadSubjects 在 dataFolder 缺時回空(不打 RPC,對齊舊「兩者齊才 load」)', async () => {
    // 舊 selectComposerManifest 只在 dataFolder 已設時才 loadComposerSubjects;
    // backend LoadChartComposerSubjects RejectsEmptyDataFolder。故 dataFolder 缺時
    // loadSubjects 必須回空 subjects、不打 RPC、不 throw。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        let rpcCalled = false;
        window.go = {
            gui: {
                App: {
                    LoadChartComposerSubjects: async () => { rpcCalled = true; return { success: true, subjects: ['X'] }; },
                },
            },
        };
        const spec = makeChartComposerSpec({});
        const out = await spec.loadSubjects('/tmp/manifest.csv', ''); // dataFolder 空

        assert.equal(rpcCalled, false, 'dataFolder 缺時不應打 LoadChartComposerSubjects RPC');
        assert.deepEqual(out.subjects, [], 'dataFolder 缺時回空 subjects');
        assert.equal(out.valueMode, 'name', 'valueMode 仍為 name');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('Composer spec.loadSubjects 在 result.success=false 時 throw(對齊舊 loadComposerSubjects)', async () => {
    // 升 throw 讓 ManifestPanel selectMpManifest/DataFolder 的 catch 統一 ShowError。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        window.go = {
            gui: {
                App: {
                    LoadChartComposerSubjects: async () => ({ success: false, message: '主題載入失敗訊息' }),
                },
            },
        };
        const spec = makeChartComposerSpec({});
        await assert.rejects(
            async () => { await spec.loadSubjects('/tmp/m.csv', '/tmp/d'); },
            (err) => {
                assert.match(err.message, /主題載入失敗訊息|載入主題失敗/, 'throw 帶 backend message 或 fallback');
                return true;
            },
            'success=false 時 loadSubjects 應 throw'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('Composer spec.onSubjectChange 呼叫 app.onComposerSubjectChange()', () => {
    // ADR-0013 後 onComposerSubjectChange 清 chart container + 清勾選分期 Set
    //(不再 reset channel / offset state)。
    let changeCalled = 0;
    const app = { onComposerSubjectChange: () => { changeCalled += 1; } };
    const spec = makeChartComposerSpec(app);
    spec.onSubjectChange(/* mp */ {});
    assert.equal(changeCalled, 1, 'onSubjectChange 應呼叫 app.onComposerSubjectChange 一次');
});

// ---------- formBody i18n 防回歸 ----------

test('Chart Composer spec.formBody 內 user-visible text 均來自 translator(無 hardcoded 繁中)', () => {
    // fake translator 回 ASCII key;production 走 i18n dictionary。任何繁中字元
    // 洩漏到 textContent 代表該位置仍 hardcoded(漏走 translator)。
    const fakeT = (key) => `__T:${key}__`;
    const html = composerFormBody(fakeT);

    // 用 DOMParser 而非 innerHTML — 避開 XSS hook 警告(read-only parse 不寫 live DOM)。
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

test('Chart Composer spec.formBody 必呼叫 translator 至少 2 次(phase selector label/helptext)', () => {
    // 2 = ADR-0013 後 formBody 內 translator call 數的下界;新增 label 時連同此數 bump。
    //
    // 對應 2 個 translator call site:form.label.composer_phases /
    // form.helptext.composer_phases_pending(channel/load 相關標籤已隨一鍵生成刪除)。
    //
    // 此 lower bound 抓「人為把翻譯整段砍光」regression。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    composerFormBody(trackingT);

    assert.ok(
        calls.length >= 2,
        `formBody 至少應呼叫 translator 2 次,實際只呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});

// ---------- spec.rpc(ADR-0013 一鍵生成:只 forward 三參數 + success=false throw)----------

test('spec.rpc 只 forward manifest/dataFolder/subject 三參數給 GenerateChartComposer', async () => {
    // ADR-0013:rpc 不再讀 _composerSelectedChannels / _composerEMGMotionOffset,
    // 也不帶 selectedChannels / emgMotionOffset 參數(offset 由 backend 從 row 讀、
    // channel 預設全選)。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        let argObj = null;
        window.go = {
            gui: {
                App: {
                    GenerateChartComposer: async (o) => {
                        argObj = o;
                        return { success: true, html: '<div></div>', phaseTimes: {} };
                    },
                },
            },
        };
        const spec = makeChartComposerSpec({});
        const ctx = {
            manifestPath: '/tmp/manifest.csv',
            dataFolder: '/tmp/data',
            subjectIdx: NaN,
            subjectName: 'Rudolph',
            subjects: [],
        };

        const result = await spec.rpc(ctx);

        assert.equal(argObj.manifestPath, '/tmp/manifest.csv', 'forward manifestPath');
        assert.equal(argObj.dataFolder, '/tmp/data', 'forward dataFolder');
        assert.equal(argObj.subjectName, undefined, 'rpc 用 ctx.subjectName 填 subject 欄位');
        assert.equal(argObj.subject, 'Rudolph', 'forward subject(name-mode)');
        assert.ok(!('selectedChannels' in argObj), 'ADR-0013:不再帶 selectedChannels');
        assert.ok(!('emgMotionOffset' in argObj), 'ADR-0013:不再帶 emgMotionOffset');
        assert.equal(result.success, true);
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});

test('spec.rpc 在 result.success=false 時 throw(envelope 統一 ShowError)', async () => {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        window.go = {
            gui: {
                App: {
                    GenerateChartComposer: async () => ({ success: false, message: '生成失敗訊息' }),
                },
            },
        };
        const spec = makeChartComposerSpec({});
        const ctx = {
            manifestPath: '/tmp/manifest.csv',
            dataFolder: '/tmp/data',
            subjectIdx: NaN,
            subjectName: 'Rudolph',
            subjects: [],
        };

        await assert.rejects(
            async () => { await spec.rpc(ctx); },
            (err) => {
                assert.match(err.message, /生成失敗訊息|Chart Composer 生成失敗/, 'throw 帶 backend message 或 fallback');
                return true;
            },
            'rpc 應 throw 而非 return falsy'
        );
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
    }
});
