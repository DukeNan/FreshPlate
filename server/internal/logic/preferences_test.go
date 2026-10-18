package logic

import (
	"freshplate/server/internal/store"
	"math"
	"testing"
)

func TestPREF002Validation(t *testing.T) {
	p, err := ValidatePreferences(store.Preferences{Allergens: []string{"milk", "milk", "egg"}, DietaryPreferences: []string{"vegan", "vegan"}, ExcludedFoodIDs: []int64{1, 1, 2}})
	if err != nil || len(p.Allergens) != 2 || len(p.DietaryPreferences) != 1 || len(p.ExcludedFoodIDs) != 2 {
		t.Fatal(p, err)
	}
	for _, v := range []store.Preferences{{Allergens: []string{"unknown"}}, {DietaryPreferences: []string{"unknown"}}, {Allergens: make([]string, 33)}, {ExcludedFoodIDs: make([]int64, 501)}, {ExcludedFoodIDs: []int64{0}}, {ExcludedFoodIDs: []int64{-1}}} {
		if _, err := ValidatePreferences(v); err == nil {
			t.Fatal("invalid accepted", v)
		}
	}
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -1, 20000.1} {
		if _, err := ValidatePreferences(store.Preferences{DailyEnergyKcal: &n}); err == nil {
			t.Fatal(n)
		}
	}
	for _, n := range []float64{0, -1, 2000.1} {
		for _, p := range []store.Preferences{{DailyProteinG: &n}, {DailyFatG: &n}, {DailyCarbohydrateG: &n}} {
			if _, err := ValidatePreferences(p); err == nil {
				t.Fatal(n)
			}
		}
	}
	energy, macro := 20000., 2000.
	if _, err := ValidatePreferences(store.Preferences{DailyEnergyKcal: &energy, DailyProteinG: &macro, DailyFatG: &macro, DailyCarbohydrateG: &macro}); err != nil {
		t.Fatal(err)
	}
	empty, err := ValidatePreferences(store.Preferences{})
	if err != nil || empty.Allergens == nil || empty.ExcludedFoodIDs == nil || empty.DietaryPreferences == nil {
		t.Fatal(empty, err)
	}
}
func TestREC001RestrictionReuse(t *testing.T) {
	p := store.Preferences{Allergens: []string{"milk"}, ExcludedFoodIDs: []int64{42}, DietaryPreferences: []string{"vegan"}}
	for _, v := range []struct {
		foods     []int64
		allergens []string
		complete  bool
	}{{[]int64{42}, nil, true}, {nil, []string{"milk"}, true}, {nil, []string{"unknown"}, true}, {nil, nil, false}} {
		if EnforceRestrictions(p, v.foods, v.allergens, v.complete) == nil {
			t.Fatal("restriction bypassed")
		}
	}
	if err := EnforceRestrictions(p, []int64{1}, []string{"egg"}, true); err != nil {
		t.Fatal("sorting preference used as restriction", err)
	}
}
