package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// A member's own data export (ADR 0019b).
//
// The design is a snapshot, not a query, and that is the whole point: the rows are
// read in one transaction — the same transaction that decides the account may export
// at all — so what the file contains is what the account could read at the moment it
// asked, and never a state that existed only because a membership changed halfway
// through.
//
// What it must never contain is stated in WP-12 and restated as the code's own
// contract below: another private conversation, anybody's credentials or sessions,
// reviewer-only evidence, or operator secrets.

// exportBounds are the ceilings applied *while* the export is built, not checked
// afterwards. An unbounded member-triggered job is a way to fill the server's disk,
// and a truncated file that looks complete is worse than one that says it stopped.
const (
	maxExportMessages    = 50_000
	maxExportAttachments = 2_000
	maxExportBytes       = 256 << 20
	exportLifetime       = 7 * 24 * time.Hour
)

// exportDocument is the shape written to disk. It is a single JSON file rather than a
// directory of files so that a member can read it without this application, and so a
// partial download is visibly partial rather than silently short.
type exportDocument struct {
	// The build's own facts first, so a reader knows what they are looking at before
	// they read anything in it.
	Format        string        `json:"format"`
	FormatVersion int           `json:"format_version"`
	GeneratedAt   string        `json:"generated_at"`
	ExpiresAt     string        `json:"server_copy_expires_at"`
	Account       exportAccount `json:"account"`
	Summary       exportSummary `json:"summary"`
	// Omissions is not a caveat appended at the end. It travels with the summary,
	// because a member whose export stopped at its bound must be able to find that fact
	// without reading the whole file — and must not be able to miss it.
	Omissions     []string             `json:"omissions"`
	Conversations []exportConversation `json:"conversations"`
}

type exportAccount struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	// Explicitly absent from this struct: password hashes, session tokens, second-factor
	// secrets, recovery codes, blocks and any moderation state. A member's export is
	// their content, not their credentials.
}

type exportSummary struct {
	Conversations int  `json:"conversations"`
	Messages      int  `json:"messages"`
	Attachments   int  `json:"attachments"`
	Truncated     bool `json:"truncated"`
}

type exportConversation struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Messages []exportMessage `json:"messages"`
}

type exportMessage struct {
	ID          string             `json:"id"`
	AuthorID    string             `json:"author_id"`
	Content     string             `json:"content"`
	CreatedAt   string             `json:"created_at"`
	EditedAt    string             `json:"edited_at,omitempty"`
	ReplyToID   string             `json:"reply_to_id,omitempty"`
	ThreadID    string             `json:"thread_id,omitempty"`
	Attachments []exportAttachment `json:"attachments"`
	Reactions   []exportReaction   `json:"reactions"`
}

type exportAttachment struct {
	ID       string `json:"id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size_bytes"`
	// StoredName is deliberately absent: it is the server's own path, and an export is
	// not a way to address files on the host.
	CreatedAt string `json:"created_at"`
}

