// frontend/src/panels/muscle_ratio_spec.mjs
//
// 肌肉比值分析 panel spec — 餵給 `ManifestPanel.run(spec)` 走 envelope
// (ADR-0007 §6,M4 交付)。MuscleRatio 是三個「CSV-success / 無 iframe」panel
// 中最簡單的一個:
//   * hideSubject: true — MuscleRatio 只吃 manifest + dataFolder(批次處理 manifest
//     內所有 subject,**無 subject 下拉**;ADR-0007 §2 入口定義)。shell 因此省略
//     subject select,rpc 也不讀 ctx.subjectIdx / subjectName。
//   * silentSuccess: true — MuscleRatio 有 PARTIAL-FAILED 語意(部分 subject 成功、
//     部分失敗):result.success=false 時走 ShowError(部分失敗),success=true 走
//     ShowMessage(完成)。envelope 的標準「rpc resolve = success → ShowMessage」
//     不適用此三態,故 onResult 自己 own dialog(對齊 main.js:2542-2548)。
//   * formBody — 只剩 panel 描述 help-text 一行(manifest + dataFolder 由 shell
//     提供,subject 被 hideSubject 省略)。對齊原 showMuscleRatioPanel(main.js:2457)
//     的 `panel.muscleratio.description` 說明文字。
//
// **為何 silentSuccess=true + onResult own dialog(有別於 CCI 的 rpc-throw 模式)**:
//   CCI(cci_spec.mjs)把 backend result.success=false 升 throw,讓 envelope 統一走
//   ShowError + 共通 message。但 MuscleRatio 的失敗不是「分析失敗」而是「部分主題
//   未完成」— Subjects 陣列仍 non-nil,結果表格要照常 render(每行標個別成功/失敗)。
//   若升 throw,onResult 不會跑、結果表格不會 render。故 MuscleRatio 改:rpc 只在
//   「缺必填輸入」時 throw(exception);backend 回的 result 一律交給 onResult,由
//   onResult 判 result.success → 業務 dialog(success / partial-failed)。三個 M4
//   panel(MuscleRatio / PhaseSync / NormalizedPhaseSync)統一此模式。
//
// **後端 param / result 欄位名(已對 Go struct 驗證,勿臆測)**:
//   MuscleRatioParams { manifestFile, dataFolder }            ← gui/muscle_ratio_handlers.go:13
//   MuscleRatioResult { subjects, success, message }          ← gui/muscle_ratio_handlers.go:49
//   MuscleRatioSubjectDTO { subject, outputAllPath, outputPhasePath,
//                           success, error, durationMs }       ← gui/muscle_ratio_handlers.go:22
//   注意:Subjects==nil 永遠代表 pre-analysis fail(整批未啟動);caller 必須先檢
//   success / subjects nil-ness 再 iterate(對齊 Go struct 註解 muscle_ratio_handlers.go:47)。
//
// invariant(由 muscle_ratio_spec.test.mjs 釘):
//   * hideSubject === true、silentSuccess === true
//   * spec.formBody 為 builder function(只有描述 help-text;無繁中洩漏)
//   * rpc 在 manifestPath / dataFolder 缺時 throw(不讀 subjectIdx)

import { AnalyzeMuscleRatio } from '../../wailsjs/go/gui/App.js';

/**
 * 肌肉比值 panel HTML body(注入 ManifestPanel shell 的 spec.formBody)。
 *
 * 原 showMuscleRatioPanel(main.js:2457)在 manifest 之前有一行 `panel.muscleratio.description`
 * 說明文字。manifest / dataFolder 由 shell 提供、subject 由 hideSubject 省略,故
 * formBody 只剩這行描述。維持 builder function 形狀以對齊 ManifestPanel spec contract
 *(`spec.formBody(tH)` 在 _renderShell 內無條件呼叫)。
 *
 * 注意:此描述 render 在 shell 的 manifest input 「之後」(formBody 注入點在三段
 * input 之下),與原 main.js 描述在 manifest 「之前」位置略異 — 純文案位置差異,
 * 不影響功能。M5 reviewer 若在意位置可調 shell 注入點,屬 internal detail。
 *
 * @param {(key: string) => string} t - translator(production: tHtml,test: fake)
 * @returns {string} formBody HTML(描述 help-text)
 */
export function muscleRatioFormBody(t) {
    return `
            <p class="help-text">${t('panel.muscleratio.description')}</p>
        `;
}

