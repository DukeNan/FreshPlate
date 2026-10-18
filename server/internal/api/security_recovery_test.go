package api

import (
	"net/http"
	"testing"
)

func TestCSRF006ConcurrentInitialCookieOverwriteRecovery(t *testing.T) {
	f := newFixture(t)
	first, second := f.browser(), f.browser()
	// Two initially cookieless responses arrive before either tab's next request.
	first.token()
	second.token()
	stale := first.csrf
	first.cookies["freshplate_csrf"] = second.cookies["freshplate_csrf"]
	w := first.request("POST", "/api/auth/register", `{"username":"csrf_race","password":"Valid initial password!"}`, nil)
	status(t, w, http.StatusForbidden)
	code(t, w, "CSRF_INVALID")
	var count int
	if err := f.st.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected old token wrote account", err)
	}
	first.token()
	if first.csrf == stale || first.csrf != second.csrf {
		t.Fatal("current anonymous cookie not reused during recovery")
	}
	status(t, first.request("POST", "/api/auth/register", `{"username":"csrf_race","password":"Valid initial password!"}`, nil), http.StatusCreated)
	if err := f.st.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatal("explicit resubmission did not create exactly one account", err)
	}
}
