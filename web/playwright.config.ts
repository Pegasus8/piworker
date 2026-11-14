import { defineConfig } from '@playwright/test'
export default defineConfig({
 testDir: './e2e',
 workers: 1,
 timeout: 60000,
 use: { baseURL: 'http://127.0.0.1:18096' },
 webServer: {
  command: 'bun run dev -- --host 127.0.0.1 --port 18096 --strictPort',
  url: 'http://127.0.0.1:18096',
  env: { VITE_API_URL: 'http://127.0.0.1:18095' },
  reuseExistingServer: false
 }
})
