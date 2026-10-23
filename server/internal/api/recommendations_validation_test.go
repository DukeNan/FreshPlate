package api

import (
	"database/sql"
	"fmt"
	"net/http/httptest"
	"testing"

	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// RECO-API-002
func TestValidationRecommendationPasswordGate(t *testing.T) {
	_, admin, user, _, _, _ := adminFixture(t)
	status(t, admin.request("POST", fmt.Sprintf("/api/admin/users/%d/reset-password", user.id), `{"temporary_password":"Temp-Pass-2026!"}`, nil), 204)
	user.login("recipe_user", "Temp-Pass-2026!")
	w := user.request("GET", "/api/recommendations", "", nil)
	status(t, w, 403)
	code(t, w, "PASSWORD_CHANGE_REQUIRED")
}

// RECO-API-003
func TestValidationRecommendationBusyReadAndRecovery(t *testing.T) {
	f, _, user := foodFixture(t)
	// A rollback-journal exclusive lock blocks real reads; normal WAL writers do not.
	if _, err := f.st.DB.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.Exec("PRAGMA busy_timeout=20"); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+dbPath(t, f))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err = raw.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer raw.Exec("ROLLBACK")
	// The route is exercised after authentication to prove the recommendation read,
	// rather than an earlier authentication lookup, maps SQLITE_BUSY to 503.
	a := &API{service: f.svc}
	route := gin.New()
	route.GET("/api/recommendations", func(c *gin.Context) {
		c.Set("session", &store.Session{User: store.User{ID: user.id}})
		a.recommendations(c)
	})
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		route.ServeHTTP(w, httptest.NewRequest("GET", "/api/recommendations", nil))
		return w
	}
	w := get()
	status(t, w, 503)
	code(t, w, "DATABASE_BUSY")
	if _, err = raw.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	status(t, get(), 200)
}
