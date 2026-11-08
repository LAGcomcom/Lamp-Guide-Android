// util.js —— h() 与图标集。图标用内联 SVG（Fluent 线条风格，16×16 网格）。

export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k === 'text') el.textContent = v;
      else if (k === 'html') el.innerHTML = v;
      else if (k === 'dataset') Object.assign(el.dataset, v);
      else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
      else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
      else el.setAttribute(k, v === true ? '' : String(v));
    }
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

// 统一 16×16、stroke 1.4、round join —— 与 WinUI 小尺寸图标的视觉重量对齐
const P = {
  home: '<path d="M2.5 6.6 8 2.4l5.5 4.2V13a1 1 0 0 1-1 1H9.9v-3.6H6.1V14H3.5a1 1 0 0 1-1-1Z"/>',
  scale: '<path d="M8 1.6v12.8M1.6 8h12.8"/><path d="M8 1.6 5.4 4.2M8 1.6l2.6 2.6M8 14.4l-2.6-2.6M8 14.4l2.6-2.6M1.6 8l2.6-2.6M1.6 8l2.6 2.6M14.4 8l-2.6-2.6M14.4 8l-2.6 2.6"/>',
  profile: '<path d="M3 13.4c0-2.4 2.2-3.9 5-3.9s5 1.5 5 3.9"/><circle cx="8" cy="5.2" r="3"/>',
  add: '<path d="M8 3v10M3 8h10"/>',
  info: '<circle cx="8" cy="8" r="6.4"/><path d="M8 7.3v4.1M8 5.1h.01"/>',
  settings: '<circle cx="8" cy="8" r="2.2"/><path d="M8 1.7v1.7M8 12.6v1.7M14.3 8h-1.7M3.4 8H1.7M12.5 3.5l-1.2 1.2M4.7 11.3l-1.2 1.2M12.5 12.5l-1.2-1.2M4.7 4.7 3.5 3.5"/>',
  sun: '<circle cx="8" cy="8" r="3"/><path d="M8 1v1.4M8 13.6V15M1 8h1.4M13.6 8H15M3.1 3.1l1 1M11.9 11.9l1 1M3.1 12.9l1-1M11.9 4.1l1-1"/>',
  moon: '<path d="M13.8 9.4A6.2 6.2 0 0 1 6.6 2.2 6.2 6.2 0 1 0 13.8 9.4Z"/>',
  expand: '<path d="M9.4 2.5h4.1v4.1M6.6 13.5H2.5V9.4M13.5 2.5 9.1 6.9M2.5 13.5l4.4-4.4"/>',
  fullscreen: '<path d="M9.4 2.5h4.1v4.1M6.6 13.5H2.5V9.4M13.5 2.5 9.1 6.9M2.5 13.5l4.4-4.4"/>',
  window: '<rect x="2.5" y="3" width="11" height="10" rx="1.2"/><path d="M2.5 6.1h11"/>',
  timer: '<circle cx="8" cy="8.6" r="5.4"/><path d="M8 5.6v3.2l2 1.3M6.2 1.8h3.6"/>',
  sync: '<path d="M2.6 8a5.4 5.4 0 0 1 9.2-3.8M13.4 8a5.4 5.4 0 0 1-9.2 3.8"/><path d="M11.8 1.4v2.8H9M4.2 14.6v-2.8H7"/>',
  eye: '<path d="M1.7 8S4.1 3.8 8 3.8 14.3 8 14.3 8 11.9 12.2 8 12.2 1.7 8 1.7 8Z"/><circle cx="8" cy="8" r="2"/>',
  folder: '<path d="M2 5.2A1.2 1.2 0 0 1 3.2 4h2.6l1.4 1.7h5.6A1.2 1.2 0 0 1 14 6.9v5.4a1.2 1.2 0 0 1-1.2 1.2H3.2A1.2 1.2 0 0 1 2 12.3Z"/>',
  edit: '<path d="M11.2 2.6l2.2 2.2-8 8-2.9.7.7-2.9Z"/>',
  pencil: '<path d="M11.2 2.6l2.2 2.2-8 8-2.9.7.7-2.9Z"/>',
  chevronDown: '<path d="M3.6 6.2 8 10.6l4.4-4.4"/>',
  chevronUp: '<path d="M3.6 9.8 8 5.4l4.4 4.4"/>',
  chevronLeft: '<path d="M9.8 3.6 5.4 8l4.4 4.4"/>',
  chevronRight: '<path d="M6.2 3.6 10.6 8 6.2 12.4"/>',
  more: '<circle cx="3.2" cy="8" r="1.1"/><circle cx="8" cy="8" r="1.1"/><circle cx="12.8" cy="8" r="1.1"/>',
  delete: '<path d="M3 4.6h10M6.2 4.6V3.2h3.6v1.4M4.6 4.6l.7 8a1 1 0 0 0 1 1h3.4a1 1 0 0 0 1-1l.7-8"/>',
  trash: '<path d="M3 4.6h10M6.2 4.6V3.2h3.6v1.4M4.6 4.6l.7 8a1 1 0 0 0 1 1h3.4a1 1 0 0 0 1-1l.7-8"/>',
  up: '<path d="M8 13V3.4M4.2 7.2 8 3.4l3.8 3.8"/>',
  down: '<path d="M8 3v9.6M4.2 8.8 8 12.6l3.8-3.8"/>',
  restart: '<path d="M13.2 8a5.2 5.2 0 1 1-1.6-3.8"/><path d="M13.4 2.2v2.8h-2.8"/>',
  check: '<path d="M3 8.6l3.3 3.3L13 4.4"/>',
  warning: '<path d="M8 2.4 1.7 13.2h12.6Z"/><path d="M8 6.4v3.4M8 11.4h.01"/>',
  error: '<circle cx="8" cy="8" r="6.4"/><path d="M5.5 5.5l5 5M10.5 5.5l-5 5"/>',
  github: '<path d="M5.4 14.3c-2.6.7-2.6-1.5-3.4-1.9m6.8 3.3v-2.2a1.9 1.9 0 0 0-.5-1.5c1.7-.2 3.2-.8 3.2-3.7a2.9 2.9 0 0 0-.8-2 2.7 2.7 0 0 0-.1-2s-.8-.2-2.4.9a8.2 8.2 0 0 0-4.2 0C6.4 4.1 5.6 4.3 5.6 4.3a2.7 2.7 0 0 0-.1 2 2.9 2.9 0 0 0-.8 2c0 2.9 1.5 3.5 3.2 3.7a1.9 1.9 0 0 0-.5 1.5v2.2"/>',
  link: '<path d="M6.8 9.2a2.6 2.6 0 0 0 3.7 0l2-2a2.6 2.6 0 0 0-3.7-3.7l-.9.9"/><path d="M9.2 6.8a2.6 2.6 0 0 0-3.7 0l-2 2a2.6 2.6 0 0 0 3.7 3.7l.9-.9"/>',
  download: '<path d="M8 2.4v7.6M4.8 6.8 8 10l3.2-3.2M2.6 12.2v.6a1 1 0 0 0 1 1h8.8a1 1 0 0 0 1-1v-.6"/>',
  close: '<path d="M3 3l10 10M13 3 3 13"/>',
  minimize: '<path d="M2.6 8h10.8"/>',
  maximize: '<rect x="2.8" y="2.8" width="10.4" height="10.4" rx="1"/>',
  restore: '<rect x="2.8" y="4.8" width="8.4" height="8.4" rx="1"/><path d="M5.6 4.8V3.4a1 1 0 0 1 1-1h6.2a1 1 0 0 1 1 1v6.2a1 1 0 0 1-1 1h-1.4"/>',
  search: '<circle cx="7.2" cy="7.2" r="4.4"/><path d="M10.5 10.5 14 14"/>',
  server: '<rect x="2.2" y="2.6" width="11.6" height="4.2" rx="1.1"/><rect x="2.2" y="9.2" width="11.6" height="4.2" rx="1.1"/><path d="M4.6 4.7h.01M4.6 11.3h.01"/>',
  layers: '<path d="M8 2.1 2.4 5.2 8 8.3l5.6-3.1Z"/><path d="M2.4 8.6 8 11.7l5.6-3.1"/><path d="M2.4 11.4 8 14.5l5.6-3.1"/>',
  globe: '<circle cx="8" cy="8" r="6.2"/><path d="M1.9 8h12.2"/><path d="M8 1.8c1.9 1.7 2.8 3.9 2.8 6.2S9.9 12.5 8 14.2C6.1 12.5 5.2 10.3 5.2 8S6.1 3.5 8 1.8Z"/>',
  terminal: '<rect x="2" y="3" width="12" height="10" rx="1.2"/><path d="M4.8 6.6 6.9 8.3 4.8 10M8.4 10.4h2.8"/>',
  /* ---- 设置与日志用的细分图标 ----
     设置页每一张卡都在说一件不同的事（预算、路由、来源、通知、日志），
     用同一套通用图标会让十一张卡看起来像同一张。这里按"这件事本身"补一套
     同笔画重量的 16px 线稿，和上面那批混在一起用。 */
  tokens: '<path d="M8 2.2c2.6 0 4.6 1.3 4.6 3.4S10.6 9 8 9 3.4 7.7 3.4 5.6 5.4 2.2 8 2.2Z"/><path d="M3.4 5.6v4.6c0 2.1 2 3.4 4.6 3.4s4.6-1.3 4.6-3.4V5.6"/><path d="M3.4 8.2c0 2.1 2 3.4 4.6 3.4s4.6-1.3 4.6-3.4"/>',
  levels: '<path d="M2.4 8.6v3.2c0 .7.6 1.3 1.3 1.3h2.4c.7 0 1.3-.6 1.3-1.3v-3.2"/><path d="M6.3 6.4v5.4c0 .7.6 1.3 1.3 1.3h2.4c.7 0 1.3-.6 1.3-1.3V6.4"/><path d="M10.2 3v8.8c0 .7.6 1.3 1.3 1.3h.5c.7 0 1.3-.6 1.3-1.3V3c0-.5-.4-.9-.9-.9h-1.3c-.5 0-.9.4-.9.9Z"/><circle cx="4.6" cy="7" r="1.3"/>',
  radar: '<circle cx="8" cy="8" r="6.2"/><path d="M8 8 12.6 4.2"/><path d="M8 8a2 2 0 1 1-2 2"/>',
  shuffle: '<path d="M2.6 4.2h2.2c.9 0 1.7.4 2.2 1.1l3 4.2c.5.7 1.3 1.1 2.2 1.1h2.2"/><path d="M2.6 11.4h2.2c.9 0 1.7-.4 2.2-1.1l1-1.4"/><path d="M8.6 5.3l1-1.4c.5-.7 1.3-1.1 2.2-1.1h2.2"/><path d="M11.4 2.2 13.6 4.2 11.4 6.2M11.4 9.6 13.6 11.6 11.4 13.6"/>',
  repeat: '<path d="M2.6 6.2a3 3 0 0 1 3-2.6h7.2"/><path d="M11.2 2.2 13.6 3.6 11.2 5"/><path d="M13.4 9.8a3 3 0 0 1-3 2.6H3.2"/><path d="M4.8 11 2.4 12.4 4.8 13.8"/>',
  route: '<circle cx="3.4" cy="3.6" r="1.6"/><circle cx="3.4" cy="12.4" r="1.6"/><circle cx="12.6" cy="8" r="1.6"/><path d="M3.4 5.2v5.6"/><path d="M5 3.6h3.2a2 2 0 0 1 2 2v.6"/><path d="M5 12.4h3.2a2 2 0 0 0 2-2v-.6"/>',
  key: '<circle cx="5.2" cy="9.4" r="2.6"/><path d="M7.1 7.5 13.2 1.8M11.2 3.4l1.6 1.6M12.6 2l1.6 1.6"/>',
  sparkle: '<path d="M7.2 2.2l1.2 3.1 3.1 1.2-3.1 1.2-1.2 3.1-1.2-3.1L3 5.3l3.1-1.2Z"/><path d="M12.4 10l.7 1.6 1.6.7-1.6.7-.7 1.6-.7-1.6-1.6-.7 1.6-.7Z"/>',
  database: '<ellipse cx="8" cy="4" rx="5" ry="2"/><path d="M3 4v4c0 1.1 2.2 2 5 2s5-.9 5-2V4"/><path d="M3 8v4c0 1.1 2.2 2 5 2s5-.9 5-2v-4"/>',
  megaphone: '<path d="M2.6 6.4h2l6.2-3.1a.7.7 0 0 1 1 .6v7.2a.7.7 0 0 1-1 .6L4.6 9.6H2.6a1.2 1.2 0 0 1-1.2-1.2v-.8a1.2 1.2 0 0 1 1.2-1.2Z"/><path d="M5.4 9.8v2.6a1.4 1.4 0 0 0 2.8 0v-1.7"/>',
  chart: '<path d="M2.6 2.6v10.2a.6.6 0 0 0 .6.6h10.2"/><rect x="4.6" y="8.6" width="2" height="3.4"/><rect x="8" y="6" width="2" height="6"/><rect x="11.4" y="3.6" width="2" height="8.4"/>',
  bell: '<path d="M4.2 7.2a3.8 3.8 0 0 1 7.6 0c0 3 1.2 4 1.2 4.2H3c0-.2 1.2-1.2 1.2-4.2Z"/><path d="M6.5 11.6a1.5 1.5 0 0 0 3 0"/>',
  cloud: '<path d="M4.6 11.6h8a2.4 2.4 0 0 0 .4-4.8 3.6 3.6 0 0 0-7 .9 2.2 2.2 0 0 0-1.4 3.9Z"/>',
  file: '<path d="M3.6 2.4h5.2l3.6 3.4v7.8a.8.8 0 0 1-.8.8H3.6a.8.8 0 0 1-.8-.8V3.2a.8.8 0 0 1 .8-.8Z"/><path d="M8.6 2.6v3.4h3.4"/><path d="M5.4 8.6h5.2M5.4 10.8h3.4"/>',
  shield: '<path d="M8 2.2 3.2 4v4.2c0 2.7 2 4.6 4.8 5.6 2.8-1 4.8-2.9 4.8-5.6V4Z"/><path d="M5.8 8.2 7.4 9.8 10.6 6.6"/>',
  play: '<circle cx="8" cy="8" r="6.2"/><path d="M6.4 5.4l4 2.6-4 2.6Z"/>',
  list: '<path d="M5.4 4.2h8.2M5.4 8h8.2M5.4 11.8h8.2"/><circle cx="2.8" cy="4.2" r=".9"/><circle cx="2.8" cy="8" r=".9"/><circle cx="2.8" cy="11.8" r=".9"/>',
  calendar: '<rect x="2.4" y="3.4" width="11.2" height="10" rx="1.2"/><path d="M2.4 6.6h11.2M5.2 2v2.4M10.8 2v2.4"/><path d="M5.2 9.2h1.6M9.2 9.2h1.6M5.2 11.4h1.6"/>',
  channel: '<path d="M2.4 4.6a1.4 1.4 0 0 1 1.4-1.4h2.8l1.4 1.8h4.6a1.4 1.4 0 0 1 1.4 1.4v5a1.4 1.4 0 0 1-1.4 1.4H3.8a1.4 1.4 0 0 1-1.4-1.4Z"/><path d="M8 7.4v4M6 9.4h4"/>',
};

