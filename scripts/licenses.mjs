#!/usr/bin/env node
// Generate THIRD-PARTY-NOTICES.md from the software that is actually redistributed.
//
// The released binary embeds the web bundle, and the Go compiler links third-party
// packages into it, so a release redistributes both. This script reads the linked
// module set from the Go toolchain, the bundled frontend packages from
// web/package.json, and each component's own license file, then writes the notices
// file. It fails instead of guessing: an unidentified or missing license stops a
// release rather than shipping one that claims a license it never read.
//
// Usage: node scripts/licenses.mjs [go-target] [output]
//        node scripts/licenses.mjs --check   (verify the checked-in file is current)

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

// option reads a named flag, so the version can be given without colliding with the
// positional arguments this script already takes.
const option = (flag, fallback) => {
  const index = process.argv.indexOf(flag);
  return index < 0 ? fallback : process.argv[index + 1];
};
const GO = process.env.BRRP_GO || "go";
const check = process.argv[2] === "--check";
// Positional arguments are the target and the output path, in that order, with named
// flags interleaved freely. Reading them positionally meant a flag between them became
// a filename — which produced files literally called `--version`.
const positionals = process.argv.slice(2).filter((value, index, all) => {
  if (value.startsWith("--")) return false;
  const previous = all[index - 1];
  return !(previous && previous.startsWith("--"));
});
const target = check ? "./cmd/brrpbrrp" : positionals[0] || "./cmd/brrpbrrp";
const output = check
  ? "THIRD-PARTY-NOTICES.md"
  : positionals[1] || "THIRD-PARTY-NOTICES.md";

const go = (...args) =>
  execFileSync(GO, args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 32 << 20,
  }).trim();

// Licensed names we can recognise from the text itself. A component whose license
// is not recognised fails the run, so a new dependency cannot arrive unnoticed.
const patterns = [
  [/ISC License/i, "ISC"],
  [/Apache License\s*\n?\s*Version 2\.0/i, "Apache-2.0"],
  [/\bMIT License\b/i, "MIT"],
  // Some MIT projects keep the grant without the words "MIT License".
  [
    /Permission is hereby granted, free of charge, to any person obtaining a copy/i,
    "MIT",
  ],
  [/GNU AFFERO GENERAL PUBLIC LICENSE/i, "AGPL-3.0"],
  [/GNU LESSER GENERAL PUBLIC LICENSE/i, "LGPL-3.0"],
  [/GNU GENERAL PUBLIC LICENSE/i, "GPL-3.0"],
  [/Mozilla Public License Version 2\.0/i, "MPL-2.0"],
  [
    /Permission to use, copy, modify, and(\/or)? distribute this software for any\s*purpose with or without fee/i,
    "ISC",
  ],
  [/Redistribution and use in source and binary forms/i, "BSD"],
  [
    /This is free and unencumbered software released into the public domain/i,
    "Unlicense",
  ],
  [/CC0 1\.0 Universal/i, "CC0-1.0"],
];

function identify(text) {
  for (const [pattern, name] of patterns) {
    if (pattern.test(text)) {
      // A BSD license is two-clause or three-clause depending on the third term.
      if (name === "BSD" && /Neither the name/i.test(text))
        return "BSD-3-Clause";
      if (name === "BSD") return "BSD-2-Clause";
      return name;
    }
  }
  return null;
}

function licenseFile(directory) {
  if (!existsSync(directory)) return null;
  const name = readdirSync(directory).find((entry) =>
    /^(licen[cs]e|copying|notice)/i.test(entry),
  );
  return name ? join(directory, name) : null;
}

function component(directory, name, version) {
  const file = licenseFile(directory);
  if (!file)
    throw new Error(`${name}@${version} has no license file in ${directory}`);
  const text = readFileSync(file, "utf8").trim();
  const license = identify(text);
  if (!license)
    throw new Error(
      `${name}@${version} uses an unrecognised license (${file})`,
    );
  return { name, version, license, text };
}

// Every module whose packages the compiler links into the released binary.
const modules = go(
  "list",
  "-deps",
  "-f",
  "{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}\t{{.Module.Version}}{{end}}{{end}}",
  target,
)
  .split("\n")
  .filter(Boolean)
  .map((line) => line.split("\t"));
const unique = new Map(
  modules.map(([path, version]) => [`${path}@${version}`, [path, version]]),
);

const components = [];
for (const [path, version] of unique.values()) {
  components.push(
    component(go("list", "-m", "-f", "{{.Dir}}", path), path, version),
  );
}

// The binary serves web/dist, so the packages bundled into it are redistributed too.
const manifest = JSON.parse(
  readFileSync(join(root, "web/package.json"), "utf8"),
);
for (const [name, range] of Object.entries(manifest.dependencies || {})) {
  const directory = join(root, "web/node_modules", name);
  const version = existsSync(join(directory, "package.json"))
    ? JSON.parse(readFileSync(join(directory, "package.json"), "utf8")).version
    : range;
  components.push(component(directory, name, version));
}

