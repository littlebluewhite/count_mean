// frontend/src/phaseLines.test.mjs
//
// Slice D (#19) test:`charts/phaseLines.mjs#updatePhaseLines` 是 CCI panel
// 與 Chart Composer panel 共用的 phase-line 重繪 helper,要證明:
//   1. happy path:phaseTimes 給 P1=1.0 + S=2.5,iframe ECharts xAxis [0..5]
//      → markLine 出現在重算後的百分比座標(全勾時等價原始百分比)。
//   2. checkbox 未勾 → 對應 phase 不會出現在 markLine。
//   3. 同樣 helper 同樣 phaseTimes,CCI ctx (`#cciChartContent iframe` +
//      `[id^="phase_"]`) 與 Composer ctx (`#composerChartContent iframe` +
//      `[id^="composer_phase_"]`) 結果一致 → 證明 helper 是純函式(no
//      hidden CCI-only state),滿足 Slice D acceptance 第 11 條
//      「CCI 既有 manual smoke 行為 1:1 不變」的 unit-test 保證面向。
//   4. phaseTimes 內無對應 phase value(spec: P1 缺漏)→ silent skip
//      (不 throw、不 banner)。
//
// 為何在 happy-dom 內 stub 整個 echarts:helper 對 iframe.contentWindow.echarts
// 的依賴只是「拿 chart instance 然後讀 option / 寫 option」,不依賴 echarts
// 內部任何渲染行為。stub 一個 minimal chart object 即可覆蓋所有 setOption /
// getOption 路徑 — 比真 echarts 加 jsdom 來得快又穩。
//
// 跑法:`node --test src/phaseLines.test.mjs`(於 frontend/)。
// 也會被 `npm test`(glob `src/*.test.mjs`)抓到。

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';

import { updatePhaseLines } from './charts/phaseLines.mjs';

// ----------------------------------------------------------------------------
// Fixture builder:在 happy-dom 內建立 iframe + stub 一顆 echarts 圖表 instance。
//
// 兩個 ctx(CCI / Composer)只差 selector 名稱;為避免測試重複,把 fixture 提
// 取成 buildCtx(opts) — 接 selector + phaseTimes + xLabels 即可組出對應 DOM。
// ----------------------------------------------------------------------------

function makeChartStub({ xLabels, pctLabels, seriesNames }) {
    // 模擬最小 echarts.getInstanceByDom 回傳的 chart 物件。
    // option 結構對齊 composerPhaseMarkLineOpts / CCI 既有圖表的雙 xAxis 寫法:
    //   xAxis[0].data: 真實時間 label
    //   xAxis[1].data: 對應的百分比 label(0% / 25% / 50% / 75% / 100%)
    //   series[*].markLine.data: 由 helper 重新寫入
    const series = (seriesNames || ['mean']).map((name) => ({
        name,
        markLine: { data: [] },
    }));
    const option = {
        xAxis: [{ data: xLabels.slice() }, { data: pctLabels.slice() }],
        series,
        legend: [{ selected: {} }],
    };
    return {
        _option: option,
        _lastSetOption: null,
        getOption() {
            // helper 內部會 mutate xAxis[1].data 之類 — 回 deep copy 避免 cross-test 滲透
            return JSON.parse(JSON.stringify(option));
        },
        setOption(patch) {
            this._lastSetOption = patch;
            // 模擬 echarts.setOption 合併行為:對 series / xAxis 做最小 patch 寫回
            if (patch.series) {
                patch.series.forEach((s, i) => {
                    if (s && s.markLine) {
                        option.series[i].markLine = s.markLine;
                    }
                });
            }
            if (patch.xAxis) {
                patch.xAxis.forEach((x, i) => {
                    if (x && x.data) {
                        option.xAxis[i].data = x.data;
                    }
                });
            }
        },
    };
}

/**
 * 在 happy-dom 內建立 chart iframe + checkbox 容器。
 * @param {object} opts
 * @param {string} opts.iframeWrapId       e.g. 'cciChartContent' / 'composerChartContent'
 * @param {string} opts.checkboxIdPrefix   e.g. 'phase_' / 'composer_phase_'
 * @param {string[]} opts.phaseNames       要建的 checkbox phase 名
 * @param {string[]} opts.xLabels          xAxis[0] (時間) labels
 * @param {string[]} opts.pctLabels        xAxis[1] (百分比) labels
 */
