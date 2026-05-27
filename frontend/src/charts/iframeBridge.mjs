// frontend/src/charts/iframeBridge.mjs
//
// Chart iframe bridge — parent ↔ chart iframe postMessage 通訊唯一進出點(ADR-0003)。
//
// 三個 primitive:
//   - subscribe(iframe, typeOrPrefix, handler) → unsubscribe
//     iframe → parent 通知。typeOrPrefix 支援 exact(如 'composer-png-result')
//     或 wildcard `'<prefix>-*'`(如 'cci-chart-*' 同時 match
//     'cci-chart-restored' / 'cci-chart-legend-changed')。
//     handler 收 `(payload, type)`。
//   - send(iframe, type, payload)
//     parent → iframe one-way 推送。
//   - requestReply(iframe, type, payload, { timeout? }) → Promise<reply>
//     parent → iframe round-trip,含 requestId 配對 + timeout(預設 10s)。
//     iframe 必須在 reply message envelope 帶相同 requestId — 否則 timeout。
//
// 設計決定見 ADR-0003。簡言:
//   * origin policy 100% hidden:inbound `['null', window.location.origin]`,
//     outbound `'*'`(iframe sandbox 無 allow-same-origin → opaque origin,
//     targetOrigin 給具體值會不 match → iframe 端自己 verify e.origin)。
//   * uniform reply envelope `{type?, requestId, payload?, error?}`:
//     有 `error: string` 時 bridge auto-reject Promise with `new Error(error)`。
//   * dispatch 順序:requestId 有 pending → 配對 resolve/reject;否則走
//     subscribe handler dispatch(by iframe.contentWindow === e.source +
//     type 匹配)。

const state = {
    initialized: false,
    listener: null,
    // Map<iframe HTMLIFrameElement, Map<typeOrPrefix string, handler[]>>
    subscriptions: new Map(),
    // Map<requestId string, { resolve, reject, timer }>
    pendingRequests: new Map(),
};

function matchesType(actualType, typeOrPrefix) {
    if (typeOrPrefix === actualType) return true;
    if (typeOrPrefix.endsWith('-*')) {
        const prefix = typeOrPrefix.slice(0, -2);
        return actualType.startsWith(prefix + '-');
    }
    return false;
}

function handleMessage(e) {
    if (e.origin !== 'null' && e.origin !== window.location.origin) {
        return;
    }
    if (!e.data || typeof e.data !== 'object') {
        return;
    }
    const { type, requestId, payload, error } = e.data;

    if (requestId && state.pendingRequests.has(requestId)) {
        const { resolve, reject, timer } = state.pendingRequests.get(requestId);
        clearTimeout(timer);
        state.pendingRequests.delete(requestId);
        if (error) {
            reject(new Error(error));
        } else {
            resolve(payload);
        }
        return;
    }

    if (!type) return;
    for (const [iframe, byType] of state.subscriptions) {
        if (e.source !== iframe.contentWindow) continue;
        for (const [typeOrPrefix, handlers] of byType) {
            if (matchesType(type, typeOrPrefix)) {
                for (const h of handlers) {
                    h(payload, type);
                }
            }
        }
    }
}

export const bridge = {
    init() {
        if (state.initialized) return;
        state.listener = handleMessage;
        window.addEventListener('message', state.listener);
        state.initialized = true;
    },

    destroy() {
        if (state.initialized && state.listener) {
            window.removeEventListener('message', state.listener);
        }
        for (const { timer, reject } of state.pendingRequests.values()) {
            clearTimeout(timer);
            reject(new Error('bridge destroyed'));
        }
        state.pendingRequests.clear();
        state.subscriptions.clear();
        state.listener = null;
        state.initialized = false;
    },

    subscribe(iframe, typeOrPrefix, handler) {
        if (!state.subscriptions.has(iframe)) {
            state.subscriptions.set(iframe, new Map());
        }
        const byType = state.subscriptions.get(iframe);
        if (!byType.has(typeOrPrefix)) {
            byType.set(typeOrPrefix, []);
        }
        byType.get(typeOrPrefix).push(handler);
        return () => {
            const list = byType.get(typeOrPrefix);
            if (!list) return;
            const idx = list.indexOf(handler);
            if (idx >= 0) list.splice(idx, 1);
            if (list.length === 0) byType.delete(typeOrPrefix);
            if (byType.size === 0) state.subscriptions.delete(iframe);
        };
    },

    send(iframe, type, payload) {
        iframe.contentWindow.postMessage({ type, payload }, '*');
    },

    requestReply(iframe, type, payload, { timeout = 10000 } = {}) {
        return new Promise((resolve, reject) => {
            const requestId = '_rid_' + Date.now() + '_' + Math.random().toString(36).slice(2);
            const timer = setTimeout(() => {
                state.pendingRequests.delete(requestId);
                reject(new Error('bridge requestReply timeout (>' + timeout + 'ms): ' + type));
            }, timeout);
            state.pendingRequests.set(requestId, { resolve, reject, timer });
            iframe.contentWindow.postMessage({ type, requestId, payload }, '*');
        });
    },
};
