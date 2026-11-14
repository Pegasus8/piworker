import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        // Use VITE_API_URL env var for Docker, fallback to localhost for local dev
        target: process.env.VITE_API_URL || 'http://localhost:8080',
        // Keep the browser's Host (e.g. localhost:3000): the API then sees
        // the editor's requests as same-origin (its CSRF check compares
        // Origin with Host), so no cors_origins entry is needed.
        changeOrigin: false
      }
    }
  }
})
