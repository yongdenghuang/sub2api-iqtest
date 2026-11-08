export default {
  userCarpool: {
    title: 'My carpool', description: 'Shared allowances and member usage, aligned with the upstream weekly window.',
    refresh: 'Refresh', loading: 'Loading shared quotas…', empty: 'No active carpool subscriptions with dynamic sharing enabled.',
    loadError: 'Unable to refresh shared quotas. Any previous data is for reference only. Please retry shortly.',
    sharing: '{count} members · Dynamic sharing', total: 'Total weekly allocation', remaining: 'Carpool quota remaining',
    spent: 'Members used {amount}', remainingHint: 'Sum of members’ available allowances',
    myAllocation: 'My allocation', myUsed: 'My usage', myRemaining: '{amount} available', commonCap: 'Same weekly cap for every member',
    officialWeekly: 'Upstream 7-day usage', usedPercent: '{percent}% used', nextReset: 'Next upstream reset',
    capacity: 'Estimated full-week upstream capacity: {amount}. Calculated from consumption; not an official dollar balance.',
    samplingHint: 'Collecting dollar-capacity samples. Using the initial budget set by the administrator.',
    roster: 'Member quota breakdown', member: 'Member', allocated: 'Allocated', used: 'Used', available: 'Remaining', me: 'Me', seat: 'Member {number}',
    allocationHint: 'Allowances adjust with active membership and retain past usage. Members above a reduced cap have zero remaining. These are usage allowances, not wallet balances or subscription fees already spent.',
    lastSync: 'Last upstream sync', cadence: 'Upstream checked about every minute · Page refreshes every 30 seconds · Times use your device timezone',
    awaitingSync: 'Waiting for first sync', awaitingReset: 'Waiting for upstream reset confirmation', countdown: 'Reset in about {days}d {hours}h {minutes}m',
    status: { waiting: 'Awaiting sync', sampling: 'Sampling', synced: 'Synced', delayed: 'Sync delayed', pending_reset: 'Confirming reset', adjusting: 'Adjustment pending' },
    hint: {
      waiting: 'The first sync has not completed. Upstream usage and reset time are not yet available.',
      delayed: 'Upstream data has not refreshed. Previous allowances are retained; wait for synchronization or contact the administrator.',
      pending_reset: 'Verifying the upstream reset. Usage is retained until confirmation, without early resets.',
      adjusting: 'Membership or upstream usage changed. Displaying the currently applied allowances while the adjustment is confirmed.'
    }
  },
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
