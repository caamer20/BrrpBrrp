// The acceptance matrix, checked rather than hoped for.
//
// `docs/v1-implementation-plan.md` §11.2 names the scenarios a release has to pass. A plan
// is a list of intentions; what makes it a contract is a gate that fails when a scenario
// has no evidence behind it, or when the evidence it names has been deleted or renamed.
// That is this script: it reads the catalog out of the plan, reads the matrix, and fails
// the build when the two disagree.
//
// A scenario is either *proved* by named, existing checks, or has an entry under `owed`
// saying what is missing and which phase it belongs to. Nothing may be silently absent, and
// nothing may claim a check that does not exist.
//
// Usage:
//   node scripts/check-acceptance-matrix.mjs [--json]
//
import { readFileSync, existsSync } from "node:fs";
import { globSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** The scenario ids the plan's catalog declares. */
export function catalogFromPlan(planText) {
  const ids = new Set();
  const catalog = planText.slice(planText.indexOf("### 11.2"));
  for (const line of catalog.split("\n")) {
    // The id may carry digits (T-A11Y-01), which an [A-Z]+ pattern would silently skip.
    const match = /^\|\s*(T-[A-Z0-9]+-[0-9]+)\s*\|/.exec(line);
    if (match) ids.add(match[1]);
    if (line.startsWith("### 11.3")) break;
  }
  return ids;
}

/**
 * Whether one named check exists.
 *
 * The kinds are the four ways this repository proves something: a Go test, a browser
 * journey, a front-end unit test, and a rehearsal or probe script. Each is looked up where
 * it actually lives, so a check that was renamed or deleted is a failure rather than a
 * stale string in a JSON file.
 */
export function checkExists(reference, base = root) {
  if (reference.startsWith("script:")) {
    const target = path.join(base, reference.slice("script:".length));
    return existsSync(target) ? null : `no such script: ${target}`;
  }
  const separator = reference.indexOf("::");
  if (separator === -1) return `a reference must be "kind::name": ${reference}`;
  const where = reference.slice(0, separator);
  const name = reference.slice(separator + 2);
  if (!where || !name)
    return `a reference must name both a place and a check: ${reference}`;
  if (where.includes(".spec.mjs")) {
    const file = path.join(base, "web/e2e", where);
    if (!existsSync(file)) return `no such journey file: ${file}`;
    // A journey title is quoted with single quotes in the spec.
    return readFileSync(file, "utf8").includes(`'${name}'`)
      ? null
      : `no such journey in ${where}: ${name}`;
  }
  if (where.includes(".test.mjs")) {
    const file = path.join(base, where);
    if (!existsSync(file)) return `no such unit test file: ${file}`;
    return readFileSync(file, "utf8").includes(`'${name}'`)
      ? null
      : `no such test in ${where}: ${name}`;
  }
  // A Go package test: `api::TestSomething` lives in internal/api/*_test.go.
  const files = globSync(`internal/${where}/*_test.go`, { cwd: base });
  if (files.length === 0) return `no such package: internal/${where}`;
  for (const file of files) {
    if (readFileSync(path.join(base, file), "utf8").includes(`func ${name}(`))
      return null;
  }
  return `no such test in internal/${where}: ${name}`;
}

export function checkMatrix({ planText, matrix, base = root }) {
  const problems = [];
  const catalog = catalogFromPlan(planText);
  if (catalog.size === 0)
    problems.push("the plan declares no acceptance scenarios");
  const scenarios = matrix.scenarios ?? {};
  for (const id of catalog) {
    if (!(id in scenarios))
      problems.push(`${id} is in the plan and absent from the matrix`);
  }
  for (const id of Object.keys(scenarios)) {
    if (!catalog.has(id))
      problems.push(`${id} is in the matrix and not in the plan`);
    const entry = scenarios[id] ?? {};
    const checks = entry.checks ?? [];
    const owed = entry.owed ?? [];
    if (checks.length === 0 && owed.length === 0)
      problems.push(
        `${id} names no check and nothing owed: a scenario cannot be empty`,
      );
    for (const reference of checks) {
      const problem = checkExists(reference, base);
      if (problem) problems.push(`${id}: ${problem}`);
    }
    for (const item of owed) {
      if (typeof item !== "string" || item.length < 20)
        problems.push(
          `${id}: an owed entry must say what is missing and where it belongs`,
        );
    }
  }
  return {
    problems,
    scenarios: catalog.size,
    proved: Object.values(scenarios).filter(
      (entry) => (entry.checks ?? []).length > 0,
    ).length,
    checks: Object.values(scenarios).reduce(
      (total, entry) => total + (entry.checks ?? []).length,
      0,
    ),
    owed: Object.values(scenarios).reduce(
      (total, entry) => total + (entry.owed ?? []).length,
      0,
    ),
    owedScenarios: Object.entries(scenarios)
      .filter(([, entry]) => (entry.owed ?? []).length > 0)
      .map(([id]) => id),
  };
}

export async function main(argv, base = root) {
  const asJson = argv.includes("--json");
  const planText = readFileSync(
    path.join(base, "docs/v1-implementation-plan.md"),
    "utf8",
  );
  const matrix = JSON.parse(
    readFileSync(path.join(base, "planning/acceptance-matrix.json"), "utf8"),
  );
  const result = checkMatrix({ planText, matrix, base });
  if (asJson) {
    console.log(JSON.stringify(result, null, 2));
  } else {
    console.log(
      `Acceptance matrix: ${result.scenarios} scenarios, ${result.checks} named checks, ` +
        `${result.owed} owed item(s) across ${result.owedScenarios.length} scenario(s).`,
    );
    for (const id of result.owedScenarios) {
      const entry = matrix.scenarios[id];
      for (const item of entry.owed ?? [])
        console.log(`  owed  ${id}  ${item}`);
    }
    for (const problem of result.problems) console.log(`  FAIL  ${problem}`);
    if (result.problems.length === 0)
      console.log("Every scenario is accounted for.");
  }
  return result.problems.length === 0 ? 0 : 1;
}

const invoked = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invoked === fileURLToPath(import.meta.url)) {
  process.exit(await main(process.argv.slice(2)));
}
