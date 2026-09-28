package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Private message evidence (ADR 0013).
//
// A private report is one voluntarily submitted, immutable snapshot of a single
// message in an accepted direct conversation, plus the reporter's explanation. It
// is not a message store, not a route into a conversation, and not a general
// moderation surface: nothing here discloses anything the reporter did not
// explicitly choose to submit, and a reviewer gains no membership from holding it.
//
// The mechanics — 90-day expiry from creation, read-filtered pagination, review
// notes, resolve/reopen, bounded cleanup — mirror the community report path on
// purpose. The disclosure boundary does not: community evidence is reviewed through
// current channel read access plus message.delete_any, and private evidence is
// reviewed only through report.review_private, which is checked inside every
// transaction so a revoked reviewer cannot read a later page or commit a mutation.

const (
	// privateReportEvidenceLifetime is 90 days from creation. Resolve and reopen
	// never move it, so it is written once at submission and only ever read.
	privateReportEvidenceLifetime = reportEvidenceLifetime
	// privateReportHourlyLimit is the durable per-account submission bound. Unlike
	// the community limiter, which is an in-memory sliding window a restart
	// resets, this is counted in the database: private evidence is disclosed data
	// with a storage cost, and a limit a restart clears is not a limit.
	privateReportHourlyLimit  = 10
	privateReportHourlyWindow = time.Hour
	// privateReportStorageLimit bounds unexpired evidence per installation. When it
	// is reached, submission is refused with a clear error rather than evicting a
	// record the reporter submitted and a reviewer may be mid-triage on.
	privateReportStorageLimit = 5000
)

// privateMessageReport is reviewer-only evidence. Submission returns a receipt, never
// this shape, and the receipt names no reviewer note, no other reporter and no
// other reporter's reason.
type privateMessageReport struct {
	ID             string `json:"id"`
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	ReporterID     string `json:"reporter_id"`
	AuthorID       string `json:"author_id"`
	Reason         string `json:"reason"`
	SnapshotText   string `json:"snapshot_text"`
	Status         string `json:"status"`
	ReviewNote     string `json:"review_note"`
	ReviewedBy     string `json:"reviewed_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ExpiresAt      string `json:"expires_at"`
}

const privateReportColumns = `id,message_id,conversation_id,COALESCE(reporter_id,''),COALESCE(author_id,''),reason,snapshot_text,status,review_note,COALESCE(reviewed_by,''),created_at,updated_at,expires_at`

func scanPrivateReport(row interface{ Scan(...any) error }) (privateMessageReport, error) {
	var report privateMessageReport
	err := row.Scan(&report.ID, &report.MessageID, &report.ConversationID, &report.ReporterID, &report.AuthorID,
		&report.Reason, &report.SnapshotText, &report.Status, &report.ReviewNote, &report.ReviewedBy,
		&report.CreatedAt, &report.UpdatedAt, &report.ExpiresAt)
	return report, err
}

// privateEvidencePreview is what the reporter is shown and then confirms. The
// preview is produced by the server from the message's current state, never from
// client text, and it carries a version derived from that state so a message edited
// between preview and confirmation cannot be submitted behind the reporter's back.
type privateEvidencePreview struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	AuthorID       string `json:"author_id"`
	AuthorName     string `json:"author_name"`
	SnapshotText   string `json:"snapshot_text"`
	HasAttachment  bool   `json:"has_attachment"`
	Version        string `json:"version"`
	ExpiresAt      string `json:"expires_at"`
	ReviewerCount  int    `json:"reviewer_count"`
}

// changedPreview is the refusal for a message that no longer matches the preview the
// reporter confirmed. It carries a fresh preview so the client can re-disclose,
// rather than a bare failure the reporter cannot act on.
type changedPreview struct {
	Error   string                  `json:"error"`
	Changed bool                    `json:"changed"`
	Preview *privateEvidencePreview `json:"preview,omitempty"`
}

// previewPrivateEvidence returns the exact snapshot that would be saved, for a
// message the caller can currently read in an accepted direct conversation.
//
// Read access is the requirement, not write access: a participant who has blocked
// the other person keeps readable history and must still be able to report it.
// conversationAccess(..., write=false) is therefore deliberate here and in
// submitPrivateReport; using the write path would silently deny a blocked victim
// the one action that matters most.
func (s *Server) previewPrivateEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
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
	preview, found, err := s.privateEvidencePreviewInTx(r.Context(), tx, principal, r.PathValue("id"))
	if err != nil {
		s.failed(w, err)
		return
	}
	if !found {
		hidden(w)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, preview)
}

// privateEvidencePreviewInTx resolves one message into the snapshot that a report
// would save, or reports that the caller may not read it. The two failure modes are
// deliberately indistinguishable: a message that does not exist, a message in a
// conversation the caller is not an accepted member of, and a message belonging to
// a community channel all produce one 404, so guessing an id discloses nothing.
func (s *Server) privateEvidencePreviewInTx(ctx context.Context, tx *sql.Tx, principal authz.Principal, messageID string) (*privateEvidencePreview, bool, error) {
	// Only a direct conversation is eligible. A community message is reported
	// through the community queue, and admitting it here would give a private
	// reviewer a second route into content their grant does not scope.
	conversationID, err := messageConversation(ctx, tx, messageID)
	if err != nil {
		return nil, false, err
	}
	scope, err := s.conversationAccess(ctx, tx, principal, conversationID, false)
	if err != nil {
		if errors.Is(err, authz.ErrUnavailable) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if scope.Kind != "direct" {
		return nil, false, nil
	}
	preview, err := s.privateEvidenceForMessage(ctx, tx, principal, messageID, scope.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return preview, true, nil
}

// messageConversation returns the conversation a message belongs to, or an empty
// string when the message does not exist. An empty id fails the access check, so a
// guessed message id is indistinguishable from one that is not there.
func messageConversation(ctx context.Context, q db.Queryer, messageID string) (string, error) {
	var conversationID string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(conversation_id,channel_id,'') FROM messages WHERE id=?`, messageID).Scan(&conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return conversationID, err
}

