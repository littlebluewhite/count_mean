// frontend/src/charts/phaseLines.mjs
//
// updatePhaseLines — CCI + Chart Composer 共用的「分期點 markLine 動態更新」helper。
//
// 設計動機:
//   舊版 `updateCCIPhaseLines` 把「iframe selector / phaseTimes / checkbox selector
//   / originalPctLabels 快取」全部寫死在 CCI context;Chart Composer panel 需要
//   完全相同的行為(從 iframe 內 ECharts instance 抓 option、依勾選 phase 重算
//   兩排 X 軸百分比、重新 attach markLine、更新位置顯示),只是 DOM selector +
//   phaseTimes 來源不同。把這段抽出來,caller(CCI / Composer)只負責綁定
//   selector + 提供 phaseTimes 即可。
//
// Public contract:
//   updatePhaseLines({iframeSelector, phaseTimes, checkboxSelector,
//                     originalPctLabelsRef, document, findNearestLabel,
//                     onAfterApply, axisMode})
//
//   - iframeSelector       : CSS selector,DOM 內定位 chart iframe(e.g.
//                            '#cciChartContent iframe' / '#composerChartContent iframe')
//   - phaseTimes           : { [phaseName]: time_in_seconds } map(undefined 值 silent skip)
//   - checkboxSelector     : CSS selector,DOM 內定位 phase checkbox(e.g.
//                            '[id^="phase_"]' / '[id^="composer_phase_"]')
//                            checkbox.value 必須為 phase name(對齊 cb.value = p 寫法)
//   - originalPctLabelsRef : caller-owned 可變 cell `{value: null | string[]}` —
//                            首次成功讀到 secondary xAxis label slice 後 cache;
//                            未來「全勾」狀態可恢復原始百分比,避免重複計算。
//   - document             : 可選,預設 globalThis.document(測試注入 happy-dom doc)
//   - findNearestLabel     : 可選,預設內建 linear scan 的「最接近時間 label」實作;
//                            CCI/Composer 有相同 helper 的話可從 caller 注入避免分歧。
//   - onAfterApply         : 可選 callback,參數為 `recalcPercents` map(phase→%)。
//                            CCI 用此更新「分期點位置顯示」UI(`#cciPhasePositions`)。
//   - axisMode             : 可選 'category' | 'value' — 決定 markLine.xAxis 怎麼算。
//                            預設 'category' (CCI 既有路徑),走 findNearestLabel 對映
//                            到 xAxis[0].data 內最接近的 string label。
//                            'value' 走 numeric phaseTime 直接當 markLine.xAxis。
//
// 為何需要 axisMode 參數:
//   舊版 helper 從 CCI 1:1 抽取時,把「xAxis[0].data 是 category labels」當隱式假設。
//   CCI 的 xAxis 確實 type='category'(time label 是 string slot),但 Chart Composer
//   後端 (internal/chart/composer.go::configureComposerAxes) 用 Type:"value" 讓 EMG /
//   motion 不同採樣率對得齊 — 此時 xAxis[0] 沒有 data 欄,xLabels=[] → nearestLabel
//   永遠 return null → markData=[] → backend 已 bake 的 numeric phase markers 被
//   chart.setOption({series: ...markLine: {data: []}}) overwrite 為空。Composer
//   panel 整個 phase line 功能消失。
//
//   修法為「caller 顯式宣告 axisMode」而非「helper 用 if (option.xAxis[0].data)
//   推斷」— 後者把不一致 axis assumption 藏進 helper,將來 caller 換 axis type
//   不會收到任何 warning,只會 silent fail;前者讓 contract 留在 helper signature。
//
// Silent skip / 純函式契約:
//   - iframe 不存在 / 未載入 ECharts / 沒有 chart instance → return false(no-op)
//   - phaseTimes 內無對應 phase value → 跳過該 phase(不 throw、不 banner)
//   - checked phases < 2 → 不重算 secondary xAxis(fallback 到 original 或 0.0%)
//
// 不負責的事:
//   - 任何 phase reconcile UI banner(這由 caller 自行處理,helper 只負責繪圖)
//   - postMessage listener 安裝(由 main.js 在 init() 一次性安裝)
//   - iframe sandbox / srcdoc 注入(由 caller showXxxResult 處理)
//
// 為何不直接讓 helper 純 functional 不依賴 DOM:CCI 的 iframe.contentWindow.echarts
// 是 cross-frame side effect,本質上不可純函式化(除非 caller 把 chart instance
// 注入)。把 helper 設計成「caller 給 selector」最貼近真實 ECharts WebView 整合,
// 同時測試端用 happy-dom + 假 chart object 模擬就好。

