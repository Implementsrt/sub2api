import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { cacheHitRate, cacheHitRateColor, formatCacheHitRate, type PersonalUsageLog } from './index'
import CacheHitRateCell from './CacheHitRateCell.vue'
import CacheMonitoringCell from './CacheMonitoringCell.vue'

const row = (rate: number) => ({ input_tokens: 100 - rate, cache_read_tokens: rate, cache_creation_tokens: 0 })
const i18n = () => createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })

describe('personal usage cache rate', () => {
  it('uses uncached input, reads and writes exactly once', () => {
    expect(cacheHitRate({ input_tokens: 10, cache_read_tokens: 80, cache_creation_tokens: 10 })).toBe(.8)
    expect(formatCacheHitRate(row(98))).toBe('98.00%')
  })

  it.each([
    [98, 'font-bold text-green-800'], [97.99, 'text-green-600'],
    [95, 'text-green-600'], [94.99, 'text-yellow-600'],
    [90, 'text-yellow-600'], [89.99, 'text-red-600'], [0, 'text-red-600'],
  ])('colors the %s%% boundary', (rate, color) => {
    expect(cacheHitRateColor(row(rate))).toContain(color)
  })

  it('renders unknown and invalid usage without a false zero rate', () => {
    for (const values of [
      { input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0 },
      { input_tokens: -1, cache_read_tokens: 0, cache_creation_tokens: 0 },
      { input_tokens: NaN, cache_read_tokens: 1, cache_creation_tokens: 0 },
    ]) expect(formatCacheHitRate(values)).toBe('—')
  })

  it('renders the colored percentage and formula', () => {
    const wrapper = mount(CacheHitRateCell, { props: { row: row(99) as PersonalUsageLog }, global: { plugins: [i18n()] } })
    expect(wrapper.text()).toBe('99.00%')
    expect(wrapper.get('span').classes()).toContain('font-bold')
    expect(wrapper.get('span').attributes('title')).toContain('普通输入')
    expect(wrapper.find('[data-testid="cache-drop-alert"]').exists()).toBe(false)
  })
})

describe('cache monitoring cell', () => {
  it('shows evidence for a detected cache drop', () => {
    const wrapper = mount(CacheHitRateCell, {
      props: { row: { ...row(70), session_id: 'session', cache_monitoring_status: 'ready', cache_drop: {
        previous_usage_id: 10, previous_created_at: '2026-11-11T10:00:00Z', gap_seconds: 30,
        previous_hit_rate: .98, current_hit_rate: .7, drop_points: 28,
        threshold_points: 20, previous_input_total: 100, current_input_total: 100,
      } } as PersonalUsageLog }, global: { plugins: [i18n()] },
    })
    expect(wrapper.text()).toContain('70.00%')
    const badge = wrapper.get('[data-testid="cache-drop-alert"]')
    expect(badge.text()).toBe('掉缓存')
    expect(badge.classes()).toContain('text-red-800')
    expect(badge.attributes('title')).toContain('98.00% → 本次: 70.00%')
    expect(badge.attributes('title')).toContain('28.00 个百分点')
    expect(badge.attributes('tabindex')).toBe('0')
  })

  it.each([
    [{}, '无会话 ID'],
    [{ session_id: 'x' }, '暂不可用'],
    [{ session_id: 'x', cache_monitoring_status: 'unavailable' }, '暂不可用'],
    [{ session_id: 'x', cache_monitoring_status: 'ready' }, '未检出'],
  ])('distinguishes monitoring availability', (values, text) => {
    const wrapper = mount(CacheMonitoringCell, { props: { row: values as PersonalUsageLog }, global: { plugins: [i18n()] } })
    expect(wrapper.text()).toBe(text)
    expect(wrapper.find('[data-testid="cache-drop-alert"]').exists()).toBe(false)
  })
})
