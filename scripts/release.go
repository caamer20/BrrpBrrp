//go:build ignore

package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	targets := flag.String("targets", "linux/amd64,linux/arm64,windows/amd64,windows/arm64,darwin/amd64,darwin/arm64", "comma-separated OS/architecture targets")
	version := flag.String("version", "0.4.0-alpha.9", "release version")
	noticesOnly := flag.String("notices", "", "write dependency notices to this file without building packages")
	sourceList := flag.Bool("source-list", false, "print the source-package contents (one path per line) without building anything")
	nativeList := flag.String("native-list", "", "print the documents a native package for this GOOS ships (one path per line) without building anything")
	flag.Parse()
	if *sourceList {
		// The same list the archive is built from, so a checker can validate it
		// without unpacking an artifact. Archive names always use forward slashes:
		// a caller parsing this list must not have to know which host produced it.
		for _, path := range dockerSourcePaths() {
			fmt.Println(filepath.ToSlash(path))
		}
		return
	}
	if *nativeList != "" {
		known := false
		for _, platform := range []string{"linux", "windows", "darwin", "macos"} {
			if *nativeList == platform {
				known = true
			}
		}
		if !known {
			fail(fmt.Errorf("unknown native platform %q", *nativeList))
		}
		for _, path := range nativeDocPaths() {
			fmt.Println(filepath.ToSlash(path))
		}
		return
	}
	if strings.ContainsAny(*version, " /\\\n\r") {
		fail(fmt.Errorf("invalid version"))
	}
	goBin := os.Getenv("BRRP_GO")
	if goBin == "" {
		goBin = "go"
	}
	notices := licenses(goBin, *version)
	if *noticesOnly != "" {
		fail(os.WriteFile(*noticesOnly, notices, 0644))
		return
	}
	fail(os.MkdirAll("dist/releases", 0755))
	sums := new(strings.Builder)
	for _, target := range strings.Split(*targets, ",") {
		platform, arch, ok := strings.Cut(target, "/")
		if !ok {
			fail(fmt.Errorf("invalid target %s", target))
		}
		stage, err := os.MkdirTemp("dist", "package-")
		fail(err)
		binary := "brrpbrrp"
		if platform == "windows" {
			binary += ".exe"
		}
		command := exec.Command(goBin, "build", "-trimpath", "-ldflags=-s -w -X brrpbrrp/internal/app.Version="+*version, "-o", filepath.Join(stage, binary), "./cmd/brrpbrrp")
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+platform, "GOARCH="+arch)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		fmt.Println("Building", target)
		fail(command.Run())
		name := fmt.Sprintf("brrpbrrp-%s-%s-%s.zip", *version, platform, arch)
		path := filepath.Join("dist/releases", name)
		file, err := os.Create(path)
		fail(err)
		archive := zip.NewWriter(file)
		add(archive, filepath.Join(stage, binary), binary, 0755)
		// The native document set is one function, so the checker can validate the same
		// files this loop ships instead of trusting a second copy of the list.
		for _, path := range nativeDocPaths() {
			mode := os.FileMode(0644)
			if strings.HasSuffix(path, ".sh") {
				mode = 0755
			}
			add(archive, path, filepath.ToSlash(path), mode)
		}
		scripts := platform
		if platform == "darwin" {
			scripts = "macos"
		}
		directory := filepath.Join("scripts", scripts)
		entries, err := os.ReadDir(directory)
		fail(err)
		for _, entry := range entries {
			if !entry.IsDir() && entry.Name() != "Setup.cmd" && entry.Name() != "Setup.command" {
				path := filepath.Join(directory, entry.Name())
				add(archive, path, filepath.ToSlash(path), 0755)
			}
		}
		if platform == "windows" {
			add(archive, "scripts/windows/Setup.cmd", "Setup.cmd", 0755)
		}
		if platform == "darwin" {
			add(archive, "scripts/macos/Setup.command", "Setup.command", 0755)
		}
		writer, err := archive.Create("THIRD_PARTY_NOTICES.txt")
		fail(err)
		_, err = writer.Write(notices)
		fail(err)
		fail(archive.Close())
		fail(file.Close())
		file, err = os.Open(path)
		fail(err)
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		fail(err)
		fail(file.Close())
		fmt.Fprintf(sums, "%x  %s\n", hash.Sum(nil), name)
		fail(os.RemoveAll(stage))
	}
	fail(os.WriteFile("dist/releases/SHA256SUMS", []byte(sums.String()), 0644))
	buildDockerSource(*version)
	fmt.Println("Native packages are in dist/releases; Docker source and checksum are in dist.")
}

