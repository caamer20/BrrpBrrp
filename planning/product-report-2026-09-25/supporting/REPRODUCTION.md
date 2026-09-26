# Reproducing the report's bug evidence

These diagnostics are development checks against the matching BrrpBrrp source tree. They do not require live accounts or production data. The JavaScript probe compiles the actual TypeScript modules into an inert in-memory browser shell; it is not a rendered-browser test. The Go probe depends on the application's existing API test helpers and is meant to run inside a disposable source copy.

Prerequisites: the report's matching source snapshot, Node 24 (or a compatible supported version), the frontend development dependencies (`npm --prefix web ci` if absent), Python 3 for the copy recipe, and Go 1.27.1. The recipes below assume a POSIX shell opened at the repository root. `.tools/go/bin/go` is the bundled Go toolchain used for the original audit; the Python recipe also accepts an installed `go` on PATH. Build the frontend with `npm --prefix web run build` first if `web/dist` is absent, because the Go app embeds those assets.

## Outbox diagnostics (BR-02 through BR-05)

From the repository root:

```sh
node planning/product-report-2026-09-25/supporting/brrpbrrp-current-outbox-probes.mjs
```

The probe has no machine-specific absolute paths. If moved elsewhere, supply the source root explicitly:

```sh
node /path/to/brrpbrrp-current-outbox-probes.mjs /path/to/BrrpBrrp
```

Observed output on the assessed source:

```text
malformed_entry_suppresses_valid_sibling 0
raw_persisted_text_characters 81920
restored_text_characters 65536
full-composer {"outbox":null,"draftHasRecoveredText":false,"toast":"Recovery promise: Your text is in the composer."}
no-text-channel {"outbox":null,"draftHasRecoveredText":false,"toast":"Recovery promise: Your text is in the composer."}
expired_entry_after_restore {"outbox":null,"draft":"","toast":""}
```

This observation script exits zero when it completes; zero does not mean the product contract passed. It intentionally prints the defective outcomes. The recovery-toast wording in this diagnostic is supplied to the real `releaseOutbox` method by the probe; the report separately cites the application's actual call site and user wording at `workspace.svelte.ts:783–787`. The source methods perform the storage deletion and draft recovery logic being inspected. The UI itself was not driven for these new cases.

## Original retry after receipt pruning (BR-01)

The retained fixture uses a `.go.txt` suffix so the historical probe is not compiled as a standalone package by `go test ./...`. The copy recipe installs it under the API package where its helpers exist. On alpha.5 the maintained receipt regression now enforces the non-recreating result; the observations below describe the assessed alpha.4 source.

The following recipe creates a disposable source copy, copies the retained test into that copy's API package, runs only that diagnostic and prints the copy location. It changes no application source in the original checkout. It retains the temporary copy for inspection; remove it when no longer needed.

```sh
python3 - <<'PY'
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

root = Path.cwd().resolve()
support = root / 'planning/product-report-2026-09-25/supporting'
probe = support / 'report_audit_test.go.txt'
assert (root / 'go.mod').exists() and probe.exists(), 'Run from the BrrpBrrp root'
copy = Path(tempfile.mkdtemp(prefix='brrpbrrp-report-reproduction-'))
for name in ('internal', 'cmd'):
    shutil.copytree(root / name, copy / name)
for name in ('go.mod', 'go.sum'):
    shutil.copy2(root / name, copy / name)
(copy / 'web').mkdir()
for source in (root / 'web').glob('*.go'):
    shutil.copy2(source, copy / 'web' / source.name)
shutil.copytree(root / 'web/dist', copy / 'web/dist')
shutil.copy2(probe, copy / 'internal/api/report_audit_test.go')
bundled = root / '.tools/go/bin/go'
go = str(bundled) if bundled.exists() else shutil.which('go')
assert go, 'Go 1.27.1 is required'
print('Disposable copy:', copy, flush=True)
result = subprocess.run([
    go, 'test', './internal/api',
    '-run', '^TestReportOriginalRetryAfterReceiptPruning$', '-count=1', '-v'
], cwd=copy, env=os.environ.copy())
raise SystemExit(result.returncode)
PY
```

**Expected on the assessed source:** the diagnostic fails with `original expired operation recreated deleted message after receipt pruning`; its preceding log shows `status=201 stored_messages=1`. The test intentionally asserts the stronger master-plan requirement. The current maintained expiry test permits key reuse after pruning, so its passing result and this diagnostic failure are consistent with the report's contract-conflict finding. The probe simulates 31 days of age by modifying only test-fixture receipt timestamps; it does not wait 31 days.

## Maintained checks used for historical-fix disposition

The frontend unit/state suite was rerun as:

```sh
npm --prefix web test
```

Result for the assessed source: 84 pass, zero fail.

The seven focused API regressions were rerun as:

```sh
.tools/go/bin/go test ./internal/api -run 'Test(AStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence|ACredentialCommitRefusesAQueuedWriteOnTheRevokedSession|ASocketRegisteredAfterItsSessionEndedIsRefused|ACombinedCopyAndStatusFailureKeepsTheDurableLocalGeneration|AStatusInsertFailureBlocksRetentionOfBothIdentities|AFinalStatusWriteFailureLeavesTheRecoveryIdentitiesIntact|AnExpiredReceiptIsNeverNewWork)$' -count=1 -v
```

Result: seven pass; API package 6.258 seconds in the original report run. Two of these tests use temporary loopback sockets. A sandbox refusal to bind a port is an environmental execution failure, not the assertion the regression is testing; rerun in a development environment that permits local loopback listeners. The original audit's first restricted attempt hit that refusal; an approved rerun passed. Go build/module caches were redirected to temporary directories for the original audit, which does not affect the test contract.

These are narrow checks. They do not rerun the full race suite, browser journeys, real-host installation, off-host restore, physical media, load/soak or independent security acceptance. Do not interpret their results as release approval.
