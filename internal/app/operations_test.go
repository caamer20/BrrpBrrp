package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataLockAndEnvironmentFile(t *testing.T) {
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = config.Prepare(); err != nil {
		t.Fatal(err)
	}
	first, err := LockData(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LockData(config)
	if err == nil {
		second.Close()
		t.Fatal("second process acquired data lock")
	}
	first.Close()
	next, err := LockData(config)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	t.Setenv("BRRP_LOG_LEVEL", "warn")
	path := filepath.Join(config.DataDir, "server.env")
	if err := os.WriteFile(path, []byte("# literal configuration\nBRRP_LOG_LEVEL=debug\nBRRP_TEST_LITERAL='$(no-execution)'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("BRRP_TEST_LITERAL")
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("BRRP_LOG_LEVEL") != "warn" || os.Getenv("BRRP_TEST_LITERAL") != "$(no-execution)" {
		t.Fatal("configuration precedence or literal parsing failed")
	}
}
