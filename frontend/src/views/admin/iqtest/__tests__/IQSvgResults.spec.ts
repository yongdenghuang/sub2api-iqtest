import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { IQResult, IQStateRow } from '@/api/admin/iqtest'
import iqtestAPI from '@/api/admin/iqtest'
import zh from '@/i18n/locales/zh/iqtest'
import IQAccountCell from '../IQAccountCell.vue'
import IQResultListDialog from '../IQResultListDialog.vue'
import IQResultDetail from '../IQResultDetail.vue'
import { svgPreviewSource } from '../svgPreview'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => {
    let value: unknown = zh
    for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
    return typeof value === 'string' ? value : key
  } })
}))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/admin/iqtest', () => ({ default: { listAccountResults: vi.fn(), getResult: vi.fn() } }))

const dialogStub = { props: ['show'], emits: ['close'], template: '<section v-if="show"><button data-close @click="$emit(\'close\')">Close</button><slot /></section>' }
const globalOptions = { stubs: { BaseDialog: dialogStub } }
function result(id: number, output?: string): IQResult {
  return { id, run_id: id, plan_id: 1, account_id: 102, account_name: 'Claude 02', platform: 'anthropic', kind: 'pelican_svg', outcome: 'pass', duration_ms: 1000, created_at: `2026-10-25T${id === 12 ? '16' : '14'}:20:00Z`, output }
}
const svg = (color: string) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle r="20" fill="${color}" /></svg>`
const summary = [result(12), result(11)]
const full = { 12: result(12, svg('red')), 11: result(11, '```svg\n' + svg('blue') + '\n```') }
const state = { account_id: 102, account_name: 'Claude 02', kind: 'pelican_svg', state: 'normal', consecutive_fails: 0, recent: [true, true], last_outcome: 'pass', updated_at: '2026-10-25T16:20:00Z' } as IQStateRow
const imageSource = (wrapper: ReturnType<typeof mount>) => decodeURIComponent(wrapper.get('img').attributes('src'))

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(iqtestAPI.listAccountResults).mockResolvedValue(summary)
  vi.mocked(iqtestAPI.getResult).mockImplementation(async id => full[id as keyof typeof full])
})

describe('account SVG results', () => {
  it('opens the latest real SVG from the account badge, then shows the exact historical row clicked', async () => {
    const wrapper = mount(IQAccountCell, { props: { states: [state] }, global: globalOptions })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(iqtestAPI.listAccountResults).toHaveBeenCalledWith(102, 'pelican_svg', 5)
    expect(iqtestAPI.getResult).toHaveBeenLastCalledWith(12)
    expect(imageSource(wrapper)).toContain('fill="red"')
    await wrapper.findAll('tbody tr')[1].trigger('click')
    await flushPromises()
    expect(iqtestAPI.getResult).toHaveBeenLastCalledWith(11)
    expect(imageSource(wrapper)).toContain('fill="blue"')
    expect(imageSource(wrapper)).not.toContain('```')
    // The view button also works for keyboard activation without a duplicate row request.
    const previousCalls = vi.mocked(iqtestAPI.getResult).mock.calls.length
    await wrapper.findAll('tbody tr')[0].get('button').trigger('click')
    await flushPromises()
    expect(iqtestAPI.getResult).toHaveBeenCalledTimes(previousCalls + 1)
    wrapper.unmount()
  })

  it('places the picture before five recent results and the evaluation last', async () => {
    const items = Array.from({ length: 7 }, (_, index) => result(12 - index))
    const wrapper = mount(IQResultListDialog, { props: { show: true, title: 'SVG', items, loading: false, previewLatestSvg: true }, global: globalOptions })
    await flushPromises()
    expect(wrapper.findAll('[data-iq-history] tbody tr')).toHaveLength(5)
    const preview = wrapper.get('[data-iq-preview]').element
    const history = wrapper.get('[data-iq-history]').element
    const evaluation = wrapper.get('[data-iq-evaluation]').element
    expect(preview.compareDocumentPosition(history) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(history.compareDocumentPosition(evaluation) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(evaluation.parentElement?.lastElementChild).toBe(evaluation)
    wrapper.unmount()
  })

  it('shows no-preview for a failed request instead of an older successful drawing', async () => {
    vi.mocked(iqtestAPI.getResult).mockResolvedValue({ ...result(12), outcome: 'infra_error', reason: 'rate_limited' })
    const wrapper = mount(IQResultListDialog, { props: { show: true, title: 'SVG', items: summary, loading: false, previewLatestSvg: true }, global: globalOptions })
    await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain(zh.iqtest.result.noSvg)
    expect(iqtestAPI.getResult).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('ignores stale detail responses when selecting another result or closing', async () => {
    const pending = new Map<number, (value: IQResult) => void>()
    vi.mocked(iqtestAPI.getResult).mockImplementation(id => new Promise(resolve => { pending.set(id, resolve) }))
    const wrapper = mount(IQResultListDialog, { props: { show: true, title: 'SVG', items: summary, loading: false }, global: globalOptions })
    await wrapper.findAll('tbody tr')[0].trigger('click')
    await wrapper.findAll('tbody tr')[1].trigger('click')
    pending.get(11)!(full[11])
    await flushPromises()
    pending.get(12)!(full[12])
    await flushPromises()
    expect(imageSource(wrapper)).toContain('fill="blue"')
    await wrapper.findAll('tbody tr')[0].trigger('click')
    expect(wrapper.find('img').exists()).toBe(false)
    await wrapper.setProps({ show: false })
    pending.get(12)!(full[12])
    await flushPromises()
    expect(wrapper.findComponent(IQResultDetail).exists()).toBe(false)
    wrapper.unmount()
  })

  it('lets a failed detail request retry without showing the wrong result', async () => {
    vi.mocked(iqtestAPI.getResult).mockRejectedValueOnce(new Error('failed'))
    const wrapper = mount(IQResultListDialog, { props: { show: true, title: 'SVG', items: summary, loading: false, previewLatestSvg: true }, global: globalOptions })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('img').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === zh.iqtest.result.retry)!.trigger('click')
    await flushPromises()
    expect(imageSource(wrapper)).toContain('fill="red"')
    wrapper.unmount()
  })

  it('keeps candy history as a list until a result is selected', async () => {
    vi.mocked(iqtestAPI.listAccountResults).mockResolvedValue([{ ...result(12), kind: 'candy' }])
    const wrapper = mount(IQAccountCell, { props: { states: [{ ...state, kind: 'candy' }] }, global: globalOptions })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(iqtestAPI.getResult).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('SVG image extraction', () => {
  it('extracts nested SVG and excludes surrounding prose', () => {
    const nested = '<svg viewBox="0 0 100 100"><svg><circle r="10" /></svg></svg>'
    const source = decodeURIComponent(svgPreviewSource('Drawing:\n```svg\n' + nested + '\n```\nDone.'))
    expect(source).toContain('xmlns="http://www.w3.org/2000/svg"')
    expect(source.endsWith('</svg></svg>')).toBe(true)
    expect(source).not.toContain('Done.')
  })
  it.each(['', '<svg><path', '<svg><path></svg>', '<svg xmlns="urn:not-svg"></svg>'])('does not preview invalid SVG: %s', output => {
    expect(svgPreviewSource(output)).toBe('')
  })
  it('keeps SVG document markup isolated in an image, never as page HTML', () => {
    const wrapper = mount(IQResultDetail, { props: { result: result(12, '<svg xmlns="http://www.w3.org/2000/svg"><script>window.bad=1</script></svg>') } })
    expect(wrapper.find('img').exists()).toBe(true)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.find('iframe, object, embed').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('visual quality verdicts', () => {
  it('does not present a historical structural 100 as a visual pass', () => {
    const wrapper = mount(IQResultDetail, { props: { result: { ...full[12], score: 100 } } })
    expect(wrapper.text()).toContain(zh.iqtest.visual.unreviewed)
    expect(wrapper.text()).toContain(zh.iqtest.visual.legacy)
    expect(wrapper.get('.flex.flex-wrap').text()).not.toContain('100')
    expect(wrapper.find('.badge-success').exists()).toBe(false)
    wrapper.unmount()
  })
  it('shows visual criteria and critical defects, even when the total is high', () => {
    const assessment = {
      pelican: { score: 23, reason: '头颈可辨' }, bicycle: { score: 23, reason: '实心块遮挡车架' },
      riding: { score: 26, reason: '脚踏连接不清楚' }, anatomy: { score: 9, reason: '严重结构缺陷' },
      clarity: { score: 9, reason: '主体完整' }, critical_errors: ['major_structure_error'], confidence: 'high', summary: '需要改进'
    }
    const wrapper = mount(IQResultDetail, { props: { result: { ...full[12], score: 90, outcome: 'quality_fail', reason: 'visual_mismatch', metrics: { evaluation_version: 'pelican-vision-v1', visual: { status: 'completed', model: 'gpt-6-astra', reviewer_account_id: 2, assessment } } } } })
    expect(wrapper.text()).toContain('视觉评分: 90')
    expect(wrapper.text()).toContain('实心块遮挡车架')
    expect(wrapper.text()).toContain(zh.iqtest.visual.issues.major_structure_error)
    expect(wrapper.find('.badge-danger').exists()).toBe(true)
    wrapper.unmount()
  })
  it('does not show an unreviewed account row as normal or green history', () => {
    const wrapper = mount(IQAccountCell, { props: { states: [{ ...state, last_outcome: 'skipped', last_reason: 'visual_review_required' }] }, global: globalOptions })
    expect(wrapper.get('button').text()).toContain(zh.iqtest.visual.unreviewed)
    expect(wrapper.find('.badge-success, .bg-green-500').exists()).toBe(false)
    wrapper.unmount()
  })
})
