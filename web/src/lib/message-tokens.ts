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
  | { kind: 'link'; value: string };

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

  for (const [index, line] of lines.entries()) {
    const fence = /^\s*(```+|~~~+)\s*([A-Za-z0-9+#-]*)\s*$/.exec(line);
    if (fence) {
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
  // snippet as prose, with its mentions and links live.
  flushCode();
  return mergeText(tokens);
}

/** Splits one line of ordinary text into mentions, links and plain runs. */
function tokenizeLine(line: string): Token[] {
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
