// Sharing a screen, from the browser's side.
//
// Three things make this different from a microphone, and each is a place where a
// naive implementation leaves somebody's screen on without anybody watching:
//
//  1. **Capture is a user gesture, and refusing is normal.** A browser may show a
//     picker, grant it, or refuse it. "Cancel" is a normal answer, not an error, and it
//     must leave the app exactly as it was.
//  2. **A track can end without us being asked.** The user stops sharing in the
//     browser's own UI, or the tab is backgrounded and the browser revokes the
//     capture. `ended` is the event that says so, and nothing else will.
//  3. **The indicator is the browser's, not ours.** Any UI this app draws is
//     supplementary. The product does not claim to stop a capture it did not start.

export type ScreenShareState =
  'idle' | 'asking' | 'sharing' | 'denied' | 'cancelled' | 'unsupported' | 'failed';

export interface ScreenShareResult {
  state: ScreenShareState;
  stream?: MediaStream;
  /** The browser's own name for what was captured, for display. */
  label?: string;
  error?: string;
}

/** Whether this browser can share a screen at all. */
export function canShareScreen(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    !!navigator.mediaDevices &&
    typeof navigator.mediaDevices.getDisplayMedia === 'function'
  );
}

/**
 * Why a refusal happened, as far as the browser will say.
 *
 * `NotAllowedError` covers both "the user said no" and "the user dismissed the
 * picker", and the browser does not distinguish them. Both are normal outcomes; the
 * message says so rather than reporting a failure the person did not cause.
 */
export function classifyCaptureFailure(error: unknown): ScreenShareResult {
  // The name is read off the error rather than through `instanceof DOMException`,
  // because a DOMException from another realm — a worker, a test harness, a
  // polyfilled environment — is still a DOMException to the code that cares.
  const name =
    typeof error === 'object' && error !== null && 'name' in error
      ? String((error as { name: unknown }).name)
      : '';
  if (name === 'NotAllowedError' || name === 'PermissionDeniedError') {
    // The browser cannot tell a refusal from a dismissal. Both are somebody choosing
    // not to share, and calling that an error would be wrong.
    return { state: 'cancelled' };
  }
  if (name === 'NotFoundError' || name === 'DevicesNotFoundError') {
    return { state: 'failed', error: 'There is no screen or window to share.' };
  }
  if (name === 'NotSupportedError' || name === 'SecurityError') {
    // A secure context is required. Saying so is more useful than "share failed".
    return {
      state: 'unsupported',
      error:
        'Screen sharing needs a secure (https) connection. This page is not one, and the browser will not allow a screen to be captured.'
    };
  }
  if (name === 'AbortError') {
    return { state: 'cancelled' };
  }
  return {
    state: 'failed',
    error: error instanceof Error ? error.message : 'The screen could not be shared.'
  };
}

/**
 * Asks the browser for a screen and resolves with what happened.
 *
 * It never throws: a refusal and an error are both ordinary outcomes here, and a
 * caller that has to catch to find out a person declined is a caller that will get it
 * wrong. `systemAudio` is requested but not required — plenty of platforms and
 * browsers do not provide it, and asking for it as a requirement would make screen
 * sharing fail outright for people whose only path to it is the video.
 */
export async function requestScreen(includeSystemAudio = false): Promise<ScreenShareResult> {
  if (!canShareScreen()) {
    return {
      state: 'unsupported',
      error:
        'This browser cannot share a screen. Screen sharing works in current Chrome, Edge and Firefox on a secure connection.'
    };
  }
  let stream: MediaStream;
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({
      video: true,
      // Requested, not required. A browser that cannot provide system audio still
      // returns a video-only stream rather than failing the whole call.
      ...(includeSystemAudio ? { audio: true } : {})
    });
  } catch (error) {
    return classifyCaptureFailure(error);
  }
  const video = stream.getVideoTracks()[0];
  if (!video) {
    stream.getTracks().forEach((track) => track.stop());
    return { state: 'failed', error: 'The browser returned a stream with no screen in it.' };
  }
  return { state: 'sharing', stream, label: video.label };
}

/**
 * Watches a stream for a capture that ends on its own.
 *
 * The `ended` event is the only signal for the three ways a share stops without us:
 * the user stops it in the browser's own UI, the tab is closed, or the browser
 * revokes the capture. Nothing else reports it, and a share that keeps sending after
 * the person stopped it is the failure this exists to prevent.
 */
export function onCaptureEnded(stream: MediaStream, handler: () => void): () => void {
  const tracks = [...stream.getVideoTracks(), ...stream.getAudioTracks()];
  for (const track of tracks) {
    track.addEventListener('ended', handler);
  }
  return () => {
    for (const track of tracks) {
      track.removeEventListener('ended', handler);
    }
  };
}

/** Stops a capture and releases the browser's indicator. */
export function stopScreen(stream: MediaStream): void {
  for (const track of [...stream.getVideoTracks(), ...stream.getAudioTracks()]) {
    track.stop();
  }
}

/**
 * The message to show for a state, phrased for somebody who did not cause it.
 *
 * "Share cancelled" reads as a system message; "You chose not to share" reads as
 * something the app understands. Both are true, and only the second is useful when
 * the person is wondering why nothing happened.
 */
export function screenShareMessage(state: ScreenShareState): string {
  switch (state) {
    case 'asking':
      return 'Choose a screen or window to share…';
    case 'sharing':
      return 'You are sharing your screen. Everyone in the conversation can see it.';
    case 'cancelled':
      return 'Nothing was shared. Your screen is not visible to anyone.';
    case 'denied':
      return 'Screen sharing was refused. Your browser controls this permission.';
    case 'unsupported':
      return 'This browser cannot share a screen on a secure connection.';
    case 'failed':
      return 'The screen could not be shared.';
    default:
      return '';
  }
}
