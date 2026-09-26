package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"brrpbrrp/internal/db"
)

// Recheck account access and consume MFA in the same transaction that creates
// the session. Password resets or MFA enrollment cannot race a sign-in.
func (s *Server) completeLogin(w http.ResponseWriter, r *http.Request, id, checkedHash, code string) bool {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return false
	}
	defer tx.Rollback()
	var currentHash string
	var mfa bool
	err = tx.QueryRow(`SELECT password_hash,EXISTS(SELECT 1 FROM user_mfa WHERE user_id=users.id AND secret!='') FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0`, id).Scan(&currentHash, &mfa)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && currentHash != checkedHash) {
		problem(w, 401, "Account access changed. Sign in again.")
		return false
	}
	if err != nil {
		s.failed(w, err)
		return false
	}
	if mfa {
		if code == "" {
			respond(w, 200, map[string]bool{"second_factor_required": true})
			return false
		}
		if !s.limiter.allow("mfa:"+id, 10, 5*time.Minute) {
			problem(w, 429, "Too many verification attempts. Try again in five minutes.")
			return false
		}
		valid, err := consumeFactor(tx, id, code)
		if err != nil {
			s.failed(w, err)
			return false
		}
		if !valid {
			problem(w, 401, "The code is incorrect or has already been used.")
			return false
		}
	}
	token, err := randomToken()
	if err != nil {
		s.failed(w, err)
		return false
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	now := db.Now()
	// A completed sign-in counts as a recent strong authentication, so the account
	// can use privileged actions without an immediate extra prompt.
	if _, err = tx.Exec(`INSERT INTO sessions(id,user_id,token_hash,created_at,expires_at,last_seen_at,user_agent,privileged_at) VALUES(?,?,?,?,?,?,?,?)`,
		db.ID(), id, hashToken(token), now, db.Timestamp(expires), now, db.SanitizeUserAgent(r.UserAgent()), now); err != nil {
		s.failed(w, err)
		return false
	}
	if err = tx.Commit(); err != nil {
		s.failed(w, err)
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", Expires: expires, MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, Secure: s.config.SecureCookie(), SameSite: http.SameSiteStrictMode})
	return true
}
