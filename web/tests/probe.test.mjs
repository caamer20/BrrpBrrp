// The probe exists because the failures that matter live at the edges of a real
// deployment, and it is only useful if it tells those failures apart from a healthy
// server. So it is pointed at real servers here: one that is healthy, one whose proxy
// swallows the websocket upgrade, one whose asset is stale, and one whose websocket
// upgrades and then says nothing.
import assert from 'node:assert/strict';
import test from 'node:test';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { runProbe, handshake, formatLine } from '../../scripts/probe.mjs';

const SHELL = `<!doctype html><html><head><script type="module" src="/assets/app-abc.js"></script></head><body></body></html>`;

/** A server with every fault made selectable, so each check can be shown to fail for the
 * reason it names rather than for the state another check left behind. */
function startServer(faults = {}) {
  const server = createServer((request, response) => {
    const path = request.url.split('?')[0];
    const headers = {
      'Content-Security-Policy': "default-src 'self'",
      'X-Content-Type-Options': 'nosniff'
    };
    if (path === '/api/setup/status') {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ setup_required: false }));
      return;
    }
    if (path === '/') {
      response.writeHead(200, { ...headers, 'content-type': 'text/html' });
      response.end(faults.noAssetLink ? '<html><body>no bundle</body></html>' : SHELL);
      return;
    }
    if (path.startsWith('/assets/')) {
      if (faults.staleAsset) {
        response.writeHead(404, { 'content-type': 'text/plain' });
        response.end('not found');
        return;
      }
      response.writeHead(200, { 'content-type': 'text/javascript' });
      response.end('export const nothing = 1;');
      return;
    }
    if (path === '/api/me') {
      if (request.headers.cookie === 'brrp_session=good') {
        response.writeHead(200, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ user: { username: 'probe' } }));
        return;
      }
      response.writeHead(401, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ detail: 'Sign in required.' }));
      return;
    }
    if (path === '/api/admin/metrics') {
      if (request.headers.cookie !== 'brrp_session=good') {
        response.writeHead(401, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ detail: 'Sign in required.' }));
        return;
      }
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ uptime_seconds: 12, routes: [{ route: 'GET /api/me' }] }));
      return;
    }
    response.writeHead(404, { 'content-type': 'application/json' });
    response.end('{}');
  });
  server.on('upgrade', (request, socket) => {
    if (faults.swallowUpgrade) return; // A proxy that eats the upgrade: the socket stays open.
    if (faults.silentUpgrade || request.headers.cookie === 'brrp_session=good') {
      const accept = createHash('sha1')
        .update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
        .digest('base64');
      socket.write(
        'HTTP/1.1 101 Switching Protocols\r\n' +
          'Upgrade: websocket\r\n' +
          'Connection: Upgrade\r\n' +
          `Sec-WebSocket-Accept: ${accept}\r\n\r\n`
      );
      if (!faults.silentUpgrade) {
        const payload = Buffer.from(JSON.stringify({ t: 'hello', d: { protocol: 1 } }));
        socket.write(Buffer.concat([Buffer.from([0x81, payload.length]), payload]));
      }
      return;
    }
    socket.write('HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n');
    socket.end();
  });
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () =>
      resolve({ server, base: `http://127.0.0.1:${server.address().port}` })
    );
  });
}

test('a healthy deployment passes every check, and the websocket is spoken to', async () => {
  const { server, base } = await startServer();
  try {
    const anonymous = await runProbe({ base, timeout: 2000 });
    assert.ok(
      anonymous.every((result) => result.ok),
      anonymous.map(formatLine).join('\n')
    );
    const handshakeResult = anonymous.find((result) => result.name === 'websocket handshake');
    // Without a session the server is expected to refuse cleanly rather than upgrade.
    assert.match(handshakeResult.detail, /refused cleanly|upgraded/);

    const signedIn = await runProbe({ base, cookie: 'brrp_session=good', timeout: 2000 });
    assert.ok(
      signedIn.every((result) => result.ok),
      signedIn.map(formatLine).join('\n')
    );
    const names = signedIn.map((result) => result.name);
    assert.deepEqual(names, [
      'setup status',
      'frontend shell',
      'frontend asset',
      'security headers',
      'authenticated session',
      'operator metrics',
      'websocket handshake'
    ]);
    assert.match(
      signedIn.find((result) => result.name === 'websocket handshake').detail,
      /upgraded, first frame hello/
    );
    assert.match(signedIn.find((result) => result.name === 'operator metrics').detail, /1 routes/);
  } finally {
    server.close();
  }
});

test('each deployment fault is named rather than reported as a healthy server', async () => {
  const cases = [
    ['swallowUpgrade', 'websocket handshake', /no answer to the upgrade/],
    ['silentUpgrade', 'websocket handshake', /upgraded but silent/],
    ['staleAsset', 'frontend asset', /answered 404/],
    ['noAssetLink', 'frontend asset', /names no \/assets\/ bundle/]
  ];
  for (const [fault, name, expected] of cases) {
    const { server, base } = await startServer({ [fault]: true });
    try {
      const results = await runProbe({ base, cookie: 'brrp_session=good', timeout: 2000 });
      const failed = results.filter((result) => !result.ok);
      assert.ok(failed.length > 0, `${fault} was reported as healthy`);
      const named = failed.find((result) => result.name === name);
      assert.ok(
        named,
        `${fault} failed somewhere other than ${name}: ${failed.map(formatLine).join('; ')}`
      );
      assert.match(named.detail, expected);
    } finally {
      server.close();
    }
  }
});

test('a refused upgrade is a clean answer, not a failure', async () => {
  // The first deployment of this product will be behind a proxy, and a proxy that answers
  // 403 is doing its job. What is a failure is silence.
  const { server, base } = await startServer();
  try {
    const result = await handshake(`ws://${base.replace('http://', '')}/ws`, '', 2000);
    assert.equal(result.status, 401);
    assert.equal(result.timedOut, undefined);
  } finally {
    server.close();
  }
});

test('an unreachable deployment fails every check instead of throwing', async () => {
  // Port 1 on loopback: nothing listens there, and the probe's job is to say so rather
  // than to crash with a stack trace an operator has to interpret.
  const results = await runProbe({ base: 'http://127.0.0.1:1', timeout: 1000 });
  assert.equal(results.length, 5);
  assert.ok(results.every((result) => !result.ok));
});
