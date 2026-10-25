import { useI18n } from 'vue-i18n'
import type { IQOutcome } from '@/api/admin/iqtest'

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
    outcomeLabel: (o?: string) => (o ? t(`iqtest.outcome.${o}`) : '—'),
    reasonLabel: (r: string) => lookup('iqtest.reason', r),
    stateLabel: (s: string) => lookup('iqtest.state', s),
    triggerLabel: (s: string) => lookup('iqtest.run.triggers', s),
    statusLabel: (s: string) => lookup('iqtest.run.statuses', s)
  }
}
