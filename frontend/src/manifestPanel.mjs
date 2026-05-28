// frontend/src/manifestPanel.mjs
//
// ManifestPanel — Frontend panel layer 的 deep module(ADR-0007)。
//
// 收乾 5 個「以 Manifest + dataFolder 為入口的 panel」共通 boilerplate:
// CCI / PhaseSync / NormalizedPhaseSync / MuscleRatio / Chart Composer。
// 每個 panel 不再各自重寫 panel template / subject load / RPC envelope /
// phase-checkbox render,而是傳一個 spec object 給 `ManifestPanel.run`。
//
// 與 Analysis pipeline family(backend Go-side handler 家族)不衝突 — 後者
// compute + CSV、本 module frontend panel layer,兩個正交軸(CONTEXT.md
// ManifestPanel 條目 + ADR-0007 §1 已釘)。
//
// API contract(by ADR-0007 §6,locked,M2-M4 5 個 spec 直接吃此 shape):
//   ManifestPanel.run({
//     titleKey: 'panel.cci.title',
//     statusRunningKey: 'status.cci_running',
//     runBtnLabelKey: 'button.start_analyze',  // optional, default 'button.start_analyze'
//                                              // Composer override 為 'button.generate_chart'
//     formBody: (t) => `<div>... ${t('form.label.muscleratio_layout')} ...</div>`,
//     rpc: async (ctx) => AnalyzeCCI(ctx.manifestPath, ctx.dataFolder, ctx.subjectIdx),
//     onResult: async (result, ctx, mp) => { mp.attachIframe(...); mp.bindPhaseCheckboxes(...); },
//     silentSuccess: false,  // optional, Composer 寫 true、其他 4 省略
//   });
//   // ctx shape:{ manifestPath, dataFolder, subjectIdx, subjectName, subjects }
//   //   subjects: 預留欄位(目前 always []);M3/M4 spec author 如需 subjects array,
//   //   自 `app._<adapter>Subjects` 取(M1 _gatherCtx 不主動 populate)。
//
// 核心不變式(由 manifestPanel.test.mjs 釘):
//   - Envelope 自帶 reentrant guard(ADR-0007 §7,防 MuscleRatio doubleclick race)
//   - `_running` flag 在 finally reset(不會永久 locked)
//   - 共通 i18n key 由 ManifestPanel hardcode(`status.analysis_done` /
//     `dialog.title.complete` / `dialog.error` / `status.analysis_failed` /
//     `error.msg.analysis_failed_dynamic`)— 不擴 i18n schema、不污染 spec API
//   - silentSuccess=true 略過 ShowMessage,其他 4 panel 預設 false
//   - registerCleanup 在「下次」run 開頭 flush(re-attach iframe 前清舊
//     subscription),取代既有 `_cciBridgeUnsub?.()` ad-hoc pattern
//
// 跟 iframeBridge(ADR-0003)的邊界:
//   - `mp.attachIframe` 只 own iframe element + sandbox + srcdoc + ready promise
//   - 不包 bridge.subscribe / send / requestReply — 由 onResult closure 直接呼
//     bridge(維持 bridge 是 parent ↔ iframe 唯一 facade,不堆 third layer)
//   - cleanup hook(`mp.registerCleanup(unsub)`)讓 closure 自行註冊 unsub
//     callback,ManifestPanel 下次 run 開頭自動 flush 全部
//
// Panel state 仍掛 app this(_cciResult / _composerPhaseTimes /
// _composerCheckedPhases 等),**不引入 mp.scope namespace**(ADR-0007 §9)。
// 既有 onLocaleChange `panelDispatch[currentPanel]()` re-render pattern 天然
// work — 只重設 functionPanel.innerHTML、不動 app instance state。

import { SelectFile, SelectDirectory } from '../wailsjs/go/gui/App.js';

