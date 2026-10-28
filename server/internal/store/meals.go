package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

type MealItemInput struct{ InventoryID, Version, QuantityMilli int64 }
type MealInput struct {
	RequestKey, RequestHash                                          string
	RecipeID, RecipeVersion, CookedServingsMilli, EatenServingsMilli int64
	AteAt                                                            time.Time
	Items                                                            []MealItemInput
}
type Meal struct {
	ID                  int64     `json:"id"`
	RecipeID            int64     `json:"recipe_id"`
	RecipeName          string    `json:"recipe_name"`
	RecipeVersion       int64     `json:"recipe_version"`
	CookedServingsMilli int64     `json:"-"`
	EatenServingsMilli  int64     `json:"-"`
	AteAt               time.Time `json:"ate_at"`
	CreatedAt           time.Time `json:"created_at"`
}

func scanMeal(r scanner) (Meal, error) {
	var m Meal
	var ate, created int64
	err := r.Scan(&m.ID, &m.RecipeID, &m.RecipeName, &m.RecipeVersion, &m.CookedServingsMilli, &m.EatenServingsMilli, &ate, &created)
	m.AteAt = time.Unix(ate, 0).UTC()
	m.CreatedAt = time.Unix(created, 0).UTC()
	return m, err
}

const mealColumns = "id,recipe_id,recipe_name,recipe_version,cooked_servings_milli,eaten_servings_milli,ate_at,created_at"

