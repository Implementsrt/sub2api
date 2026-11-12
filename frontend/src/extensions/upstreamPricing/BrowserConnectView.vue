<template>
  <main class="mx-auto max-w-xl space-y-5 p-8">
    <h1 class="text-xl font-semibold">连接上游定价面板</h1>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <template v-if="site && !done">
      <p class="break-all text-sm">正在连接：{{ site }}</p>
      <p class="text-sm text-gray-500">请从该站点个人设置取得面板访问令牌。仅验证读取权限，确认应用前不会修改已保存的定价配置。无需提供密码或模型调用 Key。</p>
      <form class="space-y-4" @submit.prevent="submit">
        <label class="block text-sm">面板访问令牌<input v-model="token" required type="password" autocomplete="off" class="input mt-1" /></label>
        <label v-if="provider === 'newapi'" class="block text-sm">New API 用户 ID（如需要）<input v-model.trim="userId" inputmode="numeric" autocomplete="off" class="input mt-1" /></label>
        <button class="btn btn-primary" :disabled="busy">{{ busy ? '正在验证…' : '授权验证并导入' }}</button>
      </form>
    </template>
    <p v-if="done" role="status" class="text-green-700">已验证。请回到原 Sub2API 页面选择分组，预览并应用。此页可以关闭。</p>
  </main>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
const site = ref(''), token = ref(''), userId = ref(''), error = ref(''), busy = ref(false), done = ref(false)
const provider = ref('newapi')
let ticket = window.location.hash.slice(1)
window.history.replaceState(window.history.state, '', window.location.pathname)
const controller = new AbortController()
async function request(action: string, body: Record<string, string>) {
  const response = await fetch(`/api/v1/upstream-pricing/browser-import/${action}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), cache: 'no-store', credentials: 'omit', signal: controller.signal })
  const result = await response.json()
  if (!response.ok || result.code !== 0) throw new Error(result.message || '连接失败，请重新发起')
  return result.data
}
onMounted(async () => {
  try {
    if (!/^[a-f0-9]{48}$/.test(ticket)) throw new Error('连接已失效，请从管理员上游定价页面重新发起')
    const info = await request('info', { ticket })
    site.value = info.base_url; provider.value = info.provider || 'newapi'; userId.value = info.user_id || ''
  }
  catch (e) { error.value = (e as Error).message }
})
async function submit() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await request('complete', { ticket, token: token.value, user_id: userId.value }); done.value = true }
  catch (e) { error.value = (e as Error).message; site.value = '' }
  finally { token.value = ''; ticket = ''; busy.value = false }
}
onBeforeUnmount(() => { controller.abort(); token.value = ''; ticket = '' })
</script>