/**
 * ManifestPanel — 5 個 manifest+dataFolder panel 共用的 envelope。
 *
 * 使用方式(M5 wiring 時):
 *   class EMGAnalysisApp {
 *     constructor() {
 *       this._manifestPanel = new ManifestPanel(this);
 *       ...
 *     }
 *     showCCIPanel() { this._manifestPanel.run(cciSpec); }
 *   }
 */
export class ManifestPanel {
    /**
     * @param {object} app - EMGAnalysisApp 實例(取 showPanel / updateStatus /
     *   openOutputFolder 共用方法)。注入而非 globalThis.app:test 友善。
     */
    constructor(app) {
        this._app = app;
        this._running = false;       // reentrant guard(ADR-0007 §7)
        this._cleanups = [];         // 下次 run 開頭 flush(re-attach iframe 前清舊 unsub)
        this._currentSpec = null;    // 給 run button click handler 拿到當前 spec
    }

    /**
     * Render panel shell + 綁定 run button → _runEnvelope(spec)。
     * spec.formBody(translator) 被注入 panel 中段。
     *
     * 同步操作 — DOM render + button binding,不 await RPC。
     *
     * @param {object} spec
     * @param {string} spec.titleKey - panel header i18n key
     * @param {string} spec.statusRunningKey - rpc 執行中 status bar i18n key
     * @param {string} [spec.runBtnLabelKey='button.start_analyze'] - run button
     *   label i18n key。其他 4 panel 省略走 default;Composer 寫 'button.generate_chart'。
     * @param {(t: (k:string)=>string) => string} spec.formBody - panel 中段 HTML builder
     * @param {(ctx: object) => Promise<object>} spec.rpc - Wails backend call
     * @param {(result: object, ctx: object, mp: ManifestPanel) => Promise<void>} spec.onResult
     * @param {boolean} [spec.silentSuccess=false] - true 略過 ShowMessage(Composer 用)
     * @param {boolean} [spec.hideSubject=false] - true 時 shell 省略 subject `<select>`
     *   區塊。MuscleRatio 只吃 manifest + dataFolder(無 subject,ADR-0007 §2 入口
     *   定義),設此旗標;其他 4 panel 省略走 default(顯示 subject select)。
     * @param {(manifestPath: string, dataFolder: string) => Promise<{subjects: string[], valueMode: ('index'|'name')}>} [spec.loadSubjects]
     *   - Optional。manifest 選好後 selectMpManifest 呼此 fn 取 subject 列表並填
     *   `#mpSubject`。**Subject 兩形態(ADR-0007 §4)**:valueMode='index' 時
     *   option.value 寫 0-based 索引(CCI/PhaseSync/Normalized — subjectIdx);
     *   valueMode='name' 時 option.value 寫 subject string(Composer — subjectName,
     *   ADR-0002 canonical-key)。MuscleRatio 省略(hideSubject,無 subject load)。
     * @param {(mp: ManifestPanel) => void} [spec.afterRender] - Optional。run() 把
     *   shell + formBody render 完、showPanel 之後呼叫。PhaseSync/Normalized 用來
     *   load phase 下拉(GetAvailablePhases,manifest-independent)+ (Normalized)
     *   綁 mirror。CCI/Composer/MuscleRatio 省略。
     * @param {(mp: ManifestPanel) => void} [spec.onSubjectChange] - Optional。
     *   `#mpSubject` change 時呼叫(經 onMpSubjectChange delegator)。Composer 用來
     *   reset state(_composerEMGMotionOffset / _composerLoadedSubject)+ 清 chart。
     *   其他 panel 省略(subject change 無特殊行為)。
     */
    run(spec) {
        this._currentSpec = spec;
        const panel = document.getElementById('functionPanel');
        panel.innerHTML = this._renderShell(spec);

        const runBtn = document.getElementById('mpRunBtn');
        if (runBtn) {
            runBtn.addEventListener('click', () => {
                // 不 await — 讓 click handler 立即返回;async error 由 _runEnvelope
                // 內部 try/catch + ShowError 處理。
                this._runEnvelope(spec);
            });
        }

        this._app.showPanel();

        // afterRender hook(ADR §reversibility:reversible spec 欄位增補)。
        // shell + formBody 已 render 進 DOM、panel 已顯示後才呼叫,讓 hook 能安全
        // 找到 formBody 內的 element(PhaseSync/Normalized 的 phase 下拉)。
        // onLocaleChange re-run panelDispatch[currentPanel]() 會重新走 run() →
        // afterRender 自然 re-fire,phase 下拉跟著 re-populate(對齊既有
        // showPhaseSyncPanel:1045 / showNormalizedPhaseSyncPanel:2189 在 panel
        // render 尾端呼 loadAvailablePhases / loadNormalizedPhaseSyncPhases 的時機)。
        spec.afterRender?.(this);
    }

