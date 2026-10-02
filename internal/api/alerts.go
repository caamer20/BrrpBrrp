package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

// Operator alerts (WP-28, M5).
//
// The requirement is that alerts *reach an approved recipient*, and the sentence that
// matters is "a configured alert is not a delivered alert". So this is built around the
// opposite question: not "is a URL configured" but "did a delivery succeed, and when
// was the last one that did".
//
// That means every attempt is recorded, successes and failures alike, and a recipient
// can see the difference. An operator whose webhook has been failing for a week must
// be able to find that out from the product rather than from the absence of news —
// which is the failure mode of alerting in general.

// alertKinds are the conditions the product raises. They are named for what the
// operator has to do, not for what the server measured, because a message nobody can
// act on is a message that gets filtered.
const (
	// alertBackupStale means the last verified backup is older than the recovery point
	// targets allow. This is the RPO failing quietly, and it is the alert that matters
	// most: a server that is running fine and cannot be recovered is the worst state.
	alertBackupStale = "backup_stale"
	// alertBackupFailed means an attempt failed. Distinct from staleness because the
	// remedy differs: a repeated failure is a broken destination, not an old archive.
	alertBackupFailed = "backup_failed"
	// alertStorageLow means the data volume is running out. Reported as a condition
	// rather than only when it is critical, because the interesting moment is before
	// the failure.
	alertStorageLow = "storage_low"
	// alertCopyStale means the independent copy is behind, even where local backups are
	// current. A backup on the same host is not a backup, and this is the alert that
	// says so.
	alertCopyStale = "copy_stale"
)

type alertRecord struct {
	// At is when the condition was raised. Alerts are not a queue to be drained; each
	// one is a statement about a moment, and the history is how an operator judges
	// whether a problem is ongoing or was a moment.
	At string `json:"at"`
	// Kind is one of the constants above.
	Kind string `json:"kind"`
	// Severity is "critical" for the conditions that mean recovery is not available and
	// "warning" for the rest. A critical alert that is merely a warning trains people to
	// ignore it.
	Severity string `json:"severity"`
	// Summary is one sentence with the numbers in it. An alert that says "backup stale"
	// without saying how stale forces the recipient to go and look, which they will do
	// once.
	Summary string `json:"summary"`
	// Delivered records whether an attempt succeeded, and when. It is empty when the
	// alert has not been attempted, and it is the field the operator reads.
	Delivered string `json:"delivered"`
	// Attempts counts delivery attempts, successful or not.
	Attempts int `json:"attempts"`
	// LastError is the last failure. Kept because "the webhook returned 403" and "the
	// webhook timed out" call for different responses.
	LastError string `json:"last_error"`
}

// alertState is the raised-alert history. It is bounded: an unbounded alert log on a
// server with a broken webhook would grow on its own, which is the one thing an
// alerting system must not do.
type alertState struct {
	mu sync.Mutex
	// once guards the map's initialisation, because the health check and the delivery
	// attempt can both be the first caller.
	once sync.Once
	// history is newest-first and bounded.
	history []alertRecord
	// lastRaised keys a condition to its last alert, so a condition that persists does
	// not produce a new alert every pass. Repeated notices for an unchanged condition
	// are how an operator learns to ignore them.
	lastRaised map[string]string
	// delivered records the last *successful* delivery of anything, which is what the
	// status surface answers "are my alerts working" with.
	delivered string
}

const maxAlertHistory = 100

// alertRepeat is how long a raised condition is considered the same one. Long enough
// that a transient condition does not produce a burst, short enough that a condition
// which recovers and returns is reported again.
const alertRepeat = 6 * time.Hour

func (s *Server) alerts() *alertState {
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	if s.alertState == nil {
		s.alertState = &alertState{}
	}
	s.alertState.once.Do(func() { s.alertState.lastRaised = map[string]string{} })
	return s.alertState
}

