// Invariant + behavioural tests for cci_spec.mjs(ADR-0007 §6 / M3)。
//
// 跑法:`node --test src/panels/cci_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/panels/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位:titleKey / statusRunningKey / formBody / rpc / onResult。
//      CCI 是「標準案例」:runBtnLabelKey 省略(undefined → shell fallback
//      'button.start_analyze')、silentSuccess 省略(undefined → 預設 false)。
//   2. formBody 為 builder function 且不洩漏 hardcoded 繁中(CCI 無額外欄位 →
//      回空字串,vacuous-pass;此 test 釘「未來加欄位必經 translator」的契約)。
//   3. spec.rpc 在 manifestPath 缺時 throw。
//   4. spec.rpc 在 dataFolder 缺時 throw。
//   5. spec.rpc 在 subjectIdx 為 NaN / 負時 throw。
//   6. spec.rpc 在 backend result.success=false 時 throw(升 soft error 為 ShowError)。
//
// Mock 策略:
//   * happy-dom 提供 DOM(formBody parse / textContent 抽繁中字元)。
//   * makeCciSpec(app) factory 接 stub app — rpc throw test(3/4/5)只測 guard,
//     pre-validation throw 在 AnalyzeCCI binding 觸發前已先擋下,故不需要 stub
//     window.go。test 6 要走到 AnalyzeCCI,故 stub window.go.gui.App.AnalyzeCCI
//     回 { success:false, message:... } 驗 throw。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { cciFormBody, makeCciSpec } from './cci_spec.mjs';

// 合法 ctx(rpc 不卡在 guard 用)。manifestPath / dataFolder / subjectIdx 對應
// ManifestPanel _gatherCtx 規範化命名(rpc 內 map 到後端 manifestFile /
// dataFolder / subjectIndex)。
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

// ---------- spec shape 釘必填欄位 ----------

test('cciSpec 必須暴露 ADR-0007 §6 spec shape 所有必填欄位(CCI 標準案例)', () => {
    // CCI 是 ManifestPanel 標準案例:runBtnLabelKey 省略走 default、
    // silentSuccess 省略走 false(對比 Composer 兩者都覆寫)。
    const stubApp = {};
    const spec = makeCciSpec(stubApp);

    assert.equal(typeof spec.titleKey, 'string', 'titleKey 應為 string');
    assert.equal(spec.titleKey, 'panel.cci.title', 'titleKey 對齊現有 i18n key');

    assert.equal(typeof spec.statusRunningKey, 'string', 'statusRunningKey 應為 string');
    assert.equal(spec.statusRunningKey, 'status.cci_running', 'statusRunningKey 對齊現有 i18n key(main.js:1344)');

    // CCI 標準案例:runBtnLabelKey 省略 → shell fallback 'button.start_analyze'。
    assert.equal(
        spec.runBtnLabelKey,
        undefined,
        'runBtnLabelKey 省略(CCI 走 shell default button.start_analyze,不覆寫)'
    );

    // silentSuccess 省略 → 預設 false → envelope 成功後彈 ShowMessage。
    assert.ok(
        spec.silentSuccess === undefined || spec.silentSuccess === false,
        'silentSuccess 省略或 false(CCI 成功要 ShowMessage,不像 Composer silent)'
    );

    assert.equal(typeof spec.formBody, 'function', 'formBody 應為 builder function');
    assert.equal(typeof spec.rpc, 'function', 'rpc 應為 async function');
    assert.equal(typeof spec.onResult, 'function', 'onResult 應為 async function');
});

// ---------- formBody i18n 防回歸 ----------