func buildDockerSource(version string) {
	name := "brrpbrrp-docker-source-" + version + ".zip"
	path := filepath.Join("dist", name)
	file, err := os.Create(path)
	fail(err)
	archive := zip.NewWriter(file)
	for _, source := range dockerSourcePaths() {
		mode := os.FileMode(0644)
		if strings.HasSuffix(source, ".sh") || strings.HasSuffix(source, ".mjs") {
			mode = 0755
		}
		add(archive, source, filepath.ToSlash(source), mode)
	}
	fail(archive.Close())
	fail(file.Close())
	file, err = os.Open(path)
	fail(err)
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	fail(err)
	fail(file.Close())
	fail(os.WriteFile("dist/DOCKER-SOURCE-SHA256SUMS", []byte(fmt.Sprintf("%x  %s\n", hash.Sum(nil), name)), 0644))
}

// sourceSkipTrees are the documentation trees deliberately kept out of the
// distributable source archive. They record the operator's own hosts, network
// addresses and session details, and a public source package must not carry them.
// Everything else under docs/ is included by walking the tree, so a new document is
// packaged by default instead of being forgotten (completion review F-15).
var sourceSkipTrees = []string{
	"docs/next-release.md",
	"docs/tailscale-setup-legacy.md",
	"docs/windows-startup-fix.md",
	"docs/references",
}