type exportReaction struct {
	Emoji     string `json:"emoji"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

// exportRecord is the row in data_exports.
type exportRecord struct {
	ID                string `json:"id"`
	UserID            string `json:"user_id"`
	OperationKey      string `json:"operation_key"`
	State             string `json:"state"`
	CreatedAt         string `json:"created_at"`
	CompletedAt       string `json:"completed_at"`
	ConversationCount int    `json:"conversation_count"`
	MessageCount      int    `json:"message_count"`
	AttachmentCount   int    `json:"attachment_count"`
	BytesWritten      int64  `json:"bytes_written"`
	Truncated         bool   `json:"truncated"`
	Omissions         string `json:"omissions"`
	ExpiresAt         string `json:"expires_at"`
	CompletedBy       string `json:"completed_by"`
	FreshAuthAt       string `json:"fresh_auth_at"`
	// Scope is the set of conversation ids the build actually selected, as JSON. It is
	// written by the build transaction and read by the download check, so delivery is
	// authorized against the file's own contents rather than against what the file
	// "should" have contained. Empty means the row predates the column, and an export
	// whose scope cannot be established is one whose authorization cannot either.
	Scope string `json:"-"`
}

const exportColumns = `id,user_id,operation_key,state,created_at,completed_at,conversation_count,message_count,attachment_count,bytes_written,truncated,omissions,expires_at,COALESCE(completed_by,''),fresh_auth_at,scope`

func scanExport(row interface{ Scan(...any) error }) (exportRecord, error) {
	var record exportRecord
	err := row.Scan(&record.ID, &record.UserID, &record.OperationKey, &record.State, &record.CreatedAt, &record.CompletedAt,
		&record.ConversationCount, &record.MessageCount, &record.AttachmentCount, &record.BytesWritten,
		&record.Truncated, &record.Omissions, &record.ExpiresAt, &record.CompletedBy, &record.FreshAuthAt,
		&record.Scope)
	return record, err
}

// requestExport starts an export, or returns the one this operation key already made.
//
// The key is the account's, as everywhere else: a retried request is the same export
// rather than a second one, and a member who double-clicks does not get two copies of
// their own history.
func (s *Server) requestExport(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		OperationKey string `json:"operation_key"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.OperationKey = strings.TrimSpace(input.OperationKey)
	if len(input.OperationKey) < 8 || len(input.OperationKey) > 100 {
		problem(w, 400, "Choose a message key between 8 and 100 characters.")
		return
	}
	// An export leaves the building, so it is gated on recent strong authentication —
	// the same gate a password change uses. An export started at an unattended machine
	// is not the account holder's export.
	if !s.requireRecentAuthentication(w, r, user) {
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
	// A pending job for this account blocks a new one. Two concurrent exports would
	// read the same rows twice and write two files, and neither is what the member
	// asked for.
	var pending int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM data_exports WHERE user_id=? AND state='building'`, user.ID).Scan(&pending); err != nil {
		s.failed(w, err)
		return
	}
	existing, err := scanExport(tx.QueryRowContext(r.Context(),
		`SELECT `+exportColumns+` FROM data_exports WHERE user_id=? AND operation_key=?`, user.ID, input.OperationKey))
	if err == nil {
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		respond(w, http.StatusOK, existing)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	if pending > 0 {
		problem(w, 409, "An export is already being prepared. It will be ready shortly.")
		return
	}
	// Finished copies are bounded per account and on the server. A seven-day lifetime
	// is a statement about how long a copy may exist, not about how many may exist: an
	// account that ran one export a minute would otherwise accumulate an unbounded
	// number of files, and every one of them is a copy of somebody's history the
	// server is holding.
	//
	// The oldest are reclaimed rather than the request refused, because a member
	// asking for a new export and being told "delete something first" is a worse
	// answer than losing a copy they can rebuild at no cost to anybody.
	if err := s.reclaimExportStorage(r.Context(), tx, principal.UserID); err != nil {
		// A full server is an ordinary, explainable refusal — a 409 with words, not a
		// 500, because the member did nothing wrong and the condition clears itself when
		// a copy expires or the operator's expiry sweep runs.
		if errors.Is(err, errExportStorageFull) {
			problem(w, 409, errExportStorageFull.Error())
			return
		}
		s.failed(w, err)
		return
	}
	now := db.Now()
	id := db.ID()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO data_exports(id,user_id,operation_key,state,created_at,fresh_auth_at)
		VALUES(?,?,?,'building',?,?)`, id, principal.UserID, input.OperationKey, now, now); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.drainExportRemovals()
	// The build runs after the commit, outside the request. An export that took a
	// minute would time the request out, and a member would have no way to tell a
	// slow export from a stuck one.
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		if err := s.buildExport(context.WithoutCancel(r.Context()), id, principal.UserID); err != nil {
			s.log.Error("export failed", "error", err, "export", id)
			// The reason is recorded on the row as well as the log. A member whose export
			// failed is entitled to be told why in the place they can see, and an operator
			// reading the export history should not have to correlate a log line.
			reason := err.Error()
			if len(reason) > 500 {
				reason = reason[:500]
			}
			if _, markErr := s.store.Exec(`UPDATE data_exports SET state='failed',completed_at=?,omissions=? WHERE id=? AND state='building'`,
				db.Now(), reason, id); markErr != nil {
				s.log.Error("could not mark the export failed", "error", markErr)
			}
		}
	}()
	record, err := scanExport(s.store.QueryRow(`SELECT `+exportColumns+` FROM data_exports WHERE id=?`, id))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusAccepted, record)
}

