<template>
  <BaseDialog :show="show" :title="title" width="wide" @close="emit('close')">
    <section v-if="selectedId !== null" class="mb-3" :aria-busy="detailLoading">
      <p v-if="detailLoading" class="py-6 text-center text-sm text-gray-500" role="status">{{ t('iqtest.result.loading') }}</p>
      <div v-else-if="detailError" class="space-y-2 text-sm">
        <p role="alert" class="text-red-600 dark:text-red-400">{{ detailError }}</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="openResult(selectedId)">{{ t('iqtest.result.retry') }}</button>
      </div>
      <IQResultDetail v-else-if="detail" :result="detail">
        <template v-if="detail.kind === 'pelican_svg'" #history>
          <IQResultHistory :items="items" :loading="loading" :selected-id="selectedId" :recent="previewLatestSvg" @select="openResult" />
        </template>
      </IQResultDetail>
    </section>
    <IQResultHistory v-if="!detail || detail.kind !== 'pelican_svg'" :items="items" :loading="loading" :selected-id="selectedId" :recent="previewLatestSvg" @select="openResult" />
    <slot name="footer" />
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/utils/apiError'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQResult } from '@/api/admin/iqtest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import IQResultDetail from './IQResultDetail.vue'
import IQResultHistory from './IQResultHistory.vue'

const props = defineProps<{
  show: boolean
  title: string
  items: IQResult[]
  loading: boolean
  previewLatestSvg?: boolean
}>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
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
