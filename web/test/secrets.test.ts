import { expect, test } from 'bun:test'
import { AlertTriangle } from 'lucide-vue-next'
import {
  DEFAULT_SHORT_BELOW,
  isShortValue,
  secretList,
  shortSecretNotice,
  shortSecretsSummary
} from '../src/lib/secrets'

test('the listing keeps names and short marks only', () => {
  const list = secretList({
    secrets: [
      { name: 'pin', short: true },
      { name: 'api_key', short: false },
      // Anything else a response might carry is dropped.
      { name: 'token', short: false, value: 'leak', length: 4 },
      { name: 42, short: true },
      null
    ],
    shortBelow: 16
  })
  expect(list).toEqual({
    secrets: [
      { name: 'pin', short: true },
      { name: 'api_key', short: false },
      { name: 'token', short: false }
    ],
    shortBelow: 16
  })
})

test('a listing without secrets or threshold reads as empty, with the default threshold', () => {
  expect(DEFAULT_SHORT_BELOW).toBe(16)
  expect(secretList(null)).toEqual({ secrets: [], shortBelow: 16 })
  expect(secretList({ secrets: null, shortBelow: 'x' })).toEqual({ secrets: [], shortBelow: 16 })
  expect(secretList({ secrets: [], shortBelow: 20 }).shortBelow).toBe(20)
  // Only a strict true marks a value short.
  expect(secretList({ secrets: [{ name: 'a', short: 'yes' }] }).secrets).toEqual([{ name: 'a', short: false }])
})

test('a typed value is short below the threshold, counting characters, not bytes', () => {
  expect(isShortValue('')).toBe(true)
  expect(isShortValue('1234')).toBe(true)
  expect(isShortValue('a'.repeat(15))).toBe(true)
  expect(isShortValue('a'.repeat(16))).toBe(false)
  // Multi-byte characters count once, as the server counts runes.
  expect(isShortValue('é'.repeat(15))).toBe(true)
  expect(isShortValue('日'.repeat(16))).toBe(false)
  expect(isShortValue('🔑'.repeat(15))).toBe(true)
  expect(isShortValue('🔑'.repeat(16))).toBe(false)
  expect(isShortValue('a'.repeat(19), 20)).toBe(true)
  expect(isShortValue('a'.repeat(20), 20)).toBe(false)
})

test('a short secret is marked with an icon and words, never color alone', () => {
  const notice = shortSecretNotice(16)
  expect(notice.icon).toBe(AlertTriangle)
  expect(notice.label).toBe('Short value')
  expect(notice.text).toBe('An agent allowed to deploy flows could guess it. Use 16 characters or more.')
  expect(shortSecretNotice(20).text).toContain('Use 20 characters or more.')
})

test('the Agents page summary counts the short secrets', () => {
  const item = (name: string, short: boolean) => ({ name, short })
  expect(shortSecretsSummary(null)).toEqual({ short: null, text: 'Could not check the secrets for short values.' })
  expect(shortSecretsSummary({ secrets: [], shortBelow: 16 })).toEqual({ short: 0, text: 'No secrets are stored.' })
  expect(shortSecretsSummary({ secrets: [item('a', false), item('b', false)], shortBelow: 16 })).toEqual({
    short: 0, text: 'No secret is short: every value has 16 characters or more.'
  })
  expect(shortSecretsSummary({ secrets: [item('a', true), item('b', false)], shortBelow: 16 })).toEqual({
    short: 1, text: '1 of 2 secrets is short (fewer than 16 characters): an agent could guess it.'
  })
  expect(shortSecretsSummary({ secrets: [item('a', true), item('b', true), item('c', false)], shortBelow: 16 })).toEqual({
    short: 2, text: '2 of 3 secrets are short (fewer than 16 characters): an agent could guess them.'
  })
})
