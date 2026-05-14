// Phase 1 frontend i18n MVP — in-memory dictionary loaded from Wails backend.
//
// 設計:啟動時 initI18n(locale) 一次性從後端抓完整字典到 in-memory Map,
// 後續 t(key) 純查詢無 RPC overhead。切換語言時 changeLanguage(locale) 重抓
// 字典 + 通知 listener,由 main.js 監聽後重新呼叫當前 panel 的 show() 函式
// 來觸發 re-render(原生 JS + innerHTML 重設模式,不需 reactive framework)。
//
// 後端對應 binding (gui/app.go):
//   - GetTranslations(locale string) map[string]string
//   - SetLanguage(locale string) error
//
// 字串 fallback 行為:未載入字典或 key 缺失時,t() 回傳 key 本身(例如
// "config.panel.title"),讓開發階段一眼看出哪些字串尚未 catalog 化。

import { GetTranslations, SetLanguage } from '../wailsjs/go/gui/App.js';

const DEFAULT_LOCALE = 'zh-TW';

let currentLocale = DEFAULT_LOCALE;
let dictionary = {};
const listeners = new Set();

/**
 * 啟動時初始化:從後端載入指定 locale 的完整字典。
 * main.js init() 開頭呼叫一次,後續 t() 才有資料可查。
 * @param {string} locale - 'zh-TW' | 'zh-CN' | 'en-US' | 'ja-JP'
 */
export async function initI18n(locale) {
    currentLocale = locale || DEFAULT_LOCALE;
    dictionary = (await GetTranslations(currentLocale)) || {};
}

/**
 * 同步查詢翻譯。字典未載入或 key 缺失時 fallback 為 key 本身。
 * 支援 Go fmt.Sprintf 風格的最小子集替換(%s / %d / %v / %q)。
 * @param {string} key - i18n key (e.g. "config.panel.title")
 * @param {...any} args - 對應 verb 的替換值
 * @returns {string}
 */
export function t(key, ...args) {
    const msg = dictionary[key];
    if (msg === undefined || msg === '') {
        return key;
    }
    if (args.length === 0) {
        return msg;
    }
    let i = 0;
    return msg.replace(/%[sdvq]/g, () => {
        if (i >= args.length) {
            return '';
        }
        const v = args[i++];
        return v === undefined || v === null ? '' : String(v);
    });
}

/**
 * Escape HTML 特殊字元,給 panel.innerHTML template literal 插值前使用。
 * 翻譯字典值可能來自 user-configurable `translationsDir`(等於外部輸入),
 * 不 escape 直接插入 innerHTML 會在 Wails WebView 內形成 XSS 注入面
 * (codex review P2 fix)。純文字場合(textContent / dialog argument)
 * 仍應使用 t() 以免 double escape。
 * @param {*} s
 * @returns {string}
 */
export function escapeHtml(s) {
    if (s === null || s === undefined) {
        return '';
    }
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

/**
 * t() 後再 escape,給 panel.innerHTML template literal 使用。
 * 等同 `escapeHtml(t(key, ...args))`。
 * @param {string} key
 * @param {...any} args
 * @returns {string}
 */
export function tHtml(key, ...args) {
    return escapeHtml(t(key, ...args));
}

/**
 * 切換語言:同步後端 locale + 重抓字典 + 通知所有 listener。
 * 通常由 ConfigPanel <select id="language"> 的 onchange 呼叫。
 * 失敗時 throw,caller 應 catch 並回報使用者(避免靜默失敗)。
 * @param {string} locale
 */
export async function changeLanguage(locale) {
    await SetLanguage(locale);
    const newDict = (await GetTranslations(locale)) || {};
    currentLocale = locale;
    dictionary = newDict;
    listeners.forEach((cb) => {
        try {
            cb(locale);
        } catch (e) {
            console.error('[i18n] locale change listener threw:', e);
        }
    });
}

/**
 * 註冊 locale 變更監聽,回傳反註冊函式。
 * 典型用途:main.js 註冊「重新呼叫當前 panel 的 show() 函式」以觸發 re-render。
 * @param {(locale: string) => void} callback
 * @returns {() => void} unsubscribe
 */
export function onLocaleChange(callback) {
    listeners.add(callback);
    return () => listeners.delete(callback);
}

/**
 * 取得目前的 locale。
 * @returns {string}
 */
export function getCurrentLocale() {
    return currentLocale;
}
