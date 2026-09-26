import { spawn, spawnSync } from 'node:child_process';
import { mkdtemp, copyFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { once } from 'node:events';
import assert from 'node:assert/strict';

const binary = resolve(process.argv[2] || 'dist/brrpbrrp');
const directory = await mkdtemp(join(tmpdir(), 'brrp-package-'));
const original = join(directory, 'original');
const restored = join(directory, 'restored');
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('BRRP_')));
Object.assign(env, { BRRP_ADDR: '127.0.0.1:0', BRRP_BASE_URL: 'http://127.0.0.1:0', BRRP_TLS_MODE: 'http-local', BRRP_ALLOW_INSECURE_HTTP: 'false' });
function command(args, input, expected = 0) {
  const result = spawnSync(binary, args, { env, input, encoding: 'utf8', timeout: 15000 });
  assert.equal(result.status, expected, result.stderr || result.error?.message);
  return result.stdout + result.stderr;
}
command(['version']);
command(['create-admin', '--data-dir', original, '--username', 'release_check', '--password-stdin'], 'a disposable release passphrase\n');
command(['backup', '--data-dir', original, '--out', 'backups/check.zip']);
// Prepare an empty destination without creating its database.
command(['generate-cert', '--data-dir', restored]);
await copyFile(join(original, 'backups/check.zip'), join(restored, 'restore.zip'));
command(['restore', '--data-dir', restored, '--file', 'restore.zip']);
command(['restore', '--data-dir', restored, '--file', 'restore.zip'], undefined, 1);
const database = await readFile(join(restored, 'brrp.db'));
assert.equal(database.subarray(0, 15).toString(), 'SQLite format 3');
const server = spawn(binary, ['serve', '--data-dir', restored], { env, stdio: ['ignore', 'pipe', 'pipe'] });
const exited = once(server, 'exit');
try {
  const address = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Server did not start')), 15000);
    let output = '';
    server.stderr.on('data', chunk => {
      output += chunk.toString();
      const lines = output.split('\n'); output = lines.pop();
      for (const line of lines) {
        let entry; try { entry = JSON.parse(line); } catch { continue; }
        if (entry.msg === 'BrrpBrrp started') { clearTimeout(timer); resolve(entry.address); }
      }
    });
    server.once('error', error => { clearTimeout(timer); reject(error); });
    server.once('exit', code => { clearTimeout(timer); reject(new Error(`Server exited during startup: ${code}`)); });
  });
  const response = await fetch(`http://${address}/api/setup/status`);
  assert.equal(response.status, 200);
  assert.equal((await response.json()).setup_required, false);
  env.BRRP_ADDR = address;
  env.BRRP_BASE_URL = `http://${address}`;
  command(['healthcheck', '--data-dir', restored]);
  const readiness = command(['production-check', '--data-dir', restored], undefined, 1);
  assert.match(readiness, /configured browser origin is not HTTPS/);
  const page = await fetch(`http://${address}/`);
  assert.match(await page.text(), /assets\/index-/);
  const manifest = await fetch(`http://${address}/manifest.webmanifest`);
  assert.equal((await manifest.json()).icons.length, 2);
  const locked = command(['backup', '--data-dir', restored], undefined, 1);
  assert.match(locked, /data folder is in use/);
} finally {
  server.kill('SIGTERM');
  const [code] = await exited;
  assert.equal(code, 0, 'Server did not shut down cleanly');
}
command(['healthcheck', '--data-dir', restored], undefined, 1);
console.log('Package checks passed: CLI setup, backup, restore, lock, health, production guard, embedded UI, manifest and graceful stop.');
console.log(`Disposable test files: ${directory}`);
