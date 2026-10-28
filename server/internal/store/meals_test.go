package store

import (
	"context"
	"fmt"
	"testing"
)

func TestMealAtomicAndIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "meal_user", "user")
	ss := session(t, s, u)
	food := invFood(t, s, "meal-food", "active")
	first := invSeed(t, s, u.ID, food, 10000, "g", "", "", 1)
	second := invSeed(t, s, u.ID, food, 2000, "g", "", "", 1)
	r, err := s.DB.Exec(`INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,source,allergen_complete,allergen_source,version,created_at,updated_at) VALUES('private',?,'published','meal',2,10,'["cook"]','',1,'test',1,1,1)`, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipe, _ := r.LastInsertId()
	if _, err = s.DB.Exec("INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,1,?,1000,'g')", recipe, food); err != nil {
		t.Fatal(err)
	}
	in := MealInput{RequestKey: "key", RequestHash: "hash", RecipeID: recipe, RecipeVersion: 1, CookedServingsMilli: 2000, EatenServingsMilli: 1000, AteAt: testNow, Items: []MealItemInput{{first, 1, 3000}, {second, 1, 3000}}}
	if _, err = s.CreateMeal(ctx, ss, in, testNow, func(Preferences, RecipeDetail) error { return nil }); !isCode(err, "VERSION_CONFLICT") {
		t.Fatalf("expected conflict: %v", err)
	}
	q, v, _, _ := invRow(t, s, first)
	if q != 10000 || v != 1 {
		t.Fatalf("partial deduction: %d %d", q, v)
	}
	in.Items[1].QuantityMilli = 1000
	saved, err := s.CreateMeal(ctx, ss, in, testNow, func(Preferences, RecipeDetail) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.CreateMeal(ctx, ss, in, testNow, func(Preferences, RecipeDetail) error { return nil })
	if err != nil || repeated.ID != saved.ID {
		t.Fatalf("retry: %+v %v", repeated, err)
	}
	q, v, _, _ = invRow(t, s, first)
	if q != 7000 || v != 2 {
		t.Fatalf("deduction: %d %d", q, v)
	}
	in.RequestHash = "changed"
	if _, err = s.CreateMeal(ctx, ss, in, testNow, func(Preferences, RecipeDetail) error { return nil }); !isCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("reuse: %v", err)
	}
	other := user(t, s, "meal_other", "user")
	list, total, err := s.ListMeals(ctx, other.ID, 1, 20)
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("isolation: %v %d %v", list, total, err)
	}
	// The transaction passes current restrictions and every recipe ingredient to the validator.
	otherFood := invFood(t, s, "second-food", "active")
	if _, err = s.DB.Exec("INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,2,?,1000,'g')", recipe, otherFood); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE user_preferences SET excluded_food_ids_json=? WHERE user_id=?", fmt.Sprintf("[%d]", otherFood), u.ID); err != nil {
		t.Fatal(err)
	}
	in.RequestKey, in.RequestHash = "new", "new"
	in.Items = []MealItemInput{{first, 2, 1000}}
	_, err = s.CreateMeal(ctx, ss, in, testNow, func(p Preferences, d RecipeDetail) error {
		if len(p.ExcludedFoodIDs) != 1 || p.ExcludedFoodIDs[0] != otherFood || len(d.Ingredients) != 2 {
			t.Fatalf("stale or incomplete restriction snapshot: %+v %+v", p, d)
		}
		return E(400, "DIETARY_RESTRICTION", "blocked")
	})
	if !isCode(err, "DIETARY_RESTRICTION") {
		t.Fatalf("restriction: %v", err)
	}
	q, v, _, _ = invRow(t, s, first)
	if q != 7000 || v != 2 {
		t.Fatal("restriction must not deduct stock")
	}

}
