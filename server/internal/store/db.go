package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"freshplate/server/migrations"
	"modernc.org/sqlite"
)

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// SQLite derives new WAL/SHM permissions from the database file.
	f, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err = os.Chmod(abs+suffix, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db}
	var mode string
	if err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		db.Close()
		return nil, err
	}
	if mode != "wal" {
		db.Close()
		return nil, fmt.Errorf("WAL unavailable")
	}
	if err = s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func mapError(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 255 {
		case 5, 6:
			return E(503, "DATABASE_BUSY", "数据库繁忙，请稍后重试")
		case 19:
			if strings.Contains(se.Error(), "users.username") {
				return E(409, "USERNAME_TAKEN", "用户名已存在")
			}
		}
	}
	return err
}
func (s *Store) WithImmediate(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	c, err := s.DB.Conn(ctx)
	if err != nil {
		return mapError(err)
	}
	defer c.Close()
	if _, err = c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return mapError(err)
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, rollbackErr := c.ExecContext(cleanup, "ROLLBACK"); rollbackErr != nil {
				_ = c.Raw(func(any) error { return driver.ErrBadConn })
				if err == nil {
					err = fmt.Errorf("transaction cleanup failed: %w", rollbackErr)
				}
			}
		}
		err = mapError(err)
	}()
	if err = fn(c); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	committed = err == nil
	return err
}
func (s *Store) migrate(ctx context.Context) error {
	script, err := migrations.Files.ReadFile("001_users.sql")
	if err != nil {
		return err
	}
	sum := sha256.Sum256(script)
	checksum := hex.EncodeToString(sum[:])
	return s.WithImmediate(ctx, func(c *sql.Conn) error {
		var exists int
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			var tables int
			if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
				return err
			}
			if tables != 0 {
				return fmt.Errorf("unrecognized database schema: explicit legacy migration required")
			}
			if _, err := c.ExecContext(ctx, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, completed_at INTEGER NOT NULL)"); err != nil {
				return err
			}
			if _, err := c.ExecContext(ctx, string(script)); err != nil {
				return err
			}
			if _, err := c.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(1,?,?)", checksum, time.Now().Unix()); err != nil {
				return err
			}
		} else {
			var count, version int
			var actual string
			if err := c.QueryRowContext(ctx, "SELECT count(*),coalesce(max(version),0) FROM schema_migrations").Scan(&count, &version); err != nil {
				return err
			}
			if count != version || version < 1 || version > 2 {
				return fmt.Errorf("unsupported or incomplete migration history")
			}
			if err := c.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=1").Scan(&actual); err != nil {
				return err
			}
			if actual != checksum {
				return fmt.Errorf("migration checksum mismatch")
			}
		}
		next, err := migrations.Files.ReadFile("002_refresh_tokens.sql")
		if err != nil {
			return err
		}
		nextSum := sha256.Sum256(next)
		nextChecksum := hex.EncodeToString(nextSum[:])
		var applied int
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version=2").Scan(&applied); err != nil {
			return err
		}
		if applied == 0 {
			if _, err := c.ExecContext(ctx, string(next)); err != nil {
				return err
			}
			if _, err := c.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(2,?,?)", nextChecksum, time.Now().Unix()); err != nil {
				return err
			}
		} else {
			var actual string
			if err := c.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=2").Scan(&actual); err != nil {
				return err
			}
			if actual != nextChecksum {
				return fmt.Errorf("migration checksum mismatch")
			}
		}
		for _, table := range []string{"users", "sessions", "refresh_tokens", "user_preferences", "foods", "audit_logs"} {
			var n int
			if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("incomplete database schema: %s", table)
			}
		}
		for table, columns := range map[string]string{
			"users":            userColumns,
			"sessions":         "token_hash,user_id,auth_version,created_at,expires_at",
			"refresh_tokens":   "token_hash,session_hash,consumed",
			"user_preferences": "user_id,allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g,updated_at",
			"foods":            "id,name,status",
			"audit_logs":       "id,actor_user_id,actor_source,action,target_user_id,details_json,created_at",
		} {
			rows, err := c.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" LIMIT 0")
			if err != nil {
				return fmt.Errorf("incomplete database schema: %s: %w", table, err)
			}
			rows.Close()
		}
		for _, index := range []string{"sessions_user", "sessions_expiry", "refresh_session", "audit_created", "audit_target"} {
			var n int
			if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("missing schema index: %s", index)
			}
		}
		rows, err := c.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return fmt.Errorf("foreign key integrity check failed")
		}
		return rows.Err()
	})
}
