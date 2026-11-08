import { Card, Group, Toggle, InfoBar, Button, h, icon } from '../components/index.js';

const LABELS = { claimed: '已领取', already: '今日已领', failed: '领取失败', pending: '处理中',
  inactive: '活动未开放', login_required: '需重新登录', manual: '需手动核实' };

export default function checkinPanel(data, error, save, run, refresh) {
  const group = Group({ title: '签到中心',
    desc: '按北京时间逐账号领取，启动后补查当日任务。程序关闭期间不会执行；聚合轮换与签到分别控制。' });
  if (!data || error) {
    group.body.append(Card({ icon: 'restart', title: '签到状态暂不可用', desc: error || '正在读取设置',
      right: Button({ label: '刷新', onClick: refresh }) }));
    return group.node;
  }
  const settings = data.settings || {};
  if (data.error) group.body.append(InfoBar({ severity: 'error', title: '签到已停止', message: data.error }));
  group.body.append(Card({ icon: 'restart', width: 'full', title: '自动领取每日奖励',
    desc: '每日 ' + (settings.times || []).join('、') + '（UTC+8）；失败按间隔重试，每时段最多 4 次。',
    right: Toggle({ checked: !!settings.enabled, onChange: (v) => save({ enabled: v }) }) }));
  const times = h('input', { class: 'text-box', value: (settings.times || []).join(','),
    'aria-label': '每日签到时间，逗号分隔', placeholder: '09:00,21:00' });
  const submitTimes = () => save({ times: times.value.split(/[,，]/).map((v) => v.trim()).filter(Boolean) });
  const field = h('div', { class: 'card gw-field-card is-full' },
    h('div', { class: 'card-main has-desc' },
      h('div', { class: 'card-title', text: '每日执行时间' }),
      h('div', { class: 'card-desc', text: '填写 1 至 4 个时间，用逗号分隔。' })),
    h('div', { class: 'gw-field-line' }, times, Button({ label: '保存时间', onClick: submitTimes })));
  group.body.append(field);
  group.body.append(Card({ icon: 'play', width: 'full', title: data.running ? '正在逐账号签到' : '手动一键签到',
    desc: '仅处理已开启的渠道；跳过今日已确认的账号。关闭自动领取后仍可手动执行。',
    right: Button({ label: data.running ? '签到中…' : '一键签到', variant: 'accent', disabled: !!data.running || !!data.error, onClick: run }) }));
  const tag = (text, tone = 'mute') => h('span', { class: 'pill tone-' + tone, text });
  const stamp = (ms) => ms ? new Date(ms).toLocaleString('zh-CN', { hour12: false }) : '';
  const groups = new Map();
  for (const account of (data.accounts || [])) {
    if (!groups.has(account.channel)) groups.set(account.channel, []);
    groups.get(account.channel).push(account);
  }
  if (!groups.size) group.body.append(Card({ icon: 'key', title: '暂无已登录账号', desc: '登录渠道后，这里显示该渠道的签到能力与逐账号结果。' }));
  for (const [channel, accounts] of groups) {
    const first = accounts[0];
    const automatic = first.mode === 'automatic';
    const card = h('div', { class: 'chan-card' });
    card.append(h('div', { class: 'chan-head' },
      h('span', { class: 'card-brand', 'aria-hidden': 'true' }, icon('timer')),
      h('div', { class: 'chan-head-main' },
        h('div', { class: 'agg-turn-name' }, h('span', { text: first.label.split(' · ')[0] }),
          tag(automatic ? (first.enabled ? '参与签到' : '已停用签到') : first.mode === 'manual' ? '需手动处理' : '不支持自动签到', automatic && first.enabled ? 'ok' : 'mute')),
        h('div', { class: 'agg-turn-sub', text: first.reason })),
      automatic ? h('div', { class: 'chan-head-right' }, Toggle({ checked: !!first.enabled,
        onChange: (v) => save({ disabled: { ...(settings.disabled || {}), [channel]: !v } }) })) : null));
    const rows = h('div', { class: 'chan-accs' });
    for (const account of accounts) {
      const result = account.result || {};
      const current = result.day === data.today;
      const complete = result.coveredDay === data.today;
      let status = automatic ? (current ? (LABELS[result.status] || '尚未完成') : '今日待领取')
        : account.mode === 'manual' ? '需手动处理' : '无需自动领取';
      if (current && ['claimed', 'already'].includes(result.status) && !complete) status = '待核实今日结果';
      const detail = [stamp(result.updatedAt), result.message || '',
        result.nextAttemptAt && current && result.attempts < 4 ? '下次重试 ' + stamp(result.nextAttemptAt) : ''].filter(Boolean).join(' · ');
      rows.append(h('div', { class: 'chan-acc' },
        h('span', { class: 'chan-acc-dot ' + (complete ? 'is-ok' : 'is-mute'), 'aria-hidden': 'true' }),
        h('div', { class: 'chan-acc-main' }, h('div', { class: 'chan-acc-name', text: account.label }),
          h('div', { class: 'chan-acc-meta', text: detail || account.reason })),
        h('div', { class: 'chan-acc-side' }, tag(status, complete ? 'ok' : current && result.status === 'failed' ? 'warn' : 'mute'),
          current && result.status === 'claimed' && typeof result.amount === 'number' ? tag('本次 ' + result.amount + ' 积分') : null)));
    }
    card.append(rows);
    group.body.append(card);
  }
  return group.node;
}
