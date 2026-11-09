import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CarpoolOverview from '../CarpoolOverview.vue'
import { getMyCarpools, type MyCarpool } from '@/api/carpool'
import zh from '@/i18n/locales/zh/carpool'

vi.mock('@/api/carpool', () => ({ getMyCarpools: vi.fn() }))
const sample: MyCarpool = {
  id: 12, name: '四人车', member_count: 4, allocation_ready: true, allocation_mode: 'auto', sync_official_reset: true,
  total_usd: 2400, used_usd: 400, remaining_usd: 2000, per_member_usd: 600,
  estimated: false, upstream_capacity_usd: null, official_used_percent: 25,
  last_sync_at: '2026-11-08T10:00:00Z', next_reset_at: '2026-11-14T10:00:00Z', status: 'sampling',
  windows: [
    { window: '5h', range_source: 'official', official_used_percent: 12, start_at: '2026-11-08T08:00:00Z', reset_at: '2026-11-08T13:00:00Z', stats: { requests: 2200, tokens: 271700000, account_cost_usd: 57.75, user_cost_usd: 28.88 } },
    { window: '7d', range_source: 'official', official_used_percent: 25, start_at: '2026-11-07T10:00:00Z', reset_at: '2026-11-14T10:00:00Z', stats: { requests: 3500, tokens: 400000000, account_cost_usd: 128.49, user_cost_usd: 64.25 } }
  ],
  // A stale older server response must not make the UI render other members.
  members: [1, 2, 3, 4].map(number => ({ number, name: number === 4 ? '' : `member${number}`, is_self: number === 2, allocated_usd: 600, used_usd: 100, remaining_usd: 500 }))
}
let wrapper: VueWrapper | undefined
const setup = (hideEmpty = false) => {
  wrapper = mount(CarpoolOverview, { props: { hideEmpty }, global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] } })
  return wrapper
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-08T10:00:00Z'))
  vi.resetAllMocks()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.mocked(getMyCarpools).mockResolvedValue(structuredClone([sample]))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers() })