// privateEvidenceVersion is the fixed-size token that ties a confirmation to the
// exact text the reporter was shown. It is a digest of the message content and its
// edit state, so it is the same length for every message, is not the message, and
// changes whenever either input does.
func privateEvidenceVersion(content, editedAt string) string {
	digest := sha256.New()
	// Length-prefixed so no pair of distinct inputs can share one digest input.
	for _, part := range []string{content, editedAt} {
		fmt.Fprintf(digest, "%d:", len(part))
		io.WriteString(digest, part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

const (
	// privateEvidenceVersionLength is the hex length of a SHA-256 digest, fixed so
	// the version carries no part of the message and cannot grow with it.
	privateEvidenceVersionLength = 64
)

func isHex(value string) bool {
	if len(value) == 0 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func (s *Server) privateEvidenceForMessage(ctx context.Context, tx *sql.Tx, principal authz.Principal, messageID, conversationID string) (*privateEvidencePreview, error) {
	var content, authorID, editedAt string
	var attachments int
	// The snapshot is exactly this one message. No joins reach replies, context or
	// surrounding history, and attachments are counted only so the disclosure can
	// tell the reporter that their evidence contains no file content.
	err := tx.QueryRowContext(ctx, `SELECT m.conversation_id,m.user_id,m.content,COALESCE(m.edited_at,m.created_at),
		(SELECT count(*) FROM attachments a WHERE a.message_id=m.id) FROM messages m WHERE m.id=? AND m.conversation_id=?`,
		messageID, conversationID).Scan(&conversationID, &authorID, &content, &editedAt, &attachments)
	if err != nil {
		return nil, err
	}
	var authorName string
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(display_name,''),username) FROM users WHERE id=?`, authorID).Scan(&authorName)
	text := []rune(content)
	if len(text) > 4000 {
		text = text[:4000]
	}
	reviewers, err := s.privateEvidenceReviewerCount(ctx, tx)
	if err != nil {
		return nil, err
	}
	return &privateEvidencePreview{
		MessageID:      messageID,
		ConversationID: conversationID,
		AuthorID:       authorID,
		AuthorName:     authorName,
		SnapshotText:   string(text),
		HasAttachment:  attachments > 0,
		// The version identifies the exact text the reporter confirmed, and it is
		// bounded so it cannot be turned into a transport for the message itself.
		// The whole content cannot be used: a valid 4,000-character CJK or emoji
		// message is 8,000+ bytes, and a version that grew with the message made
		// such messages unreportable while telling the reporter their preview was
		// merely stale (completion review 10, R10-01). A digest over the content
		// and its edit state is fixed-size and still changes if and only if the
		// text a preview described is no longer the text there.
		Version:       privateEvidenceVersion(content, editedAt),
		ExpiresAt:     db.Timestamp(time.Now().Add(privateReportEvidenceLifetime)),
		ReviewerCount: reviewers,
	}, nil
}

// privateEvidenceReviewerCount is how many accounts can currently read private
// evidence. It is shown to the reporter as part of the disclosure, so it is
// computed from current authority rather than hard-coded, and it counts only
// accounts that would actually be admitted.
func (s *Server) privateEvidenceReviewerCount(ctx context.Context, q db.Queryer) (int, error) {
	authorities, err := s.store.AccountAuthorities(ctx, q)
	if err != nil {
		return 0, err
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, q)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, authority := range authorities {
		if principalFor(authority, owners, established, revision).Allowed(authz.ReportReviewPrivate) {
			count++
		}
	}
	return count, nil
}

// submitPrivateEvidence stores the reporter's confirmed snapshot.
//
// The submitted body carries only the reason and the version the reporter
// confirmed. There is deliberately no text field: the saved evidence is the
// server's current text, so a client cannot present fabricated wording as server
// evidence, and an edit between preview and confirmation is refused with a fresh
// preview instead of being silently disclosed.
func (s *Server) submitPrivateEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Reason  string `json:"reason"`
		Version string `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || utf8.RuneCountInString(input.Reason) > 2000 {
		problem(w, http.StatusBadRequest, "Explain your report in 1–2,000 characters.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Identity, session, capability and read access are all re-established inside
	// the transaction that writes the evidence, so a grant or an account change
	// landing after the route guard cannot produce a record.
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	messageID := r.PathValue("id")
	conversationID, err := messageConversation(r.Context(), tx, messageID)
	if err != nil {
		s.failed(w, err)
		return
	}
	scope, err := s.conversationAccess(r.Context(), tx, principal, conversationID, false)
	if err != nil {
		hidden(w)
		return
	}
	if scope.Kind != "direct" {
		hidden(w)
		return
	}
	// The version is a fixed-size digest, so this bounds its *shape* rather than
	// guessing a length that happens to fit the largest supported message. It runs
	// after the access check on purpose: a caller with no right to read the message
	// must learn nothing about it, including whether the body they sent was well
	// formed. The equality check below is the real gate.
	if len(input.Version) != privateEvidenceVersionLength || !isHex(input.Version) {
		problem(w, http.StatusBadRequest, "That preview is no longer current. Look at the message again.")
		return
	}
	preview, err := s.privateEvidenceForMessage(r.Context(), tx, principal, messageID, scope.ID)
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if preview.Version != input.Version {
		// The reporter confirmed different text than the server now holds. Refuse
		// and hand back a fresh preview: silently saving the replacement would
		// disclose something the reporter never saw and never agreed to.
		respond(w, http.StatusConflict, changedPreview{
			Error:   "This message changed since you read it. Check the text below before sending your report.",
			Changed: true,
			Preview: preview,
		})
		return
	}
	now := time.Now()
	// Only this reporter's own expired open receipt is removed, which is bounded to
	// one row and cannot touch anyone else's evidence.
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM private_message_reports WHERE reporter_id=? AND message_id=? AND status='open' AND expires_at<=?`, user.ID, messageID, db.Timestamp(now)); err != nil {
		s.failed(w, err)
		return
	}
	previous, err := scanPrivateReport(tx.QueryRowContext(r.Context(), `SELECT `+privateReportColumns+` FROM private_message_reports WHERE reporter_id=? AND message_id=? AND status='open'`, user.ID, messageID))
	if err == nil {
		// A concurrent retry deduplicates onto the original evidence: the stored
		// snapshot and explanation are never replaced by a later attempt.
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		respond(w, http.StatusOK, reportReceiptFor(privateMessageReport{ID: previous.ID, Status: previous.Status, CreatedAt: previous.CreatedAt, ExpiresAt: previous.ExpiresAt}, true))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	// Capacity is checked before the allowance is consumed, so a full store does
	// not also burn the reporter's hourly budget for a report that was never
	// taken. A full store is an operator condition rather than a server fault, so
	// it gets a defined answer that keeps the reporter's text rather than a
	// generic 500 (completion review 10, R10-07).
	full, err := s.privateEvidenceIsFull(r.Context(), tx)
	if err != nil {
		s.failed(w, err)
		return
	}
	if full {
		problem(w, http.StatusServiceUnavailable,
			"This server is not accepting new private reports right now. Your explanation has been kept; please try again later.")
		return
	}
	if allowed, err := s.consumePrivateReportAllowance(r.Context(), tx, user.ID, now); err != nil {
		s.failed(w, err)
		return
	} else if !allowed {
		w.Header().Set("Retry-After", "3600")
		problem(w, http.StatusTooManyRequests, "You have sent several private reports. Please try again later.")
		return
	}
	report := privateMessageReport{
		ID: db.ID(), MessageID: messageID, ConversationID: preview.ConversationID,
		ReporterID: user.ID, AuthorID: preview.AuthorID, Reason: input.Reason,
		SnapshotText: preview.SnapshotText, Status: "open",
		CreatedAt: db.Timestamp(now), UpdatedAt: db.Timestamp(now),
		ExpiresAt: db.Timestamp(now.Add(privateReportEvidenceLifetime)),
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO private_message_reports(id,message_id,conversation_id,reporter_id,author_id,reason,snapshot_text,status,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		report.ID, report.MessageID, report.ConversationID, report.ReporterID, report.AuthorID, report.Reason, report.SnapshotText, report.Status, report.CreatedAt, report.UpdatedAt, report.ExpiresAt); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusCreated, reportReceiptFor(report, false))
}

// reportReceiptFor is the reporter's own receipt. It is deliberately the same shape
// the community receipt uses: the submitter learns their report's identity and
// expiry and nothing else, so a second reporter's identity, reason or a reviewer's
// note can never reach them through this response.
func reportReceiptFor(report privateMessageReport, duplicate bool) map[string]any {
	return map[string]any{"id": report.ID, "status": report.Status, "created_at": report.CreatedAt, "expires_at": report.ExpiresAt, "duplicate": duplicate}
}

// consumePrivateReportAllowance applies the durable per-account hourly bound inside
// the submitting transaction, so two concurrent submissions cannot both read a
// count and then both write past it.
func (s *Server) consumePrivateReportAllowance(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (bool, error) {
	var started string
	var submissions int
	err := tx.QueryRowContext(ctx, `SELECT window_started_at,submissions FROM private_report_rate_limits WHERE user_id=?`, userID).Scan(&started, &submissions)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		started, submissions = db.Timestamp(now), 0
	case err != nil:
		return false, err
	}
	at, parseErr := time.Parse(time.RFC3339Nano, started)
	if parseErr != nil || now.Sub(at) >= privateReportHourlyWindow {
		// The window has elapsed, so the count restarts from this submission.
		started, submissions = db.Timestamp(now), 0
	}
	if submissions >= privateReportHourlyLimit {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO private_report_rate_limits(user_id,window_started_at,submissions) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET window_started_at=excluded.window_started_at,submissions=excluded.submissions`,
		userID, started, submissions+1); err != nil {
		return false, err
	}
	return true, nil
}

// privateEvidenceIsFull reports whether unexpired evidence has reached the
// installation bound. It never evicts: dropping a record a reviewer is mid-triage
// on, or one a reporter submitted and may appeal, would be a quieter and worse
// failure than an honest refusal. The answer is a boolean rather than an error so
// the caller can refuse deliberately instead of reporting an operator condition
// as a server fault.
func (s *Server) privateEvidenceIsFull(ctx context.Context, tx *sql.Tx) (bool, error) {
	var stored int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM private_message_reports WHERE expires_at>?`, db.Now()).Scan(&stored); err != nil {
		return false, err
	}
	return stored >= privateReportStorageLimit, nil
}

// listPrivateEvidence returns the reviewer queue.
//
// The only filter is status, expiry and the reviewer's own capability. It is
// deliberately NOT filtered by the reviewer's community channel list, because a
// direct conversation has no channel; and it is a separate endpoint from the
// community queue precisely so a community-only moderator never learns that a
// private queue exists, let alone its size.
func (s *Server) listPrivateEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "resolved" {
		problem(w, http.StatusBadRequest, "Choose open or resolved reports.")
		return
	}
	before := r.URL.Query().Get("before")
	if len(before) > 100 {
		problem(w, http.StatusBadRequest, "Invalid report cursor.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Checked in the transaction that reads the queue, so a reviewer whose grant is
	// revoked cannot read a page that was already in flight.
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ReportReviewPrivate); err != nil {
		s.authorityFailed(w, err)
		return
	}
	clause := ` WHERE status=? AND expires_at>?`
	args := []any{status, db.Now()}
	if before != "" {
		clause += ` AND id<?`
		args = append(args, before)
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT `+privateReportColumns+` FROM private_message_reports `+clause+` ORDER BY id DESC LIMIT 51`, args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	reports := make([]privateMessageReport, 0)
	for rows.Next() {
		report, err := scanPrivateReport(rows)
		if err != nil {
			rows.Close()
			s.failed(w, err)
			return
		}
		reports = append(reports, report)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	cursor := ""
	if len(reports) > 50 {
		reports = reports[:50]
		cursor = reports[49].ID
	}
	respond(w, http.StatusOK, map[string]any{"reports": reports, "next_cursor": cursor})
}

// reviewPrivateEvidence records triage. It does not delete a message, ban an
// account, or grant any power over the conversation the snapshot came from: this is
// a record of what a reviewer concluded, nothing more.
func (s *Server) reviewPrivateEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if (input.Status != "open" && input.Status != "resolved") || utf8.RuneCountInString(input.Note) > 2000 {
		problem(w, http.StatusBadRequest, "Choose open or resolved and a review note of at most 2,000 characters.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ReportReviewPrivate); err != nil {
		s.authorityFailed(w, err)
		return
	}
	// Expired evidence is indistinguishable from absent, so a reviewer cannot use
	// the queue to learn that an expired record exists.
	report, err := scanPrivateReport(tx.QueryRowContext(r.Context(), `SELECT `+privateReportColumns+` FROM private_message_reports WHERE id=? AND expires_at>?`, r.PathValue("id"), db.Now()))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if input.Status == "open" && report.ReporterID != "" {
		var exists bool
		if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM private_message_reports WHERE reporter_id=? AND message_id=? AND status='open' AND id<>?)`, report.ReporterID, report.MessageID, report.ID).Scan(&exists); err != nil {
			s.failed(w, err)
			return
		}
		if exists {
			problem(w, http.StatusConflict, "This member already has an open report for that message.")
			return
		}
	}
	// Expiry is deliberately not touched by a resolve or reopen: ninety days from
	// creation is the promise made to the reporter, and triage must not extend it.
	report.Status, report.ReviewNote, report.ReviewedBy, report.UpdatedAt = input.Status, input.Note, user.ID, db.Now()
	if _, err := tx.ExecContext(r.Context(), `UPDATE private_message_reports SET status=?,review_note=?,reviewed_by=?,updated_at=? WHERE id=?`, report.Status, report.ReviewNote, report.ReviewedBy, report.UpdatedAt, report.ID); err != nil {
		s.failed(w, err)
		return
	}
	// The audit entry names the reviewer, the action and the evidence id. It carries
	// no text, no reason and no membership, and a failure aborts the review with the
	// mutation rather than recording one without the other.
	if err := s.auditTx(r.Context(), tx, user.ID, "private_report."+input.Status, report.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, report)
}
