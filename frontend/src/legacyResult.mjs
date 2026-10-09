// frontend/src/legacyResult.mjs
//
// legacy panel(最大平均值 / 標準化 / 階段分析)的 result 分流。
// 後端以 result.success 表達成敗(例如批次全部失敗):false 時顯示失敗狀態並
// showError(result.message),不得走成功路徑。依賴皆注入,方便不載入 main.js 測試。

export async function handleLegacyResult(result, { failedStatus, setStatus, showError, onSuccess }) {
    if (!result.success) {
        setStatus(failedStatus);
        await showError(result.message);
        return;
    }

    await onSuccess(result);
}
