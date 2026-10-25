<template>
  <BaseDialog :show="show" :title="title" width="wide" @close="emit('close')">
    <section v-if="selectedId !== null" class="mb-5 border-b border-gray-100 pb-5 dark:border-dark-700" :aria-busy="detailLoading">
      <p v-if="detailLoading" class="py-6 text-center text-sm text-gray-500" role="status">{{ t('iqtest.result.loading') }}</p>
      <div v-else-if="detailError" class="space-y-2 text-sm">
        <p role="alert" class="text-red-600 dark:text-red-400">{{ detailError }}</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="openResult(selectedId)">{{ t('iqtest.result.retry') }}</button>
      </div>
      <IQResultDetail v-else-if="detail" :result="detail" />
    </section>
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead class="text-left text-xs uppercase text-gray-500">
          <tr>
            <th class="px-3 py-2">{{ t('iqtest.result.time') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.account') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.kind') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.outcome') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.reason') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.duration') }}</th>
            <th class="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="r in items"
            :key="r.id"
            class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
            :class="{ 'bg-primary-50 dark:bg-primary-900/20': selectedId === r.id }"
            @click="openResult(r.id)"
          >
            <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(r.created_at) }}</td>
            <td class="px-3 py-2">{{ r.account_name }} <span class="text-xs text-gray-500">#{{ r.account_id }}</span></td>
            <td class="px-3 py-2">{{ t(`iqtest.kind.${r.kind}`) }}</td>
            <td class="px-3 py-2"><span class="badge" :class="outcomeClass(r.outcome)">{{ outcomeLabel(r.outcome) }}</span></td>
            <td class="px-3 py-2 text-xs">{{ r.reason ? reasonLabel(r.reason) : '' }}</td>
            <td class="px-3 py-2 text-xs text-gray-500">{{ (r.duration_ms / 1000).toFixed(1) }}s</td>
            <td class="px-3 py-2 text-right">
              <button type="button" class="btn btn-secondary btn-sm" :aria-pressed="selectedId === r.id" @click.stop="openResult(r.id)">
                {{ t(r.kind === 'pelican_svg' ? 'iqtest.result.viewSvg' : 'iqtest.result.view') }}
              </button>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td colspan="7" class="px-3 py-6 text-center text-gray-500">{{ loading ? t('iqtest.result.loading') : t('iqtest.events.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <slot name="footer" />
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQResult } from '@/api/admin/iqtest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import IQResultDetail from './IQResultDetail.vue'
import { outcomeClass, useIQLabels } from './labels'

const props = defineProps<{
  show: boolean
  title: string
  items: IQResult[]
  loading: boolean
  previewLatestSvg?: boolean
}>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { outcomeLabel, reasonLabel } = useIQLabels()
const selectedId = ref<number | null>(null)
const detail = ref<IQResult | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
let request = 0

function clearSelection() {
  request++
  selectedId.value = null
  detail.value = null
  detailLoading.value = false
  detailError.value = ''
}

async function openResult(id: number) {
  if (!props.show || !props.items.some(item => item.id === id)) return
  const current = ++request
  selectedId.value = id
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  try {
    const result = await iqtestAPI.getResult(id)
    if (current === request && props.show) detail.value = result
  } catch (e) {
    if (current === request && props.show) detailError.value = extractApiErrorMessage(e, t('iqtest.common.loadFailed'))
  } finally {
    if (current === request) detailLoading.value = false
  }
}

watch(
  () => [props.show, props.loading, props.items, props.previewLatestSvg] as const,
  ([show, loading, items, previewLatest]) => {
    if (!show || loading) {
      clearSelection()
      return
    }
    if (!items.some(item => item.id === selectedId.value)) clearSelection()
    // Account history is returned newest first. Do not silently fall back to an
    // older successful drawing when the latest run failed or has no SVG.
    if (previewLatest && selectedId.value === null && items[0]?.kind === 'pelican_svg') {
      void openResult(items[0].id)
    }
  },
  { immediate: true }
)
onBeforeUnmount(clearSelection)
</script>