// releaseDocPaths returns every document a released package ships: the repository's
// own top-level documents and the whole docs/ tree apart from the operator-specific
// trees. The native packages and the source package share it, so the two sets cannot
// disagree about what a shipped link resolves to, and a new document ships by default
// rather than by remembering to add it.
//
// scripts/check-source-package.mjs validates the relative links of exactly this set
// for each package: the native packages used to ship a short allowlist whose links
// pointed at documents they did not contain.
func releaseDocPaths() []string {
	paths := []string{
		"README.md", "CONTRIBUTING.md", "LICENSE", "SECURITY.md", "CLA.md", "CLA-SIGNATORIES.md",
		"THIRD-PARTY-NOTICES.md", ".env.example", "scripts/backup.sh", "scripts/restore.sh",
		// The project documents a release ships. The source package lists these again in
		// dockerSourcePaths, and this is the list a *binary* package stages: a document
		// added here and not there would make the two packages disagree about what is
		// shipped, which is what the packaging check compares.
		"CHANGELOG.md", "CODE_OF_CONDUCT.md", "SUPPORT.md",
	}
	for _, path := range docsTree() {
		if nativeSkipDocs[path] {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// nativeDocPaths is what a native package adds to its staging directory: the shared
// release documents. It is kept as a named function because the archive writer and
// `-native-list` must agree on the set.
func nativeDocPaths() []string {
	return releaseDocPaths()
}

// nativeSkipDocs are the repository-only documents a binary package does not ship.
// They are development records that address the source tree directly — the master
// plan links to internal/ and web/ files, and the audit ledgers link to both those
// and each other — so shipping them in a binary package would advertise links the
// consumer cannot follow. They remain in the source package, where the code they
// reference is present; the checker validates each set against its own contents.
var nativeSkipDocs = map[string]bool{
	"docs/v1-implementation-plan.md":                        true,
	"docs/v1-progress.md":                                   true,
	"docs/completion-review-and-next-steps-2026-09-24.md":   true,
	"docs/completion-review-2-and-next-steps-2026-09-24.md": true,
	"docs/completion-remediation-2026-09-24.md":             true,
}

// docsTree lists the distributable documentation. Every file type is included, and
// the skip list is the only exclusion, so a new document or diagram is packaged
// without a second list to update.
func docsTree() []string {
	paths := []string{}
	fail(filepath.WalkDir("docs", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, skip := range sourceSkipTrees {
			if path == skip {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() || path == "docs" {
			return nil
		}
		if entry.Name() == ".DS_Store" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("source package contains a non-regular file: %s", path)
		}
		paths = append(paths, path)
		return nil
	}))
	return paths
}

// nativeScriptDirs lists the per-platform script directories a native package ships
// in full. The source package contains them as well, because packaging a native
// release reads them at build time: a source archive that omitted them could not
// produce the release it is meant to reproduce (review R2-10).
var nativeScriptDirs = []string{"linux", "windows", "macos"}

// dockerSourcePaths returns the exact contents of the source package, in the order
// they are written. It is a function rather than an inline literal so that
// `-source-list` and the archive itself can never disagree, and so that
// scripts/check-source-package.mjs can validate what the package promises without
// unpacking a built artifact.
func dockerSourcePaths() []string {
	paths := []string{
		"Dockerfile", ".dockerignore", ".env.docker.example", "compose.yaml", "compose.turn.yaml", "compose.backup.yaml",
		"go.mod", "go.sum", "README.md", "CONTRIBUTING.md", "LICENSE", "CLA.md", "CLA-SIGNATORIES.md", "SECURITY.md", "THIRD-PARTY-NOTICES.md", "Makefile",
		"CHANGELOG.md", "CODE_OF_CONDUCT.md", "SUPPORT.md",
		".env.example", "scripts/backup.sh", "scripts/restore.sh", "scripts/baseline-releases.json",
	}
	// The workflows are shipped so a consumer can see and reproduce the checks the
	// source is expected to pass.
	for _, workflow := range []string{"check.yml", "docker.yml"} {
		paths = append(paths, filepath.Join(".github", "workflows", workflow))
	}
	for _, script := range []string{"release.go", "check-browser-steps.mjs", "check-docker.mjs", "check-docker-inputs.mjs", "check-package.mjs", "check-source-package.mjs", "check-doc-links.mjs", "check-schema-docs.mjs", "check-acceptance-matrix.mjs", "licenses.mjs", "prepare-preview.mjs", "upgrade-recovery.mjs", "upgrade-recovery-private.mjs", "upgrade-rollback.mjs", "upgrade-schema13-private.mjs", "schema13-seed.mjs", "seed-schema13-private.mjs", "capacity-fixture.mjs", "capacity-load.mjs", "race-shard.mjs", "fetch-baseline-release.mjs", "preview-chime.mjs", "backup-encrypted-rehearsal.mjs", "full-disk-rehearsal.mjs", "probe.mjs", "rehearsal-kit.mjs"} {
		paths = append(paths, filepath.Join("scripts", script))
	}
	// The per-platform release scripts are a transitive dependency of `make release`:
	// the native packages read scripts/<platform> at build time, so a source archive
	// without them cannot reproduce a release. They were the omission review R2-10
	// found, which the old checker could not see because it only checked its own rules.
	for _, platform := range nativeScriptDirs {
		directory := filepath.Join("scripts", platform)
		entries, err := os.ReadDir(directory)
		if err != nil {
			fail(fmt.Errorf("read %s: %w (the source package must contain the platform release scripts)", directory, err))
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == ".DS_Store" {
				continue
			}
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	// docs/ is walked once, including docs/adr/: the skip list above is the only
	// exclusion, so a new document ships by default rather than by remembering.
	paths = append(paths, docsTree()...)
	for _, root := range []string{"cmd", "internal", "web"} {
		fail(filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path == filepath.Join("web", "node_modules") || path == filepath.Join("web", "dist") || path == filepath.Join("web", "test-results") || path == filepath.Join("web", "playwright-report") {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == ".DS_Store" {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("source package contains a non-regular file: %s", path)
			}
			paths = append(paths, path)
			return nil
		}))
	}
	sort.Strings(paths)
	unique := paths[:0]
	for index, path := range paths {
		if index == 0 || path != paths[index-1] {
			unique = append(unique, path)
		}
	}
	return unique
}

func add(archive *zip.Writer, path, name string, mode os.FileMode) {
	file, err := os.Open(path)
	fail(err)
	defer file.Close()
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	writer, err := archive.CreateHeader(header)
	fail(err)
	_, err = io.Copy(writer, file)
	fail(err)
}

// licenses returns the third-party notices for everything a release redistributes.
// scripts/licenses.mjs is the single source of truth: it reads the module set the
// compiler actually links into the binary, the packages the web bundle contains and
// the standard library, and it fails rather than ship a component whose license it
// could not read. The same script writes the repository's THIRD-PARTY-NOTICES.md.
func licenses(goBin, version string) []byte {
	command := exec.Command("node", "scripts/licenses.mjs", "./cmd/brrpbrrp", "-", "--version", version)
	// The notices go into the archive; the SBOM is written beside the notices file so
	// both come from the same discovered component set.
	sbomPath := "dist/releases/THIRD-PARTY-SBOM.json"
	if err := os.MkdirAll(filepath.Dir(sbomPath), 0755); err != nil {
		fail(err)
	}
	command.Env = append(os.Environ(), "BRRP_GO="+goBin, "BRRP_SBOM_PATH="+sbomPath)
	command.Stderr = os.Stderr
	data, err := command.Output()
	if err != nil {
		fail(fmt.Errorf("dependency notices: %w (the generator needs Node.js and a populated Go module cache)", err))
	}
	return data
}
func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
