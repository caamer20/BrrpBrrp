// The initials on an avatar are text, so they owe the contrast minimum too. The ink used to
// be a constant, and a deleted member's grey avatar measured 4.0:1 against it — found by the
// accessibility sweep inside the screen-share journey, not by reading the stylesheet.
import assert from 'node:assert/strict';
import test from 'node:test';
import { avatarStyle, contrast, channelsOf, luminance } from '../src/lib/avatar-ink.ts';

test('the colours are read in both shorthand forms, and rubbish is refused', () => {
  assert.deepEqual(channelsOf('#fff'), [255, 255, 255]);
  assert.deepEqual(channelsOf('#10161f'), [16, 22, 31]);
  assert.equal(channelsOf('red'), null);
  assert.equal(channelsOf('#12345'), null);
  assert.equal(channelsOf('#zzzzzz'), null);
  assert.equal(luminance('red'), null);
  assert.equal(contrast('#fff', 'red'), null);
});

test('the ink clears the minimum on the colours the product actually uses', () => {
  // The deleted member's grey is the one that failed; the palette in the account handler is
  // the set every other avatar is drawn from.
  const backgrounds = [
    '#808080',
    '#89a8f4',
    '#f2a1c0',
    '#8ad0b0',
    '#f4c76a',
    '#c0a1f2',
    '#9fd8f4',
    '#f29f9f',
    '#ffffff',
    '#000000'
  ];
  for (const background of backgrounds) {
    const { background: drawn, ink } = avatarStyle(background);
    const ratio = contrast(ink, drawn);
    assert.ok(ratio !== null && ratio >= 4.5, `${background} -> ${ink} on ${drawn} is ${ratio}`);
  }
});

test('every colour in the cube gets an ink that clears the minimum', () => {
  // A property rather than a list: an administrator can set any hex colour, so the rule has
  // to hold everywhere, not only on the swatches somebody thought to test.
  let worst = { ratio: Infinity, background: '', ink: '' };
  for (let r = 0; r < 256; r += 17) {
    for (let g = 0; g < 256; g += 17) {
      for (let b = 0; b < 256; b += 17) {
        const background = `#${[r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('')}`;
        const { background: drawn, ink } = avatarStyle(background);
        const ratio = contrast(ink, drawn) ?? 0;
        if (ratio < worst.ratio) worst = { ratio, background: drawn, ink };
      }
    }
  }
  assert.ok(
    worst.ratio >= 4.5,
    `the worst pair is ${worst.ink} on ${worst.background} at ${worst.ratio.toFixed(2)}:1`
  );
});
