export function safeReturn(value) {
  return typeof value === 'string' && /^\/(?!\/)/.test(value) && !/[\\\u0000-\u0020]/.test(value) && !/%(?:2f|5c|00|0a|0d)/i.test(value) ? value : '/settings'
}
