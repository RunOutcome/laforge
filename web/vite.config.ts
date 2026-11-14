// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// `npm run dev` proxies the API to a forge started with `la up` (LAFORGE_URL to change).
const forge = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.LAFORGE_URL ?? 'http://127.0.0.1:4646';

export default defineConfig({
  plugins: [react()],
  build: { outDir: '../forge/dist', emptyOutDir: true, chunkSizeWarningLimit: 1200 },
  server: { proxy: { '/api': { target: forge, ws: true, changeOrigin: false } } },
});
