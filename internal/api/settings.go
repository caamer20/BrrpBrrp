package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request, user db.User) {
	respond(w, 200, s.Settings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Settings db.Settings `json:"settings"`
		Confirm  string      `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	settings := input.Settings
	if settings.BackupHours == 0 {
		settings.BackupHours = 24
	}
	if settings.BackupKeep == 0 {
		settings.BackupKeep = 7
	}
	if settings.BackupHours < 1 || settings.BackupHours > 168 || settings.BackupKeep < 1 || settings.BackupKeep > 30 {
		problem(w, 400, "Keep 1–30 backups and choose an interval of 1–168 hours.")
		return
	}
	if strings.TrimSpace(settings.ServerName) == "" || len(settings.ServerName) > 80 || settings.MaxUploadBytes < 1 || settings.MaxUploadBytes > min(int64(app.MaxUpload), s.config.MaxUploadBytes) || settings.RetentionDays < 0 || settings.RetentionDays > 36500 || settings.VoiceBitrate < 8 || settings.VoiceBitrate > 64 || settings.TURNPort < 1024 || settings.TURNPort > 65535 || (settings.Theme != "light" && settings.Theme != "dark") {
		problem(w, 400, "Some settings are outside the supported range.")
		return
	}
	if settings.TURNTCPFallback && (settings.TURNTLSPort < 0 || settings.TURNTLSPort > 65535) {
		problem(w, 400, "The TURN TCP fallback port must be 0 (choose one automatically) or between 1024 and 65535.")
		return
	}
	if settings.TURNTLSPort != 0 && (settings.TURNTLSPort < 1024 || settings.TURNTLSPort > 65535) {
		problem(w, 400, "The TURN TCP fallback port must be between 1024 and 65535.")
		return
	}
	if settings.TURNTCPFallback && settings.TURNTLSPort != 0 && settings.TURNTLSPort == settings.TURNPort {
		problem(w, 400, "The TURN TCP fallback needs a different port from the UDP one.")
		return
	}
	if len(settings.TURNAdvertisedHost) > 253 {
		problem(w, 400, "The TURN advertised host is too long.")
		return
	}
	// A STUN URL is validated at the point an operator saves it, not discovered
	// broken later. ICE reports an unusable server as a network fault, so a typo here
	// would present as "the call sometimes fails" rather than "the setting is wrong" —
	// the worst possible shape for a setting an operator is told to be optional.
	settings.STUNServer = strings.TrimSpace(settings.STUNServer)
	if stun := settings.STUNServer; stun != "" {
		// `stun:host:port` is an *opaque* URL: everything after the scheme is the
		// address, so there is no authority to read a host out of. Validating it the
		// obvious way — by looking for a Host — rejects every correct value and
		// accepts "stun.example.com:3478", which parses with the hostname as a scheme.
		// What distinguishes a real one is the scheme, a non-empty remainder, and a
		// trailing port.
		parsed, err := url.Parse(stun)
		address := parsed.Opaque
		if parsed.Host != "" {
			address = parsed.Host
		}
		if err != nil || !strings.EqualFold(parsed.Scheme, "stun") || !validSTUNAddress(address) {
			problem(w, 400, "The STUN server must be a stun: URL with a host and port, for example stun:stun.example.com:3478. Leave it empty to offer no STUN server.")
			return
		}
	}
	// Forcing the relay while none is configured would advertise a constraint the
	// server cannot satisfy. It is refused here so the operator learns at the point of
	// the change, rather than discovering at the next call that the setting did nothing.
	if settings.ForceRelay && !settings.TURN {
		problem(w, 400, "Forcing the relay needs a TURN server to relay through. Configure TURN first, or turn the requirement off.")
		return
	}
	if settings.TURN && net.ParseIP(settings.TURNExternalIP) == nil {
		problem(w, 400, "TURN needs the server's reachable IPv4 address.")
		return
	}
	if settings.Purge && settings.RetentionDays < 1 {
		problem(w, 400, "Choose at least one retention day before enabling automatic deletion.")
		return
	}
	// The audit log is mandatory. A community whose administrators can silence
	// the record of their own authority, credential and access changes cannot be
	// reviewed after an incident, so the setting cannot be turned off at all.
	if !settings.Audit {
		problem(w, 400, "The audit log cannot be turned off. It records authority, credential and access changes.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Authority is re-established inside the transaction, and it is established
	// before the running configuration is touched at all.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.SettingsManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	s.settingsUpdateMu.Lock()
	defer s.settingsUpdateMu.Unlock()
	previous := s.Settings()
	// Security policy is not routine settings. Switching the privileged
	// second-factor requirement off weakens every operator action that depends on
	// it, so it needs owner authority and a recent strong sign-in rather than the
	// ordinary settings.manage capability.
	if settings.RequirePrivilegedMFA != previous.RequirePrivilegedMFA {
		if !principal.IsOwner {
			problem(w, http.StatusForbidden, "Only the owner can change the second-factor policy for privileged accounts.")
			return
		}
		// The freshness and enrolled-factor checks run through this transaction, not
		// through the store. The store holds a single connection, which this
		// transaction already owns and cannot release until it commits, so asking
		// the store here waited on the caller's own connection and the change could
		// not complete at all.
		if err := s.requirePrivilegedSecurityPolicyInTx(r.Context(), tx, user.ID, principal.IsOwner || principal.IsAdministrator()); err != nil {
			var refused problemError
			if errors.As(err, &refused) {
				problem(w, refused.Status, refused.Message)
			} else {
				s.authorityFailed(w, err)
			}
			return
		}
	}
	if settings.Purge && (!previous.Purge || settings.RetentionDays < previous.RetentionDays) && input.Confirm != previous.ServerName {
		problem(w, 400, "Confirm the server name before enabling or shortening automatic retention.")
		return
	}
	if err := s.configureTURN(settings); err != nil {
		problem(w, 400, err.Error())
		return
	}
	encoded, _ := json.Marshal(settings)
	if _, err := tx.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('public',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, string(encoded), db.Now()); err != nil {
		_ = s.configureTURN(previous)
		s.failed(w, err)
		return
	}
	action := "settings.updated"
	if settings.RequirePrivilegedMFA != previous.RequirePrivilegedMFA {
		action = "settings.security_policy_changed"
	}
	if err := s.auditTx(r.Context(), tx, user.ID, action, ""); err != nil {
		_ = s.configureTURN(previous)
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		_ = s.configureTURN(previous)
		s.failed(w, err)
		return
	}
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsMu.Unlock()
	if !settings.Voice {
		s.voice.Close()
	}
	s.hub.Broadcast("settings.updated", settings)
	respond(w, 200, settings)
}

// validSTUNAddress reports whether an opaque STUN URL body is a host and a port.
// Both are required: a host with no port leaves the browser to guess, and a bare
// port is not an address.
func validSTUNAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}
