# Alpha.5 local verification

Date: 25 September 2026 Pacific time (26 September UTC). Application 0.4.0-alpha.5, schema 10, backup format 2. Frontend build `083ff51d9960726b`.

## Final checks

| Command / scenario | Result |
| --- | --- |
| `go test -race -timeout 20m ./...` | Passed all packages; API 762.644 s. Compiled baseline includes account capacity, reports, muting, identity/session/receipt/backup fixes; final read-cursor and voice changes below landed while this run executed. |
| Read-cursor, visibility and account-removal race regressions | Passed, API 20.567 s; stale session returned 200 and recreated a cursor before the fix. |
| Final voice state/forwarding race regressions | Passed, RTC 9.456 s and API 13.553 s; actual RTP regrant failed before the fix. Includes self-mute/deafen and moderator controls. |
| `go test ./...` on final source | All packages passed; API 72.938 s, RTC 4.388 s, DB 9.271 s. |
| `go vet ./...` | Passed after final backend changes. |
| `npm --prefix web test` | 96/96 passed. |
| Svelte / Prettier / production build | Passed; zero errors and zero warnings. |
| Full Playwright suite | 13/13 passed in 1.6 minutes after final backend fixes. |
| Fresh-install backup / verify / restore | 23/23 passed; see recovery.json. |
| Actual archived alpha.3 upgrade / rollback / interrupted upgrade recovery | 53/53 passed; see rollback.json. |
| `node scripts/licenses.mjs --check` | 39 components, 20 texts; current. |
| Docker input audit and self-test | Passed, 24 COPY sources; no Docker engine was available. |
| Source/native package static checker | Passed; 349 actual source entries and 38 native document/script entries. The checker prints 350 and 39 because its list normalization counts a trailing empty line as `.`; all actual packaged links/build inputs pass. This cosmetic counter issue does not change archive contents. |
| npm audit moderate | Zero vulnerabilities. |
| Pinned govulncheck 1.8.0 | Zero affected symbols/imported-package vulnerabilities. GO-2026-5932 is module-only in unused x/crypto/openpgp. |

## Corrections during integration

- Historical `report_audit_test.go` depended on API-package helpers but was compiled as an unrelated package by `go test ./...`. It is now an inert `.go.txt` fixture; its documented recipe copies it into the API package in a disposable reproduction.
- The first full race attempt exceeded Go's ten-minute default while doing real-cost bcrypt work. CI/Make/contributor commands now allow 20 minutes per package; hashing cost and assertions were preserved. The successful full run above used that setting.
- Chromium's `getUserMedia` stalled in standalone probes independent of the app. The product now bounds capture waiting at 30 seconds, supports retry and stops late tracks after cancellation. The media test uses a real WebAudio synthetic stream through actual RTCPeerConnection, SFU and RTP counters; it preserves the non-cooperative sender and regrant assertions. This exposed the real persistent-mute bug that was fixed. OS capture/audibility remain unqualified.
- The implementation review found revoked push registration/removal, socket cleanup restoring removed metadata, stale read-cursor mutation, and concurrent account-capacity admission defects; focused regressions reproduced and now prevent them.

## Scope and artifact identity

Tests used disposable local accounts and data. No real Windows service, Docker runtime, independent restore, public deployment, hosted CI, signing, capacity/soak, beta or independent security acceptance occurred. No release gate G0–G8 passed.

The post-build release-manifest.json in this folder and the companion manifest beside the ZIPs record the frozen source checkpoint, source/native archive hashes, toolchains and extracted-consumer results. The source package deliberately excludes this planning directory, operator-specific notes, private state, data, tools and node_modules.
