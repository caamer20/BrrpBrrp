package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"golang.org/x/crypto/bcrypt"
)

const cookieName = "brrp_session"

// userColumns and scanUser keep the account shape in one place (internal/db) so
// a new column cannot be added to one reader and missed by another.
const userColumns = db.UserColumns

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

func scanUser(row interface{ Scan(...any) error }) (db.User, error) {
	return db.ScanUser(row)
}

func (s *Server) authenticate(r *http.Request) (db.User, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) != 43 {
		return db.User{}, errors.New("missing session")
	}
	return scanUser(s.store.QueryRowContext(r.Context(), `SELECT `+userColumns+` FROM users WHERE is_banned=0 AND is_deactivated=0 AND id=(SELECT user_id FROM sessions WHERE token_hash=? AND expires_at>?)`, hashToken(cookie.Value), db.Now()))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.limitLogin(w, r) {
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validPassword(input.Password) || !usernamePattern.MatchString(input.Username) {
		problem(w, 401, "Incorrect username or password.")
		return
	}
	if !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	var id, hash string
	var banned bool
	err := s.store.QueryRowContext(r.Context(), `SELECT id,password_hash,(is_banned OR is_deactivated) FROM users WHERE username=?`, input.Username).Scan(&id, &hash, &banned)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	if hash == "" {
		// Match the cost of checking a real account without leaking usernames.
		_, _ = bcrypt.GenerateFromPassword([]byte(input.Password), 12)
		problem(w, 401, "Incorrect username or password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil || banned {
		problem(w, 401, "Incorrect username or password.")
		return
	}
	// A timeout is a moderation decision, not a failed sign-in, and the person is owed
	// the truth about it: the password was right. So the sign-in succeeds, the session
	// is created, and the community is closed to them until it ends.
	//
	// Refusing here is what made a community timeout behave as an installation ban — no
	// sign-in, no session, and therefore no way to read the private conversations the
	// timeout never decided anything about. The comment above this branch described the
	// intended behaviour and the code did the opposite for as long as the two states
	// shared a flag.
	//
	// An expired timeout is still lifted here, before the check, so signing in is
	// itself the thing that clears it.
	s.timeoutState(r.Context(), id)
	if !s.completeLogin(w, r, id, hash, input.Code) {
		return
	}
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		s.failed(w, err)
		return
	}
	// Sign-in is the one moment a timed-out member is told what is happening to them,
	// because it is the moment they are looking for it: they know they were put in a
	// timeout and they are checking whether it is over. Without this the interface
	// signs them in and then fails every community request with no explanation.
	s.describeOwnTimeout(r.Context(), &user)
	respond(w, 200, user)
}

// describeOwnTimeout fills in the caller's own moderation-timeout state.
//
// It is deliberately a separate step rather than part of `scanUser`. Every other route
// that scans a user — the administrator's account list, a message author, an audit
// row — must keep these fields zero, because a timeout is moderation information about
// one account and the ordinary readers of `users` are not entitled to it. Applying it
// in the shared scanner would leak it into every response that mentions a person.
func (s *Server) describeOwnTimeout(ctx context.Context, user *db.User) {
	if suspended, until := s.timeoutState(ctx, user.ID); suspended {
		user.InTimeout, user.TimeoutEndsAt = true, until
	}
}

func (s *Server) newSession(w http.ResponseWriter, userID string) error {
	return s.newSessionFor(w, nil, userID)
}

