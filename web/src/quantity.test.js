// 共享数量工具单测（Vitest，node 环境）：
// - INV-WEB-018 库存数量字符串 → 千分单位转换与大小比较（修复小数点未去除）。
// - REC-WEB-011 食谱原料数量同契约（与库存共用 quantity.js）。
import { describe, expect, it } from 'vitest'
import { QUANTITY_RE, quantityMilliString, compareQuantity } from './quantity.js'

// INV-WEB-018 / REC-WEB-011：合法输入 → 千分单位字符串（无前导零）。
describe.each([
  ['250', '250000'],
  ['250.5', '250500'],
  ['250.125', '250125'],
  // 上限边界：999999.999 不再被误拒（修复前小数点混入导致长度误判）。
  ['999999.999', '999999999'],
  ['0.5', '500'],
  ['0.125', '125'],
  ['0', '0'],
  ['0.000', '0'],
  ['1000000', '1000000000'],   // 允许：恰等于上限，由调用方上限判断放行
  ['1000000.001', '1000000001'], // 超限：仍正常转换，由调用方上限判断拒绝
])('quantityMilliString(%j)', (input, expected) => {
  it(`返回 ${expected}`, () => {
    expect(quantityMilliString(input)).toBe(expected)
  })
})

// INV-WEB-018 / REC-WEB-011：非法输入返回 ''（不发请求、不做换算）。
describe.each([
  ['abc'],      // 非数字
  ['1.2345'],   // 四位小数
  ['-5'],       // 负数
  ['007'],      // 前导零
  [''],         // 空串
  ['1e3'],      // 科学计数法
])('quantityMilliString(%j) 非法输入', input => {
  it('返回空串', () => {
    expect(quantityMilliString(input)).toBe('')
  })
})

// INV-WEB-018 / REC-WEB-011：QUANTITY_RE 形态校验与转换函数保持一致。
describe('QUANTITY_RE', () => {
  it.each([
    ['250', true],
    ['250.5', true],
    ['0', true],
    ['0.125', true],
    ['abc', false],
    ['1.2345', false],
    ['-5', false],
    ['007', false],
    ['', false],
  ])('QUANTITY_RE.test(%j) === %s', (input, expected) => {
    expect(QUANTITY_RE.test(input)).toBe(expected)
  })
})

// INV-WEB-018：千分单位字符串比较（先长度后字典序；驱动 increaseBlocked 判定）。
describe('compareQuantity', () => {
  it('长度不同按长度比较："500" < "1250"', () => {
    expect(compareQuantity('500', '1250')).toBeLessThan(0)
    expect(compareQuantity('1250', '500')).toBeGreaterThan(0)
  })
  it('等长按字典序："500" < "501"', () => {
    expect(compareQuantity('500', '501')).toBe(-1)
    expect(compareQuantity('501', '500')).toBe(1)
    expect(compareQuantity('500', '500')).toBe(0)
  })
  it('与 quantityMilliString 联动：0.5 < 1.25 < 250.5', () => {
    const a = quantityMilliString('0.5')
    const b = quantityMilliString('1.25')
    const c = quantityMilliString('250.5')
    expect(compareQuantity(a, b)).toBeLessThan(0)
    expect(compareQuantity(b, c)).toBeLessThan(0)
  })
})
