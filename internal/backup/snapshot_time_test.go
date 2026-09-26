package backup

import (
	"context"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/db"
)

// TestASnapshotIsDatedAtItsBoundaryNotAtCompletion is the reproduced R3-03 clock gap. The
// manifest used to be dated after VACUUM INTO had written the snapshot and while the
// database entry was being compressed, so a slow snapshot of a large database declared a
// recovery point hours younger than the data the archive actually restores. Health ages
// the recovery point by exactly this value, so it was also the value that made an old
// backup look fresh.
func TestASnapshotIsDatedAtItsBoundaryNotAtCompletion(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 1)
	boundary, later := "2026-01-01T00:00:00.000000000Z", "2026-01-01T06:00:00.000000000Z"
	reads := 0
	snapshotClock = func() string {
		reads++
		if reads == 1 {
			return boundary
		}
		return later
	}
	t.Cleanup(func() { snapshotClock = func() string { return db.Now() } })

	out := filepath.Join(config.DataDir, "snapshot-time.zip")
	if err := Create(context.Background(), store, config, out, false); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the snapshot boundary was read %d times, want one conservative reading", reads)
	}
	manifest, _, err := VerifyArchiveIn(context.Background(), out, filepath.Join(config.DataDir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CreatedAt != boundary {
		t.Fatalf("the archive declares its recovery point as %s; want the snapshot boundary %s, not the completion time %s", manifest.CreatedAt, boundary, later)
	}
}
