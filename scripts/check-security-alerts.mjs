#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

// Analyze the immutable results of the tested commit, rather than the branch's
// current alert state: a later fix must not qualify an earlier vulnerable build.
export function selectAnalyses(analyses, sha, ref) {
  if (!Array.isArray(analyses))
    throw new Error("Code-scanning analyses must be an array");
  return ["go", "javascript-typescript"].map((language) => {
    const category = `/language:${language}`;
    const matches = analyses
      .filter(
        (entry) =>
          entry.commit_sha === sha &&
          entry.ref === ref &&
          !entry.error &&
          entry.tool?.name === "CodeQL" &&
          [category, `${category}/`].includes(entry.category) &&
          Number.isSafeInteger(entry.id) &&
          entry.id > 0 &&
          entry.rules_count > 0,
      )
      .sort((a, b) => b.id - a.id);
    if (!matches.length)
      throw new Error(
        `No completed ${language} CodeQL analysis for the tested source/ref`,
      );
    return matches[0];
  });
}

export function assessSarif(sarif) {
  if (
    sarif?.version !== "2.1.0" ||
    !Array.isArray(sarif.runs) ||
    !sarif.runs.length
  )
    throw new Error("Missing or malformed SARIF analysis");
  const problems = [];
  let findings = 0;
  for (const run of sarif.runs) {
    const driver = run.tool?.driver;
    if (driver?.name !== "CodeQL" || !Array.isArray(driver.rules))
      throw new Error("SARIF must contain CodeQL rules");
    const results = run.results ?? [];
    if (!Array.isArray(results)) throw new Error("Malformed SARIF results");
    for (const result of results) {
      findings++;
      const rule =
        driver.rules.find((entry) => entry.id === result.ruleId) ??
        driver.rules[result.ruleIndex];
      if (!rule || (result.ruleId && rule.id !== result.ruleId))
        throw new Error("SARIF finding has no identifiable rule");
      const value = rule.properties?.["security-severity"];
      const security =
        value !== undefined || rule.properties?.tags?.includes("security");
      if (!security) continue;
      const score =
        typeof value === "string" && value.trim() ? Number(value) : NaN;
      if (!Number.isFinite(score) || score < 0 || score > 10)
        problems.push(`Security finding ${rule.id} has no recognized severity`);
      else if (score >= 7)
        problems.push(
          `${score >= 9 ? "Critical" : "High"} security finding ${rule.id}`,
        );
      // No suppression or later GitHub dismissal silently waives this gate.
    }
  }
  return { problems, findings };
}

function readJson(args) {
  return JSON.parse(
    execFileSync("gh", ["api", ...args], {
      encoding: "utf8",
      maxBuffer: 32 << 20,
    }),
  );
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const repo = process.env.GH_REPO;
    const sha = process.env.RELEASE_COMMIT;
    const ref = process.env.RELEASE_REF;
    if (!repo || !/^[a-f0-9]{40}$/.test(sha || "") || ref !== "refs/heads/main")
      throw new Error(
        "Set GH_REPO, the exact RELEASE_COMMIT and RELEASE_REF=refs/heads/main",
      );
    let analyses;
    for (let attempt = 0; attempt < 3; attempt++) {
      const pages = readJson([
        "--paginate",
        "--slurp",
        `repos/${repo}/code-scanning/analyses?ref=${encodeURIComponent(ref)}&per_page=100&tool_name=CodeQL`,
      ]);
      if (!Array.isArray(pages) || !pages.every(Array.isArray))
        throw new Error("Unexpected paginated code-scanning response");
      try {
        analyses = selectAnalyses(pages.flat(), sha, ref);
        break;
      } catch (error) {
        if (attempt === 2) throw error;
        await delay(10000);
      }
    }
    let findings = 0;
    for (const analysis of analyses) {
      const result = assessSarif(
        readJson([
          "-H",
          "Accept: application/sarif+json",
          `repos/${repo}/code-scanning/analyses/${analysis.id}`,
        ]),
      );
      if (result.problems.length) throw new Error(result.problems.join("; "));
      findings += result.findings;
      console.log(
        `Verified CodeQL analysis ${analysis.id} (${analysis.category}) at ${sha}`,
      );
    }
    console.log(
      `CodeQL findings gate passed: both tested analyses, ${findings} finding(s), no high/critical security findings.`,
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
