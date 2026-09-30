package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func schemaTenHistory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema-ten.db")
	if err := CreateSchemaFixture(path, 10); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, statement := range []string{
		`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('u1','alice','hash','now','now'),('u2','bob','hash','now','now')`,
		`INSERT INTO channels(id,name,type,created_at,updated_at) VALUES('c1','general','text','now','now'),('c2','other','text','now','now')`,
		`INSERT INTO messages(rowid,id,conversation_id,channel_id,user_id,content,created_at,edited_at,idempotency_key,embed_json,embed_status,reply_to_id) VALUES(7,'m1',NULL,'c1','u1','alpha café evidence','2026-01-01',NULL,'k1','[]','done',NULL),(41,'m2','c1','c1','u2','reply evidence','2026-01-02','2026-01-03','k2','[{"url":"https://example.invalid"}]','done','m1'),(900,'m3','c2','c2','u1','other room','2026-01-03',NULL,'k3',NULL,'pending',NULL)`,
		`INSERT INTO attachments(id,message_id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES('a1','m1','u1','café.txt','stored-a1',123,'text/plain','now')`,
		`INSERT INTO reactions(message_id,user_id,emoji,created_at) VALUES('m1','u2','🎉','now')`,
		`INSERT INTO message_pins(message_id,pinned_by,created_at) VALUES('m1','u2','now')`,
		`INSERT INTO mentions(message_id,channel_id,target_user_id,actor_id,created_at) VALUES('m1','c1','u2','u1','now')`,
		`INSERT INTO channel_reads(user_id,channel_id,last_read_message_id,last_read_at) VALUES('u2','c1','m1','now')`,
		`INSERT INTO message_operations(account_id,idempotency_key,conversation_id,intent_digest,state,result_message_id,created_at,expires_at) VALUES('u1','k1','c1','digest1','committed','m1','2026-01-01','2026-01-02'),('u2','k2','c1','digest2','committed','m2','2026-01-02','2026-01-03'),('u1','spent','c1','digest-deleted','deleted',NULL,'2026-01-01','2026-01-02')`,
		`INSERT INTO message_reports(id,message_id,channel_id,reporter_id,author_id,reason,snapshot_text,created_at,updated_at,expires_at) VALUES('report1','m1','c1','u2','u1','retained reason','alpha café evidence','2026-01-01','2026-01-01','2099-01-01')`,
	} {
		if _, err := old.Exec(statement); err != nil {
			t.Fatalf("schema10 fixture: %v", err)
		}
	}
	return path
}

func historySnapshot(t *testing.T, q Queryer) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	for name, query := range map[string]string{
		"messages":    `SELECT rowid,id,COALESCE(conversation_id,channel_id),channel_id,user_id,content,created_at,edited_at,idempotency_key,embed_json,embed_status,reply_to_id FROM messages ORDER BY rowid`,
		"attachments": `SELECT id,message_id,uploader_id,file_name,stored_name,file_size,mime_type,created_at FROM attachments ORDER BY id`,
		"reactions":   `SELECT * FROM reactions ORDER BY message_id,user_id,emoji`,
		"pins":        `SELECT * FROM message_pins ORDER BY message_id`,
		"mentions":    `SELECT * FROM mentions ORDER BY message_id,target_user_id`,
		"reads":       `SELECT * FROM channel_reads ORDER BY user_id,channel_id`,
		"receipts":    `SELECT * FROM message_operations ORDER BY account_id,idempotency_key`,
		"reports":     `SELECT id,message_id,channel_id,reporter_id,author_id,reason,snapshot_text,status,review_note,reviewed_by,created_at,updated_at,expires_at FROM message_reports ORDER BY id`,
	} {
		rows, err := q.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		all := [][]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			all = append(all, values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[name] = string(encoded)
	}
	return snapshot
}

func sameHistory(t *testing.T, want map[string]string, store *Store) {
	t.Helper()
	for name, got := range historySnapshot(t, store) {
		if got != want[name] {
			t.Errorf("%s changed during table rebuild:\ngot %s\nwant%s", name, got, want[name])
		}
	}
}

