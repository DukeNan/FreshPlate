import { watch } from 'vue'
import { auth } from './auth.js'

let snapshot = null
watch(() => auth.generation, () => { snapshot = null }, { flush: 'sync' })

export function rememberRecommendation(item) {
  snapshot = { item, generation: auth.generation }
}

export function takeRecommendation(id) {
  const saved = snapshot
  snapshot = null
  return saved?.generation === auth.generation && saved.item.recipe.id === id ? saved.item : null
}

export function matchingRecommendation(item, version) {
  return item?.recipe.version === version ? item : null
}

// Match Go strings.TrimSpace (Unicode White_Space), including NEL and excluding BOM.
export function normalizeRecommendationQuery(query) {
  return query.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}

export function recommendationQueryError(query) {
  return [...normalizeRecommendationQuery(query)].length > 100 ? '食谱名称搜索最多 100 个字符（忽略首尾空白）。' : ''
}

export function unitLabel(unit) { return { g: '克', ml: '毫升', piece: '个' }[unit] || unit }

export function ingredientStatus(item) {
  if (item.state === 'ok') return `充足 · 已确认 ${item.have} ${unitLabel(item.unit)}`
  if (item.state === 'unknown') return '未知（库存单位无法换算）'
  return `缺 ${item.state === 'short' ? item.lack : item.need} ${unitLabel(item.unit)}`
}

export function missingSummary(ingredients) {
  return ingredients.filter(item => item.state !== 'ok').map(item => `${item.food.name} ${ingredientStatus(item)}`).join('、')
}

export function filteredCount(stats) {
  return stats.filtered_by_restrictions + stats.filtered_incomplete_metadata + stats.filtered_disabled_reference
}

export function recommendationEmpty(stats, total, query, availability) {
  if (stats.candidates === 0) return { text: '暂无可推荐的食谱', hint: '前往食谱页创建或发布食谱。', to: '/recipes' }
  if (filteredCount(stats) === stats.candidates) return { text: `你的 ${stats.candidates} 个食谱因过敏原/忌口、信息不完整或引用停用食材未列入`, hint: '请检查饮食设置及食谱数据。' }
  if (total > 0) return null
  const q = normalizeRecommendationQuery(query)
  if (q && availability === 'can_cook') return { text: `没有匹配「${q}」且可立即做的食谱`, hint: '请调整搜索条件或关闭仅可立即做筛选。' }
  if (availability === 'can_cook') return { text: '暂无可立即做的食谱', hint: '关闭筛选可查看缺料清单。' }
  return { text: '无匹配结果', hint: '请修改或清空搜索。' }
}
