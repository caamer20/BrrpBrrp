// The capacity client has to survive a real deployment's transport.
//
// Two things it did not do, both found by reading rather than by running. It opened a
// **plain TCP** socket regardless of the URL's scheme, and it read frames without ever
// answering a **ping**. The first means a run against a deployment behind HTTPS cannot
// connect at all; the second means the server's 30-second heartbeat times the client out
// and the run loses its participants part-way through.
//
// Both are the shape of failure that does not announce itself. A capacity run that
// connects to nothing still writes a report, and a report with no participants looks
// like a very fast server. So both are driven against a real socket here.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { createHash, randomBytes } from 'node:crypto';
import test from 'node:test';
import { RawSocket } from '../../scripts/capacity-load.mjs';

// A self-signed certificate for `localhost`, generated for these tests rather than
// taken from anywhere. It verifies nothing about this product; it is a TLS endpoint so
// the client's transport can be exercised. The client is pointed at it through
// `rejectUnauthorized: false`, which is what a test is allowed to do and a measurement
// run is not.
const fixture = (name) => readFileSync(new URL(`./fixtures/${name}`, import.meta.url), 'utf8');

const key = fixture('localhost-test-key.pem');
const cert = fixture('localhost-test-cert.pem');

/**
 * A WebSocket server that completes the handshake and then does whatever the test asks:
 * send a ping, send text, or close. Written by hand because the built-in client cannot
 * set the headers this server checks, and the point here is the *client*.
 */
/** Reads the HTTP request head a TLS-wrapped socket carries. */
function readUpgradeHead(socket, done) {
  let head = '';
  const onData = (chunk) => {
    head += chunk.toString('latin1');
    const end = head.indexOf('\r\n\r\n');
    if (end < 0) return;
    socket.off('data', onData);
    const lines = head.slice(0, end).split('\r\n');
    done({
      headers: Object.fromEntries(
        lines.slice(1).map((line) => {
          const at = line.indexOf(':');
          return [line.slice(0, at).toLowerCase(), line.slice(at + 1).trim()];
        })
      )
    });
  };
  socket.on('data', onData);
}

/** Answers a completed handshake with 101 and the accept value. */
function completeUpgrade(socket, request, onConnection) {
  const accept = createHash('sha1')
    .update(request.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
    .digest('base64');
  socket.write(
    'HTTP/1.1 101 Switching Protocols\r\n' +
      'Upgrade: websocket\r\n' +
      'Connection: Upgrade\r\n' +
      `Sec-WebSocket-Accept: ${accept}\r\n\r\n`
  );
  onConnection?.(socket);
}

async function startServer({ tls = false, onConnection } = {}) {
  const { createServer: createTlsServer } = await import('node:tls');
  const server = tls ? createTlsServer({ key, cert }) : createServer();
  // A TLS server never emits `upgrade` with a raw request: the socket is wrapped first,
  // so the handshake has to be accepted and the HTTP head read afterwards. Using
  // `upgrade` for both makes the TLS case hang rather than fail, which is a confusing
  // way to learn that the listener is on the wrong event.
  server.on('secureConnection', (socket) =>
    readUpgradeHead(socket, (request) => completeUpgrade(socket, request, onConnection))
  );
  server.on('upgrade', (request, socket) => completeUpgrade(socket, request, onConnection));
  const sockets = new Set();
  server.on('connection', (socket) => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
  });
  server.on('secureConnection', (socket) => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
  });
  await new Promise((done) => server.listen(0, '127.0.0.1', done));
  return {
    server,
    port: server.address().port,
    // Closing a listening server does not close the connections it accepted, and a test
    // that leaves one open keeps node's event loop alive — so the whole file appears to
    // hang after its last assertion has passed. Every socket is destroyed here.
    close: () => {
      for (const socket of sockets) socket.destroy();
      sockets.clear();
      server.close();
    }
  };
}

/** A masked server-to-client frame is never masked; this builds one. */
function serverFrame(opcode, payload) {
  const body = Buffer.from(payload);
  if (body.length < 126) return Buffer.concat([Buffer.from([0x80 | opcode, body.length]), body]);
  const header = Buffer.alloc(4);
  header[0] = 0x80 | opcode;
  header[1] = 126;
  header.writeUInt16BE(body.length, 2);
  return Buffer.concat([header, body]);
}

/** Reads one client frame's opcode and payload, unmasking it. */
function readClientFrame(socket, collected) {
  return new Promise((done) => {
    const onData = (chunk) => {
      collected.push(chunk);
      const buffer = Buffer.concat(collected);
      if (buffer.length < 2) return;
      const opcode = buffer[0] & 0x0f;
      const masked = (buffer[1] & 0x80) !== 0;
      let length = buffer[1] & 0x7f;
      let offset = 2;
      if (length === 126) {
        if (buffer.length < 4) return;
        length = buffer.readUInt16BE(2);
        offset = 4;
      }
      if (masked) offset += 4;
      if (buffer.length < offset + length) return;
      const payload = Buffer.from(buffer.subarray(offset, offset + length));
      if (masked) {
        const mask = buffer.subarray(offset - 4, offset);
        for (let index = 0; index < payload.length; index++) payload[index] ^= mask[index % 4];
      }
      socket.off('data', onData);
      collected.length = 0;
      done({ opcode, payload });
    };
    socket.on('data', onData);
  });
}

