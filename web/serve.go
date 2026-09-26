package web

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type representation struct {
	data []byte
	etag string
}

var representations sync.Map

// ServeAsset serves only embedded public assets. The caller owns route/access policy.
func ServeAsset(w http.ResponseWriter, r *http.Request, name string) {
	selected, encoding := name, ""
	quality := encodingQuality(r.Header.Get("Accept-Encoding"))
	for _, option := range []struct{ name, extension string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if quality(option.name) <= 0 || (encoding != "" && quality(option.name) <= quality(encoding)) {
			continue
		}
		if _, err := fs.Stat(Assets(), name+option.extension); err == nil {
			selected, encoding = name+option.extension, option.name
		}
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	cached, ok := representations.Load(selected)
	if !ok {
		data, err := fs.ReadFile(Assets(), selected)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		value := &representation{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}
		cached, _ = representations.LoadOrStore(selected, value)
	}
	value := cached.(*representation)
	w.Header().Set("ETag", value.etag)
	// These are precompressed bytes, so their length is known even for HEAD.
	w.Header().Set("Content-Length", strconv.Itoa(len(value.data)))
	if kind := mime.TypeByExtension(path.Ext(name)); kind != "" {
		w.Header().Set("Content-Type", kind)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(value.data))
}

func encodingQuality(header string) func(string) float64 {
	values := map[string]float64{}
	for _, part := range strings.Split(header, ",") {
		sections := strings.Split(strings.TrimSpace(part), ";")
		q := 1.0
		for _, parameter := range sections[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || parsed < 0 || parsed > 1 {
					q = 0
				} else {
					q = parsed
				}
			}
		}
		values[strings.ToLower(sections[0])] = q
	}
	return func(encoding string) float64 {
		if q, ok := values[encoding]; ok {
			return q
		}
		return values["*"]
	}
}
