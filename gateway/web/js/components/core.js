// core.js —— 卡片 / 分组 / 按钮 / 页头 / 开关 / 下拉 / 数字框 / 滑块 / 快捷键 / InfoBar / Ring / Stack
import { h, icon, clamp, reposition } from '../util.js';
import { BrandMark } from '../brands.js';

/* ============================== PageHead ============================== */
/* 页头 = 图标 + 标题 + 可选后缀说明 + 右侧动作。
   后缀说明是新增的（WinUI 的 PageFrame 只有标题），用来放"当前是哪个配置文件"
   这类上下文，省得再占一行卡片。 */
export function PageHead({ title, sub, actions, iconName }) {
  const head = h('div', { class: 'page-head' });
  if (iconName) head.append(h('span', { class: 'head-icon' }, icon(iconName)));
  head.append(h('h1', { text: title }));
  if (sub) head.append(h('span', { class: 'head-sub', text: sub }));
  // 即使 actions 为空也建容器:调用方会往里塞动态按钮,拿 null 会崩
  const box = h('div', { class: 'head-actions' });
  if (actions && actions.length) {
    for (const a of actions) {
      box.append(Button({ icon: a.icon, label: a.label, variant: a.label ? 'standard' : 'subtle', onClick: a.onClick, square: !a.label }));
    }
  }
  head.append(box);
  head.__actions = box;
  return head;
}

/* ============================== Group ============================== */
export function Group({ title, desc }) {
  const body = h('div', { class: 'group-body' });
  const head = h('div', { class: 'group-head' });
  if (title) head.append(h('div', { class: 'group-title', text: title }));
  if (desc) head.append(h('div', { class: 'group-desc', text: desc }));
  const node = h('section', { class: 'group' }, head.childNodes.length ? head : null, body);
  return { node, body: body };
}

export function Stack({ spacing = 32, children = [] }) {
  const box = h('div', { class: 'page-body' });
  if (spacing !== 32) { box.style.gap = spacing + 'px'; }
  for (const c of children.flat()) if (c) box.append(c);
  return box;
}

/* ============================== Button ============================== */
export function Button({ label, icon: iconName, variant = 'standard', onClick, square = false, disabled = false, size = 32 }) {
  const cls = ['btn'];
  if (variant === 'accent') cls.push('v-accent');
  else if (variant === 'subtle') cls.push('v-subtle');
  else if (variant === 'critical') cls.push('v-critical');
  if (!label) cls.push('is-icon-only');
  if (size === 36) cls.push('is-square-36');
  const b = h('button', { class: cls.join(' '), type: 'button', disabled: disabled || undefined, 'aria-disabled': disabled ? 'true' : null });
  if (iconName) b.append(icon(iconName));
  if (label) b.append(h('span', { text: label }));
  b.addEventListener('click', (e) => { e.stopPropagation(); if (!disabled && onClick) onClick(e); });
  return b;
}

/* ============================== Card ============================== */
/**
 * Card({ icon, title, desc, right, expandable, expanded, onExpandChange,
 *        content, onClick, disabled })
 * 展开/收起时长严格照 SettingsExpander.Resource.xaml:173-218：
 *   进 333ms cubic-bezier(0,0,0,1)；出 167ms cubic-bezier(1,1,0,1)，
 *   出的时候 48ms 处就置不可见（"进慢出快"）。
 */
