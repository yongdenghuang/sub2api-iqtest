import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import iqtestAPI, { type IQRun } from '@/api/admin/iqtest'
import IQBatchRun from '../IQBatchRun.vue'
import IQAccountSummary from '../IQAccountSummary.vue'
import AccountBulkActionsBar from '@/components/admin/account/AccountBulkActionsBar.vue'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/iqtest', () => ({ default: { runAdHoc: vi.fn(), getRun: vi.fn(), listRunResults: vi.fn(), summary: vi.fn() } }))
const globalOptions = { stubs: {
  RouterLink: { template: '<a><slot /></a>' },
  IQResultListDialog: { props: ['show','items'], template: '<div v-if="show" data-results><span>{{ items.length }}</span><slot name="footer" /></div>' },
  Pagination: { emits: ['update:page'], template: '<button data-next @click="$emit(\'update:page\', 2)">Next</button>' }
} }
function run(overrides: Partial<IQRun> = {}): IQRun {
  return { id: 12, plan_id: 0, trigger: 'adhoc', status: 'running', started_at: '', accounts: 2, requests: 1, pass: 1, quality_fail: 0, infra_error: 0, skipped: 0, ...overrides }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  sessionStorage.clear()
  vi.mocked(iqtestAPI.runAdHoc).mockResolvedValue({ run_id: 12 })
  vi.mocked(iqtestAPI.getRun).mockResolvedValue(run())
  vi.mocked(iqtestAPI.listRunResults).mockResolvedValue({ items: [], total: 60, page: 1, page_size: 50, pages: 2 })
})
afterEach(() => { vi.useRealTimers() })

describe('batch IQ testing', () => {
  it('submits both tests for only the selected IDs, prevents repeat clicks and polls through completion', async () => {
    const wrapper = mount(IQBatchRun, { global: globalOptions })
    const start = wrapper.vm.start([17, 31, 17])
    void wrapper.vm.start([99])
    await start
    expect(iqtestAPI.runAdHoc).toHaveBeenCalledTimes(1)
    expect(iqtestAPI.runAdHoc).toHaveBeenCalledWith({ account_ids: [17,31], checks: [{kind:'candy',model:'gpt-6-astra'}, {kind:'pelican_svg',model:'gpt-6-astra'}] })
    expect(wrapper.vm.busy).toBe(true)
    expect(wrapper.get('progress').attributes('value')).toBe('1')
    expect(wrapper.get('progress').attributes('max')).toBe('4')
    vi.mocked(iqtestAPI.getRun).mockResolvedValue(run({ status:'done', pass:2, infra_error:1, skipped:1 }))
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.vm.busy).toBe(false)
    expect(wrapper.get('progress').attributes('value')).toBe('4')
    expect(wrapper.emitted('updated')).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(9000)
    expect(iqtestAPI.getRun).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
  it('keeps a running batch visible and lets completed results be dismissed across reloads', async () => {
    const wrapper = mount(IQBatchRun, { global: globalOptions })
    await wrapper.vm.start([17])
    expect(wrapper.find('[aria-label="iqtest.batch.dismiss"]').exists()).toBe(false)
    vi.mocked(iqtestAPI.getRun).mockResolvedValue(run({ status: 'done', accounts: 1, pass: 2 }))
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.get('progress').classes()).toContain('sr-only')
    await wrapper.get('[aria-label="iqtest.batch.dismiss"]').trigger('click')
    expect(wrapper.find('.iq-batch-strip').exists()).toBe(false)
    expect(sessionStorage.getItem('iqtest-account-batch-run')).toBeNull()
    expect(iqtestAPI.runAdHoc).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('restores progress after refresh and never resubmits when polling fails', async () => {
    sessionStorage.setItem('iqtest-account-batch-run','12')
    vi.mocked(iqtestAPI.getRun).mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(IQBatchRun, { global: globalOptions })
    await flushPromises()
    expect(wrapper.vm.busy).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('iqtest.batch.progressFailed')
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(iqtestAPI.getRun).toHaveBeenCalledTimes(2)
    expect(iqtestAPI.runAdHoc).not.toHaveBeenCalled()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(12000)
    expect(iqtestAPI.getRun).toHaveBeenCalledTimes(2)
  })
  it('surfaces uncertain submission without automatic retry', async () => {
    vi.mocked(iqtestAPI.runAdHoc).mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(IQBatchRun, { global: globalOptions })
    await wrapper.vm.start([17])
    expect(wrapper.get('[role="alert"]').text()).toContain('iqtest.batch.submitFailed')
    await vi.advanceTimersByTimeAsync(30000)
    expect(iqtestAPI.runAdHoc).toHaveBeenCalledTimes(1)
    expect(iqtestAPI.getRun).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('paginates the exact run results', async () => {
    const wrapper = mount(IQBatchRun, { global: globalOptions })
    await wrapper.vm.start([17,31])
    await wrapper.findAll('button').find(b => b.text() === 'iqtest.result.view')!.trigger('click')
    await flushPromises()
    expect(iqtestAPI.listRunResults).toHaveBeenLastCalledWith(12,1,50)
    await wrapper.get('[data-next]').trigger('click')
    await flushPromises()
    expect(iqtestAPI.listRunResults).toHaveBeenLastCalledWith(12,2,50)
    wrapper.unmount()
  })
  it('shows the batch action only for a selection and disables it while running', async () => {
    const wrapper = mount(AccountBulkActionsBar, { props: { selectedIds: [], totalResults: 2, selectingAll: false, allResultsSelected: false } })
    expect(wrapper.text()).not.toContain('iqtest.batch.button')
    await wrapper.setProps({ selectedIds: [17] })
    const button = wrapper.findAll('button').find(b => b.text() === 'iqtest.batch.button')!
    await button.trigger('click')
    expect(wrapper.emitted('iq-test')).toHaveLength(1)
    await wrapper.setProps({ iqTesting: true })
    expect(button.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})

describe('global IQ summary', () => {
  it('refreshes totals and shows an explicit error instead of zeroes when the API fails', async () => {
    vi.mocked(iqtestAPI.summary).mockResolvedValue({ total:6, quality_fail:1, infra_error:1, normal:3, untested:1 })
    const wrapper = mount(IQAccountSummary, { props: { refreshToken: 0 } })
    await flushPromises()
    expect(wrapper.text()).toContain('iqtest.overview.total')
    vi.mocked(iqtestAPI.summary).mockRejectedValueOnce(new Error('offline'))
    await wrapper.setProps({ refreshToken: 1 })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('iqtest.overview.failed')
    expect(wrapper.text()).not.toContain('iqtest.overview.total')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('iqtest.overview.total')
    wrapper.unmount()
  })
})
