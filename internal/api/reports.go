package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

const reportEvidenceLifetime = 90 * 24 * time.Hour

// messageReport is moderator-only evidence. Submission returns a receipt, never
// this shape: another reporter's identity, reason or review note is not shared.
type messageReport struct {
	ID           string `json:"id"`
	MessageID    string `json:"message_id"`
	ChannelID    string `json:"channel_id"`
	ReporterID   string `json:"reporter_id"`
	AuthorID     string `json:"author_id"`
	Reason       string `json:"reason"`
	SnapshotText string `json:"snapshot_text"`
	Status       string `json:"status"`
	ReviewNote   string `json:"review_note"`
	ReviewedBy   string `json:"reviewed_by"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	ExpiresAt    string `json:"expires_at"`
}

const reportColumns = `id,message_id,channel_id,COALESCE(reporter_id,''),COALESCE(author_id,''),reason,snapshot_text,status,review_note,COALESCE(reviewed_by,''),created_at,updated_at,expires_at`

func scanReport(row interface{ Scan(...any) error }) (messageReport, error) {
	var report messageReport
	err := row.Scan(&report.ID, &report.MessageID, &report.ChannelID, &report.ReporterID, &report.AuthorID, &report.Reason, &report.SnapshotText, &report.Status, &report.ReviewNote, &report.ReviewedBy, &report.CreatedAt, &report.UpdatedAt, &report.ExpiresAt)
	return report, err
}

func reportReceipt(report messageReport, duplicate bool) map[string]any {
	return map[string]any{"id": report.ID, "status": report.Status, "created_at": report.CreatedAt, "expires_at": report.ExpiresAt, "duplicate": duplicate}
}

func (s *Server) reportMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Reason string `json:"reason"`
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
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	var channelID, authorID, content string
	// Resolve only the selected community message. Replies, files, embeds and
	// surrounding history are deliberately absent from the evidence snapshot.
	err = tx.QueryRowContext(r.Context(), `SELECT m.channel_id,m.user_id,m.content FROM messages m JOIN conversations c ON c.id=COALESCE(m.conversation_id,m.channel_id) AND c.kind='channel' AND c.channel_id=m.channel_id WHERE m.id=?`, r.PathValue("id")).Scan(&channelID, &authorID, &content)
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	readable, err := s.store.ChannelAccessible(r.Context(), tx, viewer(principal), channelID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !readable {
		hidden(w)
		return
	}
	now := time.Now()
	// The partial unique index is independent of time. Remove only this user's
	// expired open receipt before allowing a new report; this is bounded to one.
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM message_reports WHERE reporter_id=? AND message_id=? AND status='open' AND expires_at<=?`, user.ID, r.PathValue("id"), db.Timestamp(now)); err != nil {
		s.failed(w, err)
		return
	}
	previous, err := scanReport(tx.QueryRowContext(r.Context(), `SELECT `+reportColumns+` FROM message_reports WHERE reporter_id=? AND message_id=? AND status='open'`, user.ID, r.PathValue("id")))
	if err == nil {
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		respond(w, http.StatusOK, reportReceipt(previous, true))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	if !s.limiter.allow("report:"+user.ID, 10, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		problem(w, http.StatusTooManyRequests, "You have sent several reports. Please try again later.")
		return
	}
	text := []rune(content)
	if len(text) > 4000 {
		text = text[:4000]
	}
	report := messageReport{ID: db.ID(), MessageID: r.PathValue("id"), ChannelID: channelID, ReporterID: user.ID, AuthorID: authorID, Reason: input.Reason, SnapshotText: string(text), Status: "open", CreatedAt: db.Timestamp(now), UpdatedAt: db.Timestamp(now), ExpiresAt: db.Timestamp(now.Add(reportEvidenceLifetime))}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO message_reports(id,message_id,channel_id,reporter_id,author_id,reason,snapshot_text,status,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, report.ID, report.MessageID, report.ChannelID, report.ReporterID, report.AuthorID, report.Reason, report.SnapshotText, report.Status, report.CreatedAt, report.UpdatedAt, report.ExpiresAt)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusCreated, reportReceipt(report, false))
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request, user db.User) {
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
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.MessageDeleteAny)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// Apply channel visibility and expiry before both limit and pagination. Hidden
	// reports must not consume a page slot or leak their count through a cursor.
	clause, args := db.ReadableChannelScope(viewer(principal))
	clause += ` AND status=? AND expires_at>?`
	args = append(args, status, db.Now())
	if before != "" {
		clause += ` AND id<?`
		args = append(args, before)
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT `+reportColumns+` FROM message_reports `+clause+` ORDER BY id DESC LIMIT 51`, args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	reports := make([]messageReport, 0)
	for rows.Next() {
		report, err := scanReport(rows)
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

func (s *Server) reviewReport(w http.ResponseWriter, r *http.Request, user db.User) {
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
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.MessageDeleteAny)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	report, err := scanReport(tx.QueryRowContext(r.Context(), `SELECT `+reportColumns+` FROM message_reports WHERE id=? AND expires_at>?`, r.PathValue("id"), db.Now()))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	readable, err := s.store.ChannelAccessible(r.Context(), tx, viewer(principal), report.ChannelID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !readable {
		hidden(w)
		return
	}
	if input.Status == "open" && report.ReporterID != "" {
		var exists bool
		if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM message_reports WHERE reporter_id=? AND message_id=? AND status='open' AND id<>?)`, report.ReporterID, report.MessageID, report.ID).Scan(&exists); err != nil {
			s.failed(w, err)
			return
		}
		if exists {
			problem(w, http.StatusConflict, "This member already has an open report for that message.")
			return
		}
	}
	report.Status, report.ReviewNote, report.ReviewedBy, report.UpdatedAt = input.Status, input.Note, user.ID, db.Now()
	if _, err := tx.ExecContext(r.Context(), `UPDATE message_reports SET status=?,review_note=?,reviewed_by=?,updated_at=? WHERE id=?`, report.Status, report.ReviewNote, report.ReviewedBy, report.UpdatedAt, report.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "report."+input.Status, report.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, report)
}
