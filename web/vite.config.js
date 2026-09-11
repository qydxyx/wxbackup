import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/v1': { target: 'http://127.0.0.1:20365', changeOrigin: true },
      '/health': { target: 'http://127.0.0.1:20365', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
  },
})
