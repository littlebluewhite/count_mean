// frontend/src/panels/normalized_phase_sync_spec.mjs
//
// 標準化分期同步分析 panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M4 交付)。NormalizedPhaseSync 是三個「CSV-success / 無 iframe」
// panel 中表單最複雜的一個:
//   * subject 用 subjectIdx(number)— hideSubject 省略走 default(顯示 subject select)。
//   * silentSuccess: true — 成功訊息是雙輸出路徑
//     `t('success.msg.normalized_analysis_done', normalizedEMGPath, phaseSyncCSVPath)`
//    (非 result.message),且失敗走 result.success=false 業務分支。envelope 標準
//     ShowMessage(result.message)形狀不符,故 onResult 自己 own dialog
//    (對齊 main.js:2319-2327)。
//   * formBody — 4 個 phase 下拉(normStart / normEnd / statsStart / statsEnd),
//     panel-specific ids + `data-touched="false"` 屬性。**下拉 option 的 POPULATION
//    (loadNormalizedPhaseSyncPhases,main.js:2241-2269)與 mirror / markTouched 互動
//     邏輯(_bindNormalizedPhaseSyncMirror,main.js:2276-2293)是 M5 wiring,不在本
//     spec** — formBody 只 render 空殼結構 + data-touched 初值。
//
// **為何 silentSuccess=true + onResult own dialog(有別於 CCI 的 rpc-throw 模式)**:
//   NormalizedPhaseSync 後端錯誤通道(gui/normalized_phase_sync_handlers.go:70-75):
//   所有可預期失敗(參數驗證、路徑、load、phase range、normalize、stats、寫檔)都包成
//   result.Success=false + result.Message;Go err 只在 panic 時 non-nil。故 backend 回的
//   result 一律交給 onResult 判 result.success → 業務 dialog(success 帶雙路徑 / fail
//   帶 result.message)。rpc 只在「缺必填輸入」(exception)時 throw。
//
//   注意:rpc **不**把 result.success=false 升 throw(對比 CCI cci_spec.mjs:139)— 同
//   PhaseSync 理由:onResult 已 own dialog,結果區要照常 render。三個 M4 panel 統一此模式。
//
// **後端 param / result 欄位名(已對 Go struct 驗證,勿臆測)**:
//   NormalizedPhaseSyncParams { manifestFile, dataFolder, subjectIndex,
//     normStartPhase, normEndPhase, statsStartPhase, statsEndPhase }
//                                       ← gui/normalized_phase_sync_handlers.go:24
//     (4 個 phase 為 models.PhasePoint,wire 上 string)
//   NormalizedPhaseSyncResult { normalizedEMGPath, phaseSyncCSVPath, subject,
//     normStartPhase, normEndPhase, normStartTime, normEndTime,
//     statsStartPhase, statsEndPhase, statsStartTime, statsEndTime,
//     channelNames, channelMaxes, channelMeans, report, success, message }
//                                       ← gui/normalized_phase_sync_handlers.go:38
//
// invariant(由 normalized_phase_sync_spec.test.mjs 釘):
//   * silentSuccess === true、hideSubject 省略(undefined)
//   * spec.formBody 為 builder function 且不洩漏 hardcoded 繁中(4 個 phase 下拉)
//   * rpc 在 manifestPath / dataFolder 缺、subjectIdx NaN/<0、任一 phase 缺時 throw

import { AnalyzeNormalizedPhaseSync } from '../../wailsjs/go/gui/App.js';

