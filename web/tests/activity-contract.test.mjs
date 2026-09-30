import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The activity list and its preferences are private metadata, so the properties
// worth pinning are the ones that keep a late response from one account out of the
// next account's panel. This asserts the contract the controller implements by
// reading its source, because the behaviour is a sequence of awaits and the case
// that matters — a response resolving after the account changed — is not reachable
// from a unit test without driving the real store.

const source = readFileSync(new URL('../src/lib/activity.svelte.ts', import.meta.url), 'utf8');

test('every activity result is fenced on the account and the generation', () => {
  // A list, an acknowledgment and a preference change all go through the same
  // admission, and all three would otherwise paint the previous account's private
  // metadata into the new one's panel.
  for (const fn of ['async load(', 'async acknowledge(', 'async setPreference(']) {
    const at = source.indexOf(fn);
    assert.notEqual(at, -1, `${fn} is missing`);
    const body = source.slice(at, at + 600);
    assert.match(body, /this\.admitted\(request\)/, `${fn} does not check admission`);
  }
  // And the result of the fetch is re-checked, not just the start of it.
  const at = source.indexOf('async load(');
  assert.match(
    source.slice(at, at + 700),
    /await api<ActivityPage>[\s\S]{0,120}this\.admitted\(request\)/,
    'load does not re-check admission after awaiting'
  );
});

test('an account or authority change clears the list rather than leaving it', () => {
  assert.match(
    source,
    /reset\(\)\s*\{[\s\S]{0,200}this\.entries = \[\]/,
    'reset does not clear the entries'
  );
  assert.match(
    source,
    /reset\(\)\s*\{[\s\S]{0,400}this\.preferences = \[\]/,
    'reset does not clear the preferences'
  );
  // The version bump is what makes a late response stale, so it is part of clearing
  // rather than a separate concern.
  assert.match(source, /reset\(\)\s*\{\s*this\.version\+\+/, 'reset does not advance the version');
});

test('acknowledgment is a separate call, not a side effect of reading', () => {
  // A read that fails halfway must not have consumed the acknowledgment, so the
  // list load cannot acknowledge on the way through.
  // Bounded to the first two-space method boundary that follows `load`, so the
  // slice cannot sweep into the `acknowledge` method that comes after it.
  const loadStart = source.indexOf('async load(');
  const nextMethod = source.indexOf('\n  async ', loadStart + 1);
  const loadBody = source.slice(loadStart, nextMethod < 0 ? source.length : nextMethod);
  assert.doesNotMatch(loadBody, /this\.acknowledge\(/, 'loading the list acknowledges it');
  assert.doesNotMatch(
    loadBody,
    /\/me\/activity\/acknowledge/,
    'loading the list posts an acknowledgment'
  );
  // It is keyed by the last row the person actually saw, so a row that arrives
  // between the list and the acknowledgment is not acknowledged by accident.
  assert.match(
    source,
    /acknowledge\(\)[\s\S]{0,900}this\.entries\[this\.entries\.length - 1\]\.id/,
    'acknowledgment is not positioned at the last row shown'
  );
});

test('loading older appends rather than replacing the list', () => {
  // Resetting on "load older" would lose everything already read and re-issue a
  // cursor, so the same first page would come back.
  assert.match(
    source,
    /this\.entries = append \? \[\.\.\.this\.entries, \.\.\.page\.activity\] : page\.activity/,
    'load does not append when asked for older rows'
  );
});

test('a preference change writes through and adopts the server answer', () => {
  // A local optimistic flip would leave the panel claiming a setting the server
  // refused, which for an interruption preference means a member believes they are
  // quiet and are not.
  assert.match(
    source,
    /this\.preferences = this\.preferences\.map\(\(entry\)\s*=>\s*entry\.kind === result\.kind \? result : entry/,
    'a preference change does not adopt the server answer'
  );
});

test('the list says what happened without saying what was written', () => {
  // A lock screen and a notification list are the two places private text leaks
  // most easily, so the panel describes the event and never its content.
  const panel = readFileSync(
    new URL('../src/lib/components/ActivityPanel.svelte', import.meta.url),
    'utf8'
  );
  const describe = panel.slice(panel.indexOf('function describe('), panel.indexOf('</script>'));
  assert.doesNotMatch(
    describe,
    /entry\.(content|body|text|snapshot)/,
    'the description reads message content'
  );
  assert.match(describe, /message request/, 'the request kind is not described');
  assert.match(describe, /group you are in/, 'the group kind is not described');
});

test('the panel states that the conversation is the record', () => {
  // A bounded list that silently dropped rows would read as lost messages, so the
  // limit and the retention are stated where a member will read them.
  const panel = readFileSync(
    new URL('../src/lib/components/ActivityPanel.svelte', import.meta.url),
    'utf8'
  );
  assert.match(panel, /30 days/, 'the retention is not stated');
  assert.match(panel, /2,000/, 'the bound is not stated');
  assert.match(
    panel,
    /conversation itself is always the full record/i,
    'the conversation is not named as authoritative'
  );
});
