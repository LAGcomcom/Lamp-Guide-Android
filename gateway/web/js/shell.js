/* ==========================================================================
   shell.js —— 应用外壳：标题栏 / 左侧导航 / 页面宿主。

   这个程序只有一件事：内嵌的 zen 网关。左侧导航直接列出网关的每一个功能面
   （服务 / 模型 / 供应商 / 联网 / Agent / 日志 / 设置），每一面是一个独立页面，
   页内不再有分面标签——切面就是切页，选中态在侧栏上。

   缩放设置那几页不在这个程序里：窗口只服务网关，没有第二套设置。
   ========================================================================== */

import { h, icon } from './util.js';
import { call, on } from './bridge.js';

import createGateway, { FACES } from './pages/gateway.js';

/* 每一面在侧栏上的图标。面和图标一起改，菜单就不会和页面对不上。 */
const FACE_ICON = {
  service: 'server',
  models: 'layers',
  aggregate: 'route',
  channels: 'key',
  opencode: 'terminal',
  providers: 'profile',
  network: 'globe',
  agents: 'window',
  logs: 'terminal',
  settings: 'settings'
};

const PAGES = {};
for (const f of FACES) {
  PAGES['gw:' + f.key] = {
    label: f.label,
    icon: FACE_ICON[f.key] || 'info',
    factory: (ctx) => createGateway(ctx, { face: f.key })
  };
}

const NAV_KEYS = FACES.map((f) => 'gw:' + f.key);
const COMPACT_BREAKPOINT = 920;

/* ---------------- 全局状态 ---------------- */

const ctx = {
  store: { themeName: document.documentElement.dataset.themePreference || 'System' },
  call(method, payload) {
    return call(method, payload);
  },
  /* 页面内部的跳转：Agent 面要能把用户送到聚合面去改那个开关。没有它，
     那句「去聚合面」只能是一句说明。 */
  navigate(key) { navigate(key); }
};

let currentKey = null;
let currentPage = null;
let compact = false;

/* ---------------- 标题栏 ---------------- */

const titlebar = h('div', { class: 'titlebar' });
const drag = h('div', { class: 'titlebar-drag' });
const tbLogo = h('span', { class: 'tb-logo' }, h('img', { src: './assets/lamp-guide.svg', alt: '', width: '20', height: '20' }));
const tbTitle = h('span', { class: 'tb-title', text: 'Lamp Guide' });

const btnMin = h('button', { class: 'tb-btn', type: 'button', 'aria-label': '最小化' }, icon('minimize'));
const btnMax = h('button', { class: 'tb-btn', type: 'button', 'aria-label': '最大化' }, icon('maximize'));
const btnClose = h('button', { class: 'tb-btn is-close', type: 'button', 'aria-label': '关闭' }, icon('close'));
const btnTheme = h('button', { class: 'tb-btn tb-theme', type: 'button' });
let themeTarget = document.documentElement.dataset.theme || 'light';
let themeTransition = null;
let themeRevision = 0;

function applyTheme(theme, animate = false) {
  const root = document.documentElement;
  const revision = ++themeRevision;
  themeTarget = theme;
  if (themeTransition) themeTransition.skipTransition();
  themeTransition = null;

  const update = () => {
    // 连点时，已跳过的旧动画仍可能执行回调，只提交最后一次选择。
    if (revision !== themeRevision) return;
    root.dataset.theme = theme;
    const dark = theme === 'dark';
    const label = dark ? '切换到日间模式' : '切换到夜间模式';
    btnTheme.replaceChildren(icon(dark ? 'sun' : 'moon'));
    btnTheme.title = label;
    btnTheme.setAttribute('aria-label', label);
    btnTheme.setAttribute('aria-pressed', String(dark));
  };
  const reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!animate || reduceMotion || !document.startViewTransition || root.dataset.theme === theme) {
    root.classList.remove('theme-transitioning');
    update();
    return;
  }

  // 使用整页快照淡入；不重建页面，也不逐一给几百张模型卡片添加动画。
  root.classList.add('theme-transitioning');
  try {
    const transition = document.startViewTransition(update);
    themeTransition = transition;
    transition.ready.catch(() => {}); // 窗口隐藏或快速连点时，浏览器可跳过动画。
    transition.finished.catch(() => {}).then(() => {
      if (themeTransition !== transition) return;
      themeTransition = null;
      root.classList.remove('theme-transitioning');
    });
  } catch (_) {
    root.classList.remove('theme-transitioning');
    update();
  }
}

