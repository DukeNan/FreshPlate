export function chinaDate(date = new Date()) {
  return new Date(date.getTime() + 8 * 60 * 60 * 1000).toISOString().slice(0, 10)
}

export function chinaDateTime(date) {
  return new Date(date.getTime() + 8 * 60 * 60 * 1000).toLocaleString('zh-CN', { timeZone: 'UTC' })
}
