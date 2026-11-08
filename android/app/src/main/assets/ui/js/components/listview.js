/* ==========================================================================
   components/listview.js —— 可排序列表

   只写结构：颜色、圆角、间距由 styles/controls.css 提供（.lv / .lv-item /
   .lv-item.is-selected / .lv-item.is-dragging / .lv-drop-line / .lv-empty）。
   条目间距 2px 由 .lv 的 --gap-2 负责；选中态是左侧 3px 强调竖条 + 背景提亮，
   由 .lv-item.is-selected::before 负责。

   ListView({ items, renderItem, draggable, onReorder, onSelect, selectedIndex })
     -> HTMLElement（附带 get/setItems、get/setSelectedIndex、focusFirst）
   ========================================================================== */

import { h, icon } from '../util.js';

export function ListView({
  items = [],
  renderItem = null,
  draggable = false,
  onReorder = null,
  onSelect = null,
  selectedIndex = -1
} = {}) {
  let data = Array.isArray(items) ? items.slice() : [];
  let selected = Number.isInteger(selectedIndex) && selectedIndex >= 0 ? selectedIndex : -1;
  let dragFrom = -1;
  let dropGap = -1;

  const root = h('div', { class: 'lv', role: 'listbox', tabindex: '0' });
  const line = h('div', { class: 'lv-drop-line', 'aria-hidden': 'true' });

  /* ---------------- 渲染 ---------------- */

  function itemNodes() {
    return Array.from(root.children).filter((c) => c.classList && c.classList.contains('lv-item'));
  }

  function syncSelection() {
    for (const node of itemNodes()) {
      const idx = Number(node.getAttribute('data-index'));
      const on = idx === selected;
      node.classList.toggle('is-selected', on);
      node.setAttribute('aria-selected', on ? 'true' : 'false');
      if (on) node.setAttribute('data-selected', '1'); else node.removeAttribute('data-selected');
    }
  }

  function buildItem(item, index) {
    const node = h('div', {
      class: 'lv-item',
      role: 'option',
      'aria-selected': 'false',
      'aria-posinset': String(index + 1),
      'aria-setsize': String(data.length),
      'data-index': String(index)
    });

    const body = renderItem ? renderItem(item, index) : item;
    if (body == null || body === false) {
      node.append(h('span', { class: 'lv-item-text', text: String(item == null ? '' : item) }));
    } else if (body instanceof Node) {
      node.append(body);
    } else {
      node.append(h('span', { class: 'lv-item-text', text: String(body) }));
    }

    const handle = draggable ? h('span', { class: 'lv-grip', 'aria-hidden': 'true' }, icon('more')) : null;
    if (handle) node.append(handle);

    node.addEventListener('click', (e) => {
      if (e.target.closest && e.target.closest('.lv-grip')) return;
      selectAt(index, true);
    });

    if (draggable) {
      node.draggable = true;
      node.addEventListener('dragstart', (e) => {
        dragFrom = index;
        node.classList.add('is-dragging');
        try {
          e.dataTransfer.effectAllowed = 'move';
          e.dataTransfer.setData('text/plain', String(index));
        } catch (err) { /* 个别环境禁止写 drag 数据 */ }
      });
      node.addEventListener('dragend', () => {
        node.classList.remove('is-dragging');
        dragFrom = -1;
        hideLine();
      });
    }
    return node;
  }

  function render() {
    /* 重建前先清空：拖拽重排、setItems 都会走到这里 */
    line.remove();
    while (root.firstChild) root.removeChild(root.firstChild);
    if (!data.length) {
      root.classList.add('is-empty');
      root.append(h('div', { class: 'lv-empty', text: '暂无内容' }));
    } else {
      root.classList.remove('is-empty');
      for (let i = 0; i < data.length; i += 1) root.append(buildItem(data[i], i));
    }
    syncSelection();
  }

  /* ---------------- 拖拽重排 ---------------- */

  function hideLine() {
    dropGap = -1;
    if (line.parentNode) line.parentNode.removeChild(line);
  }

  function showLineAt(gap) {
    dropGap = gap;
    const nodes = itemNodes();
    /* 指示线插在第 gap 个条目之前；gap === 条目数时插在末尾 */
    if (gap >= nodes.length) root.append(line);
    else root.insertBefore(line, nodes[gap]);
  }

  function gapAt(clientY) {
    const nodes = itemNodes();
    for (let i = 0; i < nodes.length; i += 1) {
      const r = nodes[i].getBoundingClientRect();
      if (clientY < r.top + r.height / 2) return i;
    }
    return nodes.length;
  }

  function onDragOver(e) {
    if (!draggable || dragFrom < 0) return;
    e.preventDefault();
    try { e.dataTransfer.dropEffect = 'move'; } catch (err) { /* 忽略 */ }
    const gap = gapAt(e.clientY);
    if (gap !== dropGap) showLineAt(gap);
  }

  function onDragLeave(e) {
    if (e.relatedTarget && root.contains(e.relatedTarget)) return;
    hideLine();
  }

  function onDrop(e) {
    if (!draggable || dragFrom < 0) return;
    e.preventDefault();
    const gap = dropGap >= 0 ? dropGap : gapAt(e.clientY);
    const from = dragFrom;
    dragFrom = -1;
    hideLine();

    /* 指示线停在「第 gap 个之前」，移动 from 之后目标下标要相应回退一位 */
    const to = gap > from ? gap - 1 : gap;
    const clamped = Math.max(0, Math.min(data.length - 1, to));
    if (clamped === from) return;

    const moved = data.slice();
    const [picked] = moved.splice(from, 1);
    moved.splice(clamped, 0, picked);
    data = moved;

    if (selected === from) selected = clamped;
    else if (selected > from && selected <= clamped) selected -= 1;
    else if (selected < from && selected >= clamped) selected += 1;

    render();
    if (onReorder) onReorder(from, clamped);
  }

  /* ---------------- 键盘 ---------------- */

  function selectAt(index, notify) {
    if (index < 0 || index >= data.length) return;
    const changed = selected !== index;
    selected = index;
    syncSelection();
    if (notify && onSelect && changed) onSelect(index, data[index]);
  }

  /* 方向键只移动选中，不触发 onSelect；Enter 才确认 */
  function move(delta) {
    if (!data.length) return;
    const next = selected < 0
      ? (delta > 0 ? 0 : data.length - 1)
      : Math.max(0, Math.min(data.length - 1, selected + delta));
    selectAt(next, false);
  }

  root.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); move(1); return; }
    if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); return; }
    if (e.key === 'Enter') {
      e.preventDefault();
      if (selected < 0 && data.length) { selectAt(0, false); }
      if (onSelect && selected >= 0) onSelect(selected, data[selected]);
    }
  });

  root.addEventListener('dragover', onDragOver);
  root.addEventListener('dragleave', onDragLeave);
  root.addEventListener('drop', onDrop);

  /* ---------------- 对外 API ---------------- */

  root.getItems = () => data.slice();
  root.setItems = (next) => {
    data = Array.isArray(next) ? next.slice() : [];
    if (selected >= data.length) selected = data.length ? data.length - 1 : -1;
    hideLine();
    render();
  };
  root.getSelectedIndex = () => selected;
  root.setSelectedIndex = (index) => {
    selected = Number.isInteger(index) && index >= 0 && index < data.length ? index : -1;
    syncSelection();
  };
  root.focusFirst = () => {
    try { root.focus({ preventScroll: true }); } catch (err) { try { root.focus(); } catch (e2) { /* 忽略 */ } }
  };

  render();
  return root;
}
