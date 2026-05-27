// frontend/src/phaseMarkers.test.mjs
//
// phase-markers pure-helper 單元測試 + iframe/parent 兩檔 logic sync test
// (ADR-0003)。
//
// 跑法:`node --test src/phaseMarkers.test.mjs`(於 frontend/)
// 也會被 `npm test`(glob `src/*.test.mjs`)抓到。

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

import { recalcPercents, findNearestLabel } from './charts/phaseMarkers.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PARENT_MJS = path.join(__dirname, 'charts', 'phaseMarkers.mjs');
const IFRAME_MJS = path.join(
    __dirname,
    '..',
    '..',
    'internal',
    'chart',
    'assets',
    'phasemarkers.mjs',
);

// ----- recalcPercents -----

test('recalcPercents: happy path — 多個 checked,線性百分比', () => {
    const out = recalcPercents([
        { name: 'P0', time: 0 },
        { name: 'P1', time: 5 },
        { name: 'P2', time: 10 },
    ]);
    assert.equal(out.P0, 0);
    assert.equal(out.P1, 50);
    assert.equal(out.P2, 100);
});

test('recalcPercents: 1 個 checked — 度數無意義,該 phase 回 0', () => {
    const out = recalcPercents([{ name: 'P1', time: 3 }]);
    assert.deepEqual(out, { P1: 0 });
});

test('recalcPercents: 0 個 checked — 回空 object 不 throw', () => {
    const out = recalcPercents([]);
    assert.deepEqual(out, {});
});

// ----- findNearestLabel -----

test('findNearestLabel: happy path — parseFloat 比對最近', () => {
    const labels = ['0.0', '0.5', '1.0', '1.5', '2.0'];
    assert.equal(findNearestLabel(0.7, labels), '0.5');
    assert.equal(findNearestLabel(1.4, labels), '1.5');
    assert.equal(findNearestLabel(2.5, labels), '2.0');
});

test('findNearestLabel: empty array → null', () => {
    assert.equal(findNearestLabel(1.0, []), null);
});

test('findNearestLabel: 等距時回第一個(順序穩定)', () => {
    const labels = ['0.0', '1.0', '2.0'];
    assert.equal(findNearestLabel(0.5, labels), '0.0');
});

// ----- sync test: parent ESM 與 iframe IIFE 兩檔 logic 一致 -----
//
// 為何要這個 test:phaseMarkers 必須同時可從 parent 端(ES module export)
// 與 iframe customJS(Go //go:embed inline)呼叫。兩檔 form factor 不同
// (parent 用 `export`,iframe 用 `(function(){...window.__phaseMarkers=...})()`),
// 但 algorithm body 必須完全一致 — 否則 parent 算出來的 pct 跟 iframe 算
// 出來的 nearest label 對不上,phase marker 會顯示偏移的時間 label。
//
// 比對策略:逐行 trim 後比 — indent 差異(IIFE 內可能不額外縮排)允許,
// 但 algorithm 任何字節 drift 都會 fail。
test('sync: parent phaseMarkers.mjs 與 iframe phasemarkers.mjs algorithm 一致', async () => {
    const parent = await readFile(PARENT_MJS, 'utf8');
    const iframe = await readFile(IFRAME_MJS, 'utf8');

    for (const fname of ['recalcPercents', 'findNearestLabel']) {
        const parentBody = extractFunctionBody(parent, fname);
        const iframeBody = extractFunctionBody(iframe, fname);
        assert.ok(parentBody, `parent 端找不到 function ${fname}`);
        assert.ok(iframeBody, `iframe 端找不到 function ${fname}`);
        assert.equal(
            normalize(parentBody),
            normalize(iframeBody),
            `function ${fname} body 在 parent 與 iframe 兩檔 algorithm drift — phase marker 計算結果會偏差`,
        );
    }
});

// 提取 `function <name>` 從第一個 `{` 到對應 `}` 之間的內容(含外層 `{}`)。
// 用 bracket counting 處理 nested block(if/for body)。
function extractFunctionBody(source, fname) {
    const re = new RegExp(`function\\s+${fname}\\s*\\([^)]*\\)\\s*\\{`);
    const m = re.exec(source);
    if (!m) return null;
    const openIdx = m.index + m[0].length - 1;
    let depth = 0;
    for (let i = openIdx; i < source.length; i++) {
        const ch = source[i];
        if (ch === '{') depth++;
        else if (ch === '}') {
            depth--;
            if (depth === 0) {
                return source.slice(openIdx, i + 1);
            }
        }
    }
    return null;
}

// normalize: 逐行 trim、空行 collapse、丟 windows CRLF
function normalize(body) {
    return body
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0)
        .join('\n');
}