    /**
     * Envelope:RPC 包成共通的 try / updateStatus / ShowMessage / ShowError /
     * reentrant guard / cleanup flush 流程。
     *
     * 流程:
     *   1. Reentrant guard:若 _running 已 true,直接 return(MuscleRatio
     *      doubleclick race 防護)
     *   2. Flush 上次 run 註冊的 cleanups(re-attach iframe 前清舊 subscription)
     *   3. updateStatus(running) → rpc(ctx) → updateStatus(done)
     *   4. await onResult(result, ctx, mp)
     *   5. silentSuccess=false 時 ShowMessage(complete)
     *   6. throw 時 updateStatus(failed) + ShowError
     *   7. finally:_running = false(永遠 reset,不論 throw)
     */
    async _runEnvelope(spec) {
        if (this._running) return; // reentrant guard(ADR-0007 §7)
        this._running = true;

        // Flush 上次 run 註冊的 cleanups(在新 subscriber register 之前,避免
        // 新舊 mix)。clear 後 onResult closure 才會註冊本次新的 cleanups。
        this._flushCleanups();

        try {
            const ctx = this._gatherCtx();
            this._app.updateStatus(globalThis.t(spec.statusRunningKey));
            const result = await spec.rpc(ctx);
            this._app.updateStatus(globalThis.t('status.analysis_done'));

            // onResult 可能 async(iframe load wait / bridge subscribe);await
            // 讓後續 ShowMessage 等到結果 render 完成才彈出。
            await spec.onResult(result, ctx, this);

            if (!spec.silentSuccess) {
                await globalThis.ShowMessage(
                    globalThis.t('dialog.title.complete'),
                    result.message
                );
            }
        } catch (err) {
            this._app.updateStatus(globalThis.t('status.analysis_failed'));
            await globalThis.ShowError(
                globalThis.t('dialog.error'),
                globalThis.t('error.msg.analysis_failed_dynamic', err)
            );
        } finally {
            this._running = false;
        }
    }

    /**
     * 從 shared shell 的 #mpManifestPath / #mpDataFolder / #mpSubject 抽
     * ctx。Subject 不對稱(idx vs name)由 ManifestPanel own — 兩種形狀同時
     * 給,caller 各取所需,不擴 backend RPC signature(ADR-0007 §4)。
     *
     * `subjects` 為預留欄位(目前 always []);M3/M4 spec author 如需 subjects
     * array,自 `app._<adapter>Subjects` 取(_gatherCtx 不主動 populate)。
     */
    _gatherCtx() {
        const manifestPath = document.getElementById('mpManifestPath')?.value || '';
        const dataFolder = document.getElementById('mpDataFolder')?.value || '';
        const select = document.getElementById('mpSubject');
        // parseInt('SubjectA') === NaN — CCI/PhaseSync/Normalized/MuscleRatio
        // 把 idx 寫進 option.value,subjectIdx 自動 valid;Composer 把 subject
        // string 寫進 option.value,subjectName 走 select.value。兩種形狀
        // 同時 expose,spec 各取所需。
        const subjectIdx = select ? parseInt(select.value, 10) : NaN;
        const subjectName = select?.options?.[select.selectedIndex]?.textContent
            || select?.value
            || '';
        return {
            manifestPath,
            dataFolder,
            subjectIdx,
            subjectName,
            subjects: [], // reserved, currently always [];spec 端如需自 app._<adapter>Subjects 取
        };
    }

