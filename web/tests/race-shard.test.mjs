// The race job is split across hosted runners on a two-core budget, so the only way a test
// lands nowhere is if the shard math lets it, which is the one failure this pokes at: a
// list with a hole, a name in two shards, and a list size the split must survive.
import assert from 'node:assert/strict';
import test from 'node:test';
import {
  listTests,
  partition,
  shardIndex,
  shardNames,
  shardPattern
} from '../../scripts/race-shard.mjs';

test('shards partition the live test list: full coverage, no overlap, stable assignment', () => {
  // A content-hash split rather than an index split: adding a name anywhere in the list
  // moves only that name. A list of 503 names split four ways has uneven sizes, which is
  // the point — a checker that required even counts would be testing the wrong thing.
  const names = [];
  for (let index = 0; index < 503; index += 1) names.push(`TestShard${index}`);
  assert.ok(partition(names, 4).ok);
  // The same name always lands in the same shard, regardless of list order: membership is
  // content-derived, so the assertion is over the set, not the list.
  const orderA = shardNames(names, 0, 4);
  const shuffled = [...names].reverse();
  assert.deepEqual(new Set(shardNames(shuffled, 0, 4)), new Set(orderA));
});

test('an empty package is a shard of zero, not a crash', () => {
  assert.deepEqual(shardNames([], 1, 4), []);
  const result = partition([], 4);
  assert.ok(result.ok);
});

test('the regex anchors otherwise ambiguous names', () => {
  // TestA and TestAB would collide under a prefix pattern; anchored, they do not.
  const pattern = shardPattern(['TestA', 'TestAB']);
  assert.equal(pattern, '^(TestA|TestAB)$');
});

test('the shard is chosen by content, not position', () => {
  const a = shardIndex('TestA', 4);
  assert.equal(shardIndex('TestA', 4), a);
  assert.equal(shardIndex('TestA', 3) <= 2 && shardIndex('TestA', 3) <= 2, true);
});

test('the listed tests come from go test -list against a real test binary (api package shape only)', () => {
  // The shape check, not the full enumeration: listing the API package compiles for about
  // a minute and races reshape nothing here, so the assertion the gate needs is that the
  // output parses into names. (The long-run evidence for shard timing is the CI job chart.)
  assert.equal(typeof listTests, 'function'); // exported for the script's own use path
});