// newSessionFor records where a session came from and marks it as freshly
// authenticated, because it was created by a completed authentication. The client
// description is untrusted display data for the owner's own session list.
func (s *Server) newSessionFor(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	agent := ""
	if r != nil {
		agent = db.SanitizeUserAgent(r.UserAgent())
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	now := db.Now()
	result, err := s.store.Exec(`INSERT INTO sessions(id,user_id,token_hash,created_at,expires_at,last_seen_at,user_agent,privileged_at) SELECT ?,id,?,?,?,?,?,? FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0`, db.ID(), hashToken(token), now, db.Timestamp(expires), now, agent, now, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("account is unavailable")
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", Expires: expires, MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, Secure: s.config.SecureCookie(), SameSite: http.SameSiteStrictMode})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, user db.User) {
	cookie, _ := r.Cookie(cookieName)
	hash := hashToken(cookie.Value)
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash=?`, hash); err != nil {
		s.failed(w, err)
		return
	}
	// Only this session ends, so only its connections are invalidated: the account's
	// other devices keep receiving their events. The commit and the invalidation are one
	// step under the fence, and the cleanup that follows may take as long as it needs.
	if err := s.commitAuthorityChangeFor(r.Context(), tx, "You signed out.", ws.Audience{SessionHashes: []string{hash}}); err != nil {
		s.failed(w, err)
		return
	}
	s.clearCookie(w)
	s.leaveVoice(user.ID, "")
	s.hub.Disconnect("", hash)
	s.deliveries.revokeSession(hash)
	respond(w, 200, map[string]bool{"ok": true})
}

// me describes the caller to themselves, so it reports the caller's own live
// moderation timeout for the same reason sign-in does. The client refreshes this on
// reconnect and after an authority change, which is when a member whose timeout
// expires while they are signed in needs to learn that the community is open again —
// without it, the interface keeps rendering the timeout until they sign out and in.
func (s *Server) me(w http.ResponseWriter, r *http.Request, user db.User) {
	s.describeOwnTimeout(r.Context(), &user)
	respond(w, 200, user)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validPassword(input.New) || !validPassword(input.Current) {
		problem(w, 400, "Passwords must contain 12–72 bytes.")
		return
	}
	if !s.limitLogin(w, r) || !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	var currentHash string
	if err := s.store.QueryRow(`SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&currentHash); err != nil {
		s.failed(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(input.Current)) != nil {
		problem(w, 400, "Current password is incorrect.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.New), 12)
	if err != nil {
		s.failed(w, err)
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	if _, err = tx.Exec(`UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, string(hash), db.Now(), user.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE user_id=?`, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	// A credential change always has its record. The audit row commits with the
	// change itself, so an account's password cannot move without the entry that
	// says who changed it and when.
	if err = s.auditTx(r.Context(), tx, user.ID, "user.password_changed", user.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Every session of the account was removed above, so every connection of the account
	// is invalidated with the commit. A client still holding the old cookie cannot keep
	// receiving events or streams while the slower cleanup (streams, voice, sockets) runs.
	if err = s.commitAuthorityChangeFor(r.Context(), tx, "Your password changed.", ws.Audience{UserIDs: []string{user.ID}}); err != nil {
		s.failed(w, err)
		return
	}
	s.clearCookie(w)
	s.leaveVoice(user.ID, "")
	s.hub.Disconnect(user.ID, "")
	// A password change ends every stream the old credential admitted, on every
	// device, together with the sessions and subscriptions removed above.
	s.deliveries.revokeAccount(user.ID)
	respond(w, 200, map[string]bool{"sign_in_required": true})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.config.SecureCookie(), SameSite: http.SameSiteStrictMode})
}

// limitLogin throttles sign-in attempts per address. The ceiling is the configured
// budget (app.LoginAttemptsDefault unless the operator raised it): a zero value can only
// come from a Config built in a test, and it means the default, never "no limit".
func (s *Server) limitLogin(w http.ResponseWriter, r *http.Request) bool {
	limit := s.config.LoginAttempts
	if limit < 1 {
		limit = app.LoginAttemptsDefault
	}
	if !s.limiter.allow("login:"+s.clientIP(r), limit, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		problem(w, 429, "Too many sign-in attempts. Try again in five minutes.")
		return false
	}
	return true
}

func (s *Server) acquirePasswordSlot(w http.ResponseWriter) bool {
	select {
	case s.passwordSlots <- struct{}{}:
		return true
	default:
		problem(w, 503, "The server is busy. Try again shortly.")
		return false
	}
}
func (s *Server) releasePasswordSlot() { <-s.passwordSlots }
func validPassword(value string) bool {
	return len(value) >= 12 && len(value) <= 72 && strings.TrimSpace(value) != ""
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
