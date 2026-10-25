<template>
  <span class="badge" :class="statusClass" :title="t('iqtest.intelligence.hint')">
    {{ t(`iqtest.intelligence.${status}`) }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQStateRow } from '@/api/admin/iqtest'

const props = defineProps<{ states?: IQStateRow[] }>()
const { t } = useI18n()

// This column reports the latest candy verdict, independently of the policy's
// consecutive-failure flag and the SVG check. Request failures are not evidence
// of degraded reasoning quality.
const status = computed(() => {
  const candy = props.states?.find(state => state.kind === 'candy')
  if (candy?.last_outcome === 'pass') return 'normal'
  if (candy?.last_outcome === 'quality_fail') return 'degraded'
  return 'unknown'
})
const statusClass = computed(() => ({
  normal: 'badge-success',
  degraded: 'badge-danger',
  unknown: 'badge-gray'
}[status.value]))
</script>
