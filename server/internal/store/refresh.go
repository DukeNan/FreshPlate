package store

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

// ResolveRefresh includes consumed tokens so CSRF remains bound to the session during replay detection.
func (s *Store) ResolveRefresh(ctx context.Context, hash []byte, now time.Time) (*Session, error) {
	var sid []byte
	err := s.orm.WithContext(ctx).Table("refresh_tokens").Select("session_hash").Where("token_hash=?", hash).Row().Scan(&sid)
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
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		var sid []byte
		var consumed bool
		e := c.WithContext(ctx).Table("refresh_tokens").Select("session_hash,consumed").Where("token_hash=?", old).Row().Scan(&sid, &consumed)
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
			if e = c.WithContext(ctx).Table("sessions").Where("token_hash=?", sid).Delete(&struct{}{}).Error; e != nil {
				return e
			}
			rejected = E(401, "REFRESH_TOKEN_REUSED", "刷新令牌已被使用，请重新登录")
			return nil // Commit revocation before returning the rejection.
		}
		if e = c.WithContext(ctx).Table("refresh_tokens").Where("token_hash=?", old).Updates(map[string]any{"consumed": 1}).Error; e != nil {
			return e
		}
		e = c.WithContext(ctx).Table("refresh_tokens").Create(map[string]any{"token_hash": next, "session_hash": sid}).Error
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
