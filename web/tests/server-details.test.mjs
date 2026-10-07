// "Server details" is where an operator reads what the server reports about itself. The
// wording is the part worth pinning: a figure that was never measured must not read as
// zero, a platform that reports no process usage must say so, and the withheld-media
// counter must not be mistaken for packet loss.
import assert from 'node:assert/strict';
import test from 'node:test';
import {
  aboutFacts,
  formatBytes,
  formatDuration,
  mediaFacts,
  processFacts,
  routeLine,
  refusedOrFailed,
  slowestRoutes,
  storageFacts
} from '../src/lib/server-details.ts';

function report(overrides = {}) {
  return {
    version: '1.0.0',
    uptime_seconds: 93784,
    about: {
      schema: 25,
      backup_format: 2,
      protocol: 1,
      protocol_min: 1,
      platform: 'linux/amd64',
      go: 'go1.27.1',
      tls_mode: 'https'
    },
    routes: [],
    process: {
      reported: true,
      resident_bytes: 73400320,
      cpu_seconds: 125,
      goroutines: 41,
      go_heap_bytes: 20971520
    },
    media: { voice_users: 0, forwarded_packets: 0, forwarded_bytes: 0, dropped_packets: 0 },
    connections: 3,
    storage: { measured: false, age_seconds: 0, database_bytes: 4096 },
    ...overrides
  };
}
const value = (facts, label) => facts.find((fact) => fact.label === label)?.value;
const note = (facts, label) => facts.find((fact) => fact.label === label)?.note;

test('sizes and durations are written for a person', () => {
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(1536), '1.5 KB');
  assert.equal(formatBytes(73400320), '70.0 MB');
  assert.equal(formatBytes(3221225472), '3.0 GB');
  assert.equal(formatBytes(-1), 'Not measured yet');
  assert.equal(formatDuration(42), '42s');
  assert.equal(formatDuration(3720), '1h 2m');
  assert.equal(formatDuration(93784), '1d 2h');
});

test('the build facts an operator is asked for are all there', () => {
  const facts = aboutFacts(report());
  assert.equal(value(facts, 'Version'), '1.0.0');
  assert.equal(value(facts, 'Running for'), '1d 2h');
  assert.equal(value(facts, 'Database schema'), '25');
  assert.equal(value(facts, 'Backup format'), '2');
  assert.equal(value(facts, 'Realtime protocol'), '1');
  assert.equal(
    value(facts, 'Address mode'),
    'HTTPS, with a supplied or automatically issued certificate'
  );
  assert.equal(value(facts, 'Platform'), 'linux/amd64, go1.27.1');
});

test('a protocol range is shown as a range, and an unknown mode as itself', () => {
  const facts = aboutFacts(
    report({
      about: { ...report().about, protocol: 3, protocol_min: 2, tls_mode: 'something-new' }
    })
  );
  assert.equal(value(facts, 'Realtime protocol'), '2–3');
  assert.equal(value(facts, 'Address mode'), 'something-new');
});

test('a server that reports no build block still shows its version', () => {
  const facts = aboutFacts(report({ about: undefined }));
  assert.deepEqual(
    facts.map((fact) => fact.label),
    ['Version', 'Running for']
  );
});

test('a platform that reports no process usage says so instead of showing zero', () => {
  const facts = processFacts(
    report({ process: { ...report().process, reported: false, resident_bytes: 0, cpu_seconds: 0 } })
  );
  assert.equal(value(facts, 'Memory in use'), 'Not reported on this platform');
  assert.equal(value(facts, 'Processor time'), 'Not reported on this platform');
  // The runtime's own heap figure is always available.
  assert.equal(value(facts, 'Go heap'), '20.0 MB');
  assert.equal(value(processFacts(report()), 'Memory in use'), '70.0 MB');
});

test('storage that has not been measured is not shown as empty', () => {
  const unmeasured = storageFacts(report());
  assert.equal(value(unmeasured, 'Uploads'), 'Not measured yet');
  assert.equal(value(unmeasured, 'Backups on this machine'), 'Not measured yet');
  assert.equal(value(unmeasured, 'Free disk'), 'Could not be read');
  assert.equal(value(unmeasured, 'Database'), '4.0 KB');

  const measured = storageFacts(
    report({
      storage: {
        measured: true,
        age_seconds: 240,
        database_bytes: 4096,
        uploads_bytes: 0,
        backups_bytes: 2097152,
        free_bytes: 10737418240
      }
    })
  );
  // A measured zero is a real answer and reads as one.
  assert.equal(value(measured, 'Uploads'), '0 B');
  assert.equal(note(measured, 'Uploads'), 'Measured 4m ago.');
  assert.equal(value(measured, 'Backups on this machine'), '2.0 MB');
  assert.equal(value(measured, 'Free disk'), '10.0 GB');
});

test('a storage walk that failed is reported, not hidden', () => {
  const facts = storageFacts(
    report({
      storage: {
        measured: true,
        age_seconds: 10,
        database_bytes: 1,
        uploads_bytes: 0,
        backups_bytes: 0,
        free_disk_error: 'statfs: permission denied',
        measurement_error: 'uploads: permission denied'
      }
    })
  );
  assert.equal(note(facts, 'Free disk'), 'statfs: permission denied');
  assert.equal(value(facts, 'Measurement problem'), 'uploads: permission denied');
});

test('withheld media is described as the rule being enforced, never as loss', () => {
  const quiet = mediaFacts(report());
  assert.equal(value(quiet, 'Media forwarded'), 'Nothing yet');
  assert.equal(value(quiet, 'Media withheld'), 'None');

  const busy = mediaFacts(
    report({
      media: {
        voice_users: 4,
        forwarded_packets: 120000,
        forwarded_bytes: 15728640,
        dropped_packets: 2500
      }
    })
  );
  assert.equal(value(busy, 'In voice now'), '4');
  assert.equal(value(busy, 'Media forwarded'), '15.0 MB in 120,000 packets');
  assert.equal(value(busy, 'Media withheld'), '2,500 packets');
  assert.match(note(busy, 'Media withheld'), /not packet loss/);
});

const route = (name, total, p95, max, errors = 0) => ({
  route: name,
  total,
  in_flight: 0,
  errors,
  sum_ms: 0,
  max_ms: max,
  p95_bucket_ms: p95
});

test('the slowest routes are ranked, and what was never asked is left out', () => {
  const routes = [
    route('GET /api/channels', 900, 25, 80),
    route('GET /api/search', 40, 250, 900),
    route('POST /api/uploads', 12, 250, 1400),
    route('GET /api/admin/audit-logs', 0, 0, 0),
    // The realtime socket is open for a whole visit; its duration is not a latency.
    route('GET /ws', 30, 10000, 3600000)
  ];
  assert.deepEqual(
    slowestRoutes(routes, 2).map((entry) => entry.route),
    ['POST /api/uploads', 'GET /api/search']
  );
  assert.equal(slowestRoutes(routes).length, 3);
  assert.equal(slowestRoutes([]).length, 0);
});

test('a route line says how slow, how often and how many were refused or failed', () => {
  assert.equal(
    routeLine(route('GET /api/search', 1, 250, 900)),
    '95% within 250 ms, slowest 900 ms — 1 request'
  );
  assert.equal(
    routeLine(route('POST /api/uploads', 1200, 1000, 4100, 2)),
    '95% within 1,000 ms, slowest 4,100 ms — 1,200 requests, 2 refused or failed'
  );
  assert.equal(refusedOrFailed([route('a', 1, 5, 5, 2), route('b', 1, 5, 5, 3)]), 5);
});
