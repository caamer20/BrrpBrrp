package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) mfaStatus(w http.ResponseWriter, r *http.Request, user db.User) {
	var enabled bool
	var remaining int
	err := s.store.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND secret!=''), (SELECT count(*) FROM recovery_codes WHERE user_id=?)`, user.ID, user.ID).Scan(&enabled, &remaining)
	if err != nil {
		s.failed(w, err)
		return
	}
	// The client needs to know whether an enrolled factor is required of this
	// account (D-10: owners and administrators), and whether privileged actions are
	// currently unlocked, so it can prompt instead of failing a submission.
	privileged, err := s.store.PrivilegedAt(r.Context(), s.store, currentSessionHash(r))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]any{
		"enabled":                  enabled,
		"recovery_codes_remaining": remaining,
		"required":                 s.Settings().RequirePrivilegedMFA && (user.IsAdmin || user.IsOwner),
		"recently_authenticated":   authenticationIsRecent(privileged, time.Now()),
	})
}

func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, userID, password string) bool {
	if !s.limitLogin(w, r) || !s.acquirePasswordSlot(w) {
		return false
	}
	defer s.releasePasswordSlot()
	var hash string
	if err := s.store.QueryRow(`SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash); err != nil {
		s.failed(w, err)
		return false
	}
	if !validPassword(password) || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		problem(w, 400, "Current password is incorrect.")
		return false
	}
	return true
}

func (s *Server) setupMFA(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &input) || !s.checkPassword(w, r, user.ID, input.Password) {
		return
	}
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		s.failed(w, err)
		return
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
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
	result, err := tx.ExecContext(r.Context(), `INSERT INTO user_mfa(user_id,pending_secret,pending_until) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET pending_secret=excluded.pending_secret,pending_until=excluded.pending_until WHERE user_mfa.secret=''`, user.ID, secret, db.Timestamp(time.Now().Add(10*time.Minute)))
	if err != nil {
		s.failed(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		problem(w, 409, "Two-factor authentication is already enabled.")
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	query := url.Values{"secret": {secret}, "issuer": {"BrrpBrrp"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	respond(w, 200, map[string]string{"secret": secret, "uri": "otpauth://totp/" + url.PathEscape("BrrpBrrp:"+user.Username) + "?" + query.Encode()})
}

func (s *Server) enableMFA(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &input) || !s.limitLogin(w, r) {
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
	cookie, _ := r.Cookie(cookieName)
	var secret string
	err = tx.QueryRow(`SELECT pending_secret FROM user_mfa WHERE user_id=? AND secret='' AND pending_until>?`, user.ID, db.Now()).Scan(&secret)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	counter, valid := matchTOTP(secret, strings.TrimSpace(input.Code), time.Now(), -1)
	if !valid {
		problem(w, 400, "The code is incorrect or setup has expired.")
		return
	}
	if _, err = tx.Exec(`UPDATE user_mfa SET secret=pending_secret,pending_secret='',pending_until='',last_counter=? WHERE user_id=?`, counter, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	codes := make([]string, 10)
	for i := range codes {
		bytes := make([]byte, 10)
		if _, err = rand.Read(bytes); err != nil {
			s.failed(w, err)
			return
		}
		codes[i] = strings.ToUpper(hex.EncodeToString(bytes))
		if _, err = tx.Exec(`INSERT INTO recovery_codes(user_id,code_hash) VALUES(?,?)`, user.ID, hashToken(codes[i])); err != nil {
			s.failed(w, err)
			return
		}
	}
	// Preserve this authenticated session; all other devices must sign in with MFA.
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash!=?`, user.ID, hashToken(cookie.Value)); err != nil {
		s.failed(w, err)
		return
	}
	// Enrolling a factor is itself a strong authentication: the account just
	// proved a password and a code, so the surviving session is fresh.
	if err = db.MarkSessionPrivileged(r.Context(), tx, hashToken(cookie.Value)); err != nil {
		s.failed(w, err)
		return
	}
	// Enrolling a factor changes how the account authenticates, so its record
	// commits with the change rather than after it.
	if err = s.auditTx(r.Context(), tx, user.ID, "mfa.enabled", user.ID); err != nil {
		s.failed(w, err)
		return
	}
	// The factor change commits with the invalidation of the account's connections, so a
	// device whose session was just removed cannot be handed an event while the stream
	// and socket cleanup below is still running.
	if err = s.commitAuthorityChangeFor(r.Context(), tx, "Your account's sign-in changed.", ws.Audience{UserIDs: []string{user.ID}}); err != nil {
		s.failed(w, err)
		return
	}
	s.hub.Disconnect(user.ID, "")
	// Enrolling a factor removes the other devices' sessions above, so the streams
	// they had already been admitted for end with them.
	s.deliveries.revokeAccountExceptSession(user.ID, hashToken(cookie.Value))
	respond(w, 200, map[string]any{"recovery_codes": codes})
}

// Consume a factor in one transaction so the same code cannot authorize two requests.
func consumeFactor(tx *sql.Tx, userID, code string) (bool, error) {
	var secret string
	var last int64
	err := tx.QueryRow(`SELECT secret,last_counter FROM user_mfa WHERE user_id=? AND secret!=''`, userID).Scan(&secret, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if counter, valid := matchTOTP(secret, strings.TrimSpace(code), time.Now(), last); valid {
		_, err = tx.Exec(`UPDATE user_mfa SET last_counter=? WHERE user_id=?`, counter, userID)
		return err == nil, err
	}
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	result, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id=? AND code_hash=?`, userID, hashToken(code))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Server) disableMFA(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &input) || !s.checkPassword(w, r, user.ID, input.Password) {
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
	valid, err := consumeFactor(tx, user.ID, input.Code)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !valid {
		problem(w, 400, "The code is incorrect or has already been used.")
		return
	}
	if _, err = tx.Exec(`DELETE FROM user_mfa WHERE user_id=?`, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err = tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	cookie, _ := r.Cookie(cookieName)
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash!=?`, user.ID, hashToken(cookie.Value)); err != nil {
		s.failed(w, err)
		return
	}
	// Removing a factor required a password and a code, so this session is fresh.
	if err = db.MarkSessionPrivileged(r.Context(), tx, hashToken(cookie.Value)); err != nil {
		s.failed(w, err)
		return
	}
	// Turning a factor off weakens every privileged action that depends on it, so
	// its record commits with the change rather than after it.
	if err = s.auditTx(r.Context(), tx, user.ID, "mfa.disabled", user.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err = s.commitAuthorityChangeFor(r.Context(), tx, "Your account's sign-in changed.", ws.Audience{UserIDs: []string{user.ID}}); err != nil {
		s.failed(w, err)
		return
	}
	s.hub.Disconnect(user.ID, "")
	s.deliveries.revokeAccountExceptSession(user.ID, hashToken(cookie.Value))
	respond(w, 200, map[string]bool{"ok": true})
}
