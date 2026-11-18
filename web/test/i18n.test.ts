import { expect, test } from 'bun:test'
import en from '../src/i18n/en'
import es from '../src/i18n/es'
import { detectLocale } from '../src/i18n'

test('a saved language wins over the browser', () => {
  expect(detectLocale('es', ['en-US'])).toBe('es')
})
test('the first supported browser language is used, by its base tag', () => {
  expect(detectLocale(null, ['fr-FR', 'es-AR', 'en'])).toBe('es')
})
test('unknown or invalid languages fall back to English', () => {
  expect(detectLocale('xx', ['fr', 'de'])).toBe('en')
})

// The typecheck enforces the shape; this also catches empty strings and
// keys whose kind (text vs. function) differs between languages.
function walk(a: any, b: any, path: string[] = []): string[] {
  const problems: string[] = []
  for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
    const p = [...path, key].join('.')
    if (!(key in a) || !(key in b)) { problems.push(`missing ${p}`); continue }
    if (typeof a[key] !== typeof b[key]) problems.push(`kind ${p}`)
    else if (typeof a[key] === 'object') problems.push(...walk(a[key], b[key], [...path, key]))
    else if (typeof a[key] === 'string' && (!a[key].trim() || !b[key].trim())) problems.push(`empty ${p}`)
  }
  return problems
}
test('every English message has a Spanish one of the same kind', () => {
  expect(walk(en, es)).toEqual([])
})
