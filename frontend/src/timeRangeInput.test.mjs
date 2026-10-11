// frontend/src/timeRangeInput.test.mjs
//
// 最大平均值開始/結束秒數欄位解析:留空 → null(未給),0 → 0(合法邊界),
// 非有限值 → null。
//
// 跑法:`node --test src/timeRangeInput.test.mjs`(於 frontend/)

import test from 'node:test';
import assert from 'node:assert/strict';

import { parseOptionalSeconds } from './timeRangeInput.mjs';

test('empty or whitespace → null (not given)', () => {
    assert.equal(parseOptionalSeconds(''), null);
    assert.equal(parseOptionalSeconds('   '), null);
    assert.equal(parseOptionalSeconds(undefined), null);
    assert.equal(parseOptionalSeconds(null), null);
});

test('zero is a real bound, not "not given"', () => {
    assert.equal(parseOptionalSeconds('0'), 0);
});

test('finite numbers pass through', () => {
    assert.equal(parseOptionalSeconds('2'), 2);
    assert.equal(parseOptionalSeconds('-1.5'), -1.5);
    assert.equal(parseOptionalSeconds(' 3.25 '), 3.25);
});

test('non-finite or unparseable → null', () => {
    assert.equal(parseOptionalSeconds('abc'), null);
    assert.equal(parseOptionalSeconds('Infinity'), null);
    assert.equal(parseOptionalSeconds('1e400'), null);
});