/**
 * 標準化分期同步 panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * 原 showNormalizedPhaseSyncPanel(main.js:2123-2186)在 shell 提供的 manifest /
 * dataFolder / subject 三段之後,有第 4 點(標準化視窗 normStart/normEnd)與第 5 點
 *(統計視窗 statsStart/statsEnd)共 4 個 phase 下拉(兩組 form-row 並排)。這 4 個下拉
 * 是 NormalizedPhaseSync 獨有,保留在 formBody;其餘三段由 shell 提供。
 *
 * `data-touched="false"`:第 5 點 select 的 mirror 邏輯(第 4 點 change → 第 5 點同步
 * 直到使用者觸碰)由 M5 wiring 的 _bindNormalizedPhaseSyncMirror 讀此屬性(main.js:
 * 2276-2293)。formBody 保留初值 "false",M5 binding 才掛 change listener。下拉 option
 * 內容由 M5 loadNormalizedPhaseSyncPhases populate(main.js:2241-2269);formBody 只
 * render 空殼 + placeholder option,避免在 spec 內耦合 phase 載入 / mirror 邏輯。
 *
 * panel-specific ids(沿用原 main.js id,M5 wiring 仍可用同 id 找到):
 *   normalizedPhaseSyncNormStartPhase / NormEndPhase / StatsStartPhase / StatsEndPhase
 *
 * @param {(key: string) => string} t - translator(production: tHtml,test: fake)
 * @returns {string} formBody HTML(4 個 phase 下拉空殼)
 */
export function normalizedPhaseSyncFormBody(t) {
    return `
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>4. ${t('form.label.norm_start_phase')}</label>
                    <select id="normalizedPhaseSyncNormStartPhase" class="form-control" data-touched="false">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${t('form.label.norm_end_phase')}</label>
                    <select id="normalizedPhaseSyncNormEndPhase" class="form-control" data-touched="false">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
            </div>
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>5. ${t('form.label.stats_start_phase')}</label>
                    <select id="normalizedPhaseSyncStatsStartPhase" class="form-control" data-touched="false">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${t('form.label.stats_end_phase')}</label>
                    <select id="normalizedPhaseSyncStatsEndPhase" class="form-control" data-touched="false">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
            </div>
        `;
}

/**
 * 標準化分期同步 spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(對齊 cci / composer spec)。onResult 走 mp.openOutputFolder()
 *(不直接讀 app);`app` 目前未被 closure 直接使用 — 仍保留 factory 簽名以對齊其他
 * 4 panel 一致性。
 *
 * @param {object} _app - EMGAnalysisApp 實例(目前未用,保留簽名一致性)
 * @returns {object} spec for ManifestPanel.run(spec)
 */
