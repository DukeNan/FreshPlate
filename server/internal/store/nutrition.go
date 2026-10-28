package store

import (
	"context"
	"time"
)

// NutritionItem contains the values saved when a meal was confirmed.
type NutritionItem struct {
	MealID              int64
	AteAt               int64
	CookedServingsMilli int64
	EatenServingsMilli  int64
	QuantityMilli       int64
	Unit                string
	EnergyKcal100g      *float64 `gorm:"column:energy_kcal_100g"`
	ProteinG100g        *float64 `gorm:"column:protein_g_100g"`
	FatG100g            *float64 `gorm:"column:fat_g_100g"`
	CarbohydrateG100g   *float64 `gorm:"column:carbohydrate_g_100g"`
	DensityGml          *float64 `gorm:"column:density_g_ml"`
	PieceWeightG        *float64
}

func (s *Store) NutritionItems(ctx context.Context, userID int64, start, end time.Time) ([]NutritionItem, error) {
	var rows []NutritionItem
	err := s.orm.WithContext(ctx).Table("meal_logs AS m").Select("m.id AS meal_id, m.ate_at, m.cooked_servings_milli, m.eaten_servings_milli, i.quantity_milli, i.unit, i.energy_kcal_100g, i.protein_g_100g, i.fat_g_100g, i.carbohydrate_g_100g, i.density_g_ml, i.piece_weight_g").Joins("JOIN meal_log_items AS i ON i.meal_id = m.id").Where("m.user_id = ? AND m.ate_at >= ? AND m.ate_at < ?", userID, start.Unix(), end.Unix()).Scan(&rows).Error
	return rows, mapError(err)
}
