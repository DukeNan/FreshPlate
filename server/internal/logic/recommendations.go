package logic

import (
	"context"
	"math"
	"math/bits"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"freshplate/server/internal/store"
)

const recommendationCap int64 = 1000000000000000

type RecommendationRecipe struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Visibility      string `json:"visibility"`
	Servings        int    `json:"servings"`
	DurationMinutes int    `json:"duration_minutes"`
	Version         int64  `json:"version"`
}
type RecommendationIngredient struct {
	Food  store.Food `json:"food"`
	State string     `json:"state"`
	Unit  string     `json:"unit"`
	Need  string     `json:"need"`
	Have  *string    `json:"have" extensions:"x-nullable"`
	Lack  *string    `json:"lack" extensions:"x-nullable"`
}
type Recommendation struct {
	Recipe       RecommendationRecipe       `json:"recipe"`
	CanCook      bool                       `json:"can_cook"`
	MissingCount int                        `json:"missing_count"`
	UsesExpiring bool                       `json:"uses_expiring"`
	Ingredients  []RecommendationIngredient `json:"ingredients"`
}
type RecommendationStats struct {
	Candidates                 int  `json:"candidates"`
	FilteredByRestrictions     int  `json:"filtered_by_restrictions"`
	FilteredIncompleteMetadata int  `json:"filtered_incomplete_metadata"`
	FilteredDisabledReference  int  `json:"filtered_disabled_reference"`
	HasUsableInventory         bool `json:"has_usable_inventory"`
}
type RecommendationPage struct {
	Items []Recommendation    `json:"items"`
	Total int                 `json:"total"`
	Stats RecommendationStats `json:"stats"`
}

func ValidateRecommendationQuery(q, availability string) (string, string, error) {
	q = strings.TrimSpace(q)
	if availability == "" {
		availability = "all"
	}
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 100 || (availability != "all" && availability != "can_cook") {
		return "", "", ErrInvalidArgument
	}
	return q, availability, nil
}

func recommendationCoefficient(v *float64, max uint64) (uint64, bool) {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return 0, false
	}
	n := math.Round(*v * 1000000)
	if n < 1 || n > float64(max) {
		return 0, false
	}
	return uint64(n), true
}

// convertRecommendationQuantity rounds only at the destination unit. Comparing the
// 128-bit cap threshold before division also protects Div64 from quotient overflow.
func convertRecommendationQuantity(q int64, from, to string, density, pieceWeight *float64) (int64, bool) {
	if from == to {
		return q, true
	}
	den, denOK := recommendationCoefficient(density, 100000000)
	pw, pwOK := recommendationCoefficient(pieceWeight, 1000000000000)
	var mul, div uint64
	switch from + ">" + to {
	case "piece>g":
		if !pwOK {
			return 0, false
		}
		mul, div = pw, 1000000
	case "g>piece":
		if !pwOK {
			return 0, false
		}
		mul, div = 1000000, pw
	case "g>ml":
		if !denOK {
			return 0, false
		}
		mul, div = 1000000, den
	case "ml>g":
		if !denOK {
			return 0, false
		}
		mul, div = den, 1000000
	case "piece>ml":
		if !pwOK || !denOK {
			return 0, false
		}
		mul, div = pw, den
	case "ml>piece":
		if !pwOK || !denOK {
			return 0, false
		}
		mul, div = den, pw
	default:
		return 0, false
	}
	hi, lo := bits.Mul64(uint64(q), mul)
	capHi, capLo := bits.Mul64(uint64(recommendationCap), div)
	if hi > capHi || (hi == capHi && lo >= capLo) {
		return recommendationCap, true
	}
	n, _ := bits.Div64(hi, lo, div)
	return int64(n), true
}

