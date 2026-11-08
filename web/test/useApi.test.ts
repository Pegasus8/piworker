import { expect, mock, test } from 'bun:test'

const posts: { url: string; body: unknown }[] = []
const instance = {
  interceptors: { response: { use: () => 0 } },
  post: async (url: string, body: unknown) => { posts.push({ url, body }); return { data: { success: true, data: null } } }
}
mock.module('axios', () => ({ default: { create: () => instance } }))
// Other test files replace this module with fakes; load the real one.
const { useApi } = await import('../src/composables/useApi.ts?real')

test('a password change revokes agent tokens unless asked to keep them', async () => {
  const api = useApi()
  await api.changePassword('old-password', 'new-password')
  expect(posts.at(-1)).toEqual({ url: '/auth/password', body: { currentPassword: 'old-password', password: 'new-password', keepAgentTokens: false } })
  await api.changePassword('old-password', 'new-password', true)
  expect(posts.at(-1)).toEqual({ url: '/auth/password', body: { currentPassword: 'old-password', password: 'new-password', keepAgentTokens: true } })
})
