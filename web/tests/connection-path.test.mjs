import test from 'node:test';
import assert from 'node:assert/strict';
import { describePath } from '../src/lib/connection-path.ts';

// A call that is relayed and a call that is direct have very different failure modes,
// and the round-trip time does not tell them apart. The selected candidate pair is the
// only signal, so describing it is what makes "it is relayed" answerable at all.

const path = (local, remote) => describePath(local, remote);

test('a relayed pair is named as relayed, whatever the other end negotiated', () => {
  // The weakest link decides. A pair relayed on one side is a relayed call even when
  // the other end reached the TURN server directly.
  assert.match(path({ candidateType: 'relay' }, { candidateType: 'host' }), /relayed/i);
  assert.match(path({ candidateType: 'host' }, { candidateType: 'relay' }), /relayed/i);
  assert.match(path({ candidateType: 'relay' }, { candidateType: 'srflx' }), /relayed/i);
});

test('a direct pair says so, and says how it found the address', () => {
  assert.match(path({ candidateType: 'host' }, { candidateType: 'host' }), /local network/i);
  assert.match(path({ candidateType: 'srflx' }, { candidateType: 'srflx' }), /STUN/i);
  assert.match(path({ candidateType: 'prflx' }, { candidateType: 'host' }), /peer-reflexive/i);
});

test('an unknown or missing candidate is not invented into a claim', () => {
  // "Direct" would be a fabricated answer to a question the browser has not answered.
  // An empty string leaves the readout out rather than asserting a path.
  assert.equal(path(undefined, undefined), '');
  assert.equal(path(undefined, { candidateType: 'host' }), '');
  assert.equal(path({ candidateType: 'host' }, undefined), '');
  assert.equal(path({}, {}), '');
});