test('CCI spec.formBody 為 builder function 且不洩漏 hardcoded 繁中', () => {
    // CCI 無額外表單欄位(manifest / dataFolder / subject 全由 shell 提供),
    // formBody 回空字串 → 必然無繁中洩漏(vacuous-pass)。此 test 釘契約:
    // 未來若 M4/新需求往 CCI formBody 加欄位,任何 hardcoded 繁中都會被抓。
    const fakeT = (key) => `__T:${key}__`;
    const html = cciFormBody(fakeT);

    assert.equal(typeof html, 'string', 'formBody 應回 string');

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

// ---------- spec.rpc pre-validation throw ----------

test('spec.rpc 在 manifestPath 缺時 throw(對齊 main.js:1339-1342)', async () => {
    const spec = makeCciSpec({});
    const ctx = validCtx({ manifestPath: '' });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            // 對齊 main.js:1340 error.msg.fill_required_fields 語意。
            assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 在 dataFolder 缺時 throw', async () => {
    const spec = makeCciSpec({});
    const ctx = validCtx({ dataFolder: '' });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 在 subjectIdx 為 NaN 時 throw(未選 subject)', async () => {
    const spec = makeCciSpec({});
    // 未選 subject 時 ManifestPanel _gatherCtx 的 parseInt('') === NaN。
    const ctx = validCtx({ subjectIdx: NaN });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 在 subjectIdx 為負時 throw', async () => {
    const spec = makeCciSpec({});
    const ctx = validCtx({ subjectIdx: -1 });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            assert.match(err.message, /必要欄位|必填欄位/, 'throw 訊息含「請填寫所有必要欄位」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

// ---------- spec.rpc backend soft-error 升 throw ----------

test('spec.rpc 在 backend result.success=false 時 throw(升 soft error 為 ShowError)', async () => {
    // backend HandlerRun 用 Success=false 表 soft error(cci_handlers.go:114
    // failedCCIResult)。envelope 需 throw 才走 ShowError,故 rpc 把 soft error
    // 升 throw。此 test 走到 AnalyzeCCI binding(window['go']['gui']['App']),
    // 故 stub window.go。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        let analyzeCalled = false;
        window.go = {
            gui: {
                App: {
                    AnalyzeCCI: async (_params) => {
                        analyzeCalled = true;
                        return { success: false, message: '後端分析失敗訊息' };
                    },
                },
            },
        };

        const spec = makeCciSpec({});
        const ctx = validCtx(); // 合法 ctx → 過 guard → 走到 AnalyzeCCI

        await assert.rejects(
            async () => { await spec.rpc(ctx); },
            (err) => {
                // soft error 升 throw 時帶 backend message。
                assert.match(err.message, /後端分析失敗訊息|CCI 分析失敗/, 'throw 訊息應含 backend message 或 fallback');
                return true;
            },
            'success=false 時 rpc 應 throw 讓 envelope 走 ShowError'
        );
        assert.equal(analyzeCalled, true, '合法 ctx 應實際呼到 AnalyzeCCI(guard 已通過)');
    } finally {
        if (window?.happyDOM?.close) {
            await window.happyDOM.close();
        }
        delete globalThis.window;
    }
});

test('spec.rpc 在 backend result.success=true 時回傳 result(happy path 傳遞)', async () => {
    // success=true 時 rpc 直接回 result 給 envelope → onResult。此 test 釘
    // rpc 不會誤把成功也升 throw。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        const fakeResult = {
            success: true,
            message: 'CCI 分析完成',
            subject: 'SubjectA',
            outputCSVPath: '/out/cci.csv',
            pairNames: ['RF-BF'],
            chartHTML: '<html></html>',
            phaseTimes: { P0: 0, P1: 100 },
            phasePercents: { P0: 0, P1: 50 },
            report: 'report-text',
        };
        window.go = {
            gui: { App: { AnalyzeCCI: async (_params) => fakeResult } },
        };

        const spec = makeCciSpec({});
        const result = await spec.rpc(validCtx());

        assert.equal(result, fakeResult, 'success=true 時 rpc 應原樣回傳 result');
        assert.equal(result.success, true);
    } finally {
        if (window?.happyDOM?.close) {
            await window.happyDOM.close();
        }
        delete globalThis.window;
    }
});
