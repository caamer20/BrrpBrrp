# Continue after alpha.6

Current source: alpha.6 / schema12 / backup2. Read docs/releases/0.4.0-alpha.6.md, ADR0012, and planning/alpha6-evidence. Alpha.5 remains the last assembled package set.

Implemented: shared private-message migration, direct consent, canonical pairs, durable contact limits, blocking, shared messages/files/replies/search/read/outbox, inbox UI and personal hide/contact preferences. Private exposure defaults off. Blocking preserves accepted history and own deletion; never replay suppressed sends. Operators have no membership bypass.

Next implementation: selected-evidence private reporting without moderator browse access; private notification preferences and dispatch checks; consented groups/ownership/leave/history policy; organized threads; private-call admission and screen sharing. Continue real Windows/Tailscale and Docker lifecycle/off-host recovery, independent privacy review, capacity/soak, signing/hosted CI, beta and release acceptance in parallel.

Keep scope invariants: messages.channel_id is NULL only for private conversations; conversation_id is mandatory; preserve rowids/FTS/dependents during migration; never disable foreign keys inside a transaction. Stage uploads for their destination and revalidate capability at commit. Private actions must not leak to community audit/search/report or preview-fetch surfaces. Re-evaluate delayed contact publication after blocking, not only when queuing. Personal hide preferences remain explicit and are not cleared on new messages.

Verification: root performed complete Go integration + targeted race regressions,101frontendtests,14opt-inbrowserjourneys plus defaultoff;23backup/restore checks;57alpha5rollbackchecks;55legacyalpha3checks. Exact whole-race attempt/corrected-fixture evidence is in the ledger. Rehearsal now discovers baseline schema/format and returns nonzero on failed assertions.

Do not mark v1 or enable production privacy gates from this local evidence. No real host, physical media, independent security, scale or beta acceptance occurred. Quota at tranche start60%used; stopping observation69%used. A quota clarification (20%remaining vs20totalpoints) was unanswered; finish any already-running check before broader work, and recheck the user's budget authorization.
