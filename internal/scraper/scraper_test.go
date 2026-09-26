package scraper

import (
	"context"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestReservedAddressesAreBlocked(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fd00:ec2::254", "fe80::1", "224.0.0.1", "198.18.0.1", "64:ff9b::7f00:1"} {
		if PublicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
	if !PublicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public IP blocked")
	}
	for _, raw := range []string{"file:///etc/passwd", "http://example.com", "https://user:password@example.com", "https://example.com:8443"} {
		if _, err := ValidateURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := New().Fetch(context.Background(), "https://127.0.0.1"); err == nil {
		t.Fatal("private fetch succeeded")
	}
}

func TestOpenGraphAndGIFParsing(t *testing.T) {
	base, _ := url.Parse("https://tenor.com/view/cat")
	embed := Parse(strings.NewReader(`<html><title>Fallback</title><meta property="og:title" content="A &amp; B"><meta property="og:image" content="/cat.gif"><meta property="og:video" content="https://media.tenor.com/cat.mp4"><meta property="og:description" content="Cat time"></html>`), base)
	if embed.Title != "A & B" || embed.Image != "https://tenor.com/cat.gif" || embed.Video != "https://media.tenor.com/cat.mp4" {
		t.Fatalf("unexpected embed: %+v", embed)
	}
	links := Links("See https://example.com/cat.gif and https://example.com/cat.gif.")
	if len(links) != 1 {
		t.Fatalf("links: %v", links)
	}
}
