// Invariant + behavioural tests for muscle_ratio_spec.mjs(ADR-0007 §6 / M4)。
//
// 跑法:`node --test src/panels/muscle_ratio_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/**/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位:titleKey / statusRunningKey / formBody / rpc / onResult。
//      MuscleRatio 特例:hideSubject === true(無 subject)、silentSuccess === true
//     (partial-fail 三態 dialog 由 onResult own)。
//   2. formBody 為 builder function 且不洩漏 hardcoded 繁中(只有描述 help-text)。
//   3. formBody 呼叫 translator ≥ 1 次(描述文字)。
//   4. spec.rpc 在 manifestPath 缺時 throw。
//   5. spec.rpc 在 dataFolder 缺時 throw。
//   6. spec.rpc 不讀 subjectIdx — subjectIdx=NaN 但 manifest/folder 齊時不因 subject throw。
//
// Mock 策略:
//   * happy-dom 提供 DOM(formBody parse / textContent 抽繁中字元)。
//   * makeMuscleRatioSpec(app) factory 接 stub app。rpc throw test(4/5)只測 guard,
//     pre-validation throw 在 AnalyzeMuscleRatio binding 觸發前已先擋下,不需 stub
//     window.go。test 6 要走到 AnalyzeMuscleRatio,故 stub window.go 驗證 subjectIdx
//     不影響 rpc(MuscleRatio 無 subject)。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { muscleRatioFormBody, makeMuscleRatioSpec } from './muscle_ratio_spec.mjs';

// 合法 ctx(rpc 不卡在 guard 用)。MuscleRatio 只讀 manifestPath / dataFolder;
// subjectIdx 故意設 NaN(hideSubject=true 時 _gatherCtx 回 NaN),驗證 rpc 不讀它。
function validCtx(overrides = {}) {
    return {
        manifestPath: '/tmp/manifest.csv',
        dataFolder: '/tmp/data',
        subjectIdx: NaN,
        subjectName: '',
        subjects: [],
        ...overrides,
    };
}

// ---------- spec shape 釘必填欄位 ----------

test('muscleRatioSpec 必須暴露 ADR-0007 §6 spec shape + hideSubject/silentSuccess 特例', () => {
    // MuscleRatio 特例:hideSubject=true(無 subject 下拉)+ silentSuccess=true
    //(partial-fail 三態 dialog 由 onResult own,有別於 CCI 的 rpc-throw + envelope ShowMessage)。
    const stubApp = {};
    const spec = makeMuscleRatioSpec(stubApp);

    assert.equal(typeof spec.titleKey, 'string', 'titleKey 應為 string');
    assert.equal(spec.titleKey, 'panel.muscleratio.title', 'titleKey 對齊現有 i18n key');

    assert.equal(typeof spec.statusRunningKey, 'string', 'statusRunningKey 應為 string');
    assert.equal(spec.statusRunningKey, 'status.muscle_running', 'statusRunningKey 對齊現有 i18n key(main.js:2535)');

    // hideSubject=true:MuscleRatio 只吃 manifest + dataFolder,shell 省略 subject select。
    assert.equal(spec.hideSubject, true, 'hideSubject 鎖定為 true(MuscleRatio 無 subject,ADR-0007 §2)');

    // silentSuccess=true:onResult 自判 result.success 彈 success / partial-failed dialog。
    assert.equal(spec.silentSuccess, true, 'silentSuccess 鎖定為 true(partial-fail dialog 由 onResult own)');

    assert.equal(typeof spec.formBody, 'function', 'formBody 應為 builder function');
    assert.equal(typeof spec.rpc, 'function', 'rpc 應為 async function');
    assert.equal(typeof spec.onResult, 'function', 'onResult 應為 async function');

    // M5:MuscleRatio 三個新 optional 欄位全省略 — hideSubject=true 無 subject load,
    // 無 phase 下拉,subject change 不存在。
    assert.equal(spec.loadSubjects, undefined, 'MuscleRatio 省略 loadSubjects(hideSubject 無 subject)');
    assert.equal(spec.afterRender, undefined, 'MuscleRatio 省略 afterRender(無 phase 下拉)');
    assert.equal(spec.onSubjectChange, undefined, 'MuscleRatio 省略 onSubjectChange(無 subject)');
});

// ---------- formBody i18n 防回歸 ----------

