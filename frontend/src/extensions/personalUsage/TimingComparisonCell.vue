<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageLog } from '@/types'
import { durationSeverity, firstTokenSeverity, LATENCY_TEXT_CLASSES, formatUsageOutputRate } from '@/utils/latencyHealth'
import { fetchTimingComparison, timingDifference, timingDuration, type TimingComparison } from './timing'

const props = withDefaults(defineProps<{ row: UsageLog; allowComparison?: boolean }>(), { allowComparison: true })
const { t } = useI18n()
const result = ref<TimingComparison | null>(null)
const open = ref(false)
const busy = ref(false)
const error = ref('')
let controller: AbortController | undefined
let requestVersion = 0
function reset() {
  requestVersion++
  controller?.abort()
  result.value = null; open.value = false; busy.value = false; error.value = ''
}
watch(() => [props.row.id, props.row.account_id, props.allowComparison], reset)
onBeforeUnmount(reset)
async function query() {
  if (busy.value || !props.row.account_id || !props.allowComparison) return
  const version = ++requestVersion
  controller?.abort()
  controller = new AbortController()
  busy.value = true; open.value = true; error.value = ''; result.value = null
  try {
    const comparison = await fetchTimingComparison(props.row.account_id, props.row.id, controller.signal)
    if (version === requestVersion) result.value = comparison
  } catch (err: unknown) {
    if (version === requestVersion) {
      // The server only returns sanitized errors. Never display request/config objects.
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message
      error.value = typeof message === 'string' ? message : '站点耗时暂不可用，请检查账号面板凭证或稍后重试。'
    }
  } finally {
    if (version === requestVersion) busy.value = false
  }
}
</script>

<template>
  <div class="min-w-32 text-xs" title="本地首字与总耗时由 Sub2API 在转发路径计时，包含其测量范围内的网络等待与响应处理。">
    <div class="grid grid-cols-[max-content_max-content] gap-x-2 gap-y-0.5">
      <span class="text-gray-400">{{ t('usage.latencyFirstToken') }}</span>
      <span class="tabular-nums" :class="row.first_token_ms == null ? 'text-gray-400' : LATENCY_TEXT_CLASSES[firstTokenSeverity(row.first_token_ms)]">{{ timingDuration(row.first_token_ms) }}</span>
      <span class="text-gray-400">{{ t('usage.latencyDuration') }}</span>
      <span class="tabular-nums" :class="row.duration_ms == null ? 'text-gray-400' : LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]">{{ timingDuration(row.duration_ms) }}</span>
      <span class="text-gray-400" :title="t('usage.outputTpsHint')">{{ t('usage.outputTps') }}</span>
      <span data-testid="output-tps" class="tabular-nums text-gray-700 dark:text-gray-300">{{ formatUsageOutputRate(row) }}</span>
    </div>
    <button v-if="allowComparison && row.account_id" type="button" class="mt-1 text-primary-600 hover:underline disabled:opacity-50" :disabled="busy" @click="query">{{ busy ? '查询站点中…' : '对比上游耗时' }}</button>
    <Teleport to="body">
      <div v-if="open" class="fixed inset-0 z-[100000050] flex items-center justify-center bg-black/40 p-6" @click.self="open = false" @keydown.esc="open = false">
        <section role="dialog" aria-modal="true" aria-label="本地与上游耗时对比" class="card max-h-[85vh] w-full max-w-2xl overflow-y-auto p-6">
          <div class="flex items-center justify-between gap-4"><h3 class="text-lg font-semibold">本地与上游耗时对比</h3><button type="button" class="btn btn-secondary" autofocus @click="open = false">关闭</button></div>
          <p v-if="busy" class="mt-4 text-sm text-gray-500" role="status">正在读取站点用量日志…</p>
          <p v-if="error" class="mt-4 text-sm text-amber-700 dark:text-amber-400" role="alert">{{ error }}</p>
          <template v-if="result && !busy">
            <p class="mt-4 text-sm" :class="result.status === 'exact' ? 'text-emerald-600' : 'text-amber-600'">{{ result.status === 'exact' ? '请求 ID 对应，已匹配同一次调用' : result.status === 'ambiguous' ? '存在重复请求 ID，无法唯一匹配' : result.status === 'candidate' ? '仅找到相近候选，尚未确认为同一次调用' : '未匹配到站点记录，日志可能尚未生成或未提供相同请求 ID' }}</p>
            <table class="mt-4 w-full text-left text-sm"><thead><tr class="text-gray-500"><th class="py-2">指标</th><th>本地</th><th>上游站点</th><th>本地 − 站点</th></tr></thead><tbody>
              <tr><td class="py-2">首字耗时</td><td>{{ timingDuration(result.local.first_token_ms) }}</td><td>{{ timingDuration(result.station?.first_token_ms) }}</td><td>{{ timingDifference(result.first_token_difference_ms) }}</td></tr>
              <tr><td class="py-2">总耗时</td><td>{{ timingDuration(result.local.duration_ms) }}</td><td>{{ timingDuration(result.station?.duration_ms) }}</td><td>{{ timingDifference(result.duration_difference_ms) }}</td></tr>
            </tbody></table>
            <p v-if="result.station?.precision_ms === 1000" class="mt-2 text-xs text-gray-500">New API 总耗时精度为秒，首字耗时沿用站点 frt 毫秒字段。</p>
            <div v-if="result.candidates?.length" class="mt-4 space-y-2 rounded-lg bg-amber-50 p-3 dark:bg-amber-950/20"><h4 class="text-sm font-medium">候选记录（不计算差值）</h4><p v-for="candidate in result.candidates" :key="candidate.id" class="text-xs">#{{ candidate.id }} · {{ candidate.model }} · {{ candidate.created_at }} · 首字 {{ timingDuration(candidate.first_token_ms) }} / 总耗时 {{ timingDuration(candidate.duration_ms) }}</p></div>
            <p class="mt-3 break-all text-xs text-gray-400">查询时间：{{ result.checked_at }}</p>
          </template>
          <p class="mt-4 text-xs leading-6 text-gray-500">站点日志代表它自己的测量范围，耗时差不能直接等同于网络延迟。使用账号“上游定价”中保存的面板凭证；仅点击时查询，不影响转发或现有成本。</p>
        </section>
      </div>
    </Teleport>
  </div>
</template>
