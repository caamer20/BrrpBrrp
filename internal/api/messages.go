package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

// sendReceiptWindow is the accepted automatic retry window for one send operation. Inside it a
// retry whose content no longer exists is answered as committed-and-deleted; outside it the
// answer is a distinct expired outcome, so the client stops retrying and offers the text for an
// explicit new send with a new key (ADR 0010).
const sendReceiptWindow = 24 * time.Hour

// receiptPruneAfter is when a deleted receipt loses its destination and intent metadata.
// Its minimal spent-key tombstone survives for the account's lifetime: arbitrary legacy
// keys have no trustworthy creation time, so deleting the guard would re-admit a late retry.
const receiptPruneAfter = 30 * 24 * time.Hour

// sendIntentDigest is the canonical encoding of what the caller asked to send: the
// conversation, the trimmed content, the attachment ids and the reply target. Fields are
// length-prefixed so no concatenation of them can collide with another, and the digest is what
// makes reusing an operation key for other work a conflict instead of a silent replay.
// threadID is the thread the message is filed into, or "" for an ordinary channel
// message. It is part of the intent because the thread is part of the *destination*:
// a member who reuses a key for the same words in a different thread has asked for
// two messages and must get two, and a key that answered with the other thread's
// message leaves a send that silently never happened.
func sendIntentDigest(conversationID, threadID, content string, attachmentIDs []string, replyToID string) string {
	hasher := sha256.New()
	field := func(value string) { fmt.Fprintf(hasher, "%d:%s", len(value), value) }
	field(conversationID)
	// Bounded by its own length prefix, so "" and a thread id can never collide the
	// way two adjacent bare fields could.
	field(threadID)
	field(content)
	field(replyToID)
	field(strconv.Itoa(len(attachmentIDs)))
	attachments := append([]string(nil), attachmentIDs...)
	sort.Strings(attachments)
	for _, id := range attachments {
		field(id)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// sendReceiptDeadline is when the automatic retry window closes for one operation.
func sendReceiptDeadline(created string) string {
	at, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return created
	}
	return db.Timestamp(at.Add(sendReceiptWindow))
}

// answerSendReceipt answers a retry from the operation's durable record instead of creating
// content. The committed message is returned when it still exists and the caller may still read
// it; otherwise the outcome is a conflict that never recreates anything. The two conflicts are
// distinguishable on purpose: one says the send was committed and its content is gone, the other
// says the retry window closed and the unsent text can be recovered and sent again with a new
// key.
func (s *Server) answerSendReceipt(w http.ResponseWriter, r *http.Request, user db.User, receipt db.Receipt, conversationID, intent string) {
	if receipt.ConversationID == "" && receipt.Deleted() {
		problem(w, http.StatusConflict, "That send has expired. Your text can be recovered; send it again with a new key.")
		return
	}
	// Legacy receipts have no intent digest. Their recorded destination still limits
	// replay: permission to post in one room must not reveal a message from another.
	if receipt.ConversationID != conversationID {
		problem(w, http.StatusConflict, "That message key was used for a different message.")
		return
	}
	if receipt.IntentDigest != "" && receipt.IntentDigest != intent {
		problem(w, http.StatusConflict, "That message key was used for a different message.")
		return
	}
	if receipt.ResultMessageID != "" {
		message, err := s.store.Message(r.Context(), user.ID, receipt.ResultMessageID)
		if err == nil {
			if _, _, ok := s.requireScope(w, r, user, message.ConversationID, false); !ok {
				return
			}
			respond(w, http.StatusOK, message)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			s.failed(w, err)
			return
		}
		// The recorded message is no longer readable: deleted, purged, or the caller no longer
		// has access to the conversation. Nothing is recreated either way.
	}
	if receipt.ExpiresAt != "" && receipt.ExpiresAt < db.Now() {
		problem(w, http.StatusConflict, "That send has expired. Your text can be recovered; send it again with a new key.")
		return
	}
	problem(w, http.StatusConflict, "That send was already committed and its message is gone. It was not sent again.")
}

type messageInput struct {
	Content        string   `json:"content"`
	IdempotencyKey string   `json:"idempotency_key"`
	AttachmentIDs  []string `json:"attachment_ids"`
	ReplyToID      string   `json:"reply_to_id"`
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request, user db.User) {
	scope, _, ok := s.requireScope(w, r, user, r.PathValue("id"), false)
	if !ok {
		return
	}
	// A channel's listing is the channel's own conversation, and a thread's replies
	// are not part of it: they are read from the thread. Without this a busy thread
	// buries the channel it lives in, and a client paginating both sees every reply
	// twice.
	clause := "WHERE conversation_id=? AND thread_id IS NULL"
	args := []any{scope.ID}
	limit := pageLimit(r)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from != "" || to != "" {
		_, fromErr := uuid.Parse(from)
		_, toErr := uuid.Parse(to)
		if fromErr != nil || toErr != nil || from > to {
			problem(w, 400, "Choose a valid message range.")
			return
		}
		clause += " AND id>=? AND id<=?"
		args = append(args, from, to)
		limit = 250 // Revalidate one bounded, previously loaded history window.
	}
	if before := r.URL.Query().Get("before"); before != "" {
		clause += " AND id<?"
		args = append(args, before)
	}
	if after := r.URL.Query().Get("after"); after != "" {
		clause += " AND id>?"
		args = append(args, after)
	}
	clause += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	messages, err := s.store.Messages(r.Context(), user.ID, clause, args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	respond(w, 200, messages)
}

func (s *Server) firstUnreadMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	scope, _, ok := s.requireScope(w, r, user, r.PathValue("id"), false)
	if !ok {
		return
	}
	after := r.URL.Query().Get("after")
	if after != "" {
		if _, err := uuid.Parse(after); err != nil {
			problem(w, 400, "Choose a valid read position.")
			return
		}
	}
	var id string
	err := s.store.QueryRowContext(r.Context(), `SELECT id FROM messages WHERE conversation_id=? AND id>? AND user_id<>? ORDER BY id LIMIT 1`, scope.ID, after, user.ID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]string{"message_id": id})
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	message, _, ok := s.requireMessage(w, r, principalOf(r), r.PathValue("id"))
	if !ok {
		return
	}
	respond(w, 200, message)
}

