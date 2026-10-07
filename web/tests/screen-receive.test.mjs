import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  applyScreenAudioOnly,
  screenVideoTracks,
  VIEW_STREAM_ID,
  isViewOnlyTrack,
  screenAudioOwner,
  SCREEN_AUDIO_STREAM_PREFIX
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

// A screen's own sound arrives as an audio track on a stream the server names for the
// account that is sharing. The listener's volume for that person has to reach it, and it
// did not: the id was parsed as a voice-slot number, came out NaN, and matched nobody
// (ledger §11w, W-31). Both ends have to agree on the name, so the server's is read here.
test('a screen-audio stream names the account sharing it, and a voice slot does not', () => {
  assert.equal(
    screenAudioOwner('screen-audio-0b9c7e52-4a1d-4c56-9a3e-2f6d1c0e7a11'),
    '0b9c7e52-4a1d-4c56-9a3e-2f6d1c0e7a11'
  );
  assert.equal(screenAudioOwner('screen-audio-member_7'), 'member_7');
  // Somebody's voice, the screen's picture, and nothing at all are not a screen's sound.
  assert.equal(screenAudioOwner('voice-3'), null);
  assert.equal(screenAudioOwner('screen-0b9c7e52'), null);
  assert.equal(screenAudioOwner(''), null);
  assert.equal(screenAudioOwner(undefined), null);
  assert.equal(screenAudioOwner(null), null);
  // A name with no account in it identifies nobody.
  assert.equal(screenAudioOwner(SCREEN_AUDIO_STREAM_PREFIX), null);
});

test('the server names a screen-audio stream the way the client reads it', () => {
  const sfu = readFileSync(new URL('../../internal/rtc/sfu.go', import.meta.url), 'utf8');
  // The label, and the stream id built from it and the publishing account.
  assert.match(sfu, /label = "screen-audio"/);
  assert.match(sfu, /NewTrackLocalStaticRTP\(codec, label, label\+"-"\+p\.state\.UserID\)/);
  assert.equal(SCREEN_AUDIO_STREAM_PREFIX, 'screen-audio-');
});

test('the voice store keys a screen-audio element to its sharer, not to a slot', () => {
  const store = readFileSync(new URL('../src/lib/voice.svelte.ts', import.meta.url), 'utf8');
  assert.match(store, /const sharer = screenAudioOwner\(streamId\);/);
  assert.match(store, /this\.audioOwners\.get\(audio\) \?\? this\.routes\[/);
  assert.match(store, /this\.audioOwners\.clear\(\);/);
});
