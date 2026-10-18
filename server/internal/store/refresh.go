package store

import (
	"context"
	"database/sql"
	"time"
)

// ResolveRefresh includes consumed tokens so CSRF remains bound to the session during replay detection.
func (s *Store) ResolveRefresh(ctx context.Context, hash []byte, now time.Time) (*Session, error) {
	var sid []byte
	err := s.DB.QueryRowContext(ctx, "SELECT session_hash FROM refresh_tokens WHERE token_hash=?", hash).Scan(&sid)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, mapError(err)
	}
	return s.ResolveSession(ctx, sid, now)
}
func (s *Store) RotateRefresh(ctx context.Context, old, next []byte, now time.Time) (session *Session, err error) {
	var rejected error
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		var sid []byte
		var consumed bool
		e := c.QueryRowContext(ctx, "SELECT session_hash,consumed FROM refresh_tokens WHERE token_hash=?", old).Scan(&sid, &consumed)
		if e == sql.ErrNoRows {
			rejected = E(401, "AUTH_REQUIRED", "请重新登录")
			return nil
		}
		if e != nil {
			return e
		}
		session, e = resolveSession(ctx, c, sid, now)
		if e != nil {
			return e
		}
		if session == nil {
			rejected = E(401, "AUTH_REQUIRED", "请重新登录")
			return nil
		}
		if consumed {
			if _, e = c.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", sid); e != nil {
				return e
			}
			rejected = E(401, "REFRESH_TOKEN_REUSED", "刷新令牌已被使用，请重新登录")
			return nil // Commit revocation before returning the rejection.
		}
		if _, e = c.ExecContext(ctx, "UPDATE refresh_tokens SET consumed=1 WHERE token_hash=?", old); e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, "INSERT INTO refresh_tokens(token_hash,session_hash) VALUES(?,?)", next, sid)
		return e
	})
	if err == nil {
		err = rejected
	}
	if err != nil {
		session = nil
	}
	return
}
