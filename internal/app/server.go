package app

import (
	"context"
	"crypto/tls"
	"errors"
	"golang.org/x/crypto/acme/autocert"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func Logger(c Config) (*slog.Logger, io.Closer, error) {
	var w io.Writer = os.Stderr
	var closer io.Closer
	if c.LogFile != "" {
		f, e := openLog(c.LogFile)
		if e != nil {
			return nil, nil, e
		}
		w = f
		closer = f
	}
	level := slog.LevelInfo
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "off":
		w = io.Discard
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})), closer, nil
}

func Serve(ctx context.Context, c Config, handler http.Handler, log *slog.Logger) error {
	s := &http.Server{Addr: c.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10}
	if c.TLSMode != "http-behind-proxy" && c.TLSMode != "http-local" {
		if _, e := os.Stat(c.CertFile); e == nil {
			if c.TLSMode == "auto" {
				if err := RenewLocalCertificate(c); err != nil {
					return err
				}
			}
			s.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else if !os.IsNotExist(e) {
			return e
		} else if c.Domain != "" && c.ACMEEmail != "" {
			manager := &autocert.Manager{Prompt: autocert.AcceptTOS, Cache: autocert.DirCache(filepath.Join(c.DataDir, "certs", "acme")), HostPolicy: autocert.HostWhitelist(c.Domain), Email: c.ACMEEmail}
			s.TLSConfig = manager.TLSConfig()
			s.TLSConfig.MinVersion = tls.VersionTLS12
		} else if c.TLSMode == "auto" {
			if e := GenerateCert(c); e != nil {
				return e
			}
			s.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			log.Warn("using a local certificate authority; clients must trust its public root", "certificate", LocalCAFile(c))
		} else {
			return errors.New("https requires certificate files or domain and ACME email")
		}
	}
	if c.TLSMode == "auto" && s.TLSConfig != nil && s.TLSConfig.GetCertificate == nil {
		source, err := localCertificateSource(c)
		if err != nil {
			return err
		}
		s.TLSConfig.GetCertificate = source
	}
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return err
	}
	log.Info("BrrpBrrp started", "address", ln.Addr().String(), "base_url", c.BaseURL, "data_dir", c.DataDir, "tls_mode", c.TLSMode)
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if s.Shutdown(stop) != nil {
				_ = s.Close()
			}
		case <-done:
		}
	}()
	if s.TLSConfig != nil {
		if s.TLSConfig.GetCertificate != nil {
			err = s.ServeTLS(ln, "", "")
		} else {
			err = s.ServeTLS(ln, c.CertFile, c.KeyFile)
		}
	} else {
		err = s.Serve(ln)
	}
	close(done)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
