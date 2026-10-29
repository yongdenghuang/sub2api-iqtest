<template>
  <div class="space-y-3 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-medium">{{ result.account_name }} #{{ result.account_id }}</span>
      <span>{{ t(`iqtest.kind.${result.kind}`) }}</span>
      <span class="badge" :class="resultClass(result)">{{ resultLabel(result) }}</span>
      <span v-if="result.reason" class="text-xs text-gray-500">{{ reasonLabel(result.reason) }}</span>
      <span v-if="review?.status === 'completed' && review.assessment" class="text-xs text-gray-500">{{ t('iqtest.visual.score') }}: {{ result.score ?? 0 }}</span>
    </div>
    <p class="text-xs text-gray-500">{{ formatDateTime(result.created_at) }} · #{{ result.id }}</p>
    <p v-if="result.kind !== 'pelican_svg' && result.detail" class="whitespace-pre-wrap break-words text-gray-600 dark:text-gray-300">{{ result.detail }}</p>
    <div v-if="result.kind === 'pelican_svg'">
      <div class="input-label">{{ t('iqtest.result.svgPreview') }}</div>
      <img
        v-if="svgSrc && !imageFailed"
        :key="result.id"
        :src="svgSrc"
        :alt="t('iqtest.result.svgPreview')"
        data-iq-preview
        class="max-h-[40vh] w-full rounded-lg border border-gray-200 bg-white object-contain dark:border-dark-600"
        @error="imageFailed = true"
      />
      <p v-else class="rounded-lg bg-gray-50 p-4 text-gray-500 dark:bg-dark-900 dark:text-gray-400" role="status">
        {{ t(imageFailed ? 'iqtest.result.svgLoadFailed' : 'iqtest.result.noSvg') }}
      </p>
    </div>
    <slot name="history" />
    <details v-if="result.output" :open="result.kind !== 'pelican_svg'">
      <summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('iqtest.result.output') }}</summary>
      <pre class="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ result.output }}</pre>
    </details>
    <details v-if="result.metrics">
      <summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('iqtest.result.metrics') }}</summary>
      <pre class="mt-2 overflow-auto rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ JSON.stringify(result.metrics, null, 2) }}</pre>
    </details>
    <section data-iq-evaluation v-if="result.kind === 'pelican_svg'" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <p v-if="result.detail" class="mb-3 whitespace-pre-wrap break-words text-gray-600 dark:text-gray-300">{{ result.detail }}</p>
      <p class="font-medium">{{ t('iqtest.visual.title') }}</p>
      <p class="mt-1 text-xs text-gray-500">{{ t('iqtest.visual.standard') }}</p>
      <p v-if="unreviewedSVG(result)" class="mt-2 text-amber-700 dark:text-amber-300">{{ t('iqtest.visual.legacy') }}</p>
      <p v-else-if="review?.status === 'failed'" class="mt-2 text-amber-700 dark:text-amber-300">{{ t('iqtest.visual.reviewFailedHint') }}</p>
      <template v-if="review?.assessment">
        <p class="mt-2 text-xs text-gray-500">{{ review.model }} · {{ t('iqtest.visual.reviewer') }} #{{ review.reviewer_account_id }}</p>
        <dl class="mt-3 space-y-2">
          <div v-for="item in criteria" :key="item.key" class="rounded bg-gray-50 p-2 dark:bg-dark-800">
            <dt class="flex justify-between font-medium"><span>{{ t(`iqtest.visual.${item.key}`) }}</span><span>{{ item.value.score }} / {{ item.max }}</span></dt>
            <dd class="mt-1 text-xs text-gray-600 dark:text-gray-300">{{ item.value.reason }}</dd>
          </div>
        </dl>
        <p v-for="issue in review.assessment.critical_errors" :key="issue" class="mt-2 text-red-600 dark:text-red-400">{{ t(`iqtest.visual.issues.${issue}`) }}</p>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQResult } from '@/api/admin/iqtest'
import { formatDateTime } from '@/utils/format'
import { resultClass, useIQLabels, visualReview, unreviewedSVG } from './labels'
import { svgPreviewSource } from './svgPreview'

const props = defineProps<{ result: IQResult }>()
const { t } = useI18n()
const { resultLabel, reasonLabel } = useIQLabels()
const review = computed(() => visualReview(props.result))
const criteria = computed(() => {
  const a = review.value?.assessment
  if (!a) return []
  return ([['pelican', 25], ['bicycle', 25], ['riding', 30], ['anatomy', 10], ['clarity', 10]] as const).map(([key, max]) => ({ key, max, value: a[key] }))
})
const imageFailed = ref(false)
watch(() => [props.result.id, props.result.output], () => { imageFailed.value = false })
const svgSrc = computed(() => props.result.kind === 'pelican_svg' ? svgPreviewSource(props.result.output ?? '') : '')
</script>
