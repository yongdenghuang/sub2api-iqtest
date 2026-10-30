import { useI18n } from 'vue-i18n'
import { accountErrorReason } from '@/utils/accountErrorReason'
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
    resultLabel: (r: IQResult) => r.outcome === 'infra_error' && r.reason ? lookup('iqtest.reason', r.reason === 'auth_error' && accountErrorReason(r.detail) === 'auth_expired' ? 'auth_expired' : r.reason) : unreviewedSVG(r) ? t('iqtest.visual.unreviewed') : r.kind === 'pelican_svg' && r.outcome === 'pass' ? t('iqtest.visual.passed') : t(`iqtest.outcome.${r.outcome}`),
    accountStateLabel: (s: IQStateRow) => {
      if (s.last_reason === 'visual_review_required') return t('iqtest.visual.unreviewed')
      if (s.last_outcome === 'infra_error' || s.last_outcome === 'skipped') return s.last_reason ? lookup('iqtest.reason', s.last_reason) : t(`iqtest.outcome.${s.last_outcome}`)
      if (s.kind !== 'pelican_svg') return lookup('iqtest.state', s.state)
      if (s.last_reason === 'review_error') return t('iqtest.visual.reviewFailed')
      if (s.last_outcome === 'pass') return t('iqtest.visual.passed')
      if (s.last_outcome === 'quality_fail') return t('iqtest.visual.failed')
      return lookup('iqtest.outcome', s.last_outcome)
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
  if (state.kind === 'pelican_svg' || state.last_outcome === 'infra_error' || state.last_outcome === 'skipped') return outcomeClass(state.last_outcome)
  return stateClass(state.state)
}
