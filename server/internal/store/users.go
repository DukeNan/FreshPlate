package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const userColumns = "id,username,password_hash,role,status,must_change_password,auth_version,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scanUser(r scanner, extra ...any) (User, error) {
	var u User
	var created, updated int64
	dest := []any{&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &u.MustChangePassword, &u.AuthVersion, &created, &updated}
	err := r.Scan(append(dest, extra...)...)
	u.CreatedAt = time.Unix(created, 0).UTC()
	u.UpdatedAt = time.Unix(updated, 0).UTC()
	return u, err
}
func (s *Store) UserByName(ctx context.Context, name string) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE username=?", name))
	return u, mapError(err)
}
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
	return u, mapError(err)
}
func createUser(ctx context.Context, c *sql.Conn, name, hash, role string, now time.Time) (User, error) {
	r, err := c.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES(?,?,?,?,?)", name, hash, role, now.Unix(), now.Unix())
	if err != nil {
		return User{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return User{}, err
	}
	if _, err = c.ExecContext(ctx, "INSERT INTO user_preferences(user_id,updated_at) VALUES(?,?)", id, now.Unix()); err != nil {
		return User{}, err
	}
	return scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
}
func (s *Store) CreateUser(ctx context.Context, name, hash, role string, now time.Time) (u User, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error { var e error; u, e = createUser(ctx, c, name, hash, role, now); return e })
	return
}
func (s *Store) ResolveSession(ctx context.Context, hash []byte, now time.Time) (*Session, error) {
	return resolveSession(ctx, s.DB, hash, now)
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveSession(ctx context.Context, q querier, hash []byte, now time.Time) (*Session, error) {
	var expires int64
	columns := "u." + strings.ReplaceAll(userColumns, ",", ",u.")
	u, err := scanUser(q.QueryRowContext(ctx, "SELECT "+columns+",s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>? AND u.status='active' AND s.auth_version=u.auth_version", hash, now.Unix()), &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, mapError(err)
	}
	return &Session{User: u, TokenHash: hash, ExpiresAt: time.Unix(expires, 0).UTC()}, nil
}
func checkSession(ctx context.Context, c *sql.Conn, session Session, now time.Time, admin bool) (User, error) {
	actual, err := resolveSession(ctx, c, session.TokenHash, now)
	if err != nil {
		return User{}, err
	}
	if actual == nil || actual.User.ID != session.User.ID {
		return User{}, E(401, "AUTH_REQUIRED", "请重新登录")
	}
	u := actual.User
	if admin && u.MustChangePassword {
		return User{}, E(403, "PASSWORD_CHANGE_REQUIRED", "请先修改密码")
	}
	if admin && u.Role != "admin" {
		return User{}, E(403, "FORBIDDEN", "无权执行操作")
	}
	return u, nil
}
func sameCredential(a, b User) bool {
	return a.ID == b.ID && a.Status == "active" && a.AuthVersion == b.AuthVersion && a.PasswordHash == b.PasswordHash
}
func (s *Store) IssueSession(ctx context.Context, expected User, hash []byte, now, expires time.Time) (u User, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		var e error
		u, e = scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", expected.ID))
		if e != nil {
			return e
		}
		if !sameCredential(u, expected) {
			return E(401, "INVALID_CREDENTIALS", "用户名或密码不正确")
		}
		_, e = c.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,auth_version,created_at,expires_at) VALUES(?,?,?,?,?)", hash, u.ID, u.AuthVersion, now.Unix(), expires.Unix())
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, "INSERT INTO refresh_tokens(token_hash,session_hash) VALUES(?,?)", hash, hash)
		return e
	})
	return
}
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash)
	return mapError(err)
}
func (s *Store) CleanupSessions(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", now.Unix())
	return mapError(err)
}
func updatePassword(ctx context.Context, c *sql.Conn, id int64, hash string, must bool, now time.Time) error {
	if _, err := c.ExecContext(ctx, "UPDATE users SET password_hash=?,must_change_password=?,auth_version=auth_version+1,updated_at=? WHERE id=?", hash, must, now.Unix(), id); err != nil {
		return err
	}
	_, err := c.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", id)
	return err
}
func (s *Store) ChangePassword(ctx context.Context, session Session, expected User, hash string, now time.Time) error {
	return s.WithImmediate(ctx, func(c *sql.Conn) error {
		u, err := checkSession(ctx, c, session, now, false)
		if err != nil {
			return err
		}
		if !sameCredential(u, expected) {
			return E(401, "AUTH_REQUIRED", "身份已变化，请重新登录")
		}
		return updatePassword(ctx, c, u.ID, hash, false, now)
	})
}
func audit(ctx context.Context, c *sql.Conn, actor *int64, source, action string, target int64, details map[string]string, now time.Time) error {
	if details == nil {
		details = map[string]string{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "INSERT INTO audit_logs(actor_user_id,actor_source,action,target_user_id,details_json,created_at) VALUES(?,?,?,?,?,?)", actor, source, action, target, string(b), now.Unix())
	return err
}
func (s *Store) InitAdmin(ctx context.Context, name, hash string, now time.Time) (u User, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		var count int
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role='admin'").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return E(409, "ADMIN_EXISTS", "管理员已经存在")
		}
		var e error
		u, e = createUser(ctx, c, name, hash, "admin", now)
		if e != nil {
			return e
		}
		return audit(ctx, c, nil, "cli", "admin_init", u.ID, nil, now)
	})
	return
}
func (s *Store) RecoverAdmin(ctx context.Context, expected User, hash string, now time.Time) error {
	return s.WithImmediate(ctx, func(c *sql.Conn) error {
		u, err := scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", expected.ID))
		if err != nil {
			return err
		}
		if u.Role != "admin" || !sameCredential(u, expected) {
			return E(409, "ADMIN_RECOVERY_FORBIDDEN", "只能恢复有效的启用管理员")
		}
		if err = updatePassword(ctx, c, u.ID, hash, true, now); err != nil {
			return err
		}
		return audit(ctx, c, nil, "cli", "admin_recover_password", u.ID, nil, now)
	})
}
func (s *Store) AdminReset(ctx context.Context, actor Session, expected User, hash string, now time.Time) error {
	return s.WithImmediate(ctx, func(c *sql.Conn) error {
		a, err := checkSession(ctx, c, actor, now, true)
		if err != nil {
			return err
		}
		if a.ID == expected.ID {
			return E(409, "SELF_ADMIN_CHANGE_FORBIDDEN", "请使用本人修改密码入口")
		}
		u, err := scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", expected.ID))
		if err == sql.ErrNoRows {
			return E(404, "NOT_FOUND", "用户不存在")
		}
		if err != nil {
			return err
		}
		if u.AuthVersion != expected.AuthVersion || u.PasswordHash != expected.PasswordHash {
			return E(409, "IDENTITY_CHANGED", "目标账号已变化，请重试")
		}
		if err = updatePassword(ctx, c, u.ID, hash, true, now); err != nil {
			return err
		}
		return audit(ctx, c, &a.ID, "web", "password_reset", u.ID, nil, now)
	})
}
func (s *Store) AdminUpdate(ctx context.Context, actor Session, target int64, role, status *string, now time.Time) (u User, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		a, e := checkSession(ctx, c, actor, now, true)
		if e != nil {
			return e
		}
		if a.ID == target {
			return E(409, "SELF_ADMIN_CHANGE_FORBIDDEN", "不能修改自己的管理属性")
		}
		if role == nil && status == nil {
			return E(400, "INVALID_ARGUMENT", "至少指定一个属性")
		}
		if role != nil && *role != "admin" && *role != "user" {
			return E(400, "INVALID_ARGUMENT", "角色无效")
		}
		if status != nil && *status != "active" && *status != "disabled" {
			return E(400, "INVALID_ARGUMENT", "状态无效")
		}
		u, e = scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", target))
		if e == sql.ErrNoRows {
			return E(404, "NOT_FOUND", "用户不存在")
		}
		if e != nil {
			return e
		}
		old := u
		if role != nil {
			u.Role = *role
		}
		if status != nil {
			u.Status = *status
		}
		if old.Role == u.Role && old.Status == u.Status {
			return nil
		}
		if old.Role == "admin" && old.Status == "active" && (u.Role != "admin" || u.Status != "active") {
			var n int
			if e = c.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role='admin' AND status='active'").Scan(&n); e != nil {
				return e
			}
			if n <= 1 {
				return E(409, "LAST_ADMIN_REQUIRED", "必须保留至少一位启用管理员")
			}
		}
		if _, e = c.ExecContext(ctx, "UPDATE users SET role=?,status=?,auth_version=auth_version+?,updated_at=? WHERE id=?", u.Role, u.Status, boolInt(old.Status != u.Status), now.Unix(), target); e != nil {
			return e
		}
		if old.Status != u.Status {
			if _, e = c.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", target); e != nil {
				return e
			}
		}
		if old.Role != u.Role && old.Status != u.Status {
			if e = audit(ctx, c, &a.ID, "web", "user_updated", target, map[string]string{"role_before": old.Role, "role_after": u.Role, "status_before": old.Status, "status_after": u.Status}, now); e != nil {
				return e
			}
		} else if old.Role != u.Role {
			if e = audit(ctx, c, &a.ID, "web", "role_changed", target, map[string]string{"before": old.Role, "after": u.Role}, now); e != nil {
				return e
			}
		} else if old.Status != u.Status {
			if e = audit(ctx, c, &a.ID, "web", "status_changed", target, map[string]string{"before": old.Status, "after": u.Status}, now); e != nil {
				return e
			}
		}
		u, e = scanUser(c.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", target))
		return e
	})
	return
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
