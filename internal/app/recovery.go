package app

import (
	"fmt"
	"time"
)

// The recovery-health model: one answer to "what can this installation still recover,
// and is that recovery point fresh", used by the production check an operator runs and
// by the alerts and status the running server raises. It exists because those two used
// to disagree: the production check aged the *recoverable* snapshot, while the alert path
// aged the newest local job's completion time, so a recent backup attempt concealed an
// old independently recoverable snapshot and the alerting was quiet at exactly the moment
// the RPO had failed.

// snapshotClockTolerance is how far in the future a recorded time may be before it is
// treated as a broken clock rather than a real reading. Host clocks drift; a stamp a few
// minutes ahead is noise, a stamp tomorrow is a defect.
const snapshotClockTolerance = 5 * time.Minute

// SnapshotAge classifies one recorded snapshot time. It is deliberately small and
// shared: every freshness decision in the product — the production check's local and
// copy checks, the fallback's own age, and the alert conditions — goes through it, so
// "missing or malformed stays unknown" and "the future is invalid" cannot be decided
// differently in two places.
type SnapshotAge struct {
	// Known is whether the recorded time was present and parseable.
	Known bool
	// At is the parsed time when Known.
	At time.Time
	// Age is now-At when Known.
	Age time.Duration
	// Future is whether the time is ahead of now beyond the tolerance.
	Future bool
}

// AssessSnapshotAge parses a recorded snapshot time and classifies it.
func AssessSnapshotAge(stamp string, now time.Time) SnapshotAge {
	if stamp == "" {
		return SnapshotAge{}
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return SnapshotAge{}
	}
	return AssessSnapshotAt(at, now)
}

// AssessSnapshotAt classifies an already-parsed snapshot time. A zero time is unknown,
// the same as a stamp that could not be parsed: neither can establish freshness.
func AssessSnapshotAt(at time.Time, now time.Time) SnapshotAge {
	if at.IsZero() {
		return SnapshotAge{}
	}
	age := now.Sub(at)
	return SnapshotAge{Known: true, At: at, Age: age, Future: age < -snapshotClockTolerance}
}

// RecoveryState is the recorded recovery point in the terms the model needs. Both readers
// build one: the production check from the database file, the server from the settings
// row.
type RecoveryState struct {
	// Enabled is whether scheduled backups are on.
	Enabled bool
	// NewestSnapshotAt is the recorded generation's own snapshot time, NewestUploads
	// whether it carries uploaded files, and NewestCopied whether it has a verified
	// independent copy — the same name and digest, not merely a copy that exists.
	NewestSnapshotAt string
	NewestUploads    bool
	NewestCopied     bool
	// FallbackIdentifiable and FallbackSnapshotAt describe the newest generation that is
	// still independently recoverable when the recorded one has no verified copy;
	// FallbackUploads is its own coverage.
	FallbackIdentifiable bool
	FallbackSnapshotAt   string
	FallbackUploads      bool
	// JobError and CopyError are the newest attempt's recorded failures.
	JobError  string
	CopyError string
	// Identity names the recovery point for a message.
	Identity string
}

// RecoveryPoint is the snapshot freshness must be judged by: the newest generation with a
// verified independent copy, or, failing that, the newest generation that is still
// independently recoverable. A local copy on this host is not a recovery point, and a
// completed copy of an old archive does not make the old data fresh.
type RecoveryPoint struct {
	Known    bool
	Stamp    string
	Identity string
	Uploads  bool
}

// NewestRecoveryPoint selects the recovery point from the recorded state.
func NewestRecoveryPoint(state RecoveryState) RecoveryPoint {
	if state.NewestCopied && state.NewestSnapshotAt != "" {
		return RecoveryPoint{Known: true, Stamp: state.NewestSnapshotAt, Identity: state.Identity, Uploads: state.NewestUploads}
	}
	if state.FallbackIdentifiable && state.FallbackSnapshotAt != "" {
		return RecoveryPoint{Known: true, Stamp: state.FallbackSnapshotAt, Identity: state.Identity, Uploads: state.FallbackUploads}
	}
	return RecoveryPoint{}
}

// RecoveryVerdict is the model's answer.
type RecoveryVerdict struct {
	// Severity is "ok", "warning" or "critical". "ok" is a claim that a specific,
	// identified recovery point is fresh — never that a job ran recently.
	Severity string
	// Summary is one sentence an operator can act on.
	Summary string
	// Age is the recovery point's age when it is known.
	Age time.Duration
	// Known is whether the recovery point's time was present and parseable.
	Known bool
}

// RecoveryThresholds returns the warning and critical ages for a backup cadence.
//
// The master plan proposes a 12-hour backup-and-copy cadence with a warning at 18 hours
// and a critical at 24 hours; these factors reproduce exactly those numbers at that
// cadence and scale with an operator's own interval, so an installation that chose a
// 24-hour schedule is not warned six hours before its next backup is even due. With no
// cadence recorded, the recommended twelve hours is assumed.
func RecoveryThresholds(interval time.Duration) (warning, critical time.Duration) {
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	return interval * 3 / 2, interval * 2
}

// AssessRecovery classifies the recorded recovery state against the thresholds. It is the
// single implementation of the freshness question both readers ask.
func AssessRecovery(state RecoveryState, now time.Time, warning, critical time.Duration) RecoveryVerdict {
	if !state.Enabled {
		return RecoveryVerdict{Severity: "ok", Summary: "Scheduled backups are disabled."}
	}
	// A failed newest attempt is reported promptly and by itself: it is a job problem,
	// not a statement about the recovery point that still exists behind it.
	if state.JobError != "" {
		return RecoveryVerdict{Severity: "critical", Summary: "The latest backup attempt failed: " + state.JobError}
	}
	if state.CopyError != "" {
		return RecoveryVerdict{Severity: "critical", Summary: "The latest independent copy attempt failed: " + state.CopyError}
	}
	point := NewestRecoveryPoint(state)
	if !point.Known {
		return RecoveryVerdict{Severity: "critical", Summary: "No independently recoverable verified copy is recorded. A backup kept only on this host is not recovery."}
	}
	age := AssessSnapshotAge(point.Stamp, now)
	if !age.Known {
		// A copy completion time is not a statement about when the data was captured, so
		// an unknown snapshot stays unknown and is never reported as fresh.
		return RecoveryVerdict{Severity: "critical", Summary: "The independently recoverable snapshot's age is unknown; a copy completion time does not establish recovery freshness."}
	}
	if age.Future {
		return RecoveryVerdict{Severity: "critical", Summary: "The independently recoverable snapshot's time is in the future; check the host clock."}
	}
	identity := "the independently recoverable generation"
	if state.Identity != "" {
		identity = state.Identity
	}
	coverage := "database-only"
	if point.Uploads {
		coverage = "database and uploads"
	}
	switch {
	case age.Age > critical:
		return RecoveryVerdict{Severity: "critical", Known: true, Age: age.Age, Summary: fmt.Sprintf(
			"The independently recoverable snapshot is %s old (maximum %s); %s is %s.",
			age.Age.Round(time.Minute), critical, identity, coverage)}
	case age.Age > warning:
		return RecoveryVerdict{Severity: "warning", Known: true, Age: age.Age, Summary: fmt.Sprintf(
			"The independently recoverable snapshot is %s old (warning at %s); %s is %s.",
			age.Age.Round(time.Minute), warning, identity, coverage)}
	}
	return RecoveryVerdict{Severity: "ok", Known: true, Age: age.Age, Summary: "The independently recoverable snapshot is fresh."}
}
