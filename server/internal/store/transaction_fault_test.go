package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"modernc.org/sqlite"
)

// The wrapper delegates actual SQLite transactions and injects only cleanup failures.
type faultDriver struct {
	base                     driver.Driver
	mu                       sync.Mutex
	next, closed             int
	failCommit, failRollback bool
	rollbackCanceled         bool
}

func (d *faultDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.next++
	d.mu.Unlock()
	return &faultConn{Conn: c, d: d}, nil
}

type faultConn struct {
	driver.Conn
	d *faultDriver
}

func (c *faultConn) Close() error {
	c.d.mu.Lock()
	c.d.closed++
	c.d.mu.Unlock()
	return c.Conn.Close()
}
func (c *faultConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	fail := false
	switch strings.ToUpper(strings.TrimSpace(q)) {
	case "COMMIT":
		fail = c.d.failCommit
	case "ROLLBACK":
		fail = c.d.failRollback
		c.d.rollbackCanceled = ctx.Err() != nil
	}
	c.d.mu.Unlock()
	if fail {
		return nil, errors.New("injected transaction failure")
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *faultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

var faultDriverSeq atomic.Int64

func TestDB004And005CommitFailureAndRollbackDiscard(t *testing.T) {
	for _, rollbackFail := range []bool{false, true} {
		t.Run(fmt.Sprint(rollbackFail), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			u := user(t, s, "alice", "user")
			s.Close()
			d := &faultDriver{base: &sqlite.Driver{}, failCommit: true, failRollback: rollbackFail}
			name := fmt.Sprintf("sqlite-fault-%d", faultDriverSeq.Add(1))
			sql.Register(name, d)
			db, err := sql.Open(name, "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			defer db.Close()
			s, err = newStore(db)
			if err != nil {
				t.Fatal(err)
			}
			err = s.WithImmediate(context.Background(), func(c *gorm.DB) error {
				return c.Table("users").Where("id=?", u.ID).Update("role", "admin").Error
			})
			if err == nil {
				t.Fatal("commit failure swallowed")
			}
			d.mu.Lock()
			opened, closed := d.next, d.closed
			d.failCommit = false
			d.failRollback = false
			d.mu.Unlock()
			if rollbackFail && closed != 1 {
				t.Fatalf("failed connection not discarded: opened=%d closed=%d", opened, closed)
			}
			got, err := s.UserByID(context.Background(), u.ID)
			if err != nil || got.Role != "user" {
				t.Fatalf("injected transaction survived: %+v %v", got, err)
			}
			d.mu.Lock()
			newOpened := d.next
			canceled := d.rollbackCanceled
			d.mu.Unlock()
			if rollbackFail && newOpened != opened+1 {
				t.Fatalf("connection reused after rollback failure: %d->%d", opened, newOpened)
			}
			if !rollbackFail && newOpened != opened {
				t.Fatal("healthy rolled back connection unnecessarily discarded")
			}
			if canceled {
				t.Fatal("cleanup used canceled context")
			}
		})
	}
}
func TestDB003BusyMapsAndRecovers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, err := sql.Open("sqlite", s.DBPathForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = s.CreateUser(ctx, "alice", "hash", "user", testNow)
	elapsed := time.Since(start)
	code(t, err, "DATABASE_BUSY")
	if elapsed < 4*time.Second || elapsed > 10*time.Second {
		t.Fatalf("unbounded or skipped busy timeout: %v", elapsed)
	}
	if _, err = c.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateUser(ctx, "alice", "hash", "user", testNow); err != nil {
		t.Fatal("connection not usable after busy", err)
	}
}
func (s *Store) DBPathForTest(t *testing.T) string {
	t.Helper()
	var seq int
	var name, path string
	if err := s.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}
