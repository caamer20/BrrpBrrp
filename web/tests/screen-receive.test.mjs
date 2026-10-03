import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  applyScreenAudioOnly,
  screenVideoTracks,
  VIEW_STREAM_ID,
  isViewOnlyTrack
} from '../src/lib/screen-receive.ts';

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

test('audio-only stops the picture being decoded, and leaves the sound alone', () => {
  // Audio-only is a data preference, not a display trick. Hiding the element would keep
  // the browser receiving and decoding a picture nobody asked to see, which is the cost
  // the choice exists to avoid; muting the audio would defeat it entirely, because the
  // desktop's sound is what the viewer chose to keep.
  const video = { enabled: true };
  const audio = { enabled: true };
  const stream = { getVideoTracks: () => [video], getAudioTracks: () => [audio] };
  applyScreenAudioOnly(stream, true);
  assert.equal(video.enabled, false);
  assert.equal(audio.enabled, true);
  applyScreenAudioOnly(stream, false);
  assert.equal(video.enabled, true);
  // A stream that has not arrived, or one with no video at all (a screen whose capture
  // stopped before the announcement did), is not an error.
  assert.equal(screenVideoTracks(null).length, 0);
  assert.deepEqual(screenVideoTracks(stream), [video]);
  applyScreenAudioOnly(null, true);
  applyScreenAudioOnly({ getVideoTracks: () => [] }, true);
});

test('the stage keeps the control that turns the picture back on', () => {
  // The one thing an audio-only view must not take away. The element stays mounted in
  // both states; only the track is disabled, so the control that reverses the choice is
  // always reachable and the person is never left with a picture they cannot restore.
  const stage = readFileSync(
    new URL('../src/lib/components/ScreenStage.svelte', import.meta.url),
    'utf8'
  );
  assert.match(stage, /class:audio-only=/);
  assert.match(stage, /class="shared-screen"/);
  assert.match(stage, /muted/);
  assert.match(stage, /playsinline/);
  assert.match(stage, /Show picture/);
});
