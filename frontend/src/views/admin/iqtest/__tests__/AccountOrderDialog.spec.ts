import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountOrderDialog from '../AccountOrderDialog.vue'

const { getOrder, saveOrder, success } = vi.hoisted(() => ({ getOrder: vi.fn(), saveOrder: vi.fn(), success: vi.fn() }))
vi.mock('@/api/admin/iqtest', () => ({ iqtestAPI: { accountOrder: getOrder, saveAccountOrder: saveOrder } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: success }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const rows = () => [1, 2, 3].map(id => ({ id, name: `Account ${id}`, platform: 'openai' }))
function create() {
  return mount(AccountOrderDialog, { global: { stubs: {
    BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
    VueDraggable: { name: 'VueDraggable', props: ['modelValue', 'disabled'], emits: ['update:modelValue'], template: '<div><slot /></div>' },
    Icon: true
  } } })
}
describe('AccountOrderDialog', () => {
  beforeEach(() => {
    getOrder.mockReset().mockResolvedValue({ accounts: rows(), revision: 'version-one' })
    saveOrder.mockReset().mockResolvedValue(undefined)
    success.mockReset()
  })
  it('saves a dragged order with its revision, not scheduler priorities', async () => {
    const wrapper = create()
    await flushPromises()
    wrapper.findComponent({ name: 'VueDraggable' }).vm.$emit('update:modelValue', rows().reverse())
    await wrapper.find('[data-test="save-account-order"]').trigger('click')
    await flushPromises()
    expect(saveOrder).toHaveBeenCalledWith([3, 2, 1], 'version-one')
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('supports keyboard buttons and cancellation without saving', async () => {
    const wrapper = create()
    await flushPromises()
    await wrapper.findAll('[data-test="move-down"]')[0].trigger('click')
    expect(wrapper.findAll('[data-account-id]').map(row => row.attributes('data-account-id'))).toEqual(['2', '1', '3'])
    await wrapper.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    expect(saveOrder).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
  it('keeps failed changes visible and reloads before allowing a retry', async () => {
    saveOrder.mockRejectedValue({ response: { status: 409 } })
    const wrapper = create()
    await flushPromises()
    await wrapper.find('[data-test="save-account-order"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('iqtest.accountOrder.conflict')
    expect(wrapper.find('[data-test="save-account-order"]').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('saved')).toBeUndefined()
    getOrder.mockResolvedValue({ accounts: rows().reverse(), revision: 'version-two' })
    await wrapper.find('[role="alert"] button').trigger('click')
    await flushPromises()
    saveOrder.mockResolvedValue(undefined)
    await wrapper.find('[data-test="save-account-order"]').trigger('click')
    await flushPromises()
    expect(saveOrder).toHaveBeenLastCalledWith([3, 2, 1], 'version-two')
    wrapper.unmount()
  })
  it('does not allow saving when accounts failed to load', async () => {
    getOrder.mockRejectedValue(new Error('offline'))
    const wrapper = create()
    await flushPromises()
    expect(wrapper.find('[data-test="save-account-order"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="alert"]').text()).toContain('iqtest.accountOrder.loadFailed')
    wrapper.unmount()
  })
})