function buildCtx({ iframeWrapId, checkboxIdPrefix, phaseNames, xLabels, pctLabels }) {
    const window = new Window();
    const doc = window.document;

    // 1. iframe wrap + iframe element
    const wrap = doc.createElement('div');
    wrap.id = iframeWrapId;
    doc.body.appendChild(wrap);

    const iframe = doc.createElement('iframe');
    wrap.appendChild(iframe);

    // 為了讓 helper 走過 contentWindow / contentDocument 路徑,直接在 iframe
    // 上 monkey-patch — 比起讓 happy-dom 真起一個 nested document 來得簡單。
    const innerDoc = window.document.implementation.createHTMLDocument('');
    const chartEl = innerDoc.createElement('div');
    chartEl.setAttribute('_echarts_instance_', '1');
    innerDoc.body.appendChild(chartEl);

    const chartStub = makeChartStub({ xLabels, pctLabels, seriesNames: ['mean'] });

    Object.defineProperty(iframe, 'contentDocument', {
        get() { return innerDoc; },
        configurable: true,
    });
    Object.defineProperty(iframe, 'contentWindow', {
        get() {
            return {
                echarts: {
                    getInstanceByDom: (el) => (el === chartEl ? chartStub : null),
                },
            };
        },
        configurable: true,
    });

    // 2. checkbox 容器
    phaseNames.forEach((name) => {
        const cb = doc.createElement('input');
        cb.type = 'checkbox';
        cb.id = checkboxIdPrefix + name;
        cb.value = name;
        cb.checked = true;
        doc.body.appendChild(cb);
    });

    return {
        window,
        doc,
        iframe,
        chartStub,
        getCheckbox: (name) => doc.querySelector('#' + checkboxIdPrefix + name),
    };
}

// ----------------------------------------------------------------------------
// 1. Happy path:phaseTimes 給 P1=1.0 + S=2.5(全勾,xAxis [0..5])
// ----------------------------------------------------------------------------

test('happy path: P1=1.0 + S=2.5 全勾 → markLine 出現在預期座標', () => {
    const ctx = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],
        xLabels: ['0.0', '1.0', '2.0', '2.5', '3.0', '4.0', '5.0'],
        pctLabels: ['0%', '17%', '33%', '42%', '50%', '67%', '100%'],
    });

    const phaseTimes = { P1: 1.0, S: 2.5 };
    const originalPctLabelsRef = { value: null };

    const applied = updatePhaseLines({
        iframeSelector: '#cciChartContent iframe',
        phaseTimes,
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef,
        document: ctx.doc,
    });
    assert.equal(applied, true, '應該成功 setOption(回 true)');

    // 驗證:setOption 被呼叫且 markLine.data 包含 P1 / S
    const last = ctx.chartStub._lastSetOption;
    assert.ok(last, 'chart.setOption 應被呼叫');
    assert.ok(last.series, 'setOption 應帶 series patch');
    // markLine attach 到第一條 series(targetIdx=0)
    const ml = last.series[0].markLine;
    assert.ok(ml && Array.isArray(ml.data), 'series[0] 應有 markLine.data');
    const names = ml.data.map((d) => d.name);
    // name 格式為 "P1\n(0.0%)"(2 個勾 → minTime=1, maxTime=2.5;P1=0%、S=100%)
    assert.ok(names.some((n) => n.startsWith('P1\n(')), 'markLine 應包含 P1: ' + names.join('|'));
    assert.ok(names.some((n) => n.startsWith('S\n(')), 'markLine 應包含 S: ' + names.join('|'));
    // P1 是最小 → 0.0%;S 是最大 → 100.0%
    assert.ok(names.includes('P1\n(0.0%)'), 'P1 應為 0.0%: ' + names.join('|'));
    assert.ok(names.includes('S\n(100.0%)'), 'S 應為 100.0%: ' + names.join('|'));

    // originalPctLabelsRef 應在首次成功 setOption 後 cache
    assert.ok(originalPctLabelsRef.value, 'originalPctLabelsRef 應 cache 原始 pct labels');
    assert.deepEqual(originalPctLabelsRef.value, ['0%', '17%', '33%', '42%', '50%', '67%', '100%']);
});

