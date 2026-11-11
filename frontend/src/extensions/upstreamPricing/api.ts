import { apiClient } from '@/api/client'
import type { PricingConfig, PricingGroup, PricingState, Preview } from '.'

const path = (id: number) => `/admin/upstream-pricing/accounts/${id}`
export const upstreamPricingAPI = {
  async get(id: number, signal?: AbortSignal) { return (await apiClient.get<PricingState>(path(id), { signal })).data },
  async groups(id: number, revision: number, config: PricingConfig, signal?: AbortSignal) {
    return (await apiClient.post<PricingGroup[]>(`${path(id)}/groups`, { revision, config }, { signal, timeout: 65000 })).data
  },
  async preview(id: number, revision: number, config: PricingConfig, signal?: AbortSignal) {
    return (await apiClient.post<Preview>(`${path(id)}/preview`, { revision, config }, { signal, timeout: 70000 })).data
  },
  async apply(id: number, preview_id: string, signal?: AbortSignal) { await apiClient.post(`${path(id)}/apply`, { preview_id }, { signal }) },
  async sync(id: number, signal?: AbortSignal) { await apiClient.post(`${path(id)}/sync`, {}, { signal, timeout: 70000 }) },
  async schedule(id: number, automatic: boolean, interval_minutes: number, signal?: AbortSignal) {
    await apiClient.put(`${path(id)}/schedule`, { automatic, interval_minutes }, { signal })
  },
  async rollback(id: number, version_id: number, signal?: AbortSignal) {
    return (await apiClient.post<Preview>(`${path(id)}/rollback-preview`, { version_id }, { signal })).data
  },
}
