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

test('Chart Composer spec.formBody 收斂為空:無 user-visible 內容、不呼 translator(分期點已移結果區)', () => {
    // D6(ADR-0013):phase selector 從表單區搬到結果區(onResult 內由
    // bindPhaseCheckboxes 動態填),formBody 不再持有任何 user-visible 內容。
    // channel/load/warning 相關 UI 已於 D1 刪除。故 formBody 應收斂為空 —
    // 不呼 translator、無任何可見文字節點。此正向 test 取代舊「translator ≥ 2 次」
    // 下界,釘「formBody 收斂後無 hardcoded 內容」。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    const html = composerFormBody(trackingT);

    assert.equal(calls.length, 0, `formBody 收斂為空後不應呼 translator,實際呼了:${calls.join(', ')}`);

    // formBody 不得含任何 user-visible 文字(parse 後 textContent 去空白應為空)。
    const window = new Window();
    const parser = new window.DOMParser();
    const doc = parser.parseFromString(`<!doctype html><body>${html}</body>`, 'text/html');
    assert.equal(
        doc.body.textContent.trim(),
        '',
        'formBody 收斂後不應有 user-visible 文字(分期點已移結果區)'
    );
});

// ---------- spec.onResult(D6:分期點容器搬到結果區)----------

test('spec.onResult render 後 #composerPhaseSelector 容器存在於結果區', async () => {
    // D6:phase selector 從 formBody 搬到 onResult 的 #mpResultContent innerHTML
    //(chart container 之前)。bindPhaseCheckboxes(manifestPanel.mjs)是
    //「container 不存在則 silent no-op」,故必須確認 onResult 注入的 innerHTML
    // 在呼 bindPhaseCheckboxes 之前就已含 #composerPhaseSelector,checkbox 才會 render。
    // 此 test 釘:onResult 跑完後結果區存在 #composerPhaseSelector,且其內有
    // bindPhaseCheckboxes 填入的 phase checkbox(代表容器確實被找到、非 silent no-op)。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    globalThis.tHtml = (key) => `__T:${key}__`;
    try {
        // 結果區 shell(ManifestPanel _renderShell 提供 #mpResult / #mpResultContent)
        const result = window.document.createElement('div');
        result.id = 'mpResult';
        result.style.display = 'none';
        const content = window.document.createElement('div');
        content.id = 'mpResultContent';
        result.appendChild(content);
        window.document.body.appendChild(result);

        // 用真 ManifestPanel(attachIframe / bindPhaseCheckboxes)— onResult 依賴它。
        const { ManifestPanel } = await import('../manifestPanel.mjs');
        const app = {};
        const mp = new ManifestPanel(app);

        const spec = makeChartComposerSpec(app);
        // onResult 內 attachIframe 會掛 iframe,await ready(load event)。happy-dom
        // 對 srcdoc iframe 會自動 fire load,但保險起見在 microtask 後主動 dispatch。
        const onResultPromise = spec.onResult(
            { success: true, html: '<html><body>chart</body></html>', phaseTimes: { P0: 0, P1: 100, P2: 200 } },
            {},
            mp
        );
        // 推進 microtask 讓 attachIframe 把 iframe append 進 DOM,再手動觸發 load
        await Promise.resolve();
        const iframe = window.document.querySelector('#composerChartContent iframe');
        if (iframe) iframe.dispatchEvent(new window.Event('load'));
        await onResultPromise;

        // 結果區應顯示
        assert.equal(window.document.getElementById('mpResult').style.display, 'block', 'onResult 應顯示結果區');
        // 分期點容器存在於結果區
        const phaseSelector = window.document.getElementById('composerPhaseSelector');
        assert.ok(phaseSelector, '#composerPhaseSelector 容器應存在於結果區');
        // chart container 也在,且分期點容器排在 chart container 之前(DOM 順序)
        const chartContent = window.document.getElementById('composerChartContent');
        assert.ok(chartContent, '#composerChartContent 容器應存在');
        assert.ok(
            phaseSelector.compareDocumentPosition(chartContent) & window.Node.DOCUMENT_POSITION_FOLLOWING,
            '分期點容器應排在 chart container 之前'
        );
        // bindPhaseCheckboxes 確實找到容器並填入 checkbox(非 silent no-op)
        const checkboxes = phaseSelector.querySelectorAll('input[type="checkbox"]');
        assert.equal(checkboxes.length, 3, 'bindPhaseCheckboxes 應填入 3 個 phase checkbox(P0/P1/P2)');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
        delete globalThis.tHtml;
    }
});

// ---------- extraRunButtons + onUpdate enable 邏輯(D4 標準化視圖按鈕)----------

