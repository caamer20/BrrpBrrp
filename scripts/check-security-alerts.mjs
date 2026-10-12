#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
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

export function sourceTreeDigest(commit, cwd) {
  if (!/^[a-f0-9]{40}$/.test(commit)) throw new Error("Invalid source commit");
  const records = execFileSync("git", ["ls-tree", "-r", "-z", commit], {
    encoding: "utf8",
    cwd,
  })
    .split("\0")
    .filter(Boolean)
    .filter(
      (entry) =>
        entry.split("\t")[1] !== "scripts/codeql-reviewed-findings.json",
    )
    .sort();
  if (!records.length) throw new Error("No source tree to fingerprint");
  return createHash("sha256").update(records.join("\0")).digest("hex");
}

function physicalIdentity(location, uriBases = {}) {
  const physical = location?.physicalLocation;
  const region = physical?.region;
  if (
    !physical?.artifactLocation?.uri ||
    !Number.isSafeInteger(region?.startLine)
  )
    throw new Error("Finding has no stable physical location");
  const bases = [];
  let base = physical.artifactLocation.uriBaseId;
  while (base) {
    if (bases.some((entry) => entry.id === base) || !uriBases[base]?.uri)
      throw new Error("Finding has an unresolved or cyclic URI base");
    bases.push({ id: base, uri: uriBases[base].uri });
    base = uriBases[base].uriBaseId;
  }
  return {
    path: physical.artifactLocation.uri,
    bases,
    line: region.startLine,
    column: region.startColumn ?? 0,
    endLine: region.endLine ?? region.startLine,
    endColumn: region.endColumn ?? 0,
  };
}

export function findingIdentity(result, component, ruleId, run = {}) {
  const primary = result.partialFingerprints?.primaryLocationLineHash;
  if (!primary || !component.name || !component.semanticVersion)
    throw new Error(
      "Finding lacks a stable fingerprint or query-pack identity",
    );
  const locations = result.locations.map((location) =>
    physicalIdentity(location, run.originalUriBaseIds),
  );
  const flows = (result.codeFlows ?? [])
    .flatMap((flow) =>
      flow.threadFlows.map((thread) =>
        thread.locations.map((entry) =>
          physicalIdentity(entry.location, run.originalUriBaseIds),
        ),
      ),
    )
    .map((flow) => JSON.stringify(flow))
    .sort();
  const value = {
    component: component.name,
    component_version: component.semanticVersion,
    rule: ruleId,
    primary,
    locations,
    flows,
  };
  return {
    identity: createHash("sha256").update(JSON.stringify(value)).digest("hex"),
    component: component.name,
    component_version: component.semanticVersion,
    rule: ruleId,
    path: locations[0].path,
    line: locations[0].line,
  };
}

function validateReviews(dispositions, sourceDigest) {
  const reviews = new Map();
  if (!dispositions) return reviews;
  if (dispositions.version !== 1 || !Array.isArray(dispositions.findings))
    throw new Error("Invalid reviewed-findings record");
  if (
    dispositions.findings.length &&
    (!/^[a-f0-9]{64}$/.test(dispositions.source_digest) ||
      dispositions.source_digest !== sourceDigest)
  )
    throw new Error(
      "Reviewed findings do not match the exact analyzed source tree",
    );
  for (const entry of dispositions.findings) {
    if (
      !/^[a-f0-9]{64}$/.test(entry.identity) ||
      reviews.has(entry.identity) ||
      entry.disposition !== "false-positive" ||
      typeof entry.rule !== "string" ||
      typeof entry.component !== "string" ||
      !entry.component ||
      typeof entry.component_version !== "string" ||
      !entry.component_version ||
      typeof entry.path !== "string" ||
      !entry.path ||
      !Number.isSafeInteger(entry.line) ||
      entry.line < 1 ||
      typeof entry.reason !== "string" ||
      entry.reason.trim().length < 30 ||
      !Array.isArray(entry.evidence) ||
      !entry.evidence.length ||
      !entry.evidence.every(
        (value) => typeof value === "string" && value.trim(),
      )
    )
      throw new Error("Invalid or duplicate reviewed finding");
    reviews.set(entry.identity, entry);
  }
  return reviews;
}

