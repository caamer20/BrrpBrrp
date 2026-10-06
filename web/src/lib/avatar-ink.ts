// The colour an avatar's initials are drawn in, chosen from the background rather than
// fixed.
//
// Every member's avatar carries a colour from the product's palette, and a deleted member's
// is the grey `#808080`. The initials were drawn in one dark ink (#172334), which measures
// 4.0:1 on that grey — under the 4.5:1 minimum for text this size, and found by the
// accessibility sweep the screen-share journey runs (T-A11Y-01's local half). A fixed ink
// cannot be right for an arbitrary background, so the ink is derived: whichever of the two
// endpoints contrasts better, which is a single luminance threshold (0.179 is the point
// where black and white are equally legible, per WCAG's own definition).
//
// This is deliberately not a general colour library: two inks, one threshold, and a
// property test that walks a coarse sweep of the RGB cube asserting every result clears
// 4.5:1.

/**
 * The colours a new account's avatar is seeded from, in the order the server cycles them.
 *
 * It mirrors `internal/api/admin.go`'s own list, and it is a set of choices rather than a
 * rule: the server accepts any six-digit hex, so an account whose colour an administrator
 * set outside this list keeps it and simply shows no choice as selected.
 */
export const AVATAR_PALETTE = ['#89a8f4', '#e9af7e', '#b3d693', '#c5a4e8', '#7ac5c5', '#e59cae'];

/** The dark ink, legible on mid and light backgrounds. */
const DARK_INK = '#10161f';
/** The light ink, legible on mid and dark backgrounds. */
const LIGHT_INK = '#f7fbff';
/** The luminance at which the two inks are equally legible. */
const CROSSOVER = 0.179;

/**
 * Reads `#rgb` or `#rrggbb` into its three channels.
 *
 * An unreadable value is not this function's problem to report: it returns null and the
 * caller keeps the default ink, because a colour an administrator typed wrongly is not a
 * reason to draw nothing.
 */
export function channelsOf(color: string): [number, number, number] | null {
  const text = (color || '').trim();
  if (!text.startsWith('#')) return null;
  const hex = text.slice(1);
  if (hex.length === 3) {
    const [r, g, b] = [...hex].map((digit) => parseInt(digit + digit, 16));
    return [r, g, b].some(Number.isNaN) ? null : [r, g, b];
  }
  if (hex.length === 6) {
    const value = Number.parseInt(hex, 16);
    if (Number.isNaN(value)) return null;
    return [(value >> 16) & 0xff, (value >> 8) & 0xff, value & 0xff];
  }
  return null;
}

/** WCAG relative luminance. */
export function luminance(color: string): number | null {
  const channels = channelsOf(color);
  if (!channels) return null;
  const [r, g, b] = channels.map((channel) => {
    const value = channel / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG contrast ratio between two colours, or null if either cannot be read. */
export function contrast(a: string, b: string): number | null {
  const first = luminance(a);
  const second = luminance(b);
  if (first === null || second === null) return null;
  const lighter = Math.max(first, second);
  const darker = Math.min(first, second);
  return (lighter + 0.05) / (darker + 0.05);
}

/**
 * The ink for initials drawn on `background`: whichever end contrasts better.
 *
 * The threshold constant is here because it is the documented crossover for black and
 * white; comparing the two measured ratios is what the rule actually does, because these
 * inks are not quite black and white and a saturated mid-tone can prefer either.
 */
export function avatarInk(background: string): string {
  const value = luminance(background);
  if (value === null) return DARK_INK;
  const dark = contrast(DARK_INK, background) ?? 0;
  const light = contrast(LIGHT_INK, background) ?? 0;
  if (dark >= light) return DARK_INK;
  return light > dark ? LIGHT_INK : value > CROSSOVER ? DARK_INK : LIGHT_INK;
}

/** Mixes a colour toward black or white. `amount` is how far. */
function mix(color: string, target: [number, number, number], amount: number): string {
  const channels = channelsOf(color);
  if (!channels) return color;
  return `#${channels
    .map((channel, index) => Math.round(channel + (target[index] - channel) * amount))
    .map((channel) => channel.toString(16).padStart(2, '0'))
    .join('')}`;
}

/**
 * The background an avatar actually uses.
 *
 * A saturated mid-tone — `#aa44dd` is the one the property test finds — has no ink at this
 * size that clears 4.5:1, because every pair of extremes fails near the middle of the
 * luminance range. Rather than draw unreadable initials or refuse an administrator's
 * colour, the background is moved just far enough toward one end for its ink to pass: the
 * hue survives, and the letters are legible. The loop is bounded, so a colour that cannot
 * be read at all still terminates with the closest thing to it.
 */
export function avatarBackground(color: string): string {
  if (luminance(color) === null) return color;
  const ink = avatarInk(color);
  const target: [number, number, number] = ink === DARK_INK ? [255, 255, 255] : [0, 0, 0];
  let candidate = color;
  for (let step = 0; step < 12; step += 1) {
    if ((contrast(ink, candidate) ?? 0) >= 4.5) return candidate;
    candidate = mix(candidate, target, 0.18);
  }
  return candidate;
}

/** The pair the avatar renders: a legible background and its ink. */
export function avatarStyle(color: string): { background: string; ink: string } {
  const background = avatarBackground(color);
  return { background, ink: avatarInk(background) };
}
