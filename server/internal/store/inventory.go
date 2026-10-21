package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
)

// 库存模块存储层（方案 §2/§3.3/§5）。所有查询与修改都强制限定
// user_id 与 deleted_at IS NULL；写操作在 WithImmediate 内复核会话，
// 条件更新要求影响行数恰为 1；expiry_state 为派生字段，永不持久化。

// inventoryDateLayout 是库存到期日的唯一日期格式；与 logic 包内部的
// expiryDateLayout 保持同一约定（各自未导出，避免跨包泄漏实现细节）。
const inventoryDateLayout = "2006-01-02"

// inventoryDetailColumns 是库存批次查询列集合，f.name/f.status 来自
// foods JOIN，供响应保留停用食材名称与停用标记。
const inventoryDetailColumns = "i.id,i.user_id,i.food_id,f.name,f.status,i.quantity_milli,i.unit,i.expires_on,i.note,i.version,i.created_at,i.updated_at"

// InventoryItem 是库存批次持久化行（含关联食材名称与状态）。刻意不带
// JSON 标签：API 响应 DTO 在 api 层单独定义并派生 expiry_state，
// user_id 与内部千分单位 quantity_milli 绝不直接序列化。
type InventoryItem struct {
	ID            int64
	UserID        int64
	FoodID        int64
	FoodName      string
	FoodStatus    string
	QuantityMilli int64
	Unit          string
	ExpiresOn     *string // YYYY-MM-DD；nil 表示未知到期日
	Note          string
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// inventoryNotFound 统一表示不存在/已删除/他人资源；调用方不得据其
// 区分三者的差异（方案 §4：不得通过错误区分他人库存是否存在）。
var inventoryNotFound = E(404, "NOT_FOUND", "库存批次不存在")

func scanInventoryItem(r scanner) (InventoryItem, error) {
	var it InventoryItem
	var created, updated int64
	err := r.Scan(&it.ID, &it.UserID, &it.FoodID, &it.FoodName, &it.FoodStatus, &it.QuantityMilli, &it.Unit, &it.ExpiresOn, &it.Note, &it.Version, &created, &updated)
	if err != nil {
		return InventoryItem{}, err
	}
	it.CreatedAt = time.Unix(created, 0).UTC()
	it.UpdatedAt = time.Unix(updated, 0).UTC()
	return it, nil
}

// inventoryExpiryEqual 在 NULL 语义下比较到期日：仅当同为 nil 或指向
// 相同日期时相等，用于"相同内容 PUT 不加版本"判定。
func inventoryExpiryEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ListInventory 返回本人未删除批次分页（id DESC）。q 为食材名称字面
// 子串；foodID>0 按食材筛选；expiryState 五值筛选条件严格按方案 §3.3
// 表格（depleted: quantity_milli=0；expired/expiring/fresh/unknown 均要求
// quantity_milli>0，用完批次不进入日期状态），日期边界由本函数解析
// today 后在 Go 中算好作参数绑定；items 与 total 在同一只读事务内取得。
func (s *Store) ListInventory(ctx context.Context, userID int64, q string, foodID int64, expiryState string, page, size int, today string) (items []InventoryItem, total int, err error) {
	base, err := time.Parse(inventoryDateLayout, today)
	if err != nil {
		return nil, 0, err
	}
	todayPlus3 := base.AddDate(0, 0, 3).Format(inventoryDateLayout)
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := "i.user_id=? AND i.deleted_at IS NULL"
	args := []any{userID}
	if expiryState != "" {
		switch expiryState {
		case "depleted":
			where += " AND i.quantity_milli=0"
		case "expired":
			where += " AND i.quantity_milli>0 AND i.expires_on<?"
			args = append(args, today)
		case "expiring":
			where += " AND i.quantity_milli>0 AND i.expires_on BETWEEN ? AND ?"
			args = append(args, today, todayPlus3)
		case "fresh":
			where += " AND i.quantity_milli>0 AND i.expires_on>?"
			args = append(args, todayPlus3)
		case "unknown":
			where += " AND i.quantity_milli>0 AND i.expires_on IS NULL"
		default:
			return nil, 0, E(400, "INVALID_ARGUMENT", "无效的到期状态")
		}
	}
	if q != "" {
		where += " AND instr(f.name,?)>0"
		args = append(args, q)
	}
	if foodID > 0 {
		where += " AND i.food_id=?"
		args = append(args, foodID)
	}
	if err = tx.WithContext(ctx).Table("inventory_items i").Select("count(*)").Joins("JOIN foods f ON f.id=i.food_id").Where(where, args...).Row().Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(page, size)
	rows, err := tx.WithContext(ctx).Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where(where, args...).Order("i.id DESC").Limit(limit).Offset(offset).Rows()
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []InventoryItem{}
	for rows.Next() {
		it, e := scanInventoryItem(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	err = mapError(tx.Commit().Error)
	return
}

// InventoryByID 在写事务内复核会话后返回本人未删除批次详情；
// 停用食材的批次同样可读（保留名称与停用标记）。
func (s *Store) InventoryByID(ctx context.Context, actor Session, id int64, now time.Time) (out InventoryItem, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		if _, e := checkSession(ctx, c, actor, now, false); e != nil {
			return e
		}
		it, e := scanInventoryItem(c.WithContext(ctx).Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where("i.id=? AND i.user_id=? AND i.deleted_at IS NULL", id, actor.User.ID).Row())
		if errors.Is(e, sql.ErrNoRows) {
			return inventoryNotFound
		}
		if e != nil {
			return e
		}
		out = it
		return nil
	})
	return
}

// CreateInventory 在写事务内复核会话、复核食材 active 后创建批次
// （version=1，note 默认空串）；不因相同内容合并，两批相同食材是真实
// 库存。food 不存在或非 active 一律 FOOD_UNAVAILABLE。返回事务内最终记录。
func (s *Store) CreateInventory(ctx context.Context, actor Session, foodID, quantityMilli int64, unit string, expiresOn *string, note string, now time.Time) (out InventoryItem, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		if _, e := checkSession(ctx, c, actor, now, false); e != nil {
			return e
		}
		var status string
		e := c.WithContext(ctx).Table("foods").Select("status").Where("id=?", foodID).Row().Scan(&status)
		if errors.Is(e, sql.ErrNoRows) || (e == nil && status != "active") {
			return E(400, "FOOD_UNAVAILABLE", "食材不存在或已停用")
		}
		if e != nil {
			return e
		}
		row := inventoryRow{UserID: actor.User.ID, FoodID: foodID, QuantityMilli: quantityMilli, Unit: unit, ExpiresOn: expiresOn, Note: note, Version: 1, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
		e = c.WithContext(ctx).Table("inventory_items").Create(&row).Error
		if e != nil {
			return e
		}
		id := row.ID
		out, e = scanInventoryItem(c.WithContext(ctx).Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where("i.id=? AND i.user_id=?", id, actor.User.ID).Row())
		return e
	})
	return
}

