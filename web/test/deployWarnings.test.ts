import { describe, expect, test } from 'bun:test'
import { deployWarningText, deployWarningsSummary, issueLocation } from '@/lib/deployWarnings'
import { setLocale } from '@/i18n'

const unresolved = {
  path: '/nodes/mail/config/password', code: 'unresolved_secret', severity: 'warning' as const,
  message: 'config.password references secret "smtp_pass", which is not defined: the node gets an empty value, or fails, where it uses it',
  secret: 'smtp_pass',
}

describe('deploy warnings', () => {
  test('locates an issue path', () => {
    expect(issueLocation('/nodes/a~1b/config/x~0y')).toEqual({ nodeId: 'a/b', prop: 'x~y' })
    expect(issueLocation('/nodes/n1')).toEqual({ nodeId: 'n1', prop: '' })
    expect(issueLocation('/connections/c1')).toBeNull()
  })

  test('takes the secret name from its field, never from the message', () => {
    setLocale('en')
    const reworded = { ...unresolved, message: 'some other wording entirely' }
    expect(deployWarningText(reworded, id => id)).toContain('“smtp_pass”')
    const { secret: _omit, ...withoutField } = unresolved
    expect(deployWarningText(withoutField, id => id)).toBe('mail: ' + unresolved.message)
  })

  test('describes an unresolved secret in the interface language', () => {
    setLocale('es')
    const text = deployWarningText(unresolved, id => `Email (${id})`)
    expect(text).toContain('Email (mail)')
    expect(text).toContain('«smtp_pass»')
    expect(text).toContain('no está definido')
    setLocale('en')
    expect(deployWarningText(unresolved, id => id)).toContain('“smtp_pass”, which is not defined')
  })

  test('keeps the server message for other warnings', () => {
    setLocale('en')
    const other = { path: '/nodes/t', code: 'tool_risk', severity: 'warning' as const, message: 'something to know' }
    expect(deployWarningText(other, id => `#${id}`)).toBe('#t: something to know')
    expect(deployWarningsSummary([unresolved, other], id => id)).toMatch(/^Flow deployed with 2 warnings:/)
  })
})
