/* ==========================================================================
   components/flyout.js —— 浮层 / 菜单 / 对话框

   只写结构与几何：颜色、圆角、阴影、字号由 styles/controls.css + tokens.css
   提供（.flyout / .menu / .menu-item / .menu-sep / .mi-icon / .dialog /
   .dialog-scrim / .dialog-title）。本文件不写死任何颜色值。

   几何由这里决定（position:fixed + left/top），出现动画 150ms
   cubic-bezier(0,0,0,1)：透明度 0→1，位移 2px→0；关闭立即移除节点。
   ========================================================================== */

import { h, icon } from '../util.js';
import { Button } from './core.js';

const EDGE = 8;        /* 浮层与视口边缘的最小距离 */
const GAP = 4;         /* 浮层与 anchor 之间的缝隙 */

function rectOf(anchor) {
  if (!anchor) return { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };
  if (typeof anchor.getBoundingClientRect === 'function') return anchor.getBoundingClientRect();
  const left = Number(anchor.left) || 0;
  const top = Number(anchor.top) || 0;
  const width = Number(anchor.width) || 0;
  const height = Number(anchor.height) || 0;
  return { left, top, width, height, right: left + width, bottom: top + height };
}

function fadeIn(node) {
  if (!node || typeof node.animate !== 'function') return;
  try {
    node.animate(
      [
        { opacity: 0, transform: 'translateY(2px)' },
        { opacity: 1, transform: 'translateY(0)' }
      ],
      { duration: 150, easing: 'cubic-bezier(0,0,0,1)', fill: 'both' }
    );
  } catch (err) {
    /* 没有 Web Animations 能力时由 CSS 的 flyoutIn 兜底 */
  }
}

/* ---------------------------------------------------------------------------
   Flyout({ anchor, content, placement = 'bottom' }) -> { open, close, toggle, node }

   anchor 可为元素或返回元素的函数；content 可为元素或元素数组。
   placement：'bottom'（贴 anchor 下沿、左对齐）/ 'right'（贴右侧）。
   超出视口自动翻转：下→上、右→左。
   ------------------------------------------------------------------------- */