// createThreadMessage resolves the thread to the channel it inherits authorization
// from and then runs the ordinary send against that channel. The only difference is
// that the new message is filed into the thread, which the scope carries.
func (s *Server) createThreadMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		tx.Rollback()
		s.authorityFailed(w, err)
		return
	}
	thread, scope, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), true)
	if err != nil {
		tx.Rollback()
		s.scopeFailed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	// The channel path value is what the shared send resolves, and the thread is
	// carried alongside it so the insert files the message into the thread.
	r.SetPathValue("id", scope.ID)
	r.SetPathValue("thread", thread.ID)
	// The thread's *state* is re-checked inside the send's own committing
	// transaction, not only here. This transaction has already committed by the time
	// the insert happens, so a lock landing in between was decided by a check nothing
	// re-asks: the reply lands in a thread that stopped accepting replies before it
	// was written. The route guard decides whether the handler runs; the committing
	// transaction decides whether the row may be written, and only the second one is
	// close enough to the write to be about it (ADR 0017; completion review 14, V14-04).
	r.SetPathValue("thread_write", "1")
	s.createMessage(w, r, user)
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.limiter.allow("message-send:"+user.ID, 60, time.Minute) || !s.limiter.allow("message-burst:"+user.ID, 10, 5*time.Second) {
		problem(w, 429, "You are sending messages too quickly. Try again shortly.")
		return
	}
	var input messageInput
	if !decode(w, r, &input) {
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if !utf8.ValidString(input.Content) || utf8.RuneCountInString(input.Content) > 4000 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 100 || len(input.AttachmentIDs) > 5 || (input.Content == "" && len(input.AttachmentIDs) == 0) {
		problem(w, 400, "Send up to 4,000 characters and five attachments, with a valid message key.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Authority is re-established inside the write transaction. Access may have
	// been revoked between the route guard and this point, and a queued socket
	// send must not slip through on the older revision.
	scope, principal, err := s.scopeInTx(r.Context(), tx, user, r.PathValue("id"), true)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	channel := scope.Channel
	// One acknowledged send maps to one stored message, and the operation's identity is the
	// returning client's key scoped to the account. The destination and the content are part of
	// the intent, so reusing a key for another conversation or other work is a conflict rather
	// than a silent replay (ADR 0010 / D-11). The receipt is consulted first because it outlives
	// the message: a retry of a send whose content was deleted or purged is answered from the
	// record, never by creating the message again.
	intent := sendIntentDigest(scope.ID, r.PathValue("thread"), input.Content, input.AttachmentIDs, input.ReplyToID)
	if receipt, receiptErr := s.store.Receipt(r.Context(), tx, user.ID, input.IdempotencyKey); receiptErr == nil {
		tx.Rollback()
		s.answerSendReceipt(w, r, user, receipt, scope.ID, intent)
		return
	} else if !errors.Is(receiptErr, sql.ErrNoRows) {
		s.failed(w, receiptErr)
		return
	}
	// A message written before the receipts existed still guards its own key. The receipt
	// backfill gives every such message the same non-recreating outcome, and this lookup keeps
	// the message row itself the second guard.
	var existingID, existingChannel string
	err = tx.QueryRow(`SELECT id,conversation_id FROM messages WHERE user_id=? AND idempotency_key=?`, user.ID, input.IdempotencyKey).Scan(&existingID, &existingChannel)
	if err == nil {
		tx.Rollback()
		if existingChannel != scope.ID {
			problem(w, 409, "That message key belongs to another channel.")
			return
		}
		message, err := s.store.Message(r.Context(), user.ID, existingID)
		if !s.found(w, err) {
			return
		}
		respond(w, 200, message)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	// Claiming staged attachments is new upload work, and staging-time authority
	// does not authorize publication: re-establish the capability inside this
	// transaction, at claim time (review 8, R8-01). The acknowledged and legacy
	// replays above are answered from their records and never reach this check,
	// so a retry still returns its recorded result and a denial here commits no
	// message, receipt or claim.
	if len(input.AttachmentIDs) > 0 && !principal.Allowed(authz.MessageUpload) {
		s.scopeFailed(w, authz.Denial{Reason: "Your role does not include message.upload."})
		return
	}
	// Read the policy inside the same serialized transaction as the send. A
	// concurrent channel edit or another send cannot bypass the interval. Known
	// receipts above remain replayable and never consume another interval.

	sentAt := time.Now().UTC()
	if channel.SlowModeSeconds > 0 && !principal.Allowed(authz.ChannelManage) {
		var lastSent string
		err := tx.QueryRowContext(r.Context(), `SELECT last_sent_at FROM channel_slow_mode_sends WHERE channel_id=? AND user_id=?`, channel.ID, user.ID).Scan(&lastSent)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.failed(w, err)
			return
		}
		if err == nil {
			last, err := time.Parse(time.RFC3339Nano, lastSent)
			if err != nil {
				s.failed(w, fmt.Errorf("read channel slow mode clock: %w", err))
				return
			}
			if remaining := last.Add(time.Duration(channel.SlowModeSeconds) * time.Second).Sub(sentAt); remaining > 0 {
				seconds := int((remaining + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				problem(w, http.StatusTooManyRequests, fmt.Sprintf("Slow mode is on. Wait %d seconds before sending another message in this channel.", seconds))
				return
			}
		}
	}
	id, now := db.ID(), db.Timestamp(sentAt)
	// The operation is claimed before the message it acknowledges, so the key is claimed exactly
	// once even when two requests race: the loser's primary-key violation answers the replay
	// instead of creating a second message.
	if err := s.store.BeginOperation(r.Context(), tx, db.Receipt{
		AccountID: user.ID, Key: input.IdempotencyKey, ConversationID: scope.ID,
		IntentDigest: intent, CreatedAt: now, ExpiresAt: sendReceiptDeadline(now),
	}); err != nil {
		tx.Rollback()
		receipt, receiptErr := s.store.Receipt(r.Context(), s.store, user.ID, input.IdempotencyKey)
		if receiptErr != nil {
			s.failed(w, err)
			return
		}
		s.answerSendReceipt(w, r, user, receipt, scope.ID, intent)
		return
	}
	if input.ReplyToID != "" {
		// A reply is resolved in the destination it is being sent to, so replying to a
		// message in another thread is refused rather than quietly joining two
		// conversations under one parent.
		countClause := `SELECT count(*) FROM messages WHERE id=? AND conversation_id=?`
		countArgs := []any{input.ReplyToID, scope.ID}
		if threadID := r.PathValue("thread"); threadID != "" {
			countClause += ` AND thread_id=?`
			countArgs = append(countArgs, threadID)
		}
		var count int
		if err := tx.QueryRow(countClause, countArgs...).Scan(&count); err != nil {
			s.failed(w, err)
			return
		}
		if count != 1 {
			if r.PathValue("thread") != "" {
				problem(w, 400, "Reply to a message in this thread.")
			} else {
				problem(w, 400, "Reply to an existing message in this channel.")
			}
			return
		}
	}
	if hook := s.beginSendInterleave.Load(); hook != nil {
		(*hook)()
	}
	// A thread's own state is re-read here, immediately before the insert and inside
	// the same transaction as it.
	//
	// The check that admitted the request happened in a transaction that has already
	// committed, so a lock landing between them would be ignored. With the store's
	// single connection that cannot actually happen — a second writer waits for this
	// one — which is exactly why the gap was invisible and why no test can demonstrate
	// the difference. It is kept as defence in depth at the cost of one indexed lookup:
	// the route guard decides whether the handler runs, and the committing transaction
	// decides whether the row may be written, and the second is the only one of the two
	// that is close enough to the write to be about it (ADR 0017;
	// completion review 14, V14-04).
	if threadID := r.PathValue("thread"); threadID != "" {
		var state string
		err := tx.QueryRowContext(r.Context(), `SELECT state FROM threads WHERE id=? AND conversation_id=?`,
			threadID, scope.ID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			// The thread is gone between the check and the write. Nothing is created, and
			// the key is not claimed, so a retry is a fresh attempt rather than a conflict
			// against work that never happened.
			problem(w, 404, "Not found.")
			return
		}
		if err != nil {
			s.failed(w, err)
			return
		}
		if state == "locked" {
			problem(w, 403, "This thread is locked. You can still read it and follow it.")
			return
		}
	}
	// conversation_id records the conversation scope every reader resolves through
	// COALESCE(conversation_id, channel_id). A channel conversation's id is its channel id, so
	// the new column and the legacy column agree for every message this path writes.
	_, err = tx.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key,reply_to_id,thread_id) VALUES(?,?,NULLIF(?,''),?,?,?,?,NULLIF(?,''),NULLIF(?,''))`, id, scope.ID, scope.ChannelID, principal.UserID, input.Content, now, input.IdempotencyKey, input.ReplyToID, r.PathValue("thread"))
	if err != nil {
		s.failed(w, err)
		return
	}
	for _, attachmentID := range input.AttachmentIDs {
		result, err := tx.Exec(`UPDATE attachments SET message_id=? WHERE id=? AND uploader_id=? AND message_id IS NULL AND (staged_conversation_id=? OR (staged_conversation_id IS NULL AND ?='channel'))`, id, attachmentID, user.ID, scope.ID, scope.Kind)
		if err != nil {
			s.failed(w, err)
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			s.failed(w, err)
			return
		}
		if count != 1 {
			problem(w, 400, "An attachment is unavailable or belongs to another user.")
			return
		}
	}
	// Durable activity is written here, inside the transaction that creates the
	// message, so a delivered message always has its activity and a rolled-back send
	// has neither. The row carries the message id and not its content: activity is a
	// pointer to something that is read under its own authorization, never a second
	// copy of private text that would outlive it (ADR 0015).
	if err := s.recordDirectMessageActivity(r.Context(), tx, scope, principal.UserID, id, sentAt); err != nil {
		s.failed(w, err)
		return
	}
	if threadID := r.PathValue("thread"); threadID != "" {
		if err := s.recordThreadReplyActivity(r.Context(), tx, scope, principal.UserID, threadID, id, sentAt); err != nil {
			s.failed(w, err)
			return
		}
	}
	// The receipt is completed in the same transaction as the message, so a committed message
	// always has its receipt and a rolled-back send has neither.
	if err := s.store.CompleteOperation(r.Context(), tx, user.ID, input.IdempotencyKey, id); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE conversations SET updated_at=? WHERE id=?`, now, scope.ID); err != nil {
		s.failed(w, err)
		return
	}
	targets := []string{}
	if scope.Kind == "channel" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO channel_slow_mode_sends(channel_id,user_id,last_sent_at) VALUES(?,?,?) ON CONFLICT(channel_id,user_id) DO UPDATE SET last_sent_at=excluded.last_sent_at`, channel.ID, user.ID, now); err != nil {
			s.failed(w, err)
			return
		}
		eligible, err := s.channelEligible(r.Context(), tx, channel)
		if err != nil {
			s.failed(w, err)
			return
		}
		targets, err = syncMentions(r.Context(), tx, id, channel.ID, user, input.Content, eligible, principal.Allowed(authz.MentionEveryone))
		if err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	message, err := s.store.Message(r.Context(), user.ID, id)
	if err != nil {
		s.failed(w, err)
		return
	}
	s.publishMessage("message.created", message)
	s.queuePreview(message)
	s.notifyMentions(message, user, targets)
	respond(w, 201, message)
}

func (s *Server) editMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	// Existence is private too. Establish readable scope before distinguishing
	// another author's message from the caller's own editable content.
	message, _, ok := s.requireMessage(w, r, principalOf(r), r.PathValue("id"))
	if !ok {
		return
	}
	if message.UserID != user.ID {
		problem(w, 403, "You can only edit your own messages.")
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if !utf8.ValidString(input.Content) || utf8.RuneCountInString(input.Content) > 4000 || (input.Content == "" && len(message.Attachments) == 0) {
		problem(w, 400, "Messages must contain text or an attachment, with at most 4,000 characters.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	scope, principal, err := s.scopeInTx(r.Context(), tx, user, message.ConversationID, true)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	var author string
	if err := tx.QueryRowContext(r.Context(), `SELECT user_id FROM messages WHERE id=? AND conversation_id=?`, message.ID, scope.ID).Scan(&author); err != nil {
		s.scopeFailed(w, err)
		return
	}
	if author != user.ID {
		problem(w, 403, "You can only edit your own messages.")
		return
	}

	_, err = tx.Exec(`UPDATE messages SET content=?,edited_at=?,embed_json=NULL,embed_status='pending' WHERE id=?`, input.Content, db.Now(), message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if scope.Kind == "channel" {
		eligible, err := s.channelEligible(r.Context(), tx, scope.Channel)
		if err != nil {
			s.failed(w, err)
			return
		}
		if _, err := syncMentions(r.Context(), tx, message.ID, scope.ChannelID, user, input.Content, eligible, principal.Allowed(authz.MentionEveryone)); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	message, err = s.store.Message(r.Context(), user.ID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	s.publishMessage("message.updated", message)
	s.queuePreview(message)
	respond(w, 200, message)
}

func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	message, _, ok := s.requireMessage(w, r, principal, r.PathValue("id"))
	if !ok {
		return
	}
	if message.UserID != user.ID && !principal.Allowed(authz.MessageDeleteAny) {
		problem(w, 403, "You can only delete your own messages.")
		return
	}
	if !confirmed(w, r, message.ID) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	scope, current, err := s.scopeInTx(r.Context(), tx, user, message.ConversationID, false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	var author string
	if err := tx.QueryRowContext(r.Context(), `SELECT user_id FROM messages WHERE id=? AND conversation_id=?`, message.ID, scope.ID).Scan(&author); err != nil {
		s.scopeFailed(w, err)
		return
	}
	if author != user.ID && (scope.Kind != "channel" || !current.Allowed(authz.MessageDeleteAny)) {
		problem(w, 403, "You can only delete your own messages.")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM messages WHERE id=?`, message.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditCommunityMessageTx(r.Context(), tx, scope, user.ID, "message.deleted", message.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishMessage("message.deleted", message)
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.limiter.allow("search:"+user.ID, 30, time.Minute) {
		problem(w, 429, "Search limit reached. Try again in a minute.")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 200 {
		problem(w, 400, "Search for 1–200 characters.")
		return
	}
	// Quoted terms disable FTS operators and malformed query syntax.
	terms := strings.Fields(query)
	for i, term := range terms {
		terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	principal := principalOf(r)
	// Eligibility is resolved inside the query, before the limit and before the
	// count, so a hidden conversation contributes no hit, excerpt or total.
	clause, args := db.ReadableChannelScope(viewer(principal))
	clause += " AND channel_id IS NOT NULL"
	if id := r.URL.Query().Get("conversation_id"); id != "" {
		scope, _, ok := s.requireScope(w, r, user, id, false)
		if !ok {
			return
		}
		clause = "WHERE conversation_id=?"
		args = []any{scope.ID}
	}
	clause += `
  AND rowid IN (SELECT rowid FROM message_fts WHERE message_fts MATCH ?)`
	args = append(args, strings.Join(terms, " AND "))
	if channel := r.URL.Query().Get("channel_id"); channel != "" {
		channelRow, _, readable, err := s.channelAccess(r.Context(), principal, channel)
		if err != nil {
			s.failed(w, err)
			return
		}
		if !readable {
			hidden(w)
			return
		}
		clause += " AND channel_id=?"
		args = append(args, channelRow.ID)
	}
	if before := r.URL.Query().Get("before"); before != "" {
		clause += " AND id<?"
		args = append(args, before)
	}
	args = append(args, pageLimit(r))
	messages, err := s.store.Messages(r.Context(), user.ID, clause+" ORDER BY id DESC LIMIT ?", args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, messages)
}

func pageLimit(r *http.Request) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		return 50
	}
	return min(limit, 50)
}