// BuildRecommendations performs no IO; its input is one store read snapshot.
func BuildRecommendations(input store.RecommendationInput, today time.Time, availability string, page, size int) (RecommendationPage, error) {
	out := RecommendationPage{Items: []Recommendation{}}
	p, err := ValidatePreferences(input.Preferences)
	if err != nil {
		return out, err
	}
	today = UtcToday(today)
	inventory := map[int64][]store.InventoryItem{}
	for _, batch := range input.Inventory {
		if batch.QuantityMilli <= 0 || batch.FoodStatus != "active" || ExpiryState(batch.QuantityMilli, batch.ExpiresOn, today) == "expired" {
			continue
		}
		inventory[batch.FoodID] = append(inventory[batch.FoodID], batch)
		out.Stats.HasUsableInventory = true
	}
	items := []Recommendation{}
	for _, c := range input.Candidates {
		if len(c.Ingredients) == 0 {
			continue
		}
		out.Stats.Candidates++
		complete, disabled := c.MetadataComplete, false
		ids := make([]int64, 0, len(c.Ingredients))
		allergens := append([]string(nil), c.Allergens...)
		for _, i := range c.Ingredients {
			complete = complete && i.AllergenComplete
			disabled = disabled || i.Food.Status != "active"
			ids = append(ids, i.Food.ID)
			allergens = append(allergens, i.Allergens...)
		}
		if !complete {
			out.Stats.FilteredIncompleteMetadata++
			continue
		}
		if EnforceRestrictions(p, ids, allergens, true) != nil {
			out.Stats.FilteredByRestrictions++
			continue
		}
		if disabled {
			out.Stats.FilteredDisabledReference++
			continue
		}
		if !c.NameMatches {
			continue
		}
		item := Recommendation{Recipe: RecommendationRecipe{ID: c.ID, Name: c.Name, Visibility: c.Visibility, Servings: c.Servings, DurationMinutes: c.DurationMinutes, Version: c.Version}, CanCook: true, Ingredients: []RecommendationIngredient{}}
		for _, i := range c.Ingredients {
			var confirmed int64
			unknown := false
			for _, batch := range inventory[i.Food.ID] {
				if ExpiryState(batch.QuantityMilli, batch.ExpiresOn, today) == "expiring" {
					item.UsesExpiring = true
				}
				n, ok := convertRecommendationQuantity(batch.QuantityMilli, batch.Unit, i.Unit, i.DensityGml, i.PieceWeightG)
				if !ok {
					unknown = true
					continue
				}
				confirmed += min(n, recommendationCap-confirmed)
			}
			have := FormatQuantity(confirmed)
			match := RecommendationIngredient{Food: i.Food, Unit: i.Unit, Need: FormatQuantity(i.QuantityMilli), Have: &have}
			switch {
			case confirmed >= i.QuantityMilli:
				match.State = "ok"
			case unknown:
				match.State = "unknown"
				match.Have = nil
			case confirmed > 0:
				match.State = "short"
				lack := FormatQuantity(i.QuantityMilli - confirmed)
				match.Lack = &lack
			default:
				match.State = "missing"
			}
			if match.State != "ok" {
				item.CanCook = false
				item.MissingCount++
			}
			item.Ingredients = append(item.Ingredients, match)
		}
		if availability == "can_cook" && !item.CanCook {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.MissingCount != b.MissingCount {
			return a.MissingCount < b.MissingCount
		}
		if a.UsesExpiring != b.UsesExpiring {
			return a.UsesExpiring
		}
		if a.Recipe.DurationMinutes != b.Recipe.DurationMinutes {
			return a.Recipe.DurationMinutes < b.Recipe.DurationMinutes
		}
		return a.Recipe.ID > b.Recipe.ID
	})
	out.Total = len(items)
	start := (page - 1) * size
	if start < len(items) {
		out.Items = items[start:min(start+size, len(items))]
	}
	return out, nil
}

func (s *Service) Recommendations(ctx context.Context, actor store.Session, q, availability string, page, size int) (RecommendationPage, error) {
	q, availability, err := ValidateRecommendationQuery(q, availability)
	if err != nil || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return RecommendationPage{}, ErrInvalidArgument
	}
	today := UtcToday(s.Now())
	input, err := s.Store.RecommendationSnapshot(ctx, actor.User.ID, q)
	if err != nil {
		return RecommendationPage{}, err
	}
	return BuildRecommendations(input, today, availability, page, size)
}
