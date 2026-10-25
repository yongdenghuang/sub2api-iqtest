<template>
  <div class="space-y-3 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-medium">{{ result.account_name }} #{{ result.account_id }}</span>
      <span>{{ t(`iqtest.kind.${result.kind}`) }}</span>
      <span class="badge" :class="outcomeClass(result.outcome)">{{ outcomeLabel(result.outcome) }}</span>
      <span v-if="result.reason" class="text-xs text-gray-500">{{ reasonLabel(result.reason) }}</span>
      <span v-if="result.score != null" class="text-xs text-gray-500">{{ t('iqtest.result.score') }}: {{ result.score }}</span>
    </div>
    <p class="text-xs text-gray-500">{{ formatDateTime(result.created_at) }} · #{{ result.id }}</p>
    <p v-if="result.detail" class="whitespace-pre-wrap break-words text-gray-600 dark:text-gray-300">{{ result.detail }}</p>
    <div v-if="result.kind === 'pelican_svg'">
      <div class="input-label">{{ t('iqtest.result.svgPreview') }}</div>
      <img
        v-if="svgSrc && !imageFailed"
        :key="result.id"
        :src="svgSrc"
        :alt="t('iqtest.result.svgPreview')"
        class="max-h-96 w-full rounded-lg border border-gray-200 bg-white object-contain dark:border-dark-600"
        @error="imageFailed = true"
      />
      <p v-else class="rounded-lg bg-gray-50 p-4 text-gray-500 dark:bg-dark-900 dark:text-gray-400" role="status">
        {{ t(imageFailed ? 'iqtest.result.svgLoadFailed' : 'iqtest.result.noSvg') }}
      </p>
    </div>
    <details v-if="result.output" :open="result.kind !== 'pelican_svg'">
      <summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('iqtest.result.output') }}</summary>
      <pre class="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ result.output }}</pre>
    </details>
    <details v-if="result.metrics">
      <summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('iqtest.result.metrics') }}</summary>
      <pre class="mt-2 overflow-auto rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ JSON.stringify(result.metrics, null, 2) }}</pre>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQResult } from '@/api/admin/iqtest'
import { formatDateTime } from '@/utils/format'
import { outcomeClass, useIQLabels } from './labels'
import { svgPreviewSource } from './svgPreview'

const props = defineProps<{ result: IQResult }>()
const { t } = useI18n()
const { outcomeLabel, reasonLabel } = useIQLabels()
const imageFailed = ref(false)
watch(() => [props.result.id, props.result.output], () => { imageFailed.value = false })
const svgSrc = computed(() => props.result.kind === 'pelican_svg' ? svgPreviewSource(props.result.output ?? '') : '')
</script>
