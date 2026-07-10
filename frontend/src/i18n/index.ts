import { createI18n } from 'vue-i18n'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

export type LocaleCode = 'en-US' | 'zh-CN'

const LOCALE_KEY = 'asterrouter_locale'
const DEFAULT_LOCALE: LocaleCode = 'en-US'

function isLocale(value: string): value is LocaleCode {
  return value === 'en-US' || value === 'zh-CN'
}

function detectLocale(): LocaleCode {
  const saved = localStorage.getItem(LOCALE_KEY)
  if (saved && isLocale(saved)) {
    return saved
  }
  if (navigator.language.toLowerCase().startsWith('zh')) {
    return 'zh-CN'
  }
  return DEFAULT_LOCALE
}

const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: {
    'en-US': enUS,
    'zh-CN': zhCN
  }
})

export async function initI18n(): Promise<void> {
  document.documentElement.setAttribute('lang', getLocale())
}

export function getLocale(): LocaleCode {
  const locale = i18n.global.locale.value
  return isLocale(locale) ? locale : DEFAULT_LOCALE
}

export function setLocale(locale: LocaleCode): void {
  i18n.global.locale.value = locale
  localStorage.setItem(LOCALE_KEY, locale)
  document.documentElement.setAttribute('lang', locale)
}

export const availableLocales = [
  { code: 'en-US' as const, label: 'English' },
  { code: 'zh-CN' as const, label: '简体中文' }
]

export default i18n
