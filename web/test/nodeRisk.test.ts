import { expect, test } from 'bun:test'
import { nodeRisk } from '../src/lib/nodeRisk'
import { setLocale } from '../src/i18n'

test('a node risk reads as a label, an icon and an explanation', () => {
  setLocale('en')
  expect(nodeRisk('safe')).toMatchObject({ label: 'Safe', icon: 'ShieldCheck', marked: false })
  expect(nodeRisk('external')).toMatchObject({ label: 'External', marked: true })
  expect(nodeRisk('external').hint).toContain('other services')
  expect(nodeRisk('privileged')).toMatchObject({ label: 'Privileged', icon: 'ShieldAlert', variant: 'destructive', marked: true })
  setLocale('es')
  expect(nodeRisk('privileged').hint).toContain('este mismo dispositivo')
  setLocale('en')
})

test('a missing or unknown risk reads as the highest', () => {
  expect(nodeRisk(undefined)).toMatchObject({ icon: 'ShieldQuestion', marked: true })
  expect(nodeRisk('weird').hint).toContain('privileged')
})
