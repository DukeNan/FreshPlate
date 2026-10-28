package logic

import (
	"testing"
	"time"

	"freshplate/server/internal/store"
)

func TestNutritionDays(t *testing.T) {
	end := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	energy, protein, fat, carbs, weight := 200.0, 10.0, 5.0, 20.0, 50.0
	item := store.NutritionItem{MealID: 1, AteAt: end.Unix(), CookedServingsMilli: 2000, EatenServingsMilli: 1000, QuantityMilli: 2000, Unit: "piece", EnergyKcal100g: &energy, ProteinG100g: &protein, FatG100g: &fat, CarbohydrateG100g: &carbs, PieceWeightG: &weight}
	days := NutritionDays(end, []store.NutritionItem{item, item})
	if len(days) != 7 || days[0].Date != "2026-10-21" || days[6].MealsCount != 1 || *days[6].EnergyKcal != 200 || *days[6].ProteinG != 10 {
		t.Fatalf("incorrect total: %+v", days)
	}
	item.PieceWeightG = nil
	days = NutritionDays(end, []store.NutritionItem{item})
	if days[6].Complete || days[6].EnergyKcal != nil || days[6].MealsCount != 1 || !days[0].Complete || *days[0].EnergyKcal != 0 {
		t.Fatalf("unknown value not preserved: %+v", days)
	}
}