    /**
     * Render panel shell HTML — 包 header(titleKey + back button)+ 共通的
     * manifest / dataFolder / subject 三個 input + spec.formBody 注入 +
     * run button + 結果區。
     *
     * `spec.hideSubject === true` 時省略 subject `<select>` 區塊(MuscleRatio
     * 只吃 manifest + dataFolder,ADR-0007 §2 入口定義 — 無 subject)。此時
     * _gatherCtx 的 `#mpSubject` lookup 回 null → subjectIdx=NaN / subjectName=''
     *(spec.rpc 不讀這兩欄即可)。其他 4 panel 省略 hideSubject 走 default。
     *
     * `formBody` 為 builder function 而非 string(ADR-0007 §6 + reversibility:
     * locale change 時 frozen string 無法 retranslate,builder fn 走 onLocaleChange
     * 自然 re-render)。
     *
     * 沿用 main.js innerHTML + tHtml convention — 不引入 DOMPurify / DOM builder。
     * Trust boundary:iframe `sandbox=allow-scripts`(處理 backend HTML);panel
     * template 本身只 i18n key + 靜態結構,無 user input 注入。
     *
     * 共用 shared ids(`mpManifestPath` / `mpDataFolder` / `mpSubject` /
     * `mpRunBtn` / `mpResult`)— M5 wiring 會新增 `app.selectMpManifest()` 等
     * 共用 helper 對齊。在 M1 階段,wiring 的 onclick 暫先用 `app.xxx()`
     * placeholder,M5 補真實實作。
     */
    _renderShell(spec) {
        const tH = globalThis.tHtml;
        return `
            <div class="panel-header">
                <h2>${tH(spec.titleKey)}</h2>
                <button class="btn-back" onclick="app.showMainMenu()">${tH('button.back')}</button>
            </div>

            <div class="form-group">
                <label>1. ${tH('form.label.manifest')}</label>
                <div class="input-group drop-zone" data-drop-target="mpManifestPath" style="--wails-drop-target: drop;">
                    <input type="text" id="mpManifestPath" class="form-control" placeholder="${tH('form.placeholder.manifest')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMpManifest()">${tH('button.browse')}</button>
                </div>
            </div>

            <div class="form-group">
                <label>2. ${tH('form.label.data_folder')}</label>
                <div class="input-group">
                    <input type="text" id="mpDataFolder" class="form-control" placeholder="${tH('form.placeholder.data_folder')}" readonly>
                    <button class="btn btn-secondary" onclick="app.selectMpDataFolder()">${tH('button.browse')}</button>
                </div>
            </div>

            ${spec.hideSubject ? '' : `
            <div class="form-group">
                <label>3. ${tH('form.label.subject')}</label>
                <select id="mpSubject" class="form-control" disabled onchange="app.onMpSubjectChange()">
                    <option value="">${tH('form.placeholder.load_manifest_first')}</option>
                </select>
            </div>`}

            ${spec.formBody(tH)}

            <div class="button-group">
                <button id="mpRunBtn" class="btn btn-primary">${tH(spec.runBtnLabelKey || 'button.start_analyze')}</button>
                <button class="btn btn-secondary" onclick="app.showMainMenu()">${tH('button.back')}</button>
            </div>

            <div id="mpResult" class="result-section" style="display:none;">
                <h3>${tH('result.section.title')}</h3>
                <div id="mpResultContent"></div>
            </div>
        `;
    }

    // ==================== helpers ====================

