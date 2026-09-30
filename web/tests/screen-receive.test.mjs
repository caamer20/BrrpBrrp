import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { VIEW_STREAM_ID, isViewOnlyTrack } from '../src/lib/screen-receive.ts';

// A client asking to *watch* a screen and a client asking to *share* one produce
// byte-identical offers: a receiving video transceiver emits a media section with no
// direction attribute, and the SDP default makes that `sendrecv`. The marker is the only
// thing that tells them apart, so both sides have to agree on its value and on where it
// is read from.

const read = (file) => readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');

test('both sides use the same marker value', () => {
  const client = read('src/lib/screen-receive.ts');
  const server = read('../internal/rtc/publications.go');
  const declared = VIEW_STREAM_ID;
  // The server recognises the same literal. Two spellings is how a marker stops working
  // without anything failing.
  assert.match(client, new RegExp(`VIEW_STREAM_ID = '${declared}'`));
  assert.match(server, new RegExp(`ViewStreamID = "${declared}"`));
});

test('the server reads the marker from a negotiated track, not from SDP text', () => {
  const server = read('../internal/rtc/sfu.go');
  // Reading the description is what failed twice: a bare receiving transceiver has no
  // direction attribute, and Pion reuses a same-kind transceiver rather than creating
  // one, so a bare recvonly offer matches the server's own sendonly screen output.
  assert.match(server, /transceiver\.Receiver\(\)/);
  assert.match(server, /IsViewOnlyStream\(receiver\.Track\(\)\.StreamID\(\)\)/);
});

test('a track arriving with no stream is not treated as a view', () => {
  // An absent stream is not evidence of anything, and saying it is would let a track
  // with no description be filed as a viewer's.
  assert.equal(isViewOnlyTrack(undefined), false);
  assert.equal(isViewOnlyTrack(null), false);
  assert.equal(isViewOnlyTrack({ getVideoTracks: () => [] }), false);
});

test('a view track is told from a microphone by its stream id', () => {
  const view = { getVideoTracks: () => [{ id: VIEW_STREAM_ID + '-abc' }] };
  const microphone = { getVideoTracks: () => [{ id: 'webrtc-video-1234' }] };
  assert.equal(isViewOnlyTrack(view), true);
  assert.equal(isViewOnlyTrack(microphone), false);
});

test('the workspace renders a shared screen once, muted and inline', () => {
  const workspace = read('src/lib/components/Workspace.svelte');
  // Muted because a video element that unmutes would play the sharer's system audio at
  // every viewer; inline because a phone should not be taken fullscreen by somebody else
  // starting a share.
  assert.match(workspace, /class="shared-screen"/);
  assert.match(workspace, /muted/);
  assert.match(workspace, /playsinline/);
  assert.match(workspace, /aria-label="A screen being shared/);
});
