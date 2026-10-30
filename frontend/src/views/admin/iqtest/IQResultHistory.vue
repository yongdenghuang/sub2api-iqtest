<template>
  <section data-iq-history>
    <h3 v-if="recent" class="mb-2 text-sm font-medium">{{ t('iqtest.result.recentFive') }}</h3>
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead class="text-left text-xs uppercase text-gray-500">
          <tr>
            <th class="px-3 py-2">{{ t('iqtest.result.time') }}</th>
            <th v-if="!recent" class="px-3 py-2">{{ t('iqtest.result.account') }}</th>
            <th v-if="!recent" class="px-3 py-2">{{ t('iqtest.result.kind') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.model') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.outcome') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.reason') }}</th>
            <th class="px-3 py-2">{{ t('iqtest.result.duration') }}</th>
            <th class="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="r in visibleItems"
            :key="r.id"
            class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
            :class="{ 'bg-primary-50 dark:bg-primary-900/20': selectedId === r.id }"
            @click="emit('select', r.id)"
          >
            <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(r.created_at) }}</td>
            <td v-if="!recent" class="px-3 py-2">{{ r.account_name }} <span class="text-xs text-gray-500">#{{ r.account_id }}</span></td>
            <td v-if="!recent" class="px-3 py-2">{{ t(`iqtest.kind.${r.kind}`) }}</td>
            <td class="px-3 py-2 text-xs font-mono" data-iq-model>{{ r.model || t('iqtest.result.modelUnknown') }}</td>
            <td class="px-3 py-2"><span class="badge" :class="resultClass(r)">{{ resultLabel(r) }}</span></td>
            <td class="px-3 py-2 text-xs">{{ r.reason ? reasonLabel(r.reason) : '' }}</td>
            <td class="px-3 py-2 text-xs text-gray-500">{{ (r.duration_ms / 1000).toFixed(1) }}s</td>
            <td class="px-3 py-2 text-right">
              <button type="button" class="btn btn-secondary btn-sm" :aria-pressed="selectedId === r.id" @click.stop="emit('select', r.id)">
                {{ t(r.kind === 'pelican_svg' ? 'iqtest.result.viewSvg' : 'iqtest.result.view') }}
              </button>
            </td>
          </tr>
          <tr v-if="!visibleItems.length">
            <td :colspan="recent ? 6 : 8" class="px-3 py-6 text-center text-gray-500">{{ loading ? t('iqtest.result.loading') : t('iqtest.events.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQResult } from '@/api/admin/iqtest'
import { formatDateTime } from '@/utils/format'
import { resultClass, useIQLabels } from './labels'
const props = defineProps<{ items: IQResult[]; selectedId: number | null; loading: boolean; recent?: boolean }>()
const emit = defineEmits<{ select: [id: number] }>()
const { t } = useI18n()
const { resultLabel, reasonLabel } = useIQLabels()
const visibleItems = computed(() => props.recent ? props.items.slice(0, 5) : props.items)
</script>
