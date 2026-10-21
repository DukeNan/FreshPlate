package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ResourceAudit 是公共资源（food/recipe）管理审计事件；账号审计仍走
// audit_logs，两者互不混用。
type ResourceAudit struct {
	ID           int64          `json:"id"`
	ActorUserID  int64          `json:"actor_user_id"`
	ResourceType string         `json:"resource_type"`
	ResourceID   int64          `json:"resource_id"`
	Action       string         `json:"action"`
	Details      map[string]any `json:"details"`
	CreatedAt    time.Time      `json:"created_at"`
}

// insertResourceAudit 在当前写事务内追加一条资源审计。details 只含变更字段名、
// 前后状态与前后版本，由本层 json.Marshal 编码；资源类型与 action 由业务代码
// 固定生成，不提供通用写入或删除 API。
func insertResourceAudit(ctx context.Context, c *gorm.DB, actorID int64, resourceType string, resourceID int64, action string, details map[string]any, now time.Time) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	err = c.WithContext(ctx).Table("resource_audit_logs").Create(map[string]any{"actor_user_id": actorID, "resource_type": resourceType, "resource_id": resourceID, "action": action, "details_json": string(b), "created_at": now.Unix()}).Error
	return err
}

// ListResourceAudit 按 id DESC 分页扫描资源审计；resource_type/action 为空
// 不过滤，resourceID<=0 表示不按资源过滤。total 与当前页在同一只读事务内读取。
func (s *Store) ListResourceAudit(ctx context.Context, resourceType string, resourceID int64, action string, page, size int) (items []ResourceAudit, total int, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := "1=1"
	args := []any{}
	if resourceType != "" {
		where += " AND resource_type=?"
		args = append(args, resourceType)
	}
	if resourceID > 0 {
		where += " AND resource_id=?"
		args = append(args, resourceID)
	}
	if action != "" {
		where += " AND action=?"
		args = append(args, action)
	}
	if err = tx.WithContext(ctx).Table("resource_audit_logs").Select("count(*)").Where(where, args...).Row().Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(page, size)
	rows, err := tx.WithContext(ctx).Table("resource_audit_logs").Select("id,actor_user_id,resource_type,resource_id,action,details_json,created_at").Where(where, args...).Order("id DESC").Limit(limit).Offset(offset).Rows()
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []ResourceAudit{}
	for rows.Next() {
		var a ResourceAudit
		var d string
		var at int64
		if e := rows.Scan(&a.ID, &a.ActorUserID, &a.ResourceType, &a.ResourceID, &a.Action, &d, &at); e != nil {
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
