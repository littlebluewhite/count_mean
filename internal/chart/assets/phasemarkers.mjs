/* phase-markers iframe-side canonical (ADR-0003).

   Loaded into chart iframe customJS via //go:embed by assets.go;
   helpers exposed at window.__phaseMarkers.{recalcPercents, findNearestLabel}
   for the inline customJS body to consume.

   IMPORTANT: algorithm body 必須與 frontend/src/charts/phaseMarkers.mjs 同步
   — sync test in frontend/src/phaseMarkers.test.mjs 強制比對。修改此檔請同步
   修改 frontend/src/charts/phaseMarkers.mjs。

   為何此檔是 .mjs:Vite/Node 跑 frontend test 時不會 import 此檔(iframe-side
   只被 Go embed 成字串、最終 inline 進 chart HTML script tag),`.mjs` 副檔名
   只是讓編輯器 syntax-highlight 為 JS module。 */
(function() {
function recalcPercents(checkedPhases) {
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
function findNearestLabel(targetTime, stringLabels) {
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
window.__phaseMarkers = { recalcPercents: recalcPercents, findNearestLabel: findNearestLabel };
})();