    /**
     * Attach iframe element 到 containerId,套 sandbox=allow-scripts、srcdoc、
     * height,並回 `{ iframe, ready }`。`ready` 為 iframe `load` event resolve
     * 的 Promise — 跟 P2-10 既有 convention 對齊({once: true} 避免 leak)。
     *
     * **不**包 bridge.subscribe / send / requestReply(ADR-0003 §reversibility
     * + ADR-0007 §8):那會在 bridge 上加 third layer。closure 直接呼:
     *
     *   const { iframe, ready } = mp.attachIframe({ containerId, html, height });
     *   await ready;  // 必須 await,iframe-side listener 此刻才掛好
     *   mp.registerCleanup(bridge.subscribe(iframe, 'cci-chart-*', handler));
     *   mp.bindPhaseCheckboxes({ iframe, bridge, ... }); // 內部會 emitUpdate(),
     *                                                     // 沒 await ready 會 silent drop
     *   bridge.send(iframe, 'cci-update-phase-markers', payload);
     *
     * @param {object} opts
     * @param {string} opts.containerId - wrapper element id(已存在於 DOM)
     * @param {string} opts.html - iframe srcdoc 內容(由 backend sanitize)
     * @param {string} [opts.height='620px'] - iframe.style.height
     * @returns {{ iframe: HTMLIFrameElement, ready: Promise<void> }}
     */
    attachIframe({ containerId, html, height = '620px' }) {
        const wrapper = document.getElementById(containerId);
        if (!wrapper) {
            throw new Error(`ManifestPanel.attachIframe: container #${containerId} not found`);
        }

        const iframe = document.createElement('iframe');
        iframe.style.width = '100%';
        iframe.style.height = height;
        iframe.style.border = 'none';
        // ADR-0003 + P1-12:sandbox=allow-scripts(無 allow-same-origin)。
        // Go 端 sanitize 是唯一防線;跨 frame DOM access 在 wails dev opaque-
        // origin 下 silent fail(見 memory/feedback_wails_sandbox_iframe_crossframe)。
        iframe.sandbox = 'allow-scripts';
        iframe.scrolling = 'no'; // 避免雙重 scroll(Composer 1300px iframe 經驗)
        iframe.srcdoc = html;
        wrapper.appendChild(iframe);

        const ready = new Promise((resolve) => {
            // {once: true}:對齊 P2-10 既有 guard,避免多次觸發或 leak
            iframe.addEventListener('load', () => resolve(), { once: true });
        });

        return { iframe, ready };
    }

