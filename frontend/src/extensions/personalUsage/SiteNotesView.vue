<script setup lang="ts">
import {computed,ref} from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'

const price=ref('0.1'),hitRate=ref('85'),cachePricePercent=ref('10')
const calculation=computed(()=>{
  if([price.value,hitRate.value,cachePricePercent.value].some(value=>String(value).trim()===''))return null
  const p=Number(price.value),hit=Number(hitRate.value),cache=Number(cachePricePercent.value)
  if(![p,hit,cache].every(Number.isFinite)||p<0||hit<0||hit>100||cache<0||cache>100)return null
  const currentWeight=(100-hit+hit*cache/100)/100
  const baselineWeight=(1+99*cache/100)/100
  const factor=p===0?1:currentWeight/baselineWeight
  const equivalent=p*factor
  if(!Number.isFinite(equivalent))return null
  return {equivalent,factor,change:(factor-1)*100,currentWeight,baselineWeight}
})
const numberFormat=new Intl.NumberFormat('zh-CN',{maximumSignificantDigits:8})
const format=(value:number)=>numberFormat.format(value)
const difference=computed(()=>{
  if(!calculation.value)return ''
  const change=calculation.value.change
  return Math.abs(change)<0.005?'费用相同':`${change>0?'多花':'少花'} ${Math.abs(change).toFixed(2)}%`
})

const patterns = ['降智', '缓存命中低', '切号掉缓存', '中断仍收费']
const sites = [
  { name: '2chat', flags: [false, true, false, false], note: '缓存命中率约 80%' },
  { name: '奶龙 Waw', flags: [false, true, false, false], note: '缓存命中率约 88%' },
  { name: '浮生', flags: [false, false, true, true], note: '缓存命中率约 95%' },
  { name: '极智', flags: [false, false, false, false], note: '缓存命中率约 99%' },
  { name: '鹿鸣', flags: [false, false, true, false], note: '' },
]
</script>

<template>
  <AppLayout>
  <div class="space-y-6" data-testid="site-notes">
    <div>
      <h2 class="text-xl font-semibold text-gray-900 dark:text-white">个人站点说明</h2>
      <p class="mt-2 text-sm text-gray-500">以下为 codex_quota 中保存的个人使用记录，不是站点实时状态；命中率为当时观察值。</p>
      <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">✓ 已记录 / 暂无记录</p>
    </div>

    <section class="overflow-hidden border-y border-gray-200 dark:border-dark-700" aria-label="站点套路说明表">
      <div class="overflow-x-auto">
        <table class="w-full min-w-[900px] table-fixed text-left text-sm">
          <thead class="border-b border-gray-200 bg-gray-50 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800/60 dark:text-dark-400">
            <tr><th scope="col" class="w-28 px-4 py-3 font-medium">站点</th><th v-for="pattern in patterns" :key="pattern" scope="col" class="px-3 py-3 text-center font-medium">{{ pattern }}</th><th scope="col" class="w-64 px-4 py-3 font-medium">备注</th></tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="site in sites" :key="site.name" class="text-gray-700 dark:text-dark-200">
              <th scope="row" class="px-4 py-4 font-medium">{{ site.name }}</th>
              <td v-for="(flag,index) in site.flags" :key="patterns[index]" class="px-3 py-4 text-center" :class="flag ? 'font-semibold text-amber-700 dark:text-amber-400' : 'text-gray-400'" :aria-label="flag ? '已记录' : '暂无记录'">{{ flag ? '✓' : '/' }}</td>
              <td class="break-words px-4 py-4 text-xs leading-5">{{ site.note || '/' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="card space-y-5 p-6" aria-labelledby="cache-calculator-title" data-testid="cache-cost-calculator">
      <div>
        <h3 id="cache-calculator-title" class="text-lg font-semibold text-gray-900 dark:text-white">缓存命中率费用计算器</h3>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">把当前定价换算为 99% 缓存命中率下的等效定价，比较缓存命中差异带来的输入费用。</p>
      </div>
      <div class="grid grid-cols-3 gap-5">
        <label class="block">
          <span class="field-label">当前定价</span>
          <input v-model="price" type="number" min="0" step="any" class="input w-full" aria-label="当前定价" aria-describedby="cache-price-help">
          <span id="cache-price-help" class="mt-2 block text-xs text-gray-500">单价或倍率均可，折算结果沿用同一单位。</span>
        </label>
        <label class="block">
          <span class="field-label">缓存命中率（%）</span>
          <input v-model="hitRate" type="number" min="0" max="100" step="any" class="input w-full" aria-label="缓存命中率（%）">
          <span class="mt-2 block text-xs text-gray-500">缓存读取 Token 占总输入 Token 的比例。</span>
        </label>
        <label class="block">
          <span class="field-label">缓存读取价格占普通输入价格（%）</span>
          <input v-model="cachePricePercent" type="number" min="0" max="100" step="any" class="input w-full" aria-label="缓存读取价格占普通输入价格（%）" aria-describedby="cache-ratio-help">
          <span id="cache-ratio-help" class="mt-2 block text-xs text-gray-500">默认按普通输入的 10% 计算，可按模型实际价差修改。</span>
        </label>
      </div>
      <div v-if="calculation" class="grid grid-cols-2 gap-5" aria-live="polite" aria-atomic="true">
        <div class="rounded-xl bg-primary-50 p-5 dark:bg-primary-900/20">
          <p class="text-sm text-primary-700 dark:text-primary-300">相当于 99% 命中率的定价</p>
          <p class="mt-2 text-3xl font-semibold tabular-nums text-primary-800 dark:text-primary-200" data-testid="cache-equivalent-price">{{format(calculation.equivalent)}}</p>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">当前定价 {{price}}、缓存命中率 {{hitRate}}%</p>
        </div>
        <div class="rounded-xl bg-gray-50 p-5 dark:bg-dark-800">
          <p class="text-sm text-gray-600 dark:text-dark-300">相比同价、99% 缓存命中率</p>
          <p class="mt-2 text-2xl font-semibold tabular-nums" :class="calculation.change>0?'text-amber-700 dark:text-amber-400':'text-primary-700 dark:text-primary-300'" data-testid="cache-cost-difference">{{difference}}</p>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">输入费用为 {{format(calculation.factor)}} 倍</p>
        </div>
      </div>
      <p v-else role="alert" class="text-sm text-amber-700 dark:text-amber-400">请填写有效数值：定价不小于 0，命中率和缓存价格比例为 0～100%。</p>
      <details class="text-xs leading-6 text-gray-500 dark:text-dark-400">
        <summary class="cursor-pointer">计算方式与范围</summary>
        <p class="mt-2">等效定价 = 当前定价 × [(1 − 当前命中率) + 当前命中率 × 缓存价格比例] ÷ [1% + 99% × 缓存价格比例]。</p>
        <p>比较相同总输入 Token 量，并假设定价按相同比例作用于普通输入和缓存读取；输出费用、缓存写入费用、工具费用不计入。因此结果表示输入费用的等效价格，不代表整次调用的总费用或响应质量。</p>
      </details>
    </section>



  </div>
  </AppLayout>
</template>
