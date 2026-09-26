package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type progress func(string)

func install(ctx context.Context, p Plan, report progress) (err error) {
	if err := nativeCheck(); err != nil {
		return err
	}
	if err := checkDestination(&p); err != nil {
		return err
	}
	report("Creating the application and data folders")
	if err := os.MkdirAll(p.Paths.InstallDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(p.Paths.DataDir, 0700); err != nil {
		return err
	}
	if err := nativeSecure(ctx, p); err != nil {
		return err
	}
	var copied, configured bool
	defer func() {
		// Registration starts only after these files and the resume record exist.
		if err != nil {
			if copied {
				_ = os.Remove(p.Paths.Binary)
			}
			if configured {
				_ = os.Remove(p.Paths.Config)
			}
		}
	}()
	report("Copying BrrpBrrp and saving your settings")
	source, err := os.Executable()
	if err != nil {
		return err
	}
	if err := copyExecutable(source, p.Paths.Binary); err != nil {
		return err
	}
	copied = true
	if err := writeNew(p.Paths.Config, []byte(p.configText()), 0600); err != nil {
		return err
	}
	configured = true
	if err := nativeOwnFiles(ctx, p); err != nil {
		return err
	}
	metadata, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(p.Paths.InstallDir, "installation.json"), metadata, 0600); err != nil {
		return err
	}
	return nil
}

func startInstallation(ctx context.Context, p Plan) error {
	// Registration is idempotent for this exact installation. Saving the record
	// first lets setup recover if registration or the first start is interrupted.
	if err := nativeRegister(ctx, p); err != nil {
		return err
	}
	if err := nativeStart(ctx, p); err != nil {
		return err
	}
	return nativeDesktop(ctx, p)
}

func ensureServiceFile(path string, data []byte) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return writeNew(path, data, 0644)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("An unrelated service file already exists at %s.", path)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(existing) != string(data) {
		return fmt.Errorf("A different service configuration already exists at %s. It has not been changed.", path)
	}
	return nil
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	return nil
}

func writeNew(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func command(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Installer choices must not be silently replaced by preview/development
	// settings inherited from the terminal that launched this process.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "BRRP_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		return fmt.Errorf("%s: %w\n%s", filepath.Base(name), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func existingInstallation(paths Paths) *Plan {
	data, err := os.ReadFile(filepath.Join(paths.InstallDir, "installation.json"))
	if err != nil {
		return nil
	}
	var p Plan
	if json.Unmarshal(data, &p) != nil || p.Paths.Binary != paths.Binary || p.Paths.InstallDir != paths.InstallDir {
		return nil
	}
	config, err := os.ReadFile(p.Paths.Config)
	if err != nil || string(config) != p.configText() {
		// A manually reconfigured server is managed through its service controls;
		// stale wizard settings must not direct account setup to another address.
		return nil
	}
	if _, err := os.Stat(p.Paths.Binary); err != nil {
		return nil
	}
	return &p
}
