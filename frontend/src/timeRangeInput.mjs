// frontend/src/timeRangeInput.mjs
//
// 最大平均值 panel 的開始/結束秒數欄位解析。留空 = 未給 → null(後端 *float64 nil,
// 由 calculator 從資料首/末樣本補,ADR-0050);填 0 就是 0,不再與留空混為一談。
// 非有限值(NaN / ±Infinity)也回 null:JSON 本來就會把它們序列化成 null。

export function parseOptionalSeconds(raw) {
    if (raw == null || String(raw).trim() === '') {
        return null;
    }

    const v = Number(raw);
    return Number.isFinite(v) ? v : null;
}