// listExports is the account's own history of exports. It shows what was produced, how
// much it held and what it could not include — which is the part a member needs when
// they are trying to work out whether they have everything.
func (s *Server) listExports(w http.ResponseWriter, r *http.Request, user db.User) {
	rows, err := s.store.QueryContext(r.Context(),
		`SELECT `+exportColumns+` FROM data_exports WHERE user_id=? ORDER BY id DESC LIMIT 20`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	records := []exportRecord{}
	for rows.Next() {
		record, err := scanExport(rows)
		if err != nil {
			s.failed(w, err)
			return
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, records)
}

// buildExport assembles the document. Every row is read through one transaction, which
// is what makes the file a snapshot of a single state rather than a walk through a
// moving target.
func (s *Server) buildExport(ctx context.Context, exportID, userID string) error {
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The account is re-read here rather than trusted from the request: an export
	// queued a moment ago must not run for an account that has since been erased, and
	// a missing account here is the reason the job stops rather than re-materialising
	// data that no longer has an owner.
	account, err := s.store.Account(ctx, tx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("the account was erased before the export could run")
	}
	if err != nil {
		return err
	}
	principal, err := s.authz.Resolve(ctx, tx, account)
	if err != nil {
		return err
	}
	// An inactive account's export is not built. A ban, a deactivation and a timeout
	// all mean the account is not currently permitted to do things, and an export is a
	// large read.
	if principal.Inactive {
		return errors.New("this account is not currently permitted to export")
	}

	document := exportDocument{
		Format:        "brrpbrrp-member-export",
		FormatVersion: 1,
		GeneratedAt:   db.Now(),
		Account: exportAccount{
			ID: account.ID, Username: account.Username,
			DisplayName: account.DisplayName, CreatedAt: account.CreatedAt,
		},
		Omissions:     []string{},
		Conversations: []exportConversation{},
	}

	// Only conversations this account can read *now*. The read filter is the same one a
	// message list uses, so an export can never contain a conversation the interface
	// would refuse to show — including a direct pair or a group whose membership was
	// withdrawn after the messages were written.
	conversations, err := s.exportableConversations(ctx, tx, principal)
	if err != nil {
		return err
	}
	omitted := []string{}
	truncated := false
	totalMessages, totalAttachments := 0, 0

	for _, conversation := range conversations {
		// The ceiling is applied *before* reading a conversation's messages, not after
		// counting them: an export that reads everything and then discards most of it
		// is still a job that filled the disk.
		if totalMessages >= maxExportMessages {
			truncated = true
			omitted = append(omitted, fmt.Sprintf(
				"stopped after %d messages; %d conversations were not included",
				maxExportMessages, len(conversations)-len(document.Conversations)))
			break
		}
		exported, err := s.exportConversation(ctx, tx, conversation, &totalMessages, &totalAttachments, &truncated)
		if err != nil {
			return err
		}
		if exported == nil {
			omitted = append(omitted, fmt.Sprintf("%s could not be read in full", conversation.Title))
			continue
		}
		document.Conversations = append(document.Conversations, *exported)
	}
	if truncated {
		document.Omissions = append(document.Omissions, omitted...)
	}
	document.Summary = exportSummary{
		Conversations: len(document.Conversations),
		Messages:      totalMessages,
		Attachments:   totalAttachments,
		Truncated:     truncated,
	}
	document.ExpiresAt = db.Timestamp(time.Now().UTC().Add(exportLifetime))

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(encoded)) > maxExportBytes {
		// The row bound and the byte bound are the same bound seen from two sides. The
		// earlier version recorded the omission and then wrote the oversized file
		// anyway, so the ceiling protected nothing except the reader's patience: a
		// member whose export was over the limit got a document the server had already
		// declared it would not produce.
		//
		// So it is not written. The job fails, the member is told why in the place they
		// can see it, and nothing lands on disk for a delivery to serve. The
		// conversation-level bound above normally stops this long before: 50,000
		// messages of 4,000 characters is about 200 MiB, under a 256 MiB ceiling.
		document.Summary.Truncated = true
		document.Omissions = append(document.Omissions, "the file exceeded its size limit and was not written")
		// The row still records what was reached, so the member can see how much of
		// their history the build covered even though no file was produced.
		return s.recordTruncatedExport(ctx, tx, exportID, userID, document)
	}

	path := s.exportPath(exportID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Written to a temporary name and renamed, so a reader never sees a half-written
	// file and a crash never leaves one that looks complete.
	temporary := path + ".partial"
	if err := os.WriteFile(temporary, encoded, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	// The scope is the file's own list of conversations, taken from what was written
	// rather than from what was selected. A conversation that failed to read and was
	// omitted is not in the file and must not be in the scope, or a later download
	// would refuse the whole export for a conversation it never contained.
	scope := make([]string, 0, len(document.Conversations))
	for _, conversation := range document.Conversations {
		scope = append(scope, conversation.ID)
	}
	encodedScope, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	now := db.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE data_exports SET state='ready',completed_at=?,conversation_count=?,message_count=?,attachment_count=?,
		bytes_written=?,truncated=?,omissions=?,expires_at=?,completed_by=?,scope=?
		WHERE id=? AND user_id=? AND state='building'`,
		now, document.Summary.Conversations, document.Summary.Messages, document.Summary.Attachments,
		int64(len(encoded)), boolToInt(document.Summary.Truncated), strings.Join(document.Omissions, "; "),
		document.ExpiresAt, userID, string(encodedScope), exportID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// exportableConversations is the set this account may export, resolved with the same
// rules a conversation list uses.
func (s *Server) exportableConversations(ctx context.Context, q db.Queryer, principal authz.Principal) ([]exportConversation, error) {
	// Community channels the account can read.
	channels, err := q.QueryContext(ctx, `SELECT c.id,c.name FROM channels c
		JOIN conversations v ON v.id=c.id AND v.kind='channel'
		ORDER BY c.name LIMIT 200`)
	if err != nil {
		return nil, err
	}
	conversations := []exportConversation{}
	for channels.Next() {
		var id, name string
		if err := channels.Scan(&id, &name); err != nil {
			channels.Close()
			return nil, err
		}
		channel, err := s.store.Channel(ctx, q, id)
		if err != nil {
			continue
		}
		lists, err := s.store.ChannelAccessLists(ctx, q, id)
		if err != nil {
			continue
		}
		if !principal.MayReadChannel(channel, lists[id]) {
			continue
		}
		conversations = append(conversations, exportConversation{ID: id, Kind: "channel", Title: name})
	}
	channels.Close()
	if err := channels.Err(); err != nil {
		return nil, err
	}

	// Private conversations the account is an accepted, usable member of. This is the
	// check that makes "never another private conversation" true rather than
	// aspirational: a withdrawn membership excludes the conversation, whatever its
	// history says.
	// Private conversations are not selected at all when the feature is off. The
	// route's own gate covers the request; this covers the *build*, which runs after
	// the response and can still find private rows. Without it an export built on a
	// server with private conversations disabled contained them anyway, because the
	// selection queried the conversation table directly rather than asking the same
	// question the interface asks (completion review 14, V14-02).
	if !s.config.PrivateConversations {
		return conversations, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT c.id, c.kind, COALESCE(d.name,'') FROM conversations c
		LEFT JOIN group_details d ON d.conversation_id=c.id
		JOIN conversation_members m ON m.conversation_id=c.id AND m.user_id=? AND m.state='accepted'
		WHERE c.kind IN ('direct','group') AND c.state='active' ORDER BY c.id LIMIT 200`, principal.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, name string
		if err := rows.Scan(&id, &kind, &name); err != nil {
			return nil, err
		}
		title := name
		if title == "" {
			title = s.exportPeerName(ctx, q, id, principal.UserID)
		}
		conversations = append(conversations, exportConversation{ID: id, Kind: kind, Title: title})
	}
	return conversations, rows.Err()
}

// exportPeerName names a direct conversation for the file. A group already has a
// name; a direct pair is named for the other member, and only for the one this account
// is actually paired with.
func (s *Server) exportPeerName(ctx context.Context, q db.Queryer, conversationID, viewerID string) string {
	var name string
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(u.display_name,''),u.username,'member')
		FROM conversation_members m JOIN users u ON u.id=m.user_id
		WHERE m.conversation_id=? AND m.state='accepted' AND m.user_id<>? LIMIT 1`,
		conversationID, viewerID).Scan(&name); err != nil {
		return "direct conversation"
	}
	return name
}

// exportConversation reads one conversation's messages, returning nil when it could
// not be read in full so the caller can say so rather than shipping a short file
// without saying it.
func (s *Server) exportConversation(ctx context.Context, q db.Queryer, conversation exportConversation, messages, attachments *int, truncated *bool) (*exportConversation, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,user_id,content,created_at,COALESCE(edited_at,''),COALESCE(reply_to_id,''),COALESCE(thread_id,'')
		FROM messages WHERE conversation_id=? ORDER BY id LIMIT ?`, conversation.ID, maxExportMessages+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := exportConversation{ID: conversation.ID, Kind: conversation.Kind, Title: conversation.Title, Messages: []exportMessage{}}
	for rows.Next() {
		// The row ceiling is checked *inside* the read, so a conversation larger than
		// the whole export's budget stops rather than being materialised first.
		if *messages >= maxExportMessages {
			*truncated = true
			break
		}
		var message exportMessage
		if err := rows.Scan(&message.ID, &message.AuthorID, &message.Content, &message.CreatedAt, &message.EditedAt, &message.ReplyToID, &message.ThreadID); err != nil {
			return nil, err
		}
		message.Attachments = []exportAttachment{}
		message.Reactions = []exportReaction{}
		if *attachments < maxExportAttachments {
			files, err := q.QueryContext(ctx, `SELECT a.id,a.file_name,a.mime_type,a.file_size,a.created_at
				FROM attachments a JOIN messages m ON m.id=a.message_id WHERE m.id=?`, message.ID)
			if err != nil {
				return nil, err
			}
			for files.Next() {
				var file exportAttachment
				if err := files.Scan(&file.ID, &file.FileName, &file.MimeType, &file.Size, &file.CreatedAt); err != nil {
					files.Close()
					return nil, err
				}
				message.Attachments = append(message.Attachments, file)
				*attachments++
			}
			files.Close()
			reactions, err := q.QueryContext(ctx, `SELECT emoji,user_id,created_at FROM reactions WHERE message_id=?`, message.ID)
			if err != nil {
				return nil, err
			}
			for reactions.Next() {
				var reaction exportReaction
				if err := reactions.Scan(&reaction.Emoji, &reaction.UserID, &reaction.CreatedAt); err != nil {
					reactions.Close()
					return nil, err
				}
				message.Reactions = append(message.Reactions, reaction)
			}
			reactions.Close()
		} else {
			*truncated = true
		}
		out.Messages = append(out.Messages, message)
		*messages++
	}
	return &out, rows.Err()
}

func (s *Server) exportPath(exportID string) string {
	return filepath.Join(s.config.DataDir, "exports", exportID+".json")
}

// downloadExport streams a produced export to the account that made it.
//
// Re-read authority at download time as well as at build time: an export is produced
// while an account may export and downloaded while it still may. A withdrawal between
// the two has to be visible here, or the file outlives the permission that made it.
func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request, user db.User) {
	record, err := scanExport(s.store.QueryRow(
		`SELECT `+exportColumns+` FROM data_exports WHERE id=? AND user_id=?`, r.PathValue("id"), user.ID))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	switch record.State {
	case "building":
		problem(w, 409, "This export is still being prepared.")
		return
	case "failed":
		problem(w, 409, "This export could not be completed.")
		return
	}
	if record.ExpiresAt != "" && record.ExpiresAt <= db.Now() {
		problem(w, 410, "This export's server copy has expired. Request a new one.")
		return
	}
	// A row written before the scope column exists cannot be checked, so it is refused
	// rather than served. "We cannot tell what this contains" is not a reason to hand
	// it over; the member can request a new export, which will have a scope.
	var scope []string
	if err := json.Unmarshal([]byte(record.Scope), &scope); err != nil {
		problem(w, 410, "This export was made before the server could record what it contained. Request a new one.")
		return
	}
	// The response is registered before its access is resolved, with the export's
	// complete set of conversation scopes — not an empty scope pretending to be an
	// unclaimed upload. Registration before the decision puts the interval between the
	// check and the first byte inside the registry: a withdrawal that commits in that
	// interval either is seen by the checks below or ends this already-registered
	// response, whichever side of the commit the decision lands on (review 17, V17-02).
	ctx, done := s.deliveries.begin(scope, user.ID, currentSessionHash(r))
	defer done()
	if hook := s.streamAdmission.Load(); hook != nil {
		(*hook)()
	}
	// Admission re-establishes the account's authority and the session that made the
	// request, through the same protocol a file download uses. The middleware resolved
	// the caller before this response existed and a revocation can have committed in
	// between; the registry cancels what it already holds, and this decision is what
	// refuses what it does not.
	principal, err := s.admitStream(r.Context(), r, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if principal.Inactive {
		problem(w, 403, "This account is not currently permitted to download an export.")
		return
	}
	// The file is re-authorized against its own recorded scope, with the same rules a
	// conversation list or a history read uses. Checking only "does this account own
	// this export" is not enough: ownership of the *file* is not ownership of the
	// conversations inside it, and a member removed from a group kept receiving that
	// group's text from a file built before the removal — history 404, export 200, with
	// the message in the body (completion review 14, V14-02).
	for _, conversationID := range scope {
		readable, err := s.exportScopeStillReadable(r.Context(), s.store, principal, conversationID)
		if err != nil {
			s.failed(w, err)
			return
		}
		if !readable {
			problem(w, 403, "You can no longer read part of this export, so the server is not delivering it. Request a new export and it will contain only what you can read now.")
			return
		}
	}
	// A revocation that committed while this transfer was being admitted has already
	// ended the registered response. Serving now would write a status the body cannot
	// follow, so the refusal is stated instead.
	if ctx.Err() != nil {
		problem(w, 403, "Your access changed while this download was starting. Request a new export and it will contain only what you can read now.")
		return
	}
	file, err := os.Open(s.exportPath(record.ID))
	if os.IsNotExist(err) {
		// The row outlives its file when a build failed after the row was written, or
		// when the file was swept as an orphan. Either way there is nothing to serve and
		// the member is told to ask again rather than shown a 404 for an export they
		// are entitled to.
		problem(w, 410, "This export's file is no longer on the server. Request a new one.")
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		s.failed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// The name carries the build time rather than a request time, so a member with
	// several exports can tell them apart in their downloads folder.
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="brrpbrrp-export-%s-%s.json"`, user.Username, record.CreatedAt[:10]))
	// A no-store header: this is one account's history, and a cached copy in a shared
	// machine's proxy would outlive the session that asked for it.
	w.Header().Set("Cache-Control", "no-store")
	// Revocable, and range-capable: an export can be hundreds of megabytes, so a
	// client that is resuming a download issues a Range, and a transfer that runs for
	// minutes is exactly the one a withdrawal needs to interrupt.
	http.ServeContent(w, r, filepath.Base(s.exportPath(record.ID)), stat.ModTime(), revocableReader{ctx: ctx, reader: file})
}

// deleteExport is the member's own deletion of a produced file. It is offered because
// "request it and it expires" is a worse answer than "remove it now" for somebody who
// has just decided they do not want a copy.
func (s *Server) deleteExport(w http.ResponseWriter, r *http.Request, user db.User) {
	record, err := scanExport(s.store.QueryRow(
		`SELECT `+exportColumns+` FROM data_exports WHERE id=? AND user_id=?`, r.PathValue("id"), user.ID))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if record.State == "building" {
		problem(w, 409, "This export is still being prepared.")
		return
	}
	_ = os.Remove(s.exportPath(record.ID))
	if _, err := s.store.Exec(`DELETE FROM data_exports WHERE id=? AND user_id=?`, record.ID, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": record.ID, "deleted": true})
}

// expireExports removes lapsed server copies. It is bounded, and it is a sweep rather
// than a timer on each file: one pass is easier to reason about than a scheduled
// deletion that might not run.
func (s *Server) expireExports(ctx context.Context, q db.Queryer) (int, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,user_id FROM data_exports WHERE expires_at<>'' AND expires_at<=? LIMIT 200`, db.Now())
	if err != nil {
		return 0, err
	}
	lapsed := []exportRecord{}
	for rows.Next() {
		var record exportRecord
		if err := rows.Scan(&record.ID, &record.UserID); err != nil {
			rows.Close()
			return 0, err
		}
		lapsed = append(lapsed, record)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	removed := 0
	for _, record := range lapsed {
		_ = os.Remove(s.exportPath(record.ID))
		if _, err := q.ExecContext(ctx, `DELETE FROM data_exports WHERE id=?`, record.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// exportScopeStillReadable asks the *current* access question for one conversation, at
// delivery time. It is the same rules a conversation list uses, not a re-derivation of
// what the build decided: a member who lost a role, a membership, or a restricted
// channel's access since the build must not receive the file that predates it.
//
// There is no partial delivery. A file is a single document, and a member handed half
// of one has been handed content they cannot read by any other route, with no marker
// telling them which half. The refusal names the reason and points at the new export,
// which will be built from what is readable now.
func (s *Server) exportScopeStillReadable(ctx context.Context, q db.Queryer, principal authz.Principal, conversationID string) (bool, error) {
	var kind, state string
	if err := q.QueryRowContext(ctx, `SELECT kind,state FROM conversations WHERE id=?`, conversationID).Scan(&kind, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// A conversation that no longer exists cannot be read, whatever the file says.
			return false, nil
		}
		return false, err
	}
	if state != "active" {
		return false, nil
	}
	if kind != "channel" && !s.config.PrivateConversations {
		// The feature was turned off after this file was built. The private
		// conversations inside it are exactly what the operator disabled, and a file
		// already on disk would otherwise keep delivering them for its remaining
		// lifetime — which is up to seven days after the decision was made.
		//
		// The build is gated the same way (see `exportableConversations`), so a file
		// with a private conversation in it can only predate the change.
		return false, nil
	}
	if kind == "channel" {
		channel, err := s.store.Channel(ctx, q, conversationID)
		if err != nil {
			return false, nil
		}
		lists, err := s.store.ChannelAccessLists(ctx, q, conversationID)
		if err != nil {
			return false, err
		}
		return principal.MayReadChannel(channel, lists[conversationID]), nil
	}
	// A private conversation: current, accepted, usable membership. A withdrawn or
	// ended membership is exactly the case this check exists for, and it is the same
	// condition a history read would refuse.
	var member bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members
		WHERE conversation_id=? AND user_id=? AND state='accepted')`, conversationID, principal.UserID).Scan(&member); err != nil {
		return false, err
	}
	return member, nil
}

// exportIDsFor lists an account's export ids, for erasure to remove their files.
// It runs inside the erasure transaction because the rows cascade with the account:
// after the delete there is nothing left to name the files, and a file with no record
// is a file no sweep can ever reach.
func (s *Server) exportIDsFor(q db.Queryer, userID string) ([]string, error) {
	rows, err := q.QueryContext(context.Background(), `SELECT id FROM data_exports WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// errExportStorageFull reports that the server is already holding its ceiling of
// finished copies, so a new job cannot be admitted. It is a refusal rather than a
// silent admission: the alternative is a server that exceeds its own stated bound
// quietly, which is the failure a bound exists to prevent.
var errExportStorageFull = errors.New("this server is already holding the maximum number of prepared exports; try again after one expires, or delete one of yours")

// exportStorageBounds are the ceilings on finished copies. They are small because the
// export is a retrieval convenience, not an archive: a member who wants more asks
// again, and a server holding a year of one account's exports is a liability rather
// than a feature.
const (
	maxReadyExportsPerAccount = 3
	maxReadyExportsOnServer   = 50
)

// reclaimExportStorage removes the oldest finished copies until the account and the
// server are both inside their bounds. Files go with the rows, in that order, so a
// crash between the two leaves an orphan file rather than a row pointing at nothing —
// and the maintenance sweep, which works from the directory, cleans up orphans.
func (s *Server) reclaimExportStorage(ctx context.Context, tx *sql.Tx, userID string) error {
	// The server bound first, so one account cannot consume the whole allowance.
	var server int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM data_exports WHERE state='ready'`).Scan(&server); err != nil {
		return err
	}
	var account int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM data_exports WHERE state='ready' AND user_id=?`, userID).Scan(&account); err != nil {
		return err
	}
	// The count is of *finished* copies and this runs before the new one is built, so
	// the new one will make one more. The bound has to account for the copy being
	// created, not only for what exists: an account at the limit that asks for another
	// export is asking for the copy that tips it over.
	excess := account - (maxReadyExportsPerAccount - 1)
	// The server bound caps how much this account's excess may be relieved by: a
	// server inside its allowance has not earned the right to hold more on an
	// account's behalf, so a healthy server clamps nothing and a *full* one relieves
	// only as much as the room allows.
	//
	// `excess` is what this account is over by; the slack is what the server has spare.
	// The relief is the smaller of the two, and zero spare means no relief at all.
	if slack := maxReadyExportsOnServer - server; slack < excess {
		excess = slack
	}
	// A full server is a refusal, not a job admitted anyway.
	//
	// When the slack is zero the clamp above makes `excess` zero, the function returns
	// "nothing to do", and the new export is inserted anyway — so the server ends up
	// holding one more copy than its own ceiling says it should. The bound was
	// therefore only ever enforced for the *account*, and quietly not enforced for the
	// server at exactly the moment it mattered.
	if server >= maxReadyExportsOnServer {
		return errExportStorageFull
	}
	if excess <= 0 {
		return nil
	}
	// Oldest first. `id` is the stored creation order, which is also the export's
	// creation time, so this is the order a member would expect.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM data_exports WHERE state='ready'
		AND user_id=? ORDER BY id LIMIT ?`, userID, excess)
	if err != nil {
		return err
	}
	stale := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := tx.ExecContext(ctx, `DELETE FROM data_exports WHERE id=? AND state='ready'`, id); err != nil {
			return err
		}
	}
	// The file removal is queued rather than done here: this runs inside the request's
	// transaction, and unlinking a file a concurrent reader is streaming is a race the
	// download path already handles by refusing an unknown row. The row goes first,
	// so nothing can authorize a download of a file we are about to remove.
	s.exportRemovalsMu.Lock()
	s.pendingExportRemovals = append(s.pendingExportRemovals, stale...)
	s.exportRemovalsMu.Unlock()
	return nil
}

// drainExportRemovals unlinks the files whose rows have already gone. It runs after a
// commit, from the same place the rest of the post-commit cleanup runs.
func (s *Server) drainExportRemovals() {
	s.exportRemovalsMu.Lock()
	pending := s.pendingExportRemovals
	s.pendingExportRemovals = nil
	s.exportRemovalsMu.Unlock()
	for _, id := range pending {
		if err := os.Remove(s.exportPath(id)); err != nil && !os.IsNotExist(err) {
			s.log.Warn("a reclaimed export file could not be removed", "export", id, "error", err)
		}
	}
}

// abandonStaleExports closes jobs that no process is running.
//
// A `building` row is the only thing blocking that account from asking again, and a
// crash between the insert and the build finishing left one behind permanently: the
// member's later request was answered "an export is already being prepared" by a job
// that no longer exists, and nothing in the product could tell the difference.
//
// A row older than the longest a build can take is not a build. The grace is generous
// because being wrong in this direction is cheap — a build that is genuinely still
// running is not expected to outlive an hour on any supported host, and the worst case
// of marking it failed is that a member asks again.
func (s *Server) abandonStaleExports(ctx context.Context, q db.Queryer, grace time.Duration) (int, error) {
	result, err := q.ExecContext(ctx, `UPDATE data_exports SET state='failed',completed_at=?,omissions='the server stopped before this export finished'
		WHERE id IN (SELECT id FROM data_exports WHERE state='building' AND created_at<? LIMIT 100)`,
		db.Now(), db.Timestamp(time.Now().Add(-grace)))
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// recordTruncatedExport closes a job that reached its byte ceiling.
//
// The row is marked `failed` rather than `ready`, because a member offered a
// "successful" export that has no file is a worse outcome than a refusal: the
// interface would show a download button for something that 404s. The counts and the
// omission are still recorded, so the member can see how far the build got.
func (s *Server) recordTruncatedExport(ctx context.Context, tx *sql.Tx, exportID, userID string, document exportDocument) error {
	now := db.Now()
	_, err := tx.ExecContext(ctx, `UPDATE data_exports SET state='failed',completed_at=?,conversation_count=?,message_count=?,
		attachment_count=?,bytes_written=0,truncated=1,omissions=?,completed_by=?
		WHERE id=? AND user_id=? AND state='building'`,
		now, document.Summary.Conversations, document.Summary.Messages, document.Summary.Attachments,
		strings.Join(document.Omissions, "; "), userID, exportID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
