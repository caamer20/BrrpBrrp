// The acceptance matrix is only a gate if it fails when it should. These tests point it at
// small trees of their own: one healthy, one missing a scenario, one naming a check that is
// not there, and one whose scenario says nothing at all. Without them the script's exit
// code is a claim rather than a check — and a gate that cannot fail is worse than no gate,
// because it is believed.
import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import {
  catalogFromPlan,
  checkExists,
  checkMatrix,
  main
} from '../../scripts/check-acceptance-matrix.mjs';

const PLAN = `# Plan

### 11.2 Mandatory scenario catalog

| Test ID | Scenario and pass condition | Packages / requirements |
| --- | --- | --- |
| T-ONE-01 | The first thing has to work. | WP-01 |
| T-TWO-01 | The second thing has to work too. | WP-02 |

### 11.3 Browser and physical-device matrix
`;

/** A tree with a plan, a matrix and the four kinds of check the script looks for. */
function tree({ matrix }) {
  const root = mkdtempSync(path.join(tmpdir(), 'brrp-matrix-'));
  mkdirSync(path.join(root, 'docs'), { recursive: true });
  mkdirSync(path.join(root, 'planning'), { recursive: true });
  mkdirSync(path.join(root, 'internal/api'), { recursive: true });
  mkdirSync(path.join(root, 'web/e2e'), { recursive: true });
  mkdirSync(path.join(root, 'web/tests'), { recursive: true });
  mkdirSync(path.join(root, 'scripts'), { recursive: true });
  writeFileSync(path.join(root, 'docs/v1-implementation-plan.md'), PLAN);
  writeFileSync(
    path.join(root, 'internal/api/example_test.go'),
    'func TestOneThing(t *testing.T) {}\n'
  );
  writeFileSync(
    path.join(root, 'web/e2e/some.spec.mjs'),
    "test('a journey that exists', async () => {});\n"
  );
  writeFileSync(
    path.join(root, 'web/tests/unit.test.mjs'),
    "test('a unit that exists', () => {});\n"
  );
  writeFileSync(path.join(root, 'scripts/rehearsal.mjs'), '// a rehearsal\n');
  writeFileSync(
    path.join(root, 'planning/acceptance-matrix.json'),
    JSON.stringify(matrix, null, 2)
  );
  return root;
}

const healthy = {
  scenarios: {
    'T-ONE-01': {
      checks: [
        'api::TestOneThing',
        'some.spec.mjs::a journey that exists',
        'web/tests/unit.test.mjs::a unit that exists',
        'script:scripts/rehearsal.mjs'
      ]
    },
    'T-TWO-01': {
      checks: ['api::TestOneThing'],
      owed: ['Phase D: the second thing needs a second host, which this machine is not.']
    }
  }
};

test('the catalog is read from the plan, including ids that carry digits', () => {
  const ids = catalogFromPlan(PLAN);
  assert.deepEqual([...ids].sort(), ['T-ONE-01', 'T-TWO-01']);
  // The real catalog has T-A11Y-01 in it, and an [A-Z]+ pattern skipped that row silently:
  // the scenario would have been reported as "in the matrix and not in the plan".
  const withDigits = PLAN.replace('T-TWO-01', 'T-A11Y-01');
  assert.ok(catalogFromPlan(withDigits).has('T-A11Y-01'));
});

test('every kind of check is looked up where it lives', () => {
  const root = tree({ matrix: healthy });
  assert.equal(checkExists('api::TestOneThing', root), null);
  assert.equal(checkExists('some.spec.mjs::a journey that exists', root), null);
  assert.equal(checkExists('web/tests/unit.test.mjs::a unit that exists', root), null);
  assert.equal(checkExists('script:scripts/rehearsal.mjs', root), null);
  assert.match(checkExists('api::TestMissing', root), /no such test/);
  assert.match(checkExists('some.spec.mjs::a journey that is gone', root), /no such journey/);
  assert.match(checkExists('web/tests/unit.test.mjs::a unit that is gone', root), /no such test/);
  assert.match(checkExists('script:scripts/gone.mjs', root), /no such script/);
  assert.match(checkExists('api::TestOne', root), /no such test/);
  assert.match(checkExists('nonsense', root), /must be "kind::name"/);
});

test('a healthy tree passes, and its owed items are counted rather than hidden', async () => {
  const root = tree({ matrix: healthy });
  const result = checkMatrix({
    planText: PLAN,
    matrix: healthy,
    base: root
  });
  assert.deepEqual(result.problems, []);
  assert.equal(result.scenarios, 2);
  assert.equal(result.checks, 5);
  assert.equal(result.owed, 1);
  assert.deepEqual(result.owedScenarios, ['T-TWO-01']);
  assert.equal(await main([], root), 0);
  assert.equal(await main(['--json'], root), 0);
});

test('a scenario the plan declares and the matrix omits fails the build', () => {
  const matrix = { scenarios: { 'T-ONE-01': { checks: ['api::TestOneThing'] } } };
  const result = checkMatrix({ planText: PLAN, matrix, base: tree({ matrix }) });
  assert.ok(
    result.problems.some((problem) => problem.includes('T-TWO-01 is in the plan and absent'))
  );
});

test('a check that no longer exists fails the build, naming the scenario', () => {
  const matrix = {
    scenarios: {
      'T-ONE-01': { checks: ['api::TestRemoved'] },
      'T-TWO-01': { checks: ['api::TestOneThing'] }
    }
  };
  const result = checkMatrix({ planText: PLAN, matrix, base: tree({ matrix }) });
  assert.equal(result.problems.length, 1);
  assert.match(result.problems[0], /T-ONE-01: no such test in internal\/api: TestRemoved/);
});

test('a scenario with neither a check nor an owed item is refused', () => {
  const matrix = {
    scenarios: { 'T-ONE-01': { checks: [] }, 'T-TWO-01': { checks: ['api::TestOneThing'] } }
  };
  const result = checkMatrix({ planText: PLAN, matrix, base: tree({ matrix }) });
  assert.ok(result.problems.some((problem) => problem.includes('names no check and nothing owed')));
});

test('an owed item has to say what is missing', () => {
  const matrix = {
    scenarios: {
      'T-ONE-01': { checks: [], owed: ['later'] },
      'T-TWO-01': { checks: ['api::TestOneThing'] }
    }
  };
  const result = checkMatrix({ planText: PLAN, matrix, base: tree({ matrix }) });
  assert.ok(result.problems.some((problem) => problem.includes('must say what is missing')));
});

test('an id the plan does not declare is refused, so a typo cannot sit here unread', () => {
  const matrix = {
    scenarios: {
      'T-ONE-01': { checks: ['api::TestOneThing'] },
      'T-TYPO-01': { checks: ['api::TestOneThing'] }
    }
  };
  const result = checkMatrix({ planText: PLAN, matrix, base: tree({ matrix }) });
  assert.ok(
    result.problems.some((problem) =>
      problem.includes('T-TYPO-01 is in the matrix and not in the plan')
    )
  );
});