/**
 * 預設 findNearestLabel:linear scan 找最接近 targetTime(秒)的 label。
 * 對應 main.js#_findNearestLabel — labels 是 string array(parseFloat 解析)。
 */
function defaultFindNearestLabel(targetTime, labels) {
    if (!labels || labels.length === 0) return null;
    let nearest = labels[0];
    let minDiff = Math.abs(parseFloat(labels[0]) - targetTime);
    for (const label of labels) {
        const diff = Math.abs(parseFloat(label) - targetTime);
        if (diff < minDiff) {
            minDiff = diff;
            nearest = label;
        }
    }
    return nearest;
}

/**
 * 從 iframe 取得 ECharts instance + option。
 * 任一步驟失敗回 null — caller 應 silent return 而非 throw。
 *
 * 抽成獨立 helper 是為了讓「iframe 不存在 / echarts 沒掛 / instance 沒綁」這
 * 條故障路徑單一,測試只要 stub 一處就能模擬。
 */
function resolveChartFromIframe(iframeSelector, doc) {
    const iframe = doc.querySelector(iframeSelector);
    if (!iframe || !iframe.contentWindow || !iframe.contentWindow.echarts) {
        return null;
    }
    const iframeDoc = iframe.contentDocument;
    if (!iframeDoc) return null;
    const chartEl = iframeDoc.querySelector('[_echarts_instance_]');
    if (!chartEl) return null;
    const chart = iframe.contentWindow.echarts.getInstanceByDom(chartEl);
    if (!chart) return null;
    return chart;
}

/**
 * 共用 phase line update。
 * 行為與舊 `updateCCIPhaseLines` 1:1 — caller 端切換 selector + phaseTimes 即可
 * 同時服務 CCI / Composer。
 *
 * @returns {boolean} true = 有實際 setOption;false = silent no-op(iframe / chart 缺失)
 */
