import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'
import { createI18n } from 'vue-i18n'
import { i18n, loadLocaleMessages } from '..'
import en from '../locales/en'
import zh from '../locales/zh'
import { formatRelativeTime } from '@/utils/format'

function collectMessages(node: unknown, path = ''): [string, string][] {
  if (typeof node === 'string') return [[path, node]]
  if (!node || typeof node !== 'object') return []
  return Object.entries(node).flatMap(([key, value]) =>
    collectMessages(value, path ? `${path}.${key}` : key)
  )
}

function parameters(message: string) {
  const { ast } = baseCompile(message, { jit: true })
  const cases = ast.body.type === 1 ? ast.body.cases : [ast.body]
  const named = new Set<string>()
  const list = new Set<number>()
  for (const branch of cases) {
    for (const item of branch.items) {
      if (item.type === 4) named.add(item.key)
      if (item.type === 5) list.add(item.index)
    }
  }
  return { named: [...named], list: [...list] }
}

describe('real locale interpolation', () => {
  const initialLocale = i18n.global.locale.value

  beforeAll(async () => {
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')
    i18n.global.locale.value = 'zh'
  })

  afterAll(() => {
    i18n.global.locale.value = initialLocale
    vi.useRealTimers()
  })

  it.each([
    ['admin.accounts.startingTestForAccount', { name: 'Account A' }, '开始测试账号：Account A'],
    ['admin.accounts.testAccountTypeLabel', { type: 'oauth' }, '账号类型：oauth'],
    ['admin.accounts.usingModel', { model: 'test-model' }, '使用模型：test-model'],
    ['admin.accounts.bulkActions.selectedAll', { count: 12 }, '已选择全部 12 个账号'],
    ['admin.ops.diagnosis.cpuHigh', { usage: '85.2' }, 'CPU使用率偏高 (85.2%)'],
    ['admin.ops.diagnosis.ttftHigh', { ttft: 2500 }, '首 Token 时间偏高 (2500ms)'],
    ['admin.ops.diagnosis.upstreamCritical', { rate: '25.0' }, '上游错误率严重偏高 (25.0%)'],
    ['admin.ops.diagnosis.errorHigh', { rate: '30.0' }, '错误率过高 (30.0%)'],
    ['admin.ops.diagnosis.slaCritical', { sla: '89.50' }, 'SLA 严重低于目标 (89.50%)'],
    ['admin.ops.diagnosis.healthCritical', { score: 35 }, '综合健康评分过低 (35)']
  ])('interpolates %s', (key, values, expected) => {
    expect(i18n.global.t(key, values)).toBe(expected)
  })

  it('formats recent usage in minutes, hours and days through the global helper', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-18T10:00:00Z'))
    expect(formatRelativeTime('2026-11-18T09:55:00Z')).toBe('5分钟前')
    expect(formatRelativeTime('2026-11-18T08:00:00Z')).toBe('2小时前')
    expect(formatRelativeTime('2026-11-15T10:00:00Z')).toBe('3天前')
    vi.useRealTimers()
  })

  for (const [locale, messages] of [['zh', zh], ['en', en]] as const) {
    const entries = collectMessages(messages)
      .map(([key, message]) => ({ key, message, ...parameters(message) }))
      .filter(({ named, list }) => named.length > 0 || list.length > 0)

    it(`${locale}: substitutes parameters in all ${entries.length} messages`, () => {
      const errors: string[] = []
      for (const { key, named, list } of entries) {
        const values = list.length
          ? Array.from({ length: Math.max(...list) + 1 }, () => 7)
          : Object.fromEntries(named.map((name) => [name, 7]))
        const output = i18n.global.t(key, values, { locale })
        const tokens = [...named.map((name) => `{${name}}`), ...list.map((index) => `{${index}}`)]
        if (output === key || tokens.some((token) => output.includes(token))) errors.push(key)
      }
      expect(errors, `${locale}: ${errors.length} messages failed interpolation`).toEqual([])
    })
  }

  it('compiles strings without eval or the Function constructor under strict CSP', () => {
    const instance = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { probe: 'CSP probe: {value}', literal: "user{'@'}example.com" } }
    })
    const constructor = vi.spyOn(globalThis, 'Function').mockImplementation(() => {
      throw new Error('CSP blocks unsafe-eval')
    })
    const evaluate = vi.spyOn(globalThis, 'eval').mockImplementation(() => {
      throw new Error('CSP blocks unsafe-eval')
    })
    try {
      expect(instance.global.t('probe', { value: 'compiled' })).toBe('CSP probe: compiled')
      expect(instance.global.t('literal')).toBe('user@example.com')
      expect(constructor).not.toHaveBeenCalled()
      expect(evaluate).not.toHaveBeenCalled()
    } finally {
      constructor.mockRestore()
      evaluate.mockRestore()
      instance.dispose()
    }
  })
})
