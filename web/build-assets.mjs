import { createHash } from 'node:crypto';
import { readFile, writeFile, readdir } from 'node:fs/promises';
import { join } from 'node:path';
import { gzipSync, brotliCompressSync, constants } from 'node:zlib';

// Build the complete startup graph once, for compression and atomic offline installs.
export function productionAssets() {
  let outDir;
  let entryFiles = [];
  return {
    name: 'brrp-production-assets',
    apply: 'build',
    enforce: 'post',
    configResolved(config) {
      outDir = join(config.root, config.build.outDir);
    },
    generateBundle(_options, bundle) {
      const seen = new Set();
      function visit(name) {
        if (seen.has(name)) return;
        seen.add(name);
        const chunk = bundle[name];
        if (chunk?.type === 'chunk') {
          for (const dependency of chunk.imports) visit(dependency);
          for (const css of chunk.viteMetadata?.importedCss || []) seen.add(css);
        }
      }
      for (const chunk of Object.values(bundle))
        if (chunk.type === 'chunk' && chunk.isEntry) visit(chunk.fileName);
      entryFiles = [...seen];
    },
    async closeBundle() {
      const files = await readdir(join(outDir, 'assets'));
      const hash = createHash('sha256');
      for (const name of files.sort())
        hash.update(name).update(await readFile(join(outDir, 'assets', name)));
      const template = await readFile(join(outDir, 'service-worker.js'), 'utf8');
      const originalIndex = await readFile(join(outDir, 'index.html'), 'utf8');
      hash.update(template).update(originalIndex);
      const build = hash.digest('hex').slice(0, 16);
      const index = originalIndex.replace(
        '</head>',
        `<meta name="brrp-build" content="${build}"></head>`
      );
      await writeFile(join(outDir, 'index.html'), index);
      for (const match of index.matchAll(/(?:src|href)="(\/assets\/[^"?#]+)"/g))
        entryFiles.push(match[1].slice(1));
      const precache = [
        '/',
        ...new Set(entryFiles.map((name) => '/' + name)),
        '/manifest.webmanifest',
        '/icons/icon-192.png',
        '/icons/icon-512.png'
      ];
      await writeFile(join(outDir, 'app-version.json'), JSON.stringify({ build }));
      await writeFile(
        join(outDir, 'service-worker.js'),
        template
          .replace('__BRRP_BUILD__', build)
          .replace('/*__BRRP_PRECACHE__*/ []', JSON.stringify(precache))
      );
      for (const name of [
        ...files.map((name) => 'assets/' + name),
        'index.html',
        'service-worker.js',
        'app-version.json'
      ]) {
        if (!/\.(?:js|css|html|json|svg)$/.test(name)) continue;
        const data = await readFile(join(outDir, name));
        await writeFile(join(outDir, name + '.gz'), gzipSync(data, { level: 9 }));
        await writeFile(
          join(outDir, name + '.br'),
          brotliCompressSync(data, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } })
        );
      }
      console.log(`Offline startup: ${precache.length} assets, build ${build}`);
    }
  };
}
