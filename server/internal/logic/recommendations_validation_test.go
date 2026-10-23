package logic

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"freshplate/server/internal/store"
)

// Independent exact-rational oracle checks all unit directions, including large
// intermediates; it does not repeat the production Mul64/Div64 implementation.
// RECO-LOGIC-007
func TestValidationRecommendationConversionOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20261023))
	units := []string{"g", "ml", "piece"}
	for _, from := range units {
		for _, to := range units {
			for n := 0; n < 300; n++ {
				q := int64(rng.Intn(1000000000) + 1)
				denInt := int64(rng.Intn(100000000) + 1)
				pwInt := int64(rng.Int63n(1000000000000) + 1)
				density, pw := float64(denInt)/1000000, float64(pwInt)/1000000
				grams := map[string]*big.Rat{
					"g": big.NewRat(1, 1), "ml": big.NewRat(denInt, 1000000), "piece": big.NewRat(pwInt, 1000000),
				}
				exact := new(big.Rat).Mul(big.NewRat(q, 1), grams[from])
				exact.Quo(exact, grams[to])
				want := new(big.Int).Quo(exact.Num(), exact.Denom())
				if want.Cmp(big.NewInt(1000000000000000)) > 0 {
					want.SetInt64(1000000000000000)
				}
				got, ok := convertRecommendationQuantity(q, from, to, &density, &pw)
				if !ok || got != want.Int64() {
					t.Fatalf("%s>%s q=%d den=%d pw=%d got=%d/%t want=%s", from, to, q, denInt, pwInt, got, ok, want)
				}
			}
		}
	}
}

// RECO-LOGIC-008
func TestValidationRecommendationSaturationAndInvariants(t *testing.T) {
	today := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	density, pw := 0.000001, 1000000.0
	candidate := store.RecommendationCandidate{ID: 1, MetadataComplete: true, NameMatches: true, Ingredients: []store.RecommendationIngredient{{Food: store.Food{ID: 1, Status: "active"}, QuantityMilli: 1000, Unit: "ml", AllergenComplete: true, DensityGml: &density, PieceWeightG: &pw}}}
	input := store.RecommendationInput{Candidates: []store.RecommendationCandidate{candidate}}
	for i := 0; i < 9224; i++ {
		input.Inventory = append(input.Inventory, store.InventoryItem{FoodID: 1, FoodStatus: "active", QuantityMilli: 1000000000, Unit: "piece"})
	}
	result, err := BuildRecommendations(input, today, "all", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	item := result.Items[0]
	if !item.CanCook || item.MissingCount != 0 || *item.Ingredients[0].Have != "1000000000000" {
		t.Fatalf("saturation or invariant: %+v %+v", item, item.Ingredients[0])
	}
	// Per-batch flooring: two 0.5 milli contributions must not become one milli.
	pw = 0.5
	candidate.Ingredients[0].Unit = "g"
	candidate.Ingredients[0].QuantityMilli = 1
	input.Candidates[0] = candidate
	input.Inventory = []store.InventoryItem{{FoodID: 1, FoodStatus: "active", QuantityMilli: 1, Unit: "piece"}, {FoodID: 1, FoodStatus: "active", QuantityMilli: 1, Unit: "piece"}}
	result, err = BuildRecommendations(input, today, "all", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	item = result.Items[0]
	if item.CanCook || item.MissingCount != 1 || item.Ingredients[0].State != "missing" || *item.Ingredients[0].Have != "0" {
		t.Fatalf("flooring or invariant: %+v %+v", item, item.Ingredients[0])
	}
}
