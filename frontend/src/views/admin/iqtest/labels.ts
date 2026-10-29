import { useI18n } from 'vue-i18n'
import type { IQOutcome, IQResult, IQStateRow, IQVisualReview } from '@/api/admin/iqtest'

export function outcomeClass(o?: IQOutcome) {
  switch (o) {
    case 'pass':
      return 'badge-success'
    case 'quality_fail':
      return 'badge-danger'
    case 'infra_error':
      return 'badge-warning'
    default:
      return 'badge-gray'
  }
}

export function stateClass(s: string) {
  return s === 'flagged' ? 'badge-danger' : s === 'suspect' ? 'badge-warning' : 'badge-success'
}

/** Translated labels that fall back to the raw code for values the locale does not know. */
export function useIQLabels() {
  const { t } = useI18n()
  function lookup(prefix: string, v: string) {
    const key = `${prefix}.${v}`
    const s = t(key)
    return s === key ? v : s
  }
  return {
    resultLabel: (r: IQResult) => unreviewedSVG(r) ? t('iqtest.visual.unreviewed') : r.kind === 'pelican_svg' && r.outcome === 'pass' ? t('iqtest.visual.passed') : t(`iqtest.outcome.${r.outcome}`),
    accountStateLabel: (s: IQStateRow) => {
      if (s.kind !== 'pelican_svg') return lookup('iqtest.state', s.state)
      if (s.last_reason === 'visual_review_required') return t('iqtest.visual.unreviewed')
      if (s.last_reason === 'review_error') return t('iqtest.visual.reviewFailed')
      if (s.last_outcome === 'pass') return t('iqtest.visual.passed')
      if (s.last_outcome === 'quality_fail') return t('iqtest.visual.failed')
      return t(`iqtest.outcome.${s.last_outcome}`)
    },
    outcomeLabel: (o?: string) => (o ? t(`iqtest.outcome.${o}`) : '—'),
    reasonLabel: (r: string) => lookup('iqtest.reason', r),
    stateLabel: (s: string) => lookup('iqtest.state', s),
    triggerLabel: (s: string) => lookup('iqtest.run.triggers', s),
    statusLabel: (s: string) => lookup('iqtest.run.statuses', s)
  }
}

export function visualReview(result: IQResult): IQVisualReview | undefined {
  const metrics = result.metrics as { evaluation_version?: string; visual?: IQVisualReview } | null
  return result.kind === 'pelican_svg' && metrics?.evaluation_version === 'pelican-vision-v1' ? metrics.visual : undefined
}
export function unreviewedSVG(result: IQResult): boolean {
  return result.kind === 'pelican_svg' && (result.reason === 'visual_review_required' || (result.outcome === 'pass' && (visualReview(result)?.status !== 'completed' || !visualReview(result)?.assessment)))
}
export function resultClass(result: IQResult) { return unreviewedSVG(result) ? 'badge-gray' : outcomeClass(result.outcome) }
export function accountStateClass(state: IQStateRow) {
  if (state.kind === 'pelican_svg') return outcomeClass(state.last_outcome)
  return stateClass(state.state)
}
