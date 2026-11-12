<template>
  <AppLayout><div class="space-y-5">
    <h1 class="text-2xl font-semibold">上游价格对比</h1>
    <p class="text-sm text-gray-500">直接读取各账号当前已应用的同步结果。折后单价已乘一次分组倍率；单位为 USD / 百万 Token，按次价格单独显示。缺失价格显示 —。</p>
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="page = 1; load()">
      <label class="text-sm">模型<input v-model.trim="model" class="input" placeholder="筛选模型" /></label>
      <label class="text-sm">上游账号<select v-model.number="accountId" class="input"><option :value="0">全部账号</option><option v-for="a in result?.accounts" :key="a.id" :value="a.id">{{ a.name }}</option></select></label>
      <button class="btn btn-primary" :disabled="busy">刷新已同步结果</button>
    </form>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <details class="rounded-lg border p-3"><summary class="cursor-pointer text-sm">账号同步状态（{{ result?.accounts.length || 0 }}）</summary>
      <ul class="mt-3 space-y-2 text-xs"><li v-for="a in result?.accounts" :key="a.id">
        <b>{{ a.name }}</b> · {{ a.provider || '未配置' }} · {{ a.group || '—' }} · {{ a.automatic ? '自动同步' : '手动同步' }} · 版本 {{ a.version_id || '—' }} · 最近成功 {{ date(a.last_success) }} · 下次 {{ date(a.next_sync) }}
        <span v-if="a.account_changed" class="ml-2 text-red-600">账号已变更，快照仅供历史参考</span><span v-else-if="a.stale" class="ml-2 text-amber-600">价格可能过期</span><span v-if="a.last_error" class="ml-2 text-amber-600">{{ a.last_error }}</span>
      </li></ul>
    </details>
    <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
      <table class="w-full whitespace-nowrap text-left text-sm">
        <thead class="bg-gray-50 dark:bg-dark-800"><tr><th v-for="h in ['模型', '账号 / 分组', '倍率', '输入', '输出', '缓存读取', '缓存写入', '按次（USD）', '状态']" :key="h" class="p-3">{{ h }}</th></tr></thead>
        <tbody><tr v-for="row in result?.items" :key="`${row.account_id}:${row.model}`" class="border-t border-gray-100 dark:border-dark-700">
          <td class="p-3">{{ row.model }}<span v-if="row.base.manual" class="ml-1 text-xs text-amber-600">人工覆盖</span></td>
          <td class="p-3">{{ accounts.get(row.account_id)?.name }} / {{ accounts.get(row.account_id)?.group }}</td>
          <td class="p-3">{{ row.multiplier }}x</td>
          <td v-for="key in fields" :key="key" class="p-3" :title="`原价：${row.base[key] ?? '未知'}`">{{ displayPrice(row, key) }}</td>
          <td class="p-3 text-xs" :title="row.base.reason">{{ accounts.get(row.account_id)?.account_changed ? '账号已变更' : row.base.mode === 'unavailable' ? row.base.reason : accounts.get(row.account_id)?.stale ? '价格可能过期' : `已同步 #${row.version_id}` }}</td>
        </tr><tr v-if="!result?.items.length"><td colspan="9" class="p-6 text-center text-gray-500">{{ busy ? '正在读取…' : '没有已应用的匹配价格；请先在账号的上游定价中预览并应用' }}</td></tr></tbody>
      </table>
    </div>
    <p class="text-xs text-gray-500">缓存写入列为默认时长；其他缓存时长请在账号的上游定价中查看。把鼠标放在价格上可查看未乘倍率的原价。</p>
    <div class="flex items-center gap-4 text-sm"><button class="btn btn-secondary" :disabled="busy || page <= 1" @click="page--; load()">上一页</button><span>{{ page }} / {{ Math.max(1, Math.ceil((result?.total || 0) / 100)) }} · {{ result?.total || 0 }} 条</span><button class="btn btn-secondary" :disabled="busy || page * 100 >= (result?.total || 0)" @click="page++; load()">下一页</button></div>
  </div></AppLayout>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { upstreamPricingAPI } from './api'
import type { PricingComparison } from '.'
const result = ref<PricingComparison>(), model = ref(''), accountId = ref(0), page = ref(1), busy = ref(false), error = ref('')
const fields = ['input', 'output', 'cache_read', 'cache_write', 'request'] as const
const accounts = computed(() => new Map(result.value?.accounts.map(a => [a.id, a])))
function displayPrice(row: PricingComparison['items'][number], key: typeof fields[number]) {
  if (accounts.value.get(row.account_id)?.account_changed || row.effective.mode === 'unavailable') return '—'
  if ((row.effective.mode === 'per_request') !== (key === 'request')) return '—'
  return row.effective[key] ?? '—'
}
const date = (value: string | null) => value ? new Date(value).toLocaleString() : '—'
const controller = new AbortController()
async function load() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { result.value = await upstreamPricingAPI.comparison({ model: model.value, account_id: accountId.value, page: page.value, page_size: 100 }, controller.signal) }
  catch (e) { if (!controller.signal.aborted) error.value = (e as Error).message || '读取失败' }
  finally { busy.value = false }
}
onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>
