package app

import (
	"strings"
	"testing"
	"time"
)

// The recovery-health model is one answer to "what can this installation still recover,
// and is it fresh", used by the production check and by the server's alerts. These tests
// pin the properties the two readers must agree on: the recovery point is the newest
// *independently recoverable* snapshot, an unknown time is never green, a late copy does
// not reset the snapshot's age, and the thresholds are the ratified ones.

func recoveryState(snapshotAt string, copied bool) RecoveryState {
	return RecoveryState{Enabled: true, NewestSnapshotAt: snapshotAt, NewestCopied: copied, NewestUploads: true}
}

func TestRecoveryThresholdsReproduceTheRatifiedCadence(t *testing.T) {
	warning, critical := RecoveryThresholds(12 * time.Hour)
	if warning != 18*time.Hour || critical != 24*time.Hour {
		t.Fatalf("a twelve-hour cadence warns at %s and is critical at %s; the plan's numbers are 18h and 24h", warning, critical)
	}
	// The factors scale with an operator's own cadence, so a 24-hour installation is not
	// warned six hours before its next backup is due.
	if warning, critical := RecoveryThresholds(24 * time.Hour); warning != 36*time.Hour || critical != 48*time.Hour {
		t.Fatalf("a twenty-four hour cadence warned at %s and was critical at %s", warning, critical)
	}
	if warning, _ := RecoveryThresholds(0); warning != 18*time.Hour {
		t.Fatalf("an unrecorded cadence assumed %s rather than the recommended twelve hours", warning)
	}
}

func TestRecoveryFreshnessBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	stamp := func(age time.Duration) string { return now.Add(-age).Format(time.RFC3339Nano) }

	for _, testCase := range []struct {
		name     string
		age      time.Duration
		severity string
	}{
		{"fresh", 11 * time.Hour, "ok"},
		{"exactly the warning age is still ok", 18 * time.Hour, "ok"},
		{"just past the warning", 18*time.Hour + time.Second, "warning"},
		{"exactly the critical age is still a warning", 24 * time.Hour, "warning"},
		{"just past the critical", 24*time.Hour + time.Second, "critical"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verdict := AssessRecovery(recoveryState(stamp(testCase.age), true), now, warning, critical)
			if verdict.Severity != testCase.severity {
				t.Fatalf("an age of %s was %q, want %q: %s", testCase.age, verdict.Severity, testCase.severity, verdict.Summary)
			}
		})
	}
}

func TestARecentLocalJobDoesNotConcealAnOldRecoverableSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	state := RecoveryState{
		Enabled: true,
		// The newest local generation is minutes old — but it has never been copied, so
		// it is not a recovery point. What the installation can actually recover is the
		// fallback, and that is over a day old.
		NewestSnapshotAt:     now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
		NewestCopied:         false,
		FallbackIdentifiable: true,
		FallbackSnapshotAt:   now.Add(-30 * time.Hour).Format(time.RFC3339Nano),
		Identity:             "gen-7",
	}
	warning, critical := RecoveryThresholds(12 * time.Hour)
	verdict := AssessRecovery(state, now, warning, critical)
	if verdict.Severity != "critical" {
		t.Fatalf("a recent local job concealed an old recoverable snapshot: %q — %s", verdict.Severity, verdict.Summary)
	}
	if !strings.Contains(verdict.Summary, "gen-7") {
		t.Fatalf("the alert does not name the generation it is about: %s", verdict.Summary)
	}
}

func TestALateCopyDoesNotMakeOldDataFresh(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	// The newest generation has a verified copy, and the copy completed moments ago —
	// but the snapshot inside it was taken thirty hours ago. Completion time is not
	// capture time, and the alert must age the data, not the job.
	state := recoveryState(now.Add(-30*time.Hour).Format(time.RFC3339Nano), true)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	verdict := AssessRecovery(state, now, warning, critical)
	if verdict.Severity != "critical" {
		t.Fatalf("a fresh copy of an old snapshot was reported as %q: %s", verdict.Severity, verdict.Summary)
	}
}

func TestAnUnknownSnapshotIsNeverFresh(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	for _, stamp := range []string{"", "not-a-timestamp"} {
		verdict := AssessRecovery(recoveryState(stamp, true), now, warning, critical)
		if verdict.Severity != "critical" {
			t.Fatalf("a snapshot time of %q was reported as %q", stamp, verdict.Severity)
		}
		if verdict.Known {
			t.Fatalf("a snapshot time of %q was treated as a known recovery point", stamp)
		}
	}
}

func TestAFutureSnapshotIsAClockDefect(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	verdict := AssessRecovery(recoveryState(now.Add(time.Hour).Format(time.RFC3339Nano), true), now, warning, critical)
	if verdict.Severity != "critical" || !strings.Contains(verdict.Summary, "future") {
		t.Fatalf("a future snapshot was reported as %q: %s", verdict.Severity, verdict.Summary)
	}
}

func TestNoVerifiedCopyIsNotRecovery(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	// Plenty of recent local work, no copy anywhere: a backup on this host is not
	// recovery, so this is critical regardless of how fresh the local job is.
	state := RecoveryState{Enabled: true, NewestSnapshotAt: now.Format(time.RFC3339Nano), NewestCopied: false}
	verdict := AssessRecovery(state, now, warning, critical)
	if verdict.Severity != "critical" {
		t.Fatalf("a local-only backup was reported as %q", verdict.Severity)
	}
}

func TestAJobFailureIsReportedPromptly(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	state := recoveryState(now.Add(-time.Hour).Format(time.RFC3339Nano), true)
	state.JobError = "the destination refused the connection"
	verdict := AssessRecovery(state, now, warning, critical)
	if verdict.Severity != "critical" || !strings.Contains(verdict.Summary, "refused") {
		t.Fatalf("a failed job was reported as %q: %s", verdict.Severity, verdict.Summary)
	}
	if verdict.Known {
		t.Fatal("a failed job was reported as a known snapshot age rather than as a job failure")
	}
}

func TestDisabledBackupsDoNotAlert(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	warning, critical := RecoveryThresholds(12 * time.Hour)
	verdict := AssessRecovery(RecoveryState{Enabled: false}, now, warning, critical)
	if verdict.Severity != "ok" {
		t.Fatalf("backups disabled was reported as %q", verdict.Severity)
	}
}
