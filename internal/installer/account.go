package installer

import (
	"bytes"
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
	"path/filepath"
	"strings"
	"time"
)

func clientFor(p Plan) (*http.Client, string, error) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext}
	scheme := "http"
	if p.Settings.Mode == "network" {
		data, err := os.ReadFile(filepath.Join(p.Paths.DataDir, "certs", "tls.crt"))
		if err != nil {
			return nil, "", err
		}
		if root, err := os.ReadFile(filepath.Join(p.Paths.DataDir, "certs", "root.crt")); err == nil {
			data = root
		} else if !os.IsNotExist(err) {
			return nil, "", err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, "", errors.New("The server's trust certificate could not be read.")
		}
		u, _ := url.Parse(p.URL)
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: u.Hostname()}
		scheme = "https"
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("The local server unexpectedly redirected setup.")
	}}
	return client, scheme + "://" + p.Address, nil
}

func waitReady(ctx context.Context, p Plan) (bool, error) {
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var last error
	for {
		client, address, err := clientFor(p)
		if err == nil {
			req, _ := http.NewRequestWithContext(deadline, "GET", address+"/api/setup/status", nil)
			response, requestErr := client.Do(req)
			if requestErr == nil {
				var result struct {
					Required *bool `json:"setup_required"`
				}
				err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
				response.Body.Close()
				client.CloseIdleConnections()
				if err == nil && response.StatusCode == 200 && result.Required != nil {
					return *result.Required, nil
				}
				if err == nil {
					err = fmt.Errorf("server returned HTTP %d", response.StatusCode)
				}
			} else {
				err = requestErr
				client.CloseIdleConnections()
			}
		}
		last = err
		select {
		case <-deadline.Done():
			return false, fmt.Errorf("The service did not become ready: %v. Check %s for details.", last, filepath.Join(p.Paths.DataDir, "logs", "server.log"))
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (w *wizard) account(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot create accounts.")
		return
	}
	var input struct {
		ServerName string `json:"server_name"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if !readJSON(out, r, &input) {
		return
	}
	w.mu.Lock()
	if w.state.Phase != "account" || w.state.Plan == nil {
		w.mu.Unlock()
		problem(out, 409, "Finish installing the server first.")
		return
	}
	p := *w.state.Plan
	w.state.Phase = "creating"
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.state.Phase == "creating" {
			w.state.Phase = "account"
		}
		w.mu.Unlock()
	}()
	token, err := os.ReadFile(filepath.Join(p.Paths.DataDir, "setup-token"))
	if err != nil {
		if required, checkErr := waitReady(r.Context(), p); checkErr == nil && !required {
			w.mu.Lock()
			w.state.Phase = "complete"
			w.mu.Unlock()
			reply(out, 200, map[string]bool{"created": true})
			return
		}
		problem(out, 500, "The server's first-time setup token is unavailable. Retry the server check.")
		return
	}
	client, address, err := clientFor(p)
	if err != nil {
		problem(out, 500, err.Error())
		return
	}
	defer client.CloseIdleConnections()
	data, _ := json.Marshal(map[string]string{"token": strings.TrimSpace(string(token)), "server_name": input.ServerName, "username": input.Username, "password": input.Password})
	req, _ := http.NewRequestWithContext(r.Context(), "POST", address+"/api/setup", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", p.URL)
	response, err := client.Do(req)
	if err != nil {
		problem(out, 502, "Could not reach the local server. Retry account creation.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&result)
		if result.Error == "" {
			result.Error = "Account creation failed. Check the fields and try again."
		}
		problem(out, 400, result.Error)
		return
	}
	w.mu.Lock()
	w.state.Phase = "complete"
	w.state.Message = "Your administrator account is ready"
	w.mu.Unlock()
	reply(out, 201, map[string]bool{"created": true})
}
