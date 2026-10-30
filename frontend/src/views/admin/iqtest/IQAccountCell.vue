<template>
  <!-- IQ-test results of one account, shown as a column of the official account list. -->
  <div class="flex flex-col gap-1">
    <span v-if="!states?.length" class="text-xs text-gray-400">{{ t('iqtest.column.none') }}</span>
    <button
      v-for="s in states"
      :key="s.kind"
      type="button"
      class="flex items-center gap-1.5 rounded px-1 py-0.5 text-left text-xs hover:bg-gray-100 dark:hover:bg-dark-700"
      :title="tooltip(s)"
      @click="openHistory(s)"
    >
      <span class="w-8 shrink-0 text-gray-500 dark:text-gray-400">{{ t(`iqtest.kindShort.${s.kind}`) }}</span>
      <span class="badge" :class="accountStateClass(s)">{{ accountStateLabel(s) }}</span>
      <span v-if="s.consecutive_fails > 0 && s.last_outcome === 'quality_fail'" class="text-red-600 dark:text-red-400">×{{ s.consecutive_fails }}</span>
      <span v-if="s.last_reason !== 'visual_review_required'" class="inline-flex gap-0.5">
        <span
          v-for="(ok, i) in (s.recent || []).slice(-RECENT)"
          :key="i"
          class="inline-block h-2 w-2 rounded-sm"
          :class="ok ? 'bg-green-500' : 'bg-red-500'"
        ></span>
      </span>
    </button>
  </div>

  <IQResultListDialog :show="showList" :title="listTitle" :items="listItems" :loading="listLoading" :preview-latest-svg="previewLatestSvg" @close="closeHistory" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import iqtestAPI from '@/api/admin/iqtest'
import type { IQResult, IQStateRow } from '@/api/admin/iqtest'
import IQResultListDialog from './IQResultListDialog.vue'
import { accountStateClass, useIQLabels } from './labels'

defineProps<{ states?: IQStateRow[] }>()

const RECENT = 6
const { t } = useI18n()
const appStore = useAppStore()
const { outcomeLabel, reasonLabel, accountStateLabel } = useIQLabels()

function tooltip(s: IQStateRow) {
  const parts = [`${t('iqtest.column.last')}: ${outcomeLabel(s.last_outcome)}`]
  if (s.last_reason) parts.push(reasonLabel(s.last_reason))
  parts.push(formatDateTime(s.updated_at))
  return `${t(`iqtest.kind.${s.kind}`)} · ${parts.join(' · ')}`
}

const showList = ref(false)
const listTitle = ref('')
const listItems = ref<IQResult[]>([])
const listLoading = ref(false)

const previewLatestSvg = ref(false)
let historyRequest = 0

function closeHistory() {
  historyRequest++
  showList.value = false
}
onBeforeUnmount(closeHistory)

async function openHistory(s: IQStateRow) {
  const request = ++historyRequest
  previewLatestSvg.value = s.kind === 'pelican_svg'
  listTitle.value = `${s.account_name} #${s.account_id} · ${t(`iqtest.kind.${s.kind}`)}`
  listItems.value = []
  showList.value = true
  listLoading.value = true
  try {
    const items = await iqtestAPI.listAccountResults(s.account_id, s.kind, s.kind === 'pelican_svg' ? 5 : 100)
    if (request === historyRequest) listItems.value = items
  } catch (e) {
    if (request === historyRequest) appStore.showError(extractApiErrorMessage(e, t('iqtest.common.loadFailed')))
  } finally {
    if (request === historyRequest) listLoading.value = false
  }
}
</script>
