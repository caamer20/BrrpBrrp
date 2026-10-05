package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// The alert path's own properties (F-11): freshness comes from the independently
// recoverable snapshot, delivery does not block the maintenance pass, a failed delivery
// is retried rather than silenced, and a condition with a known moment is evaluated
// promptly instead of waiting for the bounded cadence.

// recoveryStatusFixture writes a backup-status row in which the newest local work is
// minutes old but the only independently recoverable generation is old. It is the shape
// the review found: the alert path aged the recent local job and stayed quiet.
func recoveryStatusFixture(t *testing.T, s *Server, now time.Time, recoverableAge time.Duration) {
	t.Helper()
	stamp := func(at time.Time) string { return db.Timestamp(at) }
	status := s.backupStatus()
	status.Name = "brrpbrrp-local.zip"
	status.Digest = "digest-local"
	status.SnapshotAt = stamp(now.Add(-5 * time.Minute))
	status.LastSuccess = stamp(now.Add(-4 * time.Minute))
	// The newest generation has no verified copy, so it is not a recovery point.
	status.CopyName = ""
	status.CopyDigest = ""
	status.LastCopy = ""
	status.LastCopyAttempt = stamp(now.Add(-2 * time.Hour))
	// What the installation can still recover:
	status.RecoverableID = "gen-7"
	status.RecoverableName = "brrpbrrp-gen-7.zip"
	status.RecoverableCopyName = "copy-gen-7.zip"
	status.RecoverableCopyDigest = "digest-gen-7"
	status.RecoverableCopyAt = stamp(now.Add(-recoverableAge))
	status.RecoverableSnapshotAt = stamp(now.Add(-recoverableAge))
	status.RecoverableIncludesUploads = true
	if err := s.saveBackupStatus(status); err != nil {
		t.Fatal(err)
	}
}

func TestAlertFreshnessComesFromTheRecoverableSnapshot(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enableBackups(t, handler, admin, s)
	s.settings.BackupHours = 12 // thresholds: warning at 18h, critical at 24h

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	recoveryStatusFixture(t, s, now, 30*time.Hour)

	conditions := s.alertConditions(now)
	var stale *alertCondition
	for index := range conditions {
		if conditions[index].Kind == alertBackupStale {
			stale = &conditions[index]
		}
	}
	if stale == nil || !stale.Active {
		t.Fatalf("a thirty-hour-old recovery point with minute-old local work raised no staleness condition: %+v", conditions)
	}
	if stale.Severity != "critical" {
		t.Fatalf("a thirty-hour-old recovery point was reported as %q", stale.Severity)
	}
	// The message must name the generation's own age, not the local job's.
	if !containsReason([]string{stale.Summary}, "independently recoverable") {
		t.Fatalf("the condition does not say what it measured: %s", stale.Summary)
	}
	// The fixture is the exact shape that made the old reading quiet: the newest local
	// job completed minutes ago, and the pre-fix condition aged *that* against three
	// intervals. The pre-fix file cannot be run as an overlay here because the fix also
	// gave this function the clock the test controls (its signature changed), so the
	// proof is the fixture: the reading the old code used is fresh, and the condition is
	// still raised.
	last, err := time.Parse(time.RFC3339Nano, s.backupStatus().LastSuccess)
	if err != nil || now.Sub(last) > time.Hour {
		t.Fatalf("the fixture no longer describes the defect: LastSuccess=%v err=%v", last, err)
	}
}

// TestALateCopyDoesNotRefreshTheCondition is the same model seen through the server: the
// copy completed recently, but the snapshot inside it is old, so the condition stands.
func TestALateCopyDoesNotRefreshTheCondition(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enableBackups(t, handler, admin, s)
	s.settings.BackupHours = 12

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	status := s.backupStatus()
	status.Name = "brrpbrrp-local.zip"
	status.Digest = "digest-local"
	status.SnapshotAt = db.Timestamp(now.Add(-30 * time.Hour))
	status.CopyName = "brrpbrrp-local.zip"
	status.CopyDigest = "digest-local"
	status.LastCopy = db.Timestamp(now.Add(-1 * time.Minute)) // copied just now
	status.LastSuccess = db.Timestamp(now.Add(-30 * time.Hour))
	if err := s.saveBackupStatus(status); err != nil {
		t.Fatal(err)
	}
	conditions := s.alertConditions(now)
	active := false
	for _, condition := range conditions {
		if condition.Kind == alertBackupStale && condition.Active && condition.Severity == "critical" {
			active = true
		}
	}
	if !active {
		t.Fatalf("a fresh copy of a thirty-hour-old snapshot was treated as fresh: %+v", conditions)
	}
}

// TestAFailedDeliveryIsRetriedRatherThanSilenced proves the delivery policy: a transient
// refusal is attempted again inside the alert window, and a later success is recorded.
func TestAFailedDeliveryIsRetriedRatherThanSilenced(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	var attempts atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiver.URL)
	s.alertDeliveryAttempts = 4
	s.alertDeliveryBackoff = 5 * time.Millisecond

	s.raise(alertBackupStale, "critical", "No verified backup has been recorded.")
	waitForAlert(t, "the delivery to succeed after transient refusals", func() bool {
		status := s.alertStatusReport()
		return len(status.History) == 1 && status.History[0].Delivered != ""
	})
	if got := attempts.Load(); got != 3 {
		t.Fatalf("the transient failure took %d attempts to succeed, want 3", got)
	}
	status := readJSON[alertStatus](t, request(t, handler, "GET", "/api/admin/alerts", nil, admin))
	if status.Delivered == "" {
		t.Fatal("the status does not report the successful delivery")
	}
}

