import apiClient from './client'

export interface CarpoolMember {
  number: number
  name: string
  is_self: boolean
  allocated_usd: number
  used_usd: number
  remaining_usd: number
}

export interface CarpoolUsageWindow {
  window: '5h' | '7d'
  range_source: 'official' | 'recent'
  official_used_percent: number | null
  start_at: string | null
  reset_at: string | null
  stats: {
    requests: number
    tokens: number
    account_cost_usd: number
    user_cost_usd: number
  } | null
}

export interface MyCarpool {
  id: number
  name: string
  member_count: number
  allocation_ready: boolean
  allocation_mode: 'manual' | 'auto'
  sync_official_reset: boolean
  total_usd: number
  used_usd: number
  remaining_usd: number
  per_member_usd: number
  estimated: boolean
  upstream_capacity_usd: number | null
  official_used_percent: number | null
  next_reset_at: string | null
  last_sync_at: string | null
  status: 'waiting' | 'sampling' | 'synced' | 'delayed' | 'pending_reset' | 'adjusting'
  members: CarpoolMember[]
  windows: CarpoolUsageWindow[]
}

export async function getMyCarpools(signal?: AbortSignal): Promise<MyCarpool[]> {
  return (await apiClient.get<MyCarpool[]>('/carpool', { signal })).data
}
