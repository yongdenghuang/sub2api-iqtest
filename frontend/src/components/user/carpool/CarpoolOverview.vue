<template>
  <section v-if="groups.length || error || !hideEmpty" class="space-y-4" :aria-label="t('userCarpool.title')">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('userCarpool.title') }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refresh()">
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />{{ t('userCarpool.refresh') }}
      </button>
    </div>
    <p v-if="error" role="alert" class="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('userCarpool.loadError') }}</p>
    <p v-if="!groups.length && !error" role="status" class="rounded-xl border border-gray-200 bg-white p-6 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-800">
      {{ t(loading ? 'userCarpool.loading' : 'userCarpool.empty') }}
    </p>

    <article v-for="g in groups" :key="g.id" class="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800" data-testid="carpool-card">
      <header class="flex items-center justify-between gap-3 px-5 pb-4 pt-5">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-900/30"><Icon name="users" size="lg" /></div>
          <div class="min-w-0">
            <h3 class="break-words text-base font-semibold text-gray-900 dark:text-white">{{ g.name }}</h3>
            <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span class="rounded-md bg-emerald-50 px-1.5 py-0.5 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">OpenAI</span>
              <span>{{ t('userCarpool.sharing', { count: g.member_count }) }} · {{ t(g.allocation_mode === 'manual' ? 'userCarpool.manual' : 'userCarpool.auto') }}</span>
            </div>
          </div>
        </div>
        <span class="badge shrink-0" :class="status(g) === 'synced' ? 'badge-success' : ['delayed', 'pending_reset'].includes(status(g)) ? 'badge-warning' : 'badge-gray'">{{ t(`userCarpool.status.${status(g)}`) }}</span>
      </header>

      <div class="space-y-4 px-5 pb-5">
        <div class="grid grid-cols-2 gap-3 xl:grid-cols-4">
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/50">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(g.allocation_mode === 'manual' ? 'userCarpool.manualTotal' : 'userCarpool.autoTotal') }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ allocation(g, g.total_usd) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.spent', { amount: money(g.used_usd) }) }}</p>
          </div>
          <div class="rounded-xl bg-primary-50 p-3 dark:bg-primary-900/20">
            <p class="text-xs text-primary-700 dark:text-primary-300">{{ t('userCarpool.remaining') }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-primary-700 dark:text-primary-300">{{ allocation(g, g.remaining_usd) }}</p>
            <p class="mt-1 text-xs text-primary-700/80 dark:text-primary-300/80">{{ t('userCarpool.remainingHint') }}</p>
          </div>
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/50">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.myAllocation') }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ allocation(g, self(g)?.allocated_usd) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.commonCap') }}</p>
          </div>
          <div class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/50">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.myUsed') }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ money(self(g)?.used_usd) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.myRemaining', { amount: allocation(g, self(g)?.remaining_usd) }) }}</p>
          </div>
        </div>

        <section :aria-label="t('userCarpool.myWindows')" class="space-y-3">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('userCarpool.myWindows') }}</h4>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.windowScope') }}</span>
          </div>
          <div class="grid gap-4 lg:grid-cols-2">
            <div v-for="w in windows(g)" :key="w.window" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700" :data-window="w.window">
              <div class="mb-4 flex items-center justify-between gap-2">
                <span class="rounded-lg px-2 py-1 text-sm font-semibold" :class="w.window === '5h' ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'">{{ t(`userCarpool.windows.${w.window}`) }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ w.range_source === 'recent' ? t('userCarpool.recentWindow', { window: t(`userCarpool.windows.${w.window}`) }) : t(windowEnded(w) ? 'userCarpool.windowEnded' : 'userCarpool.currentWindow') }}</span>
              </div>
              <dl class="grid grid-cols-2 gap-x-4 gap-y-4">
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.requests') }}</dt>
                  <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white" :title="fullNumber(w.stats?.requests)">{{ compact(w.stats?.requests) }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400" :title="t('userCarpool.tokensHint')">{{ t('userCarpool.tokens') }}</dt>
                  <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white" :title="fullNumber(w.stats?.tokens)">{{ compact(w.stats?.tokens) }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400" :title="t('userCarpool.accountCostHint')">{{ t('userCarpool.accountCost') }}</dt>
                  <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ money(w.stats?.account_cost_usd) }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400" :title="t('userCarpool.userCostHint')">{{ t('userCarpool.userCost') }}</dt>
                  <dd class="mt-1 text-lg font-semibold tabular-nums text-primary-700 dark:text-primary-300">{{ money(w.stats?.user_cost_usd) }}</dd>
                </div>
              </dl>
              <p v-if="!w.stats" class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('userCarpool.windowUnavailable') }}</p>
              <div class="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700">
                <div class="flex items-center justify-between gap-2 text-xs">
                  <span class="text-gray-600 dark:text-gray-300">{{ t('userCarpool.officialShared') }}</span>
                  <span class="tabular-nums text-gray-700 dark:text-gray-200">{{ w.official_used_percent === null ? '—' : t('userCarpool.usedPercent', { percent: w.official_used_percent }) }}</span>
                </div>
                <div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700" role="progressbar" :aria-label="`${t(`userCarpool.windows.${w.window}`)} · ${t('userCarpool.officialShared')}`" :aria-valuenow="w.official_used_percent ?? undefined" :aria-valuemin="0" :aria-valuemax="100">
                  <div class="h-full rounded-full transition-all" :class="(w.official_used_percent ?? 0) >= 90 ? 'bg-amber-500' : 'bg-primary-500'" :style="{ width: `${w.official_used_percent ?? 0}%` }"></div>
                </div>
                <div class="mt-3 space-y-1 text-xs">
                  <p class="font-medium text-primary-700 dark:text-primary-300">{{ windowEnded(w) ? t('userCarpool.windowAwaiting') : !w.reset_at && g.last_sync_at ? t('userCarpool.resetUnavailable') : countdown(w.reset_at) }}</p>
                  <p class="text-gray-500 dark:text-gray-400">{{ t(windowEnded(w) ? 'userCarpool.windowEnd' : 'userCarpool.nextReset') }}：{{ date(w.reset_at) }}</p>
                  <p v-if="w.start_at" class="text-gray-400 dark:text-gray-500">{{ t('userCarpool.windowStart') }}：{{ date(w.start_at) }}</p>
                </div>
              </div>
            </div>
          </div>
          <p v-if="g.allocation_mode !== 'manual'" class="text-xs text-gray-500 dark:text-gray-400">{{ g.upstream_capacity_usd === null ? t('userCarpool.samplingHint') : t('userCarpool.capacity', { amount: money(g.upstream_capacity_usd) }) }}</p>
          <p v-if="['delayed', 'pending_reset', 'adjusting', 'waiting'].includes(status(g))" class="text-xs text-amber-700 dark:text-amber-300">{{ t(`userCarpool.hint.${status(g)}`) }}</p>
        </section>
        <footer class="space-y-1 border-t border-gray-100 pt-3 text-xs leading-relaxed text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <p>{{ t(g.sync_official_reset ? 'userCarpool.syncResetHint' : 'userCarpool.localResetHint') }}</p>
          <p>{{ t('userCarpool.lastSync') }}：{{ date(g.last_sync_at) }} · {{ t('userCarpool.cadence') }}</p>
        </footer>
      </div>
    </article>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getMyCarpools, type MyCarpool, type CarpoolUsageWindow } from '@/api/carpool'

