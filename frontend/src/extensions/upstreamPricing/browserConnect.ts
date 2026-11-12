// Runs only after an explicit click on the selected station. No browser storage
// or login sessions are accessed. The fragment contains a short-lived ticket,
// never a station credential.
export function stationConnectBookmark(site: string, receiver: string): string {
  const expected = new URL(site).origin
  const destination = new URL(receiver)
  if (!['http:', 'https:'].includes(destination.protocol) || !['localhost', '127.0.0.1', '[::1]'].includes(destination.hostname)) throw new Error('需要本机连接地址')
  return 'javascript:' + encodeURIComponent(`(()=>{if(location.origin!==${JSON.stringify(expected)}){alert('请先打开指定站点');return}if(confirm('连接到本机 Sub2API？将在本机页面验证你提供的面板访问令牌。'))window.open(${JSON.stringify(receiver)},'_blank','noopener,noreferrer')})()`)
}
