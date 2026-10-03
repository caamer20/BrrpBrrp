// How a message's text becomes renderable pieces.
//
// Two things matter here and both are about not being fooled by the text itself:
//
// A code block is literal. `@alice` inside one is code, not a mention, and turning it
// into a live mention would notify somebody about text that never mentioned them —
// and in a shared snippet, a link to the wrong person. The same is true of a URL: one
// inside a code block is a sample, not somewhere to go.
//
// A URL is only a link if it is really one. The scheme is checked against an exact
// allowlist rather than a prefix test, so `https://` glued onto something else is not
// a link, and nothing is ever handed to the DOM as markup — every piece is text, and
// the component decides what it is.

export type Token =
  | { kind: 'text'; value: string }
  | { kind: 'code'; value: string }
  | { kind: 'mention'; value: string; name: string }
  | { kind: 'link'; value: string }
  | { kind: 'emphasis'; style: 'bold' | 'italic' | 'strike'; tokens: Token[] }
  | { kind: 'quote'; tokens: Token[] };

/**
 * The emphasis markers a message may use, longest first.
 *
 * Longest first is not a preference: `**bold**` read with `*` before `**` is an empty
 * italic pair followed by text, which is how a formatting pass turns a bold word into
 * punctuation.
 */
const EMPHASIS: { marker: string; style: 'bold' | 'italic' | 'strike' }[] = [
  { marker: '**', style: 'bold' },
  { marker: '__', style: 'bold' },
  { marker: '~~', style: 'strike' },
  { marker: '*', style: 'italic' },
  { marker: '_', style: 'italic' }
];

/**
 * How deep emphasis may nest. A cap rather than a limit on the person: it is a bound on
 * the work one message can make the renderer do, and three is already past anything
 * somebody types on purpose.
 */
const MAX_EMPHASIS_DEPTH = 3;

const WORD = /[A-Za-z0-9_]/;

/**
 * The schemes a message may link to.
 *
 * `http` and `https` only. `javascript:`, `data:` and `file:` are absent because a
 * message is written by anybody who can post, and a link is a thing a reader clicks.
 */
const LINK_SCHEME = /^https?:\/\//i;

/**
 * Splits message text into text, code, mention and link pieces.
 *
 * Code blocks are fenced with three or more backticks on their own line, with an
 * optional language tag on the opening fence — the convention every code highlighter
 * uses, so a snippet pasted from a terminal renders the way its author expected. An
 * unterminated fence runs to the end of the message rather than being silently
 * dropped, because a half-pasted snippet should still read as code.
 */
