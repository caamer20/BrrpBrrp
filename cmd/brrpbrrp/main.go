package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"brrpbrrp/internal/api"
	"brrpbrrp/internal/app"
	"brrpbrrp/internal/backup"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/installer"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runPlatform(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "BrrpBrrp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "install" {
		flags := flag.NewFlagSet("install", flag.ContinueOnError)
		preview := flags.Bool("preview", false, "preview the wizard without installing or changing this computer")
		noBrowser := flags.Bool("no-browser", false, "print the local setup URL without opening a browser")
		if len(args) > 0 {
			args = args[1:]
		}
		if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
			return nil
		} else if err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected install arguments")
		}
		if !*preview {
			if launched, err := installer.Elevate(); launched || err != nil {
				return err
			}
		}
		return installer.Run(ctx, installer.Options{Preview: *preview, NoBrowser: *noBrowser})
	}
	command := "serve"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command == "version" {
		fmt.Println("BrrpBrrp", app.Version)
		return nil
	}
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Print(usage)
		return nil
	}
	switch command {
	case "serve", "healthcheck", "production-check", "backup", "restore", "verify-backup", "create-admin", "recover-owner", "setup-token", "generate-cert", "repair-cert", "service-install", "service-uninstall", "preflight", "migrate", "verify":
	default:
		return fmt.Errorf("unknown command %q; run brrpbrrp help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configFile := flags.String("config", "", "read literal environment settings from this file")
	dataDir := flags.String("data-dir", "", "data directory (overrides environment)")
	var out, source, username, scratchDir string
	var uploads, passwordStdin, verifyBackups bool
	var backupMaxAge, certMinValidity time.Duration
	var minFreeGiB uint
	var requireCopy bool
	switch command {
	case "production-check":
		flags.DurationVar(&backupMaxAge, "backup-max-age", 36*time.Hour, "maximum age of the newest complete backup and copy")
		flags.DurationVar(&certMinValidity, "cert-min-validity", 14*24*time.Hour, "minimum remaining HTTPS certificate lifetime")
		flags.UintVar(&minFreeGiB, "min-free-gib", 2, "minimum free data-disk space in GiB")
		flags.BoolVar(&requireCopy, "require-copy", true, "require a fresh extra backup copy")
		flags.BoolVar(&verifyBackups, "verify-backup", true, "verify the newest backup archive's manifest, digests and database")
		flags.StringVar(&scratchDir, "scratch-dir", "", "directory for the database a verification extracts (default: the data folder's scratch directory, not a small /tmp)")
	case "backup":
		flags.StringVar(&out, "out", "backups/backup-"+db.ID()+".zip", "output ZIP path inside the data directory")
		flags.BoolVar(&uploads, "include-uploads", true, "include attachment files")
	case "restore":
		flags.StringVar(&source, "file", "", "backup ZIP path inside a new data directory")
	case "verify-backup":
		flags.StringVar(&source, "file", "", "backup ZIP path to check without restoring it")
		flags.StringVar(&scratchDir, "scratch-dir", "", "directory for the extracted database; point this at real storage when /tmp is small")
	case "create-admin":
		flags.StringVar(&username, "username", "", "new administrator username")
		flags.BoolVar(&passwordStdin, "password-stdin", false, "read password from standard input without putting it in process arguments")
	case "recover-owner":
		flags.StringVar(&username, "username", "", "username of the current installation owner")
		flags.BoolVar(&passwordStdin, "password-stdin", false, "read the replacement password from standard input without putting it in process arguments")
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments; flags belong after the command")
	}
	if command == "production-check" && (backupMaxAge <= 0 || certMinValidity < 0 || minFreeGiB > 1024) {
		return errors.New("invalid production-check thresholds")
	}
	if *configFile != "" {
		if err := app.LoadEnvFile(*configFile); err != nil {
			return err
		}
	}
	if *dataDir != "" {
		if err := os.Setenv("BRRP_DATA_DIR", *dataDir); err != nil {
			return err
		}
	}
	if command == "verify-backup" {
		// Read-only and deliberately independent of the data directory: the copy an
		// operator wants checked usually lives somewhere else. Scratch storage is a
		// parameter because a container's temporary filesystem is often far too
		// small for a realistic database.
		if source == "" {
			return errors.New("verify-backup requires --file")
		}
		return verifyArchive(ctx, source, scratchDir)
	}
	if command == "service-install" || command == "service-uninstall" {
		return manageService(command, *configFile)
	}
	config, err := app.LoadConfig()
	if err != nil {
		return err
	}
	if command == "healthcheck" {
		return app.CheckHealth(ctx, config)
	}
	if command == "preflight" {
		return preflight(config)
	}
	if command == "production-check" {
		if scratchDir == "" {
			// The data folder is the storage the operator already sized for this
			// installation, so verification does not depend on a system temporary
			// filesystem that may be a small tmpfs.
			scratchDir = filepath.Join(config.DataDir, "scratch")
		}
		options := app.ProductionCheckOptions{
			BackupMaxAge: backupMaxAge, CertMinValidity: certMinValidity,
			MinFreeBytes: uint64(minFreeGiB) << 30, RequireCopy: requireCopy, Now: time.Now(),
		}
		if verifyBackups {
			// A monitor that only dates an archive cannot tell a readable backup from a
			// truncated one, so the recorded recovery generation and its copy are both
			// verified, including the checks a restore applies, before the result is
			// reported.
			options.VerifyArchive = func(ctx context.Context, path string) (app.ArchiveSummary, error) {
				manifest, digest, err := backup.VerifyArchiveIn(ctx, path, scratchDir)
				return app.ArchiveSummary{Digest: digest, IncludesUploads: manifest.IncludesUploads, SchemaVersion: manifest.SchemaVersion, Attachments: len(manifest.Attachments)}, err
			}
		}
		return app.CheckProduction(ctx, config, options)
	}
	if err = config.Prepare(); err != nil {
		return err
	}
	if command == "setup-token" {
		token, err := os.ReadFile(config.SetupTokenFile)
		if err != nil {
			return err
		}
		fmt.Println(string(token))
		return nil
	}
	lock, err := app.LockData(config)
	if err != nil {
		return err
	}
	defer lock.Close()
	if command == "repair-cert" {
		return app.RepairCertificate(config)
	}
	if command == "generate-cert" {
		return app.GenerateCert(config)
	}
	if command == "restore" {
		if source == "" {
			return errors.New("restore requires --file")
		}
		if err := backup.Restore(ctx, config, source); err != nil {
			return err
		}
		fmt.Println("Restored. All users must sign in again.")
		return nil
	}
	if command == "verify" {
		// Verify never migrates and never repairs. It reports the schema an
		// installation actually records, so it can check a data folder an earlier
		// release is still serving, or a snapshot taken before an upgrade, without
		// changing either. Applying schema changes is the migrate command's job, or
		// the server's when it starts.
		store, err := db.OpenWithoutMigrating(config.DBFile)
		if err != nil {
			return err
		}
		defer store.Close()
		return verifyDatabase(ctx, store, config)
	}
	if command == "backup" {
		// A backup is the installation as it is, so it is taken without migrating:
		// the snapshot an operator takes before an upgrade is the same command as
		// every other backup, and its manifest records the schema the copied database
		// itself has.
		store, err := db.OpenWithoutMigrating(config.DBFile)
		if err != nil {
			return err
		}
		defer store.Close()
		if err := backup.Create(ctx, store, config, out, uploads); err != nil {
			return err
		}
		path, _ := app.Contained(config.DataDir, out)
		fmt.Println("Backup saved:", path)
		return nil
	}
	store, err := db.OpenWithOptions(config.DBFile, db.Options{Synchronous: config.SQLiteSynchronous})
	if err != nil {
		return err
	}
	defer store.Close()
	switch command {
	case "migrate":
		// Migrate has already run as part of opening, because an upgraded binary
		// must reconcile the schema before it serves anything. The explicit command
		// repeats the check and states the result.
		if err := store.Verify(ctx); err != nil {
			return err
		}
		report, err := db.Inspect(config.DBFile)
		if err != nil {
			return err
		}
		fmt.Printf("Schema %d verified: %d users, %d channels, %d messages, %d attachments.\n",
			report.SchemaVersion, report.Counts["users"], report.Counts["channels"], report.Counts["messages"], report.Counts["attachments"])
		return nil
	case "create-admin":
		if !passwordStdin {
			return errors.New("create-admin requires --password-stdin; prefer first-time browser setup for normal installation")
		}
		password, err := passphrase()
		if err != nil {
			return err
		}
		if err := store.CreateAdministrator(ctx, username, password, config.MaxUploadBytes); err != nil {
			return err
		}
		if err := os.Remove(config.SetupTokenFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("Administrator created:", username)
		return nil
	case "recover-owner":
		if !passwordStdin {
			return errors.New("recover-owner requires --password-stdin; do not put a passphrase in command arguments or shell history")
		}
		password, err := passphrase()
		if err != nil {
			return err
		}
		recovery, err := store.RecoverOwner(ctx, username, password)
		if err != nil {
			return err
		}
		fmt.Println("Owner access restored:", recovery.Username)
		fmt.Println("  password replaced; every signed-in device was signed out")
		if recovery.FactorRemoved {
			fmt.Printf("  second factor removed, together with %d recovery codes\n", recovery.CodesRemoved)
		} else {
			fmt.Println("  no second factor was enrolled, so none was removed")
		}
		if recovery.Reactivated {
			fmt.Println("  the account had been deactivated and can sign in again")
		}
		if recovery.BanCleared {
			fmt.Println("  the account had been banned and the ban was lifted")
		}
		fmt.Println("Recorded in the audit log as owner.recovered_offline. Sign in and enroll a new authenticator, then keep the new passphrase and the codes somewhere safe.")
		return nil
	}
	logger, closer, err := app.Logger(config)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	server, err := api.New(config, store, logger)
	if err != nil {
		return err
	}
	defer server.Close()
	logger.Info("server version", "version", app.Version, "binary", filepath.Base(os.Args[0]))
	return app.Serve(ctx, config, server.Handler(), logger)
}

// passphrase reads a secret from standard input so it never appears in process
// arguments or shell history. One trailing newline is stripped.
func passphrase() (string, error) {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 75))
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"), nil
}

