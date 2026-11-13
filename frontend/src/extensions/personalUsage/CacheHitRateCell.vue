<template>
  <div data-testid="cache-hit-rate-cell" class="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
    <span class="whitespace-nowrap text-sm tabular-nums" :class="cacheHitRateColor(row)" :title="rateTitle">
      {{ formatCacheHitRate(row) }}
    </span>
    <CacheMonitoringCell v-if="row.cache_drop" :row="row" />
  </div>
</template>

<script setup lang="ts">
import { cacheHitRateColor, formatCacheHitRate, usePersonalUsageLabels, type PersonalUsageLog } from './index'
import { computed } from 'vue'
import CacheMonitoringCell from './CacheMonitoringCell.vue'
const props = defineProps<{ row: PersonalUsageLog }>()
const labels = usePersonalUsageLabels()
const rateTitle = computed(() => {
  const status = props.row.cache_monitoring_status === 'identity_missing' ? labels.value.identityMissing
    : props.row.cache_monitoring_status === 'not_comparable' ? labels.value.notComparable
    : !props.row.session_id && !props.row.session_identity?.thread_id ? labels.value.noSession
    : props.row.cache_monitoring_status !== 'ready' ? labels.value.unavailable : labels.value.noDrop
  return `${labels.value.formula}\n${props.row.cache_drop ? labels.value.rule : `${status}\n${labels.value.rule}`}`
})
</script>
