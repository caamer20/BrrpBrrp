#!/usr/bin/env node
// Static audit of what the Dockerfile needs from the build context.
//
// The image build runs on a Linux host with Docker (`.github/workflows/docker.yml`),
// so a missing input is only discovered when it happens to run. This check answers
// the questions that do not need Docker at all: is every COPY source present, does
// `.dockerignore` let it into the context, does each stage that runs node or go
// actually have that runtime, and does the runtime image ship the project license
// and the generated third-party notices?
//
// It is deliberately a static check and is labelled as one: passing here is not a
// successful image build, and the runtime lifecycle check remains
// `scripts/check-docker.mjs`.
//
// Usage: node scripts/check-docker-inputs.mjs [Dockerfile] [context]

import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, join, posix, relative } from "node:path";
import { fileURLToPath } from "node:url";

// --- Self-test --------------------------------------------------------------
//
// The audit is only worth running if it catches the defect it was written for, so
// `--self-test` runs it against two disposable fixtures: the Dockerfile form that
// had the incomplete notices stage, and the current form. The first must fail with
// each specific complaint, the second must pass. Without this, a later edit that
// silently stops detecting the problem would look like a passing check.
if (process.argv.includes("--self-test")) {
  const script = fileURLToPath(import.meta.url);
  const repository = dirname(dirname(script));
  const workspace = mkdtempSync(join(tmpdir(), "brrpbrrp-docker-audit-"));
  try {
    const context = join(workspace, "context");
    for (const directory of [
      "cmd/brrpbrrp",
      "internal/app",
      "scripts",
      "web/src",
      "web/public",
      "web/dist",
      "web/node_modules/dep",
    ]) {
      mkdirSync(join(context, directory), { recursive: true });
    }
    const files = {
      "go.mod": "module brrpbrrp\n",
      "go.sum": "",
      LICENSE: "MIT\n",
      "cmd/brrpbrrp/main.go": "package main\n",
      "internal/app/app.go": "package app\n",
      "scripts/release.go": "//go:build ignore\n\npackage main\n",
      "scripts/licenses.mjs": "// fixture\n",
      "scripts/release-targets.json": '["linux/amd64"]\n',
      "web/assets.go": "package web\n",
      "web/index.html": "<!doctype html>\n",
      "web/package.json": "{}\n",
      "web/package-lock.json": "{}\n",
      "web/src/main.ts": "\n",
      "web/public/robots.txt": "\n",
      "web/dist/index.html": "<!doctype html>\n",
      "web/node_modules/dep/package.json": "{}\n",
      "web/node_modules/dep/LICENSE": "MIT\n",
    };
    for (const [path, content] of Object.entries(files))
      writeFileSync(join(context, path), content);
    writeFileSync(
      join(context, ".dockerignore"),
      readFileSync(join(repository, ".dockerignore")),
    );

    const head = `ARG NODE_IMAGE=node:24.21.0-alpine3.24
ARG GO_IMAGE=golang:1.27.1-alpine3.24

FROM \${NODE_IMAGE} AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
COPY web/ ./

`;
    // The shape this audit was written for: the Go stage runs the notices
    // generator with no Node runtime, no generator and no frontend packages.
    const broken = `${head}FROM \${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY scripts/release.go ./scripts/release.go
COPY web/*.go ./web/
COPY --from=frontend /src/web/dist/ ./web/dist/
ARG TARGETOS
ARG TARGETARCH
RUN go run scripts/release.go -notices /out/THIRD_PARTY_NOTICES.txt && \\
    CGO_ENABLED=0 GOOS=\${TARGETOS} GOARCH=\${TARGETARCH} go build -o /out/brrpbrrp ./cmd/brrpbrrp

FROM scratch AS runtime
COPY --from=build /out/brrpbrrp /usr/local/bin/brrpbrrp
COPY --from=build /out/THIRD_PARTY_NOTICES.txt /usr/share/licenses/brrpbrrp/THIRD_PARTY_NOTICES.txt
`;
    const fixed = readFileSync(join(repository, "Dockerfile"), "utf8");
    writeFileSync(join(workspace, "Dockerfile.broken"), broken);
    writeFileSync(join(workspace, "Dockerfile.fixed"), fixed);

    const run = (dockerfile) =>
      spawnSync(process.execPath, [script, dockerfile, context], {
        encoding: "utf8",
      });
    const complaints = [
      "has no Node runtime",
      "without copying scripts/licenses.mjs",
      "without copying scripts/release-targets.json",
      "without copying web/package.json",
      "without the bundled frontend packages",
      "does not copy the project LICENSE",
    ];
    let failures = 0;
    const brokenRun = run(join(workspace, "Dockerfile.broken"));
    if (brokenRun.status === 0) {
      console.error("self-test: the incomplete notices stage was not detected");
      failures += 1;
    }
    for (const complaint of complaints) {
      if (!brokenRun.stderr.includes(complaint)) {
        console.error(`self-test: expected complaint missing: ${complaint}`);
        failures += 1;
      }
    }
    const fixedRun = run(join(workspace, "Dockerfile.fixed"));
    if (fixedRun.status !== 0) {
      console.error(
        `self-test: the current Dockerfile did not pass: ${fixedRun.stderr}`,
      );
      failures += 1;
    }
    if (failures > 0) {
      console.error(
        `Docker input audit self-test failed (${failures} problem(s)).`,
      );
      process.exit(1);
    }
    console.log(
      `Docker input audit self-test passed: the incomplete notices stage produced ${complaints.length} expected complaints and the current Dockerfile passed.`,
    );
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
  process.exit(0);
}

const dockerfilePath = process.argv[2] || "Dockerfile";
const context = process.argv[3] || ".";
const dockerfile = readFileSync(dockerfilePath, "utf8");

const problems = [];
const fail = (message) => problems.push(message);

// --- Dockerfile parsing -----------------------------------------------------

// Join line continuations, then keep only instructions (comments and blanks drop).
const lines = dockerfile
  .replace(/\\\r?\n\s*/g, " ")
  .split(/\r?\n/)
  .map((line) => line.trim())
  .filter((line) => line && !line.startsWith("#"));

const globalArgs = new Map();
const stages = [];
for (const line of lines) {
  const arg = line.match(/^ARG\s+([A-Za-z0-9_]+)=(.+)$/);
  if (arg && stages.length === 0) {
    globalArgs.set(arg[1], arg[2]);
    continue;
  }
  const from = line.match(/^FROM\s+(?:--\S+\s+)*(\S+)(?:\s+AS\s+(\S+))?$/i);
  if (from) {
    const base = globalArgs.get(from[1].replace(/^\$\{|\}$/g, "")) || from[1];
    stages.push({
      name: from[2] || `stage${stages.length}`,
      base,
      baseIsStage: stages.some((stage) => stage.name === from[2]),
      lines: [],
    });
    continue;
  }
  if (stages.length > 0) stages[stages.length - 1].lines.push(line);
}

if (stages.length === 0) fail(`${dockerfilePath}: no FROM instruction found`);

const expandedBase = (stage) =>
  globalArgs.get(stage.base.replace(/^\$\{|\}$/g, "")) || stage.base;
const nodeBase = globalArgs.get("NODE_IMAGE") || "node";
const goBase = globalArgs.get("GO_IMAGE") || "golang";

// A stage has a runtime if its base image provides it, if an ancestor stage does,
// or if the stage installs it before the instruction that needs it.
function providesRuntime(stage, kind, beforeLine) {
  const seen = new Set();
  const search = (current) => {
    if (!current || seen.has(current.name)) return false;
    seen.add(current.name);
    const base = expandedBase(current);
    const has =
      kind === "node"
        ? /(^|\/)node(:|@|$)/.test(base)
        : /(^|\/)golang(:|@|$)/.test(base);
    if (has) return true;
    if (
      !current.baseIsStage &&
      !stages.some((value) => value.name === current.base)
    )
      return false;
    return search(stages.find((value) => value.name === current.base));
  };
  if (search(stage)) return true;
  const install =
    kind === "node"
      ? /apk add[^\n]*\b(nodejs|node|npm)\b/
      : /apk add[^\n]*\bgolang\b/;
  // The instruction that needs the runtime counts too: a stage may install Node and
  // use it in the same RUN line.
  const relevant =
    beforeLine === undefined
      ? stage.lines
      : stage.lines.slice(0, beforeLine + 1);
  return relevant.some((line) => line.startsWith("RUN ") && install.test(line));
}

// --- .dockerignore ----------------------------------------------------------

// Docker matches rules in order and the last matching rule wins, with `!` taking a
// path back out of the excluded set.
const ignoreRules = readFileSync(join(context, ".dockerignore"), "utf8")
  .split(/\r?\n/)
  .map((line) => line.trim())
  .filter((line) => line && !line.startsWith("#"))
  .map((rule) => {
    const negated = rule.startsWith("!");
    const pattern = (negated ? rule.slice(1) : rule)
      .replace(/^\//, "")
      .replace(/\/$/, "");
    const source = pattern
      .split("/")
      .map((part) =>
        part === "**"
          ? ".*"
          : part.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, "[^/]*"),
      )
      .join("/");
    return { negated, pattern, regex: new RegExp(`^${source}$`) };
  });

const excluded = (path) => {
  const normalized = path.replace(/^\.\//, "");
  let isExcluded = false;
  for (const rule of ignoreRules) {
    if (
      rule.regex.test(normalized) ||
      rule.regex.test(normalized.replace(/\/.*$/, ""))
    ) {
      isExcluded = !rule.negated;
    }
  }
  return isExcluded;
};

// --- COPY sources -----------------------------------------------------------

const copiedSources = [];
for (const stage of stages) {
  for (const line of stage.lines) {
    if (!/^(COPY|ADD)\s/i.test(line)) continue;
    const tokens = line.replace(/^(COPY|ADD)\s+/i, "").split(/\s+/);
    const flags = tokens.filter((token) => token.startsWith("--"));
    const args = tokens.filter((token) => !token.startsWith("--"));
    if (args.length < 2) {
      fail(`${dockerfilePath}: ${line} has no destination`);
      continue;
    }
    const from = flags
      .find((flag) => flag.startsWith("--from="))
      ?.slice("--from=".length);
    for (const source of args.slice(0, -1)) {
      copiedSources.push({ stage: stage.name, source, from, line });
      if (from !== undefined) {
        if (!stages.some((value) => value.name === from)) {
          fail(
            `stage ${stage.name}: "COPY --from=${from}" names no stage in this Dockerfile`,
          );
        }
        continue;
      }
      // A directory source copies its whole tree, so the context only needs one
      // included file beneath it.
      const candidates = source.includes("*")
        ? (() => {
            const directory = source.replace(/\/[^/]*$/, "") || ".";
            const suffix = source.split("/").pop().replace(/\*/g, "");
            if (!existsSync(join(context, directory))) return [];
            return readdirSync(join(context, directory))
              .filter((entry) => !suffix || entry.endsWith(suffix))
              .map((entry) =>
                posix.join(directory === "." ? "" : directory, entry),
              );
          })()
        : [source];
      if (candidates.length === 0) {
        fail(
          `stage ${stage.name}: "${line}" matches no file on this host (${source})`,
        );
        continue;
      }
      const included = candidates.some((candidate) => {
        const path = join(context, candidate);
        if (!existsSync(path)) return false;
        // A directory is included when something inside it is: `web/**` is excluded
        // while `!web/*.go` brings the Go sources back.
        if (!statSync(path).isDirectory()) return !excluded(candidate);
        // Walk one useful level deep: a directory survives the ignore rules only if
        // something inside it does.
        const walk = (directory) =>
          readdirSync(directory).some((entry) => {
            const child = join(directory, entry);
            const childPath = relative(context, child).replace(/\\/g, "/");
            if (excluded(childPath)) return false;
            return statSync(child).isDirectory() ? walk(child) : true;
          });
        return walk(path);
      });
      if (!included) {
        fail(
          `stage ${stage.name}: "${line}" source ${source} is excluded by .dockerignore (or absent)`,
        );
      }
    }
  }
}

// --- Runtime availability ---------------------------------------------------

for (const stage of stages) {
  stage.lines.forEach((line, index) => {
    if (!line.startsWith("RUN ")) return;
    // The notices generator reads the linked module set with the Go toolchain and
    // the bundled frontend packages with Node, whether it is invoked directly or
    // through scripts/release.go -notices. Both runtimes and the declared inputs must
    // be in the stage that runs it: this is the defect class the audit exists for.
    const generatesNotices =
      /scripts\/licenses\.mjs/.test(line) ||
      /scripts\/release\.go\b[^\n]*-notices\b/.test(line);
    if (generatesNotices) {
      if (!providesRuntime(stage, "node", index)) {
        fail(
          `stage ${stage.name}: generates notices but has no Node runtime in its base image or a prior install`,
        );
      }
      if (!providesRuntime(stage, "go", index)) {
        fail(
          `stage ${stage.name}: generates notices but has no Go toolchain in its base image or a prior install`,
        );
      }
      for (const input of ["scripts/licenses.mjs", "scripts/release-targets.json", "web/package.json"]) {
        if (
          !copiedSources.some(
            (value) => value.stage === stage.name && value.source === input,
          )
        ) {
          fail(
            `stage ${stage.name}: generates notices without copying ${input} into the stage`,
          );
        }
      }
      if (
        !copiedSources.some(
          (value) =>
            value.stage === stage.name && value.source.includes("node_modules"),
        )
      ) {
        fail(
          `stage ${stage.name}: generates notices without the bundled frontend packages (web/node_modules)`,
        );
      }
      if (
        !copiedSources.some(
          (value) =>
            value.stage === stage.name && value.source.includes("dist"),
        )
      ) {
        fail(
          `stage ${stage.name}: generates notices without the served web build (web/dist), which the inspected Go package embeds`,
        );
      }
      return;
    }
    if (
      /\b(node|npm|npx)\b[^]*?\b(scripts\/[\w.-]+\.mjs|npm (ci|run|test)|npx )/.test(
        line,
      ) &&
      !providesRuntime(stage, "node", index)
    ) {
      fail(
        `stage ${stage.name}: runs Node (${line.slice(0, 60)}...) but neither its base image nor the stage provides node`,
      );
    }
    if (
      /\bgo (list|build|run|mod|env|vet|tool)\b/.test(line) &&
      !providesRuntime(stage, "go", index)
    ) {
      fail(
        `stage ${stage.name}: runs Go (${line.slice(0, 60)}...) but neither its base image nor the stage provides the toolchain`,
      );
    }
  });
}

// --- The runtime image carries the distribution license and notices ---------

const final = stages[stages.length - 1];
const finalSources = copiedSources.filter(
  (value) => value.stage === final.name,
);
if (!finalSources.some((value) => /(^|\/)LICENSE$/.test(value.source))) {
  fail(
    `final stage ${final.name}: does not copy the project LICENSE into the image`,
  );
}
if (!finalSources.some((value) => /THIRD_PARTY_NOTICES/i.test(value.source))) {
  fail(
    `final stage ${final.name}: does not copy THIRD_PARTY_NOTICES into the image`,
  );
}

// --- Result -----------------------------------------------------------------

if (problems.length > 0) {
  console.error(
    `Docker input audit failed for ${dockerfilePath} (${problems.length} problem(s)):`,
  );
  for (const problem of problems) console.error(`  - ${problem}`);
  process.exit(1);
}
const stagesList = stages.map((stage) => stage.name).join(", ");
console.log(
  `Docker input audit passed: ${copiedSources.length} COPY sources across stages ${stagesList} are present and included in the build context; the final stage ships LICENSE and THIRD_PARTY_NOTICES.`,
);
console.log(
  "This is a static audit. It is not an image build; run scripts/check-docker.mjs on a Docker host for that.",
);
