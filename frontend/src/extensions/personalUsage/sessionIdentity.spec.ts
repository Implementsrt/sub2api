import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import SessionNameCell from './SessionNameCell.vue'
import CacheHitRateCell from './CacheHitRateCell.vue'
import type { PersonalUsageLog } from './index'

const global = () => ({ plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })] })

describe('execution session names', () => {
  it('distinguishes child identity and parent correlation', () => {
    const wrapper = mount(SessionNameCell, { props: { row: {
      session_name: 'Pascal · audit', session_id: 'parent-session',
      session_identity: { match: 'thread', thread_id: 'child-thread', parent_thread_id: 'parent-thread', subagent: 'subagent' },
    } as PersonalUsageLog }, global: global() })
    expect(wrapper.text()).toContain('Pascal · audit')
    expect(wrapper.text()).toContain('子代理')
    expect(wrapper.text()).not.toContain('线程未记录')
    expect(wrapper.attributes('title')).toContain('Thread ID: child-thread')
    expect(wrapper.attributes('title')).toContain('Session ID: parent-session')
  })

  it.each(['thread', 'unresolved'] as const)('labels guardian with %s metadata', (match) => {
    const wrapper = mount(SessionNameCell, { props: { row: {
      session_name: 'Window title', session_identity: { match, subagent: 'guardian' },
    } as PersonalUsageLog }, global: global() })
    expect(wrapper.text()).toContain('Guardian 审核')
    expect(wrapper.text().includes('线程未记录')).toBe(match === 'unresolved')
  })

  it('does not present a legacy window title as a known execution thread', () => {
    const wrapper = mount(SessionNameCell, { props: { row: { session_name: 'Window title' } as PersonalUsageLog }, global: global() })
    expect(wrapper.text()).toContain('线程未记录')
    expect(wrapper.attributes('title')).toContain('无法区分父窗口、子代理和审核请求')
  })

  it.each(['thread', 'related_session'] as const)('associates a tool by %s without a missing-thread warning', (match) => {
    const row = {
      session_name: 'Latest session name', session_id: 'parent',
      session_identity: { match, kind: 'tool', ...(match === 'thread' ? { thread_id: 'child' } : {}) },
      cache_monitoring_status: 'not_comparable', input_tokens: 90, cache_read_tokens: 10, cache_creation_tokens: 0,
    } as PersonalUsageLog
    const wrapper = mount(SessionNameCell, { props: { row }, global: global() })
    expect(wrapper.text()).toContain('Latest session name')
    expect(wrapper.text()).toContain('工具调用')
    expect(wrapper.text()).not.toContain('线程未记录')
    expect(wrapper.attributes('title')).toContain('不参与掉缓存比较')
    expect(wrapper.attributes('title').includes('Thread ID: child')).toBe(match === 'thread')
    const rate = mount(CacheHitRateCell, { props: { row }, global: global() })
    expect(rate.get('span').attributes('title')).toContain('工具调用不参与掉缓存比较')
  })

  it('does not invent a session name for an unassociated historical tool', () => {
    const wrapper = mount(SessionNameCell, { props: { row: {
      session_identity: { match: 'related_session', kind: 'tool' },
    } as PersonalUsageLog }, global: global() })
    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).toContain('工具调用')
    expect(wrapper.text()).not.toContain('线程未记录')
    expect(wrapper.attributes('title')).toContain('无会话 ID')
  })

  it.each(['identity_missing', 'not_comparable'] as const)('explains excluded monitoring: %s', (status) => {
    const wrapper = mount(CacheHitRateCell, { props: { row: {
      input_tokens: 90, cache_read_tokens: 10, cache_creation_tokens: 0, cache_monitoring_status: status,
    } as PersonalUsageLog }, global: global() })
    expect(wrapper.text()).toBe('10.00%')
    expect(wrapper.get('span').attributes('title')).toContain('不参与掉缓存比较')
    expect(wrapper.find('[data-testid="cache-drop-alert"]').exists()).toBe(false)
  })
})
