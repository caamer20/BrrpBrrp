import assert from 'node:assert/strict';
import test from 'node:test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  assessSarif,
  selectAnalyses,
  findingIdentity,
  sourceTreeDigest
} from '../../scripts/check-security-alerts.mjs';

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
  assert.deepEqual(assessSarif(sarif('4.3')), { problems: [], findings: 1, reviewed: 0 });
  const clean = sarif('8.1');
  clean.runs[0].results = [];
  assert.deepEqual(assessSarif(clean), { problems: [], findings: 0, reviewed: 0 });
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

test('GitHub CodeQL query-pack extension rules are checked, including referenced findings', () => {
  const extension = sarif('8.1');
  extension.runs[0].tool.extensions = [
    { name: 'codeql/go-queries', rules: extension.runs[0].tool.driver.rules }
  ];
  delete extension.runs[0].tool.driver.rules;
  extension.runs[0].results = [];
  assert.deepEqual(assessSarif(extension), { problems: [], findings: 0, reviewed: 0 });
  extension.runs[0].results = [{ rule: { index: 0, toolComponent: { index: 0 } } }];
  assert.match(assessSarif(extension).problems.join(';'), /High security finding/);
});

test('unknown tool components and ambiguous rule identifiers fail closed', () => {
  const missing = sarif('8.1');
  missing.runs[0].results[0].rule = { toolComponent: { index: 10 } };
  assert.throws(() => assessSarif(missing), /unknown or ambiguous tool component/);
  const ambiguous = sarif('8.1');
  ambiguous.runs[0].tool.extensions = [{ rules: ambiguous.runs[0].tool.driver.rules }];
  assert.throws(() => assessSarif(ambiguous), /identifiable rule/);
});

test('component names, GUIDs and indices must identify the same unique component', () => {
  const response = sarif('4.3');
  const medium = response.runs[0].tool.driver.rules;
  const critical = structuredClone(sarif('9.8').runs[0].tool.driver.rules);
  delete response.runs[0].tool.driver.rules;
  response.runs[0].tool.extensions = [
    { name: 'queries', guid: 'one', rules: medium },
    { name: 'queries', guid: 'two', rules: critical }
  ];
  for (const toolComponent of [
    { name: 'queries' },
    { index: 0, name: 'different' },
    { index: 0, guid: 'two' }
  ]) {
    response.runs[0].results = [{ rule: { id: 'go/example', toolComponent } }];
    assert.throws(() => assessSarif(response), /unknown or ambiguous/);
  }
  response.runs[0].results = [
    { rule: { id: 'go/example', toolComponent: { index: 1, name: 'queries', guid: 'two' } } }
  ];
  assert.match(assessSarif(response).problems.join(';'), /Critical security finding/);
});

function reviewedFixture() {
  const response = sarif('8.1');
  const component = response.runs[0].tool.driver;
  component.semanticVersion = '2.27.2';
  const result = response.runs[0].results[0];
  result.partialFingerprints = { primaryLocationLineHash: '0123456789abcdef:1' };
  result.locations = [
    {
      physicalLocation: {
        artifactLocation: { uri: 'internal/example.go' },
        region: { startLine: 3, startColumn: 1 }
      }
    }
  ];
  result.codeFlows = [
    { threadFlows: [{ locations: [{ location: structuredClone(result.locations[0]) }] }] }
  ];
  const sourceDigest = 'c'.repeat(64);
  const record = {
    ...findingIdentity(result, component, result.ruleId),
    disposition: 'false-positive',
    reason: 'A test-only verified trust-boundary rationale for this exact result.',
    evidence: ['internal/example.go:3']
  };
  return {
    response,
    context: {
      sourceDigest,
      dispositions: { version: 1, source_digest: sourceDigest, findings: [record] }
    }
  };
}

test('a reviewed false positive must match the complete result and analyzed source identity', () => {
  const { response, context } = reviewedFixture();
  assert.deepEqual(assessSarif(response, context), { problems: [], findings: 1, reviewed: 1 });
});

test('changed analyzed source invalidates reviewed findings without refreshing them', () => {
  const { response, context } = reviewedFixture();
  assert.throws(
    () => assessSarif(response, { ...context, sourceDigest: 'd'.repeat(64) }),
    /exact analyzed source/
  );
});

