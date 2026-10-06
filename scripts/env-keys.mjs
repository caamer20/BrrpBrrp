// Shared by the two audits that ask whether the `BRRP_*` keys the server reads are
// reachable where an operator would look:
//
//   check-compose-env.mjs   — is each one passed into the container by a Compose file?
//   check-env-coverage.mjs  — is each one documented in .env.example and deployment.md?
//
// "Which keys does the server read" is the half that must not drift. A copy of this walk
// in each script would let one audit go blind while the other kept reporting success, which
// is the shape of the defect both exist to catch.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

export const sourceRoots = ["internal", "cmd"];

function goFiles(directory) {
  const files = [];
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) {
      files.push(...goFiles(path));
    } else if (entry.endsWith(".go") && !entry.endsWith("_test.go")) {
      files.push(path);
    }
  }
  return files;
}

// The keys the server reads, taken from the string literals in its own source. Test files
// are excluded on purpose: the load and benchmark harnesses read their own knobs
// (`BRRP_EXPORT_LOAD`, `BRRP_VIDEO_LOAD_SECONDS`, and the rest) and those are a developer's
// promise, not an operator's.
export function serverKeys(root) {
  const keys = new Set();
  for (const directory of sourceRoots) {
    const path = join(root, directory);
    if (!existsSync(path)) continue;
    for (const file of goFiles(path)) {
      const text = readFileSync(file, "utf8");
      for (const match of text.matchAll(/"BRRP_[A-Z0-9_]+"/g)) {
        keys.add(match[0].slice(1, -1));
      }
    }
  }
  return keys;
}

// The keys a Compose file passes into the container: the names on the left of an
// `environment:` mapping, in either the mapping or the list form. A name that appears only
// inside a `${...}` substitution on the right is not a pass-through.
export function composeKeys(root, composeFiles) {
  const keys = new Set();
  for (const name of composeFiles) {
    const path = join(root, name);
    if (!existsSync(path)) continue;
    let environmentIndent = null;
    for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) continue;
      const indent = line.length - line.trimStart().length;
      if (environmentIndent !== null && indent <= environmentIndent) {
        environmentIndent = null;
      }
      if (trimmed === "environment:") {
        environmentIndent = indent;
        continue;
      }
      if (environmentIndent === null) continue;
      const mapping = trimmed.match(/^(BRRP_[A-Z0-9_]+)\s*:/);
      if (mapping) {
        keys.add(mapping[1]);
        continue;
      }
      const item = trimmed.match(/^-\s*(BRRP_[A-Z0-9_]+)=/);
      if (item) keys.add(item[1]);
    }
  }
  return keys;
}
