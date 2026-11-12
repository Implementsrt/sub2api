// Move less-used entries after each section's normal links. Call only after
// permission and feature filtering, so folding cannot expose hidden entries.
export const personalSecondaryPaths = ['/monitor', '/subscriptions', '/redeem', '/profile']
export const systemSecondaryPaths = ['/admin/subscriptions', '/admin/announcements', '/admin/proxies', '/admin/redeem', '/admin/promo-codes']

export function splitSidebarItems<T extends { path: string }>(items: T[], secondaryPaths: readonly string[]) {
  const selected = new Set(secondaryPaths)
  return {
    primary: items.filter(item => !selected.has(item.path)),
    secondary: secondaryPaths.flatMap(path => items.filter(item => item.path === path)),
  }
}
