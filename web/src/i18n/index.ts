// Interface language. Messages are plain typed objects: `en` is the source
// of truth and every other locale must have exactly its shape (a missing or
// extra key fails the typecheck). Parameterised messages are functions.
//
// Components read `messages` (a computed, auto-unwrapped in templates:
// `{{ m.flows.title }}`); plain modules call `msg()` at the moment they
// build a string, never at import time, so a language change applies.
import { computed, ref, watch } from 'vue'
import en from './en'
import es from './es'

export type Messages = typeof en
export const LOCALES = { en: 'English', es: 'Español' } as const
export type Locale = keyof typeof LOCALES
const catalogs: Record<Locale, Messages> = { en, es }
const STORAGE_KEY = 'piworker-locale'

function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && value in LOCALES
}

// The saved choice wins; otherwise the browser's first supported language;
// otherwise English.
export function detectLocale(saved: string | null, languages: readonly string[]): Locale {
  if (isLocale(saved)) return saved
  for (const language of languages) {
    if (typeof language !== 'string') continue
    const base = language.toLowerCase().split('-')[0]
    if (isLocale(base)) return base
  }
  return 'en'
}

function readSaved(): string | null {
  try { return localStorage.getItem(STORAGE_KEY) } catch { return null }
}

export const locale = ref<Locale>(detectLocale(readSaved(), typeof navigator === 'undefined' ? [] : [...(navigator.languages ?? []), navigator.language]))
export const messages = computed<Messages>(() => catalogs[locale.value])
export function msg(): Messages { return catalogs[locale.value] }

export function setLocale(next: Locale) {
  locale.value = next
  try { localStorage.setItem(STORAGE_KEY, next) } catch { /* private mode */ }
}

if (typeof document !== 'undefined') {
  watch(locale, value => { document.documentElement.lang = value }, { immediate: true })
}

// Formatting follows the interface language.
export function formatDate(value: string | number | Date, options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' }) {
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? String(value) : new Intl.DateTimeFormat(locale.value, options).format(date)
}
export function formatDateTime(value: string | number | Date) {
  return formatDate(value, { dateStyle: 'medium', timeStyle: 'short' })
}
export function formatNumber(value: number, options?: Intl.NumberFormatOptions) {
  return new Intl.NumberFormat(locale.value, options).format(value)
}
// "3 minutes ago" / "hace 3 minutos".
export function formatRelative(value: string | number | Date, now = Date.now()) {
  const time = value instanceof Date ? value.getTime() : new Date(value).getTime()
  if (Number.isNaN(time)) return String(value)
  const seconds = Math.round((time - now) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale.value, { numeric: 'auto' })
  const units: [Intl.RelativeTimeFormatUnit, number][] = [['year', 31536000], ['month', 2592000], ['day', 86400], ['hour', 3600], ['minute', 60]]
  for (const [unit, size] of units) if (Math.abs(seconds) >= size) return rtf.format(Math.round(seconds / size), unit)
  return rtf.format(seconds, 'second')
}
