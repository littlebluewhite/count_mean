// Regression test for P0-2:
//   frontend/src/main.js#loadChartColumns 之前直接把 CSV header 字串
//   拼進 innerHTML,當 header 包含 `<img src=x onerror=...>` 之類 payload
//   時會在 Wails WebView 內被當 HTML 解析(後端 GetCSVHeaders 沒 escape)。
//
// 本檔同時做兩件事:
//   1. 行為測試(happy-dom):重現 loadChartColumns 的渲染邏輯,把惡意
//      header 餵進去,驗證 (a) 不會觸發 attached handler、(b) <label>
//      文字節點裡原樣保留完整字串(textContent === payload)。
//   2. 原始碼靜態檢查:確保 main.js 內 loadChartColumns 沒回退到舊的
//      `${col}` 直插 innerHTML 寫法,且使用 createElement + textContent。
//
// 跑法:`node --test src/loadChartColumns.test.mjs`(於 frontend/ 下)。
// 需要 devDependency `happy-dom`(package.json 已加)。

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { Window } from 'happy-dom';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const MAIN_JS = path.join(__dirname, 'main.js');

// 將 main.js 的 loadChartColumns 渲染邏輯(已修版本)抽到測試裡跑,
// 必須與 production 程式碼結構保持一致 — 任何偏移都會在下面的
// 靜態 source guard 被擋住。
function renderColumnSelector(container, headers, doc = globalThis.document) {
    // 對齊 main.js#loadChartColumns:createElement + textContent
    headers.forEach((col, index) => {
        const item = doc.createElement('div');
        item.className = 'checkbox-item';

        const cb = doc.createElement('input');
        cb.type = 'checkbox';
        cb.id = `col_${index}`;
        cb.value = String(index);
        cb.checked = true;
        if (index === 0) {
            cb.disabled = true;
        }

        const label = doc.createElement('label');
        label.setAttribute('for', `col_${index}`);
        label.textContent = col;

        item.appendChild(cb);
        item.appendChild(label);
        container.appendChild(item);
    });
}

test('CSV header 含 <img onerror> payload 時不會觸發 XSS', () => {
    const window = new Window();
    const document = window.document;
    // happy-dom 會在 <img> 載入失敗時同步呼叫 onerror;
    // 用 window 上的 flag 偵測有沒有真的執行
    window._xssFired = false;

    const selector = document.createElement('div');
    selector.id = 'columnSelector';
    document.body.appendChild(selector);

    const headers = [
        'time',
        '<img src=x onerror="window._xssFired=true">',
        'EMG_2',
    ];

    renderColumnSelector(selector, headers, document);

    // 1. 沒有 <img> 子節點被建立(代表沒被當 HTML 解析)
    const imgs = selector.querySelectorAll('img');
    assert.equal(imgs.length, 0, '惡意 header 不可被解析為 <img>');

    // 2. window._xssFired 維持 false
    assert.equal(window._xssFired, false, 'onerror handler 不可被執行');

    // 3. <label> 的 textContent 完整保留原始 payload(含 < > 等字元)
    const labels = selector.querySelectorAll('label');
    assert.equal(labels.length, 3);
    assert.equal(labels[0].textContent, 'time');
    assert.equal(labels[1].textContent, '<img src=x onerror="window._xssFired=true">');
    assert.equal(labels[2].textContent, 'EMG_2');

    // 4. checkbox 該 disabled 的只有 index === 0
    const inputs = selector.querySelectorAll('input[type="checkbox"]');
    assert.equal(inputs.length, 3);
    assert.equal(inputs[0].disabled, true, 'time 欄(index 0)應 disabled');
    assert.equal(inputs[1].disabled, false);
    assert.equal(inputs[2].disabled, false);
    assert.equal(inputs[0].checked, true);
    assert.equal(inputs[1].checked, true);
});

test('main.js loadChartColumns 原始碼未回退到不安全的 innerHTML 寫法', async () => {
    const src = await readFile(MAIN_JS, 'utf8');

    // 定位 loadChartColumns 函式 body(到下一個 method 為止)
    const startMarker = 'async loadChartColumns(';
    const startIdx = src.indexOf(startMarker);
    assert.ok(startIdx >= 0, '找不到 loadChartColumns 函式');

    // loadChartColumns 之後接 previewInteractiveChart,以此為下界
    const endMarker = 'async previewInteractiveChart(';
    const endIdx = src.indexOf(endMarker, startIdx);
    assert.ok(endIdx > startIdx, '找不到 previewInteractiveChart 邊界');

    const body = src.slice(startIdx, endIdx);

    // 不可再用 ${col} 樣式把 raw header 拼進 innerHTML
    // (loading / error 訊息走 tHtml() 仍允許,所以我們只擋 ${col} 直插。)
    assert.ok(
        !/innerHTML\s*=\s*headers\.map/.test(body),
        '不可再用 headers.map(...) 拼 innerHTML(P0-2 已修)'
    );
    assert.ok(
        !/\$\{col\}/.test(body),
        '不可再把 ${col} 直接插進模板字串(P0-2 已修)'
    );

    // 必須使用 createElement + textContent 模式
    assert.match(
        body,
        /createElement\(\s*['"]label['"]\s*\)/,
        '應使用 document.createElement("label")'
    );
    assert.match(
        body,
        /\.textContent\s*=\s*col\b/,
        '應使用 label.textContent = col'
    );
});
