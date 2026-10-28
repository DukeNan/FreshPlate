package logic

import (
	"time"

	"freshplate/server/internal/calendar"
	"freshplate/server/internal/store"
)

type NutritionDay struct {
	Date       string   `json:"date"`
	MealsCount int      `json:"meals_count"`
	EnergyKcal *float64 `json:"energy_kcal"`
	ProteinG   *float64 `json:"protein_g"`
	FatG       *float64 `json:"fat_g"`
	CarbsG     *float64 `json:"carbs_g"`
	Complete   bool     `json:"complete"`
}

// NutritionDays totals only complete days; unknown data is never treated as zero.
func NutritionDays(end time.Time, items []store.NutritionItem) []NutritionDay {
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, calendar.China)
	days := make([]NutritionDay, 7)
	seen := make(map[int64]bool)
	for i := range days {
		zero := [4]float64{}
		days[i] = NutritionDay{Date: end.AddDate(0, 0, i-6).Format("2006-01-02"), EnergyKcal: &zero[0], ProteinG: &zero[1], FatG: &zero[2], CarbsG: &zero[3], Complete: true}
	}
	for _, item := range items {
		date := time.Unix(item.AteAt, 0).In(calendar.China).Format("2006-01-02")
		for i := range days {
			day := &days[i]
			if day.Date != date {
				continue
			}
			if !seen[item.MealID] {
				day.MealsCount++
				seen[item.MealID] = true
			}
			grams := float64(item.QuantityMilli) / 1000
			switch item.Unit {
			case "g":
			case "ml":
				if item.DensityGml == nil {
					day.Complete = false
					continue
				}
				grams *= *item.DensityGml
			case "piece":
				if item.PieceWeightG == nil {
					day.Complete = false
					continue
				}
				grams *= *item.PieceWeightG
			default:
				day.Complete = false
				continue
			}
			if item.EnergyKcal100g == nil || item.ProteinG100g == nil || item.FatG100g == nil || item.CarbohydrateG100g == nil || item.CookedServingsMilli <= 0 {
				day.Complete = false
				continue
			}
			scale := grams / 100 * float64(item.EatenServingsMilli) / float64(item.CookedServingsMilli)
			*day.EnergyKcal += *item.EnergyKcal100g * scale
			*day.ProteinG += *item.ProteinG100g * scale
			*day.FatG += *item.FatG100g * scale
			*day.CarbsG += *item.CarbohydrateG100g * scale
		}
	}
	for i := range days {
		if !days[i].Complete {
			days[i].EnergyKcal = nil
			days[i].ProteinG = nil
			days[i].FatG = nil
			days[i].CarbsG = nil
		}
	}
	return days
}
