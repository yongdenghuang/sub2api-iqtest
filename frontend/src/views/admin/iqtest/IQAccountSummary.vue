<template>
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
    <div v-if="summary && !error" class="flex flex-wrap items-center gap-5" :title="t('iqtest.overview.hint')">
      <span>{{ t('iqtest.overview.total', { count: summary.total }) }}</span>
      <span class="text-red-600 dark:text-red-400">{{ t('iqtest.overview.quality', { count: summary.quality_fail }) }}</span>
      <span class="text-amber-600 dark:text-amber-400">{{ t('iqtest.overview.request', { count: summary.infra_error }) }}</span>
    </div>
    <div v-else-if="error" class="flex items-center gap-2" role="alert">
      <span class="text-red-600">{{ t('iqtest.overview.failed') }}</span>
      <button type="button" class="text-primary-600" @click="load">{{ t('iqtest.result.retry') }}</button>
    </div>
    <span v-else role="status">{{ t('iqtest.result.loading') }}</span>
    <span class="text-gray-500">{{ t('iqtest.overview.note') }}</span>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import iqtestAPI, { type IQAccountSummary } from '@/api/admin/iqtest'
const props = defineProps<{ refreshToken: number }>()
const { t } = useI18n()
const summary = ref<IQAccountSummary | null>(null)
const error = ref(false)
let sequence = 0
async function load() {
  const current = ++sequence
  try {
    const data = await iqtestAPI.summary()
    if (current === sequence) { summary.value = data; error.value = false }
  } catch {
    if (current === sequence) error.value = true
  }
}
watch(() => props.refreshToken, load, { immediate: true })
onBeforeUnmount(() => { sequence++ })
</script>
