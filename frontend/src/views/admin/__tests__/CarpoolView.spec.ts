import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CarpoolView from '../CarpoolView.vue'
import zh from '@/i18n/locales/zh/carpool'
import { carpoolApi, type CarpoolGroup } from '@/api/admin/carpool'

vi.mock('@/api/admin/carpool', () => ({ carpoolApi: { list: vi.fn(), save: vi.fn(), sync: vi.fn(), events: vi.fn() } }))
const group: CarpoolGroup = {
  id: 12, name: '四人车', account_id: 2, account_name: 'Source', account_count: 1, eligible: true, weekly_limit: 600,
  config: { enabled: true, initial_total_usd: 2400 },
  state: { percent: 25, checked_at: '2026-11-08T10:00:00Z', reset_at: '2026-11-14T10:00:00Z', estimated: false },
  members: [{ id: 1, user_id: 4, name: 'Member', usage: 100 }]
}
const setup = () => mount(CarpoolView, { global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })], stubs: { AppLayout: { template: '<div><slot /></div>' } } } })

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
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { enabled: true, initial_total_usd: 3200 })
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
    expect(carpoolApi.save).toHaveBeenCalledWith(12, { enabled: false, initial_total_usd: 2400 })
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
