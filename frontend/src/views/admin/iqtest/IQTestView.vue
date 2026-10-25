<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('iqtest.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('iqtest.description') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">
            {{ t('iqtest.common.refresh') }}
          </button>
          <button v-if="tab === 'plans'" type="button" class="btn btn-primary" @click="openCreate">
            {{ t('iqtest.plan.create') }}
          </button>
        </div>
      </div>

      <div class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="item in tabs"
          :key="item"
          type="button"
          class="-mb-px border-b-2 px-4 py-2 text-sm font-medium"
          :class="tab === item
            ? 'border-primary-600 text-primary-600 dark:text-primary-400'
            : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'"
          @click="setTab(item)"
        >
          {{ t(`iqtest.tabs.${item}`) }}
        </button>
      </div>

      <!-- Flagged accounts -->
      <section v-if="tab === 'flagged'" class="space-y-4">
        <div class="flex flex-wrap items-center gap-4">
          <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="stateFilter.flagged" type="checkbox" @change="loadStates(1)" />
            {{ t('iqtest.flagged.onlyFlagged') }}
          </label>
          <select v-model="stateFilter.kind" class="input w-56" @change="loadStates(1)">
            <option value="">{{ t('iqtest.flagged.allKinds') }}</option>
            <option value="candy">{{ t('iqtest.kind.candy') }}</option>
            <option value="pelican_svg">{{ t('iqtest.kind.pelican_svg') }}</option>
          </select>
        </div>
        <div class="overflow-x-auto rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800">
          <table class="min-w-full text-sm">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-3 py-2">{{ t('iqtest.result.account') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.kind') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.tabs.flagged') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.flagged.consecutiveFails') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.flagged.recent') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.outcome') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.time') }}</th>
                <th class="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!states.length">
                <td colspan="8" class="px-3 py-8 text-center text-gray-500">{{ t('iqtest.flagged.empty') }}</td>
              </tr>
              <tr v-for="s in states" :key="s.account_id + s.kind" class="border-t border-gray-100 dark:border-dark-700">
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900 dark:text-white">{{ s.account_name }}</div>
                  <div class="text-xs text-gray-500">#{{ s.account_id }} · {{ s.platform }}</div>
                </td>
                <td class="px-3 py-2">{{ t(`iqtest.kind.${s.kind}`) }}</td>
                <td class="px-3 py-2"><span class="badge" :class="stateClass(s.state)">{{ t(`iqtest.state.${s.state}`) }}</span></td>
                <td class="px-3 py-2">{{ s.consecutive_fails }}</td>
                <td class="px-3 py-2">
                  <span class="inline-flex gap-0.5">
                    <span
                      v-for="(ok, i) in s.recent || []"
                      :key="i"
                      class="inline-block h-3 w-3 rounded-sm"
                      :class="ok ? 'bg-green-500' : 'bg-red-500'"
                    ></span>
                  </span>
                </td>
                <td class="px-3 py-2">
                  <span class="badge" :class="outcomeClass(s.last_outcome)">{{ outcomeLabel(s.last_outcome) }}</span>
                  <span v-if="s.last_reason" class="ml-1 text-xs text-gray-500">{{ reasonLabel(s.last_reason) }}</span>
                </td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(s.updated_at) }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-right">
                  <button type="button" class="btn btn-secondary btn-sm" @click="openAccountHistory(s)">
                    {{ t('iqtest.flagged.history') }}
                  </button>
                  <button
                    v-if="s.state !== 'normal'"
                    type="button"
                    class="btn btn-secondary btn-sm ml-1"
                    @click="clearFlag(s)"
                  >
                    {{ t('iqtest.flagged.clear') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination
          v-if="statePage.total > 0"
          :page="statePage.page"
          :total="statePage.total"
          :page-size="statePage.size"
          @update:page="(p: number) => loadStates(p)"
          @update:pageSize="(n: number) => { statePage.size = n; loadStates(1) }"
        />
      </section>

      <!-- Plans -->
      <section v-if="tab === 'plans'" class="space-y-4">
        <div class="overflow-x-auto rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800">
          <table class="min-w-full text-sm">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-3 py-2">{{ t('iqtest.plan.name') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.plan.checks') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.plan.schedule') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.plan.scope') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.plan.nextRun') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.plan.lastRun') }}</th>
                <th class="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!plans.length">
                <td colspan="7" class="px-3 py-8 text-center text-gray-500">{{ t('iqtest.plan.empty') }}</td>
              </tr>
              <tr v-for="p in plans" :key="p.id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="px-3 py-2">
                  <span class="font-medium text-gray-900 dark:text-white">{{ p.name }}</span>
                  <span class="badge ml-2" :class="p.enabled ? 'badge-success' : 'badge-gray'">
                    {{ p.enabled ? t('iqtest.plan.enabled') : t('iqtest.plan.manualOnly') }}
                  </span>
                </td>
                <td class="px-3 py-2">{{ p.checks.map((c) => t(`iqtest.kind.${c.kind}`)).join(' / ') }}</td>
                <td class="px-3 py-2">{{ scheduleText(p) }}</td>
                <td class="px-3 py-2">{{ scopeText(p) }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ p.enabled && p.next_run_at ? formatDateTime(p.next_run_at) : t('iqtest.plan.never') }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ p.last_run_at ? formatDateTime(p.last_run_at) : t('iqtest.plan.never') }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-right">
                  <button type="button" class="btn btn-secondary btn-sm" @click="runPlan(p)">{{ t('iqtest.plan.runNow') }}</button>
                  <button type="button" class="btn btn-secondary btn-sm ml-1" @click="openEdit(p)">{{ t('common.edit') }}</button>
                  <button type="button" class="btn btn-danger btn-sm ml-1" @click="askDelete(p)">{{ t('common.delete') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Runs -->
      <section v-if="tab === 'runs'" class="space-y-4">
        <div class="overflow-x-auto rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800">
          <table class="min-w-full text-sm">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-3 py-2">#</th>
                <th class="px-3 py-2">{{ t('iqtest.run.plan') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.run.trigger') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.run.status') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.run.started') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.run.accounts') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.outcome.pass') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.outcome.quality_fail') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.outcome.infra_error') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.outcome.skipped') }}</th>
                <th class="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!runs.length">
                <td colspan="11" class="px-3 py-8 text-center text-gray-500">{{ t('iqtest.plan.empty') }}</td>
              </tr>
              <tr v-for="r in runs" :key="r.id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="px-3 py-2">{{ r.id }}</td>
                <td class="px-3 py-2">{{ planName(r.plan_id) }}</td>
                <td class="px-3 py-2">{{ triggerLabel(r.trigger) }}</td>
                <td class="px-3 py-2">{{ statusLabel(r.status) }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(r.started_at) }}</td>
                <td class="px-3 py-2">{{ r.accounts }}</td>
                <td class="px-3 py-2 text-green-600">{{ r.pass }}</td>
                <td class="px-3 py-2 text-red-600">{{ r.quality_fail }}</td>
                <td class="px-3 py-2 text-amber-600">{{ r.infra_error }}</td>
                <td class="px-3 py-2 text-gray-500">{{ r.skipped }}</td>
                <td class="px-3 py-2 text-right">
                  <button type="button" class="btn btn-secondary btn-sm" @click="openRun(r)">{{ t('iqtest.result.view') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination
          v-if="runPage.total > 0"
          :page="runPage.page"
          :total="runPage.total"
          :page-size="runPage.size"
          @update:page="(p: number) => loadRuns(p)"
          @update:pageSize="(n: number) => { runPage.size = n; loadRuns(1) }"
        />
      </section>

      <!-- Events -->
      <section v-if="tab === 'events'" class="space-y-4">
        <div class="overflow-x-auto rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800">
          <table class="min-w-full text-sm">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-3 py-2">{{ t('iqtest.result.time') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.account') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.kind') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.events.from') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.events.to') }}</th>
                <th class="px-3 py-2">{{ t('iqtest.result.reason') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!events.length">
                <td colspan="6" class="px-3 py-8 text-center text-gray-500">{{ t('iqtest.events.empty') }}</td>
              </tr>
              <tr v-for="e in events" :key="e.id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDateTime(e.created_at) }}</td>
                <td class="px-3 py-2">{{ e.account_name }} <span class="text-xs text-gray-500">#{{ e.account_id }}</span></td>
                <td class="px-3 py-2">{{ t(`iqtest.kind.${e.kind}`) }}</td>
                <td class="px-3 py-2">{{ stateLabel(e.from) }}</td>
                <td class="px-3 py-2">{{ stateLabel(e.to) }}</td>
                <td class="max-w-md truncate px-3 py-2 text-xs text-gray-500" :title="e.detail">{{ e.detail }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination
          v-if="eventPage.total > 0"
          :page="eventPage.page"
          :total="eventPage.total"
          :page-size="eventPage.size"
          @update:page="(p: number) => loadEvents(p)"
          @update:pageSize="(n: number) => { eventPage.size = n; loadEvents(1) }"
        />
      </section>
    </div>

    <!-- Plan editor -->
    <BaseDialog :show="showPlan" :title="editingId ? t('iqtest.plan.edit') : t('iqtest.plan.create')" width="wide" @close="showPlan = false">
      <form id="iqtest-plan-form" class="space-y-5" @submit.prevent="savePlan">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('iqtest.plan.name') }}</label>
            <input v-model="form.name" class="input" maxlength="100" required />
          </div>
          <div class="flex items-end gap-3">
            <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="form.enabled" type="checkbox" />
              {{ t('iqtest.plan.enabled') }}
            </label>
            <span class="text-xs text-gray-500">{{ t('iqtest.plan.enabledHint') }}</span>
          </div>
        </div>

        <fieldset class="space-y-3">
          <legend class="input-label">{{ t('iqtest.plan.checks') }}</legend>
          <div v-for="k in kinds" :key="k" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <label class="inline-flex items-center gap-2 text-sm font-medium text-gray-800 dark:text-gray-200">
              <input v-model="form.checks[k].on" type="checkbox" />
              {{ t(`iqtest.kind.${k}`) }}
            </label>
            <div v-if="form.checks[k].on" class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-3">
              <div v-if="k === 'candy'" class="md:col-span-3">
                <label class="input-label">{{ t('iqtest.plan.candyMode') }}</label>
                <select v-model="form.checks[k].candy_mode" class="input">
                  <option value="anywhere">{{ t('iqtest.plan.candyAnywhere') }}</option>
                  <option value="final">{{ t('iqtest.plan.candyFinal') }}</option>
                </select>
              </div>
              <div class="md:col-span-3">
                <label class="input-label">{{ t('iqtest.plan.customPrompt') }}</label>
                <textarea v-model="form.checks[k].prompt" rows="3" class="input" :placeholder="k === 'candy' ? defaults?.candy_prompt : defaults?.pelican_prompt"></textarea>
              </div>
              <div>
                <label class="input-label">{{ t('iqtest.plan.model') }}</label>
                <input v-model="form.checks[k].model" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('iqtest.plan.maxTokens') }}</label>
                <input v-model.number="form.checks[k].max_tokens" type="number" min="0" max="64000" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('iqtest.plan.timeout') }}</label>
                <input v-model.number="form.checks[k].timeout_seconds" type="number" min="0" max="900" class="input" />
              </div>
            </div>
          </div>
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="input-label">{{ t('iqtest.plan.schedule') }}</legend>
          <div class="flex gap-4 text-sm text-gray-700 dark:text-gray-300">
            <label class="inline-flex items-center gap-2"><input v-model="form.schedule_type" type="radio" value="interval" />{{ t('iqtest.plan.interval') }}</label>
            <label class="inline-flex items-center gap-2"><input v-model="form.schedule_type" type="radio" value="cron" />{{ t('iqtest.plan.cron') }}</label>
          </div>
          <div v-if="form.schedule_type === 'interval'">
            <label class="input-label">{{ t('iqtest.plan.intervalMinutes', { min: defaults?.min_interval_minutes ?? 10 }) }}</label>
            <input v-model.number="form.interval_minutes" type="number" :min="defaults?.min_interval_minutes ?? 10" class="input w-48" />
          </div>
          <div v-else>
            <input v-model="form.cron_expr" class="input" placeholder="0 3 * * *" />
            <p class="mt-1 text-xs text-gray-500">{{ t('iqtest.plan.cronHint') }}</p>
          </div>
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="input-label">{{ t('iqtest.plan.scope') }}</legend>
          <div class="flex flex-wrap gap-4 text-sm text-gray-700 dark:text-gray-300">
            <label class="inline-flex items-center gap-2"><input v-model="form.scope_type" type="radio" value="all" />{{ t('iqtest.plan.scopeAll') }}</label>
            <label class="inline-flex items-center gap-2"><input v-model="form.scope_type" type="radio" value="groups" />{{ t('iqtest.plan.scopeGroups') }}</label>
            <label class="inline-flex items-center gap-2"><input v-model="form.scope_type" type="radio" value="accounts" />{{ t('iqtest.plan.scopeAccounts') }}</label>
          </div>
          <div v-if="form.scope_type === 'groups'" class="grid max-h-48 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 p-2 sm:grid-cols-2 dark:border-dark-600">
            <label v-for="g in groups" :key="g.id" class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="form.group_ids" type="checkbox" :value="g.id" />
              {{ g.name }} <span class="text-xs text-gray-400">#{{ g.id }}</span>
            </label>
          </div>
          <div v-if="form.scope_type === 'accounts'" class="space-y-2">
            <div class="flex gap-2">
              <input v-model="accountQuery" class="input" :placeholder="t('iqtest.plan.searchAccount')" @keydown.enter.prevent="searchAccounts" />
              <button type="button" class="btn btn-secondary" @click="searchAccounts">{{ t('common.search') }}</button>
            </div>
            <div v-if="accountHits.length" class="flex flex-wrap gap-2">
              <button
                v-for="a in accountHits"
                :key="a.id"
                type="button"
                class="badge badge-gray cursor-pointer"
                @click="addAccount(a.id, a.name)"
              >
                + {{ a.name }} #{{ a.id }}
              </button>
            </div>
            <div class="flex flex-wrap gap-2">
              <span v-for="id in form.account_ids" :key="id" class="badge badge-primary">
                {{ accountNames[id] || '#' + id }}
                <button type="button" class="ml-1" @click="removeAccount(id)">×</button>
              </span>
            </div>
          </div>
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="input-label">{{ t('iqtest.plan.policy') }}</legend>
          <div class="flex gap-4 text-sm text-gray-700 dark:text-gray-300">
            <label class="inline-flex items-center gap-2"><input v-model="form.policy.mode" type="radio" value="consecutive" />{{ t('iqtest.plan.policyConsecutive') }}</label>
            <label class="inline-flex items-center gap-2"><input v-model="form.policy.mode" type="radio" value="pass_rate" />{{ t('iqtest.plan.policyPassRate') }}</label>
          </div>
          <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
            <div v-if="form.policy.mode === 'consecutive'">
              <label class="input-label">{{ t('iqtest.plan.consecutiveFails') }}</label>
              <input v-model.number="form.policy.consecutive_fails" type="number" min="1" class="input" />
            </div>
            <template v-else>
              <div>
                <label class="input-label">{{ t('iqtest.plan.window') }}</label>
                <input v-model.number="form.policy.window" type="number" min="1" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('iqtest.plan.minSamples') }}</label>
                <input v-model.number="form.policy.min_samples" type="number" min="1" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('iqtest.plan.minPassRate') }}</label>
                <input v-model.number="form.policy.min_pass_rate" type="number" min="0" max="1" step="0.05" class="input" />
              </div>
            </template>
            <div>
              <label class="input-label">{{ t('iqtest.plan.recoverPasses') }}</label>
              <input v-model.number="form.policy.recover_passes" type="number" min="1" class="input" />
            </div>
          </div>
          <p class="text-xs text-gray-500">{{ t('iqtest.plan.policyNote') }}</p>
        </fieldset>

        <details>
          <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('iqtest.plan.advanced') }}</summary>
          <div class="mt-3 grid grid-cols-2 gap-3 md:grid-cols-4">
            <div>
              <label class="input-label">{{ t('iqtest.plan.maxConcurrency') }}</label>
              <input v-model.number="form.max_concurrency" type="number" min="1" max="20" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('iqtest.plan.maxRequests') }}</label>
              <input v-model.number="form.max_requests_per_run" type="number" min="0" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('iqtest.plan.jitter') }}</label>
              <input v-model.number="form.jitter_seconds" type="number" min="0" max="300" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('iqtest.plan.keep') }}</label>
              <input v-model.number="form.keep_results" type="number" min="5" max="1000" class="input" />
            </div>
          </div>
        </details>
      </form>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" @click="showPlan = false">{{ t('iqtest.common.cancel') }}</button>
          <button type="submit" form="iqtest-plan-form" class="btn btn-primary" :disabled="saving">{{ t('iqtest.common.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <IQResultListDialog :show="showList" :title="listTitle" :items="listItems" :loading="listLoading" @close="showList = false" />

    <ConfirmDialog
      :show="!!toDelete"
      :title="t('common.delete')"
      :message="toDelete ? t('iqtest.plan.confirmDelete', { name: toDelete.name }) : ''"
      danger
      @confirm="doDelete"
      @cancel="toDelete = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import iqtestAPI from '@/api/admin/iqtest'
import type {
  IQCheckConfig,
  IQDefaults,
  IQEvent,
  IQKind,
  IQPlan,
  IQResult,
  IQRun,
  IQStateRow
} from '@/api/admin/iqtest'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import IQResultListDialog from './IQResultListDialog.vue'
import { outcomeClass, stateClass, useIQLabels } from './labels'

type Tab = 'flagged' | 'plans' | 'runs' | 'events'

const { t } = useI18n()
const appStore = useAppStore()

const tabs: Tab[] = ['flagged', 'plans', 'runs', 'events']
const kinds: IQKind[] = ['candy', 'pelican_svg']
const tab = ref<Tab>('flagged')
const loading = ref(false)
const defaults = ref<IQDefaults | null>(null)

// ---------- data ----------
const states = ref<IQStateRow[]>([])
const stateFilter = reactive({ flagged: false, kind: '' as '' | IQKind })
const statePage = reactive({ page: 1, size: 20, total: 0 })
const plans = ref<IQPlan[]>([])
const runs = ref<IQRun[]>([])
const runPage = reactive({ page: 1, size: 20, total: 0 })
const events = ref<IQEvent[]>([])
const eventPage = reactive({ page: 1, size: 20, total: 0 })

function fail(e: unknown, key = 'iqtest.common.loadFailed') {
  appStore.showError(extractApiErrorMessage(e, t(key)))
}

async function loadStates(page = statePage.page) {
  loading.value = true
  try {
    const res = await iqtestAPI.listStates(page, statePage.size, {
      flagged: stateFilter.flagged,
      kind: stateFilter.kind
    })
    states.value = res.items ?? []
    statePage.page = res.page
    statePage.total = res.total
  } catch (e) {
    fail(e)
  } finally {
    loading.value = false
  }
}

async function loadPlans() {
  loading.value = true
  try {
    plans.value = await iqtestAPI.listPlans()
  } catch (e) {
    fail(e)
  } finally {
    loading.value = false
  }
}

async function loadRuns(page = runPage.page) {
  loading.value = true
  try {
    if (!plans.value.length) plans.value = await iqtestAPI.listPlans()
    const res = await iqtestAPI.listRuns(page, runPage.size)
    runs.value = res.items ?? []
    runPage.page = res.page
    runPage.total = res.total
  } catch (e) {
    fail(e)
  } finally {
    loading.value = false
  }
}

async function loadEvents(page = eventPage.page) {
  loading.value = true
  try {
    const res = await iqtestAPI.listEvents(page, eventPage.size)
    events.value = res.items ?? []
    eventPage.page = res.page
    eventPage.total = res.total
  } catch (e) {
    fail(e)
  } finally {
    loading.value = false
  }
}

function refresh() {
  if (tab.value === 'flagged') return loadStates()
  if (tab.value === 'plans') return loadPlans()
  if (tab.value === 'runs') return loadRuns()
  return loadEvents()
}

function setTab(next: Tab) {
  tab.value = next
  refresh()
}

// ---------- labels ----------
const planName = (id: number) =>
  plans.value.find((p) => p.id === id)?.name ?? (id ? `#${id}` : t('iqtest.run.triggers.adhoc'))
const { outcomeLabel, reasonLabel, stateLabel, triggerLabel, statusLabel } = useIQLabels()
function scheduleText(p: IQPlan) {
  return p.schedule_type === 'cron'
    ? `cron ${p.cron_expr}`
    : `${t('iqtest.plan.interval')} ${p.interval_minutes} min`
}
function scopeText(p: IQPlan) {
  if (p.scope_type === 'groups') return `${t('iqtest.plan.scopeGroups')} (${p.group_ids?.length ?? 0})`
  if (p.scope_type === 'accounts') return `${t('iqtest.plan.scopeAccounts')} (${p.account_ids?.length ?? 0})`
  return t('iqtest.plan.scopeAll')
}

// ---------- plan editor ----------
interface CheckForm {
  on: boolean
  prompt: string
  model: string
  max_tokens: number
  timeout_seconds: number
  candy_mode: 'anywhere' | 'final'
}
const blankCheck = (): CheckForm => ({
  on: false,
  prompt: '',
  model: '',
  max_tokens: 0,
  timeout_seconds: 0,
  candy_mode: 'anywhere'
})

function blankForm() {
  return {
    name: '',
    enabled: false,
    checks: { candy: { ...blankCheck(), on: true }, pelican_svg: blankCheck() } as Record<IQKind, CheckForm>,
    schedule_type: 'interval' as 'interval' | 'cron',
    interval_minutes: 360,
    cron_expr: '',
    scope_type: 'all' as 'all' | 'groups' | 'accounts',
    group_ids: [] as number[],
    account_ids: [] as number[],
    platforms: [] as string[],
    policy: {
      mode: 'consecutive' as 'consecutive' | 'pass_rate',
      consecutive_fails: 3,
      window: 10,
      min_samples: 5,
      min_pass_rate: 0.5,
      recover_passes: 2
    },
    max_concurrency: 3,
    max_requests_per_run: 0,
    jitter_seconds: 5,
    keep_results: 50
  }
}

const showPlan = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const form = reactive(blankForm())
const groups = ref<{ id: number; name: string }[]>([])
const accountQuery = ref('')
const accountHits = ref<{ id: number; name: string }[]>([])
const accountNames = reactive<Record<number, string>>({})

async function ensureGroups() {
  if (groups.value.length) return
  try {
    groups.value = (await adminAPI.groups.getAll()).map((g) => ({ id: g.id, name: g.name }))
  } catch (e) {
    fail(e)
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, blankForm())
  accountHits.value = []
  showPlan.value = true
  ensureGroups()
}

function openEdit(p: IQPlan) {
  editingId.value = p.id
  const f = blankForm()
  f.name = p.name
  f.enabled = p.enabled
  f.schedule_type = p.schedule_type
  f.interval_minutes = p.interval_minutes || 360
  f.cron_expr = p.cron_expr || ''
  f.scope_type = p.scope_type
  f.group_ids = [...(p.group_ids ?? [])]
  f.account_ids = [...(p.account_ids ?? [])]
  f.platforms = [...(p.platforms ?? [])]
  f.policy = { ...f.policy, ...p.policy }
  f.max_concurrency = p.max_concurrency
  f.max_requests_per_run = p.max_requests_per_run
  f.jitter_seconds = p.jitter_seconds
  f.keep_results = p.keep_results
  f.checks.candy.on = false
  for (const c of p.checks) {
    f.checks[c.kind] = {
      on: true,
      prompt: c.prompt ?? '',
      model: c.model ?? '',
      max_tokens: c.max_tokens ?? 0,
      timeout_seconds: c.timeout_seconds ?? 0,
      candy_mode: c.candy_mode === 'final' ? 'final' : 'anywhere'
    }
  }
  Object.assign(form, f)
  accountHits.value = []
  showPlan.value = true
  ensureGroups()
}

async function searchAccounts() {
  try {
    const res = await adminAPI.accounts.list(1, 10, { search: accountQuery.value.trim() })
    accountHits.value = (res.items ?? []).map((a) => ({ id: a.id, name: a.name }))
  } catch (e) {
    fail(e)
  }
}
function addAccount(id: number, name: string) {
  accountNames[id] = name
  if (!form.account_ids.includes(id)) form.account_ids.push(id)
}
function removeAccount(id: number) {
  form.account_ids = form.account_ids.filter((x) => x !== id)
}

function buildChecks(): IQCheckConfig[] {
  const out: IQCheckConfig[] = []
  for (const k of kinds) {
    const c = form.checks[k]
    if (!c.on) continue
    const item: IQCheckConfig = { kind: k }
    if (c.prompt.trim()) item.prompt = c.prompt
    if (c.model.trim()) item.model = c.model.trim()
    if (c.max_tokens > 0) item.max_tokens = c.max_tokens
    if (c.timeout_seconds > 0) item.timeout_seconds = c.timeout_seconds
    if (k === 'candy') item.candy_mode = c.candy_mode
    out.push(item)
  }
  return out
}

async function savePlan() {
  const checks = buildChecks()
  if (!checks.length) {
    appStore.showError(t('iqtest.plan.checks'))
    return
  }
  const body: Partial<IQPlan> = {
    name: form.name,
    enabled: form.enabled,
    checks,
    schedule_type: form.schedule_type,
    interval_minutes: form.schedule_type === 'interval' ? form.interval_minutes : undefined,
    cron_expr: form.schedule_type === 'cron' ? form.cron_expr : undefined,
    scope_type: form.scope_type,
    group_ids: form.scope_type === 'groups' ? form.group_ids : undefined,
    account_ids: form.scope_type === 'accounts' ? form.account_ids : undefined,
    platforms: form.platforms,
    policy: { ...form.policy },
    max_concurrency: form.max_concurrency,
    max_requests_per_run: form.max_requests_per_run,
    jitter_seconds: form.jitter_seconds,
    keep_results: form.keep_results
  }
  saving.value = true
  try {
    if (editingId.value) await iqtestAPI.updatePlan(editingId.value, body)
    else await iqtestAPI.createPlan(body)
    appStore.showSuccess(t('iqtest.plan.saved'))
    showPlan.value = false
    await loadPlans()
  } catch (e) {
    fail(e, 'iqtest.common.saveFailed')
  } finally {
    saving.value = false
  }
}

async function runPlan(p: IQPlan) {
  try {
    await iqtestAPI.runPlan(p.id)
    appStore.showSuccess(t('iqtest.plan.started'))
    loadPlans()
  } catch (e) {
    const msg = extractApiErrorMessage(e, '')
    if (msg.includes('already running')) appStore.showError(t('iqtest.plan.alreadyRunning'))
    else fail(e, 'iqtest.common.saveFailed')
  }
}

const toDelete = ref<IQPlan | null>(null)
function askDelete(p: IQPlan) {
  toDelete.value = p
}
async function doDelete() {
  const p = toDelete.value
  toDelete.value = null
  if (!p) return
  try {
    await iqtestAPI.deletePlan(p.id)
    appStore.showSuccess(t('iqtest.plan.deleted'))
    await loadPlans()
  } catch (e) {
    fail(e, 'iqtest.common.saveFailed')
  }
}

// ---------- results ----------
const showList = ref(false)
const listTitle = ref('')
const listItems = ref<IQResult[]>([])
const listLoading = ref(false)

async function openRun(r: IQRun) {
  listTitle.value = `${t('iqtest.run.detail')} #${r.id}`
  listItems.value = []
  showList.value = true
  listLoading.value = true
  try {
    listItems.value = (await iqtestAPI.listRunResults(r.id, 1, 200)).items ?? []
  } catch (e) {
    fail(e)
  } finally {
    listLoading.value = false
  }
}

async function openAccountHistory(s: IQStateRow) {
  listTitle.value = `${s.account_name} #${s.account_id} · ${t(`iqtest.kind.${s.kind}`)}`
  listItems.value = []
  showList.value = true
  listLoading.value = true
  try {
    listItems.value = await iqtestAPI.listAccountResults(s.account_id, s.kind, 100)
  } catch (e) {
    fail(e)
  } finally {
    listLoading.value = false
  }
}

async function clearFlag(s: IQStateRow) {
  try {
    await iqtestAPI.clearState(s.account_id, s.kind)
    appStore.showSuccess(t('iqtest.flagged.cleared'))
    await loadStates()
  } catch (e) {
    fail(e, 'iqtest.common.saveFailed')
  }
}

onMounted(async () => {
  try {
    defaults.value = await iqtestAPI.defaults()
  } catch {
    /* defaults are only placeholders */
  }
  loadPlans()
  loadStates(1)
})
</script>
