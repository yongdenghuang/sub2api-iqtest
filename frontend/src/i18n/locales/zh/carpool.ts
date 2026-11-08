export default {
  userCarpool: {
    title: '我的拼车', description: '本车额度与车友用量，跟随官方周窗口同步。',
    refresh: '刷新', loading: '正在读取拼车额度…', empty: '暂无已启用动态均分的生效拼车订阅。',
    loadError: '暂时无法刷新拼车数据；如有旧数据，仅供参考，请稍后重试。',
    sharing: '{count} 位车友 · 动态均分', total: '本车本周总额度', remaining: '本车剩余额度',
    spent: '车友已用 {amount}', remainingHint: '各车友可用额度之和',
    myAllocation: '我的分配', myUsed: '我的已用', myRemaining: '还可用 {amount}', commonCap: '与其他车友同一周上限',
    officialWeekly: '官方 7 天用量', usedPercent: '已用 {percent}%', nextReset: '下次官方重置',
    capacity: '上游整周额度估算 {amount}，为用量推算值，并非官方美元余额。',
    samplingHint: '美元额度采样中，暂按当前已生效额度使用，样本足够后自动调整。',
    roster: '本车车友额度明细', member: '车友', allocated: '分配', used: '已用', available: '剩余', me: '我', seat: '车友 {number}',
    allocationHint: '额度按本车生效人数动态调整，已用金额保留；超过调整后的上限时，剩余按 0 计。这里显示用量额度，不是钱包余额，也不代表已经花费的订阅费。',
    lastSync: '最近官方同步', cadence: '官方约每分钟查询 · 页面每 30 秒刷新 · 时间按当前设备时区显示',
    awaitingSync: '等待首次同步', awaitingReset: '等待官方确认重置', countdown: '约 {days} 天 {hours} 小时 {minutes} 分后重置',
    status: { waiting: '待同步', sampling: '采样中', synced: '已同步', delayed: '同步延迟', pending_reset: '重置确认中', adjusting: '待调整' },
    hint: {
      waiting: '首次同步尚未完成，官方用量和重置时间暂不可用。',
      delayed: '官方数据暂未更新，保留上次额度；最新状态请等待同步或联系管理员。',
      pending_reset: '正在核对官方重置，确认前保留已用额度，不会提前清零。',
      adjusting: '人数或官方用量有变化，当前显示已生效额度，均分调整正在确认。'
    }
  },
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