// ----------------------------------------------------------------------------
// 2. Checkbox 未勾 → 對應 phase 不會出現在 markLine
// ----------------------------------------------------------------------------

test('checkbox 未勾 → 該 phase 不會出現在 markLine', () => {
    const ctx = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],
        xLabels: ['0.0', '1.0', '2.5', '5.0'],
        pctLabels: ['0%', '20%', '50%', '100%'],
    });

    // 取消勾選 S
    ctx.getCheckbox('S').checked = false;

    const phaseTimes = { P1: 1.0, S: 2.5 };
    const originalPctLabelsRef = { value: null };

    updatePhaseLines({
        iframeSelector: '#cciChartContent iframe',
        phaseTimes,
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef,
        document: ctx.doc,
    });

    const last = ctx.chartStub._lastSetOption;
    const ml = last.series[0].markLine;
    const names = ml.data.map((d) => d.name);
    assert.ok(!names.some((n) => n.startsWith('S\n')), 'S 未勾不應出現: ' + names.join('|'));
    // P1 勾了 → 仍出現
    assert.ok(names.some((n) => n.startsWith('P1\n')), 'P1 應仍出現: ' + names.join('|'));
});

// ----------------------------------------------------------------------------
// 3. CCI ctx + Composer ctx 結果一致(helper 純函式驗證)
// ----------------------------------------------------------------------------

test('CCI ctx 與 Composer ctx 同 helper 同 phaseTimes → markLine 結果一致', () => {
    // 完全相同 phaseTimes / xLabels / pctLabels — 只差 selector + checkbox prefix
    const phaseTimes = { P1: 1.0, S: 2.5 };
    const xLabels = ['0.0', '1.0', '2.5', '5.0'];
    const pctLabels = ['0%', '20%', '50%', '100%'];

    const cci = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],
        xLabels,
        pctLabels,
    });
    const composer = buildCtx({
        iframeWrapId: 'composerChartContent',
        checkboxIdPrefix: 'composer_phase_',
        phaseNames: ['P1', 'S'],
        xLabels,
        pctLabels,
    });

    updatePhaseLines({
        iframeSelector: '#cciChartContent iframe',
        phaseTimes,
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef: { value: null },
        document: cci.doc,
    });
    updatePhaseLines({
        iframeSelector: '#composerChartContent iframe',
        phaseTimes,
        checkboxSelector: '[id^="composer_phase_"]',
        originalPctLabelsRef: { value: null },
        document: composer.doc,
    });

    const cciML = cci.chartStub._lastSetOption.series[0].markLine.data
        .map((d) => d.name).sort();
    const composerML = composer.chartStub._lastSetOption.series[0].markLine.data
        .map((d) => d.name).sort();

    assert.deepEqual(
        composerML, cciML,
        'CCI ctx 與 Composer ctx markLine name 應一致(helper 是純函式)\n' +
        '  CCI:      ' + cciML.join('|') + '\n' +
        '  Composer: ' + composerML.join('|')
    );

    // 額外驗 xAxis nearest 對齊
    const cciAxis = cci.chartStub._lastSetOption.xAxis[1].data;
    const composerAxis = composer.chartStub._lastSetOption.xAxis[1].data;
    assert.deepEqual(
        composerAxis, cciAxis,
        '兩 ctx 重算的 secondary xAxis pct labels 應一致'
    );
});

// ----------------------------------------------------------------------------
// 4. phaseTimes 內無對應 phase value → silent skip
// ----------------------------------------------------------------------------

