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

	migfs "freshplate/server/migrations"
	"gorm.io/gorm"
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
	s, err := newStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sql.ErrNoRows
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 255 {
		case 5, 6:
			return E(503, "DATABASE_BUSY", "数据库繁忙，请稍后重试")
		case 19:
			if strings.Contains(se.Error(), "users.username") {
				return E(409, "USERNAME_TAKEN", "用户名已存在")
			}
			if strings.Contains(se.Error(), "foods.name") {
				return E(409, "FOOD_NAME_TAKEN", "食材名称已存在")
			}
		}
	}
	return err
}
func (s *Store) withImmediateSQL(ctx context.Context, fn func(*sql.Conn) error) (err error) {
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

// migrationChain 声明完整的顺序迁移链；只能向末尾追加新版本，
// 已应用条目的版本号与文件内容（参与校验和）都不得修改。
// （不直接命名 migrations：refresh_test.go 已以非别名导入 migrations 包。）
var migrationChain = []struct {
	version int
	file    string
}{
	{1, "001_users.sql"},
	{2, "002_refresh_tokens.sql"},
	{3, "003_foods.sql"},
	{4, "004_inventory.sql"},
	{5, "005_recipes.sql"},
}

// foodColumns 是 v3 重建后 foods 表的全部列，供启动结构校验使用。
const foodColumns = "id,name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,density_g_ml,piece_weight_g,conversion_source,allergens_json,allergen_complete,allergen_source,version,created_at,updated_at"

// migrate 是声明式顺序迁移器：在单个 IMMEDIATE 事务内校验迁移历史、
// 从当前版本之后依序补齐缺失迁移并做统一结构检查；任何一步失败都整体回滚，
// 服务不得带病启动。不提供自动降级，回退须使用完整备份。
func (s *Store) migrate(ctx context.Context) error {
	type step struct {
		version  int
		script   string
		checksum string
	}
	chain := make([]step, 0, len(migrationChain))
	for _, m := range migrationChain {
		script, err := migfs.Files.ReadFile(m.file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(script)
		chain = append(chain, step{m.version, string(script), hex.EncodeToString(sum[:])})
	}
	return s.withImmediateSQL(ctx, func(c *sql.Conn) error {
		var exists int
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&exists); err != nil {
			return err
		}
		applied := 0
		if exists == 0 {
			// 无版本表的库只有空库可自动初始化；旧库必须显式迁移。
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
		} else {
			var count, version int
			if err := c.QueryRowContext(ctx, "SELECT count(*),coalesce(max(version),0) FROM schema_migrations").Scan(&count, &version); err != nil {
				return err
			}
			// 版本行必须从 1 连续到最大值；空表或跳版都视为损坏历史。
			if count != version || version < 1 {
				return fmt.Errorf("unsupported or incomplete migration history")
			}
			if version > len(chain) {
				return fmt.Errorf("database schema from a newer version: %d", version)
			}
			rows, err := c.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
			if err != nil {
				return err
			}
			for applied < len(chain) && rows.Next() {
				var v int64
				var actual string
				if err := rows.Scan(&v, &actual); err != nil {
					rows.Close()
					return err
				}
				if int(v) != chain[applied].version {
					rows.Close()
					return fmt.Errorf("unsupported or incomplete migration history")
				}
				if actual != chain[applied].checksum {
					rows.Close()
					return fmt.Errorf("migration checksum mismatch")
				}
				applied++
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		// 从当前版本之后依序执行缺失迁移并登记版本行；任一步失败整体回滚。
		for _, m := range chain[applied:] {
			if _, err := c.ExecContext(ctx, m.script); err != nil {
				return err
			}
			if _, err := c.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,?)", m.version, m.checksum, time.Now().Unix()); err != nil {
				return err
			}
		}
		// 统一结构检查：缺表、缺列、缺索引或外键悬空都拒绝启动。
		for _, table := range []string{"users", "sessions", "refresh_tokens", "user_preferences", "foods", "audit_logs", "resource_audit_logs", "inventory_items", "recipes", "recipe_ingredients", "recipe_allergens"} {
			var n int
			if err := c.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("incomplete database schema: %s", table)
			}
		}
		for table, columns := range map[string]string{
			"users":               userColumns,
			"sessions":            "token_hash,user_id,auth_version,created_at,expires_at",
			"refresh_tokens":      "token_hash,session_hash,consumed",
			"user_preferences":    "user_id,allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g,updated_at",
			"foods":               foodColumns,
			"audit_logs":          "id,actor_user_id,actor_source,action,target_user_id,details_json,created_at",
			"resource_audit_logs": "id,actor_user_id,resource_type,resource_id,action,details_json,created_at",
			"inventory_items":     "id,user_id,food_id,quantity_milli,unit,expires_on,note,version,created_at,updated_at,deleted_at",
			"recipes":             "id,visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,source,allergen_complete,allergen_source,version,created_at,updated_at",
			"recipe_ingredients":  "recipe_id,position,food_id,quantity_milli,unit",
			"recipe_allergens":    "recipe_id,allergen_code",
		} {
			rows, err := c.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" LIMIT 0")
			if err != nil {
				return fmt.Errorf("incomplete database schema: %s: %w", table, err)
			}
			rows.Close()
		}
		for _, index := range []string{"sessions_user", "sessions_expiry", "refresh_session", "audit_created", "audit_target", "resource_audit_resource", "inventory_user", "inventory_user_expiry", "recipes_owner", "recipes_visibility", "recipe_ingredients_food"} {
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
