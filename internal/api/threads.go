package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Threads are a grouping inside a channel conversation, not a second message store
// (ADR 0017). Every handler here resolves the thread to its channel and then goes
// through the same conversationAccess the channel's own messages use, so a thread can
// never become a way around a channel restriction: a reader's access to a thread
// changes exactly when their access to the channel does.

// threadSummary is the channel-list view. It is deliberately small — the thread's
// subject, its state and when it last moved — because the channel list is loaded
// whole and a thread's replies are not in it.
type threadSummary struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	RootMessageID  string `json:"root_message_id"`
	// RootDeletedAt is when the thread's subject was removed. A thread without a root
	// is a tombstone, not a broken record, and the interface says so rather than
	// presenting an unexplained empty first line.
	RootDeletedAt string `json:"root_deleted_at,omitempty"`
	Title         string `json:"title"`
	State         string `json:"state"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	ReplyCount    int    `json:"reply_count"`
	LastReplyAt   string `json:"last_reply_at"`
	Followers     int    `json:"followers"`
	Following     bool   `json:"following"`
	// Unread counts the replies after this reader's own thread cursor, and
	// UnreadMentions the mentions among them. A thread's reading position is its own:
	// these numbers never move with the channel's cursor and never move it.
	Unread         int `json:"unread"`
	UnreadMentions int `json:"unread_mentions"`
}

// resolveThread finds a thread and the channel it inherits from. A thread that does
// not exist and one whose channel the reader cannot see are both unavailable, so a
// thread id cannot be used to probe for a channel's existence.
func (s *Server) resolveThread(ctx context.Context, q db.Queryer, id string) (threadSummary, messageScope, error) {
	var t threadSummary
	var scope messageScope
	// A tombstoned thread has no root, so the column is nullable: `COALESCE` keeps the
	// scan total rather than failing a thread whose subject was withdrawn. The moment is
	// read too, so the interface can say *why* there is no first message.
	err := q.QueryRowContext(ctx, `SELECT id,conversation_id,COALESCE(root_message_id,''),title,state,COALESCE(created_by,''),created_at,updated_at,COALESCE(root_deleted_at,'')
		FROM threads WHERE id=?`, id).
		Scan(&t.ID, &t.ConversationID, &t.RootMessageID, &t.Title, &t.State, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.RootDeletedAt)
	if err != nil {
		return t, scope, err
	}
	scope.ID = t.ConversationID
	return t, scope, nil
}

// threadAccess resolves a thread for a principal, applying the channel's own read or
// post rules and the thread's own state. The order matters: the channel is checked
// first so that a locked thread in a channel the reader cannot see is still not
// found, rather than reported as locked.
func (s *Server) threadAccess(ctx context.Context, q db.Queryer, principal authz.Principal, id string, write bool) (threadSummary, messageScope, error) {
	var t threadSummary
	t, scope, err := s.resolveThread(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return t, scope, authz.ErrUnavailable
	}
	if err != nil {
		return t, scope, err
	}
	// The channel's authorization, unchanged. A thread in a restricted channel is as
	// restricted as the channel, and a thread never widens what its channel allows.
	scope, err = s.conversationAccess(ctx, q, principal, t.ConversationID, write)
	if err != nil {
		return t, scope, err
	}
	// A thread is only ever a channel thread; the schema cannot say otherwise, and a
	// direct or group conversation has no channels to inherit.
	if scope.Kind != "channel" {
		return t, scope, authz.ErrUnavailable
	}
	// Locked and archived both stop replies and leave everything else alone. They are
	// statements about writing, not about access, which is why a locked or archived
	// thread is still readable, searchable and followed. They differ in intent —
	// locked is a pause, archived is out of the way until somebody reopens it — and
	// the refusal says which state the writer must resolve.
	if write && t.State == "archived" {
		return t, scope, refuse(403, "This thread is archived. Reopen it to reply.")
	}
	if write && t.State == "locked" {
		return t, scope, refuse(403, "This thread is locked. You can still read it and follow it.")
	}
	return t, scope, nil
}

func (s *Server) countThread(ctx context.Context, q db.Queryer, id string) (replies, followers int, last string) {
	_ = q.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(created_at),'') FROM messages WHERE thread_id=?`, id).Scan(&replies, &last)
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM thread_follows WHERE thread_id=?`, id).Scan(&followers)
	return replies, followers, last
}

// listThreads is the channel's thread list. Archived threads are omitted unless asked
// for, because that is what "archived" means: it is out of the way, still there, and
// reopening it restores the list entry.
func (s *Server) listThreads(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// Reuse the channel's own gate rather than repeating it: a list of threads must
	// never be reachable by someone who cannot read the channel it belongs to.
	if _, err := s.conversationAccess(r.Context(), tx, principal, r.PathValue("id"), false); err != nil {
		s.scopeFailed(w, err)
		return
	}
	wantArchived := r.URL.Query().Get("archived") == "1"
	clause := `SELECT t.id,t.conversation_id,COALESCE(t.root_message_id,''),t.title,t.state,COALESCE(t.created_by,''),t.created_at,t.updated_at,COALESCE(t.root_deleted_at,''),
		(SELECT count(*) FROM messages m WHERE m.thread_id=t.id),
		(SELECT COALESCE(max(m.created_at),'') FROM messages m WHERE m.thread_id=t.id),
		(SELECT count(*) FROM thread_follows f WHERE f.thread_id=t.id),
		(SELECT count(*) FROM thread_follows f WHERE f.thread_id=t.id AND f.user_id=?),
		(SELECT count(*) FROM messages m WHERE m.thread_id=t.id AND m.user_id<>? AND m.id>COALESCE(tr.last_read_message_id,'')),
		(SELECT count(*) FROM mentions mn WHERE mn.target_user_id=? AND mn.message_id IN
			(SELECT m.id FROM messages m WHERE m.thread_id=t.id AND m.id>COALESCE(tr.last_read_message_id,'')))
		FROM threads t LEFT JOIN thread_reads tr ON tr.thread_id=t.id AND tr.user_id=?
		WHERE t.conversation_id=?`
	args := []any{user.ID, user.ID, user.ID, user.ID, r.PathValue("id")}
	if !wantArchived {
		clause += ` AND t.state!='archived'`
	}
	clause += ` ORDER BY t.updated_at DESC LIMIT 200`
	rows, err := tx.QueryContext(r.Context(), clause, args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	threads := []threadSummary{}
	for rows.Next() {
		var t threadSummary
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.RootMessageID, &t.Title, &t.State, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.RootDeletedAt,
			&t.ReplyCount, &t.LastReplyAt, &t.Followers, &t.Following, &t.Unread, &t.UnreadMentions); err != nil {
			s.failed(w, err)
			return
		}
		threads = append(threads, t)
	}
	respond(w, 200, threads)
}

// createThread starts a thread from an existing message. The root is an ordinary
// message that keeps its identity and its link: promotion records a relationship, it
// does not re-parent or rewrite the message, so anything already pointing at it
// still resolves (ADR 0017, "Replies keep their identity").
func (s *Server) createThread(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Title         string `json:"title"`
		RootMessageID string `json:"root_message_id"`
		Follow        *bool  `json:"follow"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	// A title is a label a person reads, not a lookup key, and it is bounded so a
	// channel list cannot be filled with unreadable entries.
	if !utf8.ValidString(input.Title) || utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 120 {
		problem(w, 400, "Give the thread a name of at most 120 characters.")
		return
	}
	if _, err := uuid.Parse(input.RootMessageID); err != nil {
		problem(w, 400, "Start a thread from an existing message.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Post authority is re-established inside the transaction. Starting a thread is
	// writing to the channel, so it is gated on the channel's post rule rather than
	// its read rule.
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	conversationID := r.PathValue("id")
	scope, err := s.conversationAccess(r.Context(), tx, principal, conversationID, true)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	if scope.Kind != "channel" {
		hidden(w)
		return
	}
	var rootConversation, rootThread string
	err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(conversation_id,''),COALESCE(thread_id,'') FROM messages WHERE id=?`, input.RootMessageID).
		Scan(&rootConversation, &rootThread)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 400, "Start a thread from an existing message in this channel.")
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if rootConversation != conversationID {
		// The message exists but not here. Saying so would confirm its existence in a
		// channel the reader may not be in, so this is the same refusal as a message
		// that does not exist at all.
		problem(w, 400, "Start a thread from an existing message in this channel.")
		return
	}
	// A message already inside a thread cannot be a second root. Promoting a reply
	// into a nested thread is not something the product offers, and silently
	// re-parenting an existing thread's root would orphan everything in it.
	if rootThread != "" {
		var state string
		_ = tx.QueryRowContext(r.Context(), `SELECT state FROM threads WHERE id=?`, rootThread).Scan(&state)
		if state != "archived" {
			problem(w, 409, "That message is already part of a thread.")
			return
		}
	}
	var existing string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM threads WHERE root_message_id=?`, input.RootMessageID).Scan(&existing)
	if err == nil {
		tx.Rollback()
		t, _, accessErr := s.threadAccess(r.Context(), s.store, principal, existing, false)
		if accessErr != nil {
			s.scopeFailed(w, accessErr)
			return
		}
		_ = scope
		respond(w, 200, s.threadWithCounts(r, user, t))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	now := db.Timestamp(time.Now().UTC())
	id := db.ID()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO threads(id,conversation_id,root_message_id,title,state,created_by,created_at,updated_at) VALUES(?,?,?,?,'active',?,?,?)`,
		id, conversationID, input.RootMessageID, input.Title, principal.UserID, now, now); err != nil {
		s.failed(w, err)
		return
	}
	// The root joins its own thread, so every message in a thread is reachable by one
	// filter and the channel listing can exclude thread replies entirely.
	if _, err := tx.ExecContext(r.Context(), `UPDATE messages SET thread_id=? WHERE id=?`, id, input.RootMessageID); err != nil {
		s.failed(w, err)
		return
	}
	if input.Follow == nil || *input.Follow {
		// Starting a thread you are in is the common case, and following it is the
		// useful default. It is bounded below, so the default cannot grow a table.
		if err := s.followThread(r.Context(), tx, principal.UserID, id, now); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishThread("thread.created", id, conversationID)
	thread, scope, err := s.threadAccess(r.Context(), s.store, principal, id, false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	_ = scope
	respond(w, 201, s.threadWithCounts(r, user, thread))
}

// threadWithCounts fills the summary fields that are computed rather than stored.
func (s *Server) threadWithCounts(r *http.Request, user db.User, t threadSummary) threadSummary {
	t.ReplyCount, t.Followers, t.LastReplyAt = s.countThread(r.Context(), s.store, t.ID)
	// The reader's own position in this thread, not the channel's: the two projections
	// are independent, so opening a thread neither clears the channel's count nor is
	// cleared by the channel being read.
	_ = s.store.QueryRowContext(r.Context(), `SELECT
		(SELECT count(*) FROM messages m WHERE m.thread_id=t.id AND m.user_id<>? AND m.id>COALESCE(tr.last_read_message_id,'')),
		(SELECT count(*) FROM mentions mn WHERE mn.target_user_id=? AND mn.message_id IN
			(SELECT m.id FROM messages m WHERE m.thread_id=t.id AND m.id>COALESCE(tr.last_read_message_id,'')))
		FROM threads t LEFT JOIN thread_reads tr ON tr.thread_id=t.id AND tr.user_id=?
		WHERE t.id=?`, user.ID, user.ID, user.ID, t.ID).Scan(&t.Unread, &t.UnreadMentions)
	if err := s.store.QueryRowContext(r.Context(), `SELECT 1 FROM thread_follows WHERE thread_id=? AND user_id=?`, t.ID, user.ID).Scan(new(int)); err == nil {
		t.Following = true
	}
	return t
}

// followThread applies the bound on the write rather than by a later sweep, so
// following cannot be used to grow a table without limit. The starter of a thread
// always keeps their own follow: the bound excludes them rather than refusing the
// first follow, which would be a surprising way to learn the limit.
func (s *Server) followThread(ctx context.Context, q db.Queryer, userID, threadID, now string) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM thread_follows WHERE user_id=?`, userID).Scan(&count); err != nil {
		return err
	}
	var starter string
	_ = q.QueryRowContext(ctx, `SELECT COALESCE(created_by,'') FROM threads WHERE id=?`, threadID).Scan(&starter)
	if err := q.QueryRowContext(ctx, `SELECT 1 FROM thread_follows WHERE thread_id=? AND user_id=?`, threadID, userID).Scan(new(int)); err == nil {
		return nil
	}
	if count >= maxFollowedThreads && starter != userID {
		return refuse(409, fmt.Sprintf("You are following %d threads, which is the limit. Unfollow one to follow another.", maxFollowedThreads))
	}
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO thread_follows(user_id,thread_id,created_at) VALUES(?,?,?)`, userID, threadID, now)
	return err
}

// maxFollowedThreads bounds one account's subscriptions. It is a per-account bound,
// not a server bound: a busy member following many threads is not a storage problem,
// and a limit chosen to protect the table should not be the one a member feels first.
const maxFollowedThreads = 200

func (s *Server) setThreadFollow(w http.ResponseWriter, r *http.Request, user db.User, follow bool) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	thread, _, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	now := db.Timestamp(time.Now().UTC())
	if follow {
		if err := s.followThread(r.Context(), tx, principal.UserID, thread.ID, now); err != nil {
			var refused problemError
			if errors.As(err, &refused) {
				problem(w, refused.Status, refused.Message)
			} else {
				s.failed(w, err)
			}
			return
		}
	} else if _, err := tx.ExecContext(r.Context(), `DELETE FROM thread_follows WHERE thread_id=? AND user_id=?`, thread.ID, principal.UserID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]bool{"following": follow})
}

// setThreadState archives, reopens or locks a thread. It is one handler rather than
// three because they are one decision — what a thread's state is — and three copies
// would be three places to keep the audit and the event in step.
func (s *Server) setThreadState(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		State string `json:"state"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.State != "active" && input.State != "archived" && input.State != "locked" {
		problem(w, 400, "A thread is active, archived or locked.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	thread, _, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	// Managing a thread is channel work: it changes what everyone in the channel can
	// write, so it takes the channel's management capability rather than being open
	// to anyone who can post. A member who can post is not automatically someone who
	// gets to decide whether the rest of the channel can still post here.
	if !s.manageThread(principal) {
		s.scopeFailed(w, authz.Denial{Reason: "You do not have permission to manage threads in this channel."})
		return
	}
	if thread.State == input.State {
		tx.Rollback()
		respond(w, 200, thread)
		return
	}
	now := db.Timestamp(time.Now().UTC())
	if _, err := tx.ExecContext(r.Context(), `UPDATE threads SET state=?,updated_at=? WHERE id=?`, input.State, now, thread.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "thread."+input.State, thread.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishThread("thread."+input.State, thread.ID, thread.ConversationID)
	thread.State = input.State
	thread.UpdatedAt = now
	respond(w, 200, thread)
}

// manageThread is the channel's own management decision, unchanged. It is deliberately
// not "can post here": archiving or locking a thread changes what every other member
// of the channel can write, so it takes exactly the authority the channel edit takes
// and nothing looser. Anything else would let a member who cannot edit the channel
// decide who may post in it.
func (s *Server) manageThread(principal authz.Principal) bool {
	return principal.Allowed(authz.ChannelManage)
}

// publishThread announces a thread state change to the channel's audience only after
// the commit, so a rolled-back change never reaches a socket.
func (s *Server) publishThread(kind, threadID, conversationID string) {
	s.publishToConversation(context.Background(), conversationID, kind,
		map[string]string{"thread_id": threadID, "conversation_id": conversationID})
}

// getThread returns one thread with its counts. It is the route a deep link uses, so
// it resolves the channel before returning anything: a link to a thread in a channel
// the caller cannot read must not confirm the thread exists.
func (s *Server) getThread(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	thread, _, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	// The store is a single connection. Committing before the counts are read is not
	// tidiness: leaving this transaction open and then querying s.store would wait for
	// the connection this transaction is already holding, forever.
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, s.threadWithCounts(r, user, thread))
}

// listThreadMessages pages one thread. The channel's messages exclude thread replies
// entirely, so a busy thread can neither bury the channel it lives in nor be reached
// twice by a client that paginates both. A 1,000-reply thread is fully reachable one
// bounded page at a time, which is the property that makes a large thread usable
// rather than merely stored (ADR 0017, "Bounds").
func (s *Server) listThreadMessages(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	thread, _, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	// The same single-connection rule: the authorization transaction is closed before
	// the page is read, so the read uses the store directly.
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	clause := "WHERE thread_id=?"
	args := []any{thread.ID}
	// A thread root whose message was deleted is a tombstone, not a vanished subject:
	// the replies stay readable and say what they were attached to. A thread whose root
	// is gone is removed with it, so this only happens if the root is unreadable.
	if before := r.URL.Query().Get("before"); before != "" {
		clause += " AND id<?"
		args = append(args, before)
	}
	if after := r.URL.Query().Get("after"); after != "" {
		clause += " AND id>?"
		args = append(args, after)
	}
	clause += " ORDER BY id DESC LIMIT ?"
	args = append(args, pageLimit(r))
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

func (s *Server) followThreadRoute(w http.ResponseWriter, r *http.Request, user db.User) {
	s.setThreadFollow(w, r, user, true)
}

func (s *Server) unfollowThreadRoute(w http.ResponseWriter, r *http.Request, user db.User) {
	s.setThreadFollow(w, r, user, false)
}

// recordThreadReplyActivity addresses a new reply to the people following the thread,
// inside the sending transaction. Following is a personal subscription and nothing
// else, so this is exactly the follower set and nobody else: not every channel member,
// and not the sender even if they follow their own thread.
func (s *Server) recordThreadReplyActivity(ctx context.Context, tx *sql.Tx, scope messageScope, senderID, threadID, messageID string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT f.user_id FROM thread_follows f JOIN users u ON u.id=f.user_id
		WHERE f.thread_id=? AND f.user_id<>? AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0`, threadID, senderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	recipients := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		recipients = append(recipients, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, recipientID := range recipients {
		if err := s.recordActivity(ctx, tx, recipientID, activityThreadReply, messageID, scope.ID, senderID, now); err != nil {
			return err
		}
	}
	// The thread's own timestamp is the thread's position in the channel list, so a
	// reply moves it whether or not anyone follows it.
	_, err = tx.ExecContext(ctx, `UPDATE threads SET updated_at=? WHERE id=?`, db.Timestamp(now), threadID)
	return err
}

// markThreadRead moves the reader's position in one thread, and only in that thread.
//
// A thread is a conversation with a subject, and it moves at its own rate: a quiet
// thread beside a busy channel, or a busy thread nobody has opened, are the ordinary
// cases rather than the edge ones. Sharing the channel's cursor made both
// unanswerable — a member who read forty replies in a thread had, on the only cursor
// that existed, read the channel as far as the thread's newest reply, and a member who
// read the channel had marked a thread they never opened as read.
//
// The position is personal, so a request admitted before sign-out or account removal
// cannot recreate it after the identity cleanup committed, exactly as for a channel.
func (s *Server) markThreadRead(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		MessageID string `json:"last_read_message_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// The channel's own access rule still decides: a thread is only as restricted as
	// the channel it inherits from, so a member who has lost the channel has lost the
	// thread and cannot move a cursor in it.
	thread, _, err := s.threadAccess(r.Context(), tx, principal, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	// The position may only be a message of *this* thread. A cursor in one thread that
	// points at a message in another would make the next unread count meaningless.
	var id string
	err = tx.QueryRowContext(r.Context(),
		`SELECT id FROM messages WHERE id=? AND thread_id=?`, input.MessageID, thread.ID).Scan(&id)
	if !s.found(w, err) {
		return
	}
	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO thread_reads(user_id,thread_id,last_read_message_id,last_read_at) VALUES(?,?,?,?)
		 ON CONFLICT(user_id,thread_id) DO UPDATE SET last_read_message_id=max(last_read_message_id,excluded.last_read_message_id),last_read_at=excluded.last_read_at`,
		user.ID, thread.ID, id, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]string{"thread_id": thread.ID, "last_read_message_id": id})
}
