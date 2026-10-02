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
 * The two capture profiles the product offers, as data rather than as code paths.
 *
 * A screen showing movement and a screen showing text are different pictures with
 * different costs, and one profile cannot serve both: a film at 5 fps is a slideshow,
 * and a document at 15 fps spends four times the bandwidth on a still image. The
 * motion preset therefore asks for 720p at 15 fps and lets the encoder drop resolution
 * under pressure, and the text preset asks for 1080p at 5 fps and tells the encoder to
 * keep the resolution and drop frames instead — a blurred frame of text is unreadable,
 * a late one is merely late.
 *
 * `parameters.maxBitrate` is the sender's own ceiling and sits below the server's
 * per-publication ceiling (`ScreenBitrateCeiling`, 2.5 Mbit/s): a client that obeys its
 * cap never meets the server's enforcement, which is what makes that enforcement a
 * bound on a misbehaving publisher rather than on everybody.
 */
export type ScreenPreset = 'motion' | 'text';

export interface ScreenPresetSpec {
  id: ScreenPreset;
  label: string;
  /** What the picker is asked for. Ideals, not requirements: a screen of another size
   * is shared at its own size rather than refused. */
  constraints: MediaTrackConstraints;
  /** The encoding's ceiling and rate, applied to the live sender. */
  encoding: RTCRtpEncodingParameters;
  /** How the encoder degrades when it cannot keep up. It belongs to the send
   * parameters rather than to an encoding, and it is what separates "keep the
   * resolution, drop frames" from "keep the frame rate, blur the text". */
  degradationPreference: RTCDegradationPreference;
  /** Advisory, for the encoder: what the pixels are for. */
  contentHint: 'motion' | 'text';
}

export const SCREEN_PRESETS: Record<ScreenPreset, ScreenPresetSpec> = {
  motion: {
    id: 'motion',
    label: 'Motion — 720p at 15 fps',
    constraints: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 15 } },
    encoding: { maxBitrate: 2_000_000, maxFramerate: 15 },
    degradationPreference: 'balanced',
    contentHint: 'motion'
  },
  text: {
    id: 'text',
    label: 'Text — 1080p at 5 fps',
    constraints: { width: { ideal: 1920 }, height: { ideal: 1080 }, frameRate: { ideal: 5 } },
    encoding: { maxBitrate: 2_000_000, maxFramerate: 5 },
    degradationPreference: 'maintain-resolution',
    contentHint: 'text'
  }
};

/**
 * Reads a preset out of stored text, falling back to motion.
 *
 * A preference is a string in storage, and storage is editable: a value this build does
 * not know must not produce a share with no profile at all.
 */
export function screenPresetFromValue(value: string | null | undefined): ScreenPreset {
  return Object.prototype.hasOwnProperty.call(SCREEN_PRESETS, value ?? '')
    ? (value as ScreenPreset)
    : 'motion';
}

/**
 * Applies a preset to a sender that already exists, which is what makes the choice
 * effective *during* a share rather than only at the next one.
 *
 * Best-effort by construction: a browser that refuses one of the parameters still
 * shares the screen, and the caller is told whether the full preset took hold so the
 * interface can say what is actually happening instead of what was asked for.
 */
