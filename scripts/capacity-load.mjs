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
import { execFileSync } from 'node:child_process';
import { createConnection } from 'node:net';
import { createHash, randomBytes } from 'node:crypto';

// A minimal text-only WebSocket client.
//
// The built-in `WebSocket` cannot set request headers, and this server requires the
// Origin to match its configured base URL exactly — so a driver built on it can never
// connect at all, and a capacity run that silently measured nothing would be the worst
// possible outcome. This is the frame handling a measurement needs and no more: a
// handshake, masked text frames out, unmasked text frames in, and a close.

class RawSocket {
  constructor({ host, port, path, cookie, origin }) {
    this.host = host;
    this.port = port;
    this.path = path;
    this.cookie = cookie;
    this.origin = origin;
    this.socket = null;
    this.buffer = Buffer.alloc(0);
    this.onMessage = () => {};
  }

  connect() {
    return new Promise((resolve, reject) => {
      const key = randomBytes(16).toString('base64');
      this.socket = createConnection({ host: this.host, port: this.port }, () => {
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
      });
      this.socket.on('error', reject);
      this.socket.on('data', (chunk) => {
        this.buffer = Buffer.concat([this.buffer, chunk]);
        if (!this.handshaken) {
          const end = this.buffer.indexOf('\r\n\r\n');
          if (end < 0) return;
          const head = this.buffer.subarray(0, end).toString('latin1');
          this.buffer = this.buffer.subarray(end + 4);
          if (!/ 101 /.test(head.split('\r\n')[0])) {
            reject(new Error(`websocket handshake failed: ${head.split('\r\n')[0]}`));
            return;
          }
          // The server's accept value proves the handshake, not just the status line.
          const expected = createHash('sha1')
            .update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
            .digest('base64');
          if (!head.includes(expected)) {
            reject(new Error('websocket accept value did not match'));
            return;
          }
          this.handshaken = true;
          resolve();
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
      if (opcode === 0x1) this.onMessage(payload.toString('utf8'));
    }
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

const args = process.argv.slice(2);
const option = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index < 0 ? fallback : args[index + 1];
};

const base = (option('--base', process.env.BRRP_BASE_URL) || '').replace(/\/$/, '');
const cookie = option('--cookie', process.env.BRRP_COOKIE || '');
const clientCount = Number(option('--clients', 5));
const durationSeconds = Number(option('--duration', 60));
const channelCount = Number(option('--channels', 4));
const intervalMs = Number(option('--interval-ms', 500));
const reportPath = option('--report', '');
const label = option('--label', 'ad-hoc');

if (!base || !cookie) {
  console.error('usage: --base URL --cookie NAME=VALUE [--clients N] [--duration SECONDS]');
  process.exit(2);
}

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
      port: Number(target.port) || 80,
      path: '/ws',
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

async function main() {
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

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
