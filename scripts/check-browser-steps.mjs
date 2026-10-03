#!/usr/bin/env node
// Audit of the CI browser steps, and proof that a failed browser run fails the job.
//
// The defect this exists for: both browser steps piped `npm run test:e2e` into
// `tee` and relied on the step's own shell to decide the exit status. GitHub's
// unspecified Linux shell is `bash -e {0}`, which has no `pipefail`, so the
// pipeline's status was `tee`'s status. A failing suite that still printed the
// text the step greps for (`[directs]`) left the required job green, and because
// `packages` needs `browser`, the promotion gate passed without the journeys
// having run. Reading the YAML and the grep cannot catch this: the grep only
// proves project text appeared, which a failing run also produces.
//
// So this checks two things, and the second is the actual proof:
//
//   1. every browser step declares a shell whose documented command line
//      includes `pipefail`, and the diagnostics are collected by a step that
//      runs even when the test step fails (so strict shell handling cannot
//      skip the artifact collection);
//   2. each browser step's literal `run:` body is executed here, under the exact
//      command line GitHub documents for that shell, against a stub `npm` that
//      fails while printing `[directs]`. Each must exit non-zero. A second run
//      with a stub that succeeds must exit zero, so a harness that simply
//      refuses everything is not mistaken for a passing check.
//
// Usage: node scripts/check-browser-steps.mjs [--self-test] [--workflow <path>]

import { spawnSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptPath = fileURLToPath(import.meta.url);
const repository = resolve(dirname(scriptPath), '..');

// The shells GitHub documents for `jobs.<job_id>.steps[*].shell`. Only the ones
// a Linux step can use are modelled with the command line that actually runs;
// anything else is a step this check refuses to reason about rather than one it
// silently passes. See the workflow-syntax reference:
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax
const documentedShells = {
  '(unspecified on Linux)': 'bash -e {0}',
  bash: 'bash --noprofile --norc -eo pipefail {0}',
  sh: 'sh -e {0}'
};
const supportsPipefail = (command) => command.includes('pipefail');
const isPortableShell = (name) => Object.hasOwn(documentedShells, name);

// `fail` records into the audit that is currently running. The self-test audits
// a defective fixture on purpose, and its problems are that fixture's findings
// rather than defects in this repository's workflow, so each audit collects into
// its own list and only the real workflow's list decides the exit status.
let current = null;

function fail(message) {
  console.error(`browser-step audit: ${message}`);
  current?.push(message);
}

// --- Workflow parsing ------------------------------------------------------
//
// The workflow is not parsed as general YAML: this reads the step list the file
// actually uses (a `- name:` entry with optional `shell:`, `working-directory:`
// and a `run:` literal block). A shape this reader does not recognize is a
// failure, not a silent skip, so the audit cannot pass by finding nothing.

function parseSteps(source) {
  const steps = [];
  let step;
  let blockIndent = 0;
  let inBlock = false;
  for (const line of source.split('\n')) {
    const entry = /^( *)- (name|uses|run|id):(.*)$/.exec(line);
    if (entry) {
      const label = entry[3].trim();
      // A `- uses:` entry is an action, not a name. It is still a step, and
      // treating it as anything else would hide the artifact upload from the
      // audit that depends on it.
      step = {
        name: label || `(${entry[2]} step)`,
        action: entry[2],
        uses: entry[2] === 'uses' ? label : undefined,
        indent: entry[1].length,
        lines: []
      };
      steps.push(step);
      inBlock = false;
      // `- run: <inline script>` carries its body on the same line.
      if (entry[2] === 'run' && !/^[|>][-+]?\d*$/.test(label)) step.run = label;
      continue;
    }
    if (!step) continue;
    const property = /^( *)([A-Za-z-]+):(.*)$/.exec(line);
    if (property && property[1].length > step.indent) {
      const indent = property[1].length;
      const key = property[2];
      const value = property[3];
      if ((key === 'run' || key === 'path') && /^[|>][-+]?\d*$/.test(value.trim())) {
        inBlock = true;
        blockIndent = indent;
        step.blockKey = key;
        continue;
      }
      inBlock = false;
      if (key === 'shell') step.shell = value.trim();
      if (key === 'working-directory') step.workingDirectory = value.trim();
      if (key === 'if') step.ifCondition = value.trim();
      if (key === 'uses') step.uses = value.trim();
      if (key === 'name') step.label = value.trim();
      continue;
    }
    if (!inBlock) continue;
    // Inside the block scalar: dedent to the script the runner writes to disk.
    if (line.trim() === '') {
      step.lines.push('');
      continue;
    }
    if (line.length - line.trimStart().length <= blockIndent) {
      inBlock = false;
      continue;
    }
    step.lines.push(line.slice(blockIndent + 2));
  }
  for (const entry of steps) {
    if (!entry.lines.length) continue;
    const body = entry.lines.join('\n');
    if (entry.blockKey === 'path') entry.paths = body;
    else entry.run = body;
  }
  return steps;
}

// Each audit collects its own findings, so auditing the deliberately defective
// self-test fixture cannot leave its problems behind in this repository's result.
function auditWorkflow(path, label) {
  const found = [];
  current = found;
  try {
    return { diagnosed: audit(path, label), problems: found };
  } finally {
    current = null;
  }
}

function audit(path, label) {
  const steps = parseSteps(readFileSync(path, 'utf8'));
  if (steps.length === 0) fail(`${label} declares no steps, so nothing was audited`);

  const browserSteps = steps.filter((step) => step.run && /test:e2e/.test(step.run));
  if (browserSteps.length === 0) fail(`${label} has no step that runs the browser suite`);

  const diagnosed = [];
  for (const step of browserSteps) {
    const shell = step.shell ?? '(unspecified on Linux)';
    if (!isPortableShell(shell)) {
      fail(`step "${step.name}" declares shell "${shell}", which this audit cannot execute`);
      continue;
    }
    if (!supportsPipefail(documentedShells[shell])) {
      fail(
        `step "${step.name}" runs the browser suite with shell "${shell}" (${documentedShells[shell]}), which reports the status of the last command in the pipeline; a failing \`npm run test:e2e | tee\` would pass. Declare \`shell: bash\`.`
      );
    }
    diagnosed.push({ step, shell, command: documentedShells[shell] });
  }

  const uploads = steps.filter((step) => /^actions\/upload-artifact@/.test(step.uses || ''));
  if (uploads.length === 0) fail(`${label} uploads no browser diagnostics`);
  const alwaysUploads = uploads.some((step) => step.ifCondition === 'always()');
  if (uploads.length && !alwaysUploads)
    fail(
      `${label} uploads browser diagnostics in a step that is skipped on failure; the failing run's logs, traces and screenshots must be uploaded with \`if: always()\``
    );
  // With pipefail the run body stops at the failing pipeline, so a `mv` after it
  // never happens and the failed run's artifacts stay in the original directory.
  // Uploading only the renamed copies would discard exactly the traces,
  // screenshots and logs a failure produces.
  const uploadsOriginals = uploads.some((step) => /(test-results|report-[\w-]+)\//.test(step.paths || ''));
  if (alwaysUploads && !uploadsOriginals)
    fail(
      `${label} collects only the renamed artifact paths; a failed run leaves its traces and screenshots in the original directories, which must be uploaded too`
    );

  // The promotion gate is only a gate if it is still downstream of the browser
  // result. A failing browser step that nothing depends on is not a gate.
  const source = readFileSync(path, 'utf8');
  const packages = /needs:\s*\[([^\]]*)\]/.exec(source);
  if (!packages || !/browser/.test(packages[1]))
    fail(`${label} does not make package production depend on the browser job`);

  return diagnosed;
}

// --- Execution proof --------------------------------------------------------

// The stub stands in for `npm run test:e2e`. It prints the same marker the step
// greps for and exits with the requested code, so the only thing the step can be
// reacting to is the test process's own status.
function npmStub(workspace, exitCode) {
  const bin = join(workspace, 'bin');
  mkdirSync(bin, { recursive: true });
  const stub = join(bin, 'npm');
  writeFileSync(
    stub,
    `#!/bin/sh\nprintf '%s\\n' '> brrpbrrp@0.0.0 test:e2e' 'Running 1 test using 1 worker' '[directs] > example.spec.mjs:10:1 passed (1.2s)' '[voice-firefox] > example.spec.mjs:10:1 passed (1.2s)'\nexit ${exitCode}\n`
  );
  chmodSync(stub, 0o755);
  return bin;
}

// GitHub writes the step body to a file and runs it as `bash ... <file>`. The
// runner also sets the step's working directory, so the body runs from there.
function runStep(entry, exitCode) {
  const { step, command } = entry;
  const workspace = mkdtempSync(join(tmpdir(), 'brrpbrrp-browser-step-'));
  try {
    const working = join(workspace, step.workingDirectory || '.');
    mkdirSync(working, { recursive: true });
    const bin = npmStub(workspace, exitCode);
    const file = join(workspace, 'step.sh');
    writeFileSync(file, step.run);
    const result = spawnSync('/bin/sh', ['-c', command.replace('{0}', JSON.stringify(file))], {
      cwd: working,
      encoding: 'utf8',
      env: { ...process.env, PATH: `${bin}:${process.env.PATH}` }
    });
    if (result.error) return { status: 1, output: String(result.error), log: '' };
    // The log the step writes is read from the step's own body rather than from a list
    // kept here: a second engine's step writes its own file, and an audit that only knew
    // about the two logs it was written beside would report the new step as one that
    // never ran anything.
    const logName = /tee\s+(\S+\.log)/.exec(step.run)?.[1];
    const log = logName ? readFileSync(join(working, logName), 'utf8') : '';
    return { status: result.status, output: (result.stdout || '') + (result.stderr || ''), log };
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
}

function proveFailurePropagates(diagnosed, label, found) {
  const record = (message) => {
    console.error(`browser-step audit: ${message}`);
    found.push(message);
  };
  for (const entry of diagnosed) {
    const failing = runStep(entry, 1);
    if (failing.status === 0)
      record(
        `${label}: step "${entry.step.name}" exited 0 when the test command failed while printing [directs] — a failing browser suite would pass this job`
      );
    if (!failing.log.includes('[directs]'))
      record(
        `${label}: step "${entry.step.name}" produced no log containing [directs], so the control did not exercise the step's own grep`
      );
    const passing = runStep(entry, 0);
    if (passing.status !== 0)
      record(
        `${label}: step "${entry.step.name}" exited ${passing.status} when the test command succeeded: ${passing.output.trim()}`
      );
  }
}

// --- Entry point ------------------------------------------------------------

const selfTest = process.argv.includes('--self-test');
const override = process.argv.indexOf('--workflow');
const workflowPath =
  override === -1 ? join(repository, '.github', 'workflows', 'check.yml') : process.argv[override + 1];

// This audit executes POSIX shell command lines and writes a POSIX stub into
// PATH, so it is only meaningful where those exist; it runs in the Linux job that
// owns the browser steps. Refusing to pretend elsewhere is deliberate: a platform
// that cannot run the control must fail rather than skip and look green
// (completion review 10, R10-05).
if (process.platform === 'win32') {
  console.error(
    'browser-step audit: this check executes POSIX shell command lines and cannot run on Windows.'
  );
  console.error(
    'It belongs in the Linux browser job, where a Windows run cannot prove the pipeline failure property.'
  );
  process.exit(1);
}

const real = auditWorkflow(workflowPath, 'the browser steps');
const problems = real.problems;
proveFailurePropagates(real.diagnosed, 'the browser steps', problems);

if (selfTest) {
  // The audit is only worth running if it still catches the defect it was
  // written for, so the pre-fix form is run through the same code path: the
  // pipeline masks a failure there, and the audit must refuse it. The fixture's
  // own findings are asserted on, not reported against this repository.
  const workspace = mkdtempSync(join(tmpdir(), 'brrpbrrp-browser-audit-'));
  const fixtureProblems = [];
  try {
    const defective = join(workspace, 'defective.yml');
    writeFileSync(
      defective,
      `name: Check
jobs:
  browser:
    steps:
      - name: Browser journeys (private conversations enabled)
        run: |
          npm run test:e2e 2>&1 | tee browser-private.log
          test -d test-results && mv test-results test-results-private || true
          grep -q '\\[directs\\]' browser-private.log || exit 1
      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        if: failure()
  packages:
    needs: [test, browser, docker, recovery, rollback]
    steps:
      - run: node scripts/release.go
`
    );
    const result = auditWorkflow(defective, 'the pre-fix fixture');
    const complain = (message) => {
      console.error(`self-test: ${message}`);
      fixtureProblems.push(message);
    };
    if (result.problems.length === 0)
      complain('the pre-fix fixture was accepted, so the audit no longer detects R9-01');
    if (
      !result.problems.some((message) => /Declare `shell: bash`/.test(message))
    )
      complain('the pre-fix fixture was not rejected for its missing pipefail shell');
    if (result.diagnosed.length !== 1)
      complain(`expected one browser step in the fixture, found ${result.diagnosed.length}`);
    if (result.diagnosed.length === 1) {
      const masked = runStep(result.diagnosed[0], 1);
      if (masked.status !== 0)
        complain(
          `the pre-fix fixture did not mask the failure (status ${masked.status}); it no longer reproduces R9-01, so the control proves nothing`
        );
      if (!masked.log.includes('[directs]'))
        complain('the pre-fix fixture did not print [directs], so the masking is not the R9-01 case');
    }
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
  if (fixtureProblems.length) {
    console.error(`Browser-step audit self-test failed (${fixtureProblems.length} problem(s)).`);
    process.exit(1);
  }
  console.log('Browser-step audit self-test passed: the pre-fix workflow was rejected for masking a failing suite, and the mask was confirmed to hide a failure that printed [directs].');
}

if (problems.length) {
  console.error(`Browser-step audit failed (${problems.length} problem(s)).`);
  process.exit(1);
}
console.log(
  `Browser-step audit passed: ${real.diagnosed.length} browser step(s) run under a shell that reports the test process's own status, each verified to fail on a failing suite that still prints [directs] and to pass on a succeeding one, with diagnostics collected even when the test step fails.`
);
console.log(
  'This is a static and stubbed check of the workflow definition; it is not a browser run. A hosted execution of the workflow is still required.'
);