// preflight inspects a database before an operator authorizes an upgrade. It
// deliberately does not open the store, because opening it would migrate it.
func preflight(config app.Config) error {
	report, err := db.Inspect(config.DBFile)
	if err != nil {
		return err
	}
	free, freeErr := app.FreeDiskBytes(config.DataDir)
	payload := struct {
		db.Inspection
		Durability string `json:"configured_synchronous"`
		FreeBytes  uint64 `json:"free_bytes"`
	}{Inspection: report, Durability: config.SQLiteSynchronous, FreeBytes: free}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	if freeErr != nil {
		fmt.Fprintln(os.Stderr, "BrrpBrrp: free space could not be measured:", freeErr)
	}
	if report.FutureSchema {
		return fmt.Errorf("database schema %d is newer than this binary supports (schema %d); install the newer version again or restore a pre-upgrade backup", report.SchemaVersion, report.SupportedVersion)
	}
	return nil
}

// verifyDatabase reports the schema a data folder records and checks the
// invariants that are meaningful for that schema, without applying a migration or
// repairing anything. A check that changes what it checks is not a check, so a
// database an earlier release still serves is described rather than upgraded, and
// an upgrade that committed without finishing its postconditions is reported as
// unfinished instead of being treated as done.
func verifyDatabase(ctx context.Context, store *db.Store, config app.Config) error {
	version, err := store.SchemaVersionIn(ctx, store)
	if err != nil {
		return err
	}
	if version > db.SchemaVersion {
		return &db.UpgradeError{Version: version, Supported: db.SchemaVersion}
	}
	if pending, err := store.UpgradePending(ctx); err != nil {
		return err
	} else if pending {
		return fmt.Errorf("the data folder records schema %d but an upgrade to %d has not finished: its search-index rebuild and verification are outstanding. Run `brrpbrrp migrate`, or start the server, to complete and verify the upgrade before relying on this data", version, db.SchemaVersion)
	}
	if version < db.SchemaVersion {
		return fmt.Errorf("the data folder records schema %d and this binary serves %d. Verification never migrates: run `brrpbrrp migrate` to apply the pending migrations, or take a snapshot with `brrpbrrp backup` first, then verify again", version, db.SchemaVersion)
	}
	if err := store.Verify(ctx); err != nil {
		return err
	}
	report, err := db.Inspect(config.DBFile)
	if err != nil {
		return err
	}
	fmt.Printf("Schema %d verified: %d users, %d channels, %d messages, %d attachments. No migration was applied and nothing was repaired.\n",
		report.SchemaVersion, report.Counts["users"], report.Counts["channels"], report.Counts["messages"], report.Counts["attachments"])
	return nil
}

