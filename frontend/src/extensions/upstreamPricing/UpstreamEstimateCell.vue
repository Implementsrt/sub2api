<template>
  <span v-if="estimate?.status === 'estimated'" class="whitespace-nowrap text-sm tabular-nums" :title="details" tabindex="0">
    {{ formatUpstreamAmount(estimate) }}
    <span v-if="estimate.stale" class="ml-1 rounded bg-amber-100 px-1 text-xs text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">{{ labels.stale }}</span>
  </span>
  <span v-else class="whitespace-nowrap text-xs text-gray-400" :title="estimate?.reason || labels.unavailable" tabindex="0">
    {{ estimate?.status === 'not_configured' ? labels.notConfigured : estimate?.status === 'pending' ? labels.pending : '—' }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatUpstreamAmount, useUpstreamPricingLabels, type UpstreamEstimate } from '.'
const props = defineProps<{ row: { upstream_estimate?: UpstreamEstimate | null } }>()
const labels = useUpstreamPricingLabels()
const estimate = computed(() => props.row.upstream_estimate)
const details = computed(() => {
  const e = estimate.value
  if (!e) return ''
  const l = labels.value
  const lines = [l.note, `${l.source}: ${e.source || '—'}`, `${l.group}: ${e.group || '—'}`, `${l.model}: ${e.model || '—'}`,
    `${l.version}: ${e.version_id || '—'}`, `${l.multiplier}: ${e.multiplier ?? '—'}`, `${l.at}: ${e.pricing_at || '—'}`, `${l.synced}: ${e.synced_at || '—'}`]
  if (e.price) {
    lines.push(l.unit)
    for (const [label, value] of [[l.input, e.price.input], [l.output, e.price.output], [l.cacheRead, e.price.cache_read], [l.cacheWrite, e.price.cache_write],
      [l.cache1h, e.price.cache_write_1h], [l.cache30m, e.price.cache_write_30m], [l.cache12h, e.price.cache_write_12h], [l.request, e.price.request]]) {
      if (value != null) lines.push(`${label}: ${value}`)
    }
    if (e.price.manual) lines.push(l.manual)
  }
  if (e.usage) lines.push(`Tokens: input=${e.usage.input}, output=${e.usage.output}, cache_read=${e.usage.cache_read}, cache_write=${e.usage.cache_write}, 5m=${e.usage.cache_write_5m}, 1h=${e.usage.cache_write_1h}, 30m=${e.usage.cache_write_30m}, 12h=${e.usage.cache_write_12h}`)
  return lines.join('\n')
})
</script>