export function updatePhaseLines({
    iframeSelector,
    phaseTimes,
    checkboxSelector,
    originalPctLabelsRef,
    document: docArg,
    findNearestLabel,
    onAfterApply,
    axisMode,
} = {}) {
    const doc = docArg || (typeof document !== 'undefined' ? document : null);
    if (!doc) return false;

    const chart = resolveChartFromIframe(iframeSelector, doc);
    if (!chart) return false;

    const mode = axisMode === 'value' ? 'value' : 'category';

    const option = chart.getOption();
    // 首次成功讀到 secondary xAxis label slice 後 cache 給 caller。
    // option.xAxis[1].data 為 echarts 「百分比輔助軸」的 label array,後續切換 phase
    // checkbox 想恢復「全勾原始百分比」時用得到。
    if (
        originalPctLabelsRef &&
        originalPctLabelsRef.value == null &&
        option.xAxis &&
        option.xAxis[1] &&
        option.xAxis[1].data
    ) {
        originalPctLabelsRef.value = option.xAxis[1].data.slice();
    }

    const xLabels = (option.xAxis && option.xAxis[0] && option.xAxis[0].data) || [];
    const times = phaseTimes || {};

    // 收集 checked phases(對齊 main.js#updateCCIPhaseLines line 1879-1884)。
    // cb.value === phase name(showCCIResult 在 createElement 時 cb.value = p)。
    // phaseTimes 內無對應 phase 值 → silent skip。
    const checkedPhases = [];
    const checkboxes = doc.querySelectorAll(checkboxSelector);
    checkboxes.forEach((cb) => {
        if (cb.checked && times[cb.value] !== undefined) {
            checkedPhases.push({ name: cb.value, time: times[cb.value] });
        }
    });

    // 重算百分比:min checked = 0%、max checked = 100%
    let minTime = Infinity;
    let maxTime = -Infinity;
    for (const p of checkedPhases) {
        if (p.time < minTime) minTime = p.time;
        if (p.time > maxTime) maxTime = p.time;
    }
    const duration = maxTime - minTime;

    // 重建 secondary xAxis 百分比 label。
    //   - 全勾 + 有 originalPctLabels cache → 直接還原(perf + avoid 浮點累積誤差)
    //   - >=2 個勾且 duration > 0 → 線性重算
    //   - 否則 fallback original cache 或 0.0%
    let newPctLabels;
    const allCheckboxes = doc.querySelectorAll(checkboxSelector);
    const allChecked = checkedPhases.length === allCheckboxes.length;
    const cached = originalPctLabelsRef && originalPctLabelsRef.value;
    if (allChecked && cached) {
        newPctLabels = cached;
    } else if (checkedPhases.length >= 2 && duration > 0 && isFinite(duration)) {
        newPctLabels = xLabels.map((label) => {
            const tVal = parseFloat(label);
            return ((tVal - minTime) / duration * 100).toFixed(1) + '%';
        });
    } else if (cached) {
        newPctLabels = cached;
    } else {
        newPctLabels = xLabels.map(() => '0.0%');
    }

    const recalcPercents = {};
    for (const p of checkedPhases) {
        recalcPercents[p.name] = duration > 0 ? ((p.time - minTime) / duration * 100) : 0;
    }

    // 建 markLine data — phase name 在 label 換行加百分比 `\n(xx.x%)`
    //
    // axisMode='category' (CCI 既有路徑):xAxis[0].type='category',markLine.xAxis
    // 必須是 xLabels 內某個 string;走 findNearestLabel 對映 phaseTime 到最接近
    // 的 string label(echarts 不接受 category axis 上的 numeric markLine value)。
    //
    // axisMode='value' (Composer 走 Type:"value" 對齊跨採樣率):xAxis[0].data 不
    // 存在(value axis 從 series data 推 min/max),markLine.xAxis 直接用 numeric
    // phaseTime — composerPhaseMarkLineOpts 在 backend 也是這樣 bake 進 HTML 的。
    const nearest = findNearestLabel || defaultFindNearestLabel;
    const markData = [];
    for (const p of checkedPhases) {
        const pct = recalcPercents[p.name].toFixed(1);
        let xVal;
        if (mode === 'value') {
            // numeric — backend composerPhaseMarkLineOpts.XAxis 也是 p.sec 直接塞
            xVal = p.time;
        } else {
            // category — 走 nearestLabel
            const nearestLabel = nearest(p.time, xLabels);
            if (!nearestLabel) continue;  // 找不到 → silent skip(對齊舊行為)
            xVal = nearestLabel;
        }
        markData.push({
            name: p.name + '\n(' + pct + '%)',
            xAxis: xVal,
            lineStyle: { type: 'dashed', color: '#888', width: 1 },
            label: { show: true, formatter: '{b}', position: 'end' },
        });
    }

    // markLine attach 到「第一條可見 mean curve series」(跳過 ±SD pair、跳過
    // legend 取消勾選的)。對齊舊 CCI 行為。
    if (option.series && option.series.length > 0) {
        const legendSelected = (option.legend && option.legend[0] && option.legend[0].selected) || {};
        let targetIdx = 0;
        for (let i = 0; i < option.series.length; i++) {
            const name = option.series[i].name || '';
            if (name.includes(' -SD') || name.includes(' +SD')) continue;
            if (legendSelected[name] !== false) {
                targetIdx = i;
                break;
            }
        }

        const seriesUpdate = option.series.map((s, i) => {
            if (i === targetIdx) {
                return {
                    markLine: {
                        silent: true,
                        symbol: ['none', 'none'],
                        data: markData,
                    },
                };
            }
            if (s.markLine && s.markLine.data && s.markLine.data.length > 0) {
                return { markLine: { data: [] } };
            }
            return {};
        });

        // axisMode='value' 下 secondary xAxis 是 type='value' 且 Min:0/Max:100,
        // 不該被 data:[] 覆蓋。只在 category mode 才 patch 第二軸 pct labels。
        if (mode === 'value') {
            chart.setOption({ series: seriesUpdate });
        } else {
            chart.setOption({ series: seriesUpdate, xAxis: [{}, { data: newPctLabels }] });
        }
    }

    if (typeof onAfterApply === 'function') {
        onAfterApply(recalcPercents);
    }

    return true;
}
