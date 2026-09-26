import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { productionAssets } from './build-assets.mjs';
export default defineConfig({
  plugins: [svelte(), productionAssets()],
  server: {
    proxy: { '/api': 'http://127.0.0.1:8080', '/ws': { target: 'ws://127.0.0.1:8080', ws: true } }
  }
});