test('phaseTimes 內無 P1 → silent skip(不 throw、不 banner)', () => {
    const ctx = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],  // checkbox 仍有 P1
        xLabels: ['0.0', '2.5', '5.0'],
        pctLabels: ['0%', '50%', '100%'],
    });

    // phaseTimes 內缺 P1
    const phaseTimes = { S: 2.5 };
    const originalPctLabelsRef = { value: null };

    let threw = false;
    try {
        updatePhaseLines({
            iframeSelector: '#cciChartContent iframe',
            phaseTimes,
            checkboxSelector: '[id^="phase_"]',
            originalPctLabelsRef,
            document: ctx.doc,
        });
    } catch (e) {
        threw = true;
    }
    assert.equal(threw, false, 'phaseTimes 缺漏 phase 不應 throw');

    const last = ctx.chartStub._lastSetOption;
    if (last && last.series && last.series[0].markLine) {
        const names = last.series[0].markLine.data.map((d) => d.name);
        assert.ok(!names.some((n) => n.startsWith('P1\n')),
            'P1 在 phaseTimes 缺漏時不應出現於 markLine: ' + names.join('|'));
    }
});

// ----------------------------------------------------------------------------
// 5. iframe 不存在 → silent no-op(回 false 而非 throw)
// ----------------------------------------------------------------------------

test('iframe 不存在 → silent no-op(回 false)', () => {
    const window = new Window();
    const doc = window.document;
    // 完全沒建 iframe wrap
    const applied = updatePhaseLines({
        iframeSelector: '#missingChartContent iframe',
        phaseTimes: { P1: 1 },
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef: { value: null },
        document: doc,
    });
    assert.equal(applied, false, '無 iframe 應 silent no-op 回 false');
});

// ----------------------------------------------------------------------------
// 6. onAfterApply callback 收到正確 recalcPercents map
// ----------------------------------------------------------------------------

test('onAfterApply callback 收到 recalcPercents map(CCI 用此更新位置顯示)', () => {
    const ctx = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],
        xLabels: ['0.0', '1.0', '2.5', '5.0'],
        pctLabels: ['0%', '20%', '50%', '100%'],
    });

    let captured = null;
    updatePhaseLines({
        iframeSelector: '#cciChartContent iframe',
        phaseTimes: { P1: 1.0, S: 2.5 },
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef: { value: null },
        document: ctx.doc,
        onAfterApply: (rp) => { captured = rp; },
    });

    assert.ok(captured, 'onAfterApply 應被觸發');
    // P1 是 minTime → 0%、S 是 maxTime → 100%
    assert.equal(captured.P1.toFixed(1), '0.0');
    assert.equal(captured.S.toFixed(1), '100.0');
});

// ----------------------------------------------------------------------------
// 7. axisMode='value' (Chart Composer 用) — xAxis[0].type=='value' 且無 data,
//    markLine.xAxis 必須為 numeric phaseTime 而非 nearest label。
//
// Slice D codex P1 finding:helper 從 CCI 1:1 抽出時隱含 category xAxis 假設,
// 走 `xLabels = option.xAxis[0].data || []` → Composer 走 value axis(無 data)
// 結果 xLabels=[] → nearestLabel=null → markLine 為空 → backend bake 的
// numeric phase markers 被 overwrite 沒了。
//
// 修法:helper 新增 axisMode 參數;'category' 維持 nearestLabel 路徑,'value'
// 直接用 phaseTime 數值。
// ----------------------------------------------------------------------------

function buildValueAxisCtx({ iframeWrapId, checkboxIdPrefix, phaseNames }) {
    // value-axis 版本的 chart stub:xAxis[0].type='value' 且無 data 屬性。
    // xAxis[1] 同樣保留(percent 軸仍是 category)— 對齊 composer.go
    // configureComposerAxes (bottom='value' / top='value' 但前端把 top 當
    // category 重算 pctLabels 顯示)。
    const window = new Window();
    const doc = window.document;

    const wrap = doc.createElement('div');
    wrap.id = iframeWrapId;
    doc.body.appendChild(wrap);
    const iframe = doc.createElement('iframe');
    wrap.appendChild(iframe);

    const innerDoc = window.document.implementation.createHTMLDocument('');
    const chartEl = innerDoc.createElement('div');
    chartEl.setAttribute('_echarts_instance_', '1');
    innerDoc.body.appendChild(chartEl);

    const option = {
        xAxis: [
            { type: 'value' },                    // bottom time axis — 無 data
            { data: ['0%', '50%', '100%'] },      // top percent axis(仍 category)
        ],
        series: [{ name: 'mean', markLine: { data: [] } }],
        legend: [{ selected: {} }],
    };
    const chartStub = {
        _option: option,
        _lastSetOption: null,
        getOption() { return JSON.parse(JSON.stringify(option)); },
        setOption(patch) {
            this._lastSetOption = patch;
            if (patch.series) {
                patch.series.forEach((s, i) => {
                    if (s && s.markLine) option.series[i].markLine = s.markLine;
                });
            }
            if (patch.xAxis) {
                patch.xAxis.forEach((x, i) => {
                    if (x && x.data) option.xAxis[i].data = x.data;
                });
            }
        },
    };

    Object.defineProperty(iframe, 'contentDocument', {
        get() { return innerDoc; }, configurable: true,
    });
    Object.defineProperty(iframe, 'contentWindow', {
        get() {
            return {
                echarts: {
                    getInstanceByDom: (el) => (el === chartEl ? chartStub : null),
                },
            };
        },
        configurable: true,
    });

    phaseNames.forEach((name) => {
        const cb = doc.createElement('input');
        cb.type = 'checkbox';
        cb.id = checkboxIdPrefix + name;
        cb.value = name;
        cb.checked = true;
        doc.body.appendChild(cb);
    });

    return { doc, chartStub };
}

