package personalusage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestTitleResolver(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, "codex with spaces")
	claude := filepath.Join(home, "claude")
	if err := os.MkdirAll(codex, 0700); err != nil {
		t.Fatal(err)
	}
	id := "01a1c88f-8e4e-78d1-852e-e9f46ffcd6c6"
	claudeID := "090bbee6-cc94-4c3a-b856-80c3bf0c4ef0"
	missing := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(`{"id":"`+id+`","thread_name":"Old index title"}`+"\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE threads (id TEXT PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO threads VALUES (?, ?)", id, "最新名称"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(claude, "projects", "test-project", claudeID)
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "custom-title.json"), []byte(`{"customTitle":"Claude 名称"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := &TitleResolver{CodexHome: codex, ClaudeHome: claude}
	got := r.Resolve(context.Background(), []string{id, claudeID, missing, "../../secret", "*"})
	if got[id] != "最新名称" || got[claudeID] != "Claude 名称" || got[missing] != "" || len(got) != 3 {
		t.Fatalf("unexpected title resolution: %#v", got)
	}
	if _, err := db.Exec("UPDATE threads SET title = ? WHERE id = ?", "Renamed", id); err != nil {
		t.Fatal(err)
	}
	if r.Resolve(context.Background(), []string{id})[id] != "Renamed" {
		t.Fatal("rename should be visible immediately")
	}
	if _, err := db.Exec("ALTER TABLE threads ADD COLUMN name TEXT"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE threads SET name = ? WHERE id = ?", "Desktop rename", id); err != nil {
		t.Fatal(err)
	}
	if r.Resolve(context.Background(), []string{id})[id] != "Desktop rename" {
		t.Fatal("desktop name must take precedence over original title")
	}
	if _, err := db.Exec("UPDATE threads SET name = ' ' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if r.Resolve(context.Background(), []string{id})[id] != "Renamed" {
		t.Fatal("empty desktop name must fall back to title")
	}
}

func TestTitleResolverFallbackAndSanitization(t *testing.T) {
	home := t.TempDir()
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"id":"`+id+`","thread_name":"Index title"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := &TitleResolver{CodexHome: home, ClaudeHome: home}
	if r.Resolve(context.Background(), []string{id})[id] != "Index title" {
		t.Fatal("index fallback failed")
	}
	if cleanTitle("  hello\nworld\x00 ") != "hello world" {
		t.Fatal("control characters must be sanitized")
	}
}