// raise records an alert and attempts delivery outside the lock.
//
// The lock is held only for the bookkeeping. A webhook that takes thirty seconds to
// time out must not hold the maintenance pass that raised it, and must not hold the
// next one either.
func (s *Server) raise(kind, severity, summary string) {
	state := s.alerts()
	state.mu.Lock()
	if last, seen := state.lastRaised[kind]; seen {
		if when, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(when) < alertRepeat {
			// The same condition, still true. Recording it again would fill the history
			// with one fact and train the recipient to stop reading.
			state.mu.Unlock()
			return
		}
	}
	now := db.Now()
	record := alertRecord{At: now, Kind: kind, Severity: severity, Summary: summary}
	state.history = append([]alertRecord{record}, state.history...)
	if len(state.history) > maxAlertHistory {
		state.history = state.history[:maxAlertHistory]
	}
	state.lastRaised[kind] = now
	state.mu.Unlock()

	endpoint, secret := s.alertTarget()
	if endpoint == "" {
		// No destination is a configuration state, not a failure, and it is reported in
		// the status rather than here. A "delivery failed" alert for an alert nobody
		// configured is noise.
		return
	}
	delivered, attempts, lastError := s.deliverAlert(endpoint, secret, record)
	state.mu.Lock()
	for index := range state.history {
		if state.history[index].At == record.At && state.history[index].Kind == record.Kind {
			state.history[index].Delivered = delivered
			state.history[index].Attempts = attempts
			state.history[index].LastError = lastError
			if delivered != "" {
				state.delivered = delivered
			}
			break
		}
	}
	state.mu.Unlock()
	if lastError != "" {
		s.log.Warn("operator alert was not delivered", "kind", kind, "error", lastError)
	}
}

// alertTarget reads the delivery destination.
//
// The secret is optional and is read separately from the URL, so a URL can be
// configured without a secret and a secret can be rotated by changing the environment
// rather than by rewriting configuration that also carries the destination.
func (s *Server) alertTarget() (endpoint, secret string) {
	endpoint = strings.TrimSpace(os.Getenv("BRRP_ALERT_WEBHOOK"))
	secret = os.Getenv("BRRP_ALERT_SECRET")
	if endpoint == "" {
		return "", ""
	}
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		// A destination that is not an http(s) URL cannot be delivered to, and silently
		// accepting it would produce a permanently failing alert nobody can diagnose.
		return "", ""
	}
	return endpoint, secret
}

// deliverAlert attempts one delivery and reports what happened. It is the whole
// delivery mechanism on purpose: a queue would mean an operator had to know about a
// second thing to find out that alerting itself was broken.
func (s *Server) deliverAlert(endpoint, secret string, record alertRecord) (delivered string, attempts int, lastError string) {
	delivered = db.Now()
	payload, err := json.Marshal(record)
	if err != nil {
		return "", 0, "the alert could not be encoded"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", 0, "the alert destination could not be addressed"
	}
	request.Header.Set("Content-Type", "application/json")
	// The kind travels as a header as well as in the body, so a receiver can route
	// without parsing, and a misconfigured receiver is obvious in its logs.
	request.Header.Set("X-Brrpbrrp-Alert", record.Kind)
	request.Header.Set("X-Brrpbrrp-Severity", record.Severity)
	if secret != "" {
		// A shared secret over the body, so a receiver can tell a real alert from a
		// forged one posted to a URL that leaked. The timestamp is inside the signed
		// bytes, so a captured request cannot be replayed later.
		mac := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(mac, "%s.", record.At)
		mac.Write(payload)
		request.Header.Set("X-Brrpbrrp-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", 1, "the alert destination could not be reached"
	}
	defer response.Body.Close()
	// The body is drained so the connection is reusable; a recipient that returns a
	// large error page should not cost a connection.
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 1, "the alert destination answered " + strconv.Itoa(response.StatusCode)
	}
	return delivered, 1, ""
}

// alertStatus is what the operator surface answers, and the question it answers is
// "are my alerts working" rather than "are my alerts configured".
type alertStatus struct {
	// Configured is whether a destination is set. It is not a health answer.
	Configured bool `json:"configured"`
	// Delivered is when an alert last reached the destination. It is the field to read:
	// a configured destination that has never delivered is worse than no destination,
	// because it looks like coverage.
	Delivered string `json:"delivered"`
	// Destination is the host, not the full URL: the path may carry a token.
	Destination string           `json:"destination"`
	History     []alertRecord    `json:"history"`
	Conditions  []alertCondition `json:"conditions"`
}

// alertCondition is a *current* health reading, separate from the history. A receiver
// asking "is this server healthy right now" wants the answer; the history is for "was
// this healthy last Tuesday".
type alertCondition struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Active   bool   `json:"active"`
	Summary  string `json:"summary"`
}

func (s *Server) alertStatusReport() alertStatus {
	state := s.alerts()
	state.mu.Lock()
	history := make([]alertRecord, len(state.history))
	copy(history, state.history)
	delivered := state.delivered
	state.mu.Unlock()

	endpoint, _ := s.alertTarget()
	status := alertStatus{
		Configured: endpoint != "",
		Delivered:  delivered,
		History:    history,
		Conditions: s.alertConditions(),
	}
	if endpoint != "" {
		// The host only. A webhook URL commonly carries a token in its path, and an
		// operator panel is somewhere that ends up in screenshots.
		if index := strings.Index(endpoint, "://"); index >= 0 {
			rest := endpoint[index+3:]
			if slash := strings.Index(rest, "/"); slash >= 0 {
				status.Destination = rest[:slash]
			} else {
				status.Destination = rest
			}
		}
	}
	return status
}

