package store

import (
	"context"
	"testing"
)

// RECO-STORE-003
func TestValidationRecommendationEmptyAndInactiveInventory(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	snapshot, err := s.RecommendationSnapshot(context.Background(), admin.User.ID, "")
	if err != nil || len(snapshot.Candidates) != 0 || len(snapshot.Inventory) != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", snapshot, err)
	}
	if _, err = s.RecommendationSnapshot(context.Background(), 9223372036854775807, ""); err == nil {
		t.Fatal("missing preferences accepted")
	}
	food, _, _ := seedRecipeFoods(t, s)
	for _, row := range []struct {
		quantity int64
		deleted  any
	}{{0, nil}, {1000, int64(1)}, {1000, nil}} {
		if _, err = s.DB.Exec("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,deleted_at,created_at,updated_at) VALUES(?,?,?,'g',?,1,1)", admin.User.ID, food, row.quantity, row.deleted); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err = s.RecommendationSnapshot(context.Background(), admin.User.ID, "")
	if err != nil || len(snapshot.Inventory) != 1 || snapshot.Inventory[0].QuantityMilli != 1000 {
		t.Fatalf("inactive inventory snapshot=%+v err=%v", snapshot, err)
	}
}
