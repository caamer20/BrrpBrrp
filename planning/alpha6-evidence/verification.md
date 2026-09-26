# Alpha.6 verification checkpoint

26 September2026. Source alpha.6, schema12, backup2. Frontend `2e25bd1cf7a5e3f4`. No production exposure, publication or new distribution archives.

| Check | Observed result |
| --- | --- |
| Complete Go integration suite | Passed all packages; API72.009s. |
| Go vet | Passed. |
| Whole Go race attempt | All packages except API passed. API832.401s had one failure: a new test tried pinning with a role lacking message.pin. This was a fixture error, not a product permission bypass. No data race reported. This command exited1 and is not recorded as a full green run. |
| Corrected private pipeline/audit fixture | Granted the real moderator pin role; focused race passed9.816s. The peer-administrator control was then strengthened with the actual administrator role and passed race5.338s. |
| Late publication guard | Final block/deactivation regression passed race7.805s after reproducing late private activity before the fix. |
| Contact/consent/blocking | Focused race40.571s, including durable limits, restart, explicit consent, authority changes and erased-peer history. |
| Edit existence / upload capability | Focused race11.213s; pre-fix defects reproduced; permitted controls preserved. |
| Schema11 preservation/failure/restart | Focused race4.641s; complete DB suite5.882s. Full race DB83.705s also passed. |
| Frontend units | 101/101 passed. |
| Svelte / formatting / production build | Passed;0errors/0warnings. |
| Default feature-off browser check | 1/1 passed;10.3s. |
| Full opted-in browser suite | 14/14 passed;1.7m, including DM consent/files/replies/search/outsider denial/block recovery and previous privacy/voice/ownership journeys. |
| Fresh backup/restore | 23/23 passed; recovery.json. |
| Actual alpha.5 upgrade/rollback | 57/57 passed; rollback.json. |
| Actual legacy alpha.3 upgrade/rollback | 55/55 passed; legacy-rollback.json. |
| Dependency notices | Current39components/20texts. No dependency changes. |
| Docker input audit | Static24COPY inputs passed; no runtime available. |
| Source/native documentation paths | Checker passed; it retains the previously documented cosmetic +1 displayed-count issue. |

The first rollback rehearsal applied obsolete schema2/format1 assumptions to alpha.5. The script now captures actual baseline metadata, accepts compatible pre-upgrade snapshots, checks newer-format refusal and exits nonzero when assertions fail. Both actual baseline branches passed after correction.

Automatic review rejected clearing DM hide preferences during send. That change was not applied; explicit personal hide settings remain intact. New messages update conversation activity only.

No Windows/Tailscale host, Docker runtime, physical microphone, off-host recovery, capacity/soak, independent security review, hosted CI/signing or beta acceptance was performed. Private conversations remain disabled by default. DM-specific push/report policy, groups, organized threads and screen sharing remain. No v1 release gate is declared passed.

Quota meter:60%used at tranche start;69% at stopping observation. The optional20%remaining-versus20total-points clarification was unanswered; implementation stopped around the conservative allowance while remaining live checks were collected. The v1 objective is still open.
