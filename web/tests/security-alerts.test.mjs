import assert from 'node:assert/strict';
import test from 'node:test';
import { assessSarif, selectAnalyses } from '../../scripts/check-security-alerts.mjs';

const sha = 'a'.repeat(40);
const ref = 'refs/heads/main';
const analyses = ['go', 'javascript-typescript'].map((language, index) => ({
  id: index + 1,
  commit_sha: sha,
  ref,
  category: `/language:${language}/`,
  tool: { name: 'CodeQL' },
  rules_count: 100,
  error: ''
}));
const sarif = (score, extra = {}) => ({
  version: '2.1.0',
  runs: [
    {
      tool: {
        driver: {
          name: 'CodeQL',
          rules: [
            { id: 'go/example', properties: { 'security-severity': score, tags: ['security'] } }
          ]
        }
      },
      results: [{ ruleId: 'go/example', ruleIndex: 0, ...extra }]
    }
  ]
});

test('clean exact-revision analyses and lower-severity findings pass', () => {
  assert.deepEqual(selectAnalyses(analyses, sha, ref), analyses);
  assert.deepEqual(assessSarif(sarif('4.3')), { problems: [], findings: 1 });
  const clean = sarif('8.1');
  clean.runs[0].results = [];
  assert.deepEqual(assessSarif(clean), { problems: [], findings: 0 });
});
test('high and critical results block even when suppressed or dismissed elsewhere', () => {
  for (const score of ['7', '8.1', '9', '10'])
    assert.match(
      assessSarif(sarif(score, { suppressions: [{ status: 'accepted' }] })).problems.join(';'),
      /security finding/
    );
});
test('a later clean commit cannot mask findings in the candidate analysis', () => {
  const later = analyses.map((x) => ({ ...x, id: x.id + 10, commit_sha: 'b'.repeat(40) }));
  assert.deepEqual(selectAnalyses([...later, ...analyses], sha, ref), analyses);
  assert.ok(assessSarif(sarif('8.1')).problems.length);
});
test('missing, failed, wrong-source, wrong-tool or lookalike-category analyses fail closed', () => {
  for (const entries of [
    [],
    analyses.slice(0, 1),
    analyses.map((x) => ({ ...x, commit_sha: 'b'.repeat(40) })),
    analyses.map((x) => ({ ...x, error: 'failed' })),
    analyses.map((x) => ({ ...x, tool: { name: 'Other' } })),
    analyses.map((x) => ({ ...x, category: `${x.category}untrusted` })),
    analyses.map((x) => ({ ...x, rules_count: 0 }))
  ])
    assert.throws(() => selectAnalyses(entries, sha, ref), /No completed/);
});
test('malformed SARIF, unknown rules and security severity fail closed', () => {
  assert.throws(() => assessSarif({ runs: [] }), /SARIF/);
  const unknown = sarif('8');
  unknown.runs[0].results[0].ruleId = 'missing';
  assert.throws(() => assessSarif(unknown), /identifiable rule/);
  for (const score of [undefined, '', 'unknown', '-1', '11'])
    assert.ok(assessSarif(sarif(score)).problems.length);
});
