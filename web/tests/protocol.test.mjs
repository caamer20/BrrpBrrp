import test from 'node:test';
import assert from 'node:assert/strict';
import { PROTOCOL, protocolVerdict } from '../src/lib/protocol.ts';

// The protocol handshake (ENG-12). A tab left open across an update speaks an older
// conversation than the server; the frame that opens every connection says so, and the
// client's answer decides whether it keeps sending or asks to be updated first. The text
// is never at risk either way: the composer refuses to hand it over unchanged, and the
// update path keeps drafts.

test('a server inside the accepted range is compatible', () => {
  assert.deepEqual(protocolVerdict({ protocol: PROTOCOL, protocol_min: PROTOCOL }), {
    ok: true,
    message: ''
  });
  assert.equal(protocolVerdict({ protocol: PROTOCOL + 2, protocol_min: PROTOCOL }).ok, true);
});

test('a client older than the server minimum is told to update, by name', () => {
  const verdict = protocolVerdict({ protocol: PROTOCOL + 1, protocol_min: PROTOCOL + 1 });
  assert.equal(verdict.ok, false);
  assert.match(verdict.message, /older than the server/i);
  assert.match(verdict.message, /drafts stay here/i);
});

test('a client newer than the server is told which side is stale', () => {
  const verdict = protocolVerdict({ protocol: PROTOCOL - 1, protocol_min: PROTOCOL - 1 });
  assert.equal(verdict.ok, false);
  assert.match(verdict.message, /server is older/i);
});

test('a server that predates the handshake is not treated as incompatible', () => {
  // The fields arrive with the first protocol-aware release, so a server without them
  // cannot be ahead of this bundle; refusing it would turn a missing field into a broken
  // installation.
  assert.equal(protocolVerdict({ user_id: 'u1' }).ok, true);
  assert.equal(protocolVerdict({}).ok, true);
  assert.equal(protocolVerdict(undefined).ok, true);
  assert.equal(protocolVerdict(null).ok, true);
  // A field of the wrong type is not a number either, and guessing about it would be
  // worse than tolerating it.
  assert.equal(protocolVerdict({ protocol: 'one', protocol_min: true }).ok, true);
});
