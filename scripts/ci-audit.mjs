#!/usr/bin/env node
// Shared static gates for local checks, full CI and the capped Linux check.
import { spawnSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const go = process.env.BRRP_GO || "go";
const gofmt =
  process.env.BRRP_GOFMT ||
  (go.includes("/") || go.includes("\\")
    ? join(dirname(go), process.platform === "win32" ? "gofmt.exe" : "gofmt")
    : "gofmt");

function run(command, args, capture = false) {
  console.log(`> ${command} ${args.join(" ")}`);
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: "utf8",
    stdio: capture ? "pipe" : "inherit",
    maxBuffer: 32 << 20,
  });
  if (result.error || result.status !== 0) {
    if (result.error) console.error(result.error.message);
    if (capture) console.error(result.stderr || result.stdout);
    process.exit(result.status || 1);
  }
  return result.stdout;
}

run(go, ["vet", "./..."]);
const unformatted = run(gofmt, ["-l", "internal", "cmd", "scripts"], true).trim();
if (unformatted) {
  console.error(`gofmt would rewrite these files:\n${unformatted}`);
  process.exit(1);
}
run(go, ["mod", "tidy", "-diff"]);
run(process.execPath, ["scripts/licenses.mjs", "--check"]);

for (const audit of [
  "docker-inputs",
  "compose-env",
  "env-coverage",
  "workflow-shells",
  "doc-links",
  "schema-docs",
]) {
  run(process.execPath, [`scripts/check-${audit}.mjs`, "--self-test"]);
  run(process.execPath, [`scripts/check-${audit}.mjs`]);
}
for (const audit of ["source-package", "acceptance-matrix"]) {
  run(process.execPath, [`scripts/check-${audit}.mjs`]);
}
for (const script of readdirSync(join(root, "scripts")).sort()) {
  if (script.endsWith(".mjs"))
    run(process.execPath, ["--check", `scripts/${script}`]);
}

const router = readFileSync(join(root, "internal/api/router.go"), "utf8");
for (const route of [
  "/api/channels",
  "/api/channels/{id}/messages",
  "/api/directs",
  "/api/admin/users",
  "/api/search",
]) {
  if (!router.includes(route)) {
    console.error(`The capacity tooling names ${route}, which the router does not serve.`);
    process.exit(1);
  }
}
console.log("Static audits passed; capacity fixture routes match the product.");