test('axisMode="value": markLine.xAxis 為 numeric phaseTime(Composer value-axis 路徑)', () => {
    const ctx = buildValueAxisCtx({
        iframeWrapId: 'composerChartContent',
        checkboxIdPrefix: 'composer_phase_',
        phaseNames: ['P1', 'S'],
    });

    updatePhaseLines({
        iframeSelector: '#composerChartContent iframe',
        phaseTimes: { P1: 1.5, S: 3.25 },
        checkboxSelector: '[id^="composer_phase_"]',
        originalPctLabelsRef: { value: null },
        document: ctx.doc,
        axisMode: 'value',
    });

    const last = ctx.chartStub._lastSetOption;
    assert.ok(last, 'value-mode 應觸發 setOption');
    const ml = last.series[0].markLine;
    assert.ok(ml && Array.isArray(ml.data), 'series[0].markLine.data 應存在');
    assert.equal(ml.data.length, 2, '兩個勾選 phase → 兩個 markLine');

    // value mode 的 xAxis 必須為 numeric(秒數)而非 string label。
    const p1 = ml.data.find((d) => d.name.startsWith('P1\n'));
    const s = ml.data.find((d) => d.name.startsWith('S\n'));
    assert.ok(p1, 'markLine 應含 P1');
    assert.ok(s, 'markLine 應含 S');
    assert.equal(typeof p1.xAxis, 'number', 'P1.xAxis 應為 number(value-axis 模式),got ' + typeof p1.xAxis);
    assert.equal(p1.xAxis, 1.5, 'P1.xAxis 應等於 phaseTimes.P1 (1.5),got ' + p1.xAxis);
    assert.equal(typeof s.xAxis, 'number', 'S.xAxis 應為 number');
    assert.equal(s.xAxis, 3.25);
});

test('axisMode 預設="category"(向後相容,CCI 既有路徑釘住為 category)', () => {
    // 不傳 axisMode → 走 nearestLabel(category 路徑),驗 markLine.xAxis 為 string label。
    const ctx = buildCtx({
        iframeWrapId: 'cciChartContent',
        checkboxIdPrefix: 'phase_',
        phaseNames: ['P1', 'S'],
        xLabels: ['0.0', '1.0', '2.5', '5.0'],
        pctLabels: ['0%', '20%', '50%', '100%'],
    });

    updatePhaseLines({
        iframeSelector: '#cciChartContent iframe',
        phaseTimes: { P1: 1.0, S: 2.5 },
        checkboxSelector: '[id^="phase_"]',
        originalPctLabelsRef: { value: null },
        document: ctx.doc,
        // axisMode 省略 → 預設 'category'
    });

    const last = ctx.chartStub._lastSetOption;
    const ml = last.series[0].markLine;
    const p1 = ml.data.find((d) => d.name.startsWith('P1\n'));
    assert.ok(p1, 'category-mode 預設仍應產生 markLine');
    assert.equal(typeof p1.xAxis, 'string',
        'category-mode 下 markLine.xAxis 應為 string label(nearest 找到的 xLabel),got ' + typeof p1.xAxis);
});
