#!/usr/bin/env node
// Private repositories cannot expose release counts to an unauthenticated badge
// service. Query GitHub with the maintainer/workflow token and save only the total.
import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const root = fileURLToPath(new URL("../", import.meta.url));
const targets = JSON.parse(
  readFileSync(new URL("release-targets.json", import.meta.url), "utf8"),
);

export function countDownloads(pages) {
  if (!Array.isArray(pages) || !pages.every(Array.isArray))
    throw new Error("GitHub did not return paginated releases");
  let total = 0;
  for (const release of pages.flat()) {
    if (!release || typeof release.tag_name !== "string")
      throw new Error("GitHub returned an invalid release");
    // Include published candidates as well as stable versions. CI fixture tags
    // and drafts are excluded even when their filenames resemble old packages.
    if (
      release.draft ||
      !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(
        release.tag_name,
      )
    )
      continue;
    if (!Array.isArray(release.assets))
      throw new Error("Release assets are missing");
    const version = release.tag_name.slice(1);
    const names = new Set(
      targets.map(
        (target) => `brrpbrrp-${version}-${target.replace("/", "-")}.zip`,
      ),
    );
    names.add(`brrpbrrp-docker-source-${version}.zip`);
    for (const asset of release.assets) {
      if (!names.has(asset.name)) continue;
      if (
        !Number.isSafeInteger(asset.download_count) ||
        asset.download_count < 0
      )
        throw new Error("GitHub returned an invalid archive download count");
      total += asset.download_count;
      if (!Number.isSafeInteger(total))
        throw new Error("Download total exceeds integer precision");
    }
  }
  return total;
}

export function renderBadge(total) {
  const count = total.toLocaleString("en-US");
  const valueWidth = Math.max(34, count.length * 7 + 16);
  const width = 112 + valueWidth;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="20" role="img" aria-label="Release downloads: ${count}">
  <title>Release downloads: ${count}</title>
  <clipPath id="rounded"><rect width="${width}" height="20" rx="3"/></clipPath>
  <g clip-path="url(#rounded)"><path fill="#555" d="M0 0h112v20H0z"/><path fill="#2368a0" d="M112 0h${valueWidth}v20H112z"/></g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,DejaVu Sans,sans-serif" font-size="11"><text x="56" y="14">release downloads</text><text x="${112 + valueWidth / 2}" y="14">${count}</text></g>
</svg>
`;
}

export function updateBadge({
  repository,
  output,
  readPages = fetchReleasePages,
}) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || ""))
    throw new Error("GITHUB_REPOSITORY must be owner/name");
  // Complete and validate the entire API response before replacing the last
  // successful badge. Authentication/API failures must never reset it to zero.
  const total = countDownloads(readPages(repository));
  writeFileSync(output, renderBadge(total));
  return total;
}

function fetchReleasePages(repository) {
  const result = spawnSync(
    "gh",
    [
      "api",
      "--paginate",
      "--slurp",
      `repos/${repository}/releases?per_page=100`,
    ],
    { encoding: "utf8", maxBuffer: 32 << 20 },
  );
  if (result.error || result.status !== 0)
    throw new Error(
      `Could not read GitHub releases: ${result.error?.message || result.stderr.trim()}`,
    );
  return JSON.parse(result.stdout);
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const { values } = parseArgs({
      options: { output: { type: "string", default: resolve(root, "docs/assets/downloads.svg") } },
    });
    const total = updateBadge({
      repository: process.env.GITHUB_REPOSITORY,
      output: resolve(values.output),
    });
    console.log(`Release archive downloads: ${total}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