// verifyArchive checks a backup without restoring it, so an operator can prove a
// copy is readable and unmodified before it is needed. Nothing is written to the
// data directory.
func verifyArchive(ctx context.Context, path, scratchDir string) error {
	manifest, digest, err := backup.VerifyArchiveIn(ctx, path, scratchDir)
	if err != nil {
		return err
	}
	var attachments int64
	for _, attachment := range manifest.Attachments {
		attachments += attachment.Size
	}
	database := int64(0)
	if manifest.Database != nil {
		database = manifest.Database.Size
	}
	fmt.Println("Archive:", path)
	fmt.Printf("  format %d, created %s", manifest.Format, manifest.CreatedAt)
	if manifest.AppVersion != "" {
		fmt.Printf(", written by %s", manifest.AppVersion)
	}
	if manifest.SchemaVersion > 0 {
		fmt.Printf(", schema %d", manifest.SchemaVersion)
	}
	fmt.Println()
	fmt.Printf("  database %d bytes, %d attachments, %d attachment bytes\n", database, len(manifest.Attachments), attachments)
	if !manifest.IncludesUploads && len(manifest.Attachments) == 0 {
		fmt.Println("  this archive holds no attachment files")
	}
	fmt.Println("  sha256", digest)
	if manifest.Format < 2 {
		fmt.Println("Verified: structure, sizes, checksums and the database passed. This archive predates recorded digests, so per-file digests could not be compared.")
		return nil
	}
	fmt.Println("Verified: every entry matches its recorded size and digest, and the database passes its integrity and reference checks.")
	return nil
}

