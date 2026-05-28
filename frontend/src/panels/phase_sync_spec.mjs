// frontend/src/panels/phase_sync_spec.mjs
//
// 分期同步分析 panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M4 交付)。PhaseSync 是「CSV-success / 無 iframe」panel:
//   * subject 用 subjectIdx(number)— hideSubject 省略走 default(顯示 subject select)。
//   * silentSuccess: true — 成功訊息是 `t('success.msg.analysis_done', result.outputPath)`
//    (outputPath-based,非 result.message),且失敗走 result.success=false 業務分支,
//     envelope 的標準「rpc resolve = success → ShowMessage(result.message)」訊息形狀
//     不符,故 onResult 自己 own dialog(對齊 main.js:1156-1161)。
//   * formBody — startPhase + endPhase 兩個下拉(empty option 結構,panel-specific
//     ids phaseSyncStartPhase / phaseSyncEndPhase)。**下拉 option 的 POPULATION
//    (loadAvailablePhases,main.js:1103-1129)是 M5 wiring,不在本 spec** — formBody
//     只 render 空殼結構。
//
// **為何 silentSuccess=true + onResult own dialog(有別於 CCI 的 rpc-throw 模式)**:
//   PhaseSync 後端用 dual/mixed 錯誤通道(gui/app.go:1144-1147):Validate 失敗(必填
//   缺、路徑 traversal)→ `(nil, err)` 走 err channel(Wails binding reject → envelope
//   catch → ShowError);Execute/WriteCSV 失敗 → `(failedResult, nil)` 走 result.success
//   =false。後者不是「分析失敗」exception 而是業務軟失敗,且成功訊息要帶 outputPath。
//   故 rpc 只在「缺必填輸入」(exception)時 throw;backend 回的 result 一律交給
//   onResult,由 onResult 判 result.success → 業務 dialog(success 帶 outputPath /
//   fail 帶 result.message)。三個 M4 panel 統一此模式。
//
//   注意:rpc **不**把 result.success=false 升 throw(對比 CCI cci_spec.mjs:139)。
//   CCI 的 onResult 沒有 dialog 責任(envelope ShowMessage),失敗升 throw 才走 ShowError;
//   PhaseSync 的 onResult 已 own dialog,升 throw 反而會跳過結果區 render + 用錯訊息形狀。
//
// **後端 param / result 欄位名(已對 Go struct 驗證,勿臆測)**:
//   PhaseSyncParams { manifestFile, dataFolder, startPhase, endPhase, subjectIndex }
//                                                            ← gui/app.go:1050
//     (startPhase/endPhase 為 models.PhasePoint,wire 上是 string)
//   PhaseSyncResult { outputPath, subject, startPhase, startTime, endPhase, endTime,
//                     channelNames, channelMeans, channelMaxes, report, success, message }
//                                                            ← gui/app.go:1059
//
// invariant(由 phase_sync_spec.test.mjs 釘):
//   * silentSuccess === true、hideSubject 省略(undefined)
//   * spec.formBody 為 builder function 且不洩漏 hardcoded 繁中(start/end phase 下拉)
//   * rpc 在 manifestPath / dataFolder 缺、subjectIdx NaN/<0、startPhase/endPhase 缺時 throw

import { AnalyzePhaseSync, LoadPhaseManifest } from '../../wailsjs/go/gui/App.js';

/**
 * 分期同步 panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * 原 showPhaseSyncPanel(main.js:987-1042)在 shell 提供的 manifest / dataFolder /
 * subject 三段之後,有第 4 點「開始分期點 + 結束分期點」兩個下拉(form-row 並排)。
 * 這兩個下拉是 PhaseSync 獨有(CCI 沒有),保留在 formBody;其餘三段由 shell 提供。
 *
 * 下拉 option 內容(start / end phase 列表)由 M5 wiring 的 loadAvailablePhases
 * 動態 populate(對齊 main.js:1103-1129);formBody 只 render 空殼 + 一個 placeholder
 * option,避免在 spec 內耦合 phase 載入邏輯。
 *
 * panel-specific ids:phaseSyncStartPhase / phaseSyncEndPhase(沿用原 main.js id,
 * M5 wiring 的 loadAvailablePhases 仍可用同 id 找到)。
 *
 * @param {(key: string) => string} t - translator(production: tHtml,test: fake)
 * @returns {string} formBody HTML(start/end phase 下拉空殼)
 */
