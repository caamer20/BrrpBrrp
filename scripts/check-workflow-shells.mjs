#!/usr/bin/env node
// Audit of the shell each CI step body runs under, on a job that can land on Windows.
//
// The defect this exists for: the `test` job runs on a three-OS matrix and several of its
// steps are written in Bash — a `$(…)` capture, an `if [ -n … ]`, a `for … do … done`.
// GitHub's default shell on a Windows runner is PowerShell, which cannot parse them, and
// which also does not stop at the first failing native command in a two-line body: a
// failing self-test followed by a passing check leaves the step green (review finding
// F-04, W-09). The fix is `shell: bash` — declared for the job or for the step — and this
// audit is what keeps a step added later from quietly going back to the default.
//
// It reads the workflow as text. It does not run a step and it is not a hosted run: a
// Windows runner executing the job is still the evidence that the job passes there.
//
// Usage: node scripts/check-workflow-shells.mjs [--self-test] [--workflow <path>]

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// Splits `jobs:` into its jobs, each with the lines that belong to it.
function parseJobs(source) {
  const lines = source.split(/\r?\n/);
  const jobs = [];
  let inJobs = false;
  let current = null;
  for (const [index, line] of lines.entries()) {
    if (/^jobs:\s*$/.test(line)) {
      inJobs = true;
      continue;
    }
    if (!inJobs) continue;
    if (/^\S/.test(line)) break; // another top-level key ends the jobs map
    const header = /^ {2}([A-Za-z0-9_-]+):\s*$/.exec(line);
    if (header) {
      current = { name: header[1], lines: [], start: index + 1 };
      jobs.push(current);
      continue;
    }
    if (current) current.lines.push({ text: line, number: index + 1 });
  }
  return jobs;
}

// A job can land on Windows when its runner, or the matrix that names its runner, does.
function runsOnWindows(job) {
  return job.lines.some(({ text }) => /^\s+(runs-on|os):.*windows-/.test(text));
}

// `defaults: run: shell: bash` at the job's own level.
function defaultsToBash(job) {
  for (const [index, { text }] of job.lines.entries()) {
    if (!/^ {4}defaults:\s*$/.test(text)) continue;
    let inRun = false;
    for (const { text: next } of job.lines.slice(index + 1)) {
      if (/^ {0,4}\S/.test(next)) break;
      if (/^ {6}run:\s*$/.test(next)) inRun = true;
      else if (/^ {6}\S/.test(next)) inRun = false;
      else if (inRun && /^ {8}shell:\s*bash\s*$/.test(next)) return true;
    }
  }
  return false;
}

// The job's steps, each with whether its body spans lines and the shell it declares.
function parseSteps(job) {
  const steps = [];
  let current = null;
  for (const { text, number } of job.lines) {
    if (/^ {6}- /.test(text)) {
      current = { number, name: "", multiline: false, hasRun: false, shell: "" };
      steps.push(current);
    }
    if (!current) continue;
    const key = /^ {6}(?:- | {2})(name|run|shell):\s*(.*)$/.exec(text);
    if (!key) continue;
    if (key[1] === "name") current.name = key[2];
    if (key[1] === "shell") current.shell = key[2].trim();
    if (key[1] === "run") {
      current.hasRun = true;
      current.multiline = /^[|>][+-]?\s*$/.test(key[2].trim());
      if (!current.name) current.name = key[2].trim();
    }
  }
  return steps.filter((step) => step.hasRun);
}

function audit(source) {
  const problems = [];
  let checked = 0;
  for (const job of parseJobs(source)) {
    if (!runsOnWindows(job)) continue;
    const jobDefault = defaultsToBash(job);
    for (const step of parseSteps(job)) {
      if (!step.multiline) continue;
      checked++;
      if (step.shell === "bash" || (!step.shell && jobDefault)) continue;
      problems.push(
        `job "${job.name}", line ${step.number} (${step.name || "unnamed step"}): a multi-line body on a job that can run on Windows, with ${
          step.shell ? `shell "${step.shell}"` : "no shell declared"
        }. Declare \`shell: bash\` on the step or \`defaults: run: shell: bash\` on the job.`,
      );
    }
  }
  return { problems, checked };
}

const fixture = (jobBody) => `name: fixture
on: [push]
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: \${{ matrix.os }}
${jobBody}
  linux:
    runs-on: ubuntu-latest
    steps:
      - name: Bash on a Linux runner is the runner's own default
        run: |
          if [ -n "$x" ]; then exit 1; fi
`;
const bashStep = `      - name: Two commands
        run: |
          node a.mjs --self-test
          node a.mjs
`;

if (process.argv.includes("--self-test")) {
  const cases = [
    ["a multi-line body with no shell on a Windows-capable job", `    steps:\n${bashStep}`, 1],
    ["the same body under a job-level default", `    defaults:\n      run:\n        shell: bash\n    steps:\n${bashStep}`, 0],
    ["the same body with its own shell", `    steps:\n${bashStep}        shell: bash\n`, 0],
    ["a step that overrides the job default with PowerShell", `    defaults:\n      run:\n        shell: bash\n    steps:\n${bashStep}        shell: pwsh\n`, 1],
    ["a single-line body", `    steps:\n      - run: go vet ./...\n`, 0],
  ];
  const failures = [];
  for (const [label, body, expected] of cases) {
    const { problems } = audit(fixture(body));
    if (problems.length !== expected)
      failures.push(`${label}: expected ${expected} problem(s), found ${problems.length}`);
  }
  if (failures.length) {
    for (const failure of failures) console.error(`self-test: ${failure}`);
    process.exit(1);
  }
  console.log(
    `Workflow-shell audit self-test passed: ${cases.length} fixtures, including the pre-fix shape (rejected) and a PowerShell override of a Bash default (rejected).`,
  );
  process.exit(0);
}

const override = process.argv.indexOf("--workflow");
const workflowPath =
  override > -1 ? resolve(process.argv[override + 1]) : resolve(repository, ".github/workflows/check.yml");
const { problems, checked } = audit(readFileSync(workflowPath, "utf8"));
if (problems.length) {
  for (const problem of problems) console.error(problem);
  console.error(`Workflow-shell audit failed (${problems.length} problem(s)).`);
  process.exit(1);
}
if (checked === 0) {
  console.error("Workflow-shell audit found no multi-line step on a Windows-capable job; the parser no longer matches the workflow.");
  process.exit(1);
}
console.log(
  `Workflow-shell audit passed: ${checked} multi-line step(s) on Windows-capable jobs all run under bash.`,
);
console.log("This reads the workflow definition; it is not a hosted run.");
