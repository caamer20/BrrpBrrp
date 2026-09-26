package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedFrontend(t *testing.T) {
	b, err := fs.ReadFile(Assets(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "BrrpBrrp") || !strings.Contains(string(b), "/assets/") {
		t.Fatal("missing compiled frontend")
	}
}
