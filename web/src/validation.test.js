import { expect, it } from 'vitest'
import { passwordError } from './validation.js'
import { safeReturn } from './navigation.js'
it('accepts 12 Unicode characters without requiring character combinations', () => {
  expect(passwordError('界'.repeat(12))).toBe('')
  expect(passwordError('a'.repeat(12))).toBe('')
  expect(passwordError('😀'.repeat(128))).toBe('')
  expect(passwordError('a'.repeat(11))).not.toBe('')
  expect(passwordError('😀'.repeat(129))).not.toBe('')
})
it('allows only internal return paths', () => {
  expect(safeReturn('/admin/users?page=2')).toBe('/admin/users?page=2')
  for (const path of ['//evil.example', 'https://evil.example', '/\\evil.example', '/%2fevil.example', '/\nevil.example', undefined]) expect(safeReturn(path)).toBe('/settings')
})