test('spec.extraRunButtons 產出 #composerStandardizeBtn 且初始 disabled', () => {
    // D4:標準化視圖按鈕由 spec.extraRunButtons 注入(shell button-group 內、
    // #mpRunBtn 之後)。生成前永遠灰 → HTML 必須帶 disabled。label 走 translator
    // (button.standardize_view),onclick 委派 app.standardizeComposerView()。
    const spec = makeChartComposerSpec({});
    assert.equal(typeof spec.extraRunButtons, 'function', 'extraRunButtons 應為 builder function');

    const fakeT = (key) => `__T:${key}__`;
    const html = spec.extraRunButtons(fakeT);

    // 解析成 DOM 驗 disabled attribute(read-only parse,避開 XSS hook)。
    const window = new Window();
    const parser = new window.DOMParser();
    const doc = parser.parseFromString(`<!doctype html><body>${html}</body>`, 'text/html');
    const btn = doc.getElementById('composerStandardizeBtn');

    assert.ok(btn, '應產出 #composerStandardizeBtn');
    assert.ok(btn.hasAttribute('disabled'), '標準化視圖按鈕初始必須 disabled(生成前灰)');
    assert.match(btn.getAttribute('onclick') || '', /standardizeComposerView/, 'onclick 委派 app.standardizeComposerView()');
    assert.match(btn.textContent, /button\.standardize_view/, 'label 走 translator(button.standardize_view)');
});

test('onResult 傳的 onUpdate 依勾選分期數 enable/disable 標準化視圖按鈕(>=2 亮、<2 灰)', async () => {
    // D4:onResult 呼 bindPhaseCheckboxes 時多傳 onUpdate;首次 render(全勾)就
    // enable,每次 checkbox change 即時更新。透過跑完整 onResult(render 3 phase
    // 全勾 → 按鈕應 enable),再 uncheck 到剩 1 個(< 2 → disable)驗證。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;
    globalThis.tHtml = (key) => `__T:${key}__`;
    try {
        // shell:#mpResult / #mpResultContent + extraRunButtons 注入的按鈕(初始 disabled)。
        const result = window.document.createElement('div');
        result.id = 'mpResult';
        result.style.display = 'none';
        const content = window.document.createElement('div');
        content.id = 'mpResultContent';
        result.appendChild(content);
        window.document.body.appendChild(result);

        const btn = window.document.createElement('button');
        btn.id = 'composerStandardizeBtn';
        btn.disabled = true; // 對齊 extraRunButtons 初始 disabled
        window.document.body.appendChild(btn);

        const { ManifestPanel } = await import('../manifestPanel.mjs');
        const app = {};
        const spec = makeChartComposerSpec(app);
        const mp = new ManifestPanel(app);

        const onResultPromise = spec.onResult(
            { success: true, html: '<html><body>c</body></html>', phaseTimes: { P0: 0, P1: 100, P2: 200 } },
            {},
            mp
        );
        await Promise.resolve();
        const iframe = window.document.querySelector('#composerChartContent iframe');
        if (iframe) iframe.dispatchEvent(new window.Event('load'));
        await onResultPromise;

        // 首次 render 全勾(3 個 ≥ 2)→ onUpdate 應 enable 按鈕。
        assert.equal(btn.disabled, false, '首次 render 全勾(3 phase)後標準化視圖按鈕應 enable');

        // uncheck 到剩 1 個 → < 2 → onUpdate 應 disable。
        const checkboxes = Array.from(
            window.document.querySelectorAll('#composerPhaseSelector input[type="checkbox"]')
        );
        assert.equal(checkboxes.length, 3, '應有 3 個 phase checkbox');
        // 取消其中兩個,觸發 change(emitUpdate → onUpdate)。
        checkboxes[0].checked = false;
        checkboxes[0].dispatchEvent(new window.Event('change'));
        checkboxes[1].checked = false;
        checkboxes[1].dispatchEvent(new window.Event('change'));

        assert.equal(btn.disabled, true, '勾選剩 1 個(< 2)時標準化視圖按鈕應 disable');

        // 重新勾回 1 個 → 共 2 個 ≥ 2 → 應 enable。
        checkboxes[0].checked = true;
        checkboxes[0].dispatchEvent(new window.Event('change'));
        assert.equal(btn.disabled, false, '勾選回到 2 個(>= 2)時標準化視圖按鈕應 enable');
    } finally {
        if (window?.happyDOM?.close) await window.happyDOM.close();
        delete globalThis.window;
        delete globalThis.document;
        delete globalThis.tHtml;
    }
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
