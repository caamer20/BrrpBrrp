// Asking the server to *send* this client a screen, without claiming to publish one.
//
// The server cannot tell a viewer from a publisher by reading the offer: a receiving
// video transceiver produces a media section with no direction attribute at all, which
// the SDP default makes `sendrecv`. Two clients with opposite intentions produce
// byte-identical descriptions. What does survive is the *negotiated* transceiver — a
// viewer's track lands on its own `recvonly` video transceiver, distinct from the
// server's output — and the only reliable way to put a track there from a browser is
// to give it its own stream id.
//
// This is why the stream id is exported rather than inlined: the server reads the same
// string, and a marker that lives in one place on each side is a marker that can be
// changed in one place.

/** The stream id a client puts on a track it only wants to receive. */
export const VIEW_STREAM_ID = 'brrpbrrp-view';

/**
 * Whether a track is one the client only receives.
 *
 * Used to tell a screen from a microphone on arrival, which is the other half of the
 * same problem: a client that has both cannot tell which `ontrack` event is a person
 * speaking and which is somebody's desktop.
 */
export function isViewOnlyTrack(stream: MediaStream | undefined | null): boolean {
  if (!stream) return false;
  const track = stream.getVideoTracks()[0];
  return !!track && track.id.startsWith(VIEW_STREAM_ID);
}

/**
 * The video half of a received screen.
 *
 * A screen stream is video with an optional audio track beside it (the screen's own
 * audio is a separate publication). Only the video half is ever silenced: a viewer who
 * chose audio-only wants to hear the person's desktop, which is the reason the choice
 * exists.
 */
export function screenVideoTracks(stream: MediaStream | undefined | null): MediaStreamTrack[] {
  return stream ? stream.getVideoTracks() : [];
}

/**
 * Whether the picture is being decoded at all, on this viewer.
 *
 * Audio-only is a data preference, not a display trick: a hidden element still receives
 * and decodes the video, so the tracks are disabled rather than the element concealed.
 * The caller keeps the element mounted, because the control that turns the picture back
 * on has to stay reachable.
 */
export function applyScreenAudioOnly(
  stream: MediaStream | undefined | null,
  audioOnly: boolean
): void {
  for (const track of screenVideoTracks(stream)) track.enabled = !audioOnly;
}
