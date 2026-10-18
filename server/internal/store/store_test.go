package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

func TestSEC004DatabaseAndSidecarPermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "private.db")
	for _, existing := range []bool{false, true} {
		if existing {
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		}
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			info, err := os.Stat(path + suffix)
			if err != nil {
				s.Close()
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				s.Close()
				t.Fatalf("existing=%v %s mode=%o", existing, suffix, info.Mode().Perm())
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func user(t *testing.T, s *Store, name, role string) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), name, "hash-original", role, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func session(t *testing.T, s *Store, u User) Session {
	t.Helper()
	h := bytes.Repeat([]byte{byte(u.ID)}, 32)
	_, err := s.IssueSession(context.Background(), u, h, testNow, testNow.Add(7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return Session{User: u, TokenHash: h, ExpiresAt: testNow.Add(7 * 24 * time.Hour)}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var e *AppError
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
func TestREG004AtomicDefaultsAndConcurrentUnique(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.DB.Exec("CREATE TRIGGER fail_preferences BEFORE INSERT ON user_preferences BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "alice", "hash", "user", testNow); err == nil {
		t.Fatal("expected injected failure")
	}
	if _, err := s.UserByName(ctx, "alice"); err != sql.ErrNoRows {
		t.Fatalf("partial registration: %v", err)
	}
	if _, err := s.DB.Exec("DROP TRIGGER fail_preferences"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.CreateUser(ctx, "alice", "hash", "user", testNow); errs <- err }()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		} else {
			code(t, err, "USERNAME_TAKEN")
		}
	}
	if ok != 1 {
		t.Fatalf("created %d users", ok)
	}
	u, _ := s.UserByName(ctx, "alice")
	p, err := s.Preferences(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Allergens) != 0 || len(p.ExcludedFoods) != 0 || p.DailyEnergyKcal != nil {
		t.Fatalf("bad defaults: %+v", p)
	}
}
func TestDB001PragmasConstraintsAndReconnection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if s.DB.Stats().MaxOpenConnections != 1 {
		t.Fatal("pool must have one connection")
	}
	for i := 0; i < 2; i++ {
		var fk, busy int
		var mode string
		if err := s.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}
		s.DB.QueryRow("PRAGMA busy_timeout").Scan(&busy)
		s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode)
		if fk != 1 || busy != 5000 || mode != "wal" {
			t.Fatalf("bad pragmas: %d %d %s", fk, busy, mode)
		}
		s.DB.SetMaxIdleConns(0)
		s.DB.SetMaxIdleConns(1)
	}
	u := user(t, s, "alice", "user")
	for _, q := range []string{fmt.Sprintf("UPDATE users SET role='owner' WHERE id=%d", u.ID), fmt.Sprintf("UPDATE users SET status='deleted' WHERE id=%d", u.ID), fmt.Sprintf("INSERT INTO sessions VALUES(X'00',%d,1,0,1)", u.ID), "INSERT INTO user_preferences(user_id,updated_at) VALUES(9999,0)", fmt.Sprintf("DELETE FROM users WHERE id=%d", u.ID)} {
		if _, err := s.DB.ExecContext(ctx, q); err == nil {
			t.Fatalf("constraint missing: %s", q)
		}
	}
}
func TestSESAndCON001PasswordRevocationAndStaleHash(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	if got, err := s.ResolveSession(ctx, ss.TokenHash, testNow.Add(7*24*time.Hour)); err != nil || got != nil {
		t.Fatalf("expiry: %v %v", got, err)
	}
	if err := s.ChangePassword(ctx, ss, u, "hash-new", testNow); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolveSession(ctx, ss.TokenHash, testNow); err != nil || got != nil {
		t.Fatalf("revoked session resolved: %v %v", got, err)
	}
	_, err := s.IssueSession(ctx, u, bytes.Repeat([]byte{9}, 32), testNow, testNow.Add(time.Hour))
	code(t, err, "INVALID_CREDENTIALS")
	code(t, s.ChangePassword(ctx, ss, u, "stale-overwrite", testNow), "AUTH_REQUIRED")
	now, _ := s.UserByID(ctx, u.ID)
	if now.PasswordHash != "hash-new" || now.AuthVersion != u.AuthVersion+1 {
		t.Fatalf("stale overwritten: %+v", now)
	}
}
func TestPREF003DisabledReferenceRetentionAndMissingSettings(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := user(t, s, "alice", "user")
	b := user(t, s, "bobby", "user")
	sa, sb := session(t, s, a), session(t, s, b)
	p, _ := s.Preferences(ctx, a.ID)
	p.ExcludedFoodIDs = []int64{1}
	if _, err := s.SavePreferences(ctx, sa, p, testNow); err != nil {
		t.Fatal(err)
	}
	s.DB.Exec("UPDATE foods SET status='disabled' WHERE id=1")
	got, err := s.SavePreferences(ctx, sa, p, testNow)
	if err != nil || len(got.ExcludedFoods) != 1 || got.ExcludedFoods[0].Status != "disabled" {
		t.Fatalf("lost disabled restriction: %+v %v", got, err)
	}
	_, err = s.SavePreferences(ctx, sb, p, testNow)
	code(t, err, "INVALID_ARGUMENT")
	p.ExcludedFoodIDs = []int64{}
	if _, err = s.SavePreferences(ctx, sa, p, testNow); err != nil {
		t.Fatal(err)
	}
	p.ExcludedFoodIDs = []int64{1}
	_, err = s.SavePreferences(ctx, sa, p, testNow)
	code(t, err, "INVALID_ARGUMENT")
	p.ExcludedFoodIDs = []int64{99999}
	_, err = s.SavePreferences(ctx, sa, p, testNow)
	code(t, err, "INVALID_ARGUMENT")
	s.DB.Exec("DELETE FROM user_preferences WHERE user_id=?", a.ID)
	if _, err = s.Preferences(ctx, a.ID); err == nil {
		t.Fatal("missing preferences silently allowed")
	}
}
func TestADM003ConcurrentAdminsCannotRemoveEachOther(t *testing.T) {
	for _, mode := range []string{"role", "status", "both"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			a := user(t, s, "admin_a", "admin")
			b := user(t, s, "admin_b", "admin")
			sa, sb := session(t, s, a), session(t, s, b)
			role, status := "user", "disabled"
			var rp, sp *string
			if mode != "status" {
				rp = &role
			}
			if mode != "role" {
				sp = &status
			}
			errs := make(chan error, 2)
			go func() { _, err := s.AdminUpdate(context.Background(), sa, b.ID, rp, sp, testNow); errs <- err }()
			go func() { _, err := s.AdminUpdate(context.Background(), sb, a.ID, rp, sp, testNow); errs <- err }()
			ok := 0
			for i := 0; i < 2; i++ {
				if <-errs == nil {
					ok++
				}
			}
			if ok != 1 {
				t.Fatalf("expected one winner, got %d", ok)
			}
			var n int
			s.DB.QueryRow("SELECT count(*) FROM users WHERE role='admin' AND status='active'").Scan(&n)
			if n != 1 {
				t.Fatalf("active admins: %d", n)
			}
		})
	}
}
func TestCON003And004ActorRevalidationAndAuditAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := user(t, s, "admin_a", "admin")
	b := user(t, s, "admin_b", "admin")
	u := user(t, s, "alice", "user")
	sa, sb, su := session(t, s, a), session(t, s, b), session(t, s, u)
	role := "user"
	if _, err := s.AdminUpdate(ctx, sb, a.ID, &role, nil, testNow); err != nil {
		t.Fatal(err)
	}
	role = "admin"
	_, err := s.AdminUpdate(ctx, sa, u.ID, &role, nil, testNow)
	code(t, err, "FORBIDDEN")
	if _, err = s.DB.Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.AdminReset(ctx, sb, u, "new-hash", testNow); err == nil {
		t.Fatal("audit failure not propagated")
	}
	actual, _ := s.UserByID(ctx, u.ID)
	if actual.PasswordHash != u.PasswordHash || actual.AuthVersion != u.AuthVersion || actual.MustChangePassword {
		t.Fatal("password mutation survived audit failure")
	}
	if got, _ := s.ResolveSession(ctx, su.TokenHash, testNow); got == nil {
		t.Fatal("session deletion survived failed audit")
	}
	_, err = s.AdminUpdate(ctx, sb, u.ID, &role, nil, testNow)
	if err == nil {
		t.Fatal("audit failure ignored")
	}
	actual, _ = s.UserByID(ctx, u.ID)
	if actual.Role != "user" {
		t.Fatal("role update survived failed audit")
	}
	s.DB.Exec("DROP TRIGGER fail_audit")
	if err = s.AdminReset(ctx, sb, u, "new-hash", testNow); err != nil {
		t.Fatal(err)
	}
	actual, _ = s.UserByID(ctx, u.ID)
	if !actual.MustChangePassword {
		t.Fatal("reset missing forced password change")
	}
	if got, _ := s.ResolveSession(ctx, su.TokenHash, testNow); got != nil {
		t.Fatal("old session survives reset")
	}
	audits, total, err := s.ListAudit(ctx, 1, 100, u.ID, "")
	if err != nil || total != 1 || len(audits) != 1 {
		t.Fatalf("audit list: %v %d %v", audits, total, err)
	}
	bts := fmt.Sprintf("%+v", audits)
	if strings.Contains(bts, "hash") || strings.Contains(bts, "password=") {
		t.Fatal("secret in audit")
	}
}
func TestCLIAndPagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin, err := s.InitAdmin(ctx, "admin", "first-hash", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.InitAdmin(ctx, "other", "hash", testNow); err == nil {
		t.Fatal("duplicate init allowed")
	}
	session(t, s, admin)
	if err = s.RecoverAdmin(ctx, admin, "recovered", testNow); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserByID(ctx, admin.ID)
	if !got.MustChangePassword || got.AuthVersion != 2 {
		t.Fatal("recovery flags missing")
	}
	u := user(t, s, "alice", "user")
	if err = s.RecoverAdmin(ctx, u, "no", testNow); err == nil {
		t.Fatal("user recovery allowed")
	}
	users, total, err := s.ListUsers(ctx, 1, 1, "a", "", "active")
	if err != nil || total != 2 || len(users) != 1 || users[0].ID != u.ID {
		t.Fatalf("paging: %+v %d %v", users, total, err)
	}
	logs, total, err := s.ListAudit(ctx, 1, 1, admin.ID, "")
	if err != nil || total != 2 || len(logs) != 1 || logs[0].Action != "admin_recover_password" {
		t.Fatalf("audit paging: %+v %d %v", logs, total, err)
	}
}
func TestMIG001And002RefuseUnknownOrCorruptHistory(t *testing.T) {
	for _, mutation := range []string{"UPDATE schema_migrations SET version=2", "UPDATE schema_migrations SET checksum='changed'", "DELETE FROM schema_migrations", "DROP TABLE user_preferences", "ALTER TABLE users DROP COLUMN updated_at", "DROP INDEX sessions_user"} {
		t.Run(mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal("idempotent migration", err)
			}
			if _, err = s.DB.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if s, err = Open(path); err == nil {
				s.Close()
				t.Fatal("corrupt migration accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("CREATE TABLE old_preferences(id INTEGER)")
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("legacy schema accepted without owner migration")
	}
}
func TestDB002And004RollbackCancelAndPanic(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := user(t, s, "alice", "user")
	for _, mode := range []string{"error", "cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			req, stop := context.WithCancel(ctx)
			defer stop()
			var err error
			func() {
				defer func() {
					r := recover()
					if mode == "panic" && r != "expected" {
						t.Errorf("panic not rethrown: %v", r)
					}
				}()
				err = s.WithImmediate(req, func(c *sql.Conn) error {
					if _, e := c.ExecContext(req, "UPDATE users SET role='admin' WHERE id=?", u.ID); e != nil {
						return e
					}
					switch mode {
					case "cancel":
						stop()
						return req.Err()
					case "panic":
						panic("expected")
					default:
						return errors.New("expected")
					}
				})
			}()
			if mode != "panic" && err == nil {
				t.Fatal("error swallowed")
			}
			got, e := s.UserByID(ctx, u.ID)
			if e != nil || got.Role != "user" {
				t.Fatalf("rollback failed: %+v %v", got, e)
			}
		})
	}
}

