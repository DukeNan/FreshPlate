package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// foodDetailColumns 与 db.go 启动校验使用的 foodColumns 保持同一列集合；
// 两处必须同步修改。
const foodDetailColumns = "id,name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,density_g_ml,piece_weight_g,conversion_source,allergens_json,allergen_complete,allergen_source,version,created_at,updated_at"

// FoodDetail 是食材完整详情，含全部业务列。可空列使用指针区分 NULL（未知）
// 与零值（有来源证明的 0）；Allergens 由 allergens_json JSON 解码；
// 时间一律为 UTC。
type FoodDetail struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	EnergyKcal100g     *float64  `json:"energy_kcal_100g"`
	ProteinG100g       *float64  `json:"protein_g_100g"`
	FatG100g           *float64  `json:"fat_g_100g"`
	CarbohydrateG100g  *float64  `json:"carbohydrate_g_100g"`
	NutritionSource    *string   `json:"nutrition_source"`
	NutritionReference *string   `json:"nutrition_reference"`
	DensityGml         *float64  `json:"density_g_ml"`
	PieceWeightG       *float64  `json:"piece_weight_g"`
	ConversionSource   *string   `json:"conversion_source"`
	Allergens          []string  `json:"allergens"`
	AllergenComplete   bool      `json:"allergen_complete"`
	AllergenSource     *string   `json:"allergen_source"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// FoodSummary 是目录与管理列表摘要；刻意不携带营养、换算与来源详情，
// 避免旧摘要消费方（如设置页）依赖变化。
type FoodSummary struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Version          int64  `json:"version"`
	AllergenComplete bool   `json:"allergen_complete"`
}

// FoodWrite 是创建/更新共用的可编辑字段集合；id、version、时间戳等服务端
// 字段不在其中，不能由客户端提交。
type FoodWrite struct {
	Name               string
	Status             string
	EnergyKcal100g     *float64
	ProteinG100g       *float64
	FatG100g           *float64
	CarbohydrateG100g  *float64
	NutritionSource    *string
	NutritionReference *string
	DensityGml         *float64
	PieceWeightG       *float64
	ConversionSource   *string
	Allergens          []string
	AllergenComplete   bool
	AllergenSource     *string
}

// foodCreateFields 是创建审计 details 记录的输入字段名，顺序固定；
// 更新审计的变更字段列表也沿用同一顺序。
var foodCreateFields = []string{"name", "status", "energy_kcal_100g", "protein_g_100g", "fat_g_100g", "carbohydrate_g_100g", "nutrition_source", "nutrition_reference", "density_g_ml", "piece_weight_g", "conversion_source", "allergens", "allergen_complete", "allergen_source"}

func scanFoodDetail(r scanner) (FoodDetail, error) {
	var f FoodDetail
	var allergens string
	var created, updated int64
	err := r.Scan(&f.ID, &f.Name, &f.Status, &f.EnergyKcal100g, &f.ProteinG100g, &f.FatG100g, &f.CarbohydrateG100g, &f.NutritionSource, &f.NutritionReference, &f.DensityGml, &f.PieceWeightG, &f.ConversionSource, &allergens, &f.AllergenComplete, &f.AllergenSource, &f.Version, &created, &updated)
	if err != nil {
		return f, err
	}
	if err = json.Unmarshal([]byte(allergens), &f.Allergens); err != nil {
		return f, err
	}
	if f.Allergens == nil {
		f.Allergens = []string{}
	}
	f.CreatedAt = time.Unix(created, 0).UTC()
	f.UpdatedAt = time.Unix(updated, 0).UTC()
	return f, nil
}

// foodArgs 组装 INSERT/UPDATE 共用的可编辑列参数（不含 version 与时间戳）。
func foodArgs(in FoodWrite) ([]any, error) {
	allergens, err := json.Marshal(in.Allergens)
	if err != nil {
		return nil, err
	}
	return []any{in.Name, in.Status, in.EnergyKcal100g, in.ProteinG100g, in.FatG100g, in.CarbohydrateG100g, in.NutritionSource, in.NutritionReference, in.DensityGml, in.PieceWeightG, in.ConversionSource, string(allergens), boolInt(in.AllergenComplete), in.AllergenSource}, nil
}

// foodPtrEqual 在 NULL 语义下比较可空标量：仅当两者同为 nil 或指向相同值时相等。
func foodPtrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// foodChanges 返回新旧状态间发生变化的字段名列表，顺序与 foodCreateFields 一致。
func foodChanges(old FoodDetail, in FoodWrite) []string {
	fields := []string{}
	add := func(name string, changed bool) {
		if changed {
			fields = append(fields, name)
		}
	}
	add("name", old.Name != in.Name)
	add("status", old.Status != in.Status)
	add("energy_kcal_100g", !foodPtrEqual(old.EnergyKcal100g, in.EnergyKcal100g))
	add("protein_g_100g", !foodPtrEqual(old.ProteinG100g, in.ProteinG100g))
	add("fat_g_100g", !foodPtrEqual(old.FatG100g, in.FatG100g))
	add("carbohydrate_g_100g", !foodPtrEqual(old.CarbohydrateG100g, in.CarbohydrateG100g))
	add("nutrition_source", !foodPtrEqual(old.NutritionSource, in.NutritionSource))
	add("nutrition_reference", !foodPtrEqual(old.NutritionReference, in.NutritionReference))
	add("density_g_ml", !foodPtrEqual(old.DensityGml, in.DensityGml))
	add("piece_weight_g", !foodPtrEqual(old.PieceWeightG, in.PieceWeightG))
	add("conversion_source", !foodPtrEqual(old.ConversionSource, in.ConversionSource))
	add("allergens", !slices.Equal(old.Allergens, in.Allergens))
	add("allergen_complete", old.AllergenComplete != in.AllergenComplete)
	add("allergen_source", !foodPtrEqual(old.AllergenSource, in.AllergenSource))
	return fields
}

// FoodCatalog 返回 active 食材摘要分页；q 为参数化字面子串匹配，
// 不解释 SQL 通配符。total 与当前页在同一只读事务内读取。
func (s *Store) FoodCatalog(ctx context.Context, q string, page, size int) (items []FoodSummary, total int, err error) {
	return s.listFoods(ctx, q, "active", page, size)
}

// FoodAdminList 返回全部状态食材摘要分页；status 为空不过滤。
func (s *Store) FoodAdminList(ctx context.Context, q, status string, page, size int) (items []FoodSummary, total int, err error) {
	return s.listFoods(ctx, q, status, page, size)
}

// listFoods 是两个列表的共用实现：按 ID 倒序、instr 字面子串匹配。
func (s *Store) listFoods(ctx context.Context, q, status string, page, size int) (items []FoodSummary, total int, err error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := " WHERE 1=1"
	args := []any{}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	if q != "" {
		where += " AND instr(name,?)>0"
		args = append(args, q)
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM foods"+where, args...).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(page, size)
	args = append(args, limit, offset)
	rows, err := tx.QueryContext(ctx, "SELECT id,name,status,version,allergen_complete FROM foods"+where+" ORDER BY id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []FoodSummary{}
	for rows.Next() {
		var f FoodSummary
		if e := rows.Scan(&f.ID, &f.Name, &f.Status, &f.Version, &f.AllergenComplete); e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	err = mapError(tx.Commit())
	return
}

// FoodByID 返回食材完整详情；停用食材同样可读，用于解释既有引用。
func (s *Store) FoodByID(ctx context.Context, id int64) (FoodDetail, error) {
	f, err := scanFoodDetail(s.DB.QueryRowContext(ctx, "SELECT "+foodDetailColumns+" FROM foods WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return FoodDetail{}, E(404, "NOT_FOUND", "食材不存在")
	}
	return f, mapError(err)
}

// CreateFood 在写事务内复核管理员后创建食材（version=1）并写创建审计。
// 名称唯一约束冲突由 mapError 映射为 409 FOOD_NAME_TAKEN。
func (s *Store) CreateFood(ctx context.Context, actor Session, in FoodWrite, now time.Time) (out FoodDetail, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		a, e := checkSession(ctx, c, actor, now, true)
		if e != nil {
			return e
		}
		args, e := foodArgs(in)
		if e != nil {
			return e
		}
		r, e := c.ExecContext(ctx, "INSERT INTO foods(name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,density_g_ml,piece_weight_g,conversion_source,allergens_json,allergen_complete,allergen_source,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)", append(args, now.Unix(), now.Unix())...)
		if e != nil {
			return e
		}
		id, e := r.LastInsertId()
		if e != nil {
			return e
		}
		details := map[string]any{"fields": foodCreateFields, "status_before": nil, "status_after": in.Status, "version_before": nil, "version_after": int64(1)}
		if e = insertResourceAudit(ctx, c, a.ID, "food", id, "food_created", details, now); e != nil {
			return e
		}
		out, e = scanFoodDetail(c.QueryRowContext(ctx, "SELECT "+foodDetailColumns+" FROM foods WHERE id=?", id))
		return e
	})
	return
}

// UpdateFood 在写事务内复核管理员、读取当前版本并与调用方期望版本比较：
// 不一致返回 409；无变化返回原详情，不升版本、不写审计；有变化时以
// id+version 条件更新并要求影响行数恰为 1，随后写审计并回读。
func (s *Store) UpdateFood(ctx context.Context, actor Session, id, version int64, in FoodWrite, now time.Time) (out FoodDetail, err error) {
	err = s.WithImmediate(ctx, func(c *sql.Conn) error {
		a, e := checkSession(ctx, c, actor, now, true)
		if e != nil {
			return e
		}
		old, e := scanFoodDetail(c.QueryRowContext(ctx, "SELECT "+foodDetailColumns+" FROM foods WHERE id=?", id))
		if errors.Is(e, sql.ErrNoRows) {
			return E(404, "NOT_FOUND", "食材不存在")
		}
		if e != nil {
			return e
		}
		if old.Version != version {
			return E(409, "VERSION_CONFLICT", "食材已被他人修改，请刷新后重试")
		}
		fields := foodChanges(old, in)
		if len(fields) == 0 {
			out = old
			return nil
		}
		newVersion := old.Version + 1
		args, e := foodArgs(in)
		if e != nil {
			return e
		}
		args = append(args, newVersion, now.Unix(), id, old.Version)
		r, e := c.ExecContext(ctx, "UPDATE foods SET name=?,status=?,energy_kcal_100g=?,protein_g_100g=?,fat_g_100g=?,carbohydrate_g_100g=?,nutrition_source=?,nutrition_reference=?,density_g_ml=?,piece_weight_g=?,conversion_source=?,allergens_json=?,allergen_complete=?,allergen_source=?,version=?,updated_at=? WHERE id=? AND version=?", args...)
		if e != nil {
			return e
		}
		if n, e := r.RowsAffected(); e != nil {
			return e
		} else if n != 1 {
			return E(409, "VERSION_CONFLICT", "食材已被他人修改，请刷新后重试")
		}
		// 状态变化优先记 disabled/enabled，其余记 updated。
		action := "food_updated"
		if old.Status != in.Status {
			if in.Status == "disabled" {
				action = "food_disabled"
			} else {
				action = "food_enabled"
			}
		}
		details := map[string]any{"fields": fields, "status_before": old.Status, "status_after": in.Status, "version_before": old.Version, "version_after": newVersion}
		if e = insertResourceAudit(ctx, c, a.ID, "food", id, action, details, now); e != nil {
			return e
		}
		out, e = scanFoodDetail(c.QueryRowContext(ctx, "SELECT "+foodDetailColumns+" FROM foods WHERE id=?", id))
		return e
	})
	return
}
