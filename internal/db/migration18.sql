-- A spent command-receipt guard (ADR 0014, ADR 0015).
--
-- A receipt is what makes a replay recognisable as the same operation rather than new
-- work, so its key has to stay reserved for the account's lifetime. That does not mean
-- its payload has to: a receipt past its window is compacted to a guard, and the
-- guard has to record only that the key was used.
--
-- `outcome` carried a CHECK that forbade the empty string, so a compacted row had
-- nowhere to say "used, payload gone" and the whole payload had to be retained
-- instead. The check is widened to include 'spent', which is a statement about the
-- row rather than about the operation, and the replay path reads it as the defined
-- expired result.
--
-- SQLite cannot alter a CHECK in place, so the table is rebuilt the way migration 17
-- rebuilt the activity table, carrying every row across.

CREATE TABLE private_report_receipts_rebuilt (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    operation_key TEXT NOT NULL,
    intent_digest TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('message','request')),
    source_id TEXT NOT NULL DEFAULT '',
    attempt_id TEXT NOT NULL DEFAULT '',
    evidence_id TEXT NOT NULL DEFAULT '',
    -- 'spent' is a compacted guard: the key is reserved, the outcome is not known.
    outcome TEXT NOT NULL CHECK(outcome IN ('reported','reported_and_blocked','spent')),
    reporter_id TEXT NOT NULL DEFAULT '',
    subject_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    UNIQUE(user_id, operation_key)
);

INSERT INTO private_report_receipts_rebuilt(id,user_id,operation_key,intent_digest,source_kind,source_id,attempt_id,evidence_id,outcome,reporter_id,subject_id,created_at,expires_at)
    SELECT id,user_id,operation_key,intent_digest,source_kind,source_id,attempt_id,evidence_id,outcome,reporter_id,subject_id,created_at,expires_at
    FROM private_report_receipts;

DROP TABLE private_report_receipts;
ALTER TABLE private_report_receipts_rebuilt RENAME TO private_report_receipts;

-- The replay path looks a receipt up by (account, key); the index survives the
-- rebuild only if it is recreated, and a lookup that has to scan would be a
-- per-request cost on the hot path.
CREATE INDEX private_report_receipts_lookup ON private_report_receipts(user_id, operation_key);
CREATE INDEX private_report_receipts_expiry ON private_report_receipts(expires_at);
