package logic

import (
	"freshplate/server/internal/store"
	"strings"
	"testing"
	"time"
)

func recFloat(v float64) *float64 { return &v }
// RECO-LOGIC-001
func TestRecommendationConversions(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		q              int64
		den, pw        *float64
		want           int64
		known          bool
	}{
		{"same missing metadata", "g", "g", 123, nil, nil, 123, true},
		{"piece g", "piece", "g", 1000, nil, recFloat(50), 50000, true},
		{"g piece remainder", "g", "piece", 1000, nil, recFloat(50), 20, true},
		{"g ml", "g", "ml", 1000, recFloat(2), nil, 500, true},
		{"ml g", "ml", "g", 1000, recFloat(2), nil, 2000, true},
		{"piece ml", "piece", "ml", 1000, recFloat(2), recFloat(50), 25000, true},
		{"ml piece", "ml", "piece", 1000, recFloat(2), recFloat(50), 40, true},
		{"needs both", "piece", "ml", 1000, nil, recFloat(50), 0, false},
		{"round final only", "piece", "ml", 1, recFloat(.000001), recFloat(.000001), 1, true},
		{"cap boundary", "piece", "g", 1000000000, nil, recFloat(1000000), recommendationCap, true},
		{"composite saturation", "piece", "ml", 1000000000, recFloat(.000001), recFloat(1000000), recommendationCap, true},
		{"intermediate overflow final exact", "piece", "ml", 99999999, recFloat(100), recFloat(1000000), 999999990000, true},
		{"ml piece saturated", "ml", "piece", 1000000000, recFloat(100), recFloat(.000001), recommendationCap, true},
		{"below milli", "g", "piece", 1, nil, recFloat(1000000), 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, known := convertRecommendationQuantity(tt.q, tt.from, tt.to, tt.den, tt.pw)
			if got != tt.want || known != tt.known {
				t.Fatalf("got (%d,%v), want (%d,%v)", got, known, tt.want, tt.known)
			}
		})
	}
}
func recCandidate(id int64) store.RecommendationCandidate {
	return store.RecommendationCandidate{ID: id, Name: "recipe", Visibility: "public", Servings: 1, DurationMinutes: 10, Version: 1, MetadataComplete: true, NameMatches: true, Ingredients: []store.RecommendationIngredient{{Food: store.Food{ID: 1, Name: "food", Status: "active"}, QuantityMilli: 1000, Unit: "g", AllergenComplete: true}}}
}
// RECO-LOGIC-002
func TestRecommendationStatesAndInvariants(t *testing.T) {
	today := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name              string
		batches           []store.InventoryItem
		state, have, lack string
		expiring          bool
	}{
		{name: "empty", state: "missing", have: "0"},
		{name: "exact", batches: []store.InventoryItem{{QuantityMilli: 1000, Unit: "g"}}, state: "ok", have: "1"},
		{name: "one milli short", batches: []store.InventoryItem{{QuantityMilli: 999, Unit: "g"}}, state: "short", have: "0.999", lack: "0.001"},
		{name: "aggregate", batches: []store.InventoryItem{{QuantityMilli: 500, Unit: "g"}, {QuantityMilli: 500, Unit: "g"}}, state: "ok", have: "1"},
		{name: "unknown", batches: []store.InventoryItem{{QuantityMilli: 999, Unit: "g"}, {QuantityMilli: 1, Unit: "ml"}}, state: "unknown"},
		{name: "known sufficient", batches: []store.InventoryItem{{QuantityMilli: 1000, Unit: "g"}, {QuantityMilli: 1, Unit: "ml"}}, state: "ok", have: "1"},
		{name: "zero", batches: []store.InventoryItem{{QuantityMilli: 0, Unit: "g"}}, state: "missing", have: "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for i := range tt.batches {
				tt.batches[i].FoodID = 1
				tt.batches[i].FoodStatus = "active"
			}
			out, err := BuildRecommendations(store.RecommendationInput{Candidates: []store.RecommendationCandidate{recCandidate(1)}, Inventory: tt.batches}, today, "all", 1, 20)
			if err != nil || len(out.Items) != 1 {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			item := out.Items[0]
			m := item.Ingredients[0]
			str := func(v *string) string {
				if v == nil {
					return ""
				}
				return *v
			}
			if m.State != tt.state || str(m.Have) != tt.have || str(m.Lack) != tt.lack {
				t.Fatalf("match %+v have=%s lack=%s", m, str(m.Have), str(m.Lack))
			}
			if item.CanCook != (m.State == "ok") || item.MissingCount != map[bool]int{true: 0, false: 1}[item.CanCook] {
				t.Fatal("invariants failed")
			}
		})
	}
}
// RECO-LOGIC-003
func TestRecommendationSafetyPriorityAndStats(t *testing.T) {
	cs := []store.RecommendationCandidate{}
	for i := int64(1); i <= 6; i++ {
		cs = append(cs, recCandidate(i))
	}
	cs[0].MetadataComplete = false
	cs[0].Allergens = []string{"milk"}
	cs[0].Ingredients[0].Food.Status = "disabled"
	cs[1].Allergens = []string{"unknown"}
	cs[1].Ingredients[0].Food.Status = "disabled"
	cs[2].Ingredients[0].Food.Status = "disabled"
	cs[3].Ingredients = nil
	cs[4].NameMatches = false
	for _, availability := range []string{"all", "can_cook"} {
		out, err := BuildRecommendations(store.RecommendationInput{Candidates: cs, Preferences: store.Preferences{Allergens: []string{"milk"}}}, time.Now(), availability, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		if out.Stats.Candidates != 5 || out.Stats.FilteredIncompleteMetadata != 1 || out.Stats.FilteredByRestrictions != 1 || out.Stats.FilteredDisabledReference != 1 {
			t.Fatalf("stats %+v", out.Stats)
		}
	}
	if _, err := BuildRecommendations(store.RecommendationInput{Preferences: store.Preferences{Allergens: []string{"bad"}}}, time.Now(), "all", 1, 20); err == nil {
		t.Fatal("invalid preferences accepted")
	}
}
// RECO-LOGIC-004
func TestRecommendationDatesSortAndFilter(t *testing.T) {
	today := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	date := func(v string) *string { return &v }
	for _, tt := range []struct {
		date             *string
		state            string
		expiring, usable bool
	}{{date("2026-10-22"), "missing", false, false}, {date("2026-10-23"), "ok", true, true}, {date("2026-10-26"), "ok", true, true}, {date("2026-10-27"), "ok", false, true}, {nil, "ok", false, true}} {
		out, err := BuildRecommendations(store.RecommendationInput{Candidates: []store.RecommendationCandidate{recCandidate(1)}, Inventory: []store.InventoryItem{{FoodID: 1, FoodStatus: "active", QuantityMilli: 1000, Unit: "g", ExpiresOn: tt.date}}}, today, "all", 1, 20)
		if err != nil || out.Items[0].Ingredients[0].State != tt.state || out.Items[0].UsesExpiring != tt.expiring || out.Stats.HasUsableInventory != tt.usable {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	}
	cs := []store.RecommendationCandidate{recCandidate(1), recCandidate(2), recCandidate(3), recCandidate(4)}
	cs[0].DurationMinutes = 20
	cs[1].Ingredients[0].Food.ID = 2
	cs[2].Ingredients[0].Food.ID = 3
	inventory := []store.InventoryItem{{FoodID: 1, FoodStatus: "active", QuantityMilli: 1000, Unit: "g"}, {FoodID: 2, FoodStatus: "active", QuantityMilli: 1000, Unit: "g", ExpiresOn: date("2026-10-23")}}
	out, _ := BuildRecommendations(store.RecommendationInput{Candidates: cs, Inventory: inventory}, today, "all", 1, 20)
	for i, id := range []int64{2, 4, 1, 3} {
		if out.Items[i].Recipe.ID != id {
			t.Fatalf("sort %+v", out.Items)
		}
	}
	out, _ = BuildRecommendations(store.RecommendationInput{Candidates: cs, Inventory: inventory}, today, "can_cook", 2, 2)
	if out.Total != 3 || len(out.Items) != 1 || out.Items[0].Recipe.ID != 1 {
		t.Fatalf("filter/page %+v", out)
	}
}

// RECO-LOGIC-005
func TestRecommendationAggregateCapBoundaryOrder(t *testing.T) {
	c := recCandidate(1)
	c.Ingredients[0].PieceWeightG = recFloat(1000000)
	base := []store.InventoryItem{{FoodID: 1, FoodStatus: "active", QuantityMilli: 999999999, Unit: "piece"}, {FoodID: 1, FoodStatus: "active", QuantityMilli: 999999, Unit: "g"}}
	for _, extra := range []int64{0, 1, 2} {
		batches := append(append([]store.InventoryItem{}, base...), store.InventoryItem{FoodID: 1, FoodStatus: "active", QuantityMilli: extra, Unit: "g"})
		for _, reverse := range []bool{false, true} {
			if reverse {
				batches[0], batches[2] = batches[2], batches[0]
			}
			out, err := BuildRecommendations(store.RecommendationInput{Candidates: []store.RecommendationCandidate{c}, Inventory: batches}, time.Now(), "all", 1, 20)
			want := FormatQuantity(min(recommendationCap-1+extra, recommendationCap))
			if err != nil || *out.Items[0].Ingredients[0].Have != want || !out.Items[0].CanCook {
				t.Fatalf("extra=%d reverse=%v out=%+v err=%v", extra, reverse, out, err)
			}
		}
	}
}

// RECO-LOGIC-006
func TestValidateRecommendationQuery(t *testing.T) {
	for _, tt := range []struct {
		q, a  string
		valid bool
	}{{"  菜  ", "", true}, {"%_", "all", true}, {"", "can_cook", true}, {"", "bad", false}, {string([]byte{255}), "all", false}} {
		q, a, err := ValidateRecommendationQuery(tt.q, tt.a)
		if (err == nil) != tt.valid {
			t.Fatalf("q=%q a=%q err=%v", tt.q, tt.a, err)
		}
		if tt.valid && (q != strings.TrimSpace(tt.q) || a == "") {
			t.Fatalf("normalization q=%q a=%q", q, a)
		}
	}
	if _, _, err := ValidateRecommendationQuery(strings.Repeat("菜", 101), "all"); err == nil {
		t.Fatal("overlong query accepted")
	}
}
