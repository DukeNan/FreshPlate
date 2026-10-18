package logic

import (
	"context"
	"freshplate/server/internal/store"
	"math"
)

var allergenCodes = map[string]bool{"milk": true, "egg": true, "fish": true, "crustacean": true, "mollusc": true, "peanut": true, "soy": true, "wheat": true, "tree_nut": true, "sesame": true}
var preferenceCodes = map[string]bool{"vegetarian": true, "vegan": true, "low_salt": true}

func uniqueCodes(input []string, allowed map[string]bool) ([]string, error) {
	if len(input) > 32 {
		return nil, ErrInvalidArgument
	}
	out := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, v := range input {
		if !allowed[v] {
			return nil, ErrInvalidArgument
		}
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out, nil
}
func ValidatePreferences(p store.Preferences) (store.Preferences, error) {
	var err error
	p.Allergens, err = uniqueCodes(p.Allergens, allergenCodes)
	if err != nil {
		return p, err
	}
	p.DietaryPreferences, err = uniqueCodes(p.DietaryPreferences, preferenceCodes)
	if err != nil {
		return p, err
	}
	if len(p.ExcludedFoodIDs) > 500 {
		return p, ErrInvalidArgument
	}
	ids := make([]int64, 0, len(p.ExcludedFoodIDs))
	seen := map[int64]bool{}
	for _, id := range p.ExcludedFoodIDs {
		if id <= 0 {
			return p, ErrInvalidArgument
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	p.ExcludedFoodIDs = ids
	for _, v := range []struct {
		value *float64
		max   float64
	}{{p.DailyEnergyKcal, 20000}, {p.DailyProteinG, 2000}, {p.DailyFatG, 2000}, {p.DailyCarbohydrateG, 2000}} {
		if v.value != nil && (math.IsNaN(*v.value) || math.IsInf(*v.value, 0) || *v.value <= 0 || *v.value > v.max) {
			return p, ErrInvalidArgument
		}
	}
	return p, nil
}
func (s *Service) SavePreferences(ctx context.Context, session store.Session, p store.Preferences) (store.Preferences, error) {
	p, err := ValidatePreferences(p)
	if err != nil {
		return p, err
	}
	return s.Store.SavePreferences(ctx, session, p, s.Now())
}

// EnforceRestrictions is shared by future recommendation, AI substitution and first-time meal confirmation.
// Incomplete allergen metadata fails closed; preferences only influence ranking.
func EnforceRestrictions(p store.Preferences, foodIDs []int64, allergens []string, complete bool) error {
	if _, err := ValidatePreferences(p); err != nil {
		return err
	}
	if !complete {
		return store.E(400, "DIETARY_RESTRICTION", "Allergen information is incomplete")
	}
	excluded := map[int64]bool{}
	for _, id := range p.ExcludedFoodIDs {
		excluded[id] = true
	}
	for _, id := range foodIDs {
		if excluded[id] {
			return store.E(400, "DIETARY_RESTRICTION", "An excluded food is present")
		}
	}
	blocked := map[string]bool{}
	for _, code := range p.Allergens {
		blocked[code] = true
	}
	for _, code := range allergens {
		if !allergenCodes[code] || blocked[code] {
			return store.E(400, "DIETARY_RESTRICTION", "An allergen is restricted or unknown")
		}
	}
	return nil
}
