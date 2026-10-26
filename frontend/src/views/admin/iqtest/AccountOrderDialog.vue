<template>
  <BaseDialog :show="true" :title="t('iqtest.accountOrder.title')" width="normal"
    :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('iqtest.accountOrder.hint') }}</p>
    <div v-if="error" role="alert" class="mb-3 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
      {{ error }}
      <button type="button" class="ml-2 underline" :disabled="loading || saving" @click="load">{{ t('common.refresh') }}</button>
    </div>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-else-if="!accounts.length && !error">{{ t('empty.noData') }}</p>
    <VueDraggable v-else v-model="accounts" :animation="180" handle=".account-order-handle"
      :disabled="saving || stale" class="space-y-2" data-test="account-order-list">
      <div v-for="(account, index) in accounts" :key="account.id" :data-account-id="account.id"
        class="flex items-center gap-3 rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800">
        <span class="account-order-handle cursor-grab touch-none text-gray-400 active:cursor-grabbing" :title="t('iqtest.accountOrder.drag')">
          <Icon name="menu" size="md" />
        </span>
        <span class="w-6 shrink-0 text-center text-sm text-gray-400">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium text-gray-900 dark:text-gray-100">{{ account.name }}</p>
          <p class="text-xs text-gray-500">#{{ account.id }} · {{ account.platform }}</p>
        </div>
        <button type="button" class="rounded p-2 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700"
          :disabled="index === 0 || saving || stale" :aria-label="t('iqtest.accountOrder.moveUp', { name: account.name })"
          data-test="move-up" @click="move(index, -1)">↑</button>
        <button type="button" class="rounded p-2 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700"
          :disabled="index === accounts.length - 1 || saving || stale" :aria-label="t('iqtest.accountOrder.moveDown', { name: account.name })"
          data-test="move-down" @click="move(index, 1)">↓</button>
      </div>
    </VueDraggable>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-primary" :disabled="loading || saving || stale || !revision || !accounts.length"
        data-test="save-account-order" @click="save">{{ saving ? t('common.saving') : t('iqtest.accountOrder.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { iqtestAPI, type AccountDisplayOrder } from '@/api/admin/iqtest'
import { useAppStore } from '@/stores/app'

const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<AccountDisplayOrder['accounts']>([])
const revision = ref('')
const loading = ref(true)
const saving = ref(false)
const stale = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  revision.value = ''
  try {
    const result = await iqtestAPI.accountOrder()
    accounts.value = result.accounts
    revision.value = result.revision
    stale.value = false
  } catch {
    error.value = t('iqtest.accountOrder.loadFailed')
  } finally {
    loading.value = false
  }
}

function close() {
  if (!saving.value) emit('close')
}

function move(index: number, delta: number) {
  if (saving.value || stale.value) return
  const target = index + delta
  if (target < 0 || target >= accounts.value.length) return
  const next = [...accounts.value]
  const [account] = next.splice(index, 1)
  next.splice(target, 0, account)
  accounts.value = next
}

async function save() {
  if (loading.value || saving.value || stale.value || !revision.value || !accounts.value.length) return
  saving.value = true
  error.value = ''
  try {
    await iqtestAPI.saveAccountOrder(accounts.value.map(a => a.id), revision.value)
    appStore.showSuccess(t('iqtest.accountOrder.saved'))
    emit('saved')
  } catch (cause) {
    const status = (cause as { response?: { status?: number } }).response?.status
    // A timeout may have committed on the server. Reload before another save
    // instead of replaying a stale order over another administrator's changes.
    stale.value = true
    error.value = t(status === 409 ? 'iqtest.accountOrder.conflict' : 'iqtest.accountOrder.saveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
