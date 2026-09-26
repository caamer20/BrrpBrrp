package db

import (
	"path/filepath"
	"testing"
)

func TestSlowModeMigrationPreservesHistoryAndIndependentClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-slow-mode.db")
	if err := CreateSchemaFixture(path, 7); err != nil {
		t.Fatal(err)
	}
	before, err := OpenWithoutMigrating(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('member','member','hash','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO channels(id,name,type,created_at,updated_at) VALUES('chat','chat','text','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO messages(id,channel_id,user_id,content,idempotency_key,created_at) VALUES('message','chat','member','preserve this','existing-operation','2026-01-01T01:00:00Z')`,
	} {
		if _, err := before.Exec(query); err != nil {
			before.Close()
			t.Fatal(err)
		}
	}
	if err := before.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var seconds int
	var content, last string
	if err := store.QueryRow(`SELECT c.slow_mode_seconds,m.content,s.last_sent_at FROM channels c JOIN messages m ON m.channel_id=c.id JOIN channel_slow_mode_sends s ON s.channel_id=c.id AND s.user_id=m.user_id WHERE c.id='chat'`).Scan(&seconds, &content, &last); err != nil {
		t.Fatal(err)
	}
	if seconds != 0 || content != "preserve this" || last != "2026-01-01T01:00:00Z" {
		t.Fatalf("migration changed existing behavior/history: seconds=%d content=%q last=%q", seconds, content, last)
	}
	for _, seconds := range []int{-1, 21601} {
		if _, err := store.Exec(`UPDATE channels SET slow_mode_seconds=? WHERE id='chat'`, seconds); err == nil {
			t.Fatalf("storage accepted invalid interval %d", seconds)
		}
	}
	if _, err := store.Exec(`DELETE FROM messages WHERE id='message'`); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT last_sent_at FROM channel_slow_mode_sends WHERE channel_id='chat' AND user_id='member'`).Scan(&last); err != nil {
		t.Fatalf("message deletion reset the independent cooldown: %v", err)
	}
	if _, err := store.Exec(`DELETE FROM channels WHERE id='chat'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.QueryRow(`SELECT count(*) FROM channel_slow_mode_sends`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("channel deletion leaked send-clock rows: %d %v", count, err)
	}
}
