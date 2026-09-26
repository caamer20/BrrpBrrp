package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A release must have one identity. The same version string appears in the Go
// variable the binary and the backup manifest report, in the frontend package
// metadata the web build and the notices generator read, and as the default the
// release helper builds packages with. When these drift, an artifact exists under
// two names: the archive says one version, the manifest inside it says another, and
// a rollback cannot tell which binary produced a database. This test is the only
// thing that keeps them together (completion review F-15).
func TestVersionIdentityAgrees(t *testing.T) {
	root := filepath.Join("..", "..")

	var manifest struct {
		Version string `json:"version"`
	}
	data, err := os.ReadFile(filepath.Join(root, "web", "package.json"))
	if err != nil {
		t.Fatalf("read web/package.json: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse web/package.json: %v", err)
	}
	if manifest.Version != Version {
		t.Errorf("web/package.json declares %q while internal/app/version.go declares %q", manifest.Version, Version)
	}

	release, err := os.ReadFile(filepath.Join(root, "scripts", "release.go"))
	if err != nil {
		t.Fatalf("read scripts/release.go: %v", err)
	}
	match := regexp.MustCompile(`flag\.String\("version",\s*"([^"]+)"`).FindSubmatch(release)
	if match == nil {
		t.Fatal("scripts/release.go has no default for the version flag")
	}
	if string(match[1]) != Version {
		t.Errorf("scripts/release.go defaults to %q while internal/app/version.go declares %q", match[1], Version)
	}

	// The lockfile is generated from the manifest, so its two project records are
	// checked too: a stale lockfile version is a package metadata mismatch. Only
	// the project's own entries count — every dependency carries a version as well.
	lock, err := os.ReadFile(filepath.Join(root, "web", "package-lock.json"))
	if err != nil {
		t.Fatalf("read web/package-lock.json: %v", err)
	}
	var locked struct {
		Version  string `json:"version"`
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &locked); err != nil {
		t.Fatalf("parse web/package-lock.json: %v", err)
	}
	if locked.Version != Version {
		t.Errorf("web/package-lock.json declares %q while internal/app/version.go declares %q", locked.Version, Version)
	}
	if project, ok := locked.Packages[""]; ok && project.Version != Version {
		t.Errorf("web/package-lock.json declares the project as %q while internal/app/version.go declares %q", project.Version, Version)
	}
}

// The candidate under remediation must not still identify as the release it
// replaced: alpha.3 is the baseline the completion review examined, and an artifact
// that keeps that name cannot be told apart from it.
func TestTheCandidateIsNotTheReviewedBaseline(t *testing.T) {
	if Version == "0.4.0-alpha.3" {
		t.Fatal("the candidate still identifies as the reviewed baseline 0.4.0-alpha.3")
	}
	if Version == "" || regexp.MustCompile(`\s`).MatchString(Version) {
		t.Fatalf("version %q is not usable as release identity", Version)
	}
}
