import { spawnSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import https from 'node:https';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

// Operates only on the disposable containers/volume named by this run.
const image = process.argv[2] || 'brrpbrrp:local';
const name = `brrpbrrp-check-${randomUUID()}`;
const volume = `${name}-data`;
const directory = await mkdtemp(join(tmpdir(), 'brrpbrrp-docker-'));
const base = 'https://localhost:8082';
let createdVolume = false, createdContainer = false, port, ca;
function docker(args, { allowFailure = false, timeout = 30000 } = {}) {
  const result = spawnSync('docker', args, { encoding: 'utf8', timeout, maxBuffer: 1024 * 1024 });
  if (!allowFailure && result.status !== 0) throw new Error(result.error?.message || result.stderr || result.stdout);
  return result.stdout?.trim() || '';
}
const mounts = ['--mount', `type=volume,src=${volume},dst=/data`, '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--tmpfs', '/tmp:rw,noexec,nosuid,size=64m,mode=1777'];
async function start() {
  docker(['create', '--name', name, ...mounts, '--stop-timeout', '20', '-p', '127.0.0.1::8082', image]);
  createdContainer = true;
  docker(['start', name]);
  const deadline = Date.now() + 60000;
  for (;;) {
    const state = JSON.parse(docker(['inspect', '--format', '{{json .State}}', name]));
    if (state.Health?.Status === 'healthy') break;
    if (!state.Running || Date.now() > deadline) throw new Error('Container did not become healthy: ' + docker(['logs', name]));
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  const bindings = JSON.parse(docker(['inspect', '--format', '{{json .NetworkSettings.Ports}}', name]));
  port = Number(bindings['8082/tcp'][0].HostPort);
  docker(['cp', `${name}:/data/certs/root.crt`, join(directory, 'root.crt')]);
  ca = await readFile(join(directory, 'root.crt'));
}
function request(path, { method = 'GET', body, cookie = '', headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const data = body === undefined ? undefined : Buffer.isBuffer(body) ? body : Buffer.from(JSON.stringify(body));
    const req = https.request({ host: '127.0.0.1', port, servername: 'localhost', ca, path, method, timeout: 10000,
      headers: { Host: 'localhost:8082', Origin: base, ...(data ? { 'Content-Type': 'application/json', 'Content-Length': data.length } : {}), ...(cookie ? { Cookie: cookie } : {}), ...headers }
    }, response => {
      const chunks = [];
      response.on('data', chunk => chunks.push(chunk));
      response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, data: Buffer.concat(chunks) }));
      response.on('error', reject);
    });
    req.on('error', reject);
    req.on('timeout', () => req.destroy(new Error('Container HTTP request timed out')));
    req.end(data);
  });
}
async function json(path, options, status = 200) {
  const response = await request(path, options);
  assert.equal(response.status, status, `HTTP ${response.status}: ${response.data}`);
  return { value: JSON.parse(response.data), cookie: response.headers['set-cookie']?.[0]?.split(';')[0] };
}
try {
  docker(['info']);
  assert.equal(docker(['image', 'inspect', '--format', '{{.Config.User}}', image]), '10001:10001');
  docker(['volume', 'create', volume]); createdVolume = true;
  await start();
  const firstRoot = Buffer.from(ca);
  const token = docker(['exec', name, '/usr/local/bin/brrpbrrp', 'setup-token']);
  const password = `Disposable container check ${randomUUID()}`;
  const account = await json('/api/setup', { method: 'POST', body: { token, username: 'container_check', password, server_name: 'Container check' } }, 201);
  assert.ok(account.cookie);
  const cookie = account.cookie;
  const state = (await json('/api/state', { cookie })).value;
  const channel = state.channels.find(value => value.type === 'text').id;
  const boundary = `brrp-${randomUUID()}`;
  const content = 'Persistent attachment in a disposable container.';
  const multipart = Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="check.txt"\r\nContent-Type: text/plain\r\n\r\n${content}\r\n--${boundary}--\r\n`);
  const attachment = (await json('/api/uploads', { method: 'POST', cookie, body: multipart, headers: { 'Content-Type': `multipart/form-data; boundary=${boundary}` } }, 201)).value;
  const message = (await json(`/api/channels/${channel}/messages`, { method: 'POST', cookie, body: { content: 'This survives container recreation.', idempotency_key: randomUUID(), attachment_ids: [attachment.id] } }, 201)).value;
  docker(['exec', name, '/usr/local/bin/brrpbrrp', 'healthcheck']);
  assert.equal((await json('/api/setup/status')).value.setup_required, false);
  const page = await request('/'); assert.equal(page.status, 200);
  const script = page.data.toString().match(/src="(\/assets\/index-[^"]+\.js)"/)?.[1]; assert.ok(script);
  const asset = await request(script, { headers: { 'Accept-Encoding': 'gzip' } });
  assert.equal(asset.headers['content-encoding'], 'gzip'); assert.ok(asset.data.length > 0);
  docker(['stop', name]);
  assert.equal(docker(['inspect', '--format', '{{.State.ExitCode}}', name]), '0');
  docker(['rm', name]); createdContainer = false;
  await start();
  assert.deepEqual(ca, firstRoot, 'Container recreation changed the local trust root');
  assert.equal((await json('/api/setup/status')).value.setup_required, false);
  const login = await json('/api/login', { method: 'POST', body: { username: 'container_check', password } });
  const kept = (await json(`/api/messages/${message.id}`, { cookie: login.cookie })).value;
  assert.equal(kept.content, 'This survives container recreation.');
  assert.equal((await request(`/api/uploads/${attachment.id}`, { cookie: login.cookie })).data.toString(), content);
  docker(['stop', name]);
  docker(['run', '--rm', ...mounts, image, 'backup', '--out', 'backups/container-check.zip']);
  docker(['cp', `${name}:/data/backups/container-check.zip`, join(directory, 'backup.zip')]);
  assert.equal((await readFile(join(directory, 'backup.zip'))).subarray(0, 2).toString(), 'PK');
  console.log('Docker checks passed: non-root startup, trusted HTTPS, health, account/message/upload persistence, compressed UI, offline backup and graceful stop.');
} finally {
  if (createdContainer) docker(['rm', '-f', name], { allowFailure: true });
  if (createdVolume) docker(['volume', 'rm', volume], { allowFailure: true });
  await rm(directory, { recursive: true, force: true });
}
