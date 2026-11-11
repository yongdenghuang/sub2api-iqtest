import { apiClient } from '../client'

export interface CarpoolConfig {
  enabled: boolean
  initial_total_usd: number
  allocation_mode?: 'manual' | 'auto'
  manual_weekly_usd?: number
  sync_official_reset?: boolean
}
export interface CarpoolState {
  reset_at?: string
  checked_at?: string
  percent?: number
  capacity?: number
  estimated?: boolean
  message?: string
  last_error?: string
  members?: number
  per_member_usd?: number
}
export interface UpstreamReplacement { previous_account_id: number; account_id: number }
export interface CarpoolSaveRequest extends CarpoolConfig { replace_upstream?: UpstreamReplacement }
export interface CarpoolMember { id: number; user_id: number; name: string; usage: number; window?: string }
export interface CarpoolGroup {
  id: number
  name: string
  account_id: number
  account_name: string
  bound_account_id?: number
  bound_account_name?: string
  binding_changed?: boolean
  account_count: number
  eligible: boolean
  weekly_limit: number
  config: CarpoolConfig
  state: CarpoolState
  members: CarpoolMember[]
}
export interface CarpoolEvent {
  id: number
  kind: string
  created_at: string
  detail: { before_limit: number; after_limit: number; state: CarpoolState; previous_account_id?: number; account_id?: number }
}
export const carpoolApi = {
  async list(): Promise<CarpoolGroup[]> { return (await apiClient.get('/admin/carpool/groups')).data },
  async save(id: number, config: CarpoolSaveRequest): Promise<void> { await apiClient.put(`/admin/carpool/groups/${id}`, config) },
  async sync(id: number): Promise<CarpoolGroup> { return (await apiClient.post(`/admin/carpool/groups/${id}/sync`)).data },
  async events(id: number): Promise<CarpoolEvent[]> { return (await apiClient.get(`/admin/carpool/groups/${id}/events`)).data }
}
