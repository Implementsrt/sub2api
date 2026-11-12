import { apiClient } from '@/api/client'

export interface TimingRecord {
  id: string
  request_id?: string
  model: string
  created_at: string
  duration_ms: number | null
  first_token_ms: number | null
  precision_ms: number
}
export interface TimingComparison {
  status: 'exact' | 'candidate' | 'ambiguous' | 'unmatched'
  local: TimingRecord
  station?: TimingRecord
  candidates?: TimingRecord[]
  duration_difference_ms: number | null
  first_token_difference_ms: number | null
  checked_at: string
}
export async function fetchTimingComparison(accountID: number, usageID: number, signal?: AbortSignal) {
  return (await apiClient.get<TimingComparison>(`/admin/upstream-pricing/accounts/${accountID}/usage/${usageID}/timing`, { timeout: 35000, signal })).data
}
export function timingDuration(ms: number | null | undefined) {
  return ms == null || !Number.isFinite(ms) || ms < 0 ? '—' : `${(ms / 1000).toFixed(2)}s`
}
export function timingDifference(ms: number | null | undefined) {
  return ms == null || !Number.isFinite(ms) ? '—' : `${ms > 0 ? '+' : ''}${ms} ms`
}
