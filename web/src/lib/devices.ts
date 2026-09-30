/**
 * Working out why a microphone did not start, and saying something the reader can act
 * on.
 *
 * A call that does not start has a small number of ordinary causes, and they are not
 * the same for everybody: a browser that is not permitted, a device that was
 * unplugged, an input nobody owns, an operating system that mutes the application, a
 * headset button that latched muted. They all arrive as one error from
 * `getUserMedia`, and the generic message that used to be shown — "check browser and
 * device microphone permissions" — is the beginning of a search rather than the end of
 * one, because most people who read it have already checked the permission.
 *
 * Nothing here touches a device. It reads the name the browser attaches to an error
 * and produces a sentence, so it is testable without hardware and cannot itself
 * trigger a permission prompt.
 */

export type MicrophoneDiagnosis =
  | 'ok'
  | 'insecure-context'
  | 'no-media-devices'
  | 'permission-denied'
  | 'device-unavailable'
  | 'no-input-device'
  | 'device-in-use'
  | 'unsupported-browser'
  | 'unknown';

/**
 * Reads the diagnosis from whatever the platform threw.
 *
 * The name is the only stable part. A message differs between browsers and even
 * between versions of the same browser, and matching on it produces a diagnosis that
 * changes with an unrelated update.
 */
export function diagnoseMicrophone(error: unknown, secure: boolean): MicrophoneDiagnosis {
  if (!secure) return 'insecure-context';
  if (!hasMediaDevices()) return 'no-media-devices';
  const name = errorName(error);
  switch (name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return 'permission-denied';
    case 'NotFoundError':
    case 'DevicesNotFoundError':
      return 'no-input-device';
    case 'NotReadableError':
    case 'TrackStartError':
      return 'device-in-use';
    case 'OverconstrainedError':
    case 'ConstraintNotSatisfiedError':
      // A stored device that is no longer connected. The stored id is the reason,
      // and it is the only cause here that the person can fix by choosing again.
      return 'device-unavailable';
    case 'TypeError':
      return 'unsupported-browser';
    default:
      return 'unknown';
  }
}

/** The sentence shown for a diagnosis. Plain, second person, and actionable. */
export function microphoneMessage(diagnosis: MicrophoneDiagnosis): string {
  switch (diagnosis) {
    case 'insecure-context':
      return 'Voice needs HTTPS. Open the secure server address, or use localhost.';
    case 'no-media-devices':
      return 'This browser has no microphone access at all. Try a current Chrome, Edge, Firefox or Safari.';
    case 'permission-denied':
      return 'Your browser is blocking the microphone. Allow it in the address bar’s site settings, then try voice again.';
    case 'no-input-device':
      return 'No microphone was found. Connect one, or choose a different input in voice settings.';
    case 'device-unavailable':
      return 'The microphone you chose is not connected. Pick another one in voice settings — your choice was remembered, so it stayed selected after you unplugged it.';
    case 'device-in-use':
      return 'Another program is using your microphone. Close it — a meeting app left running is the usual cause — and try again.';
    case 'unsupported-browser':
      return 'This browser could not start the microphone with the settings the call needs.';
    case 'unknown':
      return 'The microphone did not start. Try again, and check that nothing else is using it.';
    case 'ok':
      return '';
  }
}

function errorName(error: unknown): string {
  if (error instanceof Error) return error.name;
  if (typeof error === 'object' && error !== null && 'name' in error) {
    const name = (error as { name: unknown }).name;
    if (typeof name === 'string') return name;
  }
  return '';
}

/**
 * Whether this browser can ask for a microphone at all.
 *
 * Read as a function rather than a constant so a test can supply an environment
 * without a browser, and so the answer is taken when it is needed rather than when
 * the module was loaded.
 */
function hasMediaDevices(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;
}

/**
 * Describes the inputs a browser will let us see, for a settings list.
 *
 * Browsers only name devices *after* permission has been granted at least once, so an
 * empty list here is the ordinary state for a first visit and not a fault. That is
 * reported rather than hidden, because a settings list that silently shows nothing
 * reads as a product that cannot see the microphone.
 */
export interface InputDevices {
  devices: { id: string; label: string }[];
  /** True when the browser is withholding names because permission was never given. */
  namesPending: boolean;
  error: string;
}

export async function listInputs(): Promise<InputDevices> {
  if (typeof navigator === 'undefined' || !navigator.mediaDevices?.enumerateDevices) {
    return { devices: [], namesPending: false, error: 'This browser cannot list microphones.' };
  }
  try {
    const all = await navigator.mediaDevices.enumerateDevices();
    const devices = all
      .filter((device) => device.kind === 'audioinput')
      .map((device, index) => ({
        id: device.deviceId,
        // An unnamed device is still selectable. Saying "Microphone 1" is honest and
        // leaves the person to recognise it, where an empty row would not.
        label: device.label || `Microphone ${index + 1}`
      }));
    // A browser that has never been granted permission reports the devices with
    // empty ids and labels. That is different from a machine with no microphone, and
    // the two need different words.
    return {
      devices,
      namesPending:
        devices.every((device) => !device.label.startsWith('Microphone ')) || all.length === 0,
      error: ''
    };
  } catch (error) {
    return {
      devices: [],
      namesPending: false,
      error: microphoneMessage(diagnoseMicrophone(error, true))
    };
  }
}