export function Card(opts) {
  const {
    icon: iconName, brand, title, desc, right, expandable = false, expanded = false,
    onExpandChange, content, onClick, disabled = false, width = 'auto',
  } = opts;

  const hasRight = !!right;
  const interactive = expandable || !!onClick;
  /* 分组是一张自适应网格，卡片默认占一格。width:'full' 用于"自己是完整一块"
     的条目——带文本框、带一排按钮、或者展开后要读长内容的那种，它们挤在
     半格里会变成一条竖线。 */
  const card = h('div', { class: 'card' + (expandable ? ' card-expander' : '') + (interactive ? ' is-interactive' : '') + (disabled ? ' is-disabled' : '') + (width === 'full' ? ' is-full' : '') });

  const headRow = expandable ? h('div', { class: 'card-head' }) : card;
  if (expandable) card.append(headRow);

  /* 品牌标优先于通用图标：Agent 与账号渠道都有自己的 mark，用通用小人图标
     只是"这里有一项"，用品牌标才是"这一项是谁"。 */
  if (brand) headRow.append(BrandMark({ id: brand }));
  else if (iconName) headRow.append(h('span', { class: 'card-icon' }, icon(iconName)));

  const main = h('div', { class: 'card-main' + (desc ? ' has-desc' : '') });
  main.append(h('div', { class: 'card-title', text: title }));
  if (desc) main.append(h('div', { class: 'card-desc', text: desc }));
  headRow.append(main);

  const rightBox = h('div', { class: 'card-right' + (hasRight ? ' is-control' : '') });
  if (hasRight) rightBox.append(...(Array.isArray(right) ? right : [right]));
  else rightBox.style.display = 'none';
  headRow.append(rightBox);

  let chev = null;
  if (expandable) {
    chev = h('span', { class: 'chev', 'aria-hidden': 'true' }, icon('chevronDown'));
    headRow.append(chev);
  } else {
    // 非展开卡片：尾部动作（右向箭头之类）由 right 自带
  }

  let body = null;
  let open = expanded;

  function setBodyVisible(v) {
    if (!body) return;
    body.style.display = v ? 'block' : 'none';
  }

  function buildBody() {
    if (body || !content) return;
    body = h('div', { class: 'card-body' });
    const items = Array.isArray(content) ? content : [content];
    for (const it of items) if (it) body.append(it);
    body.style.height = '0px';
    body.style.opacity = '0';
    card.append(body);
  }

  function expand(animate = true) {
    buildBody();
    if (!body) return;
    open = true;
    card.classList.add('is-open');
    const target = measureHeight(body);
    if (!animate) {
      body.style.height = 'auto';
      body.style.opacity = '1';
      return;
    }
    body.style.display = 'block';
    body.style.height = '0px';
    body.style.opacity = '1';
    body.style.transition = `height 333ms cubic-bezier(0,0,0,1)`;
    requestAnimationFrame(() => { body.style.height = target + 'px'; });
    const done = () => { body.style.height = 'auto'; body.style.transition = ''; body.removeEventListener('transitionend', done); };
    body.addEventListener('transitionend', done);
  }

  function collapse(animate = true) {
    if (!body) return;
    open = false;
    card.classList.remove('is-open');
    if (!animate) { setBodyVisible(false); return; }
    const from = body.getBoundingClientRect().height;
    body.style.height = from + 'px';
    body.style.transition = 'height 167ms cubic-bezier(1,1,0,1)';
    requestAnimationFrame(() => { body.style.height = '0px'; });
    // 48ms 处就切断可见性（不进不可见的扫尾动画）
    setTimeout(() => { if (!open && body) body.style.opacity = '0'; }, 48);
    setTimeout(() => {
      if (!open && body) { body.style.display = 'none'; body.style.transition = ''; body.style.height = '0px'; }
    }, 167);
  }

  function measureHeight(el) {
    const prev = el.style.display;
    el.style.display = 'block';
    el.style.height = 'auto';
    el.style.opacity = '1';
    const hgt = el.getBoundingClientRect().height;
    el.style.height = prev === 'none' ? '0px' : prev;
    return hgt;
  }

  if (expandable && content) {
    if (expanded) { buildBody(); expand(false); }
    else { buildBody(); setBodyVisible(false); }
  }

  const toggle = () => {
    if (disabled) return;
    if (open) collapse(true); else expand(true);
    onExpandChange && onExpandChange(open);
  };
  if (expandable) headRow.addEventListener('click', (e) => {
    // 点右侧控件不触发展开
    if (e.target.closest('.card-right')) return;
    toggle();
  });
  else if (onClick && !disabled) card.addEventListener('click', (e) => {
    if (e.target.closest('.card-right')) return;
    onClick(e);
  });

  card.__api = { expand, collapse, isOpen: () => open };
  return card;
}

