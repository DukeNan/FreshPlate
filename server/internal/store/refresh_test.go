package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"freshplate/server/migrations"
	"path/filepath"
	"testing"
)

func TestRefreshRotationReplayCommitsRevocation(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "refresh_user", "user")
	ss := session(t, s, u)
	old := ss.TokenHash
	next := bytes.Repeat([]byte{32}, 32)
	ctx := context.Background()
	rotated, err := s.RotateRefresh(ctx, old, next, testNow)
	if err != nil || rotated == nil || !bytes.Equal(rotated.TokenHash, old) {
		t.Fatalf("rotate: %+v %v", rotated, err)
	}
	_, err = s.RotateRefresh(ctx, old, bytes.Repeat([]byte{33}, 32), testNow)
	var app *AppError
	if !errors.As(err, &app) || app.Code != "REFRESH_TOKEN_REUSED" {
		t.Fatalf("replay: %v", err)
	}
	if got, err := s.ResolveSession(ctx, old, testNow); err != nil || got != nil {
		t.Fatalf("replay revocation rolled back: %+v %v", got, err)
	}
	if got, err := s.ResolveRefresh(ctx, next, testNow); err != nil || got != nil {
		t.Fatalf("descendant survived: %+v %v", got, err)
	}
}
func TestRefreshFailedInsertDoesNotConsumeToken(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "rollback_user", "user")
	ss := session(t, s, u)
	if _, err := s.DB.Exec("CREATE TRIGGER fail_refresh BEFORE INSERT ON refresh_tokens BEGIN SELECT RAISE(ABORT, 'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateRefresh(context.Background(), ss.TokenHash, bytes.Repeat([]byte{41}, 32), testNow); err == nil {
		t.Fatal("expected insert failure")
	}
	var consumed int
	if err := s.DB.QueryRow("SELECT consumed FROM refresh_tokens WHERE token_hash=?", ss.TokenHash).Scan(&consumed); err != nil || consumed != 0 {
		t.Fatalf("consumed=%d %v", consumed, err)
	}
}

func TestConcurrentRefreshRevokesSessionOnReplay(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "concurrent_refresh", "user")
	ss := session(t, s, u)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := byte(50); i < 52; i++ {
		go func(value byte) {
			<-start
			_, err := s.RotateRefresh(context.Background(), ss.TokenHash, bytes.Repeat([]byte{value}, 32), testNow)
			results <- err
		}(i)
	}
	close(start)
	success, replay := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
			continue
		}
		var app *AppError
		if errors.As(err, &app) && app.Code == "REFRESH_TOKEN_REUSED" {
			replay++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || replay != 1 {
		t.Fatalf("success=%d replay=%d", success, replay)
	}
	if got, err := s.ResolveSession(context.Background(), ss.TokenHash, testNow); err != nil || got != nil {
		t.Fatalf("concurrent replay did not revoke: %+v %v", got, err)
	}
}
func TestUpgradeV1PreservesBusinessDataAndInvalidatesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := migrations.Files.ReadFile("001_users.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(script)
	for _, q := range []string{"PRAGMA foreign_keys=ON", "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, completed_at INTEGER NOT NULL)", string(script)} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO schema_migrations VALUES(1,?,0)", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES('legacy_user','hash','user',0,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO user_preferences(user_id,updated_at) VALUES(1,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sessions(token_hash,user_id,auth_version,created_at,expires_at) VALUES(?,1,1,0,9999999999)", bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for query, want := range map[string]int{"SELECT count(*) FROM users": 1, "SELECT count(*) FROM user_preferences": 1, "SELECT count(*) FROM sessions": 0, "SELECT count(*) FROM refresh_tokens": 0, "SELECT count(*) FROM schema_migrations": 2} {
		var got int
		if err = s.DB.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d want %d err %v", query, got, want, err)
		}
	}
}

func TestRefreshReplayDeleteFailureDoesNotPretendRevoked(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "revoke_failure", "user")
	ss := session(t, s, u)
	ctx := context.Background()
	if _, err := s.RotateRefresh(ctx, ss.TokenHash, bytes.Repeat([]byte{60}, 32), testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER fail_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'injected'); END"); err != nil {
		t.Fatal(err)
	}
	_, err := s.RotateRefresh(ctx, ss.TokenHash, bytes.Repeat([]byte{61}, 32), testNow)
	var app *AppError
	if err == nil || errors.As(err, &app) && app.Code == "REFRESH_TOKEN_REUSED" {
		t.Fatalf("revocation failure misreported: %v", err)
	}
	if got, err := s.ResolveSession(ctx, ss.TokenHash, testNow); err != nil || got == nil {
		t.Fatalf("failed revoke lost session: %+v %v", got, err)
	}
}
