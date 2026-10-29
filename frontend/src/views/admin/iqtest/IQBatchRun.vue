<template>
  <section v-if="submitting || runId || error" class="iq-batch-strip border-b border-primary-100 bg-primary-50 px-2 py-1 text-xs dark:border-primary-800 dark:bg-primary-900/20" aria-live="polite">
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
      <span class="font-medium" :title="t('iqtest.batch.hint')">{{ t('iqtest.batch.title') }}<template v-if="runId"> #{{ runId }}</template></span>
      <span v-if="submitting">{{ t('iqtest.batch.submitting') }}</span>
      <template v-else-if="run">
        <span>{{ statusLabel(run.status) }} · {{ t('iqtest.batch.progress', { done: completed, total: run.accounts * 2 }) }}</span>
        <span class="text-green-700 dark:text-green-400">{{ t('iqtest.outcome.pass') }} {{ run.pass }}</span>
        <span class="text-red-600">{{ t('iqtest.batch.failed') }} {{ run.quality_fail }}</span>
        <span class="text-amber-700 dark:text-amber-400">{{ t('iqtest.batch.errors') }} {{ run.infra_error }}</span>
        <span class="text-gray-500">{{ t('iqtest.batch.skipped') }} {{ run.skipped }}</span>
      </template>
      <div class="ml-auto flex items-center gap-2">
        <button v-if="run" type="button" class="btn btn-secondary btn-sm" @click="openResults">{{ t('iqtest.result.view') }}</button>
        <RouterLink to="/admin/iqtest?tab=runs" class="text-primary-700 dark:text-primary-300">{{ t('iqtest.accountTools.history') }}</RouterLink>
        <button v-if="!busy" type="button" class="rounded p-1 text-gray-500 hover:bg-primary-100 dark:hover:bg-primary-900" :aria-label="t('iqtest.batch.dismiss')" :title="t('iqtest.batch.dismiss')" @click="dismiss">
          <Icon name="x" size="sm" />
        </button>
      </div>
    </div>
    <progress v-if="run" :class="busy ? 'mt-1 block h-1 w-full accent-teal-600' : 'sr-only'" :value="completed" :max="Math.max(1, run.accounts * 2)" :aria-label="t('iqtest.batch.title')" />
    <div v-if="error" class="flex flex-wrap items-center gap-2 py-1 text-red-600" role="alert">
      <span>{{ error }}</span>
      <button v-if="runId" type="button" class="underline" @click="poll">{{ t('iqtest.batch.retryProgress') }}</button>
    </div>
  </section>
  <IQResultListDialog :show="showResults" :title="`${t('iqtest.batch.title')} #${runId}`" :items="results" :loading="resultsLoading" @close="showResults = false">
    <template #footer>
      <p v-if="resultsError" class="mt-3 text-sm text-red-600" role="alert">{{ resultsError }} <button type="button" class="underline" @click="loadResults">{{ t('iqtest.result.retry') }}</button></p>
      <Pagination v-if="resultsTotal > 0" :page="resultPage" :page-size="50" :show-page-size-selector="false" :total="resultsTotal" @update:page="changePage" />
    </template>
  </IQResultListDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { DEFAULT_TEST_MODEL } from '@/constants/accountTestModel'
import iqtestAPI, { type IQResult, type IQRun } from '@/api/admin/iqtest'
import IQResultListDialog from './IQResultListDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useIQLabels } from './labels'
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { statusLabel } = useIQLabels()
const storageKey = 'iqtest-account-batch-run'
const runId = ref<number | null>(null)
const run = ref<IQRun | null>(null)
const submitting = ref(false)
const error = ref('')
const busy = computed(() => submitting.value || (runId.value !== null && (!run.value || run.value.status === 'running')))
const completed = computed(() => run.value ? run.value.pass + run.value.quality_fail + run.value.infra_error + run.value.skipped : 0)
const showResults = ref(false)
const results = ref<IQResult[]>([])
const resultsLoading = ref(false)
const resultsError = ref('')
const resultsTotal = ref(0)
const resultPage = ref(1)
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let polling = false
let resultsSequence = 0
function dismiss() {
  if (busy.value) return
  clearTimeout(timer)
  runId.value = null
  run.value = null
  error.value = ''
  showResults.value = false
  resultsSequence++
  try { sessionStorage.removeItem(storageKey) } catch { /* optional */ }
}
function remember(id: number) {
  try { sessionStorage.setItem(storageKey, String(id)) } catch { /* storage may be disabled */ }
}
async function start(ids: number[]) {
  if (busy.value || !ids.length) return
  submitting.value = true
  error.value = ''
  try {
    const response = await iqtestAPI.runAdHoc({ account_ids: [...new Set(ids)], checks: [{ kind: 'candy', model: DEFAULT_TEST_MODEL }, { kind: 'pelican_svg', model: DEFAULT_TEST_MODEL }] })
    if (disposed) return
    runId.value = response.run_id
    run.value = null
    remember(response.run_id)
    showResults.value = false
    results.value = []
    await poll()
  } catch (e) {
    // Never retry a POST automatically: the server may have accepted it.
    if (!disposed) error.value = (e as { response?: { status?: number } }).response?.status === 400
      ? extractApiErrorMessage(e, t('iqtest.batch.submitFailed')) : t('iqtest.batch.submitFailed')
  } finally {
    submitting.value = false
  }
}
async function poll() {
  if (!runId.value || disposed || polling) return
  clearTimeout(timer)
  polling = true
  const id = runId.value
  try {
    const data = await iqtestAPI.getRun(id)
    if (disposed || runId.value !== id) return
    const changed = !run.value || completed.value !== data.pass + data.quality_fail + data.infra_error + data.skipped || run.value.status !== data.status
    run.value = data
    error.value = ''
    if (changed) {
      emit('updated')
      if (showResults.value) void loadResults()
    }
  } catch (e) {
    if (disposed) return
    if ((e as { response?: { status?: number } }).response?.status === 404) {
      runId.value = null
      run.value = null
      try { sessionStorage.removeItem(storageKey) } catch { /* optional */ }
    }
    error.value = t('iqtest.batch.progressFailed')
  } finally {
    polling = false
    if (!disposed && runId.value && (!run.value || run.value.status === 'running')) timer = setTimeout(poll, error.value ? 10000 : 3000)
  }
}
async function loadResults() {
  if (!runId.value) return
  const seq = ++resultsSequence
  resultsLoading.value = true
  resultsError.value = ''
  try {
    const data = await iqtestAPI.listRunResults(runId.value, resultPage.value, 50)
    if (disposed || seq !== resultsSequence) return
    results.value = data.items
    resultsTotal.value = data.total
  } catch {
    if (!disposed && seq === resultsSequence) resultsError.value = t('iqtest.common.loadFailed')
  } finally {
    if (!disposed && seq === resultsSequence) resultsLoading.value = false
  }
}
function openResults() { showResults.value = true; resultPage.value = 1; void loadResults() }
function changePage(page: number) { resultPage.value = page; void loadResults() }
onMounted(() => {
  try {
    const id = Number(sessionStorage.getItem(storageKey))
    if (Number.isSafeInteger(id) && id > 0) { runId.value = id; void poll() }
  } catch { /* storage may be disabled */ }
})
onBeforeUnmount(() => { disposed = true; clearTimeout(timer); resultsSequence++ })
defineExpose({ start, busy })
</script>
