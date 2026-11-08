<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('carpool.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('carpool.description') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading || busy !== null" @click="refresh()">{{ t('carpool.refresh') }}</button>
      </div>
      <div class="rounded-lg bg-primary-50 p-3 text-sm text-primary-800 dark:bg-primary-900/20 dark:text-primary-200">
        <p>{{ t('carpool.explanation') }}</p>
        <p class="mt-1 text-xs">{{ t('carpool.scope') }}</p>
      </div>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-green-700 dark:text-green-300">{{ notice }}</p>
      <p v-if="!loading && !groups.length && !error" class="py-8 text-center text-gray-500">{{ t('carpool.empty') }}</p>

      <section v-for="g in groups" :key="g.id" class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ g.name }} <span class="text-xs font-normal text-gray-500">#{{ g.id }}</span></h2>
            <p class="mt-1 text-xs text-gray-500">{{ t('carpool.source') }} · {{ g.account_name || '—' }} <span v-if="g.account_id">#{{ g.account_id }}</span></p>
          </div>
          <span class="badge" :class="!g.config.enabled ? 'badge-gray' : g.state.last_error ? 'badge-warning' : 'badge-success'">
            {{ t(!g.config.enabled ? 'carpool.disabled' : g.state.last_error ? 'carpool.paused' : 'carpool.enabled') }}
          </span>
        </div>
        <div class="space-y-4 p-4">
          <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <div><div class="text-xs text-gray-500">{{ t('carpool.members') }}</div><div class="mt-1 text-lg font-semibold">{{ g.members.length }} <span class="text-xs font-normal text-gray-500">{{ t(g.config.allocation_mode === 'manual' ? 'carpool.manual' : 'carpool.auto') }}</span></div></div>
            <div><div class="text-xs text-gray-500">{{ t('carpool.weekly') }}</div><div class="mt-1 text-lg font-semibold text-primary-600">{{ money(g.weekly_limit) }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t('carpool.used') }}</div><div class="mt-1 text-lg font-semibold">{{ validDate(g.state.checked_at) ? `${g.state.percent ?? 0}%` : '—' }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t(g.config.allocation_mode === 'manual' ? 'carpool.totalAllocation' : 'carpool.capacity') }}</div><div class="mt-1 text-lg font-semibold">{{ g.config.allocation_mode === 'manual' ? money(g.weekly_limit * g.members.length) : g.state.estimated ? money(g.state.capacity) : t('carpool.sampling') }}</div></div>
          </div>
          <div class="flex flex-wrap gap-x-6 gap-y-1 text-xs text-gray-500">
            <span>{{ t('carpool.reset') }}：{{ date(g.state.reset_at) }}</span>
            <span>{{ t('carpool.checked') }}：{{ date(g.state.checked_at) }}</span>
          </div>
          <p v-if="g.config.enabled" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-700" :class="g.state.last_error ? 'text-amber-700 dark:text-amber-300' : 'text-gray-600 dark:text-gray-300'">{{ g.state.last_error || g.state.message || t('carpool.waiting') }}</p>

          <form v-if="drafts[g.id]" class="flex flex-wrap items-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700" @submit.prevent="save(g)">
            <label class="flex min-h-10 items-center gap-2 text-sm">
              <input v-model="drafts[g.id].enabled" type="checkbox" :disabled="busy !== null" />{{ t('carpool.enable') }}
            </label>
            <label class="text-xs text-gray-500">
              {{ t('carpool.mode') }}
              <select v-model="drafts[g.id].allocation_mode" class="input mt-1 w-40" :disabled="busy !== null">
                <option value="manual">{{ t('carpool.manual') }}</option>
                <option value="auto">{{ t('carpool.auto') }}</option>
              </select>
            </label>
            <label v-if="drafts[g.id].allocation_mode === 'manual'" class="text-xs text-gray-500">
              {{ t('carpool.manualWeekly') }}
              <input v-model.number="drafts[g.id].manual_weekly_usd" type="number" min="1" max="1000000" step="0.01" class="input mt-1 w-44" :disabled="busy !== null || !drafts[g.id].enabled" required />
            </label>
            <label v-else class="text-xs text-gray-500">
              {{ t('carpool.seed') }}
              <input v-model.number="drafts[g.id].initial_total_usd" type="number" min="1" max="1000000" step="0.01" class="input mt-1 w-48" :disabled="busy !== null || !drafts[g.id].enabled || (g.config.allocation_mode !== 'manual' && g.state.estimated)" :title="t('carpool.seedHint')" required />
            </label>
            <label class="flex min-h-10 items-center gap-2 text-sm">
              <input v-model="drafts[g.id].sync_official_reset" type="checkbox" :disabled="busy !== null" />{{ t('carpool.syncReset') }}
            </label>
            <button class="btn btn-primary" type="submit" :disabled="busy !== null || (drafts[g.id].enabled && !eligible(g))">{{ t('carpool.save') }}</button>
            <button class="btn btn-secondary" type="button" :disabled="busy !== null || !g.config.enabled || !eligible(g)" @click="sync(g)">{{ t('carpool.sync') }}</button>
          </form>
          <p v-if="!eligible(g)" class="text-xs text-amber-700 dark:text-amber-300">{{ t('carpool.ineligible') }}</p>
          <div v-if="drafts[g.id]?.enabled" class="space-y-1 text-xs text-gray-500">
            <p :class="drafts[g.id].allocation_mode === 'auto' ? 'text-amber-700 dark:text-amber-300' : ''">{{ t(drafts[g.id].allocation_mode === 'manual' ? 'carpool.manualHint' : 'carpool.autoHint') }}</p>
            <p>{{ t(drafts[g.id].sync_official_reset ? 'carpool.syncResetHint' : 'carpool.localResetHint') }}</p>
          </div>
          <p v-else class="text-xs text-gray-500">{{ t('carpool.disableHint') }}</p>
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead class="text-xs text-gray-500"><tr><th class="py-2">{{ t('carpool.name') }}</th><th>{{ t('carpool.spent') }}</th><th>{{ t('carpool.remaining') }}</th></tr></thead>
              <tbody>
                <tr v-for="m in g.members" :key="m.id" class="border-t border-gray-100 dark:border-dark-700"><td class="py-2">{{ m.name }} <span class="text-xs text-gray-400">#{{ m.user_id }}</span></td><td>{{ money(m.usage) }}</td><td :class="m.usage >= g.weekly_limit ? 'text-red-600' : ''">{{ money(Math.max(0, g.weekly_limit - m.usage)) }}</td></tr>
                <tr v-if="!g.members.length"><td colspan="3" class="py-3 text-gray-500">{{ t('carpool.noMembers') }}</td></tr>
              </tbody>
            </table>
          </div>
          <p class="text-xs text-gray-500">{{ t('carpool.fairHint') }}</p>
          <details @toggle="loadEvents(g.id, $event)">
            <summary class="cursor-pointer text-sm text-primary-600">{{ t('carpool.history') }}</summary>
            <div class="mt-2 overflow-x-auto">
              <table class="w-full text-left text-xs">
                <thead class="text-gray-500"><tr><th class="py-2">{{ t('carpool.time') }}</th><th>{{ t('carpool.reason') }}</th><th>{{ t('carpool.before') }}</th><th>{{ t('carpool.after') }}</th></tr></thead>
                <tbody><tr v-for="e in events[g.id] || []" :key="e.id" class="border-t border-gray-100 dark:border-dark-700"><td class="py-2">{{ date(e.created_at) }}</td><td>{{ t(eventLabel(e.kind)) }}</td><td>{{ money(e.detail.before_limit) }}</td><td>{{ money(e.detail.after_limit) }}</td></tr></tbody>
              </table>
              <p v-if="events[g.id]?.length === 0" class="py-2 text-xs text-gray-500">{{ t('carpool.noEvents') }}</p>
            </div>
          </details>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { carpoolApi, type CarpoolConfig, type CarpoolEvent, type CarpoolGroup } from '@/api/admin/carpool'

