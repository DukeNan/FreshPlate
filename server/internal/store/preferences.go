package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

func readPreferences(ctx context.Context, q *gorm.DB, userID int64) (p Preferences, err error) {
	var a, b, d string
	var updated int64
	err = q.WithContext(ctx).Table("user_preferences").Select("allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g,updated_at").Where("user_id=?", userID).Row().Scan(&a, &b, &d, &p.DailyEnergyKcal, &p.DailyProteinG, &p.DailyFatG, &p.DailyCarbohydrateG, &updated)
	if err == sql.ErrNoRows {
		return p, fmt.Errorf("missing user preferences")
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(a), &p.Allergens); err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(b), &p.ExcludedFoodIDs); err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(d), &p.DietaryPreferences); err != nil {
		return p, err
	}
	if p.Allergens == nil || p.ExcludedFoodIDs == nil || p.DietaryPreferences == nil {
		return p, fmt.Errorf("corrupt preferences arrays")
	}
	p.UpdatedAt = time.Unix(updated, 0).UTC()
	p.ExcludedFoods = []Food{}
	for _, id := range p.ExcludedFoodIDs {
		var f Food
		err = q.WithContext(ctx).Table("foods").Select("id,name,status").Where("id=?", id).Row().Scan(&f.ID, &f.Name, &f.Status)
		if err != nil {
			return p, fmt.Errorf("invalid excluded food reference: %w", err)
		}
		p.ExcludedFoods = append(p.ExcludedFoods, f)
	}
	return p, nil
}
func (s *Store) Preferences(ctx context.Context, id int64) (Preferences, error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err := tx.Error
	if err != nil {
		return Preferences{}, mapError(err)
	}
	defer tx.Rollback()
	p, err := readPreferences(ctx, tx, id)
	if err != nil {
		return p, mapError(err)
	}
	return p, mapError(tx.Commit().Error)
}
func (s *Store) SavePreferences(ctx context.Context, session Session, p Preferences, now time.Time) (out Preferences, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		u, e := checkSession(ctx, c, session, now, false)
		if e != nil {
			return e
		}
		if u.MustChangePassword {
			return E(403, "PASSWORD_CHANGE_REQUIRED", "请先修改密码")
		}
		old, e := readPreferences(ctx, c, u.ID)
		if e != nil {
			return e
		}
		existing := make(map[int64]bool, len(old.ExcludedFoodIDs))
		for _, id := range old.ExcludedFoodIDs {
			existing[id] = true
		}
		for _, id := range p.ExcludedFoodIDs {
			var status string
			e = c.WithContext(ctx).Table("foods").Select("status").Where("id=?", id).Row().Scan(&status)
			if e == sql.ErrNoRows {
				return E(400, "INVALID_ARGUMENT", "忌口食材不存在")
			}
			if e != nil {
				return e
			}
			if status != "active" && !existing[id] {
				return E(400, "INVALID_ARGUMENT", "不能新增停用的忌口食材")
			}
		}
		a, e := json.Marshal(p.Allergens)
		if e != nil {
			return e
		}
		b, e := json.Marshal(p.ExcludedFoodIDs)
		if e != nil {
			return e
		}
		d, e := json.Marshal(p.DietaryPreferences)
		if e != nil {
			return e
		}
		e = c.WithContext(ctx).Table("user_preferences").Where("user_id=?", u.ID).Updates(map[string]any{
			"allergens_json":           string(a),
			"excluded_food_ids_json":   string(b),
			"dietary_preferences_json": string(d),
			"daily_energy_kcal":        p.DailyEnergyKcal,
			"daily_protein_g":          p.DailyProteinG,
			"daily_fat_g":              p.DailyFatG,
			"daily_carbohydrate_g":     p.DailyCarbohydrateG,
			"updated_at":               now.Unix(),
		}).Error
		if e != nil {
			return e
		}
		out, e = readPreferences(ctx, c, u.ID)
		return e
	})
	return
}
func (s *Store) Foods(ctx context.Context) ([]Food, error) {
	rows, err := s.orm.WithContext(ctx).Table("foods").Select("id,name,status").Where("status='active'").Order("id").Rows()
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := []Food{}
	for rows.Next() {
		var f Food
		if err = rows.Scan(&f.ID, &f.Name, &f.Status); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
