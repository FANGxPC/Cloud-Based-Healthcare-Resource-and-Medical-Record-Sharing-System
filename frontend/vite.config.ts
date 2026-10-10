import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

// In development, requests to /api/* are proxied to the backend (sam local start-api by default),
// so the browser sees one origin and there is no CORS to configure locally.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', ''); // '.' = the frontend folder (where npm runs)
  return {
    plugins: [react()],
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: env.VITE_API_URL || 'http://127.0.0.1:3000',
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/api/, ''),
        },
      },
    },
  };
});