func (s *Store) CreateMeal(ctx context.Context, actor Session, in MealInput, now time.Time, validate func(Preferences, RecipeDetail) error) (out Meal, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		if _, e := checkSession(ctx, c, actor, now, false); e != nil {
			return e
		}
		var existingHash string
		e := c.WithContext(ctx).Table("meal_logs").Select("request_hash").Where("user_id=? AND request_key=?", actor.User.ID, in.RequestKey).Row().Scan(&existingHash)
		if e == nil {
			if existingHash != in.RequestHash {
				return E(409, "IDEMPOTENCY_CONFLICT", "该请求编号已用于其他记录")
			}
			out, e = scanMeal(c.WithContext(ctx).Table("meal_logs").Select(mealColumns).Where("user_id=? AND request_key=?", actor.User.ID, in.RequestKey).Row())
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		row, e := scanRecipeRow(c.WithContext(ctx).Table("recipes").Select(recipeColumns).Where("id=? AND status='published' AND (visibility='public' OR (visibility='private' AND owner_user_id=?))", in.RecipeID, actor.User.ID).Row())
		if errors.Is(e, sql.ErrNoRows) {
			return recipeNotFound
		}
		if e != nil {
			return e
		}
		if row.Version != in.RecipeVersion {
			return E(409, "VERSION_CONFLICT", "食谱已修改，请刷新后重试")
		}
		detail, e := recipeDetailFromTx(ctx, c, row)
		if e != nil {
			return e
		}
		var p Preferences
		var allergens, excluded, dietary string
		e = c.WithContext(ctx).Table("user_preferences").Select("allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g").Where("user_id=?", actor.User.ID).Row().Scan(&allergens, &excluded, &dietary, &p.DailyEnergyKcal, &p.DailyProteinG, &p.DailyFatG, &p.DailyCarbohydrateG)
		if e != nil {
			return e
		}
		for _, v := range []struct {
			raw string
			dst any
		}{{allergens, &p.Allergens}, {excluded, &p.ExcludedFoodIDs}, {dietary, &p.DietaryPreferences}} {
			if e = json.Unmarshal([]byte(v.raw), v.dst); e != nil {
				return e
			}
		}
		if p.Allergens == nil || p.ExcludedFoodIDs == nil || p.DietaryPreferences == nil {
			return E(500, "INTERNAL_ERROR", "饮食设置无效")
		}
		if e = validate(p, detail); e != nil {
			return e
		}
		name, version := row.Name, row.Version
		var id int64
		e = c.WithContext(ctx).Raw("INSERT INTO meal_logs(user_id,request_key,request_hash,recipe_id,recipe_name,recipe_version,cooked_servings_milli,eaten_servings_milli,ate_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?) RETURNING id", actor.User.ID, in.RequestKey, in.RequestHash, in.RecipeID, name, version, in.CookedServingsMilli, in.EatenServingsMilli, in.AteAt.Unix(), now.Unix()).Row().Scan(&id)
		if e != nil {
			return e
		}
		usedFoods := make(map[int64]bool)
		for _, item := range in.Items {
			var foodID, quantity, batchVersion int64
			var foodName, unit, status string
			var expires *string
			var energy, protein, fat, carb, density, piece *float64
			e = c.WithContext(ctx).Raw(`SELECT i.food_id,i.quantity_milli,i.version,i.unit,i.expires_on,f.name,f.status,f.energy_kcal_100g,f.protein_g_100g,f.fat_g_100g,f.carbohydrate_g_100g,f.density_g_ml,f.piece_weight_g FROM inventory_items i JOIN foods f ON f.id=i.food_id WHERE i.id=? AND i.user_id=? AND i.deleted_at IS NULL`, item.InventoryID, actor.User.ID).Row().Scan(&foodID, &quantity, &batchVersion, &unit, &expires, &foodName, &status, &energy, &protein, &fat, &carb, &density, &piece)
			if errors.Is(e, sql.ErrNoRows) {
				return inventoryNotFound
			}
			if e != nil {
				return e
			}
			if batchVersion != item.Version || quantity < item.QuantityMilli {
				return E(409, "VERSION_CONFLICT", "库存已变化或数量不足，请刷新后重试")
			}
			if status != "active" || (expires != nil && *expires < now.UTC().Format("2006-01-02")) {
				return E(409, "FOOD_UNAVAILABLE", "库存食材已停用或过期")
			}
			usedFoods[foodID] = true
			var n int64
			e = c.WithContext(ctx).Table("recipe_ingredients").Where("recipe_id=? AND food_id=?", in.RecipeID, foodID).Count(&n).Error
			if e != nil {
				return e
			}
			if n == 0 {
				return E(400, "INVALID_ARGUMENT", "库存食材不属于此食谱")
			}
			r := c.WithContext(ctx).Table("inventory_items").Where("id=? AND user_id=? AND version=? AND deleted_at IS NULL AND quantity_milli>=?", item.InventoryID, actor.User.ID, item.Version, item.QuantityMilli).Updates(map[string]any{"quantity_milli": quantity - item.QuantityMilli, "version": batchVersion + 1, "updated_at": now.Unix()})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return E(409, "VERSION_CONFLICT", "库存已变化，请刷新后重试")
			}
			e = c.WithContext(ctx).Exec(`INSERT INTO meal_log_items(meal_id,inventory_id,food_id,food_name,quantity_milli,unit,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,density_g_ml,piece_weight_g) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, item.InventoryID, foodID, foodName, item.QuantityMilli, unit, energy, protein, fat, carb, density, piece).Error
			if e != nil {
				return e
			}
		}
		for _, ingredient := range detail.Ingredients {
			if !usedFoods[ingredient.FoodID] {
				return E(400, "INVALID_ARGUMENT", "请为食谱中每种食材选择库存批次")
			}
		}
		out, e = scanMeal(c.WithContext(ctx).Table("meal_logs").Select(mealColumns).Where("id=? AND user_id=?", id, actor.User.ID).Row())
		return e
	})
	return
}

func (s *Store) ListMeals(ctx context.Context, userID int64, page, size int) (items []Meal, total int, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		return nil, 0, mapError(tx.Error)
	}
	defer tx.Rollback()
	var count int64
	if err = tx.Table("meal_logs").Where("user_id=?", userID).Count(&count).Error; err != nil {
		return nil, 0, mapError(err)
	}
	total = int(count)
	limit, offset := pageBounds(page, size)
	rows, e := tx.Table("meal_logs").Select(mealColumns).Where("user_id=?", userID).Order("id DESC").Limit(limit).Offset(offset).Rows()
	if e != nil {
		return nil, 0, mapError(e)
	}
	items = []Meal{}
	for rows.Next() {
		m, e := scanMeal(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	err = mapError(tx.Commit().Error)
	return
}
