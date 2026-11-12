import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import TimingComparisonCell from './TimingComparisonCell.vue'
import type { UsageLog } from '@/types'
import { fetchTimingComparison } from './timing'

vi.mock('./timing', async () => ({ ...await vi.importActual('./timing'), fetchTimingComparison: vi.fn() }))
const row = { id: 1, account_id: 2, first_token_ms: 50, duration_ms: 2000, output_tokens: 10 } as UsageLog
const mountCell = (allowComparison = true) => mount(TimingComparisonCell, { props: { row, allowComparison }, global: { stubs: { Teleport: true }, plugins: [createI18n({ legacy: false, locale: 'zh', missingWarn: false, fallbackWarn: false })] } })
afterEach(() => { vi.clearAllMocks() })

describe('on-demand station timing', () => {
  it('does not query while rendering or expose controls to ordinary users', () => {
    const wrapper = mountCell(false)
    expect(wrapper.find('button').exists()).toBe(false)
    expect(fetchTimingComparison).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('loads only on click and shows exact station measurements and precision', async () => {
    vi.mocked(fetchTimingComparison).mockResolvedValue({ status: 'exact', local: { duration_ms: 2000, first_token_ms: 50 }, station: { id: 'r', precision_ms: 1000, duration_ms: 1000, first_token_ms: 10 }, duration_difference_ms: 1000, first_token_difference_ms: 40 } as never)
    const wrapper = mountCell()
    expect(fetchTimingComparison).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(fetchTimingComparison).toHaveBeenCalledWith(2, 1, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('已匹配同一次调用')
    expect(wrapper.text()).toContain('+1000 ms')
    expect(wrapper.text()).toContain('总耗时精度为秒')
    wrapper.unmount()
  })
  it('rejects stale replies after changing usage rows', async () => {
    let resolve!: (result: never) => void
    vi.mocked(fetchTimingComparison).mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountCell()
    await wrapper.get('button').trigger('click')
    await wrapper.setProps({ row: { ...row, id: 3 } })
    resolve({ status: 'exact', local: {}, station: {}, duration_difference_ms: 9999 } as never)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('9999')
    wrapper.unmount()
  })
})
