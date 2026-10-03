package api

import (
	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Upload failures that are the caller's fault rather than the server's.
var (
	errUploadTooLarge = errors.New("upload exceeds the size limit")
	errUploadEmpty    = errors.New("the uploaded file is empty")
	errUploadNotOne   = errors.New("more than one part in the upload")
)

// diskGuard reports why an upload could not be accepted for want of free space, or ""
// when there is room.
//
// The size it plans for is the largest upload this installation accepts rather than the
// file's own size, because the file's size is not known until it has been read. That
// makes the guard conservative by design: the reserve is room for the work that keeps an
// installation recoverable — a database vacuum, a backup archive, the writes of the next
// minute — not a budget to spend to the last byte.
func (s *Server) diskGuard(size int64) string {
	reserve := s.config.StorageReserveBytes
	if reserve <= 0 {
		return ""
	}
	free, err := s.freeSpace(s.config.UploadDir)
	if err != nil {
		// A path that cannot be measured is a path that cannot be written either, and the
		// write below will say so with the real error.
		return ""
	}
	if free < uint64(reserve)+uint64(size) {
		return fmt.Sprintf(
			"The server is low on disk space (it keeps %d bytes free for backups and recovery). Ask the operator to free space or move the data directory to a larger volume.",
			reserve)
	}
	return ""
}

// quotaRefusal reports why this many stored bytes would exceed the installation's storage
// quota, or "" when they fit.
//
// It is asked *after* the file exists and with the real size, and the caller removes the
// file when it refuses: the alternative is deciding before the bytes are counted, which
// either refuses files that would fit or accepts files that will not. A read that fails is
// reported rather than ignored, because a guard that silently stops guarding is worse than
// one that refuses and says why.
func (s *Server) quotaRefusal(ctx context.Context, size int64) (string, error) {
	quota := s.config.StorageQuotaBytes
	if quota <= 0 {
		return "", nil
	}
	var used int64
	// Every row counts, staged or attached: a staged upload is bytes on the volume just as
	// much as a sent one, and maintenance is what reclaims the ones nobody claims.
	if err := s.store.QueryRowContext(ctx, `SELECT COALESCE(SUM(file_size),0) FROM attachments`).Scan(&used); err != nil {
		return "", err
	}
	if used+size > quota {
		return fmt.Sprintf(
			"The server's upload storage is full (%d of %d bytes used). Ask the operator for more, or remove files that are no longer needed.",
			used, quota), nil
	}
	return "", nil
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.limiter.allow("upload:"+user.ID, 10, time.Minute) {
		problem(w, 429, "Upload limit reached. Try again in a minute.")
		return
	}
	scopeID := r.URL.Query().Get("conversation_id")
	if scopeID != "" {
		if _, _, ok := s.requireScope(w, r, user, scopeID, true); !ok {
			return
		}
	}
	limit := min(s.config.MaxUploadBytes, s.Settings().MaxUploadBytes)
	// The disk guard is answered before a byte is written: the reason it exists is that an
	// installation which fills its volume can no longer back itself up, and a refusal
	// after the file exists is a cleanup problem. It is deliberately conservative — the
	// size it plans for is the largest upload this installation accepts — because the
	// reserve is room for recovery work rather than a budget to spend to the byte.
	if refused := s.diskGuard(limit); refused != "" {
		problem(w, http.StatusInsufficientStorage, refused)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		problem(w, 400, "Choose a file to upload.")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		problem(w, 400, "Upload one file in the file field.")
		return
	}
	defer part.Close()
	name := strings.ReplaceAll(part.FileName(), "\\", "/")
	name = filepath.Base(name)
	if len(name) > 255 || strings.ContainsAny(name, "\r\n\x00") {
		problem(w, 400, "The file name is too long or invalid.")
		return
	}
	id := db.ID()
	path, err := app.Contained(s.config.DataDir, filepath.Join(s.config.UploadDir, id))
	if err != nil {
		s.failed(w, err)
		return
	}
	var (
		size        int64
		contentType string
	)
	// The file is written, flushed and published before the row that references it
	// exists, so no commit can promise bytes that are not on disk yet, and a reader
	// never finds a half-written file under its final name.
	published, err := app.Publish(filepath.Dir(path), filepath.Base(path), func(file *os.File) error {
		header := make([]byte, 512)
		n, readErr := io.ReadFull(part, header)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return errUploadTooLarge
		}
		contentType = http.DetectContentType(header[:n])
		if _, err := file.Write(header[:n]); err != nil {
			return err
		}
		copied, err := io.Copy(file, io.LimitReader(part, limit+1-int64(n)))
		size = int64(n) + copied
		if err != nil || size > limit {
			return errUploadTooLarge
		}
		if size == 0 {
			return errUploadEmpty
		}
		if _, err := reader.NextPart(); err != io.EOF {
			return errUploadNotOne
		}
		return nil
	})
	switch {
	case errors.Is(err, errUploadTooLarge):
		problem(w, 413, fmt.Sprintf("Files must be no larger than %d bytes.", limit))
		return
	case errors.Is(err, errUploadEmpty):
		problem(w, 400, "The file is empty.")
		return
	case errors.Is(err, errUploadNotOne):
		problem(w, 400, "Upload one file at a time.")
		return
	case errors.Is(err, syscall.ENOSPC):
		// The pre-check is a moment old by the time it is acted on, and the disk can fill
		// from anything else on the host. The write that finds the volume full is refused
		// as a storage problem rather than reported as a server fault.
		problem(w, http.StatusInsufficientStorage, "The server is out of disk space. Free space or raise the volume, then try again.")
		return
	case err != nil:
		s.failed(w, err)
		return
	}
	// The quota is asked with the real size, now that the bytes are counted and before any
	// row promises them: a refusal here is one removal away from leaving nothing behind,
	// which is why it is not decided from the configured ceiling like the disk guard.
	if refused, err := s.quotaRefusal(r.Context(), size); err != nil {
		_ = os.Remove(published)
		s.failed(w, err)
		return
	} else if refused != "" {
		_ = os.Remove(published)
		problem(w, http.StatusInsufficientStorage, refused)
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		_ = os.Remove(published)
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	var principal authz.Principal
	if scopeID != "" {
		_, principal, err = s.scopeInTx(r.Context(), tx, user, scopeID, true)
	} else {
		principal, err = s.authorizeInTx(r.Context(), tx, user)
	}
	// Destination access and the route capability are separate permissions.
	// Uploading bytes can outlast either one; both must still authorize the row
	// that promises those bytes when this transaction commits.
	if err == nil && !principal.Allowed(authz.MessageUpload) {
		err = authz.Denial{Reason: "Your role does not include message.upload."}
	}
	if err != nil {
		_ = os.Remove(published)
		s.scopeFailed(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at,staged_conversation_id) VALUES(?,?,?,?,?,?,?,NULLIF(?,''))`, id, user.ID, name, id, size, contentType, db.Now(), scopeID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		_ = os.Remove(published)
		s.failed(w, err)
		return
	}
	respond(w, 201, db.Attachment{ID: id, UploaderID: user.ID, FileName: name, FileSize: size, MIMEType: contentType})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, user db.User) {
	var attachment db.Attachment
	var storedName string
	err := s.store.QueryRow(`SELECT id,COALESCE(message_id,''),uploader_id,file_name,file_size,mime_type,stored_name FROM attachments WHERE id=?`, r.PathValue("id")).Scan(&attachment.ID, &attachment.MessageID, &attachment.UploaderID, &attachment.FileName, &attachment.FileSize, &attachment.MIMEType, &storedName)
	if !s.found(w, err) {
		return
	}
	// The response is registered before its access is resolved, so the window between
	// the authorization decision and the first byte belongs to the registry: access
	// revoked in that window ends this response instead of escaping it. The channel
	// identifies a channel-wide revocation, and the account and session identify the
	// ones that end every stream a role change or a signed-out device admitted.
	channelID := ""
	if attachment.MessageID != "" {
		// The channel is read without an authorization decision: it only tags the
		// stream for a channel-wide revocation, and the request still has to pass the
		// message's access check below.
		if err := s.store.QueryRowContext(r.Context(), `SELECT conversation_id FROM messages WHERE id=?`, attachment.MessageID).Scan(&channelID); err != nil {
			channelID = ""
		}
	}
	// A claimed attachment is tagged with its conversation, so a channel-wide
	// revocation and a re-evaluation of the account's access both reach it. An
	// unclaimed upload carries no scope: it belongs to its uploader alone.
	var scopes []string
	if channelID != "" {
		scopes = []string{channelID}
	}
	ctx, done := s.deliveries.begin(scopes, user.ID, currentSessionHash(r))
	defer done()
	if hook := s.streamAdmission.Load(); hook != nil {
		(*hook)()
	}
	// The principal that reached this handler was resolved by the middleware, before the
	// response was registered. A revocation can have committed in between, and it would
	// be observed by neither the registry (which cancels what it already holds) nor the
	// next request: this response would stream under authority that no longer exists. So
	// admission re-establishes the account's authority and the session that made the
	// request, and the file's own access is checked against that decision.
	principal, err := s.admitStream(r.Context(), r, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if attachment.MessageID == "" {
		// An unclaimed upload belongs to its uploader alone.
		if attachment.UploaderID != user.ID {
			problem(w, 404, "File not found.")
			return
		}
	} else {
		_, _, ok := s.requireMessage(w, r, principal, attachment.MessageID)
		if !ok {
			// Access is resolved now, through the file's current conversation. A
			// retained URL cannot outlive the access that produced it, and a
			// revocation that happened after registration has already ended this
			// response through the context above.
			return
		}
	}
	path, err := app.Contained(s.config.DataDir, filepath.Join(s.config.UploadDir, storedName))
	if err != nil {
		s.failed(w, err)
		return
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		problem(w, 404, "File not found.")
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	defer file.Close()
	if r.URL.Query().Get("preview") == "1" {
		s.thumbnail(ctx, w, r, attachment, file)
		return
	}
	stat, err := file.Stat()
	if err != nil {
		s.failed(w, err)
		return
	}
	disposition := "attachment"
	switch attachment.MIMEType {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "audio/mpeg", "audio/ogg", "video/mp4", "video/webm":
		disposition = "inline"
	}
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.FileName}))
	w.Header().Set("Content-Type", attachment.MIMEType)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, attachment.FileName, stat.ModTime(), revocableReader{ctx: ctx, reader: file})
}
