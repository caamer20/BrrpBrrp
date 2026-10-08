#!/usr/bin/env node
// Checks that every relative markdown link in the documents a reader actually opens
// resolves to a file that exists.
//
// A broken link in a README is not a cosmetic defect: it is the one place a person
// who has not seen the repository is told where to go next, and a link to a document
// that was renamed sends them nowhere. The failure names the document and the target,
// because "a link is broken" is not something anybody can act on.
//
// The gate must pass on a fresh clone, so the documents it requires are the ones a
// clone contains. Operator notes that `.gitignore` excludes on purpose are checked
// when they exist and are never required to: `docs/next-release.md` is one, and a
// check that fails because a deliberately untracked file is absent fails for every
// reader who did nothing wrong (found when a clean worktree was first built).
//
// Usage: node scripts/check-doc-links.mjs [--list|--self-test]

import {
  existsSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// The documents a reader is sent to. A link inside a historical record is not this
// gate's business: those records are kept for what they decided, and a renamed file
// referenced from one of them is history rather than a route into the product.
const DOCUMENTS = [
  "README.md",
  "docs/community-guide.md",
  "docs/installation.md",
  "docs/deployment.md",
  "docs/host-qualification.md",
  "docs/v1-progress.md",
  "docs/production-readiness.md",
  "docs/github-setup.md",
  "docs/launch-readiness.md",
  // The documents the release candidate sends a reader to first.
  "CHANGELOG.md",
  "docs/known-limitations.md",
  "docs/upgrading.md",
  "docs/compatibility.md",
  // The project's front door: the repository's roadmap and overview, and the end-user manual
  // that is handed to people who use a server.
  "ROADMAP.md",
  "docs/roadmap.md",
  "docs/project-overview.md",
  "docs/manual/README.md",
  "docs/manual/getting-started.md",
  "docs/manual/messages.md",
  "docs/manual/channels.md",
  "docs/manual/direct-messages-and-groups.md",
  "docs/manual/voice-and-screen-sharing.md",
  "docs/manual/notifications-and-preferences.md",
  "docs/manual/account-and-security.md",
  "docs/manual/privacy-and-safety.md",
  "docs/manual/administrators.md",
  "docs/manual/troubleshooting.md",
  "docs/manual/glossary.md",
  ...["1.0.0-rc.1", "0.4.0-alpha.7", "0.4.0-alpha.6"].map(
    (release) => `docs/releases/${release}.md`,
  ),
];

// Deliberately untracked operator notes (.gitignore). A clone does not have them, so
// their absence is not a failure — but when one is present, its links are checked like
// any other document's, because somebody is reading it.
const OPTIONAL_DOCUMENTS = ["docs/next-release.md"];

// A relative link: no scheme, no host, no fragment-only target. Absolute and
// protocol links are somebody else's problem.
const LINK = /\]\(([^)\s]+?)(?:\s+"[^"]*")?\)/g;

