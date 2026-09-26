package api

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

func testPhoto(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, picture, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestThumbnailBoundsAndEXIF(t *testing.T) {
	original := testPhoto(t, 1600, 1000)
	thumb, err := makeThumbnail(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil || format != "jpeg" || config.Width != 640 || config.Height != 400 {
		t.Fatal(config, format, err)
	}
	if len(thumb) >= len(original) {
		t.Fatal("preview is not smaller")
	}
	// EXIF orientation 6 rotates a landscape phone photo clockwise.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(exif)+2))
	rotated := append(append(append([]byte{}, original[:2]...), segment...), exif...)
	rotated = append(rotated, original[2:]...)
	thumb, err = makeThumbnail(bytes.NewReader(rotated))
	if err != nil {
		t.Fatal(err)
	}
	config, _, err = image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil || config.Width != 400 || config.Height != 640 {
		t.Fatal("EXIF orientation lost", config, err)
	}
	for _, bad := range [][]byte{nil, {0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}, []byte("not an image")} {
		if jpegOrientation(bad) != 1 {
			t.Fatal("bad EXIF accepted")
		}
	}
	var huge bytes.Buffer
	// A very wide but tiny allocation fails the decode budget before allocating pixels.
	if err := png.Encode(&huge, image.NewRGBA(image.Rect(0, 0, 16001, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := makeThumbnail(bytes.NewReader(huge.Bytes())); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}

func TestThumbnailAuthorizationCacheAndOriginal(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := ordinaryUser(t, s)
	var owner, channel string
	s.store.QueryRow(`SELECT id FROM users WHERE is_admin=1 LIMIT 1`).Scan(&owner)
	s.store.QueryRow(`SELECT id FROM channels WHERE type='text' LIMIT 1`).Scan(&channel)
	id := db.ID()
	original := testPhoto(t, 1000, 800)
	if err := os.WriteFile(filepath.Join(s.config.UploadDir, id), original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES(?,?,?,?,?,?,?)`, id, owner, "photo.jpg", id, len(original), "image/jpeg", db.Now()); err != nil {
		t.Fatal(err)
	}
	path := "/api/uploads/" + id
	if w := request(t, handler, "GET", path+"?preview=1", nil, nil); w.Code != 401 {
		t.Fatal("anonymous preview access")
	}
	if w := request(t, handler, "GET", path+"?preview=1", nil, member); w.Code != 404 {
		t.Fatal("unclaimed upload exposed")
	}
	first := request(t, handler, "GET", path+"?preview=1", nil, admin)
	if first.Code != 200 || first.Header().Get("Content-Type") != "image/jpeg" || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(first.Code, first.Body.String())
	}
	second := request(t, handler, "GET", path+"?preview=1", nil, admin)
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("cached preview changed")
	}
	w := request(t, handler, "GET", path+"?download=1", nil, admin)
	if !bytes.Equal(w.Body.Bytes(), original) {
		t.Fatal("preview changed the original")
	}
	if w := request(t, handler, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "photo", IdempotencyKey: "photo-test-key", AttachmentIDs: []string{id}}, admin); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", path+"?preview=1", nil, member); w.Code != 200 {
		t.Fatal("claimed preview inaccessible", w.Body.String())
	}
	if _, err := s.store.Exec(`DELETE FROM attachments WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "GET", path+"?preview=1", nil, admin); w.Code != 404 {
		t.Fatal("cache bypassed deleted attachment permissions")
	}
}

func TestThumbnailCacheIsBounded(t *testing.T) {
	directory := t.TempDir()
	for i := 0; i < 135; i++ {
		path := filepath.Join(directory, db.ID()+".jpg")
		if err := os.WriteFile(path, []byte("preview"), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Unix(int64(i), 0)
		os.Chtimes(path, old, old)
	}
	pruneThumbnails(directory, "")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 128 {
		t.Fatal("unbounded cache", len(entries))
	}
}

// A preview killed mid-publication leaves a `.publish-*` file that no row refers
// to. The size budget only counts `.jpg` entries, so without an explicit reaper
// those leftovers would accumulate one per crash.
func TestInterruptedPreviewPublicationIsReaped(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, ".publish-abandoned")
	if err := os.WriteFile(stale, []byte("half a preview"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	inflight := filepath.Join(directory, ".publish-arriving")
	if err := os.WriteFile(inflight, []byte("still arriving"), 0600); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(directory, db.ID()+".jpg")
	if err := os.WriteFile(cached, []byte("preview"), 0600); err != nil {
		t.Fatal(err)
	}

	pruneThumbnails(directory, "")

	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned preview publication survived: %v", err)
	}
	if _, err := os.Stat(inflight); err != nil {
		t.Fatalf("a preview publication in flight was removed: %v", err)
	}
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("pruning removed a usable cached preview: %v", err)
	}
}

func BenchmarkThumbnailDecode(b *testing.B) {
	picture := image.NewRGBA(image.Rect(0, 0, 1600, 1000))
	var data bytes.Buffer
	jpeg.Encode(&data, picture, nil)
	b.ReportAllocs()
	b.SetBytes(int64(data.Len()))
	for b.Loop() {
		if _, err := makeThumbnail(bytes.NewReader(data.Bytes())); err != nil {
			b.Fatal(err)
		}
	}
}
