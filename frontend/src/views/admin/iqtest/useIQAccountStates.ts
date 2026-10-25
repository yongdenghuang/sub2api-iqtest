import { ref, watch } from 'vue'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQStateRow } from '@/api/admin/iqtest'

// Matches maxStateAccounts in backend/internal/iqtest/handler.go.
const CHUNK = 500
// The account list is replaced on every small local update; refetch for the
// same accounts only after this long.
const MIN_REFETCH_MS = 30_000

/**
 * Loads the IQ-test states of the accounts currently shown in the account list.
 * Errors are ignored on purpose: the column then just shows "not tested".
 */
export function useIQAccountStates(rows: () => { id: number }[], enabled: () => boolean) {
  const byAccount = ref<Record<number, IQStateRow[]>>({})
  let seq = 0
  let lastKey = ''
  let lastAt = 0

  async function load(ids: number[]) {
    const mine = ++seq
    const next: Record<number, IQStateRow[]> = {}
    for (let i = 0; i < ids.length; i += CHUNK) {
      const states = await iqtestAPI.listStatesByAccounts(ids.slice(i, i + CHUNK))
      for (const s of states) (next[s.account_id] ??= []).push(s)
    }
    if (mine === seq) byAccount.value = next
  }

  watch(
    () => [rows(), enabled()] as const,
    ([list, on]) => {
      if (!on || !list.length) {
        seq++
        lastKey = ''
        byAccount.value = {}
        return
      }
      const ids = list.map((r) => r.id)
      const key = ids.join(',')
      if (key === lastKey && Date.now() - lastAt < MIN_REFETCH_MS) return
      lastKey = key
      lastAt = Date.now()
      load(ids).catch(() => {})
    },
    { immediate: true }
  )

  return byAccount
}
