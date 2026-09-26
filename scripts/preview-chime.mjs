import { mkdir, writeFile } from "node:fs/promises";
import { createVoiceChime } from "../web/src/lib/voice-chime.ts";

const rate = 48000;
const samples = createVoiceChime(rate);
const wav = Buffer.alloc(44 + samples.length * 2);
wav.write("RIFF", 0);
wav.writeUInt32LE(wav.length - 8, 4);
wav.write("WAVEfmt ", 8);
wav.writeUInt32LE(16, 16);
wav.writeUInt16LE(1, 20);
wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(rate, 24);
wav.writeUInt32LE(rate * 2, 28);
wav.writeUInt16LE(2, 32);
wav.writeUInt16LE(16, 34);
wav.write("data", 36);
wav.writeUInt32LE(samples.length * 2, 40);
samples.forEach((sample, index) =>
  wav.writeInt16LE(Math.round(sample * 32767), 44 + index * 2),
);
await mkdir("dist", { recursive: true });
await writeFile("dist/voice-join.wav", wav);
console.log("Chime preview: dist/voice-join.wav");
