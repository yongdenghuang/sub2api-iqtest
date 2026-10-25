<template>
  <!-- Result list (a run or one account's history) -->
  <BaseDialog :show="show" :title="title" width="wide" @close="emit('close')">
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
          <tr v-for="r in items" :key="r.id" class="border-t border-gray-100 dark:border-dark-700">
            <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(r.created_at) }}</td>
            <td class="px-3 py-2">{{ r.account_name }} <span class="text-xs text-gray-500">#{{ r.account_id }}</span></td>
            <td class="px-3 py-2">{{ t(`iqtest.kind.${r.kind}`) }}</td>
            <td class="px-3 py-2"><span class="badge" :class="outcomeClass(r.outcome)">{{ outcomeLabel(r.outcome) }}</span></td>
            <td class="px-3 py-2 text-xs">{{ r.reason ? reasonLabel(r.reason) : '' }}</td>
            <td class="px-3 py-2 text-xs text-gray-500">{{ (r.duration_ms / 1000).toFixed(1) }}s</td>
            <td class="px-3 py-2 text-right">
              <button type="button" class="btn btn-secondary btn-sm" @click="openResult(r.id)">{{ t('iqtest.result.view') }}</button>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td colspan="7" class="px-3 py-6 text-center text-gray-500">{{ loading ? '…' : t('iqtest.events.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-if="pagination && pagination.total > 0"
      :page="pagination.page"
      :page-size="pagination.size"
      :total="pagination.total"
      :show-page-size-selector="false"
      @update:page="emit('page', $event)"
    />
  </BaseDialog>

  <!-- Result detail -->
  <BaseDialog :show="!!detail" :title="t('iqtest.result.detail')" width="wide" :z-index="60" @close="detail = null">
    <div v-if="detail" class="space-y-3 text-sm">
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-medium">{{ detail.account_name }} #{{ detail.account_id }}</span>
        <span>{{ t(`iqtest.kind.${detail.kind}`) }}</span>
        <span class="badge" :class="outcomeClass(detail.outcome)">{{ outcomeLabel(detail.outcome) }}</span>
        <span v-if="detail.reason" class="text-xs text-gray-500">{{ reasonLabel(detail.reason) }}</span>
        <span v-if="detail.score" class="text-xs text-gray-500">{{ t('iqtest.result.score') }}: {{ detail.score }}</span>
      </div>
      <p v-if="detail.detail" class="whitespace-pre-wrap break-words text-gray-600 dark:text-gray-300">{{ detail.detail }}</p>
      <div v-if="svgSrc">
        <div class="input-label">{{ t('iqtest.result.svgPreview') }}</div>
        <img :src="svgSrc" alt="svg preview" class="max-h-96 w-full rounded-lg border border-gray-200 bg-white object-contain dark:border-dark-600" />
      </div>
      <div v-if="detail.output">
        <div class="input-label">{{ t('iqtest.result.output') }}</div>
        <pre class="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ detail.output }}</pre>
      </div>
      <div v-if="detail.metrics">
        <div class="input-label">{{ t('iqtest.result.metrics') }}</div>
        <pre class="overflow-auto rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ JSON.stringify(detail.metrics, null, 2) }}</pre>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQResult } from '@/api/admin/iqtest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { outcomeClass, useIQLabels } from './labels'

const props = defineProps<{ show: boolean; title: string; items: IQResult[]; loading: boolean; pagination?: { page: number; size: number; total: number } | null }>()
const emit = defineEmits<{ close: []; page: [page: number] }>()

const { t } = useI18n()
const appStore = useAppStore()
const { outcomeLabel, reasonLabel } = useIQLabels()

const detail = ref<IQResult | null>(null)

let detailRequest = 0
watch(() => props.show, (show) => {
  if (!show) {
    detailRequest++
    detail.value = null
  }
})

async function openResult(id: number) {
  const request = ++detailRequest
  try {
    const result = await iqtestAPI.getResult(id)
    if (request === detailRequest && props.show) detail.value = result
  } catch (e) {
    appStore.showError(extractApiErrorMessage(e, t('iqtest.common.loadFailed')))
  }
}

// An <img> never runs scripts, so previewing model-produced SVG this way is safe.
const svgSrc = computed(() => {
  const d = detail.value
  if (!d || d.kind !== 'pelican_svg' || !d.output) return ''
  const i = d.output.indexOf('<svg')
  if (i < 0) return ''
  return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(d.output.slice(i))
})
</script>