describe('member shared quota overview', () => {
  it('shows only my details, distinct window costs and the shared official quota', async () => {
    const w = setup(); await flushPromises()
    expect(w.text()).toContain('$2,400.00')
    expect(w.text()).toContain('$2,000.00')
    expect(w.text()).toContain('$600.00')
    expect(w.text()).toContain('$100.00')
    expect(w.text()).toContain('$500.00')
    for (const name of ['member1', 'member3', '车友 4']) expect(w.text()).not.toContain(name)
    expect(w.find('table').exists()).toBe(false)
    const five = w.get('[data-window="5h"]')
    expect(five.text()).toContain('2.2K')
    expect(five.text()).toContain('271.7M')
    expect(five.text()).toContain('账号计费（A）$57.75')
    expect(five.text()).toContain('我的计费（U）$28.88')
    expect(five.text()).toContain('约 0 天 3 小时 0 分后重置')
    expect(five.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('12')
    const seven = w.get('[data-window="7d"]')
    expect(seven.text()).toContain('3.5K')
    expect(seven.text()).toContain('$128.49')
    expect(seven.text()).toContain('$64.25')
    expect(seven.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('25')
    expect(seven.text()).toContain('约 6 天 0 小时 0 分后重置')
    expect(w.text()).toContain('仅统计我在本车的本站请求')
    expect(w.text()).toContain('官方共享用量 · 整辆车')
    expect(w.text()).toContain('美元额度采样中')
    expect(w.text()).not.toContain('额度按本车生效人数动态调整')
  })
  it('refreshes allocations and clearly labels upstream estimates independently', async () => {
    const w = setup(); await flushPromises()
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, total_usd: 2000, estimated: true, upstream_capacity_usd: 2600, status: 'synced' }])
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(getMyCarpools).toHaveBeenCalledTimes(2)
    expect(w.text()).toContain('上游整周额度估算 $2,600.00')
    expect(w.text()).toContain('并非官方美元余额')
    expect(w.text()).not.toContain('$2,400.00')
    expect(w.text()).toContain('已同步')
  })
  it('shows manual totals and remaining quota, then follows an administrator mode change', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, allocation_mode: 'manual', status: 'synced' }])
    const w = setup(); await flushPromises()
    expect(w.text()).toContain('4 位车友 · 手动额度')
    expect(w.text()).toContain('手动设定总额度')
    expect(w.text()).toContain('$2,400.00')
    expect(w.text()).toContain('$2,000.00')
    expect(w.text()).not.toContain('采样中')
    expect(w.text()).not.toContain('每人周额度由管理员设定')
    expect(w.text()).not.toContain('不随官方用量或人数自动调整')
    expect(w.text()).toContain('已开启同步官方重置')
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, total_usd: 2000, estimated: true, upstream_capacity_usd: 2600, status: 'synced' }])
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(w.text()).toContain('4 位车友 · 动态均分')
    expect(w.text()).toContain('当前动态分配总额')
    expect(w.text()).toContain('上游整周额度估算 $2,600.00')
    expect(w.text()).not.toContain('手动设定总额度')
  })
  it('labels upstream countdown as reference when ordinary local resets are used', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, allocation_mode: 'manual', sync_official_reset: false, next_reset_at: '2026-11-08T09:59:00Z', status: 'synced' }])
    const w = setup(); await flushPromises()
    expect(w.text()).toContain('本车采用本站原有按周重置规则')
    expect(w.text()).toContain('倒计时仅供参考')
    expect(w.text()).not.toContain('重置确认中')
  })
  it('shows unknown rather than zero official use before the first sync', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, allocation_ready: false, official_used_percent: null, windows: sample.windows.map(v => ({ ...v, official_used_percent: null, start_at: null, reset_at: null, stats: null })), last_sync_at: null, next_reset_at: null, status: 'waiting' }])
    const w = setup(); await flushPromises()
    expect(w.findAll('[role="progressbar"]').every(bar => bar.attributes('aria-valuenow') === undefined)).toBe(true)
    expect(w.text()).not.toContain('2.2K')
    expect(w.text()).toContain('个人窗口统计暂不可用')
    expect(w.text()).toContain('等待首次同步')
    expect(w.text()).not.toContain('$2,400.00')
    expect(w.text()).not.toContain('已用 0%')
  })
  it('retains the snapshot with a delay warning on query failure and never claims an early reset', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, next_reset_at: '2026-11-08T10:00:15Z' }])
    const w = setup(); await flushPromises()
    vi.mocked(getMyCarpools).mockRejectedValue({ status: 503 })
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(w.text()).toContain('$2,400.00')
    expect(w.get('[role="alert"]').text()).toContain('旧数据')
    expect(w.text()).toContain('同步延迟')
    expect(w.text()).toContain('同步延迟')
    expect(w.text()).toContain('$100.00')
  })
  it.each([401, 403, 'removed'] as const)('removes personal data after access is lost: %s', async (loss) => {
    const w = setup(true); await flushPromises()
    expect(w.findAll('[data-testid="carpool-card"]')).toHaveLength(1)
    if (loss === 'removed') vi.mocked(getMyCarpools).mockResolvedValue([])
    else vi.mocked(getMyCarpools).mockRejectedValue({ status: loss })
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(w.findAll('[data-testid="carpool-card"]')).toHaveLength(0)
    expect(w.text()).not.toContain('271.7M')
    expect(w.text()).not.toContain('member2')
    if (loss === 'removed') expect(w.find('section').exists()).toBe(false)
  })
  it('shows recent personal usage when upstream provides no active five-hour window', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, windows: [{ ...sample.windows[0], range_source: 'recent', reset_at: null, official_used_percent: null }, sample.windows[1]] }])
    const w = setup(); await flushPromises()
    const five = w.get('[data-window="5h"]')
    expect(five.text()).toContain('最近 5 小时 · 本站记录')
    expect(five.text()).toContain('2.2K')
    expect(five.text()).toContain('$28.88')
    expect(five.text()).toContain('官方暂未提供重置时间')
    expect(five.text()).not.toContain('已用 0%')
    expect(w.get('[data-window="7d"]').text()).toContain('本窗口累计')
  })
  it('labels ended windows without pretending their usage has reset', async () => {
    vi.mocked(getMyCarpools).mockResolvedValue([{ ...sample, windows: [{ ...sample.windows[0], start_at: '2026-11-08T05:00:00Z', reset_at: '2026-11-08T10:00:15Z' }, sample.windows[1]] }])
    const w = setup(); await flushPromises()
    await vi.advanceTimersByTimeAsync(20000)
    expect(w.get('[data-window="5h"]').text()).toContain('上一窗口')
    expect(w.get('[data-window="5h"]').text()).toContain('窗口已到期，等待官方更新')
    expect(w.get('[data-window="5h"]').text()).toContain('2.2K')
    expect(w.get('[data-window="7d"]').text()).not.toContain('上一窗口')
  })
  it('pauses hidden-tab requests, resumes on return, and cleans up timers on navigation', async () => {
    const w = setup(); await flushPromises()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await vi.advanceTimersByTimeAsync(60000)
    expect(getMyCarpools).toHaveBeenCalledTimes(1)
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(getMyCarpools).toHaveBeenCalledTimes(2)
    w.unmount(); wrapper = undefined
    await vi.advanceTimersByTimeAsync(60000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getMyCarpools).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })
  it('aborts in-flight requests when leaving the page', async () => {
    vi.mocked(getMyCarpools).mockImplementation(() => new Promise(() => {}))
    const w = setup(); await flushPromises()
    const signal = vi.mocked(getMyCarpools).mock.calls[0][0]!
    expect(signal.aborted).toBe(false)
    w.unmount(); wrapper = undefined
    expect(signal.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
})