withDefaults(defineProps<{ hideEmpty?: boolean }>(), { hideEmpty: false })
const { t, locale } = useI18n()
const groups = ref<MyCarpool[]>([])
const loading = ref(true)
const error = ref(false)
const now = ref(Date.now())
let controller: AbortController | undefined
let timer: ReturnType<typeof setInterval> | undefined
let clock: ReturnType<typeof setInterval> | undefined
let disposed = false

const money = (value?: number) => value === undefined ? '—' : `$${value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
const allocation = (g: MyCarpool, value?: number) => g.allocation_ready ? money(value) : '—'
const self = (g: MyCarpool) => g.members.find(m => m.is_self)
const fullNumber = (value?: number) => value === undefined ? '—' : value.toLocaleString('en-US')
const compact = (value?: number) => value === undefined ? '—' : new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const windows = (g: MyCarpool): CarpoolUsageWindow[] => ['5h', '7d'].map(window => g.windows?.find(w => w.window === window) ?? { window: window as '5h' | '7d', range_source: 'recent', official_used_percent: null, start_at: null, reset_at: null, stats: null })
const windowEnded = (w: CarpoolUsageWindow) => !!w.reset_at && Date.parse(w.reset_at) <= now.value
const date = (value: string | null) => value ? new Date(value).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US') : '—'
function status(g: MyCarpool): MyCarpool['status'] {
  if (error.value || (g.last_sync_at && now.value - Date.parse(g.last_sync_at) > 180000)) return 'delayed'
  if (g.sync_official_reset && g.next_reset_at && Date.parse(g.next_reset_at) <= now.value && g.status !== 'delayed') return 'pending_reset'
  return g.status
}
function countdown(value: string | null) {
  if (!value) return t('userCarpool.awaitingSync')
  const minutes = Math.ceil((Date.parse(value) - now.value) / 60000)
  if (minutes <= 0) return t('userCarpool.awaitingReset')
  return t('userCarpool.countdown', { days: Math.floor(minutes / 1440), hours: Math.floor(minutes % 1440 / 60), minutes: minutes % 60 })
}
async function refresh() {
  if (controller || disposed) return
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const rows = await getMyCarpools(request.signal)
    if (!disposed) { groups.value = rows; error.value = false; now.value = Date.now() }
  } catch (e) {
    if (!disposed && !request.signal.aborted) {
      const failure = e as { status?: number; response?: { status?: number } }
      if ([401, 403].includes(failure.status ?? failure.response?.status ?? 0)) groups.value = []
      error.value = true
    }
  } finally {
    if (!disposed) loading.value = false
    controller = undefined
  }
}
function onVisible() {
  if (!document.hidden) { now.value = Date.now(); void refresh() }
}
onMounted(() => {
  void refresh()
  timer = setInterval(onVisible, 30000)
  clock = setInterval(() => { if (!document.hidden) now.value = Date.now() }, 10000)
  document.addEventListener('visibilitychange', onVisible)
})
onUnmounted(() => {
  disposed = true
  controller?.abort()
  clearInterval(timer)
  clearInterval(clock)
  document.removeEventListener('visibilitychange', onVisible)
})
</script>