// checkDocuments returns one message per problem. It is a function with the tree as a
// parameter so the self-test can point it at a synthetic tree rather than at this
// checkout, and so the cases that matter — a missing required document, a missing
// optional one, a broken link in either — are exercised by the gate itself rather
// than by the person who happens to run it on the right machine.
export function checkDocuments(tree, documents, optional = new Set()) {
  const failures = [];
  const optionalTargets = new Set(
    [...optional].map((document) => resolve(tree, document)),
  );
  for (const document of documents) {
    if (!existsSync(join(tree, document))) {
      // An optional document that is absent is a document this reader does not have.
      if (optional.has(document)) continue;
      failures.push(`${document} is listed here but does not exist`);
      continue;
    }
    const text = readFileSync(join(tree, document), "utf8");
    for (const [, target] of text.matchAll(LINK)) {
      if (/^(https?:|mailto:|#)/.test(target)) continue;
      // A link's target is relative to the document that contains it.
      const withoutFragment = target.split("#")[0].split("?")[0];
      if (!withoutFragment) continue;
      const resolved = resolve(dirname(join(tree, document)), withoutFragment);
      if (existsSync(resolved)) continue;
      // A link *to* an optional document is a link to something a clone may not have:
      // the same reasoning as above, from the other side. It resolves when the note is
      // there and is not a broken route when it is not.
      if (optionalTargets.has(resolved)) continue;
      failures.push(`${document} links to ${target}, which does not exist`);
    }
  }
  return failures;
}

function selfTest() {
  const tree = mkdtempSync(join(tmpdir(), "brrp-doc-links-"));
  let failures = 0;
  const expect = (name, condition) => {
    console.log(`  ${condition ? "ok  " : "FAIL"}  ${name}`);
    if (!condition) failures += 1;
  };
  try {
    mkdirSync(join(tree, "docs", "releases"), { recursive: true });
    writeFileSync(join(tree, "docs", "installation.md"), "Install.\n");
    writeFileSync(
      join(tree, "README.md"),
      "See [install](docs/installation.md) and [notes](docs/next-release.md).\n",
    );
    const documents = [
      "README.md",
      "docs/installation.md",
      "docs/next-release.md",
    ];
    const optional = new Set(["docs/next-release.md"]);

    // A fresh clone: the required documents resolve, the untracked note does not exist,
    // and a link to it from a tracked document is not a failure — that is exactly the
    // state a reader who cloned the repository is in.
    expect(
      "a tree without the untracked note passes",
      checkDocuments(tree, documents, optional).length === 0,
    );

    // A broken link in a tracked document still fails.
    writeFileSync(join(tree, "README.md"), "See [gone](docs/nowhere.md).\n");
    expect(
      "a broken link in a tracked document fails",
      checkDocuments(tree, documents, optional).some((failure) =>
        failure.includes("docs/nowhere.md"),
      ),
    );

    // A required document that is missing still fails: optional means those three
    // words, not "links are not checked any more".
    expect(
      "a missing required document fails",
      checkDocuments(
        tree,
        ["README.md", "docs/community-guide.md"],
        optional,
      ).some((failure) => failure.includes("does not exist")),
    );

    // When the untracked note *is* present, its own links are checked like anybody
    // else's, because somebody is reading it.
    writeFileSync(
      join(tree, "docs", "next-release.md"),
      "See [gone](releases/nowhere.md).\n",
    );
    expect(
      "a broken link in the present note fails",
      checkDocuments(tree, documents, optional).some((failure) =>
        failure.includes("releases/nowhere.md"),
      ),
    );

    // Absolute and fragment links are still nobody's problem. The note created above
    // keeps its broken link, so the assertion is about what these two lines do not add.
    writeFileSync(
      join(tree, "README.md"),
      "See [site](https://example.com) and [top](#top).\n",
    );
    const afterAbsolute = checkDocuments(tree, documents, optional);
    expect(
      "absolute and fragment links are ignored",
      !afterAbsolute.some(
        (failure) =>
          failure.includes("example.com") || failure.includes("#top"),
      ),
    );
  } finally {
    rmSync(tree, { recursive: true, force: true });
  }
  if (failures) {
    console.log(`\n${failures} documentation-link self-test failure(s).`);
    process.exit(1);
  }
  console.log("Documentation-link checks: self-test passed (5 cases).");
}

function main() {
  const documents = [...DOCUMENTS, ...OPTIONAL_DOCUMENTS];
  if (process.argv.includes("--list")) {
    for (const document of documents) console.log(document);
    return;
  }
  const failures = checkDocuments(root, documents, new Set(OPTIONAL_DOCUMENTS));
  for (const failure of failures) console.log(`  FAIL  ${failure}`);
  if (failures.length) {
    console.log(`\n${failures.length} broken documentation link(s).`);
    process.exit(1);
  }
  console.log(
    `Documentation links: all resolve (${documents.length} documents).`,
  );
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  main();
}
