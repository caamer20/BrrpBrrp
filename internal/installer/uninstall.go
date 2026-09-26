package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"brrpbrrp/internal/app"
)

type installedService struct {
	Binary, Config, Definition, Signature string
}

type RemovalPlan struct {
	ID          string   `json:"id"`
	Found       bool     `json:"found"`
	Binaries    []string `json:"binaries"`
	Config      string   `json:"config"`
	DataDir     string   `json:"data_dir"`
	NextDataDir string   `json:"next_data_dir"`
	Service     bool     `json:"service"`
	Blocked     string   `json:"blocked"`
	metadata    string
	service     *installedService
}

func detectInstallation(ctx context.Context, paths Paths) (*RemovalPlan, error) {
	service, err := nativeInstalledService(ctx)
	if err != nil {
		return nil, err
	}
	return inspectInstallation(paths, nativeLegacyPaths(paths), service)
}

func inspectInstallation(paths Paths, legacy []Paths, service *installedService) (*RemovalPlan, error) {
	p := &RemovalPlan{Binaries: []string{}, DataDir: paths.DataDir, service: service, Service: service != nil}
	candidates := append([]Paths{paths}, legacy...)
	allowed := []string{}
	for _, candidate := range candidates {
		allowed = append(allowed, candidate.Binary)
	}
	if service != nil {
		if !slices.ContainsFunc(allowed, func(path string) bool {
			if runtime.GOOS == "windows" {
				return strings.EqualFold(filepath.Clean(path), filepath.Clean(service.Binary))
			}
			return path == service.Binary
		}) || !filepath.IsAbs(service.Config) {
			return nil, errors.New("The registered BrrpBrrp service uses a custom application location. Remove it using your service manager; setup will not remove an unrelated executable.")
		}
		p.Config = service.Config
		p.Found = true
	}
	hash := sha256.New()
	if service != nil {
		_, _ = fmt.Fprintln(hash, service.Signature)
	}
	for _, binary := range allowed {
		info, err := regularFile(binary)
		if err != nil {
			return nil, err
		}
		if info != nil && !slices.Contains(p.Binaries, binary) {
			p.Found = true
			p.Binaries = append(p.Binaries, binary)
			_, _ = fmt.Fprintln(hash, binary, info.Size(), info.ModTime().UnixNano())
			if running, err := os.Executable(); err == nil {
				if self, err := os.Stat(running); err == nil && os.SameFile(self, info) {
					p.Blocked = "Setup is running from the installed application. Close this window, extract the latest release into a separate folder, and open Setup from that folder to uninstall."
				}
			}
		}
	}
	metadata := filepath.Join(paths.InstallDir, "installation.json")
	if info, err := regularFile(metadata); err != nil {
		return nil, err
	} else if info != nil {
		data, err := os.ReadFile(metadata)
		if err != nil {
			return nil, err
		}
		var previous Plan
		p.metadata, p.Found = metadata, true
		if json.Unmarshal(data, &previous) == nil && previous.Paths.Binary == paths.Binary && previous.Paths.InstallDir == paths.InstallDir && filepath.IsAbs(previous.Paths.Config) {
			if p.Config == "" {
				p.Config = previous.Paths.Config
			}
			if filepath.IsAbs(previous.Paths.DataDir) {
				p.DataDir = previous.Paths.DataDir
			}
		}
		_, _ = hash.Write(data)
	}
	if p.Config == "" {
		for _, candidate := range candidates {
			if info, err := regularFile(candidate.Config); err != nil {
				return nil, err
			} else if info != nil {
				p.Config = candidate.Config
				break
			}
		}
	}
	if p.Config != "" {
		info, err := regularFile(p.Config)
		if err != nil {
			return nil, err
		}
		if info != nil {
			values, err := app.ReadEnvFile(p.Config)
			if err != nil {
				return nil, fmt.Errorf("Read the existing settings: %w", err)
			}
			if data := values["BRRP_DATA_DIR"]; data != "" {
				if !filepath.IsAbs(data) {
					return nil, errors.New("The old settings use a relative data folder. Change BRRP_DATA_DIR to its absolute location before uninstalling so setup can identify the data to keep.")
				}
				p.DataDir = filepath.Clean(data)
			}
			data, err := os.ReadFile(p.Config)
			if err != nil {
				return nil, err
			}
			_, _ = hash.Write(data)
			p.Found = true
		}
	}
	if _, err := app.Contained(p.DataDir, "server.lock"); err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(p.DataDir); err == nil {
		p.Found = p.Found || len(entries) > 0
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var err error
	p.NextDataDir, err = freshDataDir(paths.DataDir)
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintln(hash, p.Binaries, p.Config, p.DataDir, p.metadata, p.Service)
	p.ID = hex.EncodeToString(hash.Sum(nil))
	return p, nil
}

// Only individual application files are removed. Data folders and configuration
// files are never passed to Remove or RemoveAll, including after a failed retry.
func uninstall(ctx context.Context, p RemovalPlan, report progress) error {
	if p.Blocked != "" {
		return errors.New(p.Blocked)
	}
	report("Stopping and removing the background service")
	if err := nativeUninstall(ctx, p); err != nil {
		return err
	}
	report("Removing the old application; keeping accounts, messages and settings")
	return removeApplicationFiles(p)
}

func removeApplicationFiles(p RemovalPlan) error {
	if _, err := os.Stat(p.DataDir); err == nil {
		lock, err := app.LockData(app.Config{DataDir: p.DataDir})
		if err != nil {
			return fmt.Errorf("The service was removed, but another server may still be using the data folder. Close any manually started BrrpBrrp server and retry: %w", err)
		}
		defer lock.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, path := range append(append([]string{}, p.Binaries...), p.metadata) {
		if path == "" {
			continue
		}
		if _, err := regularFile(path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("Could not remove %s. Close other setup windows and retry. Your data has been kept: %w", path, err)
		}
	}
	return nil
}

func regularFile(path string) (os.FileInfo, error) {
	if _, err := app.Contained(filepath.Dir(path), path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Setup will not remove a directory or link at %s.", path)
	}
	return info, nil
}

func freshDataDir(preferred string) (string, error) {
	for i := 0; i < 1000; i++ {
		path := preferred
		if i > 0 {
			path = fmt.Sprintf("%s-%d", preferred, i+1)
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("Choose an unused folder for the next installation.")
}

func (w *wizard) removalPlan(out http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.busy() {
		problem(out, 409, "Wait for the current operation to finish.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	p, err := w.detect(ctx, w.paths)
	if err != nil {
		problem(out, 400, err.Error())
		return
	}
	w.removal = p
	reply(out, 200, p)
}

func (w *wizard) removeInstallation(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot uninstall or change this computer.")
		return
	}
	if err := w.privilege(); err != nil {
		problem(out, 403, err.Error())
		return
	}
	var input struct {
		ID        string `json:"id"`
		Confirmed bool   `json:"confirmed"`
	}
	if !readJSON(out, r, &input) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.busy() {
		problem(out, 409, "Wait for the current operation to finish.")
		return
	}
	if !input.Confirmed || w.removal == nil || !w.removal.Found || input.ID != w.removal.ID {
		problem(out, 409, "Review the existing installation and confirm before uninstalling.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	p, err := w.detect(ctx, w.paths)
	if err != nil {
		problem(out, 400, err.Error())
		return
	}
	if p.ID != input.ID {
		problem(out, 409, "The installation changed. Refresh the uninstall review before trying again.")
		return
	}
	if p.Blocked != "" {
		problem(out, 409, p.Blocked)
		return
	}
	w.state = State{Phase: "removing", Message: "Preparing to uninstall", Removal: p}
	go w.performRemoval(*p)
	reply(out, 202, map[string]bool{"started": true})
}

func (w *wizard) performRemoval(p RemovalPlan) {
	ctx, cancel := context.WithTimeout(w.ctx, 90*time.Second)
	defer cancel()
	report := func(message string) { w.mu.Lock(); w.state.Message = message; w.mu.Unlock() }
	err := w.remove(ctx, p, report)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.state.Phase, w.state.Error = "remove_error", err.Error()
		return
	}
	w.state.Phase = "removed"
	w.state.Message = "The old application has been removed. Your data and settings have been kept."
	w.paths.DataDir, w.paths.Config = p.NextDataDir, filepath.Join(p.NextDataDir, "server.env")
	w.removal = nil
}

func (s State) busy() bool {
	return s.Phase == "certifying" || s.Phase == "installing" || s.Phase == "creating" || s.Phase == "removing"
}

// systemd command lines are parsed only for recognizing our four-argument
// service command. This never evaluates a shell or executes the old binary.
func serviceCommand(line string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, char := range line {
		if escaped {
			word.WriteRune(char)
			escaped = false
			started = true
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote, started = char, true
			continue
		}
		if char == ' ' || char == '\t' {
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		} else {
			word.WriteRune(char)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("The service command contains incomplete quoting.")
	}
	if started {
		args = append(args, word.String())
	}
	for i := range args {
		args[i] = strings.NewReplacer("%%", "%", "$$", "$").Replace(args[i])
	}
	if len(args) != 4 || args[1] != "serve" || args[2] != "--config" {
		return nil, errors.New("The service does not use the expected BrrpBrrp command.")
	}
	return args, nil
}