// UpdateInventory 在写事务内复核会话、按 id/user_id/未删除读取本人批次
// 并核对版本；关联食材停用时执行"只减不增"规则；内容无变化返回原详情
// 不加版本、不推进 updated_at；否则以 id+user_id+version+未删除条件更新，
// 影响行数必须为 1，最后返回事务内最终记录。
func (s *Store) UpdateInventory(ctx context.Context, actor Session, id, version, quantityMilli int64, expiresOn *string, note string, now time.Time) (out InventoryItem, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		if _, e := checkSession(ctx, c, actor, now, false); e != nil {
			return e
		}
		old, e := scanInventoryItem(c.WithContext(ctx).Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where("i.id=? AND i.user_id=? AND i.deleted_at IS NULL", id, actor.User.ID).Row())
		if errors.Is(e, sql.ErrNoRows) {
			return inventoryNotFound
		}
		if e != nil {
			return e
		}
		if old.Version != version {
			return E(409, "VERSION_CONFLICT", "库存批次已被他人修改，请刷新后重试")
		}
		if old.FoodStatus == "disabled" && quantityMilli > old.QuantityMilli {
			return E(409, "FOOD_DISABLED", "食材已停用，只允许减少数量")
		}
		if old.QuantityMilli == quantityMilli && inventoryExpiryEqual(old.ExpiresOn, expiresOn) && old.Note == note {
			out = old
			return nil
		}
		r := c.WithContext(ctx).Table("inventory_items").Where("id=? AND user_id=? AND version=? AND deleted_at IS NULL", id, actor.User.ID, old.Version).Updates(map[string]any{
			"quantity_milli": quantityMilli,
			"expires_on":     expiresOn,
			"note":           note,
			"version":        old.Version + 1,
			"updated_at":     now.Unix(),
		})
		e = r.Error
		if e != nil {
			return e
		}
		if n := r.RowsAffected; n != 1 {
			return E(409, "VERSION_CONFLICT", "库存批次已被他人修改，请刷新后重试")
		}
		out, e = scanInventoryItem(c.WithContext(ctx).Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where("i.id=? AND i.user_id=? AND i.deleted_at IS NULL", id, actor.User.ID).Row())
		return e
	})
	return
}

// DeleteInventory 在写事务内复核会话、校验版本后逻辑删除：deleted_at
// 置为当前时刻且 version+1；行保留批次 ID 供后续记餐关联。
func (s *Store) DeleteInventory(ctx context.Context, actor Session, id, version int64, now time.Time) error {
	return s.WithImmediate(ctx, func(c *gorm.DB) error {
		if _, e := checkSession(ctx, c, actor, now, false); e != nil {
			return e
		}
		var old int64
		e := c.WithContext(ctx).Table("inventory_items").Select("version").Where("id=? AND user_id=? AND deleted_at IS NULL", id, actor.User.ID).Row().Scan(&old)
		if errors.Is(e, sql.ErrNoRows) {
			return inventoryNotFound
		}
		if e != nil {
			return e
		}
		if old != version {
			return E(409, "VERSION_CONFLICT", "库存批次已被他人修改，请刷新后重试")
		}
		r := c.WithContext(ctx).Table("inventory_items").Where("id=? AND user_id=? AND version=? AND deleted_at IS NULL", id, actor.User.ID, old).Updates(map[string]any{
			"deleted_at": now.Unix(),
			"version":    gorm.Expr("version+1"),
			"updated_at": now.Unix(),
		})
		e = r.Error
		if e != nil {
			return e
		}
		if n := r.RowsAffected; n != 1 {
			return E(409, "VERSION_CONFLICT", "库存批次已被他人修改，请刷新后重试")
		}
		return nil
	})
}