func assertForeignKeys(t *testing.T, store *Store, want int) {
	t.Helper()
	var enabled int
	if err := store.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != want {
		t.Fatalf("foreign_keys=%d want%d: %v", enabled, want, err)
	}
}

func TestMessageStorageRebuildPreservesSchemaTenHistory(t *testing.T) {
	path := schemaTenHistory(t)
	before, err := OpenWithoutMigrating(path)
	if err != nil {
		t.Fatal(err)
	}
	want := historySnapshot(t, before)
	before.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sameHistory(t, want, store)
	assertForeignKeys(t, store, 1)
	if err := store.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	var channelRequired, conversationRequired int
	if err := store.QueryRow(`SELECT (SELECT "notnull" FROM pragma_table_info('messages') WHERE name='channel_id'),(SELECT "notnull" FROM pragma_table_info('messages') WHERE name='conversation_id')`).Scan(&channelRequired, &conversationRequired); err != nil {
		t.Fatal(err)
	}
	if channelRequired != 0 || conversationRequired != 1 {
		t.Fatalf("nullability channel=%d conversation=%d", channelRequired, conversationRequired)
	}
	var indexed int
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'evidence'`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("FTS lost rebuilt content: %d %v", indexed, err)
	}
	// Triggers still update search, dependent rows and deletion-safe receipts.
	if _, err := store.Exec(`UPDATE messages SET content='updated evidence' WHERE id='m2'`); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'updated'`).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("FTS update trigger: %d %v", indexed, err)
	}
	if _, err := store.Exec(`DELETE FROM messages WHERE id='m1'`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"attachments", "reactions", "message_pins", "mentions"} {
		var count int
		if err := store.QueryRow("SELECT count(*) FROM " + table + " WHERE message_id='m1'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s did not cascade after migration: %d %v", table, count, err)
		}
	}
	var reply sql.NullString
	if err := store.QueryRow(`SELECT reply_to_id FROM messages WHERE id='m2'`).Scan(&reply); err != nil || reply.Valid {
		t.Fatalf("reply cleanup: %v %v", reply, err)
	}
	receipt, err := store.Receipt(t.Context(), store, "u1", "k1")
	if err != nil || !receipt.Deleted() || receipt.State != "deleted" {
		t.Fatalf("receipt deletion trigger: %+v %v", receipt, err)
	}
	if err := store.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateMessageStorageRequiresOneValidConversationScope(t *testing.T) {
	store, err := Open(schemaTenHistory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(`INSERT INTO conversations(id,kind,created_at,updated_at) VALUES('d1','direct','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key) VALUES('private','d1',NULL,'u1','private words','now','private-key')`); err != nil {
		t.Fatal(err)
	}
	message, err := store.Message(t.Context(), "u1", "private")
	if err != nil || message.ChannelID != "" || message.ConversationID != "d1" {
		t.Fatalf("nullable channel scan: %+v %v", message, err)
	}
	for _, query := range []string{
		`INSERT INTO messages(id,channel_id,user_id,created_at,idempotency_key) VALUES('no-scope','c1','u1','now','bad1')`,
		`INSERT INTO messages(id,conversation_id,channel_id,user_id,created_at,idempotency_key) VALUES('false-community','d1','c1','u1','now','bad2')`,
		`INSERT INTO messages(id,conversation_id,channel_id,user_id,created_at,idempotency_key) VALUES('missing-projection','c1',NULL,'u1','now','bad3')`,
		`INSERT INTO messages(id,conversation_id,channel_id,user_id,created_at,idempotency_key) VALUES('missing-conversation','absent',NULL,'u1','now','bad4')`,
		`UPDATE messages SET conversation_id='d1' WHERE id='m1'`,
		`UPDATE conversations SET kind='direct',channel_id=NULL WHERE id='c1'`,
		`UPDATE conversations SET kind='channel',channel_id='c2' WHERE id='d1'`,
	} {
		if _, err := store.Exec(query); err == nil {
			t.Fatalf("invalid scope was accepted: %s", query)
		}
	}
	if err := store.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`DELETE FROM conversations WHERE id='d1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Message(t.Context(), "u1", "private"); err != sql.ErrNoRows {
		t.Fatalf("private message did not follow conversation deletion: %v", err)
	}
}

func TestMessageRebuildFailureRollsBackAndRestoresEnforcement(t *testing.T) {
	path := schemaTenHistory(t)
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{database}
	defer store.Close()
	want := historySnapshot(t, store)
	original := Migrations
	Migrations = append([]Migration(nil), original...)
	defer func() { Migrations = original }()
	for i := range Migrations {
		if Migrations[i].Version == 11 {
			Migrations[i].SQL += `; THIS IS NOT SQL;`
		}
	}
	if err := store.Migrate(t.Context()); err == nil {
		t.Fatal("broken rebuild succeeded")
	}
	sameHistory(t, want, store)
	assertForeignKeys(t, store, 1)
	if version := maxVersion(t, store); version != 10 {
		t.Fatalf("failed rebuild committed schema%d", version)
	}
	var replacement int
	if err := store.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='messages_new'`).Scan(&replacement); err != nil || replacement != 0 {
		t.Fatalf("partial replacement left behind: %d %v", replacement, err)
	}
	Migrations = original
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	sameHistory(t, want, store)
}