btnTheme.addEventListener('click', () => {
  const theme = themeTarget === 'dark' ? 'light' : 'dark';
  ctx.store.themeName = theme;
  document.documentElement.dataset.themePreference = theme;
  try { localStorage.setItem('zen-ui-theme', theme); } catch (_) {}
  applyTheme(theme, true);
});
applyTheme(document.documentElement.dataset.theme || 'light');

btnMin.addEventListener('click', () => host('Window.Minimize'));
btnMax.addEventListener('click', () => host('Window.ToggleMaximize'));
btnClose.addEventListener('click', () => host('Window.Close'));

titlebar.append(drag, tbLogo, tbTitle, h('div', { class: 'tb-buttons' }, btnTheme, btnMin, btnMax, btnClose));

/* 拖动：由页面**发起**系统的移动循环。
   CSS 的 -webkit-app-region 是 Electron 的约定，WebView2 不实现；原生
   WM_NCHITTEST 也收不到 —— WebView2 的宿主子窗口铺满整个客户区，对每个点
   都回 HTCLIENT，而且它跑在另一个进程里，HTTRANSPARENT 无法把消息透传下去。
   鼠标消息本身仍会送进页面（双击能最大化就是证据），所以在这里调
   Window.StartDrag，让 Windows 自己跑那段带 Aero Snap 的模态移动循环。
   只在左键按下时发起：右键与中键拖标题栏在原生窗口里没有这个语义。 */
titlebar.addEventListener('mousedown', (e) => {
  if (e.button !== 0) return;
  if (e.target.closest('.tb-btn')) return;
  e.preventDefault();
  host('Window.StartDrag');
});

/* 缩放热区：窗口四边与四角各铺一条透明窄带，交给 Window.StartResize。
   同一个原因 —— WebView2 盖住了 DWM 画出来的那条边框，系统自带的缩放
   命中区永远接不到鼠标。窄带用 fixed 定位，不参与页面布局。 */
const EDGES = [
  ['top', 'inset: 0 8px auto 8px; height: 4px; cursor: ns-resize'],
  ['bottom', 'inset: auto 8px 0 8px; height: 4px; cursor: ns-resize'],
  ['left', 'inset: 8px auto 8px 0; width: 4px; cursor: ew-resize'],
  ['right', 'inset: 8px 0 8px auto; width: 4px; cursor: ew-resize'],
  ['top-left', 'inset: 0 auto auto 0; width: 8px; height: 8px; cursor: nwse-resize'],
  ['top-right', 'inset: 0 0 auto auto; width: 8px; height: 8px; cursor: nesw-resize'],
  ['bottom-left', 'inset: auto auto 0 0; width: 8px; height: 8px; cursor: nesw-resize'],
  ['bottom-right', 'inset: auto 0 0 auto; width: 8px; height: 8px; cursor: nwse-resize']
];
for (const [edge, css] of EDGES) {
  const grip = h('div', { class: 'resize-grip', style: css });
  grip.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    host('Window.StartResize', { edge });
  });
  document.body.append(grip);
}

/* 双击标题栏空白区：等价于最大化切换 */
titlebar.addEventListener('dblclick', (e) => {
  if (e.target.closest('.tb-btn')) return;
  host('Window.ToggleMaximize');
});

function host(method, payload) {
  return call(method, payload == null ? null : payload)
    .catch(() => { /* 宿主未就绪时静默 */ });
}

/* 把标题栏的可拖拽带与按钮占位同步给原生层：WM_NCHITTEST 据此返回 HTCAPTION，
   拖动、双击最大化、Aero Snap 全部由系统实现。 */
