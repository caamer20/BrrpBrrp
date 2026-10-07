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
	address, open := s.guestOpen(w, r)
	if !open {
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
		// Not a name any account can have, so there is no account to count it against.
		s.guestFailed(address)
		problem(w, 401, "Incorrect username or password.")
		return
	}
	// Decided from the counter alone, before the account is looked up, so the answer is
	// the same for a name that exists and one that does not.
	attempts := signInKey(address, input.Username)
	if s.guests.blocked(attempts, credentialFailuresPerKey) {
		refuseAttempts(w, tooManySignIns)
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
		// Match the cost of checking a real account without leaking usernames, and count
		// it exactly as a wrong password for a real one is counted.
		_, _ = bcrypt.GenerateFromPassword([]byte(input.Password), 12)
		s.guestFailed(address, attempts)
		problem(w, 401, "Incorrect username or password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil || banned {
		s.guestFailed(address, attempts)
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
	// The password was right, so none of the failure counters move. What is spent is the
	// account's own rate, which only somebody holding the password can reach.
	if !s.limiter.allow("signin-rate:"+id, s.credentialRate(), credentialWindow) {
		refuseAttempts(w, tooManySignIns)
		return
	}
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

// updateMe lets a member change the two parts of their own identity that the community
// already sees — their display name and their avatar color — without asking an
// administrator to do it for them. Every other field the administrator route can change
// is authority, access or somebody else's data, and stays there.
//
// The validation is deliberately the administrator route's own rules — the same 80-byte
// bound on a display name and the same six-digit hex pattern — so the two routes cannot
// accept different values for the same two columns.
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		DisplayName *string `json:"display_name"`
		AvatarColor *string `json:"avatar_color"`
	}
	if !decode(w, r, &input) {
		return
	}
	// What the request sent is validated before the transaction and applied after the
	// account is re-read inside it. The field the request does not name keeps the value
	// the committing transaction sees, not the session's older copy of it, so a color
	// changed a moment ago by the administrator's route is not written back from a
	// stale row by a rename.
	if input.DisplayName != nil && len(*input.DisplayName) > 80 {
		problem(w, 400, "Display name is too long.")
		return
	}
	if input.AvatarColor != nil && !avatarColorPattern.MatchString(*input.AvatarColor) {
		problem(w, 400, "Choose a six-digit hex color.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// The account is read inside the committing transaction, exactly as on the
	// administrator's route: a ban, deactivation or removal that committed while this
	// request was in flight is what the change is decided against, not the session's
	// older copy of the row. It is the difference between a rename landing on an account
	// that was deactivated a moment ago and being refused.
	target, err := s.store.Account(r.Context(), tx, user.ID)
	if !s.found(w, err) {
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "A removed account cannot be edited.")
		return
	}
	if target.IsDeactivated || target.IsBanned {
		problem(w, http.StatusForbidden, "This account cannot be changed while it is deactivated or suspended.")
		return
	}
	displayName, avatarColor := target.DisplayName, target.AvatarColor
	if input.DisplayName != nil {
		displayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.AvatarColor != nil {
		avatarColor = *input.AvatarColor
	}
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	if _, err := tx.Exec(`UPDATE users SET display_name=?,avatar_color=?,updated_at=? WHERE id=?`, displayName, avatarColor, db.Now(), target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// A rename is a visible identity change, and it has its record for the same reason
	// an administrator's edit does: the audit log says who changed an account and when.
	if err := s.auditTx(r.Context(), tx, user.ID, "user.updated", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	target.DisplayName, target.AvatarColor = displayName, avatarColor
	// The same event the administrator's route emits, so every list that shows this
	// account's name or color refreshes without a reload.
	s.hub.Broadcast("user.updated", map[string]string{"id": target.ID})
	// The response has the shape of GET /api/me, including the caller's own timeout
	// state, so a client can replace its copy of the account with it rather than
	// keeping two accounts in its head.
	s.describeOwnTimeout(r.Context(), &target)
	respond(w, 200, target)
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
	if !s.credentialOpen(w, user.ID) || !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	var currentHash string
	if err := s.store.QueryRow(`SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&currentHash); err != nil {
		s.failed(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(input.Current)) != nil {
		s.credentialFailed(user.ID)
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

// The credential throttle (decision D-17 in docs/security.md).
//
// It counts failures, never attempts. The earlier throttle spent one budget per address
// on every sign-in, password change, setup, second-factor change and invitation
// acceptance whether or not it succeeded, so a community that arrived from one address —
// a household, an office, Docker Desktop's bridge, a proxy that was not declared trusted
// — was refused at its twenty-first request although nobody had failed, and one
// invitation may admit fifty people. A correct password, a valid invitation and a valid
// setup token now spend nothing.
//
// There are three counters:
//
//   - an address's failures, in the guests table, with the operator's
//     BRRP_LOGIN_ATTEMPTS as the ceiling. It is checked first, so an address that has
//     reached it creates no further keys;
//   - an address's failures against one username, also in guests, which bounds guessing
//     at a single account when an operator has raised the address ceiling for a shared
//     address. A failure from another address lands on another key, so nobody can lock a
//     member out of their own account from somewhere else;
//   - a signed-in account's failed password or code re-checks, in the members' table,
//     keyed by the account rather than by where the request came from.
//
// What none of them bounds is guessing one account from many addresses. That is bounded
// by the cost of a hash and the two password slots, which every password check on the
// server passes through.
//
// A correct password is not free either — it costs a hash and, at sign-in, leaves a
// session behind — so success has a *rate*, per account and far above anything a person
// does (credentialRate). It exists so that one account holding valid credentials cannot
// occupy the password slots or grow the session table without bound, which the old
// per-address budget prevented only by accident.
const (
	credentialWindow = 5 * time.Minute
	// credentialFailuresPerKey is how many failures one username from one address, or one
	// signed-in account, absorbs in a window.
	credentialFailuresPerKey = 10
)

const (
	tooManySignIns  = "Too many sign-in attempts. Try again in five minutes."
	tooManyRechecks = "Too many incorrect attempts. Try again in five minutes."
)

// addressCeiling is the configured budget of failures per address (app.LoginAttemptsDefault
// unless the operator raised it): a zero value can only come from a Config built in a
// test, and it means the default, never "no limit".
func (s *Server) addressCeiling() int {
	if s.config.LoginAttempts < 1 {
		return app.LoginAttemptsDefault
	}
	return s.config.LoginAttempts
}

// credentialRate is how many times one account may present a correct password in a
// window: three times the address ceiling, so sixty by default. A person signing in on
// every device they own does not come near it.
func (s *Server) credentialRate() int { return 3 * s.addressCeiling() }

func refuseAttempts(w http.ResponseWriter, message string) {
	w.Header().Set("Retry-After", "300")
	problem(w, http.StatusTooManyRequests, message)
}

// guestOpen answers whether an unauthenticated caller's address may still try, and returns
// the key its failures are counted under. Asking spends nothing.
func (s *Server) guestOpen(w http.ResponseWriter, r *http.Request) (string, bool) {
	address := "address:" + addressKey(s.clientIP(r))
	if s.guests.blocked(address, s.addressCeiling()) {
		refuseAttempts(w, tooManySignIns)
		return address, false
	}
	return address, true
}

// signInKey pairs an address with the username it tried. The name is folded to lower case
// because that is how accounts are stored, so two spellings of one name share a counter.
func signInKey(address, username string) string {
	return "signin:" + address + "|" + strings.ToLower(username)
}

// guestFailed records one failure against the address and against any further key the
// caller names. A failure that cannot be recorded — the table is full of other callers'
// counters — is not turned into a refusal for anybody; it is logged, at most once a
// window, because an operator should know the throttle is under that kind of pressure.
func (s *Server) guestFailed(address string, more ...string) {
	recorded := s.guests.note(address, credentialWindow)
	for _, key := range more {
		recorded = s.guests.note(key, credentialWindow) && recorded
	}
	if recorded || s.guests == nil {
		return
	}
	now := time.Now().Unix()
	last := s.guestsFullWarned.Load()
	if now-last >= int64(credentialWindow/time.Second) && s.guestsFullWarned.CompareAndSwap(last, now) && s.log != nil {
		s.log.Warn("the sign-in throttle is full; a failed attempt was not counted", "capacity", guestLimiterCapacity)
	}
}

// credentialOpen answers whether a signed-in account may still re-enter its password or a
// code, and credentialFailed records a wrong one. The key is the account, so where the
// request came from — and who else is behind that address — does not enter into it.
func (s *Server) credentialOpen(w http.ResponseWriter, userID string) bool {
	if s.limiter.blocked("credential:"+userID, credentialFailuresPerKey) || !s.limiter.allow("credential-rate:"+userID, s.credentialRate(), credentialWindow) {
		refuseAttempts(w, tooManyRechecks)
		return false
	}
	return true
}

func (s *Server) credentialFailed(userID string) {
	s.limiter.note("credential:"+userID, credentialWindow)
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