func TestADM004CombinedUpdateAuditOnceAndNoop(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := user(t, s, "admin_a", "admin")
	u := user(t, s, "alice", "user")
	ss := session(t, s, a)
	role, status := "admin", "disabled"
	if _, err := s.AdminUpdate(ctx, ss, u.ID, &role, &status, testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	logs, total, err := s.ListAudit(ctx, 1, 20, u.ID, "")
	if err != nil || total != 1 || logs[0].Action != "user_updated" || logs[0].Details["role_before"] != "user" || logs[0].Details["status_after"] != "disabled" {
		t.Fatalf("combined audit: %+v %d %v", logs, total, err)
	}
	got, err := s.AdminUpdate(ctx, ss, u.ID, &role, &status, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(testNow.Add(time.Second)) {
		t.Fatal("no-op changed timestamp")
	}
	_, total, err = s.ListAudit(ctx, 1, 20, u.ID, "")
	if err != nil || total != 1 {
		t.Fatal("no-op emitted audit")
	}
}
func TestSES004ResolveInvalidStates(t *testing.T) {
	for _, mutation := range []string{"UPDATE users SET auth_version=auth_version+1", "UPDATE users SET status='disabled'", "DELETE FROM sessions", "UPDATE sessions SET expires_at=0"} {
		t.Run(mutation, func(t *testing.T) {
			s := testStore(t)
			u := user(t, s, "alice", "user")
			ss := session(t, s, u)
			if _, err := s.DB.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			got, err := s.ResolveSession(context.Background(), ss.TokenHash, testNow)
			if err != nil || got != nil {
				t.Fatalf("invalid state resolved: %+v %v", got, err)
			}
		})
	}
}

// WaitCount proves the competing operation reached the occupied single-connection pool.
func waitForStoreWaiter(ctx context.Context, s *Store, previous int64) error {
	for s.DB.Stats().WaitCount == previous {
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	return nil
}

func TestPREF004FoodDisableAndSaveSerialize(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	p, err := s.Preferences(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.ExcludedFoodIDs = []int64{1}
	result := make(chan error, 1)
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		var status string
		if err := c.QueryRowContext(ctx, "SELECT status FROM foods WHERE id=1").Scan(&status); err != nil {
			return err
		}
		if status != "active" {
			return fmt.Errorf("fixture food is %s", status)
		}
		previous := s.DB.Stats().WaitCount
		go func() { _, saveErr := s.SavePreferences(ctx, ss, p, testNow); result <- saveErr }()
		if err := waitForStoreWaiter(ctx, s, previous); err != nil {
			return err
		}
		_, err := c.ExecContext(ctx, "UPDATE foods SET status='disabled' WHERE id=1")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	code(t, <-result, "INVALID_ARGUMENT")
	got, err := s.Preferences(ctx, u.ID)
	if err != nil || len(got.ExcludedFoodIDs) != 0 {
		t.Fatalf("rejected save mutated restrictions: %+v %v", got, err)
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE foods SET status='active' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePreferences(ctx, ss, p, testNow); err != nil {
		t.Fatal(err)
	}
	if err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		_, e := c.ExecContext(ctx, "UPDATE foods SET status='disabled' WHERE id=1")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.SavePreferences(ctx, ss, p, testNow.Add(time.Second))
	if err != nil || len(got.ExcludedFoods) != 1 || got.ExcludedFoods[0].Status != "disabled" {
		t.Fatalf("save-before-disable reference lost: %+v %v", got, err)
	}
}

func TestPREF005OrderedCompleteReplacements(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	first, err := s.Preferences(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	energyA, proteinA, fatA, carbsA := 1800.0, 80.0, 50.0, 250.0
	first.Allergens = []string{"milk"}
	first.ExcludedFoodIDs = []int64{1}
	first.DietaryPreferences = []string{"vegetarian"}
	first.DailyEnergyKcal = &energyA
	first.DailyProteinG = &proteinA
	first.DailyFatG = &fatA
	first.DailyCarbohydrateG = &carbsA
	energyB, proteinB, fatB, carbsB := 2200.0, 110.0, 70.0, 290.0
	second := first
	second.Allergens = []string{"egg"}
	second.ExcludedFoodIDs = []int64{2}
	second.DietaryPreferences = []string{"low_salt"}
	second.DailyEnergyKcal = &energyB
	second.DailyProteinG = &proteinB
	second.DailyFatG = &fatB
	second.DailyCarbohydrateG = &carbsB
	firstDone := make(chan struct{})
	firstResult, secondResult := make(chan error, 1), make(chan error, 1)
	go func() {
		select {
		case <-firstDone:
			_, e := s.SavePreferences(ctx, ss, second, testNow.Add(time.Second))
			secondResult <- e
		case <-ctx.Done():
			secondResult <- ctx.Err()
		}
	}()
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		previous := s.DB.Stats().WaitCount
		go func() { _, e := s.SavePreferences(ctx, ss, first, testNow); firstResult <- e; close(firstDone) }()
		return waitForStoreWaiter(ctx, s, previous)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = <-firstResult; err != nil {
		t.Fatal(err)
	}
	if err = <-secondResult; err != nil {
		t.Fatal(err)
	}
	got, err := s.Preferences(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Allergens, second.Allergens) || !reflect.DeepEqual(got.ExcludedFoodIDs, second.ExcludedFoodIDs) || !reflect.DeepEqual(got.DietaryPreferences, second.DietaryPreferences) || got.DailyEnergyKcal == nil || *got.DailyEnergyKcal != energyB || got.DailyProteinG == nil || *got.DailyProteinG != proteinB || got.DailyFatG == nil || *got.DailyFatG != fatB || got.DailyCarbohydrateG == nil || *got.DailyCarbohydrateG != carbsB || !got.UpdatedAt.Equal(testNow.Add(time.Second)) {
		t.Fatalf("complete replacements mixed fields: %+v", got)
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE foods SET status='disabled' WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	cleared := second
	cleared.ExcludedFoodIDs = []int64{}
	if _, err = s.SavePreferences(ctx, ss, cleared, testNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = s.SavePreferences(ctx, ss, second, testNow.Add(3*time.Second))
	code(t, err, "INVALID_ARGUMENT")
	got, err = s.Preferences(ctx, u.ID)
	if err != nil || len(got.ExcludedFoodIDs) != 0 {
		t.Fatalf("removed disabled restriction was reintroduced: %+v %v", got, err)
	}
}

func TestCON001OriginalHashCheckedIndependentlyOfVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	old := user(t, s, "alice", "user")
	if _, err := s.DB.ExecContext(ctx, "UPDATE users SET password_hash='replacement-hash' WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.UserByID(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.AuthVersion != old.AuthVersion || current.PasswordHash == old.PasswordHash {
		t.Fatal("fixture did not isolate password hash from version")
	}
	staleToken := bytes.Repeat([]byte{41}, 32)
	_, err = s.IssueSession(ctx, old, staleToken, testNow, testNow.Add(time.Hour))
	code(t, err, "INVALID_CREDENTIALS")
	if got, err := s.ResolveSession(ctx, staleToken, testNow); err != nil || got != nil {
		t.Fatalf("stale hash issued session: %+v %v", got, err)
	}
	token := bytes.Repeat([]byte{42}, 32)
	if _, err = s.IssueSession(ctx, current, token, testNow, testNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolveSession(ctx, token, testNow); err != nil || got == nil || got.User.ID != current.ID {
		t.Fatalf("fresh hash failed to issue session: %+v %v", got, err)
	}
}

func TestCON004StatusAuditFailureRollsBackRevocation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := user(t, s, "admin_a", "admin")
	target := user(t, s, "alice", "user")
	actor, ss := session(t, s, admin), session(t, s, target)
	if _, err := s.DB.ExecContext(ctx, "CREATE TRIGGER fail_status_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	status := "disabled"
	if _, err := s.AdminUpdate(ctx, actor, target.ID, nil, &status, testNow.Add(time.Second)); err == nil {
		t.Fatal("status audit failure swallowed")
	}
	got, err := s.UserByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != target.Status || got.AuthVersion != target.AuthVersion || !got.UpdatedAt.Equal(target.UpdatedAt) {
		t.Fatalf("status/version mutation survived failed audit: %+v", got)
	}
	if resolved, err := s.ResolveSession(ctx, ss.TokenHash, testNow); err != nil || resolved == nil {
		t.Fatalf("session revocation survived failed audit: %+v %v", resolved, err)
	}
	_, total, err := s.ListAudit(ctx, 1, 20, target.ID, "")
	if err != nil || total != 0 {
		t.Fatalf("failed operation left audit: %d %v", total, err)
	}
}
