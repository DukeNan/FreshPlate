import { expect, test } from 'vitest'
import { chinaDate, chinaDateTime } from './date.js'

test('uses China midnight regardless of browser time zone', () => {
  expect(chinaDate(new Date('2026-10-26T15:59:59Z'))).toBe('2026-10-26')
  expect(chinaDate(new Date('2026-10-26T16:00:00Z'))).toBe('2026-10-27')
  expect(chinaDate(new Date('1991-06-30T15:30:00Z'))).toBe('1991-06-30')
})

test('displays historical meal times at fixed UTC+8', () => {
  expect(chinaDateTime(new Date('1991-07-01T00:00:00Z'))).toBe(chinaDateTime(new Date('1991-07-01T08:00:00+08:00')))
  expect(chinaDateTime(new Date('1991-07-01T00:00:00Z'))).toContain('08:00:00')
})
