// frontend/src/legacyResult.test.mjs
//
// legacy panel(最大平均值 / 標準化 / 階段分析)result 分流測試:
// result.success=false 必須走失敗路徑(failure 狀態 + showError(message)),
// 不得跳成功對話框;success=true 只走成功路徑。
//
// 跑法:`node --test src/legacyResult.test.mjs`(於 frontend/)

import test from 'node:test';
import assert from 'node:assert/strict';

import { handleLegacyResult } from './legacyResult.mjs';

function makeSpies() {
    const calls = [];
    return {
        calls,
        setStatus: (s) => calls.push(['status', s]),
        showError: async (m) => { calls.push(['error', m]); },
        onSuccess: async () => { calls.push(['success']); },
    };
}

test('success=false: failure status + showError(message), no success path', async () => {
    const s = makeSpies();
    await handleLegacyResult({ success: false, message: '成功 0 個檔案，失敗 2 個檔案' }, {
        failedStatus: 'FAILED', ...s,
    });
    assert.deepEqual(s.calls, [
        ['status', 'FAILED'],
        ['error', '成功 0 個檔案，失敗 2 個檔案'],
    ]);
});

test('success=true: success path only', async () => {
    const s = makeSpies();
    await handleLegacyResult({ success: true, message: 'ok' }, { failedStatus: 'FAILED', ...s });
    assert.deepEqual(s.calls, [['success']]);
});