/**
 * 肌肉比值 spec object — 給 ManifestPanel.run(spec) 消費。
 *
 * Factory 接 `app` 注入(對齊 cci / composer spec)。MuscleRatio onResult 不寫任何
 * app this state(無 download / 無 iframe ready promise),`app` 目前未被 closure
 * 直接使用 — 仍保留 factory 簽名以對齊其他 4 panel 一致性 + 未來若需 app method
 *(例如 openOutputFolder 走 mp.openOutputFolder 已足)無需改簽名。
 *
 * @param {object} _app - EMGAnalysisApp 實例(MuscleRatio 目前未用,保留簽名一致性)
 * @returns {object} spec for ManifestPanel.run(spec)
 */
export function makeMuscleRatioSpec(_app) {
    return {
        titleKey: 'panel.muscleratio.title',
        statusRunningKey: 'status.muscle_running',
        // hideSubject: MuscleRatio 無 subject 下拉(批次處理整個 manifest);
        //   shell 省略 subject select(ADR-0007 §2 入口定義)。
        hideSubject: true,
        // silentSuccess: MuscleRatio 三態(all-success / partial-fail / pre-analysis
        //   fail)的 dialog 由 onResult 自己判 result.success 後彈,不走 envelope
        //   標準 ShowMessage(對齊 main.js:2542-2548)。
        silentSuccess: true,

        formBody: muscleRatioFormBody,

        /**
         * 呼 AnalyzeMuscleRatio RPC。
         *
         * 與 CCI/Composer 不同:**不**把 result.success=false 升 throw。MuscleRatio
         * 的 success=false 是「部分主題未完成」業務狀態,結果表格仍要 render,故交給
         * onResult 處理。rpc 只在「缺必填輸入」(exception)時 throw → envelope catch
         * 走 generic ShowError(對齊 main.js:2530-2533 既有 fill_fields guard)。
         *
         * 既有 main.js:2522-2524 的 btn.disabled 防雙擊 guard 已被 envelope reentrant
         * guard 取代(ADR-0007 §7),此處不再重複。
         *
         * @param {object} ctx - ManifestPanel _gatherCtx 結果(MuscleRatio 只讀
         *   manifestPath / dataFolder;hideSubject=true 時 subjectIdx=NaN 不讀)
         * @returns {Promise<object>} MuscleRatioResult(subjects / success / message)
         */
        rpc: async (ctx) => {
            // ctx.manifestPath / ctx.dataFolder 來自 shell 的 #mpManifestPath /
            // #mpDataFolder。對應後端 MuscleRatioParams 的 manifestFile / dataFolder
            //(gui/muscle_ratio_handlers.go:13,以 Go struct 為準)。
            if (!ctx.manifestPath || !ctx.dataFolder) {
                // 對齊 main.js:2531 既有錯誤文案(error.msg.muscle_fill_fields →
                // 「請選擇分期總檔案與數據資料夾」)。沿用裸字串而非 globalThis.t(key):
                // 對齊 M2/M3 rpc throw 裸字串慣例(rpc 不該依賴 globalThis.t 在 test
                // 環境存在);envelope catch 後以 error.msg.analysis_failed_dynamic
                // 包裝顯示(manifestPanel.mjs:150)。
                throw new Error('請選擇分期總檔案與數據資料夾');
            }

            // MuscleRatioParams 只有 manifestFile / dataFolder(無 subject)。
            return AnalyzeMuscleRatio({
                manifestFile: ctx.manifestPath,
                dataFolder: ctx.dataFolder,
            });
        },

        /**
         * Render 肌肉比值批次結果 + 自判 success 彈 dialog(對齊舊 showMuscleRatioResult
         * main.js:2558-2647 + executeMuscleRatioAnalysis 的 success 分支 :2542-2548)。
         *
         * 結果區結構(全 DOM API + textContent,XSS-safe — subject / error /
         * outputPath 來自外部 manifest 內容,user-controlled):
         *   1. summary 行:「共 N 個主題:<message>」
         *   2. 若有 subjects:per-subject 表格(主題 / 狀態 / Output1 / Output2 / 耗時 / 訊息)
         *
         * dialog(silentSuccess=true,onResult own):
         *   * result.success=true → ShowMessage(完成, message)
         *   * result.success=false → ShowError(部分失敗, message)
         *
         * @param {object} result - MuscleRatioResult(欄位見 file header,已對 Go struct 驗證)
         * @param {object} _ctx - ManifestPanel _gatherCtx 結果(本 onResult 不用)
         * @param {object} _mp - ManifestPanel 實例(MuscleRatio 不用 helper)
         */
        onResult: async (result, _ctx, _mp) => {
            const tt = globalThis.t;
            const contentDiv = document.getElementById('mpResultContent');
            contentDiv.textContent = '';

            // 1. summary 行(對齊 main.js:2564-2570)。count 走 result.muscle.subject_count
            //    i18n key(帶 %d),冒號 + message 用 textNode 拼接避免 XSS。
            const summary = document.createElement('p');
            const strong = document.createElement('strong');
            const count = result.subjects ? result.subjects.length : 0;
            strong.textContent = tt('result.muscle.subject_count', count);
            summary.appendChild(strong);
            summary.appendChild(document.createTextNode(`：${result.message || ''}`));
            contentDiv.appendChild(summary);

            // Subjects==nil/空 → pre-analysis fail(整批未啟動),只顯示 summary。
            // 對齊 main.js:2572-2575 early-return(顯示 result section 後結束)。
            if (!result.subjects || result.subjects.length === 0) {
                document.getElementById('mpResult').style.display = 'block';
                await muscleRatioDialog(result);
                return;
            }

            // 2. per-subject 表格(對齊 main.js:2577-2644)。
            const table = document.createElement('table');
            table.className = 'result-table';
            table.style.width = '100%';
            table.style.borderCollapse = 'collapse';
            table.style.marginTop = '0.5rem';

            const thead = document.createElement('thead');
            const headRow = document.createElement('tr');
            [
                tt('table.header.subject'),
                tt('table.header.status'),
                tt('table.header.output_all'),
                tt('table.header.output_phase'),
                tt('table.header.duration'),
                tt('table.header.message'),
            ].forEach((h) => {
                const th = document.createElement('th');
                th.textContent = h;
                th.style.padding = '0.25rem 0.5rem';
                th.style.textAlign = 'left';
                th.style.borderBottom = '1px solid #ddd';
                headRow.appendChild(th);
            });
            thead.appendChild(headRow);
            table.appendChild(thead);

            const tbody = document.createElement('tbody');
            result.subjects.forEach((sr) => {
                const tr = document.createElement('tr');

                const tdName = document.createElement('td');
                tdName.textContent = sr.subject;
                tdName.style.padding = '0.25rem 0.5rem';
                tr.appendChild(tdName);

                const tdStatus = document.createElement('td');
                tdStatus.textContent = sr.success ? '✓' : '✗';
                tdStatus.style.padding = '0.25rem 0.5rem';
                tdStatus.style.color = sr.success ? '#2a9d3f' : '#c0392b';
                tdStatus.style.fontWeight = 'bold';
                tr.appendChild(tdStatus);

                const tdAll = document.createElement('td');
                tdAll.textContent = sr.outputAllPath || '—';
                tdAll.style.padding = '0.25rem 0.5rem';
                tdAll.style.fontFamily = 'monospace';
                tdAll.style.fontSize = '0.85em';
                tr.appendChild(tdAll);

                const tdPhase = document.createElement('td');
                tdPhase.textContent = sr.outputPhasePath || '—';
                tdPhase.style.padding = '0.25rem 0.5rem';
                tdPhase.style.fontFamily = 'monospace';
                tdPhase.style.fontSize = '0.85em';
                tr.appendChild(tdPhase);

                const tdDuration = document.createElement('td');
                tdDuration.textContent = sr.durationMs > 0 ? sr.durationMs.toString() : '—';
                tdDuration.style.padding = '0.25rem 0.5rem';
                tdDuration.style.fontFamily = 'monospace';
                tdDuration.style.fontSize = '0.85em';
                tdDuration.style.textAlign = 'right';
                tr.appendChild(tdDuration);

                const tdErr = document.createElement('td');
                tdErr.textContent = sr.error || '';
                tdErr.style.padding = '0.25rem 0.5rem';
                tdErr.style.fontSize = '0.85em';
                tdErr.style.color = sr.error ? '#c0392b' : 'inherit';
                tr.appendChild(tdErr);

                tbody.appendChild(tr);
            });
            table.appendChild(tbody);
            contentDiv.appendChild(table);

            document.getElementById('mpResult').style.display = 'block';

            // 3. dialog(onResult own,對齊 main.js:2542-2548)。
            await muscleRatioDialog(result);
        },
    };
}

/**
 * MuscleRatio 的 success/partial-fail dialog(onResult own,silentSuccess=true)。
 *
 * 對齊 main.js:2542-2548:
 *   * result.success=true → ShowMessage(dialog.title.complete, message)
 *   * result.success=false → ShowError(dialog.title.partial_failed, message)
 *
 * 抽成 module-level helper 讓 early-return(無 subjects)與正常路徑共用同一段
 * dialog 邏輯,避免重複。走 globalThis.t / ShowMessage / ShowError(production
 * 由 main.js 掛在 window,test 由 stub 提供)。
 *
 * @param {object} result - MuscleRatioResult(success / message)
 */
async function muscleRatioDialog(result) {
    const tt = globalThis.t;
    if (result.success) {
        await globalThis.ShowMessage(tt('dialog.title.complete'), result.message);
    } else {
        await globalThis.ShowError(tt('dialog.title.partial_failed'), result.message);
    }
}
