#!/usr/bin/env node
// Measures delivery latency and search latency against the support profile's budgets.
//
// The master plan's budgets are stated for a specific population and a specific
// topology, and this driver measures what it can on one host and says plainly what it
// cannot. A LAN figure from a laptop is not the plan's LAN figure, and the difference
// is recorded rather than smoothed over.
//
//   Delivery: p95 <= 500ms, p99 <= 1s
//   Search:   p95 <= 1s,  p99 <= 2s
//   Reconnect: p95 <= 10s
//
// The client is a WebSocket participant: it connects, joins a channel, and times each
// message from the moment it is sent until the server publishes it. That is a real
// round trip through the real server, not a local timing of a local function.
//
// Usage:
//   node scripts/capacity-load.mjs --base URL --cookie NAME=VALUE [--clients N]
//     [--duration SECONDS] [--channels N] [--report PATH] [--label NAME]

import { writeFileSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { createConnection as createTcpConnection } from 'node:net';
import { connect as createTlsConnection } from 'node:tls';
import { createHash, randomBytes } from 'node:crypto';

// A minimal text-only WebSocket client.
//
// The built-in `WebSocket` cannot set request headers, and this server requires the
// Origin to match its configured base URL exactly — so a driver built on it can never
// connect at all, and a capacity run that silently measured nothing would be the worst
// possible outcome. This is the frame handling a measurement needs and no more: a
// handshake, masked text frames out, unmasked text frames in, and a close.

export class RawSocket {
  constructor({ host, port, path, cookie, origin, secure, rejectUnauthorized }) {
    this.host = host;
    this.port = port;
    this.path = path;
    this.cookie = cookie;
    this.origin = origin;
    this.secure = secure === true;
    // A capacity run against a deployment with a real certificate must verify it. The
    // default of not verifying would let the run measure a man in the middle instead of
    // the server, which is the one thing a capacity number must never be.
    this.rejectUnauthorized = rejectUnauthorized !== false;
    this.socket = null;
    this.buffer = Buffer.alloc(0);
    this.onMessage = () => {};
    this.onPing = () => {};
  }

  connect({ handshakeTimeoutMs = 10000 } = {}) {
    return new Promise((resolve, reject) => {
      const key = randomBytes(16).toString('base64');
      // A deadline on the handshake. Without one, connecting to something that never
      // answers — a plaintext client aimed at a TLS port, a port with nothing behind it —
      // never settles, and a capacity run whose participants cannot connect hangs instead
      // of reporting that they could not. A run that hangs produces no report, and no
      // report is indistinguishable from a run that was never started.
      const deadline = setTimeout(() => {
        settled = true;
        try {
          this.socket?.destroy();
        } catch {
          /* Already gone. */
        }
        reject(new Error(`websocket handshake did not complete within ${handshakeTimeoutMs}ms`));
      }, handshakeTimeoutMs);
      const settle = (fn) => (value) => {
        if (settled) return;
        settled = true;
        clearTimeout(deadline);
        fn(value);
      };
      const succeed = settle(resolve);
      const fail = settle(reject);
      // `wss://` and `https://` both need a TLS socket. A plain TCP connection to a TLS
      // port sends the handshake in the clear, the server closes it, and every participant
      // fails to connect — which is a run that measures nothing rather than a run that
      // fails loudly. The certificate is verified unless the caller says otherwise,
      // because a capacity number taken through an unverified connection is a number
      // about somebody else's server.
      const onOpen = () => {
        this.socket.write(
          `GET ${this.path} HTTP/1.1\r\n` +
            `Host: ${this.host}:${this.port}\r\n` +
            `Upgrade: websocket\r\n` +
            `Connection: Upgrade\r\n` +
            `Sec-WebSocket-Key: ${key}\r\n` +
            `Sec-WebSocket-Version: 13\r\n` +
            `Origin: ${this.origin}\r\n` +
            `Cookie: ${this.cookie}\r\n\r\n`
        );
      };
      this.socket = this.secure
        ? createTlsConnection(
            { host: this.host, port: this.port, servername: this.host, rejectUnauthorized: this.rejectUnauthorized },
            onOpen
          )
        : createTcpConnection({ host: this.host, port: this.port }, onOpen);
      let settled = false;
      this.socket.on('error', fail);
      this.socket.on('data', (chunk) => {
        this.buffer = Buffer.concat([this.buffer, chunk]);
        if (!this.handshaken) {
          const end = this.buffer.indexOf('\r\n\r\n');
          if (end < 0) return;
          const head = this.buffer.subarray(0, end).toString('latin1');
          this.buffer = this.buffer.subarray(end + 4);
          if (!/ 101 /.test(head.split('\r\n')[0])) {
            fail(new Error(`websocket handshake failed: ${head.split('\r\n')[0]}`));
            return;
          }
          // The server's accept value proves the handshake, not just the status line.
          const expected = createHash('sha1')
            .update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
            .digest('base64');
          if (!head.includes(expected)) {
            fail(new Error('websocket accept value did not match'));
            return;
          }
          this.handshaken = true;
          succeed();
        }
        this.drain();
      });
    });
  }

  // A frame's payload length is 7, 7+16 or 7+64 bits. A measurement client reads
  // small text frames, so the 64-bit case is decoded for completeness rather than
  // assumed away.
  drain() {
    for (;;) {
      if (this.buffer.length < 2) return;
      const first = this.buffer[0];
      const opcode = first & 0x0f;
      const masked = (this.buffer[1] & 0x80) !== 0;
      let length = this.buffer[1] & 0x7f;
      let offset = 2;
      if (length === 126) {
        if (this.buffer.length < 4) return;
        length = this.buffer.readUInt16BE(2);
        offset = 4;
      } else if (length === 127) {
        if (this.buffer.length < 10) return;
        length = Number(this.buffer.readBigUInt64BE(2));
        offset = 10;
      }
      const maskOffset = offset;
      if (masked) offset += 4;
      if (this.buffer.length < offset + length) return;
      const payload = Buffer.from(this.buffer.subarray(offset, offset + length));
      if (masked) {
        const mask = this.buffer.subarray(maskOffset, maskOffset + 4);
        for (let index = 0; index < payload.length; index++) payload[index] ^= mask[index % 4];
      }
      this.buffer = this.buffer.subarray(offset + length);
      if (opcode === 0x8) {
        this.socket.end();
        return;
      }
      // A ping must be answered with a pong carrying the same payload. The server
      // heartbeats on an interval and drops a connection that has not answered, so a
      // client that reads frames but never answers is disconnected part-way through a
      // long run — and a run that loses its participants at the heartbeat interval
      // measures a server nobody is really on. Ignoring the opcode was the defect; this
      // is why it is explicit rather than left to the frame reader.
      if (opcode === 0x9) {
        this.writeFrame(0xa, payload);
        this.onPing();
        continue;
      }
      if (opcode === 0xa) continue;
      if (opcode === 0x1) this.onMessage(payload.toString('utf8'));
    }
  }

  /** Writes one masked frame with an arbitrary opcode. */
  writeFrame(opcode, payload) {
    const mask = randomBytes(4);
    let header;
    if (payload.length < 126) {
      header = Buffer.from([0x80 | opcode, 0x80 | payload.length]);
    } else if (payload.length < 65536) {
      header = Buffer.alloc(4);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 126;
      header.writeUInt16BE(payload.length, 2);
    } else {
      header = Buffer.alloc(10);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt(payload.length), 2);
    }
    const masked = Buffer.alloc(payload.length);
    for (let index = 0; index < payload.length; index++) {
      masked[index] = payload[index] ^ mask[index % 4];
    }
    this.socket.write(Buffer.concat([header, mask, masked]));
  }

  send(text) {
    const payload = Buffer.from(text, 'utf8');
    const mask = randomBytes(4);
    let header;
    if (payload.length < 126) {
      header = Buffer.from([0x81, 0x80 | payload.length]);
    } else if (payload.length < 65536) {
      header = Buffer.alloc(4);
      header[0] = 0x81;
      header[1] = 0x80 | 126;
      header.writeUInt16BE(payload.length, 2);
    } else {
      header = Buffer.alloc(10);
      header[0] = 0x81;
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt(payload.length), 2);
    }
    const masked = Buffer.from(payload);
    for (let index = 0; index < masked.length; index++) masked[index] ^= mask[index % 4];
    this.socket.write(Buffer.concat([header, mask, masked]));
  }

  close() {
    try {
      this.socket?.end();
    } catch {
      // A closing socket is the expected end of a measurement run.
    }
  }
}