test('the module is importable without starting a measurement', () => {
  // Reaching here at all is the assertion: importing used to print usage and exit, which
  // made the socket untestable.
  assert.equal(typeof RawSocket, 'function');
});

test('a plaintext server still connects', async () => {
  // The permitted control. Fixing TLS must not break the development path, and a test
  // that only exercises TLS would not notice if it had.
  const { port, close } = await startServer();
  const socket = new RawSocket({
    host: '127.0.0.1',
    port,
    path: '/ws',
    cookie: 'brrp_session=x',
    origin: `http://127.0.0.1:${port}`
  });
  await socket.connect();
  assert.equal(socket.handshaken, true, 'the plaintext handshake did not complete');
  socket.socket.end();
  close();
});

test('a TLS server is connected to with TLS', async () => {
  // The defect: `createConnection` regardless of scheme, so a deployment behind HTTPS got
  // a plaintext handshake and every participant failed to connect.
  const { port, close } = await startServer({ tls: true });
  try {
    const socket = new RawSocket({
      host: '127.0.0.1',
      port,
      path: '/ws',
      cookie: 'brrp_session=x',
      origin: `https://127.0.0.1:${port}`,
      secure: true,
      // The certificate is self-signed for this test. A measurement run does not get this
      // exemption, which the next test pins.
      rejectUnauthorized: false
    });
    await socket.connect();
    assert.equal(socket.handshaken, true, 'the TLS handshake did not complete');
    assert.equal(socket.socket.encrypted, true, 'the socket was not encrypted');
    socket.socket.end();
  } finally {
    close();
  }
});

test('a plaintext client cannot speak to a TLS server', async () => {
  // The negative control: if a plaintext socket could complete this handshake, the
  // `secure` flag would be decorative and the previous test would prove nothing.
  const { port, close } = await startServer({ tls: true });
  try {
    const socket = new RawSocket({
      host: '127.0.0.1',
      port,
      path: '/ws',
      cookie: 'brrp_session=x',
      origin: `http://127.0.0.1:${port}`,
      secure: false
    });
    // A plaintext client to a TLS port gets no HTTP reply at all — the server waits for
    // a ClientHello that never arrives — so the client's own handshake timeout is what
    // ends the attempt. Without one the promise never settles, and a capacity run whose
    // participants cannot connect hangs instead of reporting that they could not.
    const outcome = await Promise.race([
      socket.connect().then(
        () => 'connected',
        () => 'refused'
      ),
      new Promise((done) => setTimeout(() => done('timeout'), 1500))
    ]);
    assert.notEqual(outcome, 'connected', 'a plaintext handshake succeeded against TLS');
  } finally {
    close();
  }
});

test('certificates are verified unless the caller opts out', () => {
  // A capacity number taken through an unverified connection is a number about somebody
  // else's server, so the default has to be verification.
  const verified = new RawSocket({
    host: 'localhost',
    port: 443,
    path: '/ws',
    cookie: 'x',
    origin: 'https://localhost',
    secure: true
  });
  assert.equal(verified.rejectUnauthorized, true);
  const optedOut = new RawSocket({
    host: 'localhost',
    port: 443,
    path: '/ws',
    cookie: 'x',
    origin: 'https://localhost',
    secure: true,
    rejectUnauthorized: false
  });
  assert.equal(optedOut.rejectUnauthorized, false);
});

test('a server ping is answered with a pong carrying the same payload', async () => {
  // The heartbeat failure. The server pings every 30 seconds and ends the connection when
  // a ping is not answered, so a client that reads frames without replying loses its
  // participants part-way through a long run.
  const payload = randomBytes(8).toString('hex');
  let serverSocket;
  const { port, close } = await startServer({
    onConnection: (socket) => {
      serverSocket = socket;
      socket.write(serverFrame(0x9, payload));
    }
  });
  const client = new RawSocket({
    host: '127.0.0.1',
    port,
    path: '/ws',
    cookie: 'brrp_session=x',
    origin: `http://127.0.0.1:${port}`
  });
  await client.connect();

  const frame = await readClientFrame(serverSocket, []);
  assert.equal(frame.opcode, 0xa, 'a ping was not answered with a pong');
  assert.equal(frame.payload.toString('utf8'), payload, 'the pong did not carry the ping payload');
  client.socket.end();
  close();
});

test('a text frame still reaches the message handler', async () => {
  // The permitted control for the ping change: answering pings must not have broken the
  // frames the measurement actually reads.
  let serverSocket;
  const { port, close } = await startServer({
    onConnection: (socket) => {
      serverSocket = socket;
      socket.write(serverFrame(0x1, JSON.stringify({ t: 'hello', d: {} })));
    }
  });
  const client = new RawSocket({
    host: '127.0.0.1',
    port,
    path: '/ws',
    cookie: 'brrp_session=x',
    origin: `http://127.0.0.1:${port}`
  });
  const received = new Promise((done) => {
    client.onMessage = (text) => done(JSON.parse(text));
  });
  await client.connect();
  const message = await received;
  assert.equal(message.t, 'hello');
  client.socket.end();
  close();
});
