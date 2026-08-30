package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestAdminIdentitySyncPreservesExistingUser(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbURL := "sqlite://" + filepath.Join(dir, "clickclack.db")
	st, err := sqlitestore.Open(filepath.Join(dir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "person@example.com", Email: "Person@Example.com"})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(file, []byte(`{"profiles":[{"id":"profile-a","displayName":"Person","emails":["person@example.com"],"hasAvatar":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return admin([]string{"identity", "sync", "--db", dbURL, "--source", "https://control.example.com", "--file", file})
	})
	var report struct{ Linked, Updated int }
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Linked != 1 || report.Updated != 1 {
		t.Fatalf("unexpected sync report: %s (%v)", output, err)
	}
	got, err := st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Person" || got.AvatarURL != "https://control.example.com/api/users/profile-a/avatar" {
		t.Fatalf("profile was not reused: %#v", got)
	}
	// Once linked, profile identity survives email changes and repeated syncs.
	if err := os.WriteFile(file, []byte(`{"profiles":[{"id":"profile-a","displayName":"Updated","emails":[]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() error {
		return admin([]string{"identity", "sync", "--db", dbURL, "--source", "https://control.example.com/", "--file", file})
	})
	got, err = st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Updated" || got.AvatarURL != "https://control.example.com/api/users/profile-a/avatar" {
		t.Fatalf("linked identity was not reused: %#v", got)
	}
}

func TestAdminIdentitySyncValidatesBeforeOpeningDatabase(t *testing.T) {
	for _, body := range []string{`{"profiles":null}`, `{"profiles":[{"id":"x","emails":["not an email"]}]}`, `{"profiles":[]} trailing`, strings.Repeat(" ", (4<<20)+1)} {
		t.Run(body[:min(len(body), 35)], func(t *testing.T) {
			dir := t.TempDir()
			file, database := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "unopened.db")
			if err := os.WriteFile(file, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := admin([]string{"identity", "sync", "--db", "sqlite://" + database, "--source", "https://control.example.com", "--file", file}); err == nil {
				t.Fatal("expected invalid export to fail")
			}
			if _, err := os.Stat(database); !os.IsNotExist(err) {
				t.Fatalf("invalid input opened database: %v", err)
			}
		})
	}
}
