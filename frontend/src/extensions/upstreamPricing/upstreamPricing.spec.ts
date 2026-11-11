import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { formatUpstreamAmount, type PricingState, type Preview, type UpstreamEstimate } from '.'
import UpstreamEstimateCell from './UpstreamEstimateCell.vue'
import UpstreamPricingPanel from './UpstreamPricingPanel.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), groups: vi.fn(), preview: vi.fn(), apply: vi.fn(), sync: vi.fn(), schedule: vi.fn(), rollback: vi.fn() }))
vi.mock('./api', () => ({ upstreamPricingAPI: api }))
const i18n = () => createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })
function state(): PricingState {
  return {
    source: { account_id: 7, revision: 1, has_credential: true, active_version: 10, last_success: '2026-11-11T01:00:00Z', next_sync: null, last_attempt: null, last_error: '',
      config: { provider: 'newapi', base_url: 'https://example.com', user_id: '42', group: 'paid', group_id: '', automatic: false, interval_minutes: 60, overrides: [] } },
    catalog: { group: { name: 'paid', multiplier: '0.5' }, models: [{ model: 'mapped', mode: 'token', input: '2', output: '8', cache_read: '0.2', cache_write: null, request: null }], supported: 1, unsupported: 0, fingerprint: 'fixture' },
    runs: [], versions: [], account_changed: false,
  }
}
function panel() { return mount(UpstreamPricingPanel, { props: { accountId: 7 }, global: { plugins: [i18n()] } }) }
function button(wrapper: ReturnType<typeof panel>, text: string) { const value = wrapper.findAll('button').find(v => v.text() === text); expect(value).toBeTruthy(); return value! }

describe('upstream cost display', () => {
  it('distinguishes missing estimates from explicit free requests', () => {
    expect(formatUpstreamAmount()).toBe('—')
    expect(formatUpstreamAmount({ status: 'unavailable', amount: null })).toBe('—')
    expect(formatUpstreamAmount({ status: 'estimated', amount: '0' })).toBe('$0')
    expect(formatUpstreamAmount({ status: 'estimated', amount: '0.000000000001' })).toBe('$0.000000000001')
    expect(formatUpstreamAmount({ status: 'estimated', amount: '-1' })).toBe('—')
  })
  it('shows source evidence and warns about stale prices', () => {
    const estimate: UpstreamEstimate = { status: 'estimated', amount: '0.001', source: 'https://example.com', group: 'paid', model: 'mapped', version_id: 10, multiplier: '0.5', stale: true }
    const wrapper = mount(UpstreamEstimateCell, { props: { row: { upstream_estimate: estimate } }, global: { plugins: [i18n()] } })
    expect(wrapper.text()).toContain('$0.001')
    expect(wrapper.text()).toContain('价格可能过期')
    expect(wrapper.attributes('title')).toContain('https://example.com')
    expect(wrapper.attributes('title')).toContain('版本: 10')
    expect(wrapper.attributes('title')).toContain('不等于站点实际账单')
  })
  it('keeps unsupported prices unknown instead of displaying zero', () => {
    const wrapper = mount(UpstreamEstimateCell, { props: { row: { upstream_estimate: { status: 'unavailable', amount: null, reason: 'missing cache price' } } }, global: { plugins: [i18n()] } })
    expect(wrapper.text()).toBe('—')
    expect(wrapper.attributes('title')).toBe('missing cache price')
  })
})

describe('upstream pricing configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.get.mockImplementation(async () => state())
    api.preview.mockImplementation(async (): Promise<Preview> => ({ id: 'preview-id', catalog: state().catalog!, added: 0, changed: 1, removed: 0 }))
    api.groups.mockResolvedValue([{ name: 'paid', multiplier: '0.5' }])
    api.apply.mockResolvedValue(undefined)
  })
  it('loads local saved state without querying the upstream site', async () => {
    const wrapper = panel(); await flushPromises()
    expect(api.get).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(api.groups).not.toHaveBeenCalled(); expect(api.preview).not.toHaveBeenCalled(); expect(api.sync).not.toHaveBeenCalled()
    expect(wrapper.find('input[type=password]').element).toHaveProperty('value', '')
    wrapper.unmount()
  })
  it('requires a new preview after a connection edit', async () => {
    const wrapper = panel(); await flushPromises()
    await button(wrapper, '预览价格差异').trigger('click'); await flushPromises()
    expect(button(wrapper, '应用此预览').exists()).toBe(true)
    await wrapper.find('input[type=url]').setValue('https://other.example.com')
    expect(wrapper.findAll('button').some(v => v.text() === '应用此预览')).toBe(false)
    expect(button(wrapper, '立即同步已保存配置').attributes()).toHaveProperty('disabled')
    expect(api.apply).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('applies only a server preview and clears the submitted token afterward', async () => {
    const wrapper = panel(); await flushPromises()
    await wrapper.find('input[type=password]').setValue('synthetic-panel-credential')
    await button(wrapper, '预览价格差异').trigger('click'); await flushPromises()
    expect(api.preview.mock.calls[0]?.[2].token).toBe('synthetic-panel-credential')
    await button(wrapper, '应用此预览').trigger('click'); await flushPromises()
    expect(api.apply).toHaveBeenCalledWith(7, 'preview-id', expect.any(AbortSignal))
    expect(wrapper.find('input[type=password]').element).toHaveProperty('value', '')
    expect(wrapper.text()).toContain('已应用价格')
    wrapper.unmount()
  })
  it('does not silently select another group', async () => {
    api.groups.mockResolvedValue([{ name: 'other-group', multiplier: '2' }])
    const wrapper = panel(); await flushPromises()
    await button(wrapper, '读取可用分组').trigger('click'); await flushPromises()
    expect(button(wrapper, '预览价格差异').attributes()).toHaveProperty('disabled')
    expect(api.apply).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('preserves the last applied catalog after an upstream error', async () => {
    api.preview.mockRejectedValue(new Error('面板凭证已失效'))
    const wrapper = panel(); await flushPromises()
    await button(wrapper, '预览价格差异').trigger('click'); await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('面板凭证已失效')
    expect(wrapper.text()).toContain('mapped')
    expect(api.apply).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
