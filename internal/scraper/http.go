package scraper

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func ValidateURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return nil, errors.New("only public HTTPS URLs on port 443 are allowed")
	}
	return parsed, nil
}

func publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("host has no addresses")
	}
	for _, address := range addresses {
		if !PublicIP(address) {
			return nil, errors.New("private and reserved addresses are blocked")
		}
	}
	return addresses, nil
}

func PublicURL(ctx context.Context, raw string) bool {
	parsed, err := ValidateURL(raw)
	if err != nil {
		return false
	}
	_, err = publicAddresses(ctx, parsed.Hostname())
	return err == nil
}

func HTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 32 << 10,
		TLSHandshakeTimeout:    3 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if port != "443" {
				return nil, errors.New("outbound port is blocked")
			}
			addresses, err := publicAddresses(ctx, host)
			if err != nil {
				return nil, err
			}
			// Dial a validated address directly; a second DNS lookup could be rebound.
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		_, err := ValidateURL(request.URL.String())
		return err
	}}
}