const usage = `BrrpBrrp — a small self-hosted community

  install                Open the local installation wizard (default command)
  serve                  Start a configured server
	  healthcheck            Check the running listener without locking its data
	  production-check       Check public HTTPS, certificate, disk and backups
  setup-token            Show the first-time setup token to the local operator
  create-admin           Create an administrator while the server is stopped
  recover-owner          Restore the owner's access while the server is stopped (offline break-glass)
  backup                 Create an offline backup of the data folder as it is, without migrating it
  restore                Restore into a new, empty data folder
  verify-backup          Check a backup archive without restoring it
  generate-cert          Generate a local CA and server certificate without overwriting keys
  repair-cert            Repair local TLS while stopped; keep the previous keys
  preflight              Report the database schema, size and pending migrations without changing it
  migrate                Apply pending migrations; verify refuses to continue if the result is unsound
  verify                 Check the recorded schema and invariants without migrating or repairing anything
  tray                   Open Windows notification-area server controls
  service-start          Start the Windows background service
  service-stop           Gracefully stop the Windows background service
  service-restart        Restart the Windows background service
  service-install        Install the Windows service (elevated terminal)
  service-uninstall      Remove the stopped Windows service; preserve all data
  version                Show the version

All server commands accept --config PATH and --data-dir PATH.
Run brrpbrrp COMMAND --help for command options.
`
