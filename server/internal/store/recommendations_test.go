package store

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
)

// RECO-STORE-001
func TestRecommendationBatchQueriesAndVisibility(t *testing.T) {
	counted := openCountedStore(t)
	s := counted.store
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	other := session(t, s, user(t, s, "rec_other", "user"))
	for i := 0; i < 40; i++ {
		d := createPublicRecipe(t, s, admin, fmt.Sprintf("recipe%d", i), food)
		if _, err := s.DB.Exec("UPDATE recipes SET status='published' WHERE id=?", d.ID); err != nil {
			t.Fatal(err)
		}
	}
	private := createPrivateRecipe(t, s, other, "private", food)
	if _, err := s.DB.Exec("UPDATE recipes SET status='published' WHERE id=?", private.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE user_preferences SET excluded_food_ids_json=? WHERE user_id=?", fmt.Sprintf("[%d]", food), admin.User.ID); err != nil {
		t.Fatal(err)
	}
	counted.reset()
	input, err := s.RecommendationSnapshot(context.Background(), admin.User.ID, "recipe1")
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Preferences.ExcludedFoodIDs) != 1 || input.Preferences.ExcludedFoodIDs[0] != food {
		t.Fatalf("excluded food preference lost: %+v", input.Preferences)
	}
	if counted.count() != 5 || len(input.Candidates) != 40 {
		t.Fatalf("queries=%d candidates=%d", counted.count(), len(input.Candidates))
	}
	matches := 0
	for _, c := range input.Candidates {
		if c.NameMatches {
			matches++
		}
		if len(c.Ingredients) != 1 || !c.Ingredients[0].AllergenComplete || len(c.Ingredients[0].Allergens) != 1 {
			t.Fatalf("ingredient %+v", c)
		}
	}
	if matches != 11 {
		t.Fatalf("literal substring matches=%d", matches)
	}
	input, err = s.RecommendationSnapshot(context.Background(), other.User.ID, "%")
	if err != nil || len(input.Candidates) != 41 {
		t.Fatalf("private snapshot=%+v err=%v", input, err)
	}
	for _, c := range input.Candidates {
		if c.NameMatches {
			t.Fatal("percent treated as wildcard")
		}
	}
}

// RECO-STORE-002
func TestRecommendationReadSnapshotDuringFoodChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	d := createPublicRecipe(t, s, admin, "snapshot", food)
	if _, err = s.DB.Exec("UPDATE recipes SET status='published' WHERE id=?", d.ID); err != nil {
		t.Fatal(err)
	}
	changed := false
	if err = s.orm.Callback().Row().After("gorm:row").Register("recommendation_snapshot_writer", func(db *gorm.DB) {
		if changed || !strings.Contains(db.Statement.SQL.String(), "FROM recipes r") {
			return
		}
		changed = true
		if _, e := writer.DB.Exec("UPDATE foods SET status='disabled',allergen_complete=0 WHERE id=?", food); e != nil {
			t.Errorf("writer: %v", e)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.orm.Callback().Row().Remove("recommendation_snapshot_writer")
	input, err := s.RecommendationSnapshot(context.Background(), admin.User.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("concurrent writer hook not triggered")
	}
	i := input.Candidates[0].Ingredients[0]
	if i.Food.Status != "active" || !i.AllergenComplete {
		t.Fatalf("snapshot mixed with concurrent write: %+v", i)
	}
	next, err := s.RecommendationSnapshot(context.Background(), admin.User.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Candidates[0].Ingredients[0].Food.Status != "disabled" {
		t.Fatal("next request did not see write")
	}
}