const { t } = useI18n()
const groups = ref<CarpoolGroup[]>([])
const drafts = ref<Record<number, CarpoolConfig>>({})
const events = ref<Record<number, CarpoolEvent[]>>({})
const loading = ref(false)
const busy = ref<number | null>(null)
const error = ref('')
const notice = ref('')
let timer: ReturnType<typeof setInterval> | undefined
const eligible = (g: CarpoolGroup) => g.eligible && g.account_count === 1 && g.account_id > 0 && g.members.length > 0
const money = (value = 0) => `$${value.toFixed(2)}`
const validDate = (value?: string) => !!value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value))
const eventLabels: Record<string, string> = { official_reset: 'carpool.officialReset', manual_limit: 'carpool.manualChange', config: 'carpool.configChange' }
const eventLabel = (kind: string) => eventLabels[kind] || 'carpool.adjust'
const date = (value?: string) => validDate(value) ? new Date(value!).toLocaleString() : '—'
function showError(e: unknown) {
  const detail = e as { response?: { data?: { message?: string } }; message?: string }
  error.value = detail.response?.data?.message || detail.message || t('carpool.error')
}
async function refresh(resetID?: number) {
  if (loading.value) return
  loading.value = true
  try {
    groups.value = await carpoolApi.list()
    for (const g of groups.value) {
      if (!drafts.value[g.id] || g.id === resetID) drafts.value[g.id] = { ...g.config, allocation_mode: g.config.allocation_mode || (g.config.enabled ? 'auto' : 'manual'), manual_weekly_usd: g.config.manual_weekly_usd || g.weekly_limit || 600, sync_official_reset: g.config.sync_official_reset ?? true }
    }
  } catch (e) { showError(e) } finally { loading.value = false }
}
async function save(g: CarpoolGroup) {
  busy.value = g.id; error.value = ''; notice.value = ''
  try {
    await carpoolApi.save(g.id, { ...drafts.value[g.id] })
    notice.value = t(drafts.value[g.id].enabled && drafts.value[g.id].allocation_mode === 'manual' ? 'carpool.manualSaved' : 'carpool.saved')
    if (events.value[g.id]) events.value[g.id] = await carpoolApi.events(g.id)
    await refresh(g.id)
  } catch (e) { showError(e) } finally { busy.value = null }
}
async function sync(g: CarpoolGroup) {
  busy.value = g.id; error.value = ''; notice.value = ''
  try {
    await carpoolApi.sync(g.id)
    await refresh()
    if (events.value[g.id]) events.value[g.id] = await carpoolApi.events(g.id)
    notice.value = t('carpool.synced')
  } catch (e) { showError(e) } finally { busy.value = null }
}
async function loadEvents(id: number, event: Event) {
  if (!(event.target as HTMLDetailsElement).open) return
  try { events.value[id] = await carpoolApi.events(id) } catch (e) { showError(e) }
}
onMounted(() => {
  void refresh()
  timer = setInterval(() => { if (!document.hidden && busy.value === null) void refresh() }, 30000)
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>
