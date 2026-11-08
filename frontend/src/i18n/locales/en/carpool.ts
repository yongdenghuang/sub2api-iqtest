export default {
  carpool: {
    title: 'Shared quota', description: 'Equal weekly caps for active subscribers, aligned with upstream resets.',
    explanation: 'Queries upstream usage every minute. Dollar capacity is estimated from observed consumption. The initial budget is used until enough samples exist. Spent quota is retained.',
    scope: 'For subscription groups with one OpenAI OAuth source. Only the group weekly cap changes; wallets and reset credits are untouched.',
    refresh: 'Refresh', empty: 'No OpenAI subscription groups. Create a group, bind one source account, and assign subscribers first.',
    source: 'Source account', members: 'Active members', share: 'Shared by {count}',
    weekly: 'Weekly cap per member', used: 'Upstream weekly usage', reset: 'Upstream reset time',
    capacity: 'Estimated full-week capacity', sampling: 'Collecting samples', disabled: 'Disabled', enabled: 'Sync enabled', paused: 'Sync error',
    checked: 'Last checked', waiting: 'Waiting for first sync',
    enable: 'Automatic allocation and resets', seed: 'Initial total weekly budget (USD)', seedHint: 'Used while collecting samples; capacity is estimated automatically afterwards.',
    save: 'Save settings', saved: 'Settings saved. Synchronization runs within a minute.', sync: 'Sync now', synced: 'Latest upstream quota synchronized.',
    ineligible: 'Requires exactly one OpenAI OAuth source and at least one active subscriber.',
    name: 'Member', spent: 'Used this week', remaining: 'Available now', history: 'Adjustment history',
    noEvents: 'No adjustments yet', time: 'Time', reason: 'Reason', before: 'Before / member', after: 'After / member',
    adjust: 'Capacity / membership change', officialReset: 'Upstream weekly reset', error: 'The operation failed. Please retry.',
    disableHint: 'Disabling keeps the last weekly cap and restores ordinary weekly resets.',
    fairHint: 'Members have the same weekly cap. After a decrease, members already over their cap cannot spend more; spent quota is not returned to the pool.',
    noMembers: 'No active subscribers. Automatic adjustments are paused.'
  }
}
