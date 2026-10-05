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
	// alertResolved is the notice that a condition which was raised is no longer true.
	// It exists because an operator who was told about a failure is owed the news that
	// it cleared: without it the last notice in the history is always the alarm, and the
	// only way to learn a backup caught up is to go and look at the status.
	alertResolved = "resolved"
)

// resolvedKind names a recovery notice for one condition, so a receiver can filter on
// the prefix and two different conditions recovering in the same window are two notices
// rather than one suppressed by the other's deduplication.
func resolvedKind(kind string) string { return alertResolved + ":" + kind }

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

const maxAlertHistory = 100

// alertRepeat is how long a raised condition is considered the same one. Long enough
// that a transient condition does not produce a burst, short enough that a condition
// which recovers and returns is reported again. It deduplicates *conditions*; it is not
// the delivery policy, and a failed delivery is retried on its own schedule rather than
// being silenced for six hours.
const alertRepeat = 6 * time.Hour

// The delivery and evaluation policy.
//
//   - Evaluation runs on a bounded cadence of its own rather than only on the hourly
//     maintenance pass, and a job or copy failure prompts an immediate evaluation, so a
//     condition cannot wait up to an hour to be noticed.
//   - Delivery is a bounded queue drained by one worker: a webhook that takes twenty
//     seconds to time out must not hold the pass that raised it, and must not spawn a
//     goroutine per alert.
//   - A failed delivery is retried a bounded number of times within the alert window,
//     because a transient refusal is not the same thing as a condition that has stopped.
const (
	alertEvaluationInterval = 5 * time.Minute
	alertQueueDepth         = 16
	alertDeliveryAttempts   = 4
	alertDeliveryBackoff    = 30 * time.Second
)

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
	// queue carries records to the delivery worker. It is bounded, and a full queue
	// drops the *delivery*, not the record: the condition stays in the history with its
	// failure visible, which is the honest outcome when alerting itself is overloaded.
	queue chan alertRecord
}

func (s *Server) alerts() *alertState {
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	if s.alertState == nil {
		s.alertState = &alertState{}
	}
	s.alertState.once.Do(func() {
		s.alertState.lastRaised = map[string]string{}
		s.alertState.queue = make(chan alertRecord, alertQueueDepth)
	})
	return s.alertState
}

// alertWorker evaluates the health conditions on a bounded cadence and on demand, and
// hands each new condition to the delivery worker. Both stages are outside the
// file-maintenance lock: a network call must never hold the pass that sweeps files, and a
// slow receiver must never delay a backup.
func (s *Server) alertWorker() {
	defer s.workers.Done()
	ticker := time.NewTicker(alertEvaluationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.raiseCurrentAlerts(time.Now())
		case <-s.alertPrompt:
			// A job or copy failure is a condition with a known moment. Evaluating now
			// rather than at the next tick is the difference between noticing a failed
			// backup in seconds and noticing it up to a cadence later.
			s.raiseCurrentAlerts(time.Now())
		}
	}
}

// alertDeliveryWorker drains the bounded queue. One worker is enough for a condition
// stream this small, and it is the reason a delivery cannot grow goroutines without
// bound.
func (s *Server) alertDeliveryWorker() {
	defer s.workers.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case record := <-s.alerts().queue:
			s.deliverWithRetries(record)
		}
	}
}

// noteAlertFailure records a delivery that could not be attempted because the queue was
// full, so the operator can see alerting itself was overloaded rather than believing the
// notice was delivered.
func (s *Server) noteAlertFailure(record alertRecord, reason string) {
	state := s.alerts()
	state.mu.Lock()
	defer state.mu.Unlock()
	for index := range state.history {
		if state.history[index].At == record.At && state.history[index].Kind == record.Kind {
			state.history[index].LastError = reason
			break
		}
	}
}

// deliverWithRetries attempts one record's delivery a bounded number of times. Each
// attempt is recorded, so the operator can tell "refused once" from "refused four times
// over three minutes", and a final failure leaves its reason on the record.
func (s *Server) deliverWithRetries(record alertRecord) {
	attempts := s.alertDeliveryAttempts
	if attempts < 1 {
		attempts = 1
	}
	backoff := s.alertDeliveryBackoff
	delivered := ""
	lastError := ""
	total := 0
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-s.ctx.Done():
			case <-time.After(backoff * time.Duration(1<<(attempt-1))):
			}
		}
		endpoint, secret := s.alertTarget()
		if endpoint == "" {
			// The destination was removed while the record waited. Nothing to attempt.
			return
		}
		when, tried, err := s.deliverAlert(endpoint, secret, record)
		total += tried
		if err == "" {
			delivered = when
			lastError = ""
			break
		}
		lastError = err
	}
	state := s.alerts()
	state.mu.Lock()
	for index := range state.history {
		if state.history[index].At == record.At && state.history[index].Kind == record.Kind {
			state.history[index].Delivered = delivered
			state.history[index].Attempts = total
			state.history[index].LastError = lastError
			if delivered != "" {
				state.delivered = delivered
			}
			break
		}
	}
	state.mu.Unlock()
	if lastError != "" {
		s.log.Warn("operator alert was not delivered", "kind", record.Kind, "attempts", total, "error", lastError)
	}
}

