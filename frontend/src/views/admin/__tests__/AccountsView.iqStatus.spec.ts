import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const { listAccounts, loadIQStates } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  loadIQStates: vi.fn()
}))

vi.mock('@/views/admin/iqtest/useIQAccountStates', () => ({
  useIQAccountStates: loadIQStates
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag: vi.fn(),
      getBatchTodayStats: vi.fn().mockResolvedValue({ stats: {} }),
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: { getAll: vi.fn().mockResolvedValue([]) },
    groups: { getAll: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const DataTableStub = {
  props: ['columns'],
  emits: ['sort'],
  template: `
    <div data-test="data-table">
      <span v-for="column in columns" :key="column.key" :data-column="column.key">
        {{ column.sortable ? 'sortable' : 'fixed' }}
      </span>
      <button data-test="sort-priority" @click="$emit('sort', 'priority', 'desc')" />
    </div>
  `
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
        IQAccountSummary: true,
        IQBatchRun: true,
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        AccountTableActions: { name: 'AccountTableActions', emits: ['refresh'], template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

describe('admin AccountsView IQ status column', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    loadIQStates.mockReset().mockImplementation(() => ref({}))
  })

  it('preserves the quality filter for list loads and selection across pages', async () => {
    const wrapper = mountView()
    await flushPromises()
    wrapper.findComponent({ name: 'AccountTableFilters' }).vm.$emit('update:filters', { iq_quality: 'quality_fail' })
    wrapper.findComponent({ name: 'AccountTableActions' }).vm.$emit('refresh')
    await flushPromises()
    expect(listAccounts.mock.calls.at(-1)?.[2]).toMatchObject({ iq_quality: 'quality_fail' })
    listAccounts.mockResolvedValue({ items: [{ id: 7 }], total: 1, pages: 1 })
    wrapper.findComponent({ name: 'AccountTableActions' }).vm.$emit('refresh')
    await flushPromises()
    wrapper.findComponent({ name: 'AccountBulkActionsBar' }).vm.$emit('select-all-results')
    await flushPromises()
    expect(listAccounts).toHaveBeenLastCalledWith(1, 1000, expect.objectContaining({ iq_quality: 'quality_fail' }))
    expect(wrapper.findComponent({ name: 'AccountBulkActionsBar' }).props('selectedIds')).toEqual([7])
    wrapper.unmount()
  })

  it('places IQ status directly after running status and before detailed tests', async () => {
    const wrapper = mountView()
    await flushPromises()
    const keys = wrapper.findAll('[data-column]').map(column => column.attributes('data-column'))
    const statusIndex = keys.indexOf('status')
    expect(keys.slice(statusIndex, statusIndex + 3)).toEqual(['status', 'iq_status', 'iqtest'])
    wrapper.unmount()
  })

  it('still loads real IQ states when the detailed test column is hidden', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['iqtest']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-column="iq_status"]').exists()).toBe(true)
    expect(wrapper.find('[data-column="iqtest"]').exists()).toBe(false)
    expect(loadIQStates.mock.calls[0][1]()).toBe(true)
    wrapper.unmount()
  })

  it('does not request IQ states when both IQ columns are hidden', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['iqtest', 'iq_status']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-column="iq_status"]').exists()).toBe(false)
    expect(loadIQStates.mock.calls[0][1]()).toBe(false)
    wrapper.unmount()
  })
})
