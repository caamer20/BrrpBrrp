-- Request-attempt identity, request-introduction evidence, and command receipts
-- (ADR 0014).
--
-- Three additions, no change to any existing row:
--
--   1. `direct_requests.attempt_id` — a canonical pair reuses its request row when
--      the pair asks again, so the pair id alone cannot tell "the request being
--      looked at now" from one that was rejected and later renewed. Each new
--      attempt rotates this, and every existing pending row is backfilled with a
--      distinct value so an upgrade never produces two attempts that look alike.
--
--   2. `private_request_reports` — evidence whose source is a pending request. It is
--      a separate table rather than a row in `private_message_reports` because a
--      request has no message, and a fake message id there would be a fabricated
--      moderation subject. The reviewer experience, capability, rate, capacity,
--      expiry and cleanup policy are the same as ADR 0013's; only the source and
--      the stored field differ.
--
--   3. `private_report_receipts` — durable command receipts. Report-and-block
--      deliberately destroys the source it reports, so "does an open report exist"
--      cannot express its retry semantics. A receipt is keyed by the account's own
--      operation key and records what happened, for reconciliation by that account
--      alone.
--
-- Retention is deliberately different per record and is not implied by the others:
-- evidence expires 90 days from creation, receipts are kept 30 days so a retry can
-- still be recognised, and restricted audit metadata outlives the evidence at 400
-- days (ADR 0014, "Retention, stated separately for each thing"). Backups are
-- outside all three and are never rewritten when a record expires.
--
-- The conversation is stored as provenance only, exactly as the message evidence
-- stores one, and it is omitted from the reviewer-facing representation: an
-- identifier is not permission to browse a conversation.

ALTER TABLE direct_requests ADD COLUMN attempt_id TEXT NOT NULL DEFAULT '';

-- Every pending row needs its own attempt. `id` is not available before the
-- backfill statement below, and the default must be unique per row, so each gets a
-- distinct value derived from the row itself. Empty defaults on rows that were
-- already settled are never read again: a settled request is not previewable and
-- not reportable.
UPDATE direct_requests SET attempt_id = 'legacy-' || conversation_id WHERE attempt_id = '';

-- An attempt is only meaningful while pending, and the lookup the report path uses
-- is "the current pending attempt of this pair". Non-pending rows are excluded so a
-- historical attempt cannot be resolved by accident.
CREATE INDEX direct_requests_attempt ON direct_requests(attempt_id) WHERE state='pending';

-- Evidence whose source is a pending request. Identities are cleared rather than
-- cascaded, so a removed account leaves no identity attached to a record that must
-- still be reviewable.
CREATE TABLE private_request_reports (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    requester_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    reporter_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
    snapshot_text TEXT NOT NULL CHECK(length(snapshot_text) <= 500),
    sender_banned INTEGER NOT NULL DEFAULT 0 CHECK(sender_banned IN (0,1)),
    sender_deactivated INTEGER NOT NULL DEFAULT 0 CHECK(sender_deactivated IN (0,1)),
    request_sent_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved')),
    review_note TEXT NOT NULL DEFAULT '' CHECK(length(review_note) <= 2000),
    reviewed_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    CHECK(attempt_id <> '')
);

-- One open record per reporter per attempt, so a concurrent retry deduplicates onto
-- the original evidence instead of replacing it.
CREATE UNIQUE INDEX private_request_reports_active_reporter_attempt
    ON private_request_reports(reporter_id, attempt_id) WHERE status='open';
CREATE INDEX private_request_reports_queue ON private_request_reports(status, id DESC);
CREATE INDEX private_request_reports_expiry ON private_request_reports(expires_at);

-- Durable command receipts. The key is the reporter's own, so another account can
-- never read or replay one. `intent_digest` binds the action and the explanation
-- together: the same key with different content is a conflict, not a silent
-- overwrite. `outcome` is the minimal historical result and never a statement about
-- current state — a block lifted later must stay lifted, so nothing here says the
-- person is blocked *now*.
CREATE TABLE private_report_receipts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    operation_key TEXT NOT NULL,
    intent_digest TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('message','request')),
    source_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL DEFAULT '',
    evidence_id TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK(outcome IN ('reported','reported_and_blocked')),
    reporter_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    UNIQUE(user_id, operation_key)
);

-- The retry lookup is by (account, key); this index serves it without a scan.
CREATE INDEX private_report_receipts_lookup ON private_report_receipts(user_id, operation_key);
-- Bounded receipt retention is reclaimed by the same maintenance pass that
-- reclaims expired evidence.
CREATE INDEX private_report_receipts_expiry ON private_report_receipts(expires_at);
