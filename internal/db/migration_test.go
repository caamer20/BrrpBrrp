package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// alphaFixture builds a database exactly as release 0.4.0-alpha.3 leaves it:
// schema 2, with the identities, conversation history, files, replies, pins,
// reactions, sessions, bans and non-ASCII names that a real installation
// contains. Tests upgrade this fixture rather than a freshly created database so
// the compatibility promises in the plan are actually exercised.
func alphaFixture(t *testing.T, path string) {
	t.Helper()
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(schema); err != nil {
		t.Fatalf("baseline schema: %v", err)
	}
	if _, err := database.Exec(migration2); err != nil {
		t.Fatalf("migration 2: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO schema_migrations(version) VALUES(1),(2)`,
		`INSERT INTO users(id,username,display_name,password_hash,is_admin,is_banned,is_deactivated,avatar_color,created_at,updated_at) VALUES
            ('u-ownerless-admin','alex','Alex','hash',1,0,0,'#89a8f4','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('u-second-admin','sam','Sam','hash',1,0,0,'#e9af7e','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('u-member','renée','Renée Ünïcode','hash',0,0,0,'#b3d693','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('u-banned','spam','Spam','hash',0,1,0,'#c5a4e8','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('u-deactivated','gone','Gone','hash',0,0,1,'#7ac5c5','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z')`,
		`INSERT INTO channels(id,name,type,category,position,topic,max_voice_users,created_at,updated_at) VALUES
            ('c-general','general','text','Gespräche',0,'The place for everything',8,'2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('c-random','random','text','Gespräche',1,'','8','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('c-vault','vault','text','Intern',2,'Private already?','8','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z'),
            ('c-lounge','Lounge','voice','',3,'Drop in','4','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z')`,
		`INSERT INTO messages(id,channel_id,user_id,content,created_at,idempotency_key,reply_to_id) VALUES
            ('018f0000-0000-7000-8000-000000000001','c-general','u-ownerless-admin','héllo wörld','2026-02-01T10:00:00.000000000Z','key-1',NULL),
            ('018f0000-0000-7000-8000-000000000002','c-general','u-member','a reply about wörld','2026-02-01T10:01:00.000000000Z','key-2','018f0000-0000-7000-8000-000000000001'),
            ('018f0000-0000-7000-8000-000000000003','c-vault','u-ownerless-admin','secret note','2026-02-01T10:02:00.000000000Z','key-3',NULL)`,
		`INSERT INTO message_pins(message_id,pinned_by,created_at) VALUES('018f0000-0000-7000-8000-000000000001','u-ownerless-admin','2026-02-01T10:03:00.000000000Z')`,
		`INSERT INTO reactions(message_id,user_id,emoji,created_at) VALUES('018f0000-0000-7000-8000-000000000001','u-member','🎉','2026-02-01T10:04:00.000000000Z')`,
		`INSERT INTO attachments(id,message_id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES
            ('a-1','018f0000-0000-7000-8000-000000000001','u-ownerless-admin','café menu.pdf','11111111-1111-7111-8111-111111111111',4096,'application/pdf','2026-02-01T10:00:00.000000000Z')`,
		`INSERT INTO mentions(message_id,channel_id,target_user_id,actor_id,created_at) VALUES('018f0000-0000-7000-8000-000000000002','c-general','u-member','u-member','2026-02-01T10:01:00.000000000Z')`,
		`INSERT INTO channel_reads(user_id,channel_id,last_read_message_id,last_read_at) VALUES('u-member','c-general','018f0000-0000-7000-8000-000000000001','2026-02-01T10:05:00.000000000Z')`,
		`INSERT INTO sessions(id,user_id,token_hash,created_at,expires_at) VALUES('s-live','u-member','live-hash','2026-02-01T00:00:00.000000000Z','2099-01-01T00:00:00.000000000Z')`,
		`INSERT INTO invitations(id,token_hash,created_by,created_at,expires_at) VALUES('inv-1','invite-hash','u-ownerless-admin','2026-02-01T00:00:00.000000000Z','2099-01-01T00:00:00.000000000Z')`,
		`INSERT INTO user_mfa(user_id,secret,last_counter) VALUES('u-ownerless-admin','JBSWY3DPEHPK3PXP',7)`,
		`INSERT INTO settings(key,value,updated_at) VALUES('setup_complete','true','2026-01-01T00:00:00.000000000Z')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("fixture statement failed: %v", err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectNeverMigratesOrCreates(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	report, err := Inspect(missing)
	if err != nil {
		t.Fatal(err)
	}
	if report.Exists || report.SchemaVersion != 0 || !report.UpgradeRequired {
		t.Fatalf("missing database report = %+v", report)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created a database file")
	}

	path := filepath.Join(t.TempDir(), "alpha.db")
	alphaFixture(t, path)
	report, err = Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Exists || report.SchemaVersion != 2 || report.SupportedVersion != SchemaVersion {
		t.Fatalf("alpha report = %+v", report)
	}
	if !report.UpgradeRequired || report.FutureSchema {
		t.Fatalf("alpha upgrade state = %+v", report)
	}
	if len(report.PendingMigrations) != len(PendingMigrations(2)) || !strings.Contains(report.PendingMigrations[0], "3 ") {
		t.Fatalf("pending migrations = %v", report.PendingMigrations)
	}
	if report.Counts["users"] != 5 || report.Counts["channels"] != 4 || report.Counts["messages"] != 3 {
		t.Fatalf("counts = %v", report.Counts)
	}
	// Preflight must leave the recorded version untouched.
	again, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.SchemaVersion != 2 || len(again.RecordedVersions) != 2 {
		t.Fatalf("inspection migrated the database: %+v", again)
	}
}

func TestUpgradeFromAlphaPreservesWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha.db")
	alphaFixture(t, path)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := maxVersion(t, store); got != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion)
	}
	// Identities and non-ASCII names survive.
	var username, display string
	if err := store.QueryRow(`SELECT username,display_name FROM users WHERE id='u-member'`).Scan(&username, &display); err != nil {
		t.Fatal(err)
	}
	if username != "renée" || display != "Renée Ünïcode" {
		t.Fatalf("identity changed: %q %q", username, display)
	}
	// Channels keep IDs, order, names, history links and their reachability.
	rows, err := store.Query(`SELECT id,name,category,position,visibility,posting FROM channels ORDER BY position`)
	if err != nil {
		t.Fatal(err)
	}
	type channelRow struct {
		id, name, category, visibility, posting string
		position                                int
	}
	stored := make([]channelRow, 0, 4)
	for rows.Next() {
		var row channelRow
		if err := rows.Scan(&row.id, &row.name, &row.category, &row.position, &row.visibility, &row.posting); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, row)
	}
	rows.Close()
	want := []channelRow{
		{"c-general", "general", "Gespräche", "public", "members", 0},
		{"c-random", "random", "Gespräche", "public", "members", 1},
		{"c-vault", "vault", "Intern", "public", "members", 2},
		{"c-lounge", "Lounge", "", "public", "members", 3},
	}
	if len(stored) != 4 {
		t.Fatalf("channels = %v", stored)
	}
	for i, row := range stored {
		if row != want[i] {
			t.Fatalf("channel %d changed: %+v, want %+v", i, row, want[i])
		}
	}
	// Search, replies, pins, reactions and attachments still work.
	var found int
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'wörld'`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 2 {
		t.Fatalf("search hits after upgrade = %d, want 2", found)
	}
	var replies, pins, reactions, attachments int
	store.QueryRow(`SELECT count(*) FROM messages WHERE reply_to_id IS NOT NULL`).Scan(&replies)
	store.QueryRow(`SELECT count(*) FROM message_pins`).Scan(&pins)
	store.QueryRow(`SELECT count(*) FROM reactions`).Scan(&reactions)
	store.QueryRow(`SELECT count(*) FROM attachments WHERE message_id IS NOT NULL`).Scan(&attachments)
	if replies != 1 || pins != 1 || reactions != 1 || attachments != 1 {
		t.Fatalf("history changed: replies=%d pins=%d reactions=%d attachments=%d", replies, pins, reactions, attachments)
	}
	// Durable categories are created from the previous free-text names, once each.
	categories, err := store.Categories(context.Background(), store, ViewerFor("", true))
	if err != nil {
		t.Fatal(err)
	}
	if len(categories) != 2 {
		t.Fatalf("categories = %+v", categories)
	}
	if categories[0].Name != "Gespräche" || categories[0].Position != 0 || categories[0].Channels != 2 {
		t.Fatalf("first category = %+v", categories[0])
	}
	if categories[1].Name != "Intern" || categories[1].Position != 2 || categories[1].Channels != 1 {
		t.Fatalf("second category = %+v", categories[1])
	}
	// Administrators keep authority; ordinary members gain nothing privileged.
	roles, err := store.Roles(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 3 {
		t.Fatalf("seeded roles = %+v", roles)
	}
	adminRoles, err := store.AssignedRoleIDs(context.Background(), store, "u-ownerless-admin")
	if err != nil || len(adminRoles) != 1 || adminRoles[0] != "role-admin" {
		t.Fatalf("administrator roles = %v (%v)", adminRoles, err)
	}
	memberRoles, err := store.AssignedRoleIDs(context.Background(), store, "u-member")
	if err != nil || len(memberRoles) != 1 || memberRoles[0] != "role-member" {
		t.Fatalf("member roles = %v (%v)", memberRoles, err)
	}
	// Ownership is deliberately not inferred from the oldest administrator.
	if owners, err := store.OwnerCount(context.Background(), store); err != nil || owners != 0 {
		t.Fatalf("owners = %d (%v)", owners, err)
	}
	if revision, err := store.AuthorizationRevision(context.Background(), store); err != nil || revision != 1 {
		t.Fatalf("authorization revision = %d (%v)", revision, err)
	}
	// Sessions, invitations, mfa and bans are untouched.
	var sessions, mfa int
	store.QueryRow(`SELECT count(*) FROM sessions`).Scan(&sessions)
	store.QueryRow(`SELECT count(*) FROM user_mfa`).Scan(&mfa)
	if sessions != 1 || mfa != 1 {
		t.Fatalf("sessions=%d mfa=%d", sessions, mfa)
	}

	// Schema 6 (ADR 0010 / M-C): every channel has exactly one conversation whose id is the
	// channel id, every message resolves its conversation scope, and every message has a
	// deletion-safe receipt. The backfill updates columns; it neither recreates nor renumbers a
	// row, which is what the identity and search assertions above prove.
	var conversations, mismatched int
	store.QueryRow(`SELECT count(*) FROM conversations WHERE kind='channel' AND channel_id=id`).Scan(&conversations)
	if conversations != len(want) {
		t.Fatalf("channel conversations = %d, want %d", conversations, len(want))
	}
	store.QueryRow(`SELECT count(*) FROM messages m WHERE m.conversation_id IS NULL`).Scan(&mismatched)
	if mismatched != 0 {
		t.Fatalf("%d messages have no conversation scope after the migration", mismatched)
	}
	store.QueryRow(`SELECT count(*) FROM messages m JOIN conversations c ON c.id=m.channel_id WHERE m.conversation_id <> c.id`).Scan(&mismatched)
	if mismatched != 0 {
		t.Fatalf("%d messages resolve to a conversation other than their channel", mismatched)
	}
	var receipts, orphanReceipts int
	store.QueryRow(`SELECT count(*) FROM message_operations WHERE result_message_id IS NOT NULL`).Scan(&receipts)
	store.QueryRow(`SELECT count(*) FROM messages m WHERE NOT EXISTS(SELECT 1 FROM message_operations o WHERE o.result_message_id=m.id)`).Scan(&orphanReceipts)
	if receipts == 0 || orphanReceipts != 0 {
		t.Fatalf("receipt backfill: %d receipts, %d messages without one", receipts, orphanReceipts)
	}
}

func TestFailedMigrationLeavesRecoverableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha.db")
	alphaFixture(t, path)
	original := Migrations
	Migrations = append(append([]Migration{}, original...), Migration{Version: SchemaVersion + 1, Name: "deliberately broken", SQL: `ALTER TABLE channels ADD COLUMN broken TEXT NOT NULL DEFAULT 'x'; CREATE TABLE broken_table(id INTEGER); THIS IS NOT SQL;`})
	defer func() { Migrations = original }()
	if _, err := Open(path); err == nil {
		t.Fatal("broken migration was accepted")
	}
	Migrations = original
	report, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 2 {
		t.Fatalf("failed migration advanced the schema to %d", report.SchemaVersion)
	}
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, table := range []string{"broken_table", "roles"} {
		var exists int
		if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 {
			t.Fatalf("interrupted migration left %s behind", table)
		}
	}
	// Reopening with the repaired registry completes the upgrade.
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	report, err = Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != SchemaVersion {
		t.Fatalf("retry left schema at %d", report.SchemaVersion)
	}
}

func TestRepeatedMigrationAndDowngradeRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repeated.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	future := SchemaVersion + 1
	if _, err := store.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, future); err != nil {
		t.Fatal(err)
	}
	err = store.Migrate(context.Background())
	var upgrade *UpgradeError
	if !errors.As(err, &upgrade) {
		t.Fatalf("future schema error = %v, want UpgradeError", err)
	}
	if !strings.Contains(upgrade.Error(), "refuses to downgrade") {
		t.Fatalf("downgrade message is not actionable: %s", upgrade.Error())
	}
	if _, err := Open(path); !errors.As(err, &upgrade) {
		t.Fatalf("open accepted a newer schema: %v", err)
	}
	report, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.FutureSchema || report.UpgradeRequired {
		t.Fatalf("future schema report = %+v", report)
	}
}

func TestVerifyDetectsGapsAndIndexDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gap.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(`DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(context.Background()); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("gap error = %v", err)
	}
	if _, err := store.Exec(`INSERT INTO schema_migrations(version) VALUES(2)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("restored history failed verification: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('u1','alex','hash','now','now')`,
		`INSERT INTO channels(id,name,type,created_at,updated_at) VALUES('c1','general','text','now','now')`,
		`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key) VALUES('018f0000-0000-7000-8000-00000000000a','c1','c1','u1','indexed text','now','key')`,
	} {
		if _, err := store.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("healthy database failed verification: %v", err)
	}
	if _, err := store.Exec(`DROP TRIGGER messages_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key) VALUES('018f0000-0000-7000-8000-00000000000b','c1','c1','u1','unindexed text','now','key2')`); err != nil {
		t.Fatal(err)
	}
	// A missing index entry is invisible to structural verification, so the
	// repair path is proved through search behaviour instead.
	var hits int
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'unindexed'`).Scan(&hits); err != nil || hits != 0 {
		t.Fatalf("unindexed message was searchable: %d %v", hits, err)
	}
	if err := store.RebuildSearchIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'unindexed'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatalf("rebuilt index does not find the message: %d %v", hits, err)
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("repaired database failed verification: %v", err)
	}
	var content string
	if err := store.QueryRow(`SELECT content FROM messages WHERE id='018f0000-0000-7000-8000-00000000000b'`).Scan(&content); err != nil || content != "unindexed text" {
		t.Fatalf("index repair changed message content: %q %v", content, err)
	}
	// An empty index is repaired from the surviving messages, not from memory.
	if _, err := store.Exec(`INSERT INTO message_fts(message_fts) VALUES('delete-all')`); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'indexed'`).Scan(&hits); err != nil || hits != 0 {
		t.Fatalf("cleared index still matched: %d %v", hits, err)
	}
	if err := store.RebuildSearchIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'indexed'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatalf("rebuilt index lost earlier history: %d %v", hits, err)
	}
}

func TestDurabilityPolicyIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durability.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var synchronous int
	if err := store.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 2 {
		t.Fatalf("default synchronous = %d, want 2 (FULL)", synchronous)
	}
	store.Close()
	relaxed, err := OpenWithOptions(path, Options{Synchronous: DurabilityNormal})
	if err != nil {
		t.Fatal(err)
	}
	defer relaxed.Close()
	if err := relaxed.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("NORMAL synchronous = %d, want 1", synchronous)
	}
	if _, err := OpenWithOptions(path, Options{Synchronous: "sometimes"}); err == nil {
		t.Fatal("unsupported durability mode was accepted")
	}
}

func maxVersion(t *testing.T, store *Store) int {
	t.Helper()
	if store == nil {
		return 0
	}
	var version int
	if err := store.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}
