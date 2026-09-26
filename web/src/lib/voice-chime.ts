// Two soft glass tones, with a rounded attack and a short, warm tail.
export function createVoiceChime(sampleRate: number): Float32Array<ArrayBuffer> {
  const samples = new Float32Array(Math.ceil(sampleRate * 0.8));
  const notes = [
    { frequency: 659.255, start: 0, level: 0.11 },
    { frequency: 987.767, start: 0.105, level: 0.095 }
  ];
  for (let index = 0; index < samples.length; index++) {
    const time = index / sampleRate;
    for (const note of notes) {
      const age = time - note.start;
      if (age < 0) continue;
      const attack = 1 - Math.exp(-age / 0.008);
      const phase = 2 * Math.PI * note.frequency * age;
      const tone =
        Math.sin(phase) * Math.exp(-age / 0.21) +
        0.12 * Math.sin(phase * 2.003) * Math.exp(-age / 0.09) +
        0.035 * Math.sin(phase * 3.01) * Math.exp(-age / 0.055);
      const fade = Math.min(1, (samples.length - 1 - index) / (sampleRate * 0.08));
      samples[index] += note.level * attack * tone * fade;
    }
  }
  return samples;
}

export class VoiceChime {
  private context: AudioContext;
  private buffer: AudioBuffer;
  private source: AudioBufferSourceNode | null = null;
  private gain: GainNode;

  constructor(context: AudioContext) {
    this.context = context;
    this.gain = context.createGain();
    this.gain.connect(context.destination);
    const samples = createVoiceChime(context.sampleRate);
    this.buffer = context.createBuffer(1, samples.length, context.sampleRate);
    this.buffer.copyToChannel(samples, 0);
  }

  play(volume = 1): boolean {
    // Skip suspended audio instead of queuing a stale notification for later.
    if (this.context.state !== 'running') return false;
    if (this.source) return true;
    const source = this.context.createBufferSource();
    source.buffer = this.buffer;
    this.gain.gain.value = Math.max(0, Math.min(1, volume));
    source.connect(this.gain);
    source.onended = () => {
      source.disconnect();
      if (this.source === source) this.source = null;
    };
    this.source = source;
    source.start();
    return true;
  }

  stop() {
    this.source?.stop();
    this.source = null;
  }
}
