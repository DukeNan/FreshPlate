package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// RecommendationCandidate carries only the snapshot fields needed by the pure matcher.
type RecommendationCandidate struct {
	ID                            int64
	Name, Visibility              string
	Servings, DurationMinutes     int
	Version                       int64
	MetadataComplete, NameMatches bool
	Allergens                     []string
	Ingredients                   []RecommendationIngredient
}
type RecommendationIngredient struct {
	Food                     Food
	QuantityMilli            int64
	Unit                     string
	AllergenComplete         bool
	Allergens                []string
	DensityGml, PieceWeightG *float64
}
type RecommendationInput struct {
	Preferences Preferences
	Candidates  []RecommendationCandidate
	Inventory   []InventoryItem
}

// RecommendationSnapshot uses five batch queries regardless of candidate/ingredient counts.
// Date and active-food filtering are deliberately left to the pure matcher.
func (s *Store) RecommendationSnapshot(ctx context.Context, userID int64, q string) (out RecommendationInput, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		return out, mapError(tx.Error)
	}
	defer tx.Rollback()
	defer func() { err = mapError(err) }()
	var a, b, d string
	p := &out.Preferences
	err = tx.Table("user_preferences").Select("allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g").Where("user_id=?", userID).Row().Scan(&a, &b, &d, &p.DailyEnergyKcal, &p.DailyProteinG, &p.DailyFatG, &p.DailyCarbohydrateG)
	if err != nil {
		return out, mapError(err)
	}
	for _, v := range []struct {
		s    string
		dest any
	}{{a, &p.Allergens}, {b, &p.ExcludedFoodIDs}, {d, &p.DietaryPreferences}} {
		if err = json.Unmarshal([]byte(v.s), v.dest); err != nil {
			return out, err
		}
	}
	if p.Allergens == nil || p.ExcludedFoodIDs == nil || p.DietaryPreferences == nil {
		return out, E(500, "INTERNAL_ERROR", "Invalid preference arrays")
	}
	rows, err := tx.Table("inventory_items i").Select(inventoryDetailColumns).Joins("JOIN foods f ON f.id=i.food_id").Where("i.user_id=? AND i.deleted_at IS NULL AND i.quantity_milli>0", userID).Rows()
	if err != nil {
		return out, mapError(err)
	}
	for rows.Next() {
		it, e := scanInventoryItem(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Inventory = append(out.Inventory, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, mapError(err)
	}
	const visible = "r.status='published' AND (r.visibility='public' OR (r.visibility='private' AND r.owner_user_id=?))"
	rows, err = tx.Table("recipes r").Select("r.id,r.name,r.visibility,r.servings,r.duration_minutes,r.version,r.allergen_complete,instr(r.name,?)>0", q).Where(visible, userID).Order("r.id").Rows()
	if err != nil {
		return out, mapError(err)
	}
	indices := map[int64]int{}
	for rows.Next() {
		var c RecommendationCandidate
		if err = rows.Scan(&c.ID, &c.Name, &c.Visibility, &c.Servings, &c.DurationMinutes, &c.Version, &c.MetadataComplete, &c.NameMatches); err != nil {
			rows.Close()
			return out, err
		}
		indices[c.ID] = len(out.Candidates)
		out.Candidates = append(out.Candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, mapError(err)
	}
	rows, err = tx.Table("recipe_ingredients ri").Select("ri.recipe_id,f.id,f.name,f.status,ri.quantity_milli,ri.unit,f.allergen_complete,f.allergens_json,f.density_g_ml,f.piece_weight_g").Joins("JOIN recipes r ON r.id=ri.recipe_id").Joins("JOIN foods f ON f.id=ri.food_id").Where(visible, userID).Order("ri.recipe_id,ri.position").Rows()
	if err != nil {
		return out, mapError(err)
	}
	for rows.Next() {
		var id int64
		var it RecommendationIngredient
		var allergens string
		if err = rows.Scan(&id, &it.Food.ID, &it.Food.Name, &it.Food.Status, &it.QuantityMilli, &it.Unit, &it.AllergenComplete, &allergens, &it.DensityGml, &it.PieceWeightG); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal([]byte(allergens), &it.Allergens); err != nil {
			rows.Close()
			return out, err
		}
		i := indices[id]
		out.Candidates[i].Ingredients = append(out.Candidates[i].Ingredients, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, mapError(err)
	}
	rows, err = tx.Table("recipe_allergens ra").Select("ra.recipe_id,ra.allergen_code").Joins("JOIN recipes r ON r.id=ra.recipe_id").Where(visible, userID).Rows()
	if err != nil {
		return out, mapError(err)
	}
	for rows.Next() {
		var id int64
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			rows.Close()
			return out, err
		}
		i := indices[id]
		out.Candidates[i].Allergens = append(out.Candidates[i].Allergens, code)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, mapError(err)
	}
	return out, mapError(tx.Commit().Error)
}
