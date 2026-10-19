package logic

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"freshplate/server/internal/store"
)

// 库存模块业务规则（方案 §3/§4/§5）：数量契约复用 quantity.go 的
// ParseQuantityMilli/FormatQuantity；创建数量必须 >0，编辑允许 0 表示
// 用完；到期日可空；备注 TrimSpace 后至多 500 码点；expiry_state 只派生
// 不持久化；today 每次请求从服务时钟取一次 UTC 日历日，贯穿 SQL 筛选
// 与响应派生。

// inventoryNoteMaxRunes 是备注码点上限（按 Unicode 码点计数而非字节）。
const inventoryNoteMaxRunes = 500

// inventoryQueryMaxRunes 是列表 q 码点上限。
const inventoryQueryMaxRunes = 100

// inventoryUnits 是库存可录入的原始单位；换算未知不阻止录入。
var inventoryUnits = map[string]bool{"g": true, "ml": true, "piece": true}

// inventoryStates 是列表 expiry_state 筛选允许的取值，与派生值一致。
var inventoryStates = map[string]bool{"depleted": true, "expired": true, "expiring": true, "fresh": true, "unknown": true}

// UtcToday 把任意时刻截断为当日 UTC 零点日历日（方案 §3.3：统一按 UTC
// 日期判断，不依赖服务器本地时区）。
func UtcToday(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ExpiryState 从数量与到期日派生响应状态（纯函数，不读时钟、不持久化）：
// 数量 0 一律 depleted（优先于一切日期状态）；无日期 unknown；
// date<today expired；today≤date≤today+3 expiring（到期日当天仍算
// "今日到期"）；其余 fresh。today 必须为 UtcToday 截断值；日期比较基于
// 规范 YYYY-MM-DD 字符串的字典序。
func ExpiryState(quantityMilli int64, expiresOn *string, today time.Time) string {
	if quantityMilli == 0 {
		return "depleted"
	}
	if expiresOn == nil {
		return "unknown"
	}
	d := today.Format(expiryDateLayout)
	d3 := today.AddDate(0, 0, 3).Format(expiryDateLayout)
	switch {
	case *expiresOn < d:
		return "expired"
	case *expiresOn <= d3:
		return "expiring"
	default:
		return "fresh"
	}
}

// ValidateInventoryQuery 校验列表筛选参数：q TrimSpace 后至多 100 码点；
// expiry_state 仅接受五个派生值或空；page/size 与全站分页上限一致。
// 返回裁剪后的 q。所有非法输入统一返回 ErrInvalidArgument。
func ValidateInventoryQuery(q, expiryState string, page, size int) (string, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > inventoryQueryMaxRunes {
		return "", ErrInvalidArgument
	}
	if expiryState != "" && !inventoryStates[expiryState] {
		return "", ErrInvalidArgument
	}
	if page < 1 || page > 1000000 {
		return "", ErrInvalidArgument
	}
	if size < 1 || size > 100 {
		return "", ErrInvalidArgument
	}
	return q, nil
}

// validateInventoryNote TrimSpace 后限制码点数；空串合法（note 默认空）。
func validateInventoryNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > inventoryNoteMaxRunes {
		return "", ErrInvalidArgument
	}
	return note, nil
}

// normalizeInventoryExpiresOn 校验可空到期日并输出规范字符串；nil 直通
// 表示未知。ParseExpiryDate 已保证输出与输入逐字符一致。
func normalizeInventoryExpiresOn(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t, err := ParseExpiryDate(*v)
	if err != nil {
		return nil, err
	}
	s := t.Format(expiryDateLayout)
	return &s, nil
}

// InventoryList 返回本人库存分页；today 从服务时钟取一次 UTC 日期，
// 贯穿 SQL 筛选与响应派生——同一次请求内时钟中途变化不影响结果
// 一致性（INV-LOGIC-026）。
func (s *Service) InventoryList(ctx context.Context, actor store.Session, q string, foodID int64, expiryState string, page, size int) (items []store.InventoryItem, total int, today time.Time, err error) {
	q, err = ValidateInventoryQuery(q, expiryState, page, size)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	now := s.Now()
	today = UtcToday(now)
	items, total, err = s.Store.ListInventory(ctx, actor.User.ID, q, foodID, expiryState, page, size, today.Format(expiryDateLayout))
	return
}

// Inventory 返回本人单批次详情；停用食材的批次同样可读（保留名称与
// 停用标记，INV-API-023）。
func (s *Service) Inventory(ctx context.Context, actor store.Session, id int64) (store.InventoryItem, time.Time, error) {
	if id <= 0 {
		return store.InventoryItem{}, time.Time{}, ErrInvalidArgument
	}
	now := s.Now()
	item, err := s.Store.InventoryByID(ctx, actor, id, now)
	return item, UtcToday(now), err
}

// CreateInventory 创建批次：数量解析后必须 >0（编辑期才允许 0 表示
// 用完，INV-LOGIC-034）；单位仅 g/ml/piece；food active 在 store 写事务
// 内复核，防止与并发停用竞争。
func (s *Service) CreateInventory(ctx context.Context, actor store.Session, foodID int64, unit, quantity string, expiresOn *string, note string) (store.InventoryItem, time.Time, error) {
	if foodID <= 0 {
		return store.InventoryItem{}, time.Time{}, ErrInvalidArgument
	}
	if !inventoryUnits[unit] {
		return store.InventoryItem{}, time.Time{}, ErrInvalidArgument
	}
	milli, err := ParseQuantityMilli(quantity)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	if milli <= 0 {
		return store.InventoryItem{}, time.Time{}, ErrInvalidArgument
	}
	expiry, err := normalizeInventoryExpiresOn(expiresOn)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	note, err = validateInventoryNote(note)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	now := s.Now()
	item, err := s.Store.CreateInventory(ctx, actor, foodID, milli, unit, expiry, note, now)
	return item, UtcToday(now), err
}

// UpdateInventory 全量替换可变字段：编辑允许数量 0 表示用完；version
// 为期望旧版本。food 停用只减规则与版本条件更新由 store 在写事务内执行。
func (s *Service) UpdateInventory(ctx context.Context, actor store.Session, id, version int64, quantity string, expiresOn *string, note string) (store.InventoryItem, time.Time, error) {
	if id <= 0 || version <= 0 {
		return store.InventoryItem{}, time.Time{}, ErrInvalidArgument
	}
	milli, err := ParseQuantityMilli(quantity)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	expiry, err := normalizeInventoryExpiresOn(expiresOn)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	note, err = validateInventoryNote(note)
	if err != nil {
		return store.InventoryItem{}, time.Time{}, err
	}
	now := s.Now()
	item, err := s.Store.UpdateInventory(ctx, actor, id, version, milli, expiry, note, now)
	return item, UtcToday(now), err
}

// DeleteInventory 校验 version 后逻辑删除本人批次；不存在/已删除/他人
// 资源统一 404。
func (s *Service) DeleteInventory(ctx context.Context, actor store.Session, id, version int64) error {
	if id <= 0 || version <= 0 {
		return ErrInvalidArgument
	}
	return s.Store.DeleteInventory(ctx, actor, id, version, s.Now())
}
