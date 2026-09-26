import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createVoiceChime, VoiceChime } from '../src/lib/voice-chime.ts';

test('chime stays quiet, finite and fades to silence at common device sample rates', () => {
  for (const rate of [24000, 44100, 48000, 96000]) {
    const samples = createVoiceChime(rate);
    assert.equal(samples.length, Math.ceil(rate * 0.8));
    assert.equal(samples[0], 0);
    assert.equal(samples.at(-1), 0);
    let peak = 0;
    for (const sample of samples) {
      assert.ok(Number.isFinite(sample));
      peak = Math.max(peak, Math.abs(sample));
    }
    assert.ok(peak > 0.05 && peak < 0.2, `unexpected peak: ${peak}`);
  }
});

function audioContext() {
  const sources = [];
  return {
    sources,
    sampleRate: 48000,
    state: 'running',
    destination: {},
    createGain: () => ({ gain: { value: 1 }, connect() {} }),
    createBuffer: () => ({ copyToChannel() {} }),
    createBufferSource() {
      const source = {
        started: false,
        stopped: false,
        disconnected: false,
        connect() {},
        disconnect() {
          this.disconnected = true;
        },
        start() {
          this.started = true;
        },
        stop() {
          this.stopped = true;
        }
      };
      sources.push(source);
      return source;
    }
  };
}

test('simultaneous arrivals share one chime and completed sources are released', () => {
  const context = audioContext();
  const chime = new VoiceChime(context);
  assert.equal(chime.play(), true);
  assert.equal(chime.play(), true);
  assert.equal(context.sources.length, 1);
  assert.equal(context.sources[0].started, true);
  context.sources[0].onended();
  assert.equal(context.sources[0].disconnected, true);
  chime.play();
  assert.equal(context.sources.length, 2);
  chime.stop();
  assert.equal(context.sources[1].stopped, true);
  chime.stop();
});

test('blocked playback is skipped without queuing a delayed chime', () => {
  const context = audioContext();
  const chime = new VoiceChime(context);
  context.state = 'suspended';
  assert.equal(chime.play(), false);
  assert.equal(context.sources.length, 0);
  context.state = 'running';
  assert.equal(context.sources.length, 0);
  assert.equal(chime.play(), true);
  context.state = 'closed';
  assert.equal(chime.play(), false);
});
