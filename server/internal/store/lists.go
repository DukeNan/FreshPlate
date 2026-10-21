package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return size, (page - 1) * size
}
func (s *Store) ListUsers(ctx context.Context, page, size int, username, role, status string) (items []User, total int, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := "1=1"
	args := []any{}
	if username != "" {
		where += " AND instr(username,?)>0"
		args = append(args, strings.ToLower(strings.TrimSpace(username)))
	}
	if role != "" {
		where += " AND role=?"
		args = append(args, role)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	if err = tx.WithContext(ctx).Table("users").Select("count(*)").Where(where, args...).Row().Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(page, size)
	rows, err := tx.WithContext(ctx).Table("users").Select(userColumns).Where(where, args...).Order("id DESC").Limit(limit).Offset(offset).Rows()
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	err = mapError(tx.Commit().Error)
	return
}
func (s *Store) ListAudit(ctx context.Context, page, size int, target int64, action string) (items []Audit, total int, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := "1=1"
	args := []any{}
	if target > 0 {
		where += " AND target_user_id=?"
		args = append(args, target)
	}
	if action != "" {
		where += " AND action=?"
		args = append(args, action)
	}
	if err = tx.WithContext(ctx).Table("audit_logs").Select("count(*)").Where(where, args...).Row().Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(page, size)
	rows, err := tx.WithContext(ctx).Table("audit_logs").Select("id,actor_user_id,actor_source,action,target_user_id,details_json,created_at").Where(where, args...).Order("id DESC").Limit(limit).Offset(offset).Rows()
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []Audit{}
	for rows.Next() {
		var a Audit
		var d string
		var at int64
		if e := rows.Scan(&a.ID, &a.ActorUserID, &a.ActorSource, &a.Action, &a.TargetUserID, &d, &at); e != nil {
			rows.Close()
			return nil, 0, e
		}
		if e := json.Unmarshal([]byte(d), &a.Details); e != nil {
			rows.Close()
			return nil, 0, e
		}
		a.CreatedAt = time.Unix(at, 0).UTC()
		items = append(items, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	err = mapError(tx.Commit().Error)
	return
}
