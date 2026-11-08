import { PageHead, Group, Button, Toggle, Combo, Dialog, InfoBar, h } from '../components/index.js';
import { BrandMark } from '../brands.js';

const labels = { available: '请求成功', unknown: '尚未确认', throttled: '暂时限流', unavailable: '不可用', 'region-blocked': '地区受限' };
const tones = { available: 'ok', throttled: 'warn', unavailable: 'bad', 'region-blocked': 'warn', unknown: 'mute' };
const pill = (text, tone = 'mute') => h('span', { class: 'pill tone-' + tone, text });
const row = (...kids) => h('div', { class: 'oc-actions' }, kids);

function clock(ms) {
  return ms ? new Date(ms).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }) : '尚未检测';
}
function waitText(until) {
  const sec = Math.max(0, Math.ceil((until - Date.now()) / 1000));
  return sec > 60 ? Math.ceil(sec / 60) + ' 分钟后可重试' : sec + ' 秒后可重试';
}

export default function createOpenCode(ctx) {
  const root = h('div', { class: 'page oc-root' });
  const head = PageHead({ title: 'OpenCode', sub: '账号与出口', actions: [] });
  const body = h('div', { class: 'page-body oc-page' });
  root.append(head, body);
  let data = null, error = '', notice = null, mounted = false, timer = null, loading = false;
  let busy = '';
  const selected = new Map();
  const connections = new Map();
  const dialogs = new Set();

  async function api(path, payload) {
    try {
      const r = await ctx.call('Gateway', { path: 'opencode/' + path, body: payload === undefined ? null : payload });
      if (!r || !r.ok) throw new Error(r?.error || '网关没有响应');
      return r.data || {};
    } catch (e) { throw new Error(e?.message || String(e)); }
  }
  function show(severity, title, message = '') {
    notice = { severity, title, message };
    render();
  }
  async function refresh() {
    if (loading) return;
    loading = true;
    try { data = await api('state'); error = ''; }
    catch (e) { error = e.message; }
    finally { loading = false; if (mounted) render(); }
  }
  async function action(key, path, payload, success) {
    if (busy) return;
    busy = key; render();
    try {
      const result = await api(path, payload);
      if (path === 'proxy-test') {
        connections.set(key, result);
        show('success', '连接测试成功', `出口 ${result.ip}${result.country ? '（' + result.country + '）' : ''} · ${result.latencyMs} ms`);
      } else if (path === 'probe') {
        const r = result.result || {};
        show(r.state === 'available' ? 'success' : 'warning', labels[r.state] || '检测完成', r.state === 'available' ? '已通过这个账号及其代理完成真实模型请求。' : r.detail || '本次未确认可用，请查看模型状态。');
      } else { show('success', success || '已保存'); }
      await refresh();
    } catch (e) { show('error', '操作未完成', e.message); }
    finally { busy = ''; if (mounted) render(); }
  }
  function openDialog(options) {
    const dlg = Dialog(options);
    dialogs.add(dlg); dlg.open();
    return dlg;
  }
  function confirm(title, message, run, label) {
    const dlg = openDialog({ title, content: h('p', { class: 'gw-hint', text: message }), primaryLabel: label, secondaryLabel: '取消', onPrimary: run });
    dlg.card.querySelector('button')?.focus();
  }
  function addAccount(anonymous = false) {
    const nickname = h('input', { class: 'text-box', placeholder: anonymous ? '例如：备用出口' : '例如：工作账号', maxlength: '80', 'aria-label': '账号名称' });
    const token = h('input', { class: 'text-box', type: 'password', autocomplete: 'off', placeholder: '粘贴 OpenCode API Key', 'aria-label': 'OpenCode API Key' });
    const err = h('div', { class: 'oc-form-error', role: 'status' });
    const content = h('div', { class: 'oc-form' },
      h('label', {}, h('span', { text: '名称（选填）' }), nickname),
      anonymous ? h('p', { class: 'gw-hint', text: '匿名通道仅使用免费模型。多个通道共用同一出口时，可能共享上游限流。' })
        : h('label', {}, h('span', { text: 'API Key' }), token), err);
    let saving = false;
    const dlg = openDialog({ title: anonymous ? '添加匿名通道' : '添加 OpenCode 账号', content, primaryLabel: '添加', secondaryLabel: '取消', onPrimary: () => {
      if (saving) return false;
      if (!anonymous && !token.value.trim()) { err.textContent = '请填写 API Key'; return false; }
      saving = true; err.textContent = '保存中…';
      api('add', { token: anonymous ? '' : token.value.trim(), nickname: nickname.value.trim(), anonymous }).then(async () => {
        dlg.close(); token.value = ''; await refresh(); show('success', anonymous ? '匿名通道已添加' : '账号已添加');
      }).catch(e => { err.textContent = e.message; }).finally(() => { saving = false; });
      return false;
    } });
    (anonymous ? nickname : token).focus();
  }
  function proxyDialog(a) {
    let mode = a.proxyMode || 'inherit';
    const input = h('input', { class: 'text-box', type: 'password', autocomplete: 'off', placeholder: a.proxySaved ? '已保存；留空保留原地址，填写可替换' : 'http://127.0.0.1:7897', 'aria-label': '代理地址' });
    const status = h('div', { class: 'gw-hint', role: 'status' });
    const custom = h('div', { class: 'oc-form' },
      h('label', {}, h('span', { text: '代理地址' }), input),
      row(...[
        ['7897', 'http://127.0.0.1:7897'], ['7890', 'http://127.0.0.1:7890'], ['1080', 'socks5://127.0.0.1:1080'], ['10808', 'socks5://127.0.0.1:10808']
      ].map(([label, value]) => Button({ label, onClick: () => { input.value = value; status.textContent = ''; } }))),
      h('div', { class: 'gw-hint', text: '支持 HTTP(S)、SOCKS5 和用户名密码。地址中的认证信息会隐藏。' }));
    const update = () => { custom.hidden = mode !== 'custom'; };
    const payload = () => {
      const out = { key: a.key, proxyMode: mode };
      if (mode === 'custom' && input.value.trim()) out.proxyUrl = input.value.trim();
      return out;
    };
    let working = false;
    const test = Button({ label: '测试连接', onClick: async () => {
      if (working) return;
      working = true; test.disabled = true; status.textContent = '正在通过所选出口检测…';
      try {
        const r = await api('proxy-test', payload());
        status.textContent = `出口 ${r.ip}${r.country ? '（' + r.country + '）' : ''} · ${r.latencyMs} ms`;
      } catch (e) { status.textContent = e.message; }
      finally { working = false; test.disabled = false; }
    } });
    const content = h('div', { class: 'oc-form' },
      h('div', { class: 'gw-hint', text: a.label + ' · 当前：' + a.proxyLabel }),
      Combo({ value: mode, options: [{ label: '跟随全局代理', value: 'inherit' }, { label: '直连', value: 'direct' }, { label: '独立代理', value: 'custom' }], onChange: v => { mode = v; status.textContent = ''; update(); } }),
      custom, row(test), status);
    const dlg = openDialog({ title: '设置账号出口', content, primaryLabel: '保存', secondaryLabel: '取消', onPrimary: () => {
      if (working) return false;
      working = true; status.textContent = '保存中…';
      api('settings', payload()).then(async () => {
        dlg.close(); input.value = ''; connections.delete(a.key); await refresh(); show('success', '账号代理已保存');
      }).catch(e => { status.textContent = e.message; }).finally(() => { working = false; });
      return false;
    } });
    update();
  }
  function accountCard(a) {
    const states = a.states || {};
    const active = Object.values(states).filter(s => s.until > Date.now()).length;
    const count = Object.keys(states).length;
    const card = h('article', { class: 'oc-account' });
    const brand = BrandMark({ id: 'opencode' });
    const identity = h('div', { class: 'oc-identity' },
      h('div', { class: 'oc-name', text: a.label }),
      h('div', { class: 'oc-account-id', text: '账号 ID：' + (a.id || a.key) }));
    card.append(h('div', { class: 'oc-account-head' }, brand, identity,
      row(pill(a.anonymous ? '匿名' : 'API Key'), pill(a.enabled ? '已启用' : '已停用', a.enabled ? 'ok' : 'mute'),
        Toggle({ checked: a.enabled, labelOn: a.label + '：已启用', labelOff: a.label + '：已停用', onChange: value => action(a.key, 'settings', { key: a.key, enabled: value }, value ? '账号已启用' : '账号已停用') }))));
    const connection = connections.get(a.key);
    const config = h('div', { class: 'oc-proxy-row' },
      h('div', { class: 'oc-proxy-copy' }, h('strong', { text: '出口代理' }),
        h('div', { class: 'oc-proxy-label', text: a.proxyLabel }),
        connection ? h('div', { class: 'gw-hint', text: `${connection.ip} · ${connection.country || '地区未知'} · ${connection.latencyMs} ms` }) : null),
      row(Button({ label: '设置代理', onClick: () => proxyDialog(a) }), Button({ label: busy === a.key ? '处理中…' : '测试连接', disabled: !!busy, onClick: () => action(a.key, 'proxy-test', { key: a.key }) })));
    card.append(config);
    const list = h('div', { class: 'oc-state-list' });
    list.append(h('div', { class: 'oc-state-summary', text: active ? `${active} 个模型处于冷却，其余模型单独判断` : count ? '以下为最近一次真实请求结果' : '尚无此通道的请求记录，可选择模型重测' }));
    for (const [model, s] of Object.entries(states).sort((a, b) => b[1].at - a[1].at)) {
      const expired = s.until && s.until <= Date.now();
      list.append(h('div', { class: 'oc-model-state' },
        h('span', { class: 'oc-model-id', text: model }),
        pill(expired ? '冷却已结束，待验证' : labels[s.state] || s.state, expired ? 'mute' : tones[s.state] || 'mute'),
        h('span', { class: 'oc-model-time', text: s.until > Date.now() ? waitText(s.until) : '检测于 ' + clock(s.at) })));
    }
    card.append(list);
    const models = data.models || [];
    const chosen = selected.get(a.key);
    const model = models.some(m => m.id === chosen) ? chosen : models[0]?.id || '';
    selected.set(a.key, model);
    const select = Combo({ value: model, options: models.map(m => ({ label: m.id, value: m.id })), onChange: v => selected.set(a.key, v) });
    select.querySelector('button')?.setAttribute('aria-label', a.label + '：重测模型');
    card.append(h('div', { class: 'oc-account-foot' },
      h('div', { class: 'oc-probe-controls' }, select, Button({ label: '重测模型', disabled: !!busy || !model, onClick: () => action(a.key, 'probe', { key: a.key, model: selected.get(a.key) }) })),
      row(Button({ label: '清除本地限流', disabled: !!busy || !count, onClick: () => confirm('清除本地限流', '只清除这个通道的本地检测与冷却记录，服务端额度不会改变。', () => action(a.key, 'reset', { key: a.key }, '本地限流记录已清除'), '清除记录') }),
        a.builtin ? null : Button({ label: '移除账号', disabled: !!busy, onClick: () => confirm('移除账号', '移除「' + a.label + '」及其本机保存的凭据和代理设置。', () => action(a.key, 'delete', { key: a.key }, '账号已移除'), '移除账号') })),
      h('div', { class: 'gw-hint oc-probe-hint', text: '重测会通过此账号和代理发送一条真实请求，可能消耗用量；停用账号也可手动检测。' })));
    return card;
  }
  function render() {
    if (!mounted) return;
    const toolbar = row(Button({ label: '刷新状态', disabled: loading, onClick: refresh }), Button({ label: '添加匿名通道', onClick: () => addAccount(true) }), Button({ label: '添加账号', variant: 'accent', onClick: () => addAccount() }));
    head.__actions.replaceChildren(toolbar);
    const nodes = [];
    if (notice) nodes.push(InfoBar({ ...notice, onClose: () => { notice = null; render(); } }));
    if (error) nodes.push(InfoBar({ severity: 'error', title: 'OpenCode 状态读取失败', message: error }));
    if (!data) { if (!error) nodes.push(h('div', { class: 'gw-hint', role: 'status', text: '正在读取账号和出口设置…' })); body.replaceChildren(...nodes); return; }
    const accounts = data.accounts || [];
    const enabled = accounts.filter(a => a.enabled).length;
    const cooling = accounts.reduce((sum, a) => sum + Object.values(a.states || {}).filter(s => s.until > Date.now()).length, 0);
    nodes.push(h('div', { class: 'oc-overview' }, ...[
      ['通道', accounts.length, '账号与匿名入口'], ['已启用', enabled, '参与请求选择'], ['模型冷却', cooling, '按账号、模型分别记录']
    ].map(([label, value, detail]) => h('div', { class: 'oc-stat' }, h('span', { text: label }), h('strong', { text: String(value) }), h('small', { text: detail })))));
    nodes.push(h('div', { class: 'oc-explanation' },
      h('strong', { text: '这里显示通道状态，剩余额度未知' }),
      h('p', { text: '刷新状态只更新本机记录。清除限流不会重置服务端额度，更换指纹也不会增加额度。多个通道共用同一出口时，可能共享上游限制。' })));
    const group = Group({ title: '账号与匿名通道', desc: '优先选择启用且该模型未冷却的账号；默认匿名通道作为后备。独立代理只影响对应通道。' });
    group.body.classList.add('oc-account-list');
    for (const a of accounts) group.body.append(accountCard(a));
    nodes.push(group.node);
    body.replaceChildren(...nodes);
  }
  return { node: root, title: 'OpenCode', onMount() {
    mounted = true; render(); refresh();
    timer = setInterval(() => {
      if (!busy && !document.querySelector('.dialog-scrim') && !document.activeElement?.matches('input, textarea, select, [contenteditable="true"]')) refresh();
    }, 15000);
  }, onUnmount() {
    mounted = false; clearInterval(timer);
    for (const dlg of dialogs) dlg.close(); dialogs.clear();
  } };
}