// TestAMaintenancePassDoesNotWaitOnASlowReceiver is the lock-ordering property: a
// webhook that hangs must not hold the pass that sweeps files.
func TestAMaintenancePassDoesNotWaitOnASlowReceiver(t *testing.T) {
	s, _ := testServer(t)
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiver.URL)

	s.raise(alertStorageLow, "critical", "Only 1 GiB remains on the data volume.")
	// The delivery worker is now holding a request open. The maintenance pass must not
	// wait for it.
	start := time.Now()
	if err := s.maintain(time.Now()); err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the maintenance pass took %s while a delivery was in flight", elapsed)
	}
	close(release)
}

// TestAJobFailureIsEvaluatedPromptly proves the prompt trigger: a condition with a known
// moment does not wait for the bounded evaluation cadence.
func TestAJobFailureIsEvaluatedPromptly(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL)
	enableBackups(t, handler, admin, s)

	now := time.Now().UTC()
	status := s.backupStatus()
	status.SnapshotError = "the destination refused the connection"
	if err := s.saveBackupStatus(status); err != nil {
		t.Fatal(err)
	}
	// Moved as the scheduler would after a failed attempt.
	s.promptAlertEvaluation()
	waitForAlert(t, "the prompt evaluation to deliver the job-failure notice", func() bool {
		return receiver.count() >= 1
	})
	record, _ := receiver.first()
	if record.Kind != alertBackupFailed {
		t.Fatalf("the prompt evaluation raised %q rather than the job failure", record.Kind)
	}
	if record.Summary == "" {
		t.Fatal("the job-failure notice carries no summary")
	}
	_ = now
}

// TestAConditionAppearingJustAfterAnEvaluationIsCaughtByTheNext pins the worst case for
// the bounded cadence: a recovery point that crosses the threshold immediately after an
// evaluation is detected at the next one, so the detection window is the cadence rather
// than the old hourly ticker. A job or copy failure does not even wait that long — the
// prompt trigger covers it, which TestAJobFailureIsEvaluatedPromptly asserts.
func TestAConditionAppearingJustAfterAnEvaluationIsCaughtByTheNext(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enableBackups(t, handler, admin, s)
	s.settings.BackupHours = 12 // warning 18h, critical 24h

	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	recoveryStatusFixture(t, s, base, 18*time.Hour-time.Minute)

	// At the evaluation the point is inside the warning age: nothing is raised.
	for _, condition := range s.alertConditions(base) {
		if condition.Kind == alertBackupStale && condition.Active {
			t.Fatalf("a recovery point inside the warning age raised a condition: %+v", condition)
		}
	}
	// One cadence later the same point has crossed the warning age, and the next
	// evaluation sees it. No clock, no wall-clock wait: the model is a function of time.
	later := base.Add(alertEvaluationInterval)
	raised := false
	for _, condition := range s.alertConditions(later) {
		if condition.Kind == alertBackupStale && condition.Active && condition.Severity == "warning" {
			raised = true
		}
	}
	if !raised {
		t.Fatal("a recovery point that crossed the warning age was not detected at the next evaluation")
	}
}

// TestARecoveredConditionIsAnnouncedAndCanBeRaisedAgain proves the recovery transition:
// an operator told about a failure is owed the news that it cleared, and the condition
// that cleared is not permanently deduplicated away.
func TestARecoveredConditionIsAnnouncedAndCanBeRaisedAgain(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	receiver := &alertReceiver{}
	receiverServer := httptest.NewServer(receiver)
	defer receiverServer.Close()
	t.Setenv("BRRP_ALERT_WEBHOOK", receiverServer.URL)
	enableBackups(t, handler, admin, s)
	s.settings.BackupHours = 12

	// A recovery point old enough to raise, and a history that says it was raised.
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	recoveryStatusFixture(t, s, now, 30*time.Hour)
	s.raiseCurrentAlerts(now)
	waitForAlert(t, "the staleness notice", func() bool { return receiver.count() >= 1 })

	// The backup catches up: the recorded recovery point is now fresh. The next
	// evaluation announces the recovery.
	status := s.backupStatus()
	status.RecoverableSnapshotAt = db.Timestamp(now.Add(-time.Hour))
	status.RecoverableCopyAt = db.Timestamp(now.Add(-50 * time.Minute))
	status.LastCopy = db.Timestamp(now.Add(-50 * time.Minute))
	if err := s.saveBackupStatus(status); err != nil {
		t.Fatal(err)
	}
	s.raiseCurrentAlerts(now)
	waitForAlert(t, "the recovery notice", func() bool {
		for _, record := range receiver.snapshot() {
			if record.Kind == resolvedKind(alertBackupStale) {
				return true
			}
		}
		return false
	})

	// It staleness recurs, it is raised again rather than suppressed by the earlier
	// six-hour window: clearing a condition resets its deduplication.
	status.RecoverableSnapshotAt = db.Timestamp(now.Add(-40 * time.Hour))
	status.RecoverableCopyAt = db.Timestamp(now.Add(-39 * time.Hour))
	status.LastCopy = db.Timestamp(now.Add(-39 * time.Hour))
	if err := s.saveBackupStatus(status); err != nil {
		t.Fatal(err)
	}
	s.raiseCurrentAlerts(now)
	waitForAlert(t, "the staleness notice to be raised again", func() bool {
		count := 0
		for _, record := range receiver.snapshot() {
			if record.Kind == alertBackupStale {
				count++
			}
		}
		return count >= 2
	})
}
