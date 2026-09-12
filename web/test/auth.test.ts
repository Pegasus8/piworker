import { beforeEach, expect, mock, test } from 'bun:test'
import { createPinia, setActivePinia } from 'pinia'

let session: { username: string } | null
let setupRequired = false
let logoutFails = false
const saved = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', { value: {
 getItem: (k: string) => saved.get(k) ?? null,
 setItem: (k: string, v: string) => saved.set(k, v),
 removeItem: (k: string) => saved.delete(k)
}, configurable: true })
mock.module('../src/composables/useApi', () => ({ useApi: () => ({
 getAuthStatus: async () => ({ enabled: true, setupRequired }),
 getSession: async () => { if (!session) throw { response: { status: 401 } }; return session },
 login: async () => { session = { username: 'owner' }; return session },
 logout: async () => { if (logoutFails) throw new Error('offline'); session = null }
}) }))
const { useAuthStore } = await import('../src/stores/auth')
beforeEach(() => { setActivePinia(createPinia()); session = null; setupRequired = false; logoutFails = false; saved.clear() })

test('legacy browser tokens are removed and cannot authenticate a user', async () => {
 saved.set('piworker_auth_token', 'old-jwt'); saved.set('piworker_auth_user', '{"username":"old"}')
 const store = useAuthStore(); await store.fetchAuthStatus()
 expect(store.isAuthenticated).toBe(false)
 expect(saved.size).toBe(0)
})
test('restores authentication from the server session without storing a browser token', async () => {
 session = { username: 'owner' }; const store = useAuthStore(); await store.fetchAuthStatus()
 expect(store.isAuthenticated).toBe(true); expect(store.user?.username).toBe('owner'); expect(saved.size).toBe(0)
})
test('first boot reports setup and login establishes a session', async () => {
 setupRequired = true; const store = useAuthStore(); await store.fetchAuthStatus()
 expect(store.setupRequired).toBe(true); expect(store.isAuthenticated).toBe(false)
 await store.signIn('owner', 'password')
 expect(store.isAuthenticated).toBe(true); expect(store.setupRequired).toBe(false); expect(saved.size).toBe(0)
})
test('logout awaits server revocation and keeps the session visible if revocation fails', async () => {
 session = { username: 'owner' }; const store = useAuthStore(); await store.fetchAuthStatus()
 logoutFails = true; await expect(store.logout()).rejects.toThrow('offline'); expect(store.isAuthenticated).toBe(true)
 logoutFails = false; await store.logout(); expect(store.isAuthenticated).toBe(false)
})