// A Go-built binary also contains portions of the standard library.
// Homebrew keeps the toolchain in libexec and its LICENSE in the same installed
// distribution's parent. Accept that exact layout without substituting a license
// from another toolchain or downloading a guessed text.
const goRoot = go("env", "GOROOT");
const goLicenseDirectory =
  !licenseFile(goRoot) &&
  basename(goRoot) === "libexec" &&
  existsSync(join(dirname(goRoot), "LICENSE"))
    ? dirname(goRoot)
    : goRoot;
components.push(
  component(
    goLicenseDirectory,
    "the Go standard library",
    go("env", "GOVERSION"),
  ),
);

components.sort((left, right) => left.name.localeCompare(right.name));

// The same license text covers many components: group by text so each is reproduced
// once with every component that depends on it.
const groups = new Map();
for (const entry of components) {
  const key = `${entry.license}\u0000${entry.text}`;
  if (!groups.has(key))
    groups.set(key, { license: entry.license, text: entry.text, members: [] });
  groups.get(key).members.push(entry);
}
const ordered = [...groups.values()].sort(
  (left, right) =>
    left.members[0].name.localeCompare(right.members[0].name) ||
    left.license.localeCompare(right.license),
);

const lines = [
  "# Third-party notices",
  "",
  "BrrpBrrp is distributed under the MIT License (see [LICENSE](LICENSE)).",
  "",
  "Each release also contains software written by other people: the Go packages the",
  "compiler links into the `brrpbrrp` executable, and the frontend packages bundled",
  "into the web assets that executable serves. Every one of them is listed below with",
  "the license it is distributed under and the license text it carries.",
  "",
  "Regenerate this file from the current dependency set with:",
  "",
  "```sh",
  "make notices",
  "```",
  "",
  "The generator reads the licenses out of the modules and packages themselves, and",
  "stops the release if one of them has no readable license, so a dependency cannot",
  "arrive without its terms being recorded here.",
  "",
  "## Components",
  "",
  "| Component | Version | License |",
  "| --- | --- | --- |",
];
for (const entry of components) {
  lines.push(`| \`${entry.name}\` | ${entry.version} | ${entry.license} |`);
}
lines.push("", "## License texts", "");
for (const group of ordered) {
  lines.push(`### ${group.license}`, "", "Applies to:", "");
  for (const member of group.members) {
    lines.push(`- \`${member.name}\` ${member.version}`);
  }
  lines.push("", "```text", group.text, "```", "");
}

// A machine-readable companion to the notices, in CycloneDX 1.5.
//
// The notices file is what a person reads; this is what a scanner and a downstream
// consumer can read. Both come from the same discovered component set, so they cannot
// disagree about what is shipped — which is the reason this is generated here rather
// than by a separate tool that would have its own idea of the dependency list.
//
// The version recorded for this build is the one the source is being released as, not
// a guess: a caller passes it, and an absent value is recorded as unknown rather than
// invented.
const sbomText =
  JSON.stringify(
    {
      bomFormat: "CycloneDX",
      specVersion: "1.5",
      version: 1,
      metadata: {
        component: {
          type: "application",
          name: "BrrpBrrp",
          version: option("--version", "unknown"),
        },
        tools: [{ vendor: "brrpbrrp", name: "scripts/licenses.mjs" }],
      },
      components: components.map((entry) => ({
        type: "library",
        name: entry.name,
        version: entry.version,
        licenses: entry.license
          ? [{ license: { id: entry.license } }]
          : undefined,
      })),
    },
    null,
    2,
  ) + "\n";
const header = `<!-- Generated by scripts/licenses.mjs. Do not edit by hand. -->\n`;
const summary = `${components.length} components, ${ordered.length} license texts`;
const content = header + lines.join("\n");
if (check) {
  const current = existsSync(join(root, output))
    ? readFileSync(join(root, output), "utf8")
    : "";
  if (current !== content) {
    console.error(
      `${output} does not match the current dependencies (${summary}). Run: make notices`,
    );
    process.exit(1);
  }
  // --check reads, it does not write. A check that regenerated the file it is
  // checking would always pass, and this step is the gate that a stale SBM cannot ship.
  const currentSbom = existsSync(join(root, "THIRD-PARTY-SBOM.json"))
    ? readFileSync(join(root, "THIRD-PARTY-SBOM.json"), "utf8")
    : "";
  if (currentSbom !== sbomText) {
    console.error(
      "THIRD-PARTY-SBOM.json does not match the current dependencies. Run: make notices",
    );
    process.exit(1);
  }
  console.log(`${output} and THIRD-PARTY-SBOM.json: up to date (${summary})`);
} else if (output === "-") {
  // "-" writes to standard output so the release helper can embed the notices in a
  // package without a second generator to keep in step.
  process.stdout.write(content);
  // Written even in the notices-only mode, which is the mode the release helper uses.
  // An SBOM stamped with an unknown version is still better than none, and the release
  // path passes the real one.
  if (output === "-" && process.env.BRRP_SBOM_PATH)
    writeFileSync(process.env.BRRP_SBOM_PATH, sbomText);
  console.error(`standard output: ${summary}`);
} else {
  writeFileSync(join(root, output), content);
  writeFileSync(join(root, "THIRD-PARTY-SBOM.json"), sbomText);
  console.log(`${output}: ${summary}`);
}
