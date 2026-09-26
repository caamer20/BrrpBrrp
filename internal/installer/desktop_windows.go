package installer

import (
	"brrpbrrp/internal/app"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"brrpbrrp/internal/desktop"
	"brrpbrrp/web"
)

// Public launch metadata contains only the app origin, never server.env or keys.
func nativeCloseTray() error { return desktop.CloseTray() }

func nativeDesktop(ctx context.Context, p Plan) error {
	version, err := exec.CommandContext(ctx, p.Paths.Binary, "version").Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(version)) != "BrrpBrrp "+app.Version {
		return nil
	}

	data, err := json.Marshal(desktop.Settings{URL: p.URL})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.Paths.InstallDir, "desktop.json"), data, 0644); err != nil {
		return err
	}
	png, err := fs.ReadFile(web.Assets(), "icons/icon-192.png")
	if err != nil {
		return err
	}
	icon := make([]byte, 22)
	binary.LittleEndian.PutUint16(icon[2:], 1)
	binary.LittleEndian.PutUint16(icon[4:], 1)
	icon[6], icon[7] = 192, 192
	binary.LittleEndian.PutUint16(icon[10:], 1)
	binary.LittleEndian.PutUint16(icon[12:], 32)
	binary.LittleEndian.PutUint32(icon[14:], uint32(len(png)))
	binary.LittleEndian.PutUint32(icon[18:], 22)
	icon = append(icon, png...)
	iconPath := filepath.Join(p.Paths.InstallDir, "brrpbrrp.ico")
	if err := os.WriteFile(iconPath, icon, 0644); err != nil {
		return err
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := `$ErrorActionPreference='Stop'; $shell=New-Object -ComObject WScript.Shell; foreach ($folder in @([Environment]::GetFolderPath('CommonPrograms'),[Environment]::GetFolderPath('CommonStartup'))) { $link=$shell.CreateShortcut((Join-Path $folder 'BrrpBrrp.lnk')); $link.TargetPath=` + quote(p.Paths.Binary) + `; $link.Arguments='tray'; $link.WorkingDirectory=` + quote(p.Paths.InstallDir) + `; $link.IconLocation=` + quote(iconPath) + `; $link.WindowStyle=7; $link.Description='BrrpBrrp server controls'; $link.Save() }`
	return command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
}

func nativeOpenTray(p Plan) error {
	programs, err := windows.KnownFolderPath(windows.FOLDERID_CommonPrograms, 0)
	if err != nil {
		return err
	}
	shortcut := filepath.Join(programs, "BrrpBrrp.lnk")
	if _, err := os.Stat(shortcut); err != nil {
		return fmt.Errorf("Update the installed application from the new release to add tray controls: %w", err)
	}
	// Explorer opens the shortcut in the interactive desktop session.
	return desktop.Launch("explorer.exe", shortcut)
}

func removeDesktop(ctx context.Context, binaryPath string) error {
	if err := desktop.CloseTray(); err != nil {
		return err
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := `$ErrorActionPreference='Stop'; $shell=New-Object -ComObject WScript.Shell; foreach ($folder in @([Environment]::GetFolderPath('CommonPrograms'),[Environment]::GetFolderPath('CommonStartup'))) { $path=Join-Path $folder 'BrrpBrrp.lnk'; if (Test-Path -LiteralPath $path) { $link=$shell.CreateShortcut($path); if ($link.TargetPath -eq ` + quote(binaryPath) + ` -and $link.Arguments -eq 'tray') { Remove-Item -LiteralPath $path } } }`
	if err := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return err
	}
	for _, name := range []string{"desktop.json", "brrpbrrp.ico"} {
		if err := os.Remove(filepath.Join(filepath.Dir(binaryPath), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
