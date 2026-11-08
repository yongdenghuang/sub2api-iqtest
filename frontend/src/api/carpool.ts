import apiClient from './client'

export interface CarpoolMember {
  number: number
  name: string
  is_self: boolean
  allocated_usd: number
  used_usd: number
  remaining_usd: number
}

export interface MyCarpool {
  id: number
  name: string
  member_count: number
  allocation_ready: boolean
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
}

export async function getMyCarpools(signal?: AbortSignal): Promise<MyCarpool[]> {
  return (await apiClient.get<MyCarpool[]>('/carpool', { signal })).data
}