export async function applyScreenPreset(
  sender: RTCRtpSender,
  preset: ScreenPreset
): Promise<boolean> {
  const spec = SCREEN_PRESETS[preset];
  try {
    const parameters = sender.getParameters();
    if (!parameters.encodings || parameters.encodings.length === 0) return false;
    parameters.encodings[0] = { ...parameters.encodings[0], ...spec.encoding };
    parameters.degradationPreference = spec.degradationPreference;
    await sender.setParameters(parameters);
    return true;
  } catch {
    return false;
  }
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
 *
 * The preset shapes the request: a share meant to show movement is asked for at a
 * lower resolution and a human frame rate, and one meant to show text at a higher
 * resolution and a lower one, because those are different pictures with different
 * costs (see `SCREEN_PRESETS`).
 */
export async function requestScreen(
  includeSystemAudio = false,
  preset: ScreenPreset = 'motion'
): Promise<ScreenShareResult> {
  if (!canShareScreen()) {
    return {
      state: 'unsupported',
      error:
        'This browser cannot share a screen. Screen sharing works in current Chrome, Edge and Firefox on a secure connection.'
    };
  }
  const spec = SCREEN_PRESETS[preset];
  let stream: MediaStream;
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({
      video: spec.constraints,
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
  // The content hint tells the encoder what the picture is *for*. It is advisory and
  // some browsers ignore it, which is why it is not the only thing that distinguishes
  // the presets — the resolution, the frame rate and the sender's ceiling are all
  // applied regardless.
  try {
    video.contentHint = spec.contentHint;
  } catch {
    /* A browser that does not take a hint shares anyway. */
  }
  return { state: 'sharing', stream, label: video.label };
}

/**
 * Decides whether a returned capture may still be published.
 *
 * `getDisplayMedia` opens a browser-owned modal. While it is open the person can do
 * anything the app allows: switch account, leave the call, move to another voice
 * channel, or have the server withdraw their permission. The promise then resolves with
 * a stream that belongs to *that* moment, and the caller is somewhere else entirely.
 *
 * Publishing it anyway is the failure this prevents, and it is a disclosure rather than
 * a glitch: the person believed they were sharing into a room they had left, or as an
 * account that no longer holds the permission, and the room they are in now receives a
 * screen they never chose to show it. So the answer is a comparison against everything
 * that identifies the audience, and a mismatch means the capture is stopped and dropped
 * rather than renegotiated.
 *
 * `generation` is the call's identity: it advances on every leave, join, reconnect and
 * authority change, so any of those is a mismatch without each being enumerated here.
 */
export interface CaptureAudience {
  /** The call the share was started from. */
  channelId: string;
  /** The signed-in account the share was started under. */
  userId: string;
  /** The call's connection identity. */
  connectionId: string;
  /** The call's generation counter, advanced by any leave, join or authority change. */
  generation: number;
}

/**
 * Whether a capture started for `started` may be published now that the app is `now`.
 *
 * Returns the reason when it may not, because "stale" on its own tells the person
 * nothing and a silent drop reads as the browser failing.
 */
export function captureStillWanted(
  started: CaptureAudience,
  now: CaptureAudience
): { ok: true } | { ok: false; reason: string } {
  // The account is checked first because it is the reason a person can act on, and the
  // one that is true whenever several of these changed at once. Signing in as somebody
  // else replaces the session and clears the call, so a generation move usually
  // accompanies it — and reporting "the conversation changed" for that would send the
  // person looking at the wrong thing. The conversation did not change; they did.
  if (started.userId !== now.userId) {
    // The sharpest case. The capture belongs to the previous account, and the next one
    // would inherit it.
    return {
      ok: false,
      reason: 'Your screen was not shared because you signed in to a different account.'
    };
  }
  if (started.generation !== now.generation) {
    return {
      ok: false,
      reason:
        'Your screen was not shared because the conversation changed while you were choosing what to share.'
    };
  }
  if (started.connectionId !== now.connectionId) {
    return {
      ok: false,
      reason:
        'Your screen was not shared because the call reconnected while you were choosing what to share.'
    };
  }
  if (started.channelId !== now.channelId) {
    return {
      ok: false,
      reason: 'Your screen was not shared because you moved to another conversation.'
    };
  }
  return { ok: true };
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

/**
 * Describes what a capture actually delivered, against what was asked for.
 *
 * System audio is requested, never required: Windows and Linux commonly cannot
 * provide it, and a browser that cannot is expected to return video-only rather than
 * fail. The person sharing needs to know which of the two they got — a silent screen
 * that looks like a muted call sends somebody hunting through their own settings for a
 * problem that is not there.
 */
export function screenAudioOutcome(
  asked: boolean,
  deliveredAudioTracks: number
): 'not-requested' | 'delivered' | 'unavailable' {
  if (!asked) return 'not-requested';
  return deliveredAudioTracks > 0 ? 'delivered' : 'unavailable';
}

/**
 * The words for that outcome, or nothing when there is nothing to say.
 *
 * Silence is the right answer for the ordinary case: a video-only share is what most
 * people get, and a notice on every share would train people to dismiss notices.
 */
export function screenAudioMessage(outcome: 'not-requested' | 'delivered' | 'unavailable'): string {
  if (outcome === 'unavailable') {
    return 'Sharing your screen. This browser or system did not provide its audio, so people will see the screen without sound.';
  }
  return '';
}