    /**
     * 渲染 phase checkbox group 到 `containerId`,phase 名走 phaseOrder
     * whitelist(P0/P1/P2/S/C/D/T0/T/O/L,從現有 CCI/Composer code 共用提取),
     * checkbox change 時 recalc pct + bridge.send `<adapter>-update-phase-markers`。
     *
     * **Panel state 自管**:跨 generate 持久的 checked Set(Composer
     * `_composerCheckedPhases`)由 caller 自己掛 app this — 此 helper 不持有
     * Set,只在每次 render 時:
     *   - 若 `checkedSet` 為空或未傳:預設全勾並寫回 set
     *   - 否則:已存在於 set 內的 phase 勾選
     * 對齊 ADR-0007 §9「panel state 留 app this」。
     *
     * @precondition iframe 必須先 await attachIframe().ready,否則首次 emitUpdate()
     *   的 bridge.send() postMessage 會 silent drop(iframe-side listener 尚未掛載)。
     *   後續 user 點 checkbox 才會正常 send,但首次 render 的同步 update 會丟失,
     *   結果就是 chart 載入時沒帶到 phase markers。
     *
     * @param {object} opts
     * @param {object} opts.phaseTimes - { P0: number, P1: number, ... } phase 名 → 時間
     * @param {string} opts.adapter - bridge message prefix(e.g. 'cci' / 'composer')
     * @param {string} opts.containerId - phase checkbox 容器 id(已存在於 DOM)
     * @param {string} [opts.idPrefix] - checkbox id 前綴(default: `${adapter}_phase_`)
     * @param {object} [opts.bridge] - iframeBridge instance(由 caller 注入,避免本 module 直接 import)
     * @param {HTMLIFrameElement} [opts.iframe] - bridge.send 對象 iframe
     * @param {Function} [opts.recalcPercents] - phaseMarkers.recalcPercents helper(由 caller 注入)
     * @param {Set<string>} [opts.checkedSet] - 跨 re-render 持久的勾選 Set(由 caller 提供)
     * @param {(pcts: object, checkedPhases: string[]) => void} [opts.onUpdate]
     *   — Optional callback,在 bridge.send 完成後 invoke。CCI 用來刷新 pct 文字
     *   顯示(`app._updatePhasePositionDisplay(pcts)`),Composer 不需要省略即可。
     *   傳入兩個參數:`pcts` 為 phase 名 → 百分比的 map(recalcPercents 結果),
     *   `checkedPhases` 為當前勾選的 phase 名 array(已過濾 phaseTimes 內存在的)。
     *   首次 render 完的 emitUpdate 也會 invoke,讓 caller 能初始化 UI。
     */
    bindPhaseCheckboxes({
        phaseTimes,
        adapter,
        containerId,
        idPrefix,
        bridge,
        iframe,
        recalcPercents,
        checkedSet,
        onUpdate,
    }) {
        const container = document.getElementById(containerId);
        if (!container) return;
        container.textContent = '';

        const prefix = idPrefix || `${adapter}_phase_`;
        // phaseOrder whitelist 從 CCI(main.js:1415)+ Composer(main.js:2001)
        // 共用提取 — 兩處硬編相同,ADR-0007 §1 要求收乾到單點。
        const phaseOrder = ['P0', 'P1', 'P2', 'S', 'C', 'D', 'T0', 'T', 'O', 'L'];
        const available = phaseOrder.filter((p) => phaseTimes[p] !== undefined);

        const useDefaultAllChecked = !checkedSet || checkedSet.size === 0;

        // 把每次 change 的處理收成 single function — 對齊 CCI updateCCIPhaseLines /
        // Composer _updateComposerPhaseLines 既有形狀。
        const emitUpdate = () => {
            const checkedNames = Array.from(
                container.querySelectorAll(`[id^="${prefix}"]:checked`)
            ).map((cb) => cb.value);
            const checkedPhases = checkedNames
                .filter((n) => phaseTimes[n] !== undefined)
                .map((n) => ({ name: n, time: phaseTimes[n] }));
            const pcts = recalcPercents(checkedPhases);
            const payload = checkedPhases.map((p) => ({ ...p, pct: pcts[p.name] }));
            // CCI 額外送 allChecked(updateCCIPhaseLines:1576);Composer 不需要。
            // 統一 payload shape 為 { checkedPhases };caller 若需 allChecked 自
            // 算(用 checkedPhases.length === availableLength)— ADR-0003 §6
            // payload shape symmetric。
            bridge.send(iframe, `${adapter}-update-phase-markers`, { checkedPhases: payload });
            // onUpdate(M3 prep,CCI-driven):bridge.send 完成後通知 caller。
            // CCI 用來呼 `app._updatePhasePositionDisplay(pcts)` 刷新 pct 文字面板;
            // Composer 不傳 onUpdate 等同無 op。`?.` 守 undefined 不 throw,
            // 並把 checkedPhases 攤平成 string array 對 caller 友善
            //(不需要再從 payload 物件 array map 出 name)。
            onUpdate?.(pcts, checkedPhases.map((p) => p.name));
        };

        available.forEach((p) => {
            const item = document.createElement('div');
            item.className = 'checkbox-item';
            const cb = document.createElement('input');
            cb.type = 'checkbox';
            cb.id = prefix + p;
            cb.value = p;
            cb.checked = useDefaultAllChecked || checkedSet.has(p);
            if (cb.checked && checkedSet) {
                checkedSet.add(p);
            }
            cb.addEventListener('change', () => {
                if (checkedSet) {
                    if (cb.checked) checkedSet.add(p);
                    else checkedSet.delete(p);
                }
                emitUpdate();
            });
            const label = document.createElement('label');
            label.setAttribute('for', cb.id);
            label.textContent = p; // whitelisted 字面字串,非 user input
            item.appendChild(cb);
            item.appendChild(label);
            container.appendChild(item);
        });

        // 首次 render 後立即發一次 update — 對齊 CCI _onComposerIframeLoaded
        // 既有「載完就重畫一次」behaviour。
        emitUpdate();
    }

