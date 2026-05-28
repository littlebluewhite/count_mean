// Invariant + behavioural tests for chart_composer_spec.mjs(ADR-0007 §6 / M2)。
//
// 跑法:`node --test src/panels/chart_composer_spec.test.mjs`(於 frontend/ 下)。
// `npm test` 透過 `src/panels/*.test.mjs` glob 抓到。
//
// 測試覆蓋:
//   1. spec 必填欄位:titleKey / statusRunningKey / runBtnLabelKey /
//      silentSuccess / formBody / rpc / onResult
//   2. formBody 內 user-visible text 均來自 translator(無繁中字元洩漏)
//   3. formBody 呼叫 translator ≥ 6 次(每個 hardcoded label/button 一次,sanity bound)
//   4. spec.rpc 在 _composerSelectedChannels 空時 throw
//   5. spec.rpc 在 _composerLoadedSubject 與 ctx.subjectName 不一致時 throw
//
// Mock 策略:
//   * happy-dom 提供 DOM(formBody 內 textContent 抽繁中字元)
//   * fake translator 回 ASCII key — 任何未經 translator 的字串會以繁中字元洩漏到
//     textContent,被 cjkRange regex 抓到
//   * makeChartComposerSpec(app) factory 接 stub app — rpc test 只測 throw 條件,
//     不真正觸發 GenerateChartComposer(後者透過 import 拉到 wailsjs,在 happy-dom
//     下 globalThis.go 缺失會 throw,但 throw 在 pre-validation 前已先觸發,本 test
//     不會走到 GenerateChartComposer 那段)
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

test('Chart Composer spec.formBody 必呼叫 translator 至少 5 次(各 label/button 一次)', () => {
    // 5 = 當前 formBody 內 translator call 數的下界;新增 label 時連同此數 bump。
    //
    // 對應 5 個 translator call site:load_emg_channels / form.label.composer_channels /
    // form.helptext.load_channels_first / form.label.composer_phases /
    // form.helptext.composer_phases_pending。
    //
    // 此 lower bound 抓「人為把翻譯整段砍光」regression。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };
    composerFormBody(trackingT);

    assert.ok(
        calls.length >= 5,
        `formBody 至少應呼叫 translator 5 次,實際只呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});

// ---------- spec.rpc pre-validation throw ----------

test('spec.rpc 在 _composerSelectedChannels 為空時 throw(對齊 main.js:1875-1878)', async () => {
    // 構造 stub app:_composerSelectedChannels 為空 Set
    const stubApp = {
        _composerSelectedChannels: new Set(),
        _composerLoadedSubject: 'Rudolph',
        _composerEMGMotionOffset: 0,
    };
    const spec = makeChartComposerSpec(stubApp);

    const ctx = {
        manifestPath: '/tmp/manifest.csv',
        dataFolder: '/tmp/data',
        subjectIdx: NaN,
        subjectName: 'Rudolph',
        subjects: [],
    };

    await assert.rejects(
        async () => {
            await spec.rpc(ctx);
        },
        (err) => {
            // 對齊 main.js:1876 既有錯誤文案 — i18n key 在 M5 wiring 補。
            assert.match(err.message, /至少選擇一個 EMG 通道|EMG 通道/, 'throw 訊息含「至少選擇一個 EMG 通道」語意');
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});

test('spec.rpc 在 _composerLoadedSubject 與 ctx.subjectName 不一致時 throw(codex P2#3 guard)', async () => {
    // 構造 stub:有勾選 channel,但 loadedSubject ≠ ctx.subjectName。
    // 對齊 main.js:1883-1889 — 防上個 subject 的 emgMotionOffset silent 套到新 subject。
    const stubApp = {
        _composerSelectedChannels: new Set(['ch1', 'ch2']),
        _composerLoadedSubject: 'SubjectA',
        _composerEMGMotionOffset: 600,
    };
    const spec = makeChartComposerSpec(stubApp);

    const ctx = {
        manifestPath: '/tmp/manifest.csv',
        dataFolder: '/tmp/data',
        subjectIdx: NaN,
        subjectName: 'SubjectB',   // 與 loadedSubject 不一致
        subjects: [],
    };

    await assert.rejects(
        async () => {
            await spec.rpc(ctx);
        },
        (err) => {
            // 對齊 main.js:1884-1887 既有錯誤文案。
            assert.match(
                err.message,
                /主題已變更|EMG 欄位/,
                'throw 訊息含「主題已變更但未重新載入 EMG 欄位」語意'
            );
            return true;
        },
        'rpc 應 throw 而非 return falsy'
    );
});