function syncHitRegions() {
  const band = 38;
  const buttons = 46 * titlebar.querySelectorAll('.tb-btn').length;
  drag.style.right = buttons + 'px';
  call('Window.SetHitRegions', { titleBarHeight: band, leftInset: 0, rightInset: buttons })
    .catch(() => { /* 非宿主环境下忽略 */ });
}

/* ---------------- 导航 ---------------- */

const pane = h('div', { class: 'pane' });

/* 状态卡：网关是否在跑 + Base URL。数据来自网关自己的 /admin/api/state，
   不编造一个"看起来在跑"的绿灯。 */
const statusDot = h('span', { class: 'dot' });
const statusText = h('span', { text: '读取中…' });
const statusSub = h('div', { class: 'row2', text: '' });
const statusCard = h('div', { class: 'status-card' },
  h('div', { class: 'row1' }, statusDot, statusText),
  statusSub
);

const paneScroll = h('div', { class: 'pane-scroll' });
pane.append(statusCard, paneScroll);

const navItems = new Map();

function addNavItem(navKey, opts) {
  const meta = PAGES[navKey];
  const item = h('div', { class: 'nav-item', role: 'button', tabindex: '0', dataset: { key: navKey } },
    h('span', { class: 'nav-icon' }, icon(opts.icon || meta.icon)),
    h('span', { class: 'nav-label', text: opts.label || meta.label })
  );
  const activate = () => navigate(navKey);
  item.addEventListener('click', activate);
  item.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); activate(); }
  });
  navItems.set(navKey, item);
  return item;
}

function buildPane() {
  for (const key of NAV_KEYS) paneScroll.append(addNavItem(key, {}));

  // 收起/展开：贴在侧栏底部（对应 NavigationView 的 PaneFooter）
  const collapseBtn = h('button', { class: 'collapse-btn', type: 'button', 'aria-label': '收起侧栏' },
    h('span', { class: 'nav-icon' }, icon('chevronLeft')),
    h('span', { class: 'lbl', text: '收起侧栏' })
  );
  collapseBtn.addEventListener('click', () => toggleSide());
  pane.append(collapseBtn);

  const foot = h('div', { class: 'side-foot', text: 'Lamp Guide' });
  foot.id = 'side-foot';
  foot.title = '';
  pane.append(foot);
}

/* 收起/展开侧栏：切 is-collapsed，宽度由 CSS 过渡；按钮文案与图标随之换向 */
let sideCollapsed = false;
function toggleSide() {
  sideCollapsed = !sideCollapsed;
  pane.classList.toggle('is-collapsed', sideCollapsed);
  titlebar.classList.toggle('is-compact', sideCollapsed || compact);
  const btn = pane.querySelector('.collapse-btn');
  if (!btn) return;
  const lbl = btn.querySelector('.lbl');
  if (lbl) lbl.textContent = sideCollapsed ? '展开侧栏' : '收起侧栏';
  btn.setAttribute('aria-label', sideCollapsed ? '展开侧栏' : '收起侧栏');
  const ic = btn.querySelector('.nav-icon');
  if (ic) ic.replaceWith(h('span', { class: 'nav-icon' }, icon(sideCollapsed ? 'chevronRight' : 'chevronLeft')));
}

/* updateStatusCard 把网关的真实 state 画到侧栏：运行状态 + Base URL + 车道数。
   拿不到就如实说连不上，不假装在跑。 */
function updateStatusCard(st, reason) {
  const up = !!(st && typeof st === 'object');
  statusDot.classList.toggle('off', !up);
  statusText.textContent = up ? '网关运行中' : '网关未启动';

  if (!up) {
    statusSub.textContent = reason || '未能与网关通信';
    const foot = document.querySelector('#side-foot');
    if (foot) { foot.textContent = 'Lamp Guide'; foot.title = ''; }
    return;
  }

  const base = String(st.baseURL || '');
  const models = Array.isArray(st.models) ? st.models : [];
  const avail = models.filter((m) => m && m.state === 'available').length;
  statusSub.textContent = (base || '没有 Base URL')
    + (models.length ? '  ·  ' + avail + '/' + models.length + ' 路可用' : '');

  const foot = document.querySelector('#side-foot');
  if (foot) {
    const dir = String(st.dataDir || '');
    foot.textContent = 'Lamp Guide';
    foot.title = dir;
  }
}

