package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestCompressedAssetsAndConditionalRequests(t *testing.T) {
	names, err := fs.Glob(Assets(), "assets/index-*.js")
	if err != nil || len(names) == 0 {
		t.Fatal("build frontend first", err)
	}
	name := names[0]
	original, err := fs.ReadFile(Assets(), name)
	if err != nil {
		t.Fatal(err)
	}
	call := func(encoding, method, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/"+name, nil)
		r.Header.Set("Accept-Encoding", encoding)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		ServeAsset(w, r, name)
		return w
	}
	identity := call("", "GET", "")
	if identity.Code != 200 || identity.Header().Get("Content-Encoding") != "" || !bytes.Equal(identity.Body.Bytes(), original) {
		t.Fatal("identity asset changed")
	}
	compressed := call("gzip", "GET", "")
	if compressed.Code != 200 || compressed.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(compressed.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("gzip not negotiated", compressed.Header())
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(unpacked, original) {
		t.Fatal("gzip body corrupted")
	}
	if compressed.Body.Len() >= len(original) {
		t.Fatal("compression did not reduce bytes")
	}
	if call("br, gzip;q=0.5", "GET", "").Header().Get("Content-Encoding") != "br" {
		t.Fatal("Brotli preference ignored")
	}
	if call("br;q=0,gzip;q=0", "GET", "").Header().Get("Content-Encoding") != "" {
		t.Fatal("disabled encoding accepted")
	}
	if call("br;q=0.1,gzip;q=0.8", "GET", "").Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("quality priority ignored")
	}
	etag := compressed.Header().Get("ETag")
	if etag == "" || etag == identity.Header().Get("ETag") {
		t.Fatal("representation ETags are not distinct")
	}
	if call("gzip", "GET", etag).Code != 304 {
		t.Fatal("conditional request did not return 304")
	}
	head := call("gzip", "HEAD", "")
	size, _ := strconv.Atoi(head.Header().Get("Content-Length"))
	if head.Body.Len() != 0 || size != compressed.Body.Len() {
		t.Fatal("HEAD does not describe compressed representation")
	}
}
