import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { countDownloads, renderBadge, updateBadge } from '../../scripts/update-download-badge.mjs';

const asset = (name, download_count) => ({ name, download_count });
const release = (tag_name, assets, draft = false) => ({ tag_name, assets, draft });

test('download count includes paginated published native and source archives', () => {
  const pages = [
    [
      release('v1.0.0-rc.1', [
        asset('brrpbrrp-1.0.0-rc.1-linux-amd64.zip', 12),
        asset('SHA256SUMS', 90)
      ])
    ],
    [
      release('v1.0.0', [
        asset('brrpbrrp-1.0.0-windows-arm64.zip', 8),
        asset('brrpbrrp-docker-source-1.0.0.zip', 3),
        asset('unrelated.zip', 99)
      ])
    ]
  ];
  assert.equal(countDownloads(pages), 23);
});

test('drafts, fixture releases, mismatched versions and unsupported targets are excluded', () => {
  const archives = [asset('brrpbrrp-1.0.0-linux-amd64.zip', 40)];
  assert.equal(
    countDownloads([
      [
        release('ci-baselines-2026-10-08', archives),
        release('v1.0.0', archives, true),
        release('v1.1.0', archives),
        release('v1.0.0', [asset('brrpbrrp-1.0.0-freebsd-amd64.zip', 10)])
      ]
    ]),
    0
  );
  assert.equal(countDownloads([[]]), 0);
});

test('malformed counts and release pages fail instead of underreporting', () => {
  assert.throws(() => countDownloads({}), /paginated/);
  assert.throws(
    () => countDownloads([[release('v1.0.0', [asset('brrpbrrp-1.0.0-linux-amd64.zip', -1)])]]),
    /invalid/
  );
  assert.throws(() => countDownloads([[release('v1.0.0', undefined)]]), /missing/);
});

test('an API failure preserves the last successful badge', () => {
  const dir = mkdtempSync(join(tmpdir(), 'brrpbrrp-badge-'));
  try {
    const output = join(dir, 'downloads.svg');
    writeFileSync(output, 'last successful count');
    assert.throws(
      () =>
        updateBadge({
          repository: 'caamer20/BrrpBrrp',
          output,
          readPages: () => {
            throw new Error('API unavailable');
          }
        }),
      /API unavailable/
    );
    assert.equal(readFileSync(output, 'utf8'), 'last successful count');
  } finally {
    rmSync(dir, { recursive: true });
  }
});

test('the badge exposes a readable exact count', () => {
  assert.match(renderBadge(1234), /aria-label="Release downloads: 1,234"/);
});