// raise records an alert and queues its delivery.
//
// The bookkeeping happens under the lock; the network call does not happen here at all.
// A webhook that takes twenty seconds to time out must not hold the maintenance pass that
// raised it, and must not hold the next one either.
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

	if endpoint, _ := s.alertTarget(); endpoint == "" {
		// No destination is a configuration state, not a failure, and it is reported in
		// the status rather than here. A "delivery failed" alert for an alert nobody
		// configured is noise.
		return
	}
	select {
	case state.queue <- record:
	default:
		// A full queue means deliveries are slower than conditions are arriving. The
		// record stays in the history with the overload visible; dropping it silently
		// would make a configured alert look delivered.
		s.noteAlertFailure(record, "the alert delivery queue was full; the notice was not attempted")
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
		Conditions: s.alertConditions(time.Now()),
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
// without a destination, and is the same model the production check uses. One
// implementation, so the panel, the alerts and the command-line check cannot disagree
// about what is wrong.
//
// Freshness is judged from the newest *independently recoverable* snapshot — the
// generation with a verified copy, or the newest one that is still recoverable when the
// latest attempt has no copy — and never from the newest local job's completion time. A
// recent local backup that has never been copied is not a recovery point, and a delayed
// copy of an old archive does not make the old data fresh.
func (s *Server) alertConditions(now time.Time) []alertCondition {
	settings := s.Settings()
	backup := s.backupStatus()
	conditions := []alertCondition{}

	interval := time.Duration(max(1, settings.BackupHours)) * time.Hour
	warning, critical := app.RecoveryThresholds(interval)
	// The newest generation has a verified independent copy only when the recorded copy
	// is the recorded generation: the same name and digest, not merely a copy that
	// exists. A copy of a different, valid archive is not a recovery of this generation.
	recordedCopy := backup.CopyName != "" && backup.CopyDigest != "" && backup.LastCopy != ""
	newestCopied := recordedCopy && backup.CopyDigest == backup.Digest
	state := app.RecoveryState{
		Enabled:              settings.BackupEnabled,
		NewestSnapshotAt:     backup.SnapshotAt,
		NewestUploads:        backup.IncludesUploads,
		NewestCopied:         newestCopied,
		FallbackIdentifiable: backup.RecoverableID != "" && backup.RecoverableCopyName != "" && backup.RecoverableCopyDigest != "",
		FallbackSnapshotAt:   backup.RecoverableSnapshotAt,
		FallbackUploads:      backup.RecoverableIncludesUploads,
		JobError:             backup.SnapshotError,
		CopyError:            backup.CopyError,
	}
	if state.NewestCopied {
		state.Identity = backup.Name
	} else {
		state.Identity = backup.RecoverableID
	}
	verdict := app.AssessRecovery(state, now, warning, critical)
	if verdict.Severity != "ok" {
		kind := alertBackupStale
		if state.JobError != "" {
			kind = alertBackupFailed
		} else if state.CopyError != "" {
			kind = alertCopyStale
		}
		conditions = append(conditions, alertCondition{
			Kind: kind, Severity: verdict.Severity, Active: true, Summary: verdict.Summary,
		})
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

// raiseCurrentAlerts converts the conditions into alerts. It runs from the alert
// worker's own bounded cadence and from a prompt trigger when a job or copy fails, not
// from the file-maintenance pass: a condition must not wait for the hourly ticker, and
// the pass holds the file lock while it sweeps.
func (s *Server) raiseCurrentAlerts(now time.Time) {
	active := map[string]bool{}
	for _, condition := range s.alertConditions(now) {
		if condition.Active {
			active[condition.Kind] = true
			s.raise(condition.Kind, condition.Severity, condition.Summary)
		}
	}
	s.resolveClearedAlerts(active)
}

// resolveClearedAlerts raises a recovery notice for each condition that was raised and is
// no longer true, and forgets it so a recurrence alerts again. A condition that has never
// been raised produces nothing: "healthy" is the status surface's answer, not an alert.
func (s *Server) resolveClearedAlerts(active map[string]bool) {
	state := s.alerts()
	state.mu.Lock()
	cleared := []string{}
	for kind := range state.lastRaised {
		if strings.HasPrefix(kind, alertResolved+":") {
			continue
		}
		if !active[kind] {
			cleared = append(cleared, kind)
		}
	}
	for _, kind := range cleared {
		delete(state.lastRaised, kind)
	}
	state.mu.Unlock()
	for _, kind := range cleared {
		s.raise(resolvedKind(kind), "info", fmt.Sprintf("The %s condition is no longer active.", kind))
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
