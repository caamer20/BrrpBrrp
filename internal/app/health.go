package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// CheckHealth probes this process's configured listener without opening the database
// or taking its lock. A proxy/public hostname is used for TLS identity, never dialing.
func CheckHealth(ctx context.Context, c Config) error {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	public, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	scheme := "https"
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	if c.TLSMode == "http-local" || c.TLSMode == "http-behind-proxy" {
		scheme = "http"
	} else {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		for _, path := range []string{LocalCAFile(c), c.CertFile} {
			if path == "" {
				continue
			}
			pem, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("health check could not read a certificate in %s", path)
			}
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: public.Hostname()}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+net.JoinHostPort(host, port)+"/api/setup/status", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d", response.StatusCode)
	}
	var status struct {
		SetupRequired *bool `json:"setup_required"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&status); err != nil || status.SetupRequired == nil {
		return errors.New("health check received an invalid server response")
	}
	return nil
}