test('moved, replaced, different-pack and different-flow findings remain blocked', () => {
  for (const mutate of [
    (s) => {
      s.runs[0].results[0].locations[0].physicalLocation.region.startLine++;
    },
    (s) => {
      s.runs[0].results[0].partialFingerprints.primaryLocationLineHash = 'other';
    },
    (s) => {
      s.runs[0].tool.driver.semanticVersion = 'next';
    },
    (s) => {
      s.runs[0].results[0].codeFlows[0].threadFlows[0].locations[0].location.physicalLocation.region
        .startLine++;
    },
    (s) => {
      s.runs[0].tool.driver.rules[0].id = 'go/unknown';
      s.runs[0].results[0].ruleId = 'go/unknown';
    }
  ]) {
    const { response, context } = reviewedFixture();
    mutate(response);
    assert.ok(assessSarif(response, context).problems.length);
  }
});

test('duplicate results cannot consume the same reviewed exception twice', () => {
  const { response, context } = reviewedFixture();
  response.runs[0].results.push(structuredClone(response.runs[0].results[0]));
  assert.throws(() => assessSarif(response, context), /Duplicate finding/);
});

test('invalid, duplicated or misleading review records fail closed', () => {
  for (const mutate of [
    (c) => {
      c.dispositions.version = 2;
    },
    (c) => {
      c.dispositions.findings[0].disposition = 'accepted-risk';
    },
    (c) => {
      c.dispositions.findings[0].reason = '';
    },
    (c) => {
      c.dispositions.findings[0].component = 1;
    },
    (c) => {
      c.dispositions.findings.push(structuredClone(c.dispositions.findings[0]));
    },
    (c) => {
      c.dispositions.findings[0].path = 'different.go';
    }
  ]) {
    const { response, context } = reviewedFixture();
    mutate(context);
    assert.throws(() => assessSarif(response, context), /Invalid|disagree/);
  }
});

test('source binding covers embedded SQL and workflow configuration at the exact commit', () => {
  const root = mkdtempSync(join(tmpdir(), 'brrp-source-review-'));
  const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
  const commit = () => {
    git('add', '.');
    git(
      '-c',
      'user.name=Source review test',
      '-c',
      'user.email=test@example.invalid',
      'commit',
      '-qm',
      'fixture'
    );
    return git('rev-parse', 'HEAD');
  };
  try {
    git('init', '-q', '--object-format=sha1');
    for (const folder of ['internal/db', '.github/workflows', 'scripts'])
      mkdirSync(join(root, folder), { recursive: true });
    writeFileSync(join(root, 'internal/db/schema.sql'), 'CREATE TABLE fixture(id TEXT);');
    writeFileSync(join(root, '.github/workflows/security.yml'), 'name: CodeQL');
    writeFileSync(join(root, 'scripts/codeql-reviewed-findings.json'), '{}');
    const first = commit();
    const original = sourceTreeDigest(first, root);
    writeFileSync(join(root, 'scripts/codeql-reviewed-findings.json'), '{"review":"updated"}');
    assert.equal(sourceTreeDigest(commit(), root), original);
    writeFileSync(join(root, 'internal/db/schema.sql'), 'CREATE TABLE changed(id TEXT);');
    const sql = commit();
    const changed = sourceTreeDigest(sql, root);
    assert.notEqual(changed, original);
    assert.equal(sourceTreeDigest(first, root), original);
    writeFileSync(
      join(root, '.github/workflows/security.yml'),
      'name: Different scanner configuration'
    );
    assert.notEqual(sourceTreeDigest(commit(), root), changed);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('URI bases participate in identity and unresolved or cyclic bases fail closed', () => {
  const { response, context } = reviewedFixture();
  const run = response.runs[0];
  run.results[0].locations[0].physicalLocation.artifactLocation.uriBaseId = 'SOURCE';
  run.originalUriBaseIds = { SOURCE: { uri: 'file:///different/' } };
  assert.ok(assessSarif(response, context).problems.length);
  delete run.originalUriBaseIds.SOURCE;
  assert.throws(() => assessSarif(response, context), /URI base/);
  run.originalUriBaseIds.SOURCE = { uri: 'file:///different/', uriBaseId: 'SOURCE' };
  assert.throws(() => assessSarif(response, context), /URI base/);
});
