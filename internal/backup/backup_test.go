package backup

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/app"
)

func TestRejectInvalidArchives(t *testing.T) {
	for _, names := range [][]string{{"../escaped"}, {"manifest.json"}, {"manifest.json", "manifest.json"}, {"uploads/../../escaped"}, {"manifest.json", "database.sqlite"}} {
		t.Run(names[0], func(t *testing.T) {
			t.Setenv("BRRP_DATA_DIR", t.TempDir())
			config, err := app.LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if err = config.Prepare(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(config.DataDir, "invalid.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for _, name := range names {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				entry.Write([]byte(`{"format":1}`))
			}
			writer.Close()
			file.Close()
			if err := Restore(context.Background(), config, path); err == nil {
				t.Fatal("invalid archive accepted")
			}
			if _, err := os.Stat(config.DBFile); !os.IsNotExist(err) {
				t.Fatal("failed restore created database")
			}
		})
	}
}