export function tokenizeMessage(input: string): Token[] {
  const tokens: Token[] = [];
  const lines = input.split('\n');
  let inCode = false;
  const code: string[] = [];

  const flushCode = () => {
    if (!code.length) return;
    tokens.push({ kind: 'code', value: code.join('\n') });
    code.length = 0;
  };

  // Consecutive quoted lines are one quote, so a quoted paragraph renders as a paragraph
  // rather than as a stack of one-line quotes. The content inside keeps every other rule:
  // a mention in a quote is still a mention, and a link still needs its scheme.
  let quoted: string[] = [];
  const flushQuote = () => {
    if (!quoted.length) return;
    const children: Token[] = [];
    quoted.forEach((line, index) => {
      if (index > 0) children.push({ kind: 'text', value: '\n' });
      for (const piece of tokenizeLine(line)) children.push(piece);
    });
    tokens.push({ kind: 'quote', tokens: mergeText(children) });
    quoted = [];
  };

  for (const [index, line] of lines.entries()) {
    const fence = /^\s*(```+|~~~+)\s*([A-Za-z0-9+#-]*)\s*$/.exec(line);
    if (fence) {
      flushQuote();
      if (inCode) {
        flushCode();
        inCode = false;
      } else {
        flushCode();
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      code.push(line);
      continue;
    }
    const quote = /^\s*>\s?(.*)$/.exec(line);
    if (quote) {
      quoted.push(quote[1]);
      continue;
    }
    flushQuote();
    // The line break is itself content. A message is not a sequence of unrelated lines:
    // `first` and `second` on consecutive lines are one sentence with a line break in
    // it, and rendering them as `firstsecond` changes what was written.
    //
    // It is emitted as a separator rather than appended to the previous run, because a
    // previous run may end with a mention or a link and appending would glue the break
    // onto a token the component renders as an element. A dedicated text token keeps
    // the merge below from folding it into anything.
    if (index > 0) tokens.push({ kind: 'text', value: '\n' });
    for (const piece of tokenizeLine(line)) tokens.push(piece);
  }
  // An unterminated fence is still code: the alternative is rendering a half-pasted
  // snippet as prose, with its mentions and links live. A quote at the end of a message
  // is flushed for the same reason.
  flushQuote();
  flushCode();
  return mergeText(tokens);
}

/**
 * Splits one line into mentions, links, code spans, emphasis and plain runs.
 *
 * Emphasis is read per line and never across one: a `**` left open at the end of a line
 * must not swallow the next, because the common ways to leave one open are a signature, a
 * pasted excerpt and a typo — and all three would silently restyle everything below them.
 */
function tokenizeLine(line: string, depth = 0): Token[] {
  if (depth >= MAX_EMPHASIS_DEPTH) return tokenizePlain(line);
  const pair = findEmphasis(line);
  if (!pair) return tokenizePlain(line);
  const pieces: Token[] = tokenizePlain(line.slice(0, pair.start));
  pieces.push({ kind: 'emphasis', style: pair.style, tokens: tokenizeLine(pair.inner, depth + 1) });
  pieces.push(...tokenizeLine(line.slice(pair.end), depth));
  return pieces;
}

/**
 * Finds the first emphasis pair on a line that is really a pair, or null when there is
 * none.
 *
 * Both markers have to be markers in their own right. `_` inside a word is how people
 * write `snake_case`, and a `*` that opens a list or sits between digits is arithmetic,
 * so an underscore only opens where it is not preceded by a word character and only
 * closes where it is not followed by one. A pair with nothing between the markers is not
 * emphasis either — that is what `**` on its own looks like.
 */
function findEmphasis(
  line: string
): { start: number; end: number; inner: string; style: 'bold' | 'italic' | 'strike' } | null {
  // The *earliest* pair wins, whatever its marker. Reading the markers in their own order
  // instead meant `a **bold** and *italic* and ~~struck~~` lost the italic: `~~` came
  // before `*` in the list, so the strike pair further down the line was taken first and
  // the recursion resumed after it, leaving the italic unread.
  let best: {
    start: number;
    end: number;
    inner: string;
    style: 'bold' | 'italic' | 'strike';
  } | null = null;
  for (const { marker, style } of EMPHASIS) {
    let from = 0;
    while (from < line.length) {
      const start = line.indexOf(marker, from);
      if (start === -1) break;
      if (best && start > best.start) break;
      const before = start > 0 ? line[start - 1] : '';
      const after = line[start + marker.length] ?? '';
      if (marker === '_' && before !== '' && WORD.test(before)) {
        from = start + 1;
        continue;
      }
      if (after === '' || /\s/.test(after)) {
        from = start + marker.length;
        continue;
      }
      // The closing marker: not part of a longer run of the same character, and for `_`
      // not inside a word.
      let search = start + marker.length;
      for (;;) {
        const end = line.indexOf(marker, search);
        if (end === -1) break;
        const inner = line.slice(start + marker.length, end);
        const closedBefore = line[end - 1] ?? '';
        const closedAfter = line[end + marker.length] ?? '';
        const sound =
          inner.length > 0 &&
          !/\s$/.test(inner) &&
          !(marker === '_' && closedAfter !== '' && WORD.test(closedAfter)) &&
          !(marker.length === 1 && closedBefore === marker);
        if (sound) {
          const candidate = { start, end: end + marker.length, inner, style };
          if (!best || candidate.start < best.start) best = candidate;
          break;
        }
        search = end + 1;
      }
      from = best && best.start === start ? start + marker.length : start + 1;
    }
  }
  return best;
}

/** Splits one line of ordinary text into mentions, links and plain runs. */
function tokenizePlain(line: string): Token[] {
  const pieces: Token[] = [];
  // A mention is a whole token. Allowing a mention to end mid-word is how `@alice`
  // inside an email address becomes a live mention for the wrong person.
  const pattern = /@[a-zA-Z0-9_]{2,32}(?![a-zA-Z0-9_])|https?:\/\/[^\s<>"]+/gi;
  let last = 0;
  for (const match of line.matchAll(pattern)) {
    const start = match.index ?? 0;
    if (start > last) pieces.push({ kind: 'text', value: line.slice(last, start) });
    const value = match[0];
    // An @ that is the local part of an email address is not a mention. Notifying
    // somebody about `alice@example.com` is both wrong and a small leak: it turns a
    // pasted address into a signal about who is on this server.
    if (value.startsWith('@') && start > 0 && /[A-Za-z0-9._%+-]/.test(line[start - 1])) {
      pieces.push({ kind: 'text', value });
      last = start + value.length;
      continue;
    }
    if (LINK_SCHEME.test(value)) {
      // Trailing punctuation is almost never part of the address somebody meant, and
      // a link to a host with a full stop on the end is a link to the wrong place.
      const trimmed = value.replace(/[.,;:!?)\]]+$/, '');
      pieces.push({ kind: 'link', value: trimmed });
      const rest = value.slice(trimmed.length);
      if (rest) pieces.push({ kind: 'text', value: rest });
    } else {
      pieces.push({ kind: 'mention', value, name: value.slice(1) });
    }
    last = start + value.length;
  }
  if (last < line.length) pieces.push({ kind: 'text', value: line.slice(last) });
  return pieces;
}

/**
 * Joins adjacent text runs and drops the empty ones.
 *
 * The fence handling and the per-line splitting both leave gaps, and a list of
 * fragments is not what the component should have to render. Merging here means the
 * component maps over a list where every element is a real thing.
 */
function mergeText(tokens: Token[]): Token[] {
  const merged: Token[] = [];
  for (const token of tokens) {
    // Whitespace is content and is never dropped. Dropping it is what made two lines
    // render as one: the break is a text token, and this used to discard it along with
    // any run of spaces, after which the merge joined the words either side together.
    //
    // An *empty* token is still dropped, because an empty message should produce no
    // tokens rather than one empty element for the component to render as nothing.
    if ((token.kind === 'text' || token.kind === 'code') && token.value === '') continue;
    const previous = merged.at(-1);
    if (token.kind === 'text' && previous?.kind === 'text') {
      previous.value += token.value;
      continue;
    }
    merged.push({ ...token });
  }
  // A run of separators with no content between them — the blank lines of a message
  // that is nothing but newlines — is kept, because that is what it said. Only a
  // completely empty result collapses, so the component can still ask "is there
  // anything here?" without receiving a phantom element.
  return merged;
}

/**
 * The host a link points at, for display.
 *
 * A link that cannot be parsed shows its own text rather than nothing, so a malformed
 * address is visible rather than silently dropped.
 */
export function linkHost(value: string): string {
  try {
    return new URL(value).hostname;
  } catch {
    return value;
  }
}
