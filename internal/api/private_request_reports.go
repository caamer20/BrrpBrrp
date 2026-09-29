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

// Recipient-only reporting of a pending request, and the durable command receipt
// that makes it retryable (ADR 0014).
//
// The whole operation is one transaction: the introduction the reporter was shown is
// saved, the request is rejected, its introduction cleared, the sender blocked, the
// restricted audit written and the receipt recorded. Any failure rolls back all of
// it — a half-applied report-and-block would leave a person believing a request was
// handled when the block had not taken, or blocked with nothing recorded.
//
// The report is deliberately never separable from the block. The disclosure before
// confirmation says so, and ordinary Block stays available for anyone who wants to
// block without reporting.

const (
	// privateReportReceiptLifetime is how long a command receipt is honoured. It is
	// shorter than the evidence it describes, so an old key cannot become new work,
	// and it is long enough that a lost response is recoverable.
	privateReportReceiptLifetime = 30 * 24 * time.Hour
	// requestIntroductionLimit bounds stored request evidence. A request
	// introduction is already capped at 500 characters by its own column; the
	// evidence bound restates it so a bad row cannot be written by another path.
	requestIntroductionLimit = 500
)

// requestEvidence is one voluntarily submitted pending-request introduction.
type requestEvidence struct {
	ID            string
	Conversation  string
	AttemptID     string
	RequesterID   string
	ReporterID    string
	Reason        string
	SnapshotText  string
	RequestSentAt string
	CreatedAt     string
	ExpiresAt     string
	Status        string
	ReviewNote    string
	ReviewedBy    string
	SenderBanned  bool
	SenderDeactiv bool
	UpdatedAt     string
}

const requestEvidenceColumns = `id,conversation_id,attempt_id,COALESCE(requester_id,''),COALESCE(reporter_id,''),reason,snapshot_text,request_sent_at,created_at,expires_at,status,review_note,COALESCE(reviewed_by,''),sender_banned,sender_deactivated,updated_at`

func scanRequestEvidence(row interface{ Scan(...any) error }) (requestEvidence, error) {
	var e requestEvidence
	err := row.Scan(&e.ID, &e.Conversation, &e.AttemptID, &e.RequesterID, &e.ReporterID,
		&e.Reason, &e.SnapshotText, &e.RequestSentAt, &e.CreatedAt, &e.ExpiresAt,
		&e.Status, &e.ReviewNote, &e.ReviewedBy, &e.SenderBanned, &e.SenderDeactiv, &e.UpdatedAt)
	return e, err
}

// requestReportPreview is what a recipient is shown and then confirms. It carries
// the server's own introduction, never the browser's copy, and a version that
// changes if the request does.
type requestReportPreview struct {
	ConversationID  string `json:"conversation_id"`
	AttemptID       string `json:"attempt_id"`
	RequesterID     string `json:"requester_id"`
	RequesterName   string `json:"requester_name"`
	Introduction    string `json:"introduction"`
	HasIntroduction bool   `json:"has_introduction"`
	RequestSentAt   string `json:"request_sent_at"`
	Version         string `json:"version"`
	ExpiresAt       string `json:"expires_at"`
	ReviewerCount   int    `json:"reviewer_count"`
}

