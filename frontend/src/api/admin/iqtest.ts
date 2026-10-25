/**
 * Admin IQ-test (scheduled account quality checks) API.
 * Kept in its own file so upgrading the official project never conflicts with it.
 */

import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'

export type IQKind = 'candy' | 'pelican_svg'
export type IQOutcome = 'pass' | 'quality_fail' | 'infra_error' | 'skipped'
export type IQFlagState = 'normal' | 'suspect' | 'flagged'

export interface IQCheckConfig {
  kind: IQKind
  prompt?: string
  model?: string
  max_tokens?: number
  timeout_seconds?: number
  candy_mode?: '' | 'anywhere' | 'final'
}

export interface IQPolicy {
  mode: 'consecutive' | 'pass_rate'
  consecutive_fails: number
  window: number
  min_samples: number
  min_pass_rate: number
  recover_passes: number
}

export interface IQPlan {
  id: number
  name: string
  enabled: boolean
  checks: IQCheckConfig[]
  schedule_type: 'interval' | 'cron'
  interval_minutes?: number
  cron_expr?: string
  scope_type: 'all' | 'groups' | 'accounts'
  group_ids?: number[]
  account_ids?: number[]
  platforms?: string[]
  policy: IQPolicy
  max_concurrency: number
  max_requests_per_run: number
  jitter_seconds: number
  keep_results: number
  next_run_at?: string | null
  last_run_at?: string | null
  created_at?: string
  updated_at?: string
}

export interface IQRun {
  id: number
  plan_id: number
  trigger: string
  status: string
  started_at: string
  finished_at?: string | null
  accounts: number
  requests: number
  pass: number
  quality_fail: number
  infra_error: number
  skipped: number
}

export interface IQResult {
  id: number
  run_id: number
  plan_id: number
  account_id: number
  account_name: string
  platform: string
  created_at: string
  kind: IQKind
  outcome: IQOutcome
  reason?: string
  detail?: string
  output?: string
  score?: number
  metrics?: unknown
  duration_ms: number
}

export interface IQStateRow {
  account_id: number
  account_name: string
  platform: string
  kind: IQKind
  plan_id: number
  flagged: boolean
  state: IQFlagState
  consecutive_fails: number
  consecutive_passes: number
  recent: boolean[]
  last_outcome: IQOutcome
  last_reason?: string
  updated_at: string
  flagged_at?: string | null
}

export interface IQEvent {
  id: number
  account_id: number
  account_name: string
  kind: IQKind
  plan_id: number
  from: string
  to: string
  detail?: string
  created_at: string
}

export interface IQAccountSummary {
  total: number
  quality_fail: number
  infra_error: number
  normal: number
  untested: number
}

export interface IQDefaults {
  default_model: string
  candy_prompt: string
  pelican_prompt: string
  policy: IQPolicy
  min_interval_minutes: number
  check_kinds: IQKind[]
}

const base = '/admin/iqtest'

export const iqtestAPI = {
  async summary(): Promise<IQAccountSummary> {
    return (await apiClient.get<IQAccountSummary>(`${base}/summary`)).data
  },
  async defaults(): Promise<IQDefaults> {
    return (await apiClient.get<IQDefaults>(`${base}/defaults`)).data
  },
  async listPlans(): Promise<IQPlan[]> {
    return (await apiClient.get<IQPlan[]>(`${base}/plans`)).data ?? []
  },
  async createPlan(plan: Partial<IQPlan>): Promise<IQPlan> {
    return (await apiClient.post<IQPlan>(`${base}/plans`, plan)).data
  },
  async updatePlan(id: number, plan: Partial<IQPlan>): Promise<IQPlan> {
    return (await apiClient.put<IQPlan>(`${base}/plans/${id}`, plan)).data
  },
  async deletePlan(id: number): Promise<void> {
    await apiClient.delete(`${base}/plans/${id}`)
  },
  async runPlan(id: number): Promise<{ run_id: number }> {
    return (await apiClient.post<{ run_id: number }>(`${base}/plans/${id}/run`)).data
  },
  async runAdHoc(body: { account_ids: number[]; checks: IQCheckConfig[] }): Promise<{ run_id: number }> {
    return (await apiClient.post<{ run_id: number }>(`${base}/runs/adhoc`, body)).data
  },
  async listRuns(page = 1, pageSize = 20, planId?: number): Promise<BasePaginationResponse<IQRun>> {
    return (
      await apiClient.get<BasePaginationResponse<IQRun>>(`${base}/runs`, {
        params: { page, page_size: pageSize, plan_id: planId }
      })
    ).data
  },
  async getRun(id: number): Promise<IQRun> {
    return (await apiClient.get<IQRun>(`${base}/runs/${id}`)).data
  },
  async listRunResults(id: number, page = 1, pageSize = 50): Promise<BasePaginationResponse<IQResult>> {
    return (
      await apiClient.get<BasePaginationResponse<IQResult>>(`${base}/runs/${id}/results`, {
        params: { page, page_size: pageSize }
      })
    ).data
  },
  async getResult(id: number): Promise<IQResult> {
    return (await apiClient.get<IQResult>(`${base}/results/${id}`)).data
  },
  async listAccountResults(accountId: number, kind?: IQKind, limit = 50): Promise<IQResult[]> {
    return (
      (await apiClient.get<IQResult[]>(`${base}/accounts/${accountId}/results`, { params: { kind, limit } }))
        .data ?? []
    )
  },
  async clearState(accountId: number, kind: IQKind): Promise<void> {
    await apiClient.post(`${base}/accounts/${accountId}/clear`, null, { params: { kind } })
  },
  async listStates(
    page = 1,
    pageSize = 20,
    filters?: { flagged?: boolean; kind?: IQKind | '' }
  ): Promise<BasePaginationResponse<IQStateRow>> {
    return (
      await apiClient.get<BasePaginationResponse<IQStateRow>>(`${base}/states`, {
        params: {
          page,
          page_size: pageSize,
          flagged: filters?.flagged ? 'true' : undefined,
          kind: filters?.kind || undefined
        }
      })
    ).data
  },
  /** All check states of the given accounts, used by the account list column. */
  async listStatesByAccounts(accountIds: number[]): Promise<IQStateRow[]> {
    return (
      (await apiClient.get<IQStateRow[]>(`${base}/states/by-accounts`, { params: { ids: accountIds.join(',') } }))
        .data ?? []
    )
  },
  async listEvents(page = 1, pageSize = 20): Promise<BasePaginationResponse<IQEvent>> {
    return (
      await apiClient.get<BasePaginationResponse<IQEvent>>(`${base}/events`, {
        params: { page, page_size: pageSize }
      })
    ).data
  }
}

export default iqtestAPI
