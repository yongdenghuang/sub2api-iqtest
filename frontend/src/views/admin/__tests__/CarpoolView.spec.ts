import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CarpoolView from '../CarpoolView.vue'
import zh from '@/i18n/locales/zh/carpool'
import { carpoolApi, type CarpoolGroup } from '@/api/admin/carpool'

vi.mock('@/api/admin/carpool', () => ({ carpoolApi: { list: vi.fn(), save: vi.fn(), sync: vi.fn(), events: vi.fn() } }))
const group: CarpoolGroup = {
  id: 12, name: '四人车', account_id: 2, account_name: 'Source', account_count: 1, eligible: true, weekly_limit: 600,
  config: { enabled: true, initial_total_usd: 2400, allocation_mode: 'auto', manual_weekly_usd: 600, sync_official_reset: true },
  state: { percent: 25, checked_at: '2026-11-08T10:00:00Z', reset_at: '2026-11-14T10:00:00Z', estimated: false },
  members: [{ id: 1, user_id: 4, name: 'Member', usage: 100 }]
}
const setup = () => mount(CarpoolView, { global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })], stubs: { teleport: true, AppLayout: { template: '<div><slot /></div>' } } } })

beforeEach(() => { vi.useFakeTimers(); vi.resetAllMocks(); vi.mocked(carpoolApi.list).mockResolvedValue(structuredClone([group])) })
afterEach(() => { vi.useRealTimers() })
describe('shared subscription quota controls', () => {
  it('shows applied limits and preserves edits across background polling', async () => {
    const wrapper = setup(); await flushPromises()
    expect(wrapper.text()).toContain('$600.00'); expect(wrapper.text()).toContain('$500.00'); expect(wrapper.text()).toContain('采样中')
    await wrapper.get('input[type="number"]').setValue(3200)
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('3200')
    expect(carpoolApi.save).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { ...group.config, initial_total_usd: 3200 })
    wrapper.unmount()
  })
  it('allows disabling after a source binding becomes invalid', async () => {
    vi.mocked(carpoolApi.list).mockResolvedValue([{ ...structuredClone(group), account_count: 2 }])
    const wrapper = setup(); await flushPromises()
    const submit = wrapper.get('button[type="submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    await wrapper.get('input[type="checkbox"]').setValue(false)
    expect(submit.attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { ...group.config, enabled: false })
    wrapper.unmount()
  })
  it('switches an estimated pool to an editable manual cap without tying it to resets', async () => {
    const estimated = { ...structuredClone(group), weekly_limit: 292.30, config: { ...group.config, manual_weekly_usd: 292.30 }, state: { ...group.state, estimated: true, capacity: 1034.35 } }
    vi.mocked(carpoolApi.list).mockResolvedValue([estimated])
    const wrapper = setup(); await flushPromises()
    expect(wrapper.get('input[type="number"]').attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('manual')
    expect(wrapper.get('input[type="number"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('input[type="number"]').setValue(600)
    await wrapper.findAll('input[type="checkbox"]')[1].setValue(false)
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('manual')
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('600')
    expect(wrapper.text()).toContain('采用本站原有按周重置规则')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { enabled: true, initial_total_usd: 2400, allocation_mode: 'manual', manual_weekly_usd: 600, sync_official_reset: false })
    expect(wrapper.get('[role="status"]').text()).toContain('立即生效')
    wrapper.unmount()
  })
  it('shows a manual pool total and refreshes the seed when switching back to auto', async () => {
    vi.mocked(carpoolApi.list).mockResolvedValue([{ ...structuredClone(group), config: { ...group.config, allocation_mode: 'manual' }, state: { ...group.state, estimated: false } }])
    const wrapper = setup(); await flushPromises()
    expect(wrapper.text()).toContain('本车分配总额')
    expect(wrapper.text()).not.toContain('采样中')
    await wrapper.get('select').setValue('auto')
    expect(wrapper.get('input[type="number"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('直接使用 Codex')
    wrapper.unmount()
  })

  it('confirms the exact old/new source and preserves edits when cancelling', async () => {
    const changed = { ...structuredClone(group), account_id: 19, account_name: 'New source', bound_account_id: 2, bound_account_name: 'Old source', binding_changed: true, state: { ...group.state, estimated: true } }
    vi.mocked(carpoolApi.list).mockResolvedValue([changed])
    const wrapper = setup(); await flushPromises()
    expect(wrapper.get('input[type="number"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('input[type="number"]').setValue(3200)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(carpoolApi.save).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Old source #2 → New source #19')
    expect(wrapper.get('[role="dialog"]').text()).toContain('$3200.00')
    expect(wrapper.get('[role="dialog"]').text()).toContain('不清零已用额度')
    const polls = vi.mocked(carpoolApi.list).mock.calls.length
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(carpoolApi.list).toHaveBeenCalledTimes(polls)
    await wrapper.get('[role="dialog"]').findAll('button').find(b => b.text() === '取消')!.trigger('click')
    expect(carpoolApi.save).not.toHaveBeenCalled()
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('3200')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const confirm = wrapper.get('[role="dialog"]').findAll('button').find(b => b.text() === '确认覆盖绑定')!
    await confirm.trigger('click'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledTimes(1)
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { ...group.config, initial_total_usd: 3200, replace_upstream: { previous_account_id: 2, account_id: 19 } })
    expect(wrapper.get('[role="status"]').text()).toContain('已覆盖上游绑定')
    wrapper.unmount()
  })
  it('requires a new confirmation if the binding changed again on the server', async () => {
    vi.mocked(carpoolApi.list).mockResolvedValue([{ ...structuredClone(group), account_id: 19, bound_account_id: 2, binding_changed: true }])
    vi.mocked(carpoolApi.save).mockRejectedValue({ status: 409, reason: 'CARPOOL_BINDING_CHANGED', message: '请刷新并确认覆盖绑定' })
    const wrapper = setup(); await flushPromises()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    vi.mocked(carpoolApi.list).mockResolvedValue([{ ...structuredClone(group), account_id: 20, bound_account_id: 2, binding_changed: true }])
    await wrapper.get('[role="dialog"]').findAll('button').find(b => b.text() === '确认覆盖绑定')!.trigger('click'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[role="alert"]').text()).toContain('请刷新')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('#20')
    wrapper.unmount()
  })
  it('confirms before syncing a replaced source and allows disabling without confirmation', async () => {
    vi.mocked(carpoolApi.list).mockResolvedValue([{ ...structuredClone(group), account_id: 19, bound_account_id: 2, binding_changed: true }])
    const wrapper = setup(); await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === '立即同步')!.trigger('click'); await flushPromises()
    expect(carpoolApi.sync).not.toHaveBeenCalled()
    await wrapper.get('[role="dialog"]').findAll('button').find(b => b.text() === '确认覆盖绑定')!.trigger('click'); await flushPromises()
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { ...group.config, replace_upstream: { previous_account_id: 2, account_id: 19 } })
    expect(carpoolApi.sync).toHaveBeenCalledWith(12)
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(carpoolApi.save).toHaveBeenLastCalledWith(12, { ...group.config, enabled: false })
    wrapper.unmount()
  })
  it('reports a failed manual sync without claiming success', async () => {
    vi.mocked(carpoolApi.sync).mockRejectedValue(new Error('账号授权失效'))
    const wrapper = setup(); await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === '立即同步')!.trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('账号授权失效')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