/* ============================== Toggle ============================== */
export function Toggle({ checked = false, onChange, labelOn = '开', labelOff = '关', showLabel = false }) {
  const box = h('button', {
    class: 'toggle', type: 'button', role: 'switch',
    'aria-checked': checked ? 'true' : 'false',
    'aria-label': checked ? labelOn : labelOff,
  }, h('span', { class: 'toggle-thumb' }));
  let on = checked;
  box.addEventListener('click', (e) => {
    e.stopPropagation();
    on = !on;
    box.setAttribute('aria-checked', on ? 'true' : 'false');
    box.setAttribute('aria-label', on ? labelOn : labelOff);
    onChange && onChange(on);
  });
  return box;
}

/* ============================== Combo ============================== */
import { Flyout, Menu } from './flyout.js';

export function Combo({ options = [], value, onChange, placeholder = '选择一个窗口' }) {
  let cur = value ?? (options[0] && options[0].value);
  const label = h('span', { class: 'combo-label' });
  const btn = h('button', { class: 'combo-btn', type: 'button' }, label, h('span', { class: 'combo-chev' }, icon('chevronDown')));
  const box = h('div', { class: 'combo' }, btn);
  const sync = () => {
    const hit = options.find((o) => o.value === cur);
    label.textContent = hit ? hit.label : placeholder;
  };
  sync();
  let fly = null;
  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    if (fly && fly.node && fly.node.isConnected) { fly.close(); return; }
    const menu = Menu({
      items: options.map((o) => ({
        label: o.label, checked: o.value === cur, className: o.value === cur ? 'is-checked' : '',
        onClick: () => { cur = o.value; sync(); onChange && onChange(cur); },
      })),
    });
    fly = Flyout({ anchor: btn, content: menu, placement: 'bottom' });
    fly.open();
  });
  box.__api = { get value() { return cur; }, set(v) { cur = v; sync(); } };
  return box;
}

/* ============================== NumberBox ============================== */
export function NumberBox({ value = 0, min = -Infinity, max = Infinity, step = 1, unit, onChange, width }) {
  const input = h('input', { type: 'text', inputmode: 'decimal', value: String(value) });
  if (width) input.parentElement;
  const box = h('div', { class: 'numbox' }, input);
  if (width) box.style.minWidth = width + 'px';
  if (unit) box.append(h('span', { class: 'unit', text: unit }));
  const commit = () => {
    let n = parseFloat(input.value);
    if (!isFinite(n)) n = value;
    n = clamp(n, min, max);
    input.value = String(n);
    onChange && onChange(n);
  };
  input.addEventListener('blur', commit);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { commit(); input.blur(); }
    if (e.key === 'ArrowUp') { e.preventDefault(); input.value = String(clamp(parseFloat(input.value) + step || 0, min, max)); commit(); }
    if (e.key === 'ArrowDown') { e.preventDefault(); input.value = String(clamp(parseFloat(input.value) - step || 0, min, max)); commit(); }
  });
  box.__api = { set(v) { input.value = String(v); } };
  return box;
}

