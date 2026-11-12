import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SiteNotesView from './SiteNotesView.vue'
import { timingDifference, timingDuration } from './timing'

describe('personal site notes and 99% cache calculator', () => {
  const create = () => mount(SiteNotesView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } } })
  it('retains the personal observations and labels them as historical notes', () => {
    const wrapper = create()
    expect(wrapper.text()).toContain('个人使用记录')
    for (const name of ['2chat', '奶龙 Waw', '浮生', '极智', '鹿鸣']) expect(wrapper.text()).toContain(name)
  })
  it('converts the same input workload to its price at 99% hit rate', async () => {
    const wrapper = create()
    await wrapper.get('[aria-label="当前定价"]').setValue('0.1')
    await wrapper.get('[aria-label="缓存命中率（%）"]').setValue('85')
    await wrapper.get('[aria-label="缓存读取价格占普通输入价格（%）"]').setValue('10')
    expect(Number(wrapper.get('[data-testid="cache-equivalent-price"]').text())).toBeCloseTo(.1 * .235 / .109, 7)
    expect(wrapper.get('[data-testid="cache-cost-difference"]').text()).toContain('115.60%')
    await wrapper.get('[aria-label="缓存命中率（%）"]').setValue('99')
    expect(wrapper.get('[data-testid="cache-cost-difference"]').text()).toBe('费用相同')
    expect(wrapper.get('[data-testid="cache-equivalent-price"]').text()).toBe('0.1')
  })
  it('accepts zero and rejects empty or out of range input', async () => {
    const wrapper = create()
    const price = wrapper.get('[aria-label="当前定价"]')
    await price.setValue('0')
    expect(wrapper.get('[data-testid="cache-equivalent-price"]').text()).toBe('0')
    await price.setValue('')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await price.setValue('1')
    await wrapper.get('[aria-label="缓存命中率（%）"]').setValue('101')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
})

describe('station timing formatting', () => {
  it('distinguishes missing and zero and preserves negative differences', () => {
    expect(timingDuration(null)).toBe('—')
    expect(timingDuration(0)).toBe('0.00s')
    expect(timingDifference(-1000)).toBe('-1000 ms')
    expect(timingDifference(5)).toBe('+5 ms')
    expect(timingDifference(null)).toBe('—')
  })
})
