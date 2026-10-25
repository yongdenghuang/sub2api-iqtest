import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { IQStateRow } from '@/api/admin/iqtest'
import zh from '@/i18n/locales/zh/iqtest'
import IQStatusCell from '../IQStatusCell.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => zh.iqtest.intelligence[key.split('.').pop() as keyof typeof zh.iqtest.intelligence]
  })
}))

function state(kind: IQStateRow['kind'], last_outcome: IQStateRow['last_outcome'], policyState: IQStateRow['state'] = 'normal'): IQStateRow {
  return { kind, last_outcome, state: policyState } as IQStateRow
}
function render(states?: IQStateRow[]) {
  return mount(IQStatusCell, {
    props: { states }
  })
}

describe('account IQ status from candy results', () => {
  it('shows normal for a candy pass even with an SVG failure or an old policy flag', () => {
    const wrapper = render([state('pelican_svg', 'quality_fail', 'flagged'), state('candy', 'pass', 'flagged')])
    expect(wrapper.text()).toBe('正常')
    expect(wrapper.classes()).toContain('badge-success')
  })
  it('shows red degraded on the first candy quality failure, without waiting for the policy threshold', () => {
    const wrapper = render([state('candy', 'quality_fail', 'suspect'), state('pelican_svg', 'pass')])
    expect(wrapper.text()).toBe('降智')
    expect(wrapper.classes()).toContain('badge-danger')
  })
  it.each(['infra_error', 'skipped'] as const)('does not label %s as degraded', outcome => {
    const wrapper = render([state('candy', outcome, 'flagged')])
    expect(wrapper.text()).toBe('待确认')
    expect(wrapper.classes()).toContain('badge-gray')
  })
  it('does not infer normal or degraded from missing candy data', () => {
    for (const states of [undefined, [], [state('pelican_svg', 'pass')]]) {
      expect(render(states).text()).toBe('待确认')
    }
  })
  it('updates when refreshed account states arrive', async () => {
    const wrapper = render()
    await wrapper.setProps({ states: [state('candy', 'quality_fail')] })
    expect(wrapper.text()).toBe('降智')
    await wrapper.setProps({ states: [state('candy', 'pass')] })
    expect(wrapper.text()).toBe('正常')
  })
})