/* ============================== Slider ============================== */
export function Slider({ min = 0, max = 100, step = 1, value = 0, unit = '', onChange, digits = 2 }) {
  let cur = value;
  const fill = h('div', { class: 'slider-fill' });
  const thumb = h('div', { class: 'slider-thumb' });
  const rail = h('div', { class: 'slider-rail' }, fill);
  const range = h('input', { type: 'range', min, max, step, value: String(value), 'aria-label': '' });
  const track = h('div', { class: 'slider-track' }, rail, thumb, range);
  const valText = h('span', { class: 'slider-value' });   // 固定宽度容器，防跳动
  const box = h('div', { class: 'slider' }, track, valText);
  const pct = () => ((cur - min) / (max - min)) * 100;
  const fmt = () => {
    const n = Number.isInteger(cur) ? String(cur) : cur.toFixed(digits).replace(/0+$/, '').replace(/.$/, '');
    return n + unit;
  };
  const sync = () => {
    fill.style.width = pct() + '%';
    thumb.style.left = pct() + '%';
    range.value = String(cur);
    valText.textContent = fmt();
  };
  range.addEventListener('input', () => {
    cur = parseFloat(range.value);
    sync();
  });
  range.addEventListener('change', () => { onChange && onChange(cur); });
  sync();
  box.__api = { set(v) { cur = v; sync(); } };
  return box;
}

/* ============================== Keycap / Shortcut ============================== */
export function Keycap({ label, variant = 'default', error = false }) {
  const cls = 'keycap' + (variant === 'small' ? ' v-small' : variant === 'small-outline' ? ' v-small-outline' : '') + (error ? ' is-error' : '');
  return h('span', { class: cls, text: label });
}

export function Shortcut({ keys = [], variant = 'inline', onEdit, error = false }) {
  if (variant === 'inline') {
    const keysBox = h('div', { class: 'shortcut-keys' });
    for (const k of keys) keysBox.append(Keycap({ label: k, variant: 'default', error }));
    const box = h('div', { class: 'shortcut-inline' }, keysBox);
    if (onEdit) {
      const pen = h('button', { class: 'shortcut-edit', type: 'button', 'aria-label': '更改快捷键' }, icon('pencil'));
      pen.addEventListener('click', (e) => { e.stopPropagation(); onEdit(); });
      box.append(pen);
    }
    return box;
  }
  // default / small 形态：整块即按钮
  const caps = h('div', { class: 'shortcut-keys' });
  for (const k of keys) caps.append(Keycap({ label: k, variant: variant === 'small' ? 'small' : (variant === 'small' ? 'small' : 'default'), error }));
  const btn = h('button', { class: 'shortcut-inline', type: 'button' }, caps);
  return btn;
}

/* ============================== InfoBar ============================== */
export function InfoBar({ severity = 'info', title, message, action, onClose, iconName }) {
  const ic = iconName || (severity === 'warn' ? 'warning' : severity === 'error' ? 'error' : severity === 'success' ? 'check' : 'info');
  const box = h('div', { class: 'infobar v-' + severity, role: 'status' },
    h('span', { class: 'ib-icon' }, icon(ic)),
  );
  const main = h('div', { class: 'ib-main' }, h('div', { class: 'ib-title', text: title }));
  if (message) main.append(h('div', { class: 'ib-msg', text: message }));
  box.append(main);
  if (action) {
    const acts = h('div', { class: 'ib-actions' });
    acts.append(Button({ label: action.label, variant: 'standard', onClick: action.onClick }));
    box.append(acts);
  }
  if (onClose) {
    const x = h('button', { class: 'ib-close', type: 'button', 'aria-label': '关闭' }, icon('close'));
    x.addEventListener('click', () => { box.remove(); onClose(); });
    box.append(x);
  }
  return box;
}

/* ============================== ProgressRing ============================== */
export function ProgressRing({ size = 16, active = true, stroke = 2 }) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const svg = `<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
    <circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="currentColor"
      stroke-opacity="0.2" stroke-width="${stroke}"/>
    <circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="currentColor"
      stroke-width="${stroke}" stroke-linecap="round"
      stroke-dasharray="${c * 0.25} ${c * 0.75}"/>
  </svg>`;
  return h('span', { class: 'ring' + (active ? '' : ' is-idle'), html: svg });
}