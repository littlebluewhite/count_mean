// Regression tests for codex review P2 — translation values must be escaped
// before insertion into innerHTML, because translationsDir is user-configurable
// (i.e. translation strings are effectively external input).
//
// 以 Node 內建 test runner 跑:`node --test src/i18n.test.mjs`(於 frontend/ 下)
// 或 `npm test`(package.json 已加 script)。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { escapeHtml, tHtml } from './i18n.js';

test('escapeHtml 將 HTML 特殊字元轉為實體', () => {
    assert.equal(escapeHtml('<script>alert(1)</script>'), '&lt;script&gt;alert(1)&lt;/script&gt;');
    assert.equal(escapeHtml('a & b'), 'a &amp; b');
    assert.equal(escapeHtml('"quoted"'), '&quot;quoted&quot;');
    assert.equal(escapeHtml("it's"), 'it&#39;s');
    assert.equal(escapeHtml('plain'), 'plain');
});

test('escapeHtml 對 null / undefined 回空字串', () => {
    assert.equal(escapeHtml(null), '');
    assert.equal(escapeHtml(undefined), '');
});

test('escapeHtml 處理非字串輸入(以 String 強轉)', () => {
    assert.equal(escapeHtml(42), '42');
    assert.equal(escapeHtml(true), 'true');
});

test('tHtml 對任意輸入產生不含原始 < > " 的安全字串', () => {
    // 模擬 translationsDir 提供惡意值:t() 查不到 key 時 fallback 為 key 本身,
    // tHtml 仍會 escape — 這代表對任意外部翻譯值都通用
    const xssPayload = '<img src=x onerror=alert(1)>';
    const result = tHtml(xssPayload);
    assert.equal(result, '&lt;img src=x onerror=alert(1)&gt;');
    assert.ok(!result.includes('<'), 'tHtml 輸出不可含原始 <');
    assert.ok(!result.includes('>'), 'tHtml 輸出不可含原始 >');
});

test('tHtml 與 t 行為等價(只多了 escape)— 純 ASCII key 不被 escape 改變', () => {
    // ASCII key 經過 t() fallback,escapeHtml 不會動到任何字元
    assert.equal(tHtml('app.title'), 'app.title');
});