export function phaseSyncFormBody(t) {
    return `
            <div class="form-row">
                <div class="form-group col-md-6">
                    <label>4. ${t('form.label.start_phase')}</label>
                    <select id="phaseSyncStartPhase" class="form-control">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
                <div class="form-group col-md-6">
                    <label>${t('form.label.end_phase')}</label>
                    <select id="phaseSyncEndPhase" class="form-control">
                        <option value="">${t('form.option.select')}</option>
                    </select>
                </div>
            </div>
        `;
}

/**
 * 分期同步 spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(對齊 cci / composer spec)。PhaseSync onResult 走
 * mp.openOutputFolder()(不直接讀 app);afterRender 走 app.loadAvailablePhases()
 * 填 phase 下拉。
 *
 * @param {object} app - EMGAnalysisApp 實例(afterRender 呼 loadAvailablePhases)
 * @returns {object} spec for ManifestPanel.run(spec)
 */
export function makePhaseSyncSpec(app) {
    return {
        titleKey: 'panel.phasesync.title',
        statusRunningKey: 'status.phasesync_running',
        // hideSubject 省略 → PhaseSync 用 subject(subjectIdx)。
        // silentSuccess: 成功訊息帶 outputPath(非 result.message),且 success=false
        //   走業務 ShowError;dialog 由 onResult own(對齊 main.js:1156-1161)。
        silentSuccess: true,

        formBody: phaseSyncFormBody,

        /**
         * Subject load(ADR-0007 §4 index-mode):同 CCI 走 LoadPhaseManifest,
         * valueMode='index'(對齊舊 loadManifestSubjects main.js:1074-1078)。
         *
         * @param {string} manifestPath - #mpManifestPath value
         * @returns {Promise<{subjects: string[], valueMode: 'index'}>}
         */
        loadSubjects: async (manifestPath) => ({
            subjects: await LoadPhaseManifest(manifestPath),
            valueMode: 'index',
        }),

        /**
         * afterRender:panel render 完後 load phase 下拉(GetAvailablePhases,
         * manifest-independent)。對齊舊 showPhaseSyncPanel:1045 的
         * `this.loadAvailablePhases()`(在 panel render 尾端呼叫)。loadAvailablePhases
         * 仍掛 app this(M5 wiring 保留),填 #phaseSyncStartPhase / #phaseSyncEndPhase
         *(formBody render 的下拉)。
         *
         * @param {object} _mp - ManifestPanel 實例(本 hook 不用,走 app.loadAvailablePhases)
         */
        afterRender: (_mp) => app.loadAvailablePhases(),

        /**
         * 呼 AnalyzePhaseSync RPC。
         *
         * 從 DOM 讀 startPhase / endPhase(formBody render 的下拉),subjectIdx 來自
         * ctx(shell 的 #mpSubject)。所有必填齊才呼 RPC,缺則 throw(exception →
         * envelope ShowError;對齊 main.js:1141-1144 既有 fill_required_fields guard)。
         *
         * **不**把 result.success=false 升 throw(見 file header):PhaseSync 後端 dual
         * 通道,Execute/Write 軟失敗走 result.success=false,結果由 onResult 判並彈
         * 業務 ShowError。rpc 原樣回傳 result。
         *
         * @param {object} ctx - ManifestPanel _gatherCtx 結果
         *   { manifestPath, dataFolder, subjectIdx, subjectName, subjects }
         * @returns {Promise<object>} PhaseSyncResult(success / message / outputPath / ...)
         */
        rpc: async (ctx) => {
            // startPhase / endPhase 從 formBody render 的下拉讀(M5 wiring 後由
            // loadAvailablePhases populate;test 直接塞 DOM value)。
            const startPhase = document.getElementById('phaseSyncStartPhase')?.value || '';
            const endPhase = document.getElementById('phaseSyncEndPhase')?.value || '';

            // 對齊 main.js:1141 既有驗證:manifest / dataFolder / subject / start / end 全必填。
            // 沿用裸字串而非 globalThis.t(key):對齊 M2/M3 rpc throw 裸字串慣例(rpc 不該
            // 依賴 globalThis.t 在 test 環境存在);envelope catch 後以
            // error.msg.analysis_failed_dynamic 包裝顯示(manifestPanel.mjs:150)。
            if (!ctx.manifestPath || !ctx.dataFolder
                || Number.isNaN(ctx.subjectIdx) || ctx.subjectIdx < 0
                || !startPhase || !endPhase) {
                throw new Error('請填寫所有必要欄位');
            }

            // PhaseSyncParams json tag:manifestFile / dataFolder / subjectIndex /
            // startPhase / endPhase(gui/app.go:1050,以 Go struct 為準)。
            // startPhase / endPhase 為 models.PhasePoint(wire 上 string),下拉 value
            // 直接送即可。
            return AnalyzePhaseSync({
                manifestFile: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
                subjectIndex: ctx.subjectIdx,
                startPhase,
                endPhase,
            });
        },

        /**
         * Render 分期同步結果區 + 自判 success 彈 dialog(對齊舊 showPhaseSyncResult
         * main.js:1177-1228 + executePhaseSyncAnalysis 的 success 分支 :1156-1161)。
         *
         * 結果區(全 DOM API + textContent,XSS-safe — subject / startPhase / endPhase /
         * outputPath / report 來自後端 manifest 內容,user-controlled):
         *   1. result-info:主題 / 分析區間(start phase + time → end phase + time)/ 輸出檔案
         *   2. 分析報告 pre(若 result.report)
         *   3. 開啟輸出資料夾按鈕(走 mp.openOutputFolder)
         *
         * dialog(silentSuccess=true,onResult own):
         *   * result.success=true → ShowMessage(成功, t('success.msg.analysis_done', outputPath))
         *   * result.success=false → ShowError(錯誤, result.message)
         *
         * **與舊 main.js 差異(刻意改善)**:舊 showPhaseSyncResult 用 hardcoded 繁中
         * label(`'主題:'` / `'分析區間:'` / `'輸出檔案:'` / `'分析報告'` /
         * `'開啟輸出資料夾'`,main.js:1188-1222)。本版改走既有 i18n key
         *(result.label.subject / analysis_range / output_file / analysis_report /
         * button.open_output_folder — 翻譯值與 hardcoded 字面完全一致,見
         * translations_zh_tw.go:244-247)。修掉 non-zh locale 下 label 仍是繁中的
         * mixed-language bug(對齊 NormalizedPhaseSync showNormalizedPhaseSyncResult
         * 已走 i18n key 的做法 + i18n_test.go:554 對 main.js hardcode label 的回歸守護)。
         *
         * @param {object} result - PhaseSyncResult(欄位見 file header,已對 Go struct 驗證)
         * @param {object} _ctx - ManifestPanel _gatherCtx 結果(本 onResult 不用)
         * @param {object} mp - ManifestPanel 實例(openOutputFolder 用)
         */
        onResult: async (result, _ctx, mp) => {
            const tt = globalThis.t;
            const contentDiv = document.getElementById('mpResultContent');
            contentDiv.textContent = '';

            const fmtTime = (x) => (typeof x === 'number' ? x.toFixed(3) : '—');

            // 1. result-info(全 user-controlled 字串走 textContent)。
            const info = document.createElement('div');
            info.className = 'result-info';
            const lines = [
                [tt('result.label.subject'), result.subject],
                [tt('result.label.analysis_range'),
                    `${result.startPhase} (${fmtTime(result.startTime)}s) → ${result.endPhase} (${fmtTime(result.endTime)}s)`],
                [tt('result.label.output_file'), result.outputPath],
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

            // 2. 分析報告(report 來自後端拼接含 user-controlled subject;textContent)。
            if (result.report) {
                const reportWrap = document.createElement('div');
                reportWrap.className = 'result-report';
                const h4 = document.createElement('h4');
                h4.textContent = tt('result.label.analysis_report');
                reportWrap.appendChild(h4);
                const pre = document.createElement('pre');
                pre.className = 'result-pre';
                pre.textContent = result.report;
                reportWrap.appendChild(pre);
                contentDiv.appendChild(reportWrap);
            }

            // 3. 開啟輸出資料夾按鈕(走 mp.openOutputFolder → app.openOutputFolder)。
            const btnGroup = document.createElement('div');
            btnGroup.className = 'button-group';
            const btn = document.createElement('button');
            btn.className = 'btn btn-primary';
            btn.textContent = tt('button.open_output_folder');
            btn.addEventListener('click', () => mp.openOutputFolder());
            btnGroup.appendChild(btn);
            contentDiv.appendChild(btnGroup);

            document.getElementById('mpResult').style.display = 'block';

            // dialog(onResult own,對齊 main.js:1156-1161)。
            if (result.success) {
                // 成功訊息帶 outputPath(非 result.message)— PhaseSync 特有形狀。
                await globalThis.ShowMessage(
                    tt('dialog.success'),
                    tt('success.msg.analysis_done', result.outputPath)
                );
            } else {
                await globalThis.ShowError(tt('dialog.error'), result.message);
            }
        },
    };
}
