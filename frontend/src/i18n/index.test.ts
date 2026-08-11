import { beforeEach, describe, expect, it } from 'vitest'
import { getLocale, initI18n, setLocale } from './index'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

function translationKeys(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value).flatMap(([key, child]) => translationKeys(child, prefix ? `${prefix}.${key}` : key))
}

describe('i18n locale state', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('persists the selected locale and updates the document language', () => {
    setLocale('zh-CN')

    expect(getLocale()).toBe('zh-CN')
    expect(localStorage.getItem('asterrouter_locale')).toBe('zh-CN')
    expect(document.documentElement.lang).toBe('zh-CN')
  })

  it('initializes the document language from current state', async () => {
    document.documentElement.removeAttribute('lang')

    await initI18n()

    expect(document.documentElement.lang).toBe('en-US')
  })

  it('keeps marketing and routing policy translation keys symmetric', () => {
    for (const namespace of ['marketing', 'routingPolicy'] as const) {
      expect(translationKeys(enUS[namespace]).sort()).toEqual(translationKeys(zhCN[namespace]).sort())
    }
  })
})