    /**
     * 委派到 app.openOutputFolder() — 共用「開啟輸出資料夾」按鈕的 onClick。
     * 5 panel 結果區共用,helper 提供 stable reference 給 onResult closure。
     */
    openOutputFolder() {
        return this._app.openOutputFolder();
    }

    // ==================== shared input pickers(M5 wiring)====================
    //
    // shell 的 onclick="app.selectMpManifest()" / "app.selectMpDataFolder()" /
    // onchange="app.onMpSubjectChange()" 由 EMGAnalysisApp 上的 thin delegator
    // forward 到此(維持 `app.xxx()` convention)。3 method 收乾既有 5 panel 各自
    // 重寫的 selectXxxManifest / selectXxxDataFolder + Composer onComposerSubjectChange。

    /**
     * 選 manifest 檔 → 寫 `#mpManifestPath` → 若 spec.loadSubjects 且非 hideSubject,
     * load subject 列表填 `#mpSubject`(subject 兩形態 idx/name 由 valueMode 決定,
     * ADR-0007 §4)。對齊既有 selectCCIManifest(main.js:1279)+ selectComposerManifest
     *(:1694)pattern,但 subject load 改走 spec 注入的 loadSubjects。
     *
     * SelectFile 失敗 / load 失敗 → ShowError(對齊既有 catch 行為)。user cancel
     *(file 為 falsy)→ no-op 不動 input。
     */
    async selectMpManifest() {
        const spec = this._currentSpec;
        try {
            const filters = [{ displayName: 'CSV Files (*.csv)', pattern: '*.csv' }];
            // SelectFile title 沿用既有 hardcoded 字串(OS-native dialog title,非
            // panel HTML — 不進 CJK-leak 測試面;對齊 selectCCIManifest:1285)。
            const file = await SelectFile('選擇分期總檔案', filters, 'open');
            if (!file) return; // user cancel
            const input = document.getElementById('mpManifestPath');
            if (input) input.value = file;

            // hideSubject(MuscleRatio)無 subject select,不 load。其他 panel 若
            // spec 帶 loadSubjects 則 load。
            if (spec?.loadSubjects && !spec.hideSubject) {
                await this._loadMpSubjects();
            }
        } catch (err) {
            // eslint-disable-next-line no-console
            console.error('選擇分期總檔案失敗:', err);
            await globalThis.ShowError(globalThis.t('dialog.error'), String(err));
        }
    }

    /**
     * 選資料夾 → 寫 `#mpDataFolder`。Composer 的 loadSubjects 需要 dataFolder
     *(LoadChartComposerSubjects 帶 dataFolder),故寫完 dataFolder 後,若
     * spec.loadSubjects 且 manifest 已選,re-load subjects(對齊既有
     * selectComposerDataFolder:1710「dataFolder 改了且 manifest 在 → reload
     * subjects」behaviour)。index-mode 的 3 panel loadSubjects 不讀 dataFolder,
     * re-load 結果等價(冪等),統一走同一條路不分流。
     */
    async selectMpDataFolder() {
        const spec = this._currentSpec;
        try {
            const folder = await SelectDirectory('選擇數據資料夾');
            if (!folder) return; // user cancel
            const input = document.getElementById('mpDataFolder');
            if (input) input.value = folder;

            const manifest = document.getElementById('mpManifestPath')?.value || '';
            if (spec?.loadSubjects && !spec.hideSubject && manifest) {
                await this._loadMpSubjects();
            }
        } catch (err) {
            // eslint-disable-next-line no-console
            console.error('選擇資料夾失敗:', err);
            await globalThis.ShowError(globalThis.t('dialog.error'), String(err));
        }
    }

