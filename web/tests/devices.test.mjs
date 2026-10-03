import test from 'node:test';
import assert from 'node:assert/strict';
import {
  diagnoseMicrophone,
  listOutputs,
  microphoneMessage,
  supportsOutputSelection
} from '../src/lib/devices.ts';

// A call that does not start has a small number of ordinary causes, and the generic
// "check your permissions" is the wrong words for most of them. Somebody whose
// headset is latched muted has already checked the permission, and reading it again
// teaches them that this message is not worth reading.

/** A DOMException in everything but its type: the module reads `error.name`. */
class FakeDOMException extends Error {
  constructor(name) {
    super(name);
    this.name = name;
  }
}

/**
 * The one global the diagnosis needs, so a test does not need a browser.
 *
 * `navigator` is a getter-only property on the global object, so it is redefined
 * rather than assigned — assigning throws, and a test helper that throws is a test
 * helper that never runs the thing it was written to check.
 */
function withMediaDevices(value, run) {
  Object.defineProperty(globalThis, 'navigator', {
    value: value ? { mediaDevices: { getUserMedia: () => {} } } : {},
    configurable: true,
    writable: true
  });
  try {
    return run();
  } finally {
    delete globalThis.navigator;
  }
}

test('each ordinary cause is told apart from the others', () => {
  const cases = [
    ['NotAllowedError', 'permission-denied'],
    ['SecurityError', 'permission-denied'],
    ['NotFoundError', 'no-input-device'],
    ['NotReadableError', 'device-in-use'],
    ['OverconstrainedError', 'device-unavailable'],
    ['TypeError', 'unsupported-browser']
  ];
  for (const [name, expected] of cases) {
    withMediaDevices(true, () => {
      assert.equal(
        diagnoseMicrophone(new FakeDOMException(name), true),
        expected,
        `${name} should be read as ${expected}`
      );
    });
  }
});

test('a page that is not secure is diagnosed before anything else', () => {
  // Order matters: an insecure page refuses the microphone too, and reading that as
  // "permission denied" sends somebody to a site permission they do not control.
  withMediaDevices(true, () => {
    assert.equal(
      diagnoseMicrophone(new FakeDOMException('NotAllowedError'), false),
      'insecure-context'
    );
  });
});

test('a browser with no microphone API is not a permission problem', () => {
  withMediaDevices(false, () => {
    assert.equal(
      diagnoseMicrophone(new FakeDOMException('NotAllowedError'), true),
      'no-media-devices'
    );
  });
});

test('an unrecognised failure is still a sentence, not a blank', () => {
  withMediaDevices(true, () => {
    assert.equal(diagnoseMicrophone(new FakeDOMException('SomethingNewIn2027'), true), 'unknown');
    assert.notEqual(microphoneMessage('unknown'), '');
  });
});

test('a thrown non-error does not crash the diagnosis', () => {
  withMediaDevices(true, () => {
    for (const thrown of [undefined, null, 'a string', 42, { name: 'NotFoundError' }]) {
      const diagnosis = diagnoseMicrophone(thrown, true);
      assert.ok(microphoneMessage(diagnosis), `a thrown ${String(thrown)} produced no words`);
    }
  });
});

test('every diagnosis has words, except success', () => {
  for (const diagnosis of [
    'insecure-context',
    'no-media-devices',
    'permission-denied',
    'no-input-device',
    'device-unavailable',
    'device-in-use',
    'unsupported-browser',
    'unknown'
  ]) {
    const message = microphoneMessage(diagnosis);
    assert.ok(message.length > 20, `"${diagnosis}" has no usable sentence`);
    assert.doesNotMatch(
      message,
      /undefined|null|NaN/,
      `"${diagnosis}" leaks a value into the message`
    );
  }
  assert.equal(microphoneMessage('ok'), '');
});

test('the words name what the reader can do, not what the browser called it', () => {
  // "device-in-use" must not read as a device error — the device is fine, and the
  // reader needs to know the other program is the problem.
  assert.match(microphoneMessage('device-in-use'), /another program|other program|close/i);
  assert.doesNotMatch(microphoneMessage('device-in-use'), /error|fail/i);
  // A stored device that is gone must say so, because the choice was remembered.
  assert.match(microphoneMessage('device-unavailable'), /remember/i);
  assert.doesNotMatch(microphoneMessage('permission-denied'), /fail/i);
});

test('no two diagnoses share the same words', () => {
  // Two causes with one message is a message that is wrong for one of them, and the
  // reader has no way to tell which.
  const messages = [
    'insecure-context',
    'no-media-devices',
    'permission-denied',
    'no-input-device',
    'device-unavailable',
    'device-in-use',
    'unsupported-browser'
  ].map(microphoneMessage);
  assert.equal(new Set(messages).size, messages.length);
});

// Output selection, where the browser has it. Firefox and Safari have no setSinkId, and
// the honest feature is the sentence that says so rather than a list that appears to work.
function withOutputSupport(devices, run, supported = true) {
  const previousMedia = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
  const previousElement = Object.getOwnPropertyDescriptor(globalThis, 'HTMLMediaElement');
  Object.defineProperty(globalThis, 'navigator', {
    value: { mediaDevices: { enumerateDevices: async () => devices } },
    configurable: true,
    writable: true
  });
  Object.defineProperty(globalThis, 'HTMLMediaElement', {
    value: supported ? { prototype: { setSinkId: async () => {} } } : { prototype: {} },
    configurable: true,
    writable: true
  });
  try {
    return run();
  } finally {
    if (previousMedia) Object.defineProperty(globalThis, 'navigator', previousMedia);
    else delete globalThis.navigator;
    if (previousElement) Object.defineProperty(globalThis, 'HTMLMediaElement', previousElement);
    else delete globalThis.HTMLMediaElement;
  }
}

test('the speakers are listed, and a browser without them says so in words', async () => {
  const devices = [
    { kind: 'audioinput', deviceId: 'mic-1', label: 'Desk microphone' },
    { kind: 'audiooutput', deviceId: 'default', label: '' },
    { kind: 'audiooutput', deviceId: 'speaker-2', label: 'Headphones' }
  ];
  await withOutputSupport(devices, async () => {
    const listed = await listOutputs();
    assert.deepEqual(
      listed.devices.map((device) => device.label),
      // Chrome reports the default output with no label before permission is granted; it
      // is still the device a person means by "system default", so it is named that.
      ['System default', 'Headphones']
    );
    assert.equal(listed.error, '');
  });
  await withOutputSupport(
    devices,
    async () => {
      const listed = await listOutputs();
      assert.deepEqual(listed.devices, []);
      assert.match(listed.error, /system default/i);
    },
    false
  );
  assert.equal(
    withOutputSupport([], () => supportsOutputSelection()),
    true
  );
  assert.equal(
    withOutputSupport([], () => supportsOutputSelection(), false),
    false
  );
});

test('the speakers are not called microphones when the listing fails', async () => {
  await withOutputSupport(null, async () => {
    const listed = await listOutputs();
    assert.deepEqual(listed.devices, []);
    // A failure message that talks about microphones is a message about the wrong device.
    assert.match(listed.error, /speakers/i);
    assert.doesNotMatch(listed.error, /microphone/i);
  });
});