export function assessSarif(sarif, { dispositions, sourceDigest } = {}) {
  if (
    sarif?.version !== "2.1.0" ||
    !Array.isArray(sarif.runs) ||
    !sarif.runs.length
  )
    throw new Error("Missing or malformed SARIF analysis");
  const problems = [];
  const reviews = validateReviews(dispositions, sourceDigest);
  const seenReviews = new Set();
  let findings = 0;
  let reviewed = 0;
  for (const run of sarif.runs) {
    const driver = run.tool?.driver;
    const components = [driver, ...(run.tool?.extensions ?? [])];
    if (
      driver?.name !== "CodeQL" ||
      components.some(
        (component) =>
          component.rules !== undefined && !Array.isArray(component.rules),
      )
    )
      throw new Error("SARIF must contain valid CodeQL rule components");
    const rules = components.flatMap((component) => component.rules ?? []);
    if (!rules.length) throw new Error("SARIF contains no CodeQL rules");
    const results = run.results ?? [];
    if (!Array.isArray(results)) throw new Error("Malformed SARIF results");
    for (const result of results) {
      findings++;
      const reference = result.rule ?? {};
      const ruleId = result.ruleId ?? reference.id;
      const extension = reference.toolComponent;
      let component = driver;
      if (extension) {
        let matches;
        if (extension.index !== undefined) {
          if (!Number.isSafeInteger(extension.index) || extension.index < 0)
            throw new Error(
              "SARIF finding refers to an unknown tool component",
            );
          matches = [run.tool.extensions?.[extension.index]].filter(Boolean);
        } else {
          if (!extension.name && !extension.guid)
            throw new Error(
              "SARIF finding refers to an unknown tool component",
            );
          matches = components;
        }
        matches = matches.filter(
          (entry) =>
            (!extension.name || entry.name === extension.name) &&
            (!extension.guid || entry.guid === extension.guid),
        );
        if (matches.length !== 1)
          throw new Error(
            "SARIF finding refers to an unknown or ambiguous tool component",
          );
        component = matches[0];
      }
      const candidates = ruleId
        ? (extension ? [component] : components).flatMap((candidate) =>
            (candidate.rules ?? [])
              .filter((entry) => entry.id === ruleId)
              .map((rule) => ({ rule, component: candidate })),
          )
        : [component.rules?.[reference.index ?? result.ruleIndex]]
            .filter(Boolean)
            .map((rule) => ({ rule, component }));
      const selected = candidates.length === 1 ? candidates[0] : undefined;
      const rule = selected?.rule;
      if (!rule || (reference.id && rule.id !== reference.id))
        throw new Error("SARIF finding has no identifiable rule");
      component = selected.component;
      const value = rule.properties?.["security-severity"];
      const security =
        value !== undefined || rule.properties?.tags?.includes("security");
      if (!security) continue;
      const score =
        typeof value === "string" && value.trim() ? Number(value) : NaN;
      if (!Number.isFinite(score) || score < 0 || score > 10)
        problems.push(`Security finding ${rule.id} has no recognized severity`);
      else if (score >= 7) {
        let identity;
        if (reviews.size)
          identity = findingIdentity(result, component, rule.id, run);
        const review = identity && reviews.get(identity.identity);
        if (review) {
          if (seenReviews.has(identity.identity))
            throw new Error("Duplicate finding matched a reviewed exception");
          if (
            Object.entries(identity).some(
              ([key, value]) => review[key] !== value,
            )
          )
            throw new Error("Reviewed finding identity fields disagree");
          seenReviews.add(identity.identity);
          reviewed++;
        } else
          problems.push(
            `${score >= 9 ? "Critical" : "High"} security finding ${rule.id}`,
          );
      }
      // No suppression or later GitHub dismissal silently waives this gate.
    }
  }
  return { problems, findings, reviewed };
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
    let reviewed = 0;
    const dispositions = JSON.parse(
      execFileSync(
        "git",
        ["show", `${sha}:scripts/codeql-reviewed-findings.json`],
        { encoding: "utf8" },
      ),
    );
    const sourceDigest = sourceTreeDigest(sha);
    for (const analysis of analyses) {
      const result = assessSarif(
        readJson([
          "-H",
          "Accept: application/sarif+json",
          `repos/${repo}/code-scanning/analyses/${analysis.id}`,
        ]),
        { dispositions, sourceDigest },
      );
      if (result.problems.length) throw new Error(result.problems.join("; "));
      findings += result.findings;
      reviewed += result.reviewed;
      console.log(
        `Verified CodeQL analysis ${analysis.id} (${analysis.category}) at ${sha}`,
      );
    }
    console.log(
      `CodeQL findings gate passed: both tested analyses, ${findings} finding(s), ${reviewed} source-bound reviewed false positive(s), no unreviewed high/critical security findings.`,
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