export function icon(name, size) {
  const path = P[name] || P.info;
  const svg = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"
    stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;
  const el = h('span', { class: 'icon', html: svg });
  if (size) { el.style.width = size + 'px'; el.style.height = size + 'px'; el.style.display = 'grid'; el.style.placeItems = 'center'; }
  return el;
}

export const iconMarkup = (name) => `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">${P[name] || P.info}</svg>`;

export function debounce(fn, ms) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

export function clamp(v, lo, hi) { return Math.min(hi, Math.max(lo, v)); }

// WinUI 的 RepositionThemeTransition 等价物：等量位移、无交错、无起飞延迟。
// 用 FLIP：记录旧位置 → 立即换布局 → 从旧位置补间到新位置。
export function reposition(el, mutate) {
  const before = new Map();
  for (const c of el.children) before.set(c, c.getBoundingClientRect().top);
  mutate();
  for (const c of el.children) {
    const oldTop = before.get(c);
    if (oldTop == null) continue;
    const newTop = c.getBoundingClientRect().top;
    const dy = oldTop - newTop;
    if (Math.abs(dy) < 1) continue;
    c.animate(
      [{ transform: `translateY(${dy}px)` }, { transform: 'translateY(0)' }],
      { duration: 250, easing: 'cubic-bezier(0.167, 0.167, 0, 1)' }
    );
  }
}