// requestVersion is a bounded digest over the introduction and the attempt, so it
// cannot grow with the text and cannot be satisfied by a different request. The
// attempt id is mixed in deliberately: the same introduction arriving as a renewed
// request is a different thing to report.
func requestVersion(introduction, attemptID string) string {
	digest := sha256.New()
	for _, part := range []string{introduction, attemptID} {
		fmt.Fprintf(digest, "%d:", len(part))
		io.WriteString(digest, part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// intentDigest binds an operation key to what was asked for. A replay under the same
// key with different content is a conflict rather than a silent overwrite, and a
// different key with the same content is ordinary new work.
func intentDigest(action, reason, attemptID string) string {
	digest := sha256.New()
	for _, part := range []string{action, reason, attemptID} {
		fmt.Fprintf(digest, "%d:", len(part))
		io.WriteString(digest, part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// pendingAttempt resolves the current pending attempt of a pair, but only for the
// given recipient. A sender, an outsider or a non-participating owner all get
// sql.ErrNoRows, so no existence oracle distinguishes "not pending" from "not yours".
func pendingAttempt(ctx context.Context, q db.Queryer, conversationID, recipientID string) (attemptID, requesterID, introduction, sentAt string, err error) {
	err = q.QueryRowContext(ctx, `SELECT r.attempt_id,r.requester_id,r.introduction,r.requested_at
		FROM direct_requests r JOIN conversations c ON c.id=r.conversation_id AND c.kind='direct' AND c.state='pending'
		WHERE r.conversation_id=? AND r.state='pending' AND r.recipient_id=? AND r.attempt_id<>''`,
		conversationID, recipientID).Scan(&attemptID, &requesterID, &introduction, &sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", "", sql.ErrNoRows
	}
	return attemptID, requesterID, introduction, sentAt, err
}

// previewRequestEvidence returns the snapshot a report would save. It requires the
// caller to be the recipient of the exact current attempt and nothing more: no
// capability, no role, no accepted-history access. A pending recipient has none of
// those, which is the whole point.
func (s *Server) previewRequestEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	conversationID := r.PathValue("id")
	if conversationID == "" {
		hidden(w)
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
	attemptID, requesterID, introduction, sentAt, err := pendingAttempt(r.Context(), tx, conversationID, principal.UserID)
	if err != nil {
		hidden(w)
		return
	}
	preview, err := s.requestPreviewFor(r.Context(), tx, principal, conversationID, attemptID, requesterID, introduction, sentAt)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, preview)
}

func (s *Server) requestPreviewFor(ctx context.Context, tx *sql.Tx, principal authz.Principal, conversationID, attemptID, requesterID, introduction, sentAt string) (*requestReportPreview, error) {
	var name string
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(display_name,''),username) FROM users WHERE id=?`, requesterID).Scan(&name)
	reviewers, err := s.privateEvidenceReviewerCount(ctx, tx)
	if err != nil {
		return nil, err
	}
	return &requestReportPreview{
		ConversationID:  conversationID,
		AttemptID:       attemptID,
		RequesterID:     requesterID,
		RequesterName:   name,
		Introduction:    introduction,
		HasIntroduction: introduction != "",
		RequestSentAt:   sentAt,
		Version:         requestVersion(introduction, attemptID),
		ExpiresAt:       db.Timestamp(time.Now().Add(privateReportEvidenceLifetime)),
		ReviewerCount:   reviewers,
	}, nil
}

// reportRequestAndBlock stores the introduction the recipient was shown, rejects the
// request, clears its introduction, blocks the sender, records the receipt and
// writes the restricted audit — in one transaction, or not at all.
func (s *Server) reportRequestAndBlock(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Reason       string `json:"reason"`
		Version      string `json:"version"`
		OperationKey string `json:"operation_key"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || utf8.RuneCountInString(input.Reason) > 2000 {
		problem(w, http.StatusBadRequest, "Explain your report in 1–2,000 characters.")
		return
	}
	input.OperationKey = strings.TrimSpace(input.OperationKey)
	if input.OperationKey == "" || len(input.OperationKey) > 100 {
		problem(w, http.StatusBadRequest, "This action needs its own identifier so a retry is recognised.")
		return
	}
	if len(input.Version) != privateEvidenceVersionLength || !isHex(input.Version) {
		problem(w, http.StatusBadRequest, "That preview is no longer current. Look at the request again.")
		return
	}
	conversationID := r.PathValue("id")
	if conversationID == "" {
		hidden(w)
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

	// A committed receipt is resolved before a still-pending source is required.
	// Report-and-block destroys its own source, so demanding one first would make
	// every retry fail on a request that has already been handled (ADR 0014).
	receipt, found, err := s.committedReceipt(r.Context(), tx, principal.UserID, input.OperationKey)
	if err != nil {
		s.failed(w, err)
		return
	}
	if found {
		if receipt.IntentDigest != intentDigest("report_and_block", input.Reason, receipt.AttemptID) {
			problem(w, http.StatusConflict, "That identifier was already used for a different report.")
			return
		}
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		respond(w, http.StatusOK, requestOperationReceipt(receipt, true))
		return
	}

	attemptID, requesterID, introduction, sentAt, err := pendingAttempt(r.Context(), tx, conversationID, principal.UserID)
	if err != nil {
		hidden(w)
		return
	}
	if requestVersion(introduction, attemptID) != input.Version {
		// The refreshed preview is returned with its own shape rather than forced
		// through the message conflict type: a request is not a message, and
		// pretending otherwise would put a message-shaped record in front of a
		// recipient who never had a message.
		preview, previewErr := s.requestPreviewFor(r.Context(), tx, principal, conversationID, attemptID, requesterID, introduction, sentAt)
		if previewErr != nil {
			s.failed(w, previewErr)
			return
		}
		respond(w, http.StatusConflict, map[string]any{
			"error":   "This request changed since you read it. Check it again before reporting.",
			"changed": true,
			"source":  "request",
			"request": preview,
		})
		return
	}
	// Characters, not bytes, and the same rule creation used (direct.go). The bound
	// is a promise about how much of an introduction a person may write, and a
	// three-byte CJK character is still one character they typed. Measuring bytes
	// here meant a request the server had accepted, previewed and shown to the
	// recipient could be refused at the moment they tried to report it
	// (completion review 12, R12-03). The stored snapshot remains bounded in bytes
	// by the column's own CHECK, which is the right place for a storage concern.
	if !utf8.ValidString(introduction) || utf8.RuneCountInString(introduction) > requestIntroductionLimit {
		problem(w, http.StatusBadRequest, "That introduction is longer than a request can hold.")
		return
	}

	// The shared private-evidence bounds apply to request evidence exactly as to
	// message evidence: one allowance, one capacity, and neither consumed by an
	// action that then fails (ADR 0014).
	full, err := s.privateEvidenceIsFull(r.Context(), tx)
	if err != nil {
		s.failed(w, err)
		return
	}
	if full {
		problem(w, http.StatusServiceUnavailable,
			"This server is not accepting new private reports right now. Your explanation has been kept; you can still block this person without reporting.")
		return
	}
	now := time.Now()
	allowed, err := s.consumePrivateReportAllowance(r.Context(), tx, user.ID, now)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "3600")
		problem(w, http.StatusTooManyRequests,
			"You have sent several private reports. You can still block this person without reporting.")
		return
	}

	evidence, err := s.insertRequestEvidence(r.Context(), tx, conversationID, attemptID, requesterID, user.ID, input.Reason, introduction, sentAt, now)
	if err != nil {
		s.failed(w, err)
		return
	}
	// The block and the rejection go through the same helpers ordinary Block uses,
	// so a pending request closes and its introduction is cleared identically.
	if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO user_blocks(blocker_id,blocked_id,created_at) VALUES(?,?,?)`, user.ID, requesterID, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := rejectDirectRequest(r.Context(), tx, conversationID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.recordReceipt(r.Context(), tx, user.ID, input.OperationKey, evidence, requesterID, attemptID, "reported_and_blocked", now); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "private_report.reported_and_blocked", evidence.ID); err != nil {
		s.failed(w, err)
		return
	}
	// commitDirectChange commits through the authority fence and publishes the
	// invalidations; it is the only commit for this transaction, exactly as in the
	// ordinary block path. A second Commit here would be an error on a closed
	// transaction and turned the whole operation into a 500 after the work had
	// already landed.
	if err := s.commitDirectChange(r.Context(), tx, user.ID, requesterID); err != nil {
		s.failed(w, err)
		return
	}
	s.signalDirectChange(user.ID, requesterID)
	respond(w, http.StatusCreated, requestOperationReceipt(requestReceipt{
		EvidenceID:   evidence.ID,
		Outcome:      "reported_and_blocked",
		SubjectID:    requesterID,
		AttemptID:    attemptID,
		CreatedAt:    evidence.CreatedAt,
		ExpiresAt:    evidence.ExpiresAt,
		IntentDigest: intentDigest("report_and_block", input.Reason, attemptID),
	}, false))
}

type requestReceipt struct {
	EvidenceID   string
	Outcome      string
	SubjectID    string
	AttemptID    string
	CreatedAt    string
	ExpiresAt    string
	IntentDigest string
}

// requestOperationReceipt is the minimal outcome. It says what happened, and never
// says the person is blocked *now*: a later deliberate unblock must stay effective
// and a replay must not block again (ADR 0014).
func requestOperationReceipt(receipt requestReceipt, duplicate bool) map[string]any {
	return map[string]any{
		"id":         receipt.EvidenceID,
		"outcome":    receipt.Outcome,
		"created_at": receipt.CreatedAt,
		"expires_at": receipt.ExpiresAt,
		"duplicate":  duplicate,
	}
}

func (s *Server) insertRequestEvidence(ctx context.Context, tx *sql.Tx, conversationID, attemptID, requesterID, reporterID, reason, introduction, sentAt string, now time.Time) (requestEvidence, error) {
	e := requestEvidence{
		ID: db.ID(), Conversation: conversationID, AttemptID: attemptID,
		RequesterID: requesterID, ReporterID: reporterID, Reason: reason,
		SnapshotText: introduction, RequestSentAt: sentAt, Status: "open",
		CreatedAt: db.Timestamp(now), UpdatedAt: db.Timestamp(now),
		ExpiresAt: db.Timestamp(now.Add(privateReportEvidenceLifetime)),
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO private_request_reports(id,conversation_id,attempt_id,requester_id,reporter_id,reason,snapshot_text,request_sent_at,status,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?,?,'open',?,?,?)`,
		e.ID, e.Conversation, e.AttemptID, e.RequesterID, e.ReporterID, e.Reason, e.SnapshotText, e.RequestSentAt, e.CreatedAt, e.UpdatedAt, e.ExpiresAt)
	return e, err
}

// committedReceipt resolves a previously committed operation for this account. It is
// checked after authentication and the feature policy but before any source
// requirement, which is what makes a retry work after the source is gone.
func (s *Server) committedReceipt(ctx context.Context, tx *sql.Tx, userID, operationKey string) (requestReceipt, bool, error) {
	var r requestReceipt
	var createdAt, expiresAt string
	err := tx.QueryRowContext(ctx, `SELECT evidence_id,outcome,subject_id,attempt_id,created_at,expires_at,intent_digest
		FROM private_report_receipts WHERE user_id=? AND operation_key=? AND expires_at>?`,
		userID, operationKey, db.Now()).Scan(&r.EvidenceID, &r.Outcome, &r.SubjectID, &r.AttemptID, &createdAt, &expiresAt, &r.IntentDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return requestReceipt{}, false, nil
	}
	if err != nil {
		return requestReceipt{}, false, err
	}
	r.CreatedAt, r.ExpiresAt = createdAt, expiresAt
	return r, true, nil
}

func (s *Server) recordReceipt(ctx context.Context, tx *sql.Tx, userID, operationKey string, evidence requestEvidence, subjectID, attemptID, outcome string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO private_report_receipts(id,user_id,operation_key,intent_digest,source_kind,source_id,attempt_id,evidence_id,outcome,reporter_id,subject_id,created_at,expires_at) VALUES(?,?,?,?,'request',?,?,?,?,?,?,?,?)`,
		db.ID(), userID, operationKey, intentDigest("report_and_block", evidence.Reason, attemptID),
		evidence.Conversation, attemptID, evidence.ID, outcome, userID, subjectID,
		db.Timestamp(now), db.Timestamp(now.Add(privateReportReceiptLifetime)))
	return err
}

// ownReportReceipt lets an ordinary reporter reconcile an operation without any
// reviewer access. It returns only the calling account's minimal outcome for its
// own key — never a reviewer note, another reporter, an evidence body, or any
// statement about current state (ADR 0014, "Own-outcome reconciliation").
func (s *Server) ownReportReceipt(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("operation_key"))
	if key == "" || len(key) > 100 {
		problem(w, http.StatusBadRequest, "That action needs its own identifier so a retry is recognised.")
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
	receipt, found, err := s.committedReceipt(r.Context(), tx, principal.UserID, key)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !found {
		// Not found and not disclosed: another account's key is indistinguishable
		// from one that never existed.
		hidden(w)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, requestOperationReceipt(receipt, true))
}

// listRequestEvidence is the reviewer queue for request-introduction evidence. It is
// a separate endpoint from the message queue, shares the same capability, policy,
// expiry and pagination, and omits the conversation identifier entirely: a reviewer
// sees that the source was a request and who sent it, never somewhere to open.
func (s *Server) listRequestEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
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
	rows, err := tx.QueryContext(r.Context(), `SELECT `+requestEvidenceColumns+` FROM private_request_reports `+clause+` ORDER BY id DESC LIMIT 51`, args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	reports := make([]requestEvidence, 0)
	for rows.Next() {
		e, err := scanRequestEvidence(rows)
		if err != nil {
			rows.Close()
			s.failed(w, err)
			return
		}
		reports = append(reports, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
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
	respond(w, http.StatusOK, map[string]any{"reports": requestEvidenceView(reports), "next_cursor": cursor})
}

// requestEvidenceView is the reviewer-facing shape. The conversation id is internal
// provenance and is deliberately not here: an identifier is not permission to
// browse a conversation, and this is the only representation a reviewer receives.
func requestEvidenceView(reports []requestEvidence) []map[string]any {
	view := make([]map[string]any, 0, len(reports))
	for _, e := range reports {
		view = append(view, map[string]any{
			"id": e.ID, "source_kind": "request", "requester_id": e.RequesterID,
			"reporter_id": e.ReporterID, "reason": e.Reason, "snapshot_text": e.SnapshotText,
			"request_sent_at": e.RequestSentAt, "status": e.Status, "review_note": e.ReviewNote,
			"reviewed_by": e.ReviewedBy, "created_at": e.CreatedAt, "expires_at": e.ExpiresAt,
			"sender_banned": e.SenderBanned, "sender_deactivated": e.SenderDeactiv,
		})
	}
	return view
}

// reviewRequestEvidence records triage on request evidence. It is deliberately
// nonpunitive: no deletion, no ban, no expiry change, exactly as for message
// evidence.
func (s *Server) reviewRequestEvidence(w http.ResponseWriter, r *http.Request, user db.User) {
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
	evidence, err := scanRequestEvidence(tx.QueryRowContext(r.Context(), `SELECT `+requestEvidenceColumns+` FROM private_request_reports WHERE id=? AND expires_at>?`, r.PathValue("id"), db.Now()))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE private_request_reports SET status=?,review_note=?,reviewed_by=?,updated_at=? WHERE id=?`,
		input.Status, input.Note, user.ID, db.Now(), evidence.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "private_request."+input.Status, evidence.ID); err != nil {
		s.failed(w, err)
		return
	}
	// The response describes the record as it now is. Returning the row read before
	// the update reported the previous status, so a caller that trusted it would
	// see the review as still open.
	evidence.Status = input.Status
	evidence.ReviewNote = input.Note
	evidence.ReviewedBy = user.ID
	evidence.UpdatedAt = db.Now()
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, requestEvidenceView([]requestEvidence{evidence})[0])
}