    /**
     * `#mpSubject` change delegator → spec.onSubjectChange?.(this)。Composer 用來
     * reset state + 清 chart(app.onComposerSubjectChange);其他 panel 省略 → no-op。
     */
    onMpSubjectChange() {
        this._currentSpec?.onSubjectChange?.(this);
    }

    /**
     * 內部 helper:呼 spec.loadSubjects(manifestPath, dataFolder) 取 subject 列表 +
     * valueMode,填 `#mpSubject` option 並 enable。**Subject 兩形態(ADR-0007 §4)**:
     *   - valueMode='index':option.value = 0-based 索引(CCI/PhaseSync/Normalized),
     *     _gatherCtx 的 parseInt(value) → subjectIdx valid。
     *   - valueMode='name':option.value = subject string(Composer,ADR-0002
     *     canonical-key),_gatherCtx 的 subjectName 走 select.value / option text。
     *
     * loadSubjects throw(例如 Composer LoadChartComposerSubjects success=false)→
     * 往外冒給 caller(selectMpManifest / selectMpDataFolder)的 catch 統一 ShowError。
     */
    async _loadMpSubjects() {
        const manifestPath = document.getElementById('mpManifestPath')?.value || '';
        const dataFolder = document.getElementById('mpDataFolder')?.value || '';
        const { subjects, valueMode } = await this._currentSpec.loadSubjects(manifestPath, dataFolder);

        const select = document.getElementById('mpSubject');
        if (!select) return;
        select.innerHTML = '';
        const placeholder = document.createElement('option');
        placeholder.value = '';
        placeholder.textContent = globalThis.tHtml('form.option.select_subject');
        select.appendChild(placeholder);

        (subjects || []).forEach((subject, index) => {
            const opt = document.createElement('option');
            // index-mode 寫 0-based 索引;name-mode 寫 subject 字串。subject text 一律
            // 顯示 subject 名(textContent 防 XSS — subject 來自 manifest CSV)。
            opt.value = valueMode === 'name' ? subject : String(index);
            opt.textContent = subject;
            select.appendChild(opt);
        });
        select.disabled = false;
        this._app.updateStatus(globalThis.t('status.subjects_loaded', (subjects || []).length));
    }

    /**
     * 註冊 cleanup fn — 下次 `_runEnvelope` 開頭自動 call 並清空。
     *
     * 典型用法(取代既有 `this._cciBridgeUnsub?.()` ad-hoc pattern):
     *   const unsub = bridge.subscribe(iframe, 'cci-chart-*', handler);
     *   mp.registerCleanup(unsub);
     *
     * 為何「下次 run 開頭 flush」而非「本次 run 結束 flush」:
     *   - 結果 render 後的 bridge.subscribe 必須存活到「user 在 result 區互動
     *     完、又跑下一次分析」之前;若本次 run 結束就 flush,result 區 phase
     *     checkbox click 不會觸發 subscriber。
     *   - 下次 run 開頭 flush 等同「re-attach iframe 前清舊 subscription」,
     *     符合既有 `this._cciBridgeUnsub?.()` 在 `showCCIResult` 開頭呼叫的時機。
     */
    registerCleanup(fn) {
        if (typeof fn === 'function') {
            this._cleanups.push(fn);
        }
    }

    _flushCleanups() {
        const pending = this._cleanups;
        this._cleanups = [];
        for (const fn of pending) {
            try {
                fn();
            } catch (e) {
                // cleanup 不應 throw,但即使 throw 也要繼續 flush 其他 cleanup
                // 與本 envelope 流程 — log + swallow,對齊 onLocaleChange catch。
                // eslint-disable-next-line no-console
                console.error('[ManifestPanel] cleanup threw:', e);
            }
        }
    }
}