export function makeNormalizedPhaseSyncSpec(_app) {
    return {
        titleKey: 'panel.normalizedphasesync.title',
        statusRunningKey: 'status.normalized_running',
        // hideSubject 省略 → 用 subject(subjectIdx)。
        // silentSuccess: 成功訊息帶雙輸出路徑(非 result.message),success=false 走業務
        //   ShowError;dialog 由 onResult own(對齊 main.js:2319-2327)。
        silentSuccess: true,

        formBody: normalizedPhaseSyncFormBody,

        /**
         * 呼 AnalyzeNormalizedPhaseSync RPC。
         *
         * 從 DOM 讀 4 個 phase(formBody render 的下拉),subjectIdx 來自 ctx。所有必填齊
         * 才呼 RPC,缺則 throw(exception → envelope ShowError;對齊 main.js:2305-2310
         * 既有 fill_required_fields guard)。
         *
         * **不**把 result.success=false 升 throw(見 file header):NormalizedPhaseSync
         * 後端所有可預期失敗走 result.success=false,結果由 onResult 判並彈業務 ShowError。
         *
         * @param {object} ctx - ManifestPanel _gatherCtx 結果
         * @returns {Promise<object>} NormalizedPhaseSyncResult
         */
        rpc: async (ctx) => {
            // 4 個 phase 從 formBody render 的下拉讀(M5 wiring 後由
            // loadNormalizedPhaseSyncPhases populate;test 直接塞 DOM value)。
            const normStartPhase = document.getElementById('normalizedPhaseSyncNormStartPhase')?.value || '';
            const normEndPhase = document.getElementById('normalizedPhaseSyncNormEndPhase')?.value || '';
            const statsStartPhase = document.getElementById('normalizedPhaseSyncStatsStartPhase')?.value || '';
            const statsEndPhase = document.getElementById('normalizedPhaseSyncStatsEndPhase')?.value || '';

            // 對齊 main.js:2305 既有驗證:manifest / dataFolder / subject + 4 個 phase 全必填。
            // 沿用裸字串(對齊 M2/M3 慣例,理由見 phase_sync_spec.mjs 同段註解)。
            if (!ctx.manifestPath || !ctx.dataFolder
                || Number.isNaN(ctx.subjectIdx) || ctx.subjectIdx < 0
                || !normStartPhase || !normEndPhase
                || !statsStartPhase || !statsEndPhase) {
                throw new Error('請填寫所有必要欄位');
            }

            // NormalizedPhaseSyncParams json tag:manifestFile / dataFolder / subjectIndex /
            // normStartPhase / normEndPhase / statsStartPhase / statsEndPhase
            //(gui/normalized_phase_sync_handlers.go:24,以 Go struct 為準)。
            return AnalyzeNormalizedPhaseSync({
                manifestFile: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
                subjectIndex: ctx.subjectIdx,
                normStartPhase,
                normEndPhase,
                statsStartPhase,
                statsEndPhase,
            });
        },

        /**
         * Render 標準化分期同步結果區 + 自判 success 彈 dialog(對齊舊
         * showNormalizedPhaseSyncResult main.js:2336-2442 + executeNormalizedPhaseSyncAnalysis
         * 的 success 分支 :2319-2327)。
         *
         * 結果區(全 DOM API + textContent,XSS-safe — subject / phase / outputPath /
         * channelNames / report 來自後端 manifest 內容,user-controlled):
         *   1. result-info:主題 / 標準化區間 / 統計區間 / Output 1 路徑 / Output 2 路徑
         *   2. 肌肉表格(若 channelNames + channelMaxes):肌肉 / 標準化最大值 / 標準化後平均
         *   3. 分析報告 pre(若 result.report)
         *   4. 開啟輸出資料夾按鈕(走 mp.openOutputFolder)
         *
         * dialog(silentSuccess=true,onResult own):
         *   * result.success=true → ShowMessage(成功, t('success.msg.normalized_analysis_done',
         *       normalizedEMGPath, phaseSyncCSVPath))  ← 雙輸出路徑形狀
         *   * result.success=false → ShowError(錯誤, result.message)
         *
         * 既有 showNormalizedPhaseSyncResult 已走 i18n key(result.label.subject /
         * norm_range / stats_range / output_normalized / output_stats / table.header.* /
         * button.open_output_folder),本版原樣沿用(對齊 i18n_test.go:554 對 main.js
         * hardcode label 的回歸守護)。
         *
         * @param {object} result - NormalizedPhaseSyncResult(欄位見 file header)
         * @param {object} _ctx - ManifestPanel _gatherCtx 結果(本 onResult 不用)
         * @param {object} mp - ManifestPanel 實例(openOutputFolder 用)
         */
        onResult: async (result, _ctx, mp) => {
            const tt = globalThis.t;
            const contentDiv = document.getElementById('mpResultContent');
            contentDiv.textContent = '';

            const fmtTime = (x) => (typeof x === 'number' ? x.toFixed(3) : '—');

            // 1. result-info(對齊 main.js:2341-2361)。
            const info = document.createElement('div');
            info.className = 'result-info';
            const lines = [
                [tt('result.label.subject'), result.subject],
                [tt('result.label.norm_range'),
                    `${result.normStartPhase} (${fmtTime(result.normStartTime)}s) → ${result.normEndPhase} (${fmtTime(result.normEndTime)}s)`],
                [tt('result.label.stats_range'),
                    `${result.statsStartPhase} (${fmtTime(result.statsStartTime)}s) → ${result.statsEndPhase} (${fmtTime(result.statsEndTime)}s)`],
                [tt('result.label.output_normalized'), result.normalizedEMGPath],
                [tt('result.label.output_stats'), result.phaseSyncCSVPath],
            ];
            lines.forEach(([label, value]) => {
                const p = document.createElement('p');
                const strong = document.createElement('strong');
                strong.textContent = label;
                p.appendChild(strong);
                p.appendChild(document.createTextNode(value != null ? String(value) : ''));
                info.appendChild(p);
            });
            contentDiv.appendChild(info);

            // 2. 肌肉表格(對齊 main.js:2363-2417)。
            if (result.channelNames && result.channelMaxes) {
                const wrap = document.createElement('div');
                wrap.style.marginTop = '1rem';
                const h4 = document.createElement('h4');
                h4.textContent = tt('result.label.muscles');
                wrap.appendChild(h4);
                const note = document.createElement('p');
                note.className = 'help-text';
                note.style.marginBottom = '0.5rem';
                note.textContent = tt('result.normalized.help_text');
                wrap.appendChild(note);

                const table = document.createElement('table');
                table.className = 'result-table';
                table.style.width = '100%';
                table.style.borderCollapse = 'collapse';
                const thead = document.createElement('thead');
                const headRow = document.createElement('tr');
                [tt('table.header.muscle'), tt('table.header.norm_max'), tt('table.header.norm_mean')].forEach((h, i) => {
                    const th = document.createElement('th');
                    th.textContent = h;
                    th.style.padding = '0.25rem 0.5rem';
                    th.style.textAlign = i === 0 ? 'left' : 'right';
                    headRow.appendChild(th);
                });
                thead.appendChild(headRow);
                table.appendChild(thead);

                const tbody = document.createElement('tbody');
                result.channelNames.forEach((name) => {
                    const tr = document.createElement('tr');
                    const tdName = document.createElement('td');
                    tdName.textContent = name;
                    tdName.style.padding = '0.25rem 0.5rem';
                    const tdMax = document.createElement('td');
                    const maxVal = result.channelMaxes[name];
                    tdMax.textContent = typeof maxVal === 'number' ? maxVal.toFixed(6) : '—';
                    tdMax.style.textAlign = 'right';
                    tdMax.style.fontFamily = 'monospace';
                    tdMax.style.padding = '0.25rem 0.5rem';
                    const tdMean = document.createElement('td');
                    const meanVal = result.channelMeans ? result.channelMeans[name] : undefined;
                    tdMean.textContent = typeof meanVal === 'number' ? meanVal.toFixed(6) : '—';
                    tdMean.style.textAlign = 'right';
                    tdMean.style.fontFamily = 'monospace';
                    tdMean.style.padding = '0.25rem 0.5rem';
                    tr.appendChild(tdName);
                    tr.appendChild(tdMax);
                    tr.appendChild(tdMean);
                    tbody.appendChild(tr);
                });
                table.appendChild(tbody);
                wrap.appendChild(table);
                contentDiv.appendChild(wrap);
            }

            // 3. 分析報告(對齊 main.js:2419-2431;textContent 防 XSS)。
            if (result.report) {
                const reportWrap = document.createElement('div');
                reportWrap.className = 'result-report';
                reportWrap.style.marginTop = '1rem';
                const h4 = document.createElement('h4');
                h4.textContent = tt('result.label.analysis_report');
                reportWrap.appendChild(h4);
                const pre = document.createElement('pre');
                pre.className = 'result-pre';
                pre.textContent = result.report;
                reportWrap.appendChild(pre);
                contentDiv.appendChild(reportWrap);
            }

            // 4. 開啟輸出資料夾按鈕(對齊 main.js:2433-2440;走 mp.openOutputFolder)。
            const btnGroup = document.createElement('div');
            btnGroup.className = 'button-group';
            const btn = document.createElement('button');
            btn.className = 'btn btn-primary';
            btn.textContent = tt('button.open_output_folder');
            btn.addEventListener('click', () => mp.openOutputFolder());
            btnGroup.appendChild(btn);
            contentDiv.appendChild(btnGroup);

            document.getElementById('mpResult').style.display = 'block';

            // dialog(onResult own,對齊 main.js:2319-2327)。
            if (result.success) {
                // 成功訊息帶雙輸出路徑(Output 1 標準化 EMG + Output 2 分期統計)。
                await globalThis.ShowMessage(
                    tt('dialog.success'),
                    tt('success.msg.normalized_analysis_done', result.normalizedEMGPath, result.phaseSyncCSVPath)
                );
            } else {
                await globalThis.ShowError(tt('dialog.error'), result.message);
            }
        },
    };
}
