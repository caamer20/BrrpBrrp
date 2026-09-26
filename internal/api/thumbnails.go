package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const thumbnailPixels = 16_000_000
const thumbnailEdge = 640

// abandonedPublicationGrace is how long a temporary publication file may sit in
// the disposable preview directory before it is removed. The hour-long
// maintenance sweep uses the same idea with a 24-hour grace period; previews are
// cheap to rebuild, so a shorter window is safe here.
const abandonedPublicationGrace = time.Hour

// Authentication and attachment ownership are checked by download before this runs.
// Derivatives are private, disposable, and excluded from data backups. ctx is the
// delivery's revocation context, so a preview being streamed ends with the access
// that admitted it just as the original file does.
func (s *Server) thumbnail(ctx context.Context, w http.ResponseWriter, r *http.Request, attachment db.Attachment, source *os.File) {
	switch attachment.MIMEType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		problem(w, 415, "This file has no image preview.")
		return
	}
	directory, err := app.Contained(s.config.DataDir, "cache/previews")
	if err != nil {
		s.failed(w, err)
		return
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		s.failed(w, err)
		return
	}
	path, err := app.Contained(s.config.DataDir, filepath.Join(directory, attachment.ID+".jpg"))
	if err != nil {
		s.failed(w, err)
		return
	}
	serve := func() bool {
		file, err := os.Open(path)
		if err != nil {
			return false
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || !stat.Mode().IsRegular() {
			return false
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		http.ServeContent(w, r, "preview.jpg", stat.ModTime(), revocableReader{ctx: ctx, reader: file})
		return true
	}
	if serve() {
		return
	}
	wait := time.NewTimer(2 * time.Second)
	defer wait.Stop()
	select {
	case s.thumbnailSlots <- struct{}{}:
		defer func() { <-s.thumbnailSlots }()
	case <-r.Context().Done():
		return
	case <-wait.C:
		w.Header().Set("Retry-After", "2")
		problem(w, 503, "Another preview is being prepared. Try again shortly.")
		return
	}
	if serve() {
		return
	}
	encoded, err := makeThumbnail(source)
	if err != nil {
		problem(w, 422, "Preview unavailable. You can still open the original file.")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	temp, err := os.CreateTemp(directory, "preview-*")
	if err != nil {
		s.failed(w, err)
		return
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(encoded)
	// A preview is a rebuildable cache entry, but it is still flushed before it
	// is published under its final name so a restart never reads a partial image.
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		problem(w, 500, "The preview could not be saved.")
		return
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		s.failed(w, err)
		return
	}
	pruneThumbnails(directory, path)
	if !serve() {
		problem(w, 500, "The preview could not be opened.")
	}
}

func makeThumbnail(source io.ReadSeeker) ([]byte, error) {
	config, _, err := image.DecodeConfig(source)
	if err != nil {
		return nil, err
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 16000 || config.Height > 16000 || int64(config.Width)*int64(config.Height) > thumbnailPixels {
		return nil, errors.New("image dimensions exceed preview budget")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	prefix := make([]byte, 65536)
	n, _ := io.ReadFull(source, prefix)
	orientation := jpegOrientation(prefix[:n])
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(source)
	if err != nil {
		return nil, err
	}
	width, height := config.Width, config.Height
	if width > thumbnailEdge || height > thumbnailEdge {
		if width >= height {
			height = max(1, height*thumbnailEdge/width)
			width = thumbnailEdge
		} else {
			width = max(1, width*thumbnailEdge/height)
			height = thumbnailEdge
		}
	}
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(resized, resized.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(resized, resized.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
	var output image.Image = resized
	if orientation > 1 && orientation <= 8 {
		outWidth, outHeight := width, height
		if orientation >= 5 {
			outWidth, outHeight = height, width
		}
		rotated := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				dx, dy := x, y
				switch orientation {
				case 2:
					dx = width - 1 - x
				case 3:
					dx, dy = width-1-x, height-1-y
				case 4:
					dy = height - 1 - y
				case 5:
					dx, dy = y, x
				case 6:
					dx, dy = height-1-y, x
				case 7:
					dx, dy = height-1-y, width-1-x
				case 8:
					dx, dy = y, width-1-x
				}
				rotated.SetRGBA(dx, dy, resized.RGBAAt(x, y))
			}
		}
		output = rotated
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, output, &jpeg.Options{Quality: 78}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// Read the small EXIF orientation field without trusting TIFF offsets or lengths.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for at := 2; at+4 <= len(data); {
		if data[at] != 0xff {
			return 1
		}
		marker := data[at+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[at+2 : at+4]))
		if size < 2 || at+2+size > len(data) {
			return 1
		}
		segment := data[at+4 : at+2+size]
		if marker == 0xe1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			tiff := segment[6:]
			var order binary.ByteOrder
			if string(tiff[:2]) == "II" {
				order = binary.LittleEndian
			} else if string(tiff[:2]) == "MM" {
				order = binary.BigEndian
			} else {
				return 1
			}
			if order.Uint16(tiff[2:4]) != 42 {
				return 1
			}
			offset := uint64(order.Uint32(tiff[4:8]))
			if offset+2 > uint64(len(tiff)) {
				return 1
			}
			count := int(order.Uint16(tiff[offset : offset+2]))
			for i := 0; i < count; i++ {
				start := offset + 2 + uint64(i)*12
				if start+12 > uint64(len(tiff)) {
					return 1
				}
				entry := tiff[start : start+12]
				if order.Uint16(entry[:2]) == 0x0112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
					value := int(order.Uint16(entry[8:10]))
					if value >= 1 && value <= 8 {
						return value
					}
					return 1
				}
			}
		}
		at += 2 + size
	}
	return 1
}

func pruneThumbnails(directory, keep string) {
	// Previews are disposable, so an interrupted preview publication leaves a
	// `.publish-*` file that no row refers to. Unlike the upload directory, this
	// sweep only looks at `.jpg` entries for its size budget, so abandoned
	// temporary files are reaped explicitly here or they accumulate forever.
	_, _ = app.SweepStaleTemporaries(directory, time.Now().Add(-abandonedPublicationGrace))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	var files []os.FileInfo
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jpg") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, info)
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	count := len(files)
	for _, file := range files {
		if count <= 128 && total <= 64<<20 {
			break
		}
		path := filepath.Join(directory, file.Name())
		if path == keep {
			continue
		}
		if os.Remove(path) == nil {
			count--
			total -= file.Size()
		}
	}
}
