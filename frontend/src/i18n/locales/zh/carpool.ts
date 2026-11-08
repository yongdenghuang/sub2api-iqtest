export default {
  carpool: {
    title: '拼车额度', description: '按生效人数均分周额度，跟随官方周窗口重置。',
    explanation: '每分钟查询官方用量。美元额度由实际消耗估算，采样不足时使用初始总额度；已用额度会保留。',
    scope: '适用于单个 OpenAI OAuth 上游的订阅分组。只调整分组周限额，不修改钱包余额，也不使用重置卡。',
    refresh: '刷新', empty: '暂无 OpenAI 订阅分组。请先创建分组、绑定一个上游账号，并添加订阅用户。',
    source: '上游账号', members: '生效车友', share: '{count} 人均分',
    weekly: '每人周上限', used: '官方周用量', reset: '官方重置时间',
    capacity: '估算整周总量', sampling: '采样中', disabled: '未启用', enabled: '同步中', paused: '同步异常',
    checked: '最近查询', waiting: '等待首次同步',
    enable: '自动均分与重置', seed: '初始总周额度（美元）', seedHint: '用于采样起步；有足够样本后自动测算。',
    save: '保存设置', saved: '设置已保存，将在一分钟内同步。', sync: '立即同步', synced: '已同步最新官方额度。',
    ineligible: '启用需绑定且只绑定一个 OpenAI OAuth 账号，并有生效车友。',
    name: '车友', spent: '本周已用', remaining: '当前可用', history: '调整记录',
    noEvents: '暂无调整记录', time: '时间', reason: '原因', before: '调整前 / 人', after: '调整后 / 人',
    adjust: '额度 / 人数调整', officialReset: '官方周窗口重置', error: '操作失败，请稍后重试。',
    disableHint: '停用后保留最后一次周上限，恢复原有按周重置规则。',
    fairHint: '每人的周上限相同。上限降低时，已超额车友暂停新增使用；已用额度不会退回池中。',
    noMembers: '当前没有生效车友，自动调整已暂停。'
  }
}
