/* ==========================================================================
   bridge.js —— 页面与 Go 宿主的唯一通道。

   协议（与 internal/shell 的 Bridge 一致）：
     页面 → 宿主：window.chrome.webview.postMessage(JSON.stringify({id, method, payload}))
     宿主 → 页面：window.__hostReceive(JSON.stringify({id, ok, ...}))
   每个请求一个自增 id，超时 30s 后 reject，防止界面永久挂起。

   宿主额外推送的消息（没有 id）走 events：__hostPush({type, ...})
   ========================================================================== */

let seq = 0;
const pending = new Map();
const listeners = new Map();

const TIMEOUT = 30000;

function post(message) {
  const chrome = window.chrome;
  if (!chrome || !chrome.webview) return false;
  chrome.webview.postMessage(JSON.stringify(message));
  return true;
}

/* 宿主回填入口。壳在 Eval 里调用它，所以必须挂在全局且早于任何模块。 */
window.__hostReceive = function (raw) {
  let msg;
  try {
    msg = JSON.parse(raw);
  } catch (err) {
    console.error('[bridge] 宿主回包不是合法 JSON：', raw);
    return;
  }
  if (msg && msg.id != null && pending.has(msg.id)) {
    const entry = pending.get(msg.id);
    pending.delete(msg.id);
    clearTimeout(entry.timer);
    entry.resolve(msg);
    return;
  }
  if (msg && msg.type) emit(msg.type, msg);
};

/* 宿主主动推送：系统主题切换、窗口状态变化。 */
window.__hostPush = window.__hostReceive;

window.__hostTheme = function (dark) {
  emit('host-theme', { dark: !!dark });
};

function emit(type, payload) {
  const set = listeners.get(type);
  if (!set) return;
  for (const fn of set) {
    try { fn(payload); } catch (err) { console.error('[bridge] 事件处理失败：', err); }
  }
}

export function on(type, fn) {
  let set = listeners.get(type);
  if (!set) { set = new Set(); listeners.set(type, set); }
  set.add(fn);
  return () => set.delete(fn);
}

/* 一次请求。没连上宿主时直接 reject，调用方据此显示「未能与后台通信」。 */
export function call(method, payload) {
  return new Promise((resolve, reject) => {
    if (!post({ id: ++seq, method, payload })) {
      reject(new Error('宿主通道不可用'));
      return;
    }
    const id = seq;
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error('宿主响应超时'));
    }, TIMEOUT);
    pending.set(id, { resolve, timer });
  });
}

/* 宿主存活判定：壳注入 window.chrome.webview 后才是 true。 */
export function available() {
  return !!(window.chrome && window.chrome.webview);
}