test('MuscleRatio spec.formBody 為 builder function 且不洩漏 hardcoded 繁中', () => {
    // formBody 只有描述 help-text(走 translator);fake translator 回 ASCII key,
    // 任何繁中字元洩漏到 textContent 代表該位置仍 hardcoded。
    const fakeT = (key) => `__T:${key}__`;
    const html = muscleRatioFormBody(fakeT);

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

test('MuscleRatio spec.formBody 必呼叫 translator 至少 1 次(描述 help-text)', () => {
    // 1 = 當前 formBody 內 translator call 數的下界(panel.muscleratio.description)。
    // 抓「描述文字被改回 hardcoded」regression。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    muscleRatioFormBody(trackingT);

    assert.ok(
        calls.length >= 1,
        `formBody 至少應呼叫 translator 1 次,實際呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});

// ---------- spec.rpc pre-validation throw ----------

test('spec.rpc 在 manifestPath 缺時 throw(對齊 main.js:2530-2533)', async () => {
    const spec = makeMuscleRatioSpec({});
    const ctx = validCtx({ manifestPath: '' });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            // 對齊 main.js:2531 error.msg.muscle_fill_fields 語意。
            assert.match(err.message, /分期總檔案|數據資料夾/, 'throw 訊息含「請選擇分期總檔案與數據資料夾」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 在 dataFolder 缺時 throw', async () => {
    const spec = makeMuscleRatioSpec({});
    const ctx = validCtx({ dataFolder: '' });

    await assert.rejects(
        async () => { await spec.rpc(ctx); },
        (err) => {
            assert.match(err.message, /分期總檔案|數據資料夾/, 'throw 訊息含「請選擇分期總檔案與數據資料夾」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 不讀 subjectIdx — manifest/folder 齊但 subjectIdx=NaN 時仍呼到 AnalyzeMuscleRatio', async () => {
    // MuscleRatio 無 subject(hideSubject=true → _gatherCtx subjectIdx=NaN)。
    // 此 test 釘:rpc guard 不檢 subjectIdx,合法 manifest/folder 即過 guard 走到
    // AnalyzeMuscleRatio(對比 CCI/PhaseSync 會因 NaN subjectIdx throw)。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        let analyzeParams = null;
        window.go = {
            gui: {
                App: {
                    AnalyzeMuscleRatio: async (params) => {
                        analyzeParams = params;
                        return { success: true, message: '已處理 2 個主題', subjects: [] };
                    },
                },
            },
        };

        const spec = makeMuscleRatioSpec({});
        const result = await spec.rpc(validCtx()); // subjectIdx=NaN 但 manifest/folder 齊

        assert.ok(analyzeParams, 'subjectIdx=NaN 不應擋下 — rpc 應呼到 AnalyzeMuscleRatio');
        assert.equal(analyzeParams.manifestFile, '/tmp/manifest.csv', 'manifestFile 走 ctx.manifestPath');
        assert.equal(analyzeParams.dataFolder, '/tmp/data', 'dataFolder 走 ctx.dataFolder');
        assert.equal('subjectIndex' in analyzeParams, false, 'MuscleRatioParams 不含 subjectIndex');
        assert.equal(result.success, true, 'rpc 原樣回傳 result(不升 throw)');
    } finally {
        if (window?.happyDOM?.close) {
            await window.happyDOM.close();
        }
        delete globalThis.window;
    }
});

test('spec.rpc 在 backend result.success=false 時不升 throw(partial-fail 交給 onResult)', async () => {
    // 有別於 CCI:MuscleRatio 的 success=false 是「部分主題未完成」業務狀態,
    // 結果表格仍要 render,故 rpc 原樣回傳 result 讓 onResult 處理 + 自判 dialog。
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    try {
        const fakeResult = {
            success: false,
            message: '部分主題未完成',
            subjects: [{ subject: 'A', success: true }, { subject: 'B', success: false, error: 'x' }],
        };
        window.go = {
            gui: { App: { AnalyzeMuscleRatio: async (_p) => fakeResult } },
        };

        const spec = makeMuscleRatioSpec({});
        const result = await spec.rpc(validCtx());

        assert.equal(result, fakeResult, 'success=false 時 rpc 應原樣回傳 result(不升 throw)');
        assert.equal(result.success, false);
    } finally {
        if (window?.happyDOM?.close) {
            await window.happyDOM.close();
        }
        delete globalThis.window;
    }
});

// ---------- onResult status bar 修正(codex round-1 P2)----------
//
// envelope(manifestPanel.mjs _runEnvelope)在 onResult 前已設 status.analysis_done。
// MuscleRatio partial fail(success=false)backend 不 throw、結果表格仍 render,
// 若 onResult 不覆寫 status,status bar 會停在「分析完成」誤導使用者。下面兩 test
// 釘:partial fail → updateStatus(dialog.title.partial_failed);success → onResult
// 不以 partial key 覆寫(保留 envelope 的 analysis_done)。
// 對齊舊 executeMuscleRatioAnalysis(main.js:2540-2548)。

// onResult 需要 DOM(#mpResultContent / #mpResult)+ globalThis.t / ShowMessage /
// ShowError。fake t 回 ASCII key 讓我們能 assert 具體 i18n key 觸發。
function setupOnResultEnv() {
    const window = new Window({ url: 'http://localhost:34115/' });
    globalThis.window = window;
    globalThis.document = window.document;

    const root = window.document.createElement('div');
    root.id = 'functionPanel';
    // onResult 寫 #mpResultContent + toggle #mpResult display
    const result = window.document.createElement('div');
    result.id = 'mpResult';
    result.style.display = 'none';
    const content = window.document.createElement('div');
    content.id = 'mpResultContent';
    result.appendChild(content);
    root.appendChild(result);
    window.document.body.appendChild(root);

    globalThis.t = (key, ...args) => `__T:${key}__${args.length ? ':' + args.join(',') : ''}`;

    const calls = { showMessage: [], showError: [] };
    globalThis.ShowMessage = async (title, msg) => { calls.showMessage.push({ title, msg }); };
    globalThis.ShowError = async (title, msg) => { calls.showError.push({ title, msg }); };

    return { window, calls };
}

async function teardownOnResultEnv(window) {
    if (window?.happyDOM?.close) {
        await window.happyDOM.close();
    }
    delete globalThis.window;
    delete globalThis.document;
    delete globalThis.t;
    delete globalThis.ShowMessage;
    delete globalThis.ShowError;
}

test('onResult partial-fail(success=false)→ app.updateStatus(dialog.title.partial_failed) 修正 status bar', async () => {
    const { window, calls } = setupOnResultEnv();
    try {
        // app stub:記錄 updateStatus 呼叫(envelope 已設 analysis_done,onResult 須覆寫)
        const statusUpdates = [];
        const app = { updateStatus: (msg) => statusUpdates.push(msg) };
        const spec = makeMuscleRatioSpec(app);

        const partialResult = {
            success: false,
            message: '部分主題未完成',
            subjects: [
                { subject: 'A', success: true, outputAllPath: '/o/a1', outputPhasePath: '/o/a2', durationMs: 12 },
                { subject: 'B', success: false, error: '處理失敗', durationMs: 0 },
            ],
        };

        await spec.onResult(partialResult, {}, {});

        // status bar 必須被覆寫為 partial_failed(對齊舊 main.js:2546)
        const partialStatus = statusUpdates.find((s) => /dialog\.title\.partial_failed/.test(s));
        assert.ok(
            partialStatus,
            `partial fail 必呼 updateStatus(dialog.title.partial_failed),實際 status 呼叫:${JSON.stringify(statusUpdates)}`
        );
        // dialog 走 ShowError(部分失敗),非 ShowMessage(完成)
        assert.equal(calls.showError.length, 1, 'partial fail 應 ShowError 一次');
        assert.match(calls.showError[0].title, /dialog\.title\.partial_failed/, 'ShowError 標題為 partial_failed');
        assert.equal(calls.showMessage.length, 0, 'partial fail 不應 ShowMessage(完成)');
    } finally {
        await teardownOnResultEnv(window);
    }
});

test('onResult success=true → 不以 partial key 覆寫 status(保留 envelope analysis_done)', async () => {
    // success 路徑 onResult 不該動 status — envelope 已設的 status.analysis_done 正確。
    // 釘:updateStatus 不應被以 partial_failed key 呼叫(避免回歸成「成功也報部分失敗」)。
    const { window, calls } = setupOnResultEnv();
    try {
        const statusUpdates = [];
        const app = { updateStatus: (msg) => statusUpdates.push(msg) };
        const spec = makeMuscleRatioSpec(app);

        const successResult = {
            success: true,
            message: '已處理 2 個主題',
            subjects: [
                { subject: 'A', success: true, outputAllPath: '/o/a1', outputPhasePath: '/o/a2', durationMs: 12 },
                { subject: 'B', success: true, outputAllPath: '/o/b1', outputPhasePath: '/o/b2', durationMs: 34 },
            ],
        };

        await spec.onResult(successResult, {}, {});

        // success 不應以 partial_failed 覆寫 status
        const partialStatus = statusUpdates.find((s) => /dialog\.title\.partial_failed/.test(s));
        assert.equal(partialStatus, undefined, 'success 路徑不應以 partial_failed 覆寫 status');
        // dialog 走 ShowMessage(完成),非 ShowError
        assert.equal(calls.showMessage.length, 1, 'success 應 ShowMessage(完成)一次');
        assert.match(calls.showMessage[0].title, /dialog\.title\.complete/, 'ShowMessage 標題為 complete');
        assert.equal(calls.showError.length, 0, 'success 不應 ShowError');
    } finally {
        await teardownOnResultEnv(window);
    }
});
