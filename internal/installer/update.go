package installer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"brrpbrrp/internal/app"
)

func canUpdate(p Plan) bool {
	source, err := os.Executable()
	if err != nil {
		return false
	}
	a, err := os.Stat(source)
	if err != nil {
		return false
	}
	b, err := os.Stat(p.Paths.Binary)
	return err == nil && !os.SameFile(a, b)
}

func (w *wizard) updateApplication(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot update the application.")
		return
	}
	if err := w.privilege(); err != nil {
		problem(out, 403, err.Error())
		return
	}
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if !readJSON(out, r, &input) {
		return
	}
	if !input.Confirm {
		problem(out, 400, "Confirm the update and brief server restart first.")
		return
	}
	w.mu.Lock()
	if w.state.Plan == nil || !w.state.Registered || w.state.busy() || !canUpdate(*w.state.Plan) {
		w.mu.Unlock()
		problem(out, 409, "Open setup from the newly extracted release to update your installed application.")
		return
	}
	p := *w.state.Plan
	// Recheck the saved plan before stopping anything or replacing an executable.
	existing := existingInstallation(w.paths)
	if existing == nil || existing.configText() != p.configText() {
		w.mu.Unlock()
		problem(out, 409, "The installation changed. Reopen setup before updating.")
		return
	}
	w.state.Phase, w.state.Error, w.state.Message = "installing", "", "Updating the application; your accounts and data stay in place"
	w.mu.Unlock()
	go func() {
		err := w.update(w.ctx, p)
		if err != nil {
			w.mu.Lock()
			w.state.Phase, w.state.Error = "error", err.Error()
			w.mu.Unlock()
			return
		}
		w.perform(p, true)
	}()
	reply(out, 202, map[string]bool{"started": true})
}

func updateInstallation(ctx context.Context, p Plan) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if !canUpdate(p) {
		return errors.New("Open setup from a freshly extracted release folder to update.")
	}
	if err := nativeCloseTray(); err != nil {
		return err
	}
	if err := nativeStop(ctx, p); err != nil {
		return err
	}
	// Restart the previous binary on any failed file operation.
	source, err := os.Executable()
	if err == nil {
		var lock *os.File
		lock, err = app.LockData(app.Config{DataDir: p.Paths.DataDir})
		if err == nil {
			err = replaceExecutable(source, p.Paths.Binary)
			lock.Close()
		}
	}
	if err != nil {
		return errors.Join(err, nativeStart(ctx, p))
	}
	return nil
}

func replaceExecutable(source, target string) error {
	// Stage a complete executable first; keep the previous build beside it.
	directory, err := os.MkdirTemp(filepath.Dir(target), "previous-build-")
	if err != nil {
		return err
	}
	stage := filepath.Join(directory, "replacement")
	if err := copyExecutable(source, stage); err != nil {
		return err
	}
	previous := filepath.Join(directory, filepath.Base(target))
	if err := os.Rename(target, previous); err != nil {
		_ = os.Remove(stage)
		return fmt.Errorf("Close the BrrpBrrp tray and other running copies, then retry the update: %w", err)
	}
	if err := os.Rename(stage, target); err != nil {
		return errors.Join(err, os.Rename(previous, target))
	}
	return nil
}