func TestCancelledMessageRebuildRestoresForeignKeysAndCanRetry(t *testing.T) {
	path := schemaTenHistory(t)
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{database}
	defer store.Close()
	want := historySnapshot(t, store)
	original := Migrations
	Migrations = append([]Migration(nil), original...)
	defer func() { Migrations = original }()
	for i := range Migrations {
		if Migrations[i].Version == 11 {
			Migrations[i].SQL += `; WITH RECURSIVE counter(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM counter WHERE n<1000000000) SELECT sum(n) FROM counter;`
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := store.Migrate(ctx); err == nil || ctx.Err() == nil {
		t.Fatalf("migration was not interrupted: %v", err)
	}
	assertForeignKeys(t, store, 1)
	sameHistory(t, want, store)
	if version := maxVersion(t, store); version != 10 {
		t.Fatalf("cancelled rebuild committed schema%d", version)
	}
	Migrations = original
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	sameHistory(t, want, store)
}

func TestCommittedMessageRebuildResumesOutstandingPostconditions(t *testing.T) {
	path := schemaTenHistory(t)
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{database}
	want := historySnapshot(t, store)
	outstanding, err := store.applySchemaMigrations(t.Context(), 0)
	if err != nil || !outstanding {
		t.Fatalf("schema commit: pending=%v err=%v", outstanding, err)
	}
	assertForeignKeys(t, store, 1)
	// Stop at the real schema/postcondition boundary, with a deliberately empty
	// derived index. A later open must rebuild it even with no pending versions.
	if _, err := store.Exec(`INSERT INTO message_fts(message_fts) VALUES('delete-all')`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sameHistory(t, want, reopened)
	if pending, err := reopened.UpgradePending(t.Context()); err != nil || pending {
		t.Fatalf("postconditions not completed: %v %v", pending, err)
	}
	var found int
	if err := reopened.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'evidence'`).Scan(&found); err != nil || found != 2 {
		t.Fatalf("reopen did not rebuild search: %d %v", found, err)
	}
}

func TestMessageRebuildRefusesPreexistingScopeCorruption(t *testing.T) {
	path := schemaTenHistory(t)
	old, err := OpenWithoutMigrating(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`UPDATE conversations SET kind='direct' WHERE id='c1'`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "conversation scopes") {
		t.Fatalf("corrupt channel projection migrated: %v", err)
	}
	report, err := Inspect(path)
	if err != nil || report.SchemaVersion != 10 {
		t.Fatalf("refused migration advanced version: %+v %v", report, err)
	}
}
