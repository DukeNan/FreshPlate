import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
vi.mock('./auth.js', () => ({ auth: reactive({ generation: 1 }) }))
import { auth } from './auth.js'
import { filteredCount, ingredientStatus, matchingRecommendation, missingSummary, normalizeRecommendationQuery, recommendationEmpty, recommendationQueryError, rememberRecommendation, takeRecommendation } from './recommendations.js'

const stats = { candidates: 5, filtered_by_restrictions: 1, filtered_incomplete_metadata: 1, filtered_disabled_reference: 1, has_usable_inventory: false }
const snapshot = { recipe: { id: 8, version: 3 }, can_cook: false, missing_count: 1, ingredients: [{ food: { id: 1, name: '鸡蛋' }, state: 'short', unit: 'piece', need: '2', have: '1', lack: '1' }] }
beforeEach(() => { auth.generation++; takeRecommendation(8) })

describe('recommendation empty states', () => {
  it('RECO-WEB-001 sums all three exclusive safety counts', () => { expect(filteredCount(stats)).toBe(3) })
  it('RECO-WEB-002 prioritizes no candidates over display filters and inventory banner', () => {
    expect(recommendationEmpty({ ...stats, candidates: 0 }, 0, '鸡蛋', 'can_cook')).toMatchObject({ text: '暂无可推荐的食谱', to: '/recipes' })
  })
  it('RECO-WEB-003 prioritizes all safety filtered over display filters', () => {
    expect(recommendationEmpty({ ...stats, candidates: 3 }, 0, '鸡蛋', 'can_cook').text).toBe('你的 3 个食谱因过敏原/忌口、信息不完整或引用停用食材未列入')
  })
  it.each([
    ['', 'all', '无匹配结果'],
    [' 鸡蛋 ', 'all', '无匹配结果'],
    [' ', 'can_cook', '暂无可立即做的食谱'],
    [' 鸡蛋 ', 'can_cook', '没有匹配「鸡蛋」且可立即做的食谱'],
  ])('RECO-WEB-010 uses combined display filters %j / %s', (q, availability, text) => {
    expect(recommendationEmpty(stats, 0, q, availability).text).toBe(text)
  })
  it('RECO-WEB-004 does not report empty when total is positive, even on an empty page or without usable inventory', () => {
    expect(recommendationEmpty(stats, 1, '', 'all')).toBeNull()
  })
})

describe('recommendation snapshot', () => {
  it('RECO-WEB-005 only transfers the clicked recipe once without writing browser storage', () => {
    rememberRecommendation(snapshot)
    expect(takeRecommendation(8)).toBe(snapshot)
    expect(takeRecommendation(8)).toBeNull()
  })
  it('RECO-WEB-006 does not reuse a different recipe or a direct visit', () => {
    expect(takeRecommendation(8)).toBeNull()
    rememberRecommendation(snapshot)
    expect(takeRecommendation(9)).toBeNull()
    expect(takeRecommendation(8)).toBeNull()
  })
  it('RECO-WEB-007 clears immediately when identity generation changes, including switching away and back', () => {
    const generation = auth.generation
    rememberRecommendation(snapshot)
    auth.generation++
    auth.generation = generation
    expect(takeRecommendation(8)).toBeNull()
  })
  it('RECO-WEB-008 drops the whole match on version mismatch', () => {
    expect(matchingRecommendation(snapshot, 3)).toBe(snapshot)
    expect(matchingRecommendation(snapshot, 4)).toBeNull()
    expect(matchingRecommendation(null, 3)).toBeNull()
  })
})

describe('ingredient descriptions', () => {
  it.each([
    ['ok', '充足 · 已确认 1 个'],
    ['short', '缺 1 个'],
    ['missing', '缺 2 个'],
    ['unknown', '未知（库存单位无法换算）'],
  ])('RECO-WEB-011 describes %s with text and exact quantities', (state, text) => {
    expect(ingredientStatus({ ...snapshot.ingredients[0], state })).toBe(text)
  })
  it('RECO-WEB-009 summarizes all non-ok ingredients including unknown without treating null as zero', () => {
    expect(missingSummary([...snapshot.ingredients, { food: { name: '酱油' }, state: 'unknown', have: null, lack: null }, { food: { name: '盐' }, state: 'ok' }])).toBe('鸡蛋 缺 1 个、酱油 未知（库存单位无法换算）')
  })
})

// RECO-WEB-012
it('RECO-WEB-012 validates trimmed Unicode code points at the 100-character boundary', () => {
  for (const character of ['菜', '𠮷']) {
    expect(recommendationQueryError(`  ${character.repeat(100)}  `)).toBe('')
    expect(recommendationQueryError(character.repeat(101))).toContain('100')
  }
  expect(recommendationQueryError('   ')).toBe('')
})

it('RECO-WEB-013 matches Go whitespace normalization at NEL and BOM boundaries', () => {
  const query = '菜'.repeat(100)
  expect(normalizeRecommendationQuery(`\u0085${query}\u0085`)).toBe(query)
  expect(recommendationQueryError(`\u0085${query}\u0085`)).toBe('')
  expect(normalizeRecommendationQuery(`\uFEFF${query}`)).toBe(`\uFEFF${query}`)
  expect(recommendationQueryError(`\uFEFF${query}`)).toContain('100')
  expect(recommendationEmpty(stats, 0, '\uFEFF', 'can_cook').text).toBe('没有匹配「\uFEFF」且可立即做的食谱')
  expect(recommendationEmpty(stats, 0, '\u0085', 'can_cook').text).toBe('暂无可立即做的食谱')
})
