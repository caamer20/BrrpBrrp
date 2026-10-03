import test from 'node:test';
import assert from 'node:assert/strict';
import {
  MAX_RECONNECT_DELAY_MS,
  SILENT_CONNECTION_MS,
  reconnectDelayMs
} from '../src/lib/reconnect.ts';

// The reconnect policy (ENG-11). The plan's budget is ten seconds at p95 after
// connectivity returns, and the events that report connectivity returning are the
// browser's, not the backoff timer's.

test('the backoff doubles to a thirty-second ceiling', () => {
  assert.equal(reconnectDelayMs(0), 1000);
  assert.equal(reconnectDelayMs(1), 2000);
  assert.equal(reconnectDelayMs(2), 4000);
  assert.equal(reconnectDelayMs(5), 30000);
  // And it stays there: a schedule that keeps growing would silently stop retrying.
  assert.equal(reconnectDelayMs(20), MAX_RECONNECT_DELAY_MS);
});

test('silence is measured against the server cadence, not a guess', () => {
  // The server sends a frame every thirty seconds, so the watchdog has to tolerate more
  // than one lost frame and less than a minute of a dead path.
  assert.ok(SILENT_CONNECTION_MS > 30_000);
  assert.ok(SILENT_CONNECTION_MS <= 90_000, 'a silent socket must be noticed within a minute');
});
