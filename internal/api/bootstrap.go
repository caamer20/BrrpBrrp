package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"brrpbrrp/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) setupRequired() (bool, error) {
	var count int
	err := s.store.QueryRow(`SELECT (SELECT count(*) FROM users)+(SELECT count(*) FROM settings WHERE key='setup_complete')`).Scan(&count)
	return count == 0, err
}

func (s *Server) prepareSetup() error {
	required, err := s.setupRequired()
	if err != nil || !required {
		return err
	}
	if token, err := os.ReadFile(s.config.SetupTokenFile); err == nil {
		if len(strings.TrimSpace(string(token))) != 43 {
			return errors.New("invalid setup token file; remove it while the server is stopped to regenerate")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.config.SetupTokenFile, []byte(token), 0600); err != nil {
		return err
	}
	s.log.Info("first-time setup is available", "url", s.config.BaseURL+"/setup", "token_file", s.config.SetupTokenFile)
	return nil
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired()
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]bool{"setup_required": required})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	address, open := s.guestOpen(w, r)
	if !open {
		return
	}
	var input struct {
		Token      string `json:"token"`
		ServerName string `json:"server_name"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !usernamePattern.MatchString(input.Username) || strings.EqualFold(input.Username, "everyone") || !validPassword(input.Password) || len(strings.TrimSpace(input.ServerName)) < 1 || len(input.ServerName) > 80 {
		problem(w, 400, "Use a 3–32 character username, a 12–72 byte password and a server name up to 80 characters.")
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	required, err := s.setupRequired()
	if err != nil {
		s.failed(w, err)
		return
	}
	if !required {
		problem(w, 409, "Setup is already complete.")
		return
	}
	token, err := os.ReadFile(s.config.SetupTokenFile)
	if err != nil || subtle.ConstantTimeCompare([]byte(input.Token), token) != 1 {
		s.guestFailed(address)
		problem(w, 403, "The setup token is incorrect.")
		return
	}
	if !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
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
	// Recheck in the write transaction so a concurrent setup cannot win twice.
	var count int
	if err := tx.QueryRow(`SELECT (SELECT count(*) FROM users)+(SELECT count(*) FROM settings WHERE key='setup_complete')`).Scan(&count); err != nil {
		s.failed(w, err)
		return
	}
	if count != 0 {
		problem(w, 409, "Setup is already complete.")
		return
	}
	id, now := db.ID(), db.Now()
	if _, err := tx.Exec(`INSERT INTO users(id,username,display_name,password_hash,is_admin,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, id, strings.ToLower(input.Username), input.Username, string(hash), now, now); err != nil {
		s.failed(w, err)
		return
	}
	// The first account is the administrator role's first holder. Ownership is a
	// separate, explicit ceremony.
	if err := db.AssignAccountRole(r.Context(), tx, id, db.AdministratorRoleID); err != nil {
		s.failed(w, err)
		return
	}
	for _, channel := range []struct{ name, kind, topic string }{{"general", "text", "The place for everything."}, {"Lounge", "voice", "Drop in and say hello."}} {
		if _, err := tx.Exec(`INSERT INTO channels(id,name,type,topic,created_at,updated_at) VALUES(?,?,?,?,?,?)`, db.ID(), channel.name, channel.kind, channel.topic, now, now); err != nil {
			s.failed(w, err)
			return
		}
	}
	// The running server's settings are still the defaults here — nothing can change them
	// before an account exists — and a new installation differs from them in exactly the
	// ways db.NewInstallationSettings says. The row and the in-memory copy are written
	// from the same value below, after the transaction commits, so scheduled backups begin
	// without a restart and cannot begin before setup has finished.
	settings := s.Settings()
	settings.BackupEnabled = db.NewInstallationSettings().BackupEnabled
	settings.ServerName = strings.TrimSpace(input.ServerName)
	settings.MaxUploadBytes = s.config.MaxUploadBytes
	encoded, _ := json.Marshal(settings)
	if _, err := tx.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('setup_complete','true',?),('public',?,?)`, now, string(encoded), now); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsMu.Unlock()
	if err := os.Remove(s.config.SetupTokenFile); err != nil {
		s.log.Warn("setup token cleanup failed", "error", err)
	}
	if err := s.newSessionFor(w, r, id); err != nil {
		s.failed(w, err)
		return
	}
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 201, user)
}
