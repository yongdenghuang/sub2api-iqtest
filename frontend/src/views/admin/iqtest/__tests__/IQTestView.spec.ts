import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import IQTestView from '../IQTestView.vue'
import IQResultListDialog from '../IQResultListDialog.vue'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQResult } from '@/api/admin/iqtest'
import type { BasePaginationResponse } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: {} }))
vi.mock('@/api/admin/iqtest', () => ({ default: {
  defaults: vi.fn(), listPlans: vi.fn(), listStates: vi.fn(), listRuns: vi.fn(), listRunResults: vi.fn()
} }))

const page = (n: number, id: number): BasePaginationResponse<IQResult> => ({
  items: [{ id, account_name: `account-${id}` } as IQResult], page: n, page_size: 50, total: 250, pages: 5
})

async function openRun() {
  const wrapper = shallowMount(IQTestView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } }
  })
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text() === 'iqtest.tabs.runs')!.trigger('click')
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text() === 'iqtest.result.view')!.trigger('click')
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(iqtestAPI.listPlans).mockResolvedValue([])
  vi.mocked(iqtestAPI.listStates).mockResolvedValue({ items: [], page: 1, total: 0 } as never)
  vi.mocked(iqtestAPI.listRuns).mockResolvedValue({ items: [{ id: 7, plan_id: 1 }], page: 1, total: 1 } as never)
  vi.mocked(iqtestAPI.listRunResults).mockResolvedValue(page(1, 1))
})

describe('IQ run result pagination', () => {
  it('loads results beyond the former 200-result limit', async () => {
    const wrapper = await openRun()
    const dialog = wrapper.findComponent(IQResultListDialog)
    expect(dialog.props('pagination')).toEqual({ page: 1, size: 50, total: 250 })
    vi.mocked(iqtestAPI.listRunResults).mockResolvedValue(page(5, 201))
    dialog.vm.$emit('page', 5)
    await flushPromises()
    expect(iqtestAPI.listRunResults).toHaveBeenLastCalledWith(7, 5, 50)
    expect(dialog.props('items')[0].id).toBe(201)
    expect(dialog.props('pagination').page).toBe(5)
    wrapper.unmount()
  })

  it('ignores an older page response arriving after the newer page', async () => {
    const wrapper = await openRun()
    const dialog = wrapper.findComponent(IQResultListDialog)
    let resolveOld!: (value: BasePaginationResponse<IQResult>) => void
    vi.mocked(iqtestAPI.listRunResults)
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce(page(3, 101))
    dialog.vm.$emit('page', 2)
    dialog.vm.$emit('page', 3)
    await flushPromises()
    resolveOld(page(2, 51))
    await flushPromises()
    expect(dialog.props('items')[0].id).toBe(101)
    expect(dialog.props('pagination').page).toBe(3)
    wrapper.unmount()
  })
})