export function Flyout({ anchor = null, content = null, placement = 'bottom' } = {}) {
  const node = h('div', { class: 'flyout' });
  node.hidden = true;
  node.tabIndex = -1;
  node.setAttribute('data-placement', placement);

  const items = Array.isArray(content) ? content : [content];
  for (const it of items) if (it) node.append(it);

  let isOpen = false;
  let previousFocus = null;

  const getAnchor = () => (typeof anchor === 'function' ? anchor() : anchor);

  function place() {
    const r = rectOf(getAnchor());
    const vw = window.innerWidth || 0;
    const vh = window.innerHeight || 0;
    const w = node.offsetWidth || 0;
    const hgt = node.offsetHeight || 0;

    let left = r.left;
    let top = r.bottom + GAP;
    let side = 'bottom';

    if (placement === 'right' || placement === 'left') {
      side = placement;
      left = side === 'right' ? r.right + GAP : r.left - GAP - w;
      top = r.top;
      if (side === 'right' && vw && left + w > vw - EDGE) {
        left = r.left - GAP - w;
        side = 'left';
      } else if (side === 'left' && left < EDGE) {
        left = r.right + GAP;
        side = 'right';
      }
      if (vh && top + hgt > vh - EDGE) top = vh - EDGE - hgt;
      if (top < EDGE) top = EDGE;
    } else if (placement === 'top') {
      side = 'top';
      top = r.top - GAP - hgt;
      if (vh && top < EDGE) {
        side = 'bottom';
        top = r.bottom + GAP;
      }
    } else {
      side = 'bottom';
      top = r.bottom + GAP;
      if (vh && top + hgt > vh - EDGE) {
        const flipped = r.top - GAP - hgt;
        if (flipped >= EDGE) {
          side = 'top';
          top = flipped;
        }
      }
    }

    /* 水平方向兜底：不允许越出视口 */
    if (vw && left + w > vw - EDGE) left = vw - EDGE - w;
    if (left < EDGE) left = EDGE;
    /* 垂直方向兜底：高于视口时也不丢出去 */
    if (vh && top + hgt > vh - EDGE) top = Math.max(EDGE, vh - EDGE - hgt);

    node.style.position = 'fixed';
    node.style.left = Math.max(0, Math.round(left)) + 'px';
    node.style.top = Math.max(0, Math.round(top)) + 'px';
    node.setAttribute('data-placement', side);
  }

  function focusInside() {
    const target = node.querySelector(
      'button:not([disabled]), [href], input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'
    ) || node;
    try { target.focus({ preventScroll: true }); } catch (err) { try { target.focus(); } catch (e2) { /* 忽略 */ } }
  }

  function onPointerDown(e) {
    if (node.contains(e.target)) return;
    const a = getAnchor();
    /* 点 anchor 本身不算「外部」，由 onAnchorClick 负责收起 */
    if (a && a.nodeType === 1 && a.contains(e.target)) return;
    close({ restoreFocus: false });
  }

  /*
   * 打开期间再点 anchor：先在捕获阶段收起并吃掉这次 click。
   * 这样即使调用方只写 onClick: () => fly.open()（没有 toggle 判断），
   * 也能靠第二次点击收起，不会卡在只能点外部或 Esc 的状态。
   */
  function onAnchorClick(e) {
    if (!isOpen) return;
    e.preventDefault();
    e.stopPropagation();
    close();
  }

  function onKeyDown(e) {
    if (e.key !== 'Escape' && e.key !== 'Esc') return;
    if (!isOpen) return;
    e.preventDefault();
    e.stopPropagation();
    close();
  }

  function onResize() {
    close({ restoreFocus: false });
  }

  function open() {
    if (isOpen) return;
    isOpen = true;
    previousFocus = document.activeElement;
    node.hidden = false;
    node.style.visibility = 'hidden';
    document.body.append(node);
    place();
    node.style.visibility = '';
    fadeIn(node);
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('keydown', onKeyDown, true);
    window.addEventListener('resize', onResize, true);
    focusInside();
    const a = getAnchor();
    if (a && a.setAttribute) a.setAttribute('aria-expanded', 'true');
    if (a && a.nodeType === 1 && a.addEventListener) a.addEventListener('click', onAnchorClick, true);
  }

  function close(options) {
    if (!isOpen) return;
    isOpen = false;
    const restore = !(options && options.restoreFocus === false);
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('resize', onResize, true);
    const focusWasInside = node.contains(document.activeElement);
    node.remove();
    const a = getAnchor();
    if (a && a.nodeType === 1 && a.removeEventListener) a.removeEventListener('click', onAnchorClick, true);
    if (a && a.setAttribute) a.setAttribute('aria-expanded', 'false');
    if (restore && focusWasInside) {
      if (a && typeof a.focus === 'function') {
        try { a.focus({ preventScroll: true }); } catch (err) { try { a.focus(); } catch (e2) { /* 忽略 */ } }
      } else if (previousFocus && typeof previousFocus.focus === 'function') {
        try { previousFocus.focus({ preventScroll: true }); } catch (err) { /* 焦点目标已失效 */ }
      }
    }
    previousFocus = null;
  }

  return {
    node,
    open,
    close,
    toggle() { if (isOpen) close(); else open(); }
  };
}

/* ---------------------------------------------------------------------------
   Menu({ items }) -> HTMLElement

   item 结构：
     { label, icon, shortcut, danger, disabled, checked, className, onClick }
   '-' / 'divider' / { type: 'divider' } 渲染分隔线 .menu-sep。
   列表项用 .menu-item（hover/active 用 SubtleFill 语义，由 controls.css 提供），
   danger 加 .is-danger，当前项加 .is-checked。
   ------------------------------------------------------------------------- */
