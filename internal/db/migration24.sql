-- A thread has its own reading position.
--
-- `conversation_reads` is keyed by conversation, so every message in a channel — a
-- thread reply included — shares one cursor. That is the wrong shape for a thread: a
-- member who reads forty replies in one thread and then opens the channel has, on the
-- only cursor that exists, read the channel as far as the thread's newest reply. And
-- a member who reads the *channel* has marked a thread they never opened as read.
--
-- The two positions are genuinely different facts. A thread is a conversation with a
-- subject, and it moves at its own rate: a quiet thread beside a busy channel, or a
-- busy thread nobody has opened, are the ordinary cases rather than the edge ones.
--
-- The table is keyed by thread, so a deleted thread takes its readers' cursors with it
-- and leaves nothing that could be reached through a thread id that no longer resolves.
CREATE TABLE thread_reads (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    last_read_message_id TEXT NOT NULL DEFAULT '',
    last_read_at TEXT NOT NULL,
    PRIMARY KEY(user_id, thread_id)
);

-- A member's unread count for one thread is read far more often than it is written:
-- every list render asks, and only opening a thread writes.
CREATE INDEX thread_reads_thread ON thread_reads(thread_id);
