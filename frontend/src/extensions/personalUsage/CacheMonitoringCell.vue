<template>
  <span v-if="row.cache_drop" data-testid="cache-drop-alert" tabindex="0" class="inline-flex rounded bg-red-100 px-2 py-0.5 text-xs font-medium text-red-800 dark:bg-red-950 dark:text-red-200" :title="evidence">
    {{ labels.cacheDrop }}
  </span>
  <span v-else class="text-xs text-gray-400 dark:text-gray-500" :title="labels.rule" data-testid="cache-monitoring-status">
    {{ !row.session_id ? labels.noSession : row.cache_monitoring_status !== 'ready' ? labels.unavailable : labels.noDrop }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { usePersonalUsageLabels, type PersonalUsageLog } from './index'
const props = defineProps<{ row: PersonalUsageLog }>()
const labels = usePersonalUsageLabels()
const evidence = computed(() => {
  const alert = props.row.cache_drop
  if (!alert) return labels.value.rule
  const text = labels.value
  return `${text.previous}: ${(alert.previous_hit_rate * 100).toFixed(2)}% → ${text.current}: ${(alert.current_hit_rate * 100).toFixed(2)}%; ${text.drop} ${alert.drop_points.toFixed(2)} ${text.points}; ${text.gap} ${alert.gap_seconds.toFixed(1)} ${text.seconds}; ${text.previousUsage} #${alert.previous_usage_id} (${alert.previous_created_at}).\n${text.rule}`
})
</script>
