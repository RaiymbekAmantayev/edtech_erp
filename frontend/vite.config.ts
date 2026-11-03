import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// API проксируется на Go-сервер: refresh-cookie остаётся same-origin.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8080', '/health': 'http://localhost:8080' },
  },
})
