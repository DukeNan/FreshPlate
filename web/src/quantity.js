// 数量字符串共享工具（库存 INV-WEB-018 / 食谱 REC-WEB-011）：
// 数量契约（方案 §3.1）：只接受 ASCII 十进制，0 或非零整数开头，至多三位
// 小数；全程字符串运算，绝不经 Number 换算（避免浮点表示进入 payload）。

// 合法数量字符串：0 或非零整数开头，至多三位小数（拒绝 "007"、"-5"、
// "1.2345"、"abc" 等形态）。
export const QUANTITY_RE = /^(0|[1-9][0-9]*)(\.[0-9]{1,3})?$/

// 数量字符串 → 千分单位十进制字符串（无前导零；非法输入返回 ''）。
// 小数部分捕获组含小数点，需先 slice(1) 去点再 padEnd 补零到三位
// （修复 "250.5" 误转 "250.50" 一类的错位拼接）。
export function quantityMilliString(text) {
  const m = QUANTITY_RE.exec(text || '')
  if (!m) return ''
  const frac = (m[2] ? m[2].slice(1) : '').padEnd(3, '0')
  return m[1] === '0' ? frac.replace(/^0+(?=\d)/, '') || '0' : `${m[1]}${frac}`
}

// 比较两个千分单位十进制字符串的数值大小（无前导零：先比长度再比字典序）。
// 返回负数表示 a<b，0 表示相等，正数表示 a>b。
export function compareQuantity(a, b) {
  if (a.length !== b.length) return a.length - b.length
  return a < b ? -1 : a > b ? 1 : 0
}
