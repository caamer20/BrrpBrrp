package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The publication contract has four boundaries a crash can hit: the file write,
// the flush, the rename and the directory entry. These tests exercise what an
// observer and the caller can see at each one, because a database reference is
// only allowed after the last of them.

func TestPublishExposesACompleteFileOnlyAfterItIsPublished(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "attachment")

	published, err := Publish(dir, "attachment", func(file *os.File) error {
		// During the write, a reader must find no file at the final name: that is
		// what stops a partially written upload from being served or referenced.
		if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("final name existed during the write: %v", statErr)
		}
		// The temporary file must already live in the destination directory, so
		// publication is a rename and never a cross-device copy.
		if got := filepath.Dir(file.Name()); got != dir {
			t.Errorf("temporary file in %s, want %s", got, dir)
		}
		_, err := file.WriteString("durable bytes")
		return err
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published != target {
		t.Fatalf("published = %s, want %s", published, target)
	}
	content, err := os.ReadFile(published)
	if err != nil {
		t.Fatalf("read published file: %v", err)
	}
	if string(content) != "durable bytes" {
		t.Fatalf("content = %q", content)
	}
	info, err := os.Stat(published)
	if err != nil {
		t.Fatalf("stat published file: %v", err)
	}
	// Windows protects files through ACLs, not Unix permission bits. The atomic
	// publication and content assertions above apply on every platform.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0600 {
		t.Fatalf("published mode = %v, want 0600", perm)
	}
	assertOnly(t, dir, "attachment")
}

func TestPublishRefusesToReplaceAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "attachment")
	if err := os.WriteFile(target, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	wrote := false
	if _, err := Publish(dir, "attachment", func(file *os.File) error {
		wrote = true
		_, err := file.WriteString("second")
		return err
	}); err == nil {
		t.Fatal("publish replaced an existing file")
	}
	if wrote {
		t.Fatal("publish wrote data before checking the destination")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first" {
		t.Fatalf("existing file changed to %q", content)
	}
	assertOnly(t, dir, "attachment")
}

func TestPublishLeavesNothingBehindWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	_, err := Publish(dir, "attachment", func(file *os.File) error {
		if _, err := file.WriteString("half a file"); err != nil {
			return err
		}
		return errors.New("connection reset")
	})
	if err == nil {
		t.Fatal("publish reported success after a failed write")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("publish lost the writer's error: %v", err)
	}
	assertOnly(t, dir)
}

func TestPublishRequiresAPlainFileName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escape", "nested/file"} {
		if _, err := Publish(dir, name, func(*os.File) error { return nil }); err == nil {
			t.Fatalf("publish accepted the name %q", name)
		}
	}
	assertOnly(t, dir)
}

// The directory-sync outcome must be a stated fact, not an accident: either the
// volume synchronized the entry, or it said it cannot. A bare error here would
// mean a real filesystem failure was being swallowed, and a silent nil on a
// platform that cannot sync would let a caller claim durability it does not have.
func TestDirectorySynchronizationIsExplicit(t *testing.T) {
	dir := t.TempDir()
	err := SyncDirectory(dir)
	if err != nil && !errors.Is(err, ErrDirectorySyncUnsupported) {
		t.Fatalf("SyncDirectory returned an unclassified error: %v", err)
	}
	// Publication succeeds either way, and the final name is present afterwards.
	published, err := Publish(dir, "attachment", func(file *os.File) error {
		_, err := file.WriteString("bytes")
		return err
	})
	if err != nil {
		t.Fatalf("publish on a volume that reports %v: %v", ErrDirectorySyncUnsupported, err)
	}
	if _, err := os.Stat(published); err != nil {
		t.Fatalf("published file missing: %v", err)
	}
}

// A kill mid-write leaves the temporary file behind, because the deferred
// cleanup never runs. It must not be mistaken for an attachment, and the reaper
// must remove an old one while leaving a write that is still in flight alone.
func TestAbandonedTemporaryPublicationsAreReaped(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, publishTempPrefix+"stale")
	if err := os.WriteFile(stale, []byte("half a file"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, publishTempPrefix+"inflight")
	if err := os.WriteFile(fresh, []byte("still arriving"), 0600); err != nil {
		t.Fatal(err)
	}
	claimed := filepath.Join(dir, "a-claimed-attachment")
	if err := os.WriteFile(claimed, []byte("referenced by a row"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(claimed, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := SweepStaleTemporaries(dir, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d files, want exactly the abandoned one", removed)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned temporary file survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a publication still being written was removed: %v", err)
	}
	if _, err := os.Stat(claimed); err != nil {
		t.Fatalf("a published file was removed: %v", err)
	}
}

func TestPublishCreatesAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads", "nested")
	published, err := Publish(dir, "attachment", func(file *os.File) error {
		_, err := file.WriteString("bytes")
		return err
	})
	if err != nil {
		t.Fatalf("publish into a missing directory: %v", err)
	}
	if _, err := os.Stat(published); err != nil {
		t.Fatalf("published file missing: %v", err)
	}
}

// assertOnly asserts the directory holds exactly the named entries, which is how
// these tests notice a temporary file that a failure path forgot to remove.
func assertOnly(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if len(got) != len(want) {
		t.Fatalf("directory holds %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("directory holds %v, want %v", got, want)
		}
	}
}