// alertConditions reads the current health of the things the product can check
// without a destination, and is the same logic the maintenance pass uses. One
// implementation, so the panel and the alerts cannot disagree about what is wrong.
func (s *Server) alertConditions() []alertCondition {
	now := time.Now()
	settings := s.Settings()
	conditions := []alertCondition{}

	// The recovery point the operator configured, compared with the last verified
	// backup. A backup that is one interval old is not a stale backup; one that is
	// three is.
	backup := s.backupStatus()
	interval := settings.BackupHours
	if interval < 1 {
		interval = 24
	}
	staleness := ""
	if backup.LastSuccess != "" {
		if when, err := time.Parse(time.RFC3339Nano, backup.LastSuccess); err == nil {
			staleness = now.Sub(when).Truncate(time.Minute).String()
			age := now.Sub(when)
			if age > time.Duration(interval*3)*time.Hour {
				conditions = append(conditions, alertCondition{
					Kind: alertBackupStale, Severity: "critical", Active: true,
					Summary: fmt.Sprintf(
						"Last verified backup is %s old and the interval is %d hours, so the %dh recovery point is at risk.",
						staleness, interval, interval*24),
				})
			}
		}
	} else if settings.BackupEnabled {
		conditions = append(conditions, alertCondition{
			Kind: alertBackupStale, Severity: "critical", Active: true,
			Summary: "Backups are enabled but no verified backup has been recorded yet.",
		})
	}
	if backup.Attempts > 0 {
		conditions = append(conditions, alertCondition{
			Kind: alertBackupFailed, Severity: "critical", Active: true,
			Summary: fmt.Sprintf("The last %d backup attempts failed; there is no fresh verified archive.", backup.Attempts),
		})
	}

	// The independent copy, which is what makes a backup a backup. A copy on the same
	// host is not one, and an operator who believes otherwise is worse off than one who
	// knows they have none.
	if settings.BackupEnabled && backup.LastCopy == "" && (backup.LastCopyAttempt != "" || backup.CopyAttempts > 0) {
		conditions = append(conditions, alertCondition{
			Kind: alertCopyStale, Severity: "critical", Active: true,
			Summary: "An independent copy destination is configured but no copy has completed.",
		})
	} else if settings.BackupEnabled && backup.LastCopy != "" && backup.CopyAttempts > 0 {
		if success, err := time.Parse(time.RFC3339Nano, backup.LastCopy); err == nil {
			conditions = append(conditions, alertCondition{
				Kind: alertCopyStale, Severity: "critical", Active: true,
				Summary: "The most recent copy attempt failed; the last good copy is " +
					now.Sub(success).Truncate(time.Minute).String() + " old.",
			})
		}
	}

	if free, err := app.FreeDiskBytes(s.config.DataDir); err == nil {
		const lowThreshold = uint64(5) << 30
		if free < lowThreshold {
			conditions = append(conditions, alertCondition{
				Kind: alertStorageLow, Severity: "critical", Active: true,
				Summary: fmt.Sprintf("Only %s remains on the data volume.", humanBytes(free)),
			})
		} else if free < uint64(20)<<30 {
			conditions = append(conditions, alertCondition{
				Kind: alertStorageLow, Severity: "warning", Active: true,
				Summary: fmt.Sprintf("%s remains on the data volume.", humanBytes(free)),
			})
		}
	}
	return conditions
}

// raiseCurrentAlerts converts the conditions into alerts. It is called from the
// maintenance pass rather than from a timer of its own, so the readings and the
// notifications come from one pass and cannot describe different moments.
func (s *Server) raiseCurrentAlerts() {
	for _, condition := range s.alertConditions() {
		if condition.Active {
			s.raise(condition.Kind, condition.Severity, condition.Summary)
		}
	}
}

func humanBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor, exponent := uint64(unit), 0
	for size := value / uint64(unit); size >= uint64(unit); size /= uint64(unit) {
		divisor *= uint64(unit)
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTPE"[exponent])
}

// alertStatusHandler answers "are my alerts working" rather than "are my alerts
// configured". The distinction is the whole point of the feature: a webhook URL that
// has never delivered looks exactly like coverage, and the operator finds out during a
// restore.
func (s *Server) alertStatusHandler(w http.ResponseWriter, r *http.Request, user db.User) {
	respond(w, 200, s.alertStatusReport())
}