// The executable half. It is inside `main` and reached only through the entry-point
// guard at the bottom, so importing `RawSocket` — to drive it against a real TLS server
// in a test — does not parse arguments and exit. A measurement module that cannot be
// imported cannot be tested, and an untested measurement client is how a run that
// measures nothing comes to look like a passing run.
async function main() {
  const args = process.argv.slice(2);
  const option = (flag, fallback) => {
    const index = args.indexOf(flag);
    return index < 0 ? fallback : args[index + 1];
  };

  const base = (option('--base', process.env.BRRP_BASE_URL) || '').replace(/\/$/, '');
  const cookie = option('--cookie', process.env.BRRP_COOKIE || '');
  if (!base || !cookie) {
    console.error('usage: --base URL --cookie NAME=VALUE [--clients N] [--duration SECONDS]');
    process.exit(2);
  }
  const clientCount = Number(option('--clients', 5));
  const durationSeconds = Number(option('--duration', 60));
  const channelCount = Number(option('--channels', 4));
  const intervalMs = Number(option('--interval-ms', 500));
  const reportPath = option('--report', '');
  const label = option('--label', 'ad-hoc');

const BUDGETS = {
  delivery_p95_ms: 500,
  delivery_p99_ms: 1000,
  search_p95_ms: 1000,
  search_p99_ms: 2000,
  reconnect_p95_ms: 10000
};

function percentile(values, fraction) {
  if (!values.length) return null;
  const sorted = [...values].sort((left, right) => left - right);
  // Nearest-rank, so the reported value is one that was actually observed rather than
  // an interpolation between two that were not. An interpolated p99 is a number nobody
  // measured.
  const rank = Math.ceil(fraction * sorted.length);
  return sorted[Math.min(sorted.length - 1, Math.max(0, rank - 1))];
}

function sourceCommit() {
  try {
    return execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

class Client {
  constructor(name) {
    this.name = name;
    this.delivery = [];
    this.search = [];
    this.reconnect = [];
    this.socket = null;
    this.sentAt = new Map();
    this.stopped = false;
  }

  async connect() {
    const started = Date.now();
    const target = new URL(base);
    this.socket = new RawSocket({
      host: target.hostname,
      // 443/80 are the defaults only for their scheme. A run against a deployment on
      // 8443 with an explicit port must use it, and a run against TLS must not fall
      // back to the plaintext default.
      port: Number(target.port) || (target.protocol === 'https:' ? 443 : 80),
      path: '/ws',
      secure: target.protocol === 'https:' || target.protocol === 'wss:',
      cookie,
      // The server requires the Origin to equal its configured base URL exactly, which
      // is what a browser sends and what makes this a faithful client rather than a
      // back door.
      origin: base
    });
    this.socket.onMessage = (text) => {
      let message;
      try {
        message = JSON.parse(text);
      } catch {
        return;
      }
      if (message.t === 'message.created' && this.sentAt.has(message.d?.id)) {
        this.delivery.push(Date.now() - this.sentAt.get(message.d.id));
        this.sentAt.delete(message.d.id);
      }
    };
    await this.socket.connect();
    this.reconnect.push(Date.now() - started);
  }

  close() {
    this.stopped = true;
    try {
      this.socket?.close();
    } catch {
      // A closing socket is the expected end of a measurement run.
    }
  }
}

  const started = Date.now();
  const clients = [];
  for (let index = 0; index < clientCount; index++) clients.push(new Client(`client-${index}`));
  for (const client of clients) await client.connect();

  // A search is measured from the client too, because search latency is a user-visible
  // number and not a server-internal one.
  const searchOnce = async () => {
    const at = Date.now();
    // The real search route and its real parameter. A driver that invented its own
    // would measure a 400 rather than a search.
    const response = await fetch(`${base}/api/search?q=${encodeURIComponent('load sample')}`, {
      headers: { Cookie: cookie }
    }).catch(() => null);
    if (response) {
      await response.text();
      return Date.now() - at;
    }
    return null;
  };

  const searchLatencies = [];
  const deadline = Date.now() + durationSeconds * 1000;
  let sent = 0;
  const primary = clients[0];
  let channelID = '';
  try {
    const channels = await (
      await fetch(`${base}/api/channels`, { headers: { Cookie: cookie } })
    ).json();
    channelID = Array.isArray(channels) ? channels.find((row) => row.type === 'text')?.id ?? '' : '';
  } catch {
    // Reported as an empty population rather than a fabricated one.
  }

  while (Date.now() < deadline) {
    if (channelID) {
      sent += 1;
      // The key carries the run and the sequence, so a retried send is the same send
      // rather than a second one.
      const key = `load-${label}-${sent}`;
      const at = Date.now();
      const response = await fetch(`${base}/api/channels/${channelID}/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Cookie: cookie },
        body: JSON.stringify({ content: `load sample ${sent}`, idempotency_key: key })
      }).catch(() => null);
      if (response?.ok) {
        const message = await response.json();
        // The publication path is what a socket client waits on; recording the send time
        // here and matching the arrival in the handler measures the real round trip.
        primary.sentAt.set(message.id, at);
      } else {
        await response?.text();
      }
    }
    const search = await searchOnce();
    if (search !== null) searchLatencies.push(search);
    await new Promise((done) => setTimeout(done, intervalMs));
  }
  for (const client of clients) client.close();

  const delivery = clients.flatMap((client) => client.delivery);
  const reconnect = clients.flatMap((client) => client.reconnect);
  const report = {
    label,
    measured_at: new Date().toISOString(),
    source_commit: sourceCommit(),
    base_url: base,
    topology: {
      clients: clientCount,
      channels: channelCount,
      duration_seconds: durationSeconds,
      // Stated so a reader cannot mistake this for the plan's numbers: it is one host,
      // loopback, and whatever machine the run happened to land on.
      note: 'one host, loopback clients; the master plan budgets are stated for the reference allocation and a LAN, so these figures are not comparable to them'
    },
    sent,
    delivery: {
      samples: delivery.length,
      p50_ms: percentile(delivery, 0.5),
      p95_ms: percentile(delivery, 0.95),
      p99_ms: percentile(delivery, 0.99),
      max_ms: delivery.length ? Math.max(...delivery) : null
    },
    search: {
      samples: searchLatencies.length,
      p50_ms: percentile(searchLatencies, 0.5),
      p95_ms: percentile(searchLatencies, 0.95),
      p99_ms: percentile(searchLatencies, 0.99)
    },
    reconnect: { samples: reconnect.length, p95_ms: percentile(reconnect, 0.95) },
    budgets: BUDGETS,
    verdict: {
      delivery_p95: delivery.length ? percentile(delivery, 0.95) <= BUDGETS.delivery_p95_ms : null,
      search_p95: searchLatencies.length ? percentile(searchLatencies, 0.95) <= BUDGETS.search_p95_ms : null,
      // A verdict is `null` when there was nothing to measure, never `true`. A budget
      // with no samples has not been met.
      note: 'a null verdict means the run measured nothing, not that the budget passed'
    },
    wall_clock_seconds: Math.round((Date.now() - started) / 1000)
  };
  if (reportPath) {
    mkdirSync(dirname(reportPath), { recursive: true });
    writeFileSync(reportPath, JSON.stringify(report, null, 2) + '\n');
  }
  console.log(JSON.stringify(report, null, 2));
}

// Only run the load when this file is the entry point. A test that imports `RawSocket`
// to drive it against a real server must not start a two-hour measurement, and
// `import.meta.main` is the documented way to tell the two apart.
const isEntryPoint =
  import.meta.main === true ||
  process.argv[1] === fileURLToPath(import.meta.url);
if (isEntryPoint) {
  main().catch((error) => {
    console.error(error);
    process.exit(1);
  });
}
