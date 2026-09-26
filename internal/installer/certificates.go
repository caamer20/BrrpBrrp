package installer

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"brrpbrrp/internal/app"
)

func certificateConfig(p Plan) app.Config {
	return app.Config{DataDir: p.Paths.DataDir, CertFile: filepath.Join(p.Paths.DataDir, "certs", "tls.crt"), KeyFile: filepath.Join(p.Paths.DataDir, "certs", "tls.key")}
}

func (w *wizard) certificateInfo(out http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.Plan == nil || w.state.Plan.Settings.Mode != "network" {
		problem(out, 404, "No local certificate is available.")
		return
	}
	_, cert, err := app.TrustCertificate(certificateConfig(*w.state.Plan))
	if err != nil {
		problem(out, 404, "The certificate is not ready. Check that the server has started.")
		return
	}
	reply(out, 200, map[string]any{"fingerprint": app.CertificateFingerprint(cert), "is_ca": cert.IsCA, "name": cert.Subject.CommonName, "expires_at": cert.NotAfter})
}

func (w *wizard) certificateAction(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot change certificates or device trust.")
		return
	}
	if err := w.privilege(); err != nil {
		problem(out, 403, err.Error())
		return
	}
	var input struct {
		Confirm     bool   `json:"confirm"`
		Fingerprint string `json:"fingerprint"`
	}
	if !readJSON(out, r, &input) {
		return
	}
	if !input.Confirm {
		problem(out, 400, "Review and confirm the certificate action first.")
		return
	}
	w.mu.Lock()
	if w.state.Plan == nil || w.state.Plan.Settings.Mode != "network" || !w.state.Registered || w.state.busy() {
		w.mu.Unlock()
		problem(out, 409, "Finish the current setup step first.")
		return
	}
	p := *w.state.Plan
	data, cert, err := app.TrustCertificate(certificateConfig(p))
	if err != nil || input.Fingerprint != app.CertificateFingerprint(cert) {
		w.mu.Unlock()
		problem(out, 409, "The certificate changed. Refresh the certificate details before continuing.")
		return
	}
	phase := w.state.Phase
	w.state.Phase = "certifying"
	w.mu.Unlock()
	if r.URL.Path == "/api/install/trust-certificate" {
		err := w.trust(r.Context(), data)
		w.mu.Lock()
		w.state.Phase = phase
		w.mu.Unlock()
		if err != nil {
			problem(out, 500, err.Error())
			return
		}
		reply(out, 200, map[string]bool{"trusted": true})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Minute)
		defer cancel()
		err := w.repair(ctx, p)
		if err != nil {
			w.mu.Lock()
			w.state.Phase = "error"
			w.state.Error = err.Error()
			w.mu.Unlock()
			return
		}
		w.perform(p, true)
	}()
	reply(out, 202, map[string]bool{"started": true})
}

func repairInstallationCertificate(ctx context.Context, p Plan) error {
	if err := nativeStop(ctx, p); err != nil {
		return err
	}
	// Use the current setup executable so an older installation can be repaired.
	binary, err := os.Executable()
	if err == nil {
		err = command(ctx, binary, "repair-cert", "--config", p.Paths.Config)
	}
	if err == nil {
		err = nativeOwnFiles(ctx, p)
	}
	if err != nil {
		restartErr := nativeStart(ctx, p)
		return errors.Join(err, restartErr)
	}
	return nil
}
