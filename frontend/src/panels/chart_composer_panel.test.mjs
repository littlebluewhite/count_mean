// Invariant test:Chart Composer panel 內所有 user-visible text 必須來自 i18n
// dictionary;不可有 hardcoded 繁中字串(Slice E 漏 sweep 之 form labels / buttons
// 之回歸保護)。
//
// 策略:注入 fake translator(回 ASCII key),render 後 textContent 若仍含
// [一-龥] 繁中字元範圍,代表該位置仍是 hardcoded 字串 — 因為任何經
// 過 translator 的字串在 fake 模式下都會變 ASCII key。
//
// 跑法:`node --test src/panels/chart_composer_panel.test.mjs`(於 frontend/ 下)。
// 注意 package.json 的 test script `node --test src/*.test.mjs` 是 glob,
// 預設不會 recursive 進 src/panels/ — 需要另外手動跑或調整 script。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { buildChartComposerPanelHtml } from './chart_composer_panel.mjs';

test('Chart Composer panel 內 user-visible text 均來自 translator(無 hardcoded 繁中)', () => {
    // fake translator 回 ASCII key;production translator (tHtml) 走 i18n
    // dictionary。Parse 後若仍出現繁中字元,代表 panel template 內仍有未經
    // translator 的 hardcoded 字串。
    const fakeT = (key) => `__T:${key}__`;

    const html = buildChartComposerPanelHtml(fakeT);

    // 用 DOMParser 而非 element.innerHTML — 避開 XSS hook 的 generic
    // warning(parser 是 read-only parse,不寫進 live DOM)。
    const window = new Window();
    const parser = new window.DOMParser();
    const doc = parser.parseFromString(`<!doctype html><body>${html}</body>`, 'text/html');
    const visibleText = doc.body.textContent;

    const cjkRange = /[一-龥]/g;
    const chineseChars = visibleText.match(cjkRange) || [];

    assert.equal(
        chineseChars.length,
        0,
        `panel 內仍有 hardcoded 繁中字元(共 ${chineseChars.length} 個):` +
        `${[...new Set(chineseChars)].join('')}\n` +
        `任何未經 translator 包裹的字串都會洩漏到 textContent。` +
        `請把對應位置改為 \${translator('xxx.key')} 並補 i18n catalog。`
    );
});

test('Chart Composer panel 必呼叫 translator 至少 9 次(每個 hardcoded label/button 一次)', () => {
    // Sanity check:9 處新 sweep + 已存在的 manifest/data_folder/subject/back 等 ≥ 9 次。
    // 防呆:確保沒人把 translator() call 全砍光留 hardcoded 字串卻通過 test。
    const calls = [];
    const trackingT = (key) => {
        calls.push(key);
        return `__T:${key}__`;
    };

    buildChartComposerPanelHtml(trackingT);

    assert.ok(
        calls.length >= 9,
        `panel 至少應呼叫 translator 9 次,實際只呼叫 ${calls.length} 次:${calls.join(', ')}`
    );
});
