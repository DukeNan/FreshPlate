package logic

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"freshplate/server/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func setupService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := newService(st, newPasswords(8, 1))
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	return s, context.Background()
}
func loginSession(t *testing.T, s *Service, ctx context.Context, name, password string) store.Session {
	t.Helper()
	_, raw, err := s.Login(ctx, name, password)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Store.ResolveSession(ctx, hash, s.Now())
	if err != nil || session == nil {
		t.Fatal(session, err)
	}
	return *session
}
func TestAUTH001AndSES002Login(t *testing.T) {
	s, ctx := setupService(t)
	u, err := s.Register(ctx, " Alice_01 ", "alice password!")
	if err != nil || u.Username != "alice_01" || u.Role != "user" {
		t.Fatal(u, err)
	}
	before := s.Passwords.Metrics().Verifications
	for _, v := range []struct{ name, password string }{{"nobody", "alice password!"}, {"alice_01", "wrong password!"}, {"not-a-valid-name", "alice password!"}} {
		if _, _, err := s.Login(ctx, v.name, v.password); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	if s.Passwords.Metrics().Verifications-before != 3 {
		t.Fatal("unknown username did not verify dummy")
	}
	first := loginSession(t, s, ctx, "Alice_01", "alice password!")
	second := loginSession(t, s, ctx, "alice_01", "alice password!")
	if string(first.TokenHash) == string(second.TokenHash) || first.ExpiresAt.Sub(s.Now()) != 7*24*time.Hour {
		t.Fatal("session uniqueness/expiry")
	}
	if _, err = s.Store.DB.Exec("UPDATE users SET status='disabled' WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Login(ctx, "alice_01", "alice password!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
}
func TestSES005ChangePassword(t *testing.T) {
	s, ctx := setupService(t)
	_, err := s.Register(ctx, "alice", "alice password!")
	if err != nil {
		t.Fatal(err)
	}
	first := loginSession(t, s, ctx, "alice", "alice password!")
	second := loginSession(t, s, ctx, "alice", "alice password!")
	if err = s.ChangePassword(ctx, first, "wrong password!", "new password!"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, first, "alice password!", "alice password!"); !errors.Is(err, ErrSamePassword) {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, first, "alice password!", "new password!"); err != nil {
		t.Fatal(err)
	}
	for _, session := range []store.Session{first, second} {
		if actual, err := s.Store.ResolveSession(ctx, session.TokenHash, s.Now()); err != nil || actual != nil {
			t.Fatal(actual, err)
		}
	}
	if _, _, err = s.Login(ctx, "alice", "alice password!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	current := loginSession(t, s, ctx, "alice", "new password!")
	if current.User.AuthVersion != first.User.AuthVersion+1 {
		t.Fatal("version not advanced")
	}
}
func TestADM002AndCLI001And002Recovery(t *testing.T) {
	s, ctx := setupService(t)
	admin, err := s.InitAdmin(ctx, " ADMIN ", "admin password!")
	if err != nil || admin.Username != "admin" {
		t.Fatal(admin, err)
	}
	if _, err = s.InitAdmin(ctx, "other_admin", "admin password!"); err == nil {
		t.Fatal("duplicate init")
	}
	u, err := s.Register(ctx, "alice", "alice password!")
	if err != nil {
		t.Fatal(err)
	}
	actor := loginSession(t, s, ctx, "admin", "admin password!")
	if err = s.ResetPassword(ctx, actor, admin.ID, "temporary password!"); err == nil {
		t.Fatal("self reset allowed")
	}
	if err = s.ResetPassword(ctx, actor, u.ID, "alice password!"); !errors.Is(err, ErrSamePassword) {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, actor, u.ID, "temporary password!"); err != nil {
		t.Fatal(err)
	}
	target := loginSession(t, s, ctx, "alice", "temporary password!")
	if !target.User.MustChangePassword {
		t.Fatal("missing forced change")
	}
	if err = s.ChangePassword(ctx, target, "temporary password!", "temporary password!"); !errors.Is(err, ErrSamePassword) {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, target, "temporary password!", "next password!"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverAdmin(ctx, " ADMIN ", "recovered password!"); err != nil {
		t.Fatal(err)
	}
	if actual, err := s.Store.ResolveSession(ctx, actor.TokenHash, s.Now()); err != nil || actual != nil {
		t.Fatal("old admin session remains", err)
	}
	if err = s.RecoverAdmin(ctx, "alice", "recovered password!"); err == nil {
		t.Fatal("recovered ordinary user")
	}
	recovered := loginSession(t, s, ctx, "admin", "recovered password!")
	if !recovered.User.MustChangePassword {
		t.Fatal("recovery bypassed change")
	}
	if _, err = s.Store.DB.Exec("UPDATE users SET status='disabled' WHERE id=?", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverAdmin(ctx, "admin", "different password!"); err == nil {
		t.Fatal("recovered disabled admin")
	}
}
func TestSES002TokenHash(t *testing.T) {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(i)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes)
	want := sha256.Sum256(bytes)
	got, err := HashToken(raw)
	if err != nil || string(got) != string(want[:]) {
		t.Fatal(got, err)
	}
	for _, v := range []string{"", raw + "=", raw[:42], raw[:42] + "!", raw[:42] + "B"} {
		if _, err := HashToken(v); err == nil {
			t.Fatal("accepted invalid token", v)
		}
	}
}
