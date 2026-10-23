package api

import (
	"encoding/json"
	"fmt"
	"freshplate/server/internal/logic"
	"testing"
)

// RECO-API-001
func TestRecommendationAPIContractFilteringAndIsolation(t *testing.T) {
	f, admin, user, food, _, _ := adminFixture(t)
	for i := 0; i < 21; i++ {
		publishRecipe(t, admin, fmt.Sprintf("dish%02d", i), food)
	}
	get := func(b *browser, path string) logic.RecommendationPage {
		t.Helper()
		w := b.request("GET", path, "", nil)
		status(t, w, 200)
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope) != 1 || envelope["data"] == nil {
			t.Fatalf("envelope %s", w.Body.String())
		}
		var fields map[string]json.RawMessage
		json.Unmarshal(envelope["data"], &fields)
		if len(fields) != 3 || fields["items"] == nil || fields["total"] == nil || fields["stats"] == nil {
			t.Fatalf("fields %s", envelope["data"])
		}
		var out logic.RecommendationPage
		if err := json.Unmarshal(envelope["data"], &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := get(user, "/api/recommendations")
	if out.Total != 21 || len(out.Items) != 20 || out.Stats.Candidates != 21 || out.Stats.HasUsableInventory {
		t.Fatalf("default %+v", out)
	}
	if out.Items[0].Ingredients[0].State != "missing" {
		t.Fatal("no inventory should be missing")
	}
	out = get(user, "/api/recommendations?q=dish01")
	if out.Total != 1 || len(out.Items) != 1 || out.Stats.Candidates != 21 {
		t.Fatalf("q %+v", out)
	}
	out = get(user, "/api/recommendations?q=%25")
	if out.Total != 0 || out.Stats.Candidates != 21 {
		t.Fatalf("literal q %+v", out)
	}
	out = get(user, "/api/recommendations?availability=can_cook")
	if out.Total != 0 || out.Items == nil {
		t.Fatalf("can cook %+v", out)
	}
	for _, query := range []string{"availability=bad", "page=0", "page_size=101", "page=1000001"} {
		status(t, user.request("GET", "/api/recommendations?"+query, "", nil), 400)
	}
	status(t, f.browser().request("GET", "/api/recommendations", "", nil), 401)
	if _, err := f.st.DB.Exec("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,created_at,updated_at) VALUES(?,?,250000,'g',?,?)", user.id, food, f.now.Unix(), f.now.Unix()); err != nil {
		t.Fatal(err)
	}
	out = get(user, "/api/recommendations?availability=can_cook&page_size=100")
	if out.Total != 21 || len(out.Items) != 21 || !out.Stats.HasUsableInventory {
		t.Fatalf("inventory %+v", out)
	}
	if get(admin, "/api/recommendations?availability=can_cook").Total != 0 {
		t.Fatal("admin saw user inventory")
	}
	status(t, user.request("PUT", "/api/me/preferences", fullPreferences, nil), 200)
	out = get(user, "/api/recommendations")
	if out.Total != 0 || out.Stats.FilteredByRestrictions != 21 {
		t.Fatalf("preferences not immediate %+v", out)
	}
}