async function refreshStatus() {
  let res;
  try {
    res = await call('Gateway', { path: 'state' });
  } catch (err) {
    updateStatusCard(null, String((err && err.message) || err));
    return false;
  }
  if (!res || res.ok !== true) {
    updateStatusCard(null, String((res && res.error) || '网关没有响应'));
    return false;
  }
  updateStatusCard(res.data || {});
  return true;
}

/* markNav 把选中态放到指定的导航项上。 */
function markNav(navKey) {
  for (const [k, node] of navItems) {
    node.classList.toggle('is-selected', k === navKey);
  }
}

/* ---------------- 内容区 ---------------- */

const contentWrap = h('div', { class: 'content-wrap' });
const content = h('div', { class: 'content' });
contentWrap.append(content);

const shell = h('div', { class: 'shell' }, pane, contentWrap);
const app = h('div', { id: 'app' }, titlebar, shell);

/* ---------------- 页面装载 ---------------- */

function navigate(key) {
  loadPage(key, true);
}

function loadPage(key, force = false) {
  if (!force && key === currentKey && currentPage) return;
  const meta = PAGES[key];
  if (!meta) return;

  if (currentPage && typeof currentPage.onUnmount === 'function') {
    try { currentPage.onUnmount(); } catch (err) { console.error(err); }
  }
  content.replaceChildren();

  currentKey = key;
  markNav(key);

  let page;
  try {
    page = meta.factory(ctx);
  } catch (err) {
    console.error('[shell] 页面构建失败：', key, err);
    content.append(h('div', { class: 'page' }, h('div', { class: 'page-head' }, h('h1', { text: '页面加载失败' }))));
    return;
  }
  currentPage = page;
  content.append(page.node);
  page.node.classList.add('page-enter');

  if (typeof page.onMount === 'function') {
    Promise.resolve()
      .then(() => page.onMount())
      .catch((err) => console.error('[shell] onMount 失败：', key, err));
  }

  content.scrollTop = 0;
}

/* ---------------- 紧凑态 ---------------- */

function applyWidth() {
  const width = shell.clientWidth;
  const next = width < COMPACT_BREAKPOINT;
  if (next === compact) return;
  compact = next;
  pane.classList.toggle('is-compact', compact);
  titlebar.classList.toggle('is-compact', compact || sideCollapsed);
  content.classList.toggle('is-narrow', shell.clientWidth < 590);
}

/* ---------------- 启动 ---------------- */

/* mark 只在页面自己量时有用：冷启动到底慢在抓取、解析还是第一次网关往返，
   外部时钟答不了，performance 时间线答得了。 */
function mark(name) {
  try { performance.mark(name); } catch (err) { /* 老内核忽略 */ }
}

async function boot() {
  mark('boot');
  document.body.classList.remove('no-anim');
  buildPane();
  mark('pane');
  document.querySelector('#app').replaceWith(app);

  applyWidth();
  syncHitRegions();

  window.addEventListener('resize', () => {
    applyWidth();
    syncHitRegions();
  });

  on('host-theme', ({ dark }) => {
    const name = String(ctx.store.themeName || 'System');
    if (name.toLowerCase() !== 'system') return;
    applyTheme(dark ? 'dark' : 'light');
  });

  const systemTheme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)');
  if (systemTheme) systemTheme.addEventListener('change', ({ matches }) => {
    if (String(ctx.store.themeName).toLowerCase() === 'system') applyTheme(matches ? 'dark' : 'light');
  });

  /* 先把第一页装上去：侧栏状态卡要等一次网关往返，网关刚起时那一次可能很慢
     （探测、出口、目录都还没落库）。如果先 await 它，用户在这几秒里点了导航，
     结果会被后面的 loadPage(NAV_KEYS[0]) 直接覆盖——看着像点击没生效。 */
  mark('nav');
  loadPage(NAV_KEYS[0]);

  /* 侧栏状态卡跟着网关走：启动、探测、出口变化都在这同一条 state 上。 */
  refreshStatus();
  setInterval(() => { refreshStatus(); }, 15000);
}

boot();