export function Menu({ items = [] } = {}) {
  const root = h('div', { class: 'menu', role: 'menu' });
  for (const item of (Array.isArray(items) ? items : [])) {
    if (!item || item === '-' || item === 'divider' || item.type === 'divider' || item.separator) {
      root.append(h('div', { class: 'menu-sep', role: 'separator' }));
      continue;
    }

    const cls = ['menu-item'];
    if (item.danger) cls.push('is-danger');
    if (item.disabled) cls.push('is-disabled');
    if (item.checked) cls.push('is-checked');
    if (item.className) {
      for (const extra of String(item.className).split(' ')) {
        if (extra && !cls.includes(extra)) cls.push(extra);
      }
    }

    const row = h('button', {
      class: cls.join(' '),
      type: 'button',
      role: item.checked ? 'menuitemcheckbox' : 'menuitem',
      'aria-checked': item.checked ? 'true' : null,
      'aria-disabled': item.disabled ? 'true' : null,
      disabled: item.disabled || undefined
    });

    const iconSlot = h('span', { class: 'mi-icon' });
    const graphic = item.icon != null ? item.icon : (item.checked ? 'check' : null);
    if (graphic != null) iconSlot.append(typeof graphic === 'string' ? icon(graphic) : graphic);
    row.append(iconSlot);

    row.append(h('span', { class: 'mi-label', text: String(item.label == null ? '' : item.label) }));

    if (item.shortcut != null) {
      row.append(h('span', { class: 'mi-shortcut', text: String(item.shortcut) }));
    }

    row.addEventListener('click', (e) => {
      if (item.disabled) { e.preventDefault(); return; }
      if (item.onClick) item.onClick(e);
    });
    root.append(row);
  }
  return root;
}

/* ---------------------------------------------------------------------------
   Dialog({ title, content, primaryLabel, onPrimary, secondaryLabel, onSecondary })
     -> { open, close, node, card }

   WinUI ContentDialog 语义：498 宽模态卡片 + 遮罩（.dialog-scrim，rgba 黑 30%
   由 tokens.css 的 --overlay 提供）。Esc 关闭；主操作默认在右侧。
   ------------------------------------------------------------------------- */
export function Dialog({
  title = '',
  content = null,
  primaryLabel = '确定',
  onPrimary = null,
  secondaryLabel = null,
  onSecondary = null
} = {}) {
  const scrim = h('div', { class: 'dialog-scrim' });
  const card = h('div', {
    class: 'dialog',
    role: 'dialog',
    'aria-modal': 'true',
    'aria-label': title || primaryLabel,
    tabindex: '-1'
  });

  if (title) card.append(h('div', { class: 'dialog-title', text: title }));

  const contentBox = h('div', { class: 'dialog-content', style: 'display:flex;flex-direction:column;gap:8px' });
  for (const it of (Array.isArray(content) ? content : [content])) if (it) contentBox.append(it);
  card.append(contentBox);

  const actions = h('div', {
    class: 'dialog-actions',
    style: 'display:flex;justify-content:flex-end;align-items:center;gap:8px;margin-top:24px'
  });
  const primary = Button({ label: primaryLabel, variant: 'accent', onClick: () => fire(onPrimary) });
  const secondary = secondaryLabel ? Button({ label: secondaryLabel, onClick: () => fire(onSecondary) }) : null;
  if (secondary) actions.append(secondary);
  actions.append(primary);
  card.append(actions);
  scrim.append(card);

  let isOpen = false;
  let previousFocus = null;

  function fire(fn) {
    const keep = fn ? fn() : undefined;
    if (keep !== false) close();
  }

  function onScrimPointer(e) {
    if (e.target === scrim) close();
  }

  function onKeyDown(e) {
    if (e.key === 'Escape' || e.key === 'Esc') {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    if (e.key !== 'Tab') return;
    const focusables = Array.from(card.querySelectorAll(
      'button:not([disabled]), [href], input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'
    ));
    if (!focusables.length) { e.preventDefault(); card.focus(); return; }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  function open() {
    if (isOpen) return;
    isOpen = true;
    previousFocus = document.activeElement;
    document.body.append(scrim);
    try { primary.focus({ preventScroll: true }); } catch (err) { /* 忽略 */ }
    document.addEventListener('keydown', onKeyDown, true);
    scrim.addEventListener('pointerdown', onScrimPointer);
  }

  function close() {
    if (!isOpen) return;
    isOpen = false;
    document.removeEventListener('keydown', onKeyDown, true);
    scrim.removeEventListener('pointerdown', onScrimPointer);
    scrim.remove();
    if (previousFocus && typeof previousFocus.focus === 'function') {
      try { previousFocus.focus({ preventScroll: true }); } catch (err) { /* 焦点目标已失效 */ }
    }
    previousFocus = null;
  }

  return { node: scrim, card, open, close, isOpen: () => isOpen };
}
