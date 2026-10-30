import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account } from '@/types'

const api = vi.hoisted(() => ({ generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), applyOAuthCredentials: vi.fn() }))
const notifications = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => notifications }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))

import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const OAuthFlow = defineComponent({
  name: 'OAuthAuthorizationFlow',
  emits: ['generate-url'],
  setup(_, { expose }) {
    const authCode = ref('')
    const oauthState = ref('')
    expose({ authCode, oauthState, inputMethod: 'manual', reset: () => { authCode.value = ''; oauthState.value = '' } })
    return { authCode, oauthState }
  },
  template: '<div><button data-generate @click="$emit(\'generate-url\')">Generate</button><input data-code v-model="authCode"/><input data-state v-model="oauthState"/></div>'
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}

const account = { id: 6, name: 'existing account', platform: 'openai', type: 'oauth', status: 'error', schedulable: false, credentials: {} } as Account
const tokens = { access_token: 'new-access', refresh_token: 'new-refresh', expires_at: 1900000000 }
const restored = { ...account, status: 'active', schedulable: true }

async function ready() {
  const wrapper = mount(ReAuthAccountModal, {
    props: { show: true, account },
    global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' }, Icon: true, OAuthAuthorizationFlow: OAuthFlow } }
  })
  await wrapper.get('[data-generate]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-code]').setValue('fresh-code')
  await wrapper.get('[data-state]').setValue('fresh-state')
  await flushPromises()
  expect(api.generateAuthUrl).toHaveBeenCalled()
  expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeUndefined()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  api.generateAuthUrl.mockResolvedValue({ auth_url: 'https://auth.example.test/authorize?state=fresh-state', session_id: 'fresh-session' })
  api.exchangeCode.mockResolvedValue(tokens)
  api.applyOAuthCredentials.mockResolvedValue(restored)
})

describe('OpenAI reauthorization recovery', () => {
  it('keeps the submit button locked until credentials and account recovery are saved', async () => {
    const saving = deferred<typeof restored>()
    api.applyOAuthCredentials.mockReturnValue(saving.promise)
    const wrapper = await ready()
    const submit = wrapper.get('button.btn-primary')
    await submit.trigger('click')
    await flushPromises()
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click')
    expect(api.exchangeCode).toHaveBeenCalledTimes(1)
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    expect(notifications.showSuccess).not.toHaveBeenCalled()
    saving.resolve(restored)
    await flushPromises()
    expect(wrapper.emitted('reauthorized')?.[0]).toEqual([restored])
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('retries saving without exchanging the already-consumed authorization code', async () => {
    api.applyOAuthCredentials.mockRejectedValueOnce(new Error('cache unavailable')).mockResolvedValueOnce(restored)
    const wrapper = await ready()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.reAuthSavePending')
    expect(wrapper.get('button.btn-primary').text()).toBe('admin.accounts.reAuthRetrySave')
    expect(wrapper.emitted('reauthorized')).toBeUndefined()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(api.exchangeCode).toHaveBeenCalledTimes(1)
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(2)
    expect(api.applyOAuthCredentials).toHaveBeenLastCalledWith(6, { type: 'oauth', credentials: tokens, extra: undefined })
    expect(wrapper.emitted('reauthorized')?.[0]).toEqual([restored])
    wrapper.unmount()
  })

  it('does not save a late OAuth response after closing the dialog', async () => {
    const exchange = deferred<typeof tokens>()
    api.exchangeCode.mockReturnValue(exchange.promise)
    const wrapper = await ready()
    await wrapper.get('button.btn-primary').trigger('click')
    await wrapper.get('button.btn-secondary').trigger('click')
    exchange.resolve(tokens)
    await flushPromises()
    expect(api.applyOAuthCredentials).not.toHaveBeenCalled()
    expect(notifications.showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('discards pending credentials when switching accounts', async () => {
    api.applyOAuthCredentials.mockRejectedValueOnce(new Error('cache unavailable'))
    const wrapper = await ready()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    await wrapper.setProps({ account: { ...account, id: 9 } })
    expect(wrapper.text()).not.toContain('admin.accounts.reAuthSavePending')
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
