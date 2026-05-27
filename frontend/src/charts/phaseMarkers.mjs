// frontend/src/charts/phaseMarkers.mjs
//
// phase-markers parent-side ES module (ADR-0003)。
//
// 與 internal/chart/assets/phasemarkers.mjs 是 sibling source — 兩檔 form
// factor 不同(parent 用 `export`,iframe 用 IIFE attach window.__phaseMarkers)
// 但兩個 function 的 algorithm 必須一致;sync test 在
// frontend/src/phaseMarkers.test.mjs 強制比對。修改此檔請同步修改 iframe 端。

export function recalcPercents(checkedPhases) {
    if (!Array.isArray(checkedPhases) || checkedPhases.length === 0) {
        return {};
    }
    if (checkedPhases.length === 1) {
        return { [checkedPhases[0].name]: 0 };
    }
    let minT = Infinity, maxT = -Infinity;
    for (const p of checkedPhases) {
        if (p.time < minT) minT = p.time;
        if (p.time > maxT) maxT = p.time;
    }
    const range = maxT - minT;
    const out = {};
    for (const p of checkedPhases) {
        out[p.name] = range > 0 ? ((p.time - minT) / range) * 100 : 0;
    }
    return out;
}

export function findNearestLabel(targetTime, stringLabels) {
    if (!Array.isArray(stringLabels) || stringLabels.length === 0) {
        return null;
    }
    let nearest = stringLabels[0];
    let nearestDist = Math.abs(parseFloat(stringLabels[0]) - targetTime);
    for (let i = 1; i < stringLabels.length; i++) {
        const d = Math.abs(parseFloat(stringLabels[i]) - targetTime);
        if (d < nearestDist) {
            nearest = stringLabels[i];
            nearestDist = d;
        }
    }
    return nearest;
}
