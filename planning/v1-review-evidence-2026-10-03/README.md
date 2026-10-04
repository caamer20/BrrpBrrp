# Evidence for the 3 October 2026 v1 planning review

This directory supports the [execution plan](../v1-release-execution-plan-2026-10-03.md). The review began on 3 October and was finalized on 4 October 2026, Pacific time. It is a local review record, not a release qualification bundle. The [manifest](manifest.json) records the reviewed source and pending-input hashes. Application, test, workflow and configuration source files were not edited by this planning task.

## Fresh results

| Check | Result | Evidence and scope |
| --- | --- | --- |
| Frontend unit suite | 348 passed; zero failed/skipped | [Raw output](frontend-unit.log); the existing test servers ran with permitted loopback access. An initial restricted attempt hit `listen EPERM`; that original raw log was overwritten by the permitted rerun and is unavailable in this bundle. |
| Svelte/type checks | Zero errors/warnings | [Raw output](frontend-check.log). |
| Frontend formatting | Pass | [Raw output](frontend-format.log). |
| Source-package audit | **Fail**: six diagnostics for three absent Makefile entry scripts | [Raw output](source-package.log). Source-list inspection additionally identified missing imported `rehearsal-kit.mjs` and `planning/acceptance-matrix.json`; those two are not among the checker's six diagnostics. |
| Acceptance-reference audit | Reference-integrity pass: 29 scenarios, 132 references, 14 owed | Does not execute checks or establish semantic coverage, source freshness or a passed release gate. |
| Doc/schema/browser-step/Docker-input audits | Pass within each configured scope | Ten selected documents; schema25 statements; three browser shell bodies with failure stub; static Docker COPY inputs. No hosted/Docker/Windows runtime implied. |
| Broad Go non-race suite | **Pass**, exit0: 11 test-bearing packages; two packages without tests | [Complete permitted-run output](go-tests.log). Exact command: `GOCACHE=/private/tmp/brrpbrrp-go-build GOPROXY=off go test ./... -count=1 -timeout 15m`. API package728.269s. First sandboxed attempt encountered denied loopback binds: [infrastructure output](go-sandbox-infrastructure.log); that is not a product-failure diagnosis. No race instrumentation. |
| Concurrent storage-quota diagnostic | **Expected failure reproducing a defect** | [Command/output](quota-reproduction.log): two eight-byte uploads accepted with quota ten; sixteen bytes committed; both HTTP201. |

No fresh browser suite, full race sweep, public-package build, current vulnerability database scan, real Windows/Docker/device/network qualification, independent review or beta/RC observation was performed. Historical pass counts in other ledgers were not promoted to fresh review evidence.

The finished plan passed architect review after four corrections: explicit mandatory load durations/rates, bounded alert evaluation/delivery, valid cross-machine clocks and measurement definitions, and separation of local preparation from later platform acceptance. Frontend and backend reviewers found no remaining must-fix accuracy issues in their work packages. These are document/code-review results, not independent release approvals.

## Reproducing the quota interleaving

The diagnostic used Go's temporary file overlay. It replaced no repository file on disk. The only instrumentation in upload handling is a scheduling barrier **after both real quota checks and before transaction admission**, forcing an interleaving the production implementation permits. It does not bypass authorization, alter quota results, replace database insertion or fake the response.

The frozen [test source](quota-review-test.go.txt) and [instrumented handler source](uploads-instrumented.go.txt) are stored as text so normal test discovery does not compile them. The [original command/output](quota-reproduction.log) contains temporary paths from the run. Use the portable procedure below to regenerate an overlay instead of reusing those expired paths.

Run only against the reviewed source. The hash guard intentionally refuses a changed handler: after a fix, write an updated regression with an appropriate scheduling seam rather than overlaying old production code onto the new implementation.

From the repository root, with Go available and a writable build cache:

```sh
python3 - <<'PY'
from pathlib import Path
import hashlib, json, os, subprocess, tempfile

root = Path.cwd()
evidence = root / 'planning/v1-review-evidence-2026-10-03'
manifest = json.loads((evidence / 'manifest.json').read_text())
actual = hashlib.sha256((root / 'internal/api/uploads.go').read_bytes()).hexdigest()
if actual != manifest['uploads_source_sha256']:
    raise SystemExit('Handler changed; do not replay this frozen overlay against new source.')

with tempfile.TemporaryDirectory(prefix='brrp-quota-review-') as scratch:
    overlay = Path(scratch) / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(root / 'internal/api/quota_review_test.go'):
            str(evidence / 'quota-review-test.go.txt'),
        str(root / 'internal/api/uploads.go'):
            str(evidence / 'uploads-instrumented.go.txt')
    }}))
    result = subprocess.run([
        'go', 'test', '-overlay', str(overlay), './internal/api',
        '-run', '^TestReviewConcurrentUploadQuota$', '-count=1', '-timeout', '30s', '-v'
    ], cwd=root, env=os.environ.copy())
    raise SystemExit(result.returncode)
PY
```

At the reviewed baseline the expected result is failure reporting `quota 10 exceeded: committed=16 status=[201 201]`. An uninstrumented burst passing once does not rule out this race. A real fix needs a normal committed regression proving that quota checking and metadata admission form one serialized decision, with files and rows reconciled after refusals.

## Evidence interpretation

The thread cursor/link, CI shell/harness, capacity-driver, export-limit, alert and metrics findings are source-confirmed findings or explicitly identified unmeasured risks in the execution plan. They were not all dynamically reproduced during this review. The next agent must add failing behavioral regressions where specified and record the result before claiming the corresponding fix or gate.

The review also used a disposable checker fixture containing `test.skip('mandatory example', ...)`; the reference lookup accepted its quoted title. This supports only the stated limitation of reference lookup, not an assertion that all current mapped tests are skipped.

Keep these local files out of a general source distribution unless an individual sanitized fixture is intentionally adopted as a product test. Do not add the whole planning directory to a package merely to satisfy the acceptance matrix's runtime dependency.
