package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"freshplate/server/internal/logic"
	appRuntime "freshplate/server/internal/runtime"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

const testPassword = "Correct horse battery 123!"
const testOrigin = "http://127.0.0.1:8080"

type fixture struct {
	t       *testing.T
	st      *store.Store
	svc     *logic.Service
	handler http.Handler
	now     time.Time
}
type browser struct {
	f       *fixture
	cookies map[string]*http.Cookie
	csrf    string
	id      int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc, err := logic.New(st)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, svc: svc, now: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
	svc.Now = func() time.Time { return f.now }
	cfg := appRuntime.Config{Address: "127.0.0.1:8080", Database: "test.db", KeyFile: "test.key", Origin: testOrigin}
	f.handler, err = New(svc, cfg, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) browser() *browser { return &browser{f: f, cookies: map[string]*http.Cookie{}} }
func (b *browser) request(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	b.f.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", b.csrf)
	if b.id > 0 {
		r.Header.Set("X-Expected-User-ID", strconv.FormatInt(b.id, 10))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.f.handler.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" {
		b.f.t.Fatalf("%s %s lacks no-store: %v", method, path, w.Header())
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
	}
}
func data(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, v); err != nil {
		t.Fatal(err)
	}
}
func code(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Error.Code != want {
		t.Fatalf("error=%s want=%s body=%s", e.Error.Code, want, w.Body.String())
	}
}
func (b *browser) token() {
	w := b.request("GET", "/api/auth/csrf", "", nil)
	status(b.f.t, w, 200)
	var p struct {
		Token string `json:"token"`
	}
	data(b.f.t, w, &p)
	b.csrf = p.Token
}
func (b *browser) register(name string) {
	b.token()
	w := b.request("POST", "/api/auth/register", fmt.Sprintf(`{"username":%q,"password":%q}`, name, testPassword), nil)
	status(b.f.t, w, 201)
}
func (b *browser) login(name, password string) {
	b.token()
	w := b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":%q}`, name, password), nil)
	status(b.f.t, w, 200)
	var p struct {
		User store.User `json:"user"`
		CSRF string     `json:"csrf_token"`
	}
	data(b.f.t, w, &p)
	b.id = p.User.ID
	b.csrf = p.CSRF
}
func (b *browser) sessionCookie() *http.Cookie { return b.cookies["freshplate_session"] }

func TestREG001_REG003_AUTH001_SES003_SES004(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register(" Alice_01 ")
	status(t, b.request("GET", "/api/me", "", nil), 401)
	w := b.request("POST", "/api/auth/register", fmt.Sprintf(`{"username":"alice_01","password":%q}`, testPassword), nil)
	status(t, w, 409)
	code(t, w, "USERNAME_TAKEN")
	verifications := f.svc.Passwords.Metrics().Verifications
	for _, name := range []string{"alice_01", "unknown_user"} {
		w = b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":"Wrong password 123!"}`, name), nil)
		status(t, w, 401)
		code(t, w, "INVALID_CREDENTIALS")
	}
	if f.svc.Passwords.Metrics().Verifications != verifications+2 {
		t.Fatal("wrong-password and unknown-user login must both verify a hash")
	}
	b.login("ALICE_01", testPassword)
	c := b.sessionCookie()
	if c == nil || c.MaxAge != 604800 || !c.HttpOnly || c.Secure || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie=%+v", c)
	}
	w = b.request("GET", "/api/me", "", nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), `"password_hash"`) || strings.Contains(w.Body.String(), "auth_version") {
		t.Fatalf("private field leaked: %s", w.Body.String())
	}
	old := c.Value
	w = b.request("POST", "/api/auth/logout", "", map[string]string{"Content-Type": ""})
	status(t, w, 204)
	if w.Body.Len() != 0 || b.sessionCookie() != nil {
		t.Fatal("logout must clear cookie and return empty body")
	}
	b.token()
	status(t, b.request("POST", "/api/auth/logout", "", nil), 204)
	b.cookies["freshplate_session"] = &http.Cookie{Name: "freshplate_session", Value: old}
	status(t, b.request("GET", "/api/me", "", nil), 401)
}

func TestREG002_PWD001_SEC002_SEC003_StrictJSON(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.token()
	valid := fmt.Sprintf(`{"username":"strict_user","password":%q}`, testPassword)
	cases := []struct {
		name, body, media string
		want              int
	}{
		{"unknown", strings.TrimSuffix(valid, "}") + `,"role":"admin"}`, "application/json", 400},
		{"trailing", valid + ` {}`, "application/json", 400}, {"empty", "", "application/json", 400}, {"null", "null", "application/json", 400},
		{"invalid_utf8", `{"username":"bad","password":"` + string([]byte{255}) + `"}`, "application/json", 400},
		{"oversize", valid + strings.Repeat(" ", 65537), "application/json", 400}, {"plain", valid, "text/plain", 415}, {"missing_type", valid, "", 415},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := b.request("POST", "/api/auth/register", tc.body, map[string]string{"Content-Type": tc.media, "X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)})
			status(t, w, tc.want)
		})
	}
	var count int
	if err := f.st.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("bad request wrote users: %d %v", count, err)
	}
	status(t, b.request("POST", "/api/auth/register", valid, map[string]string{"Content-Type": "application/json; charset=utf-8"}), 201)
}

func TestCSRF001_CSRF002_CSRF003_CSRF005_SEC001(t *testing.T) {
	f := newFixture(t)
	a := f.browser()
	a.register("csrf_user")
	anonymous := a.csrf
	a.token()
	if a.csrf != anonymous {
		t.Fatal("valid anonymous token must be reused")
	}
	a.login("csrf_user", testPassword)
	authenticated := a.csrf
	if authenticated == anonymous {
		t.Fatal("login must rotate token")
	}
	c := a.cookies["freshplate_csrf"]
	if c.MaxAge != 86400 || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("csrf cookie=%+v", c)
	}
	a.token()
	if a.csrf != authenticated {
		t.Fatal("valid authenticated token must be reused")
	}
	for _, origin := range []string{"", "null", "https://127.0.0.1:8080", "http://127.0.0.1:8081", "http://127.0.0.1:8080.evil"} {
		w := a.request("POST", "/api/auth/logout", "", map[string]string{"Origin": origin})
		status(t, w, 403)
		code(t, w, "CSRF_INVALID")
	}
	other := f.browser()
	other.login("csrf_user", testPassword)
	for _, token := range []string{"", anonymous, other.csrf, authenticated + "x"} {
		w := a.request("POST", "/api/auth/logout", "", map[string]string{"X-CSRF-Token": token})
		status(t, w, 403)
	}
	// Copy both token components to prove the signature binds the session, beyond double submit.
	a.cookies["freshplate_csrf"] = &http.Cookie{Name: "freshplate_csrf", Value: other.csrf}
	a.csrf = other.csrf
	status(t, a.request("POST", "/api/auth/logout", "", nil), 403)
	a.token()
	f.now = f.now.Add(24 * time.Hour)
	status(t, a.request("POST", "/api/auth/logout", "", nil), 403)
	a.token()
	if a.csrf == authenticated {
		t.Fatal("expired token not rotated")
	}
	a.cookies["freshplate_session"] = &http.Cookie{Name: "freshplate_session", Value: "malformed"}
	a.token()
	if a.sessionCookie() != nil {
		t.Fatal("invalid session cookie not cleared")
	}
	status(t, a.request("GET", "/api/me", "", nil), 401)
	status(t, a.request("POST", "/api/auth/logout", "", nil), 204)
}

const fullPreferences = `{"allergens":["milk","milk"],"excluded_food_ids":[],"dietary_preferences":["vegan"],"daily_energy_kcal":2100,"daily_protein_g":80,"daily_fat_g":60,"daily_carbohydrate_g":null}`

func TestID001_PREF001_PREF002_ISO001_ACL001(t *testing.T) {
	f := newFixture(t)
	a, b := f.browser(), f.browser()
	a.register("pref_alice")
	b.register("pref_bob")
	a.login("pref_alice", testPassword)
	b.login("pref_bob", testPassword)
	for _, expected := range []string{"", "01", "-1", "1.0"} {
		status(t, a.request("PUT", "/api/me/preferences", fullPreferences, map[string]string{"X-Expected-User-ID": expected}), 400)
	}
	w := a.request("PUT", "/api/me/preferences", fullPreferences, map[string]string{"X-Expected-User-ID": strconv.FormatInt(b.id, 10)})
	status(t, w, 409)
	code(t, w, "IDENTITY_CHANGED")
	status(t, a.request("PUT", "/api/me/preferences", `{"allergens":[]}`, nil), 400)
	w = a.request("PUT", "/api/me/preferences", fullPreferences, nil)
	status(t, w, 200)
	var p store.Preferences
	data(t, w, &p)
	if len(p.Allergens) != 1 || p.DailyEnergyKcal == nil || *p.DailyEnergyKcal != 2100 || p.DailyCarbohydrateG != nil {
		t.Fatalf("bad preferences %+v", p)
	}
	w = b.request("GET", "/api/me/preferences", "", nil)
	status(t, w, 200)
	data(t, w, &p)
	if len(p.Allergens) != 0 || p.DailyEnergyKcal != nil {
		t.Fatalf("A preferences exposed to B: %+v", p)
	}
	for _, route := range []struct{ method, path, body string }{{"GET", "/api/admin/users", ""}, {"GET", "/api/admin/audit-logs", ""}, {"PATCH", fmt.Sprintf("/api/admin/users/%d", b.id), `{"role":"admin"}`}, {"POST", fmt.Sprintf("/api/admin/users/%d/reset-password", b.id), `{"temporary_password":"Another password 123!"}`}} {
		w = a.request(route.method, route.path, route.body, nil)
		status(t, w, 403)
		code(t, w, "FORBIDDEN")
	}
}

func TestADM001_ADM002_SES005_PasswordGate(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.InitAdmin(context.Background(), "root_admin", testPassword); err != nil {
		t.Fatal(err)
	}
	admin, user := f.browser(), f.browser()
	user.register("managed_user")
	admin.login("root_admin", testPassword)
	user.login("managed_user", testPassword)
	path := fmt.Sprintf("/api/admin/users/%d", user.id)
	status(t, admin.request("PATCH", path, `{"role":"admin"}`, nil), 200)
	// Existing session must observe promotion without logging in again.
	status(t, user.request("GET", "/api/admin/users", "", nil), 200)
	status(t, admin.request("PATCH", path, `{"role":"user"}`, nil), 200)
	status(t, user.request("GET", "/api/admin/users", "", nil), 403)
	status(t, admin.request("POST", path+"/reset-password", `{"temporary_password":"Temporary password 123!"}`, nil), 204)
	status(t, user.request("GET", "/api/me", "", nil), 401)
	user.login("managed_user", "Temporary password 123!")
	w := user.request("GET", "/api/me", "", nil)
	status(t, w, 200)
	var u store.User
	data(t, w, &u)
	if !u.MustChangePassword {
		t.Fatal("reset did not require password change")
	}
	for _, route := range []struct{ method, path, body string }{{"GET", "/api/me/preferences", ""}, {"GET", "/api/foods", ""}, {"PUT", "/api/me/preferences", fullPreferences}, {"GET", "/api/admin/users", ""}} {
		w = user.request(route.method, route.path, route.body, nil)
		status(t, w, 403)
		code(t, w, "PASSWORD_CHANGE_REQUIRED")
	}
	user.token()
	w = user.request("PUT", "/api/me/password", `{"current_password":"Wrong password 123!","new_password":"New password here 456!"}`, nil)
	status(t, w, 400)
	code(t, w, "INVALID_CURRENT_PASSWORD")
	status(t, user.request("PUT", "/api/me/password", `{"current_password":"Temporary password 123!","new_password":"New password here 456!"}`, nil), 204)
	if user.sessionCookie() != nil {
		t.Fatal("change password left session cookie")
	}
	user.login("managed_user", "New password here 456!")
	status(t, user.request("GET", "/api/me/preferences", "", nil), 200)
	w = admin.request("GET", "/api/admin/audit-logs?target_user_id="+strconv.FormatInt(user.id, 10), "", nil)
	status(t, w, 200)
	var logs struct {
		Items []store.Audit `json:"items"`
		Total int           `json:"total"`
	}
	data(t, w, &logs)
	if logs.Total < 3 {
		t.Fatalf("missing admin/password audits: %+v", logs)
	}
	for _, secret := range []string{testPassword, "Temporary password 123!", "New password here 456!", "$argon2id$"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("audit secret leak: %q", secret)
		}
	}
	status(t, admin.request("PATCH", path, `{"status":"disabled"}`, nil), 200)
	status(t, user.request("GET", "/api/me", "", nil), 401)
	user.token()
	w = user.request("POST", "/api/auth/login", `{"username":"managed_user","password":"New password here 456!"}`, nil)
	status(t, w, 401)
	code(t, w, "INVALID_CREDENTIALS")
}

func TestSES001_SES002_FixedExpiry(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("expiry_user")
	b.login("expiry_user", testPassword)
	raw := b.sessionCookie().Value
	f.now = f.now.Add(7*24*time.Hour - time.Second)
	w := b.request("GET", "/api/me", "", nil)
	status(t, w, 200)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("read renewed fixed session")
	}
	f.now = f.now.Add(time.Second)
	status(t, b.request("GET", "/api/me", "", nil), 401)
	if b.sessionCookie() != nil {
		t.Fatal("expired cookie not cleared")
	}
	b.token()
	b.login("expiry_user", testPassword)
	if b.sessionCookie().Value == raw {
		t.Fatal("session token reused")
	}
}

func TestLIMIT001_LIMIT002_SEC004_NoTrustedXFF(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		f := newFixture(t)
		b := f.browser()
		b.token()
		for i := 0; i < 10; i++ {
			status(t, b.request("POST", "/api/auth/register", `{"username":"x","password":"short"}`, map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)}), 400)
		}
		before := f.svc.Passwords.Metrics()
		w := b.request("POST", "/api/auth/register", fmt.Sprintf(`{"username":"blocked_user","password":%q}`, testPassword), nil)
		status(t, w, 429)
		if w.Header().Get("Retry-After") == "" {
			t.Fatal("no retry header")
		}
		if f.svc.Passwords.Metrics() != before {
			t.Fatal("limited registration performed hashing")
		}
		f.now = f.now.Add(time.Hour)
		b.token()
		status(t, b.request("POST", "/api/auth/register", `{"username":"x","password":"short"}`, nil), 400)
	})
	t.Run("normalized_username", func(t *testing.T) {
		f := newFixture(t)
		b := f.browser()
		b.token()
		for i := 0; i < 10; i++ {
			status(t, b.request("POST", "/api/auth/login", `{"username":" Absent_User ","password":"short"}`, nil), 401)
		}
		before := f.svc.Passwords.Metrics()
		status(t, b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":"absent_user","password":%q}`, testPassword), nil), 429)
		if f.svc.Passwords.Metrics() != before {
			t.Fatal("limited login verified hash")
		}
	})
	t.Run("login_ip", func(t *testing.T) {
		f := newFixture(t)
		b := f.browser()
		b.token()
		for i := 0; i < 30; i++ {
			status(t, b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":"missing_%d","password":"short"}`, i), map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)}), 401)
		}
		before := f.svc.Passwords.Metrics()
		status(t, b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":"new_absent","password":%q}`, testPassword), nil), 429)
		if f.svc.Passwords.Metrics() != before {
			t.Fatal("limited IP verified hash")
		}
	})
	t.Run("password_user", func(t *testing.T) {
		f := newFixture(t)
		b := f.browser()
		b.register("limited_user")
		b.login("limited_user", testPassword)
		body := `{"current_password":"short","new_password":"Long new password 123!"}`
		for i := 0; i < 5; i++ {
			status(t, b.request("PUT", "/api/me/password", body, nil), 400)
		}
		before := f.svc.Passwords.Metrics()
		status(t, b.request("PUT", "/api/me/password", fmt.Sprintf(`{"current_password":%q,"new_password":"Long new password 123!"}`, testPassword), nil), 429)
		if f.svc.Passwords.Metrics() != before {
			t.Fatal("limited password request hashed")
		}
		status(t, b.request("GET", "/api/me", "", nil), 200)
	})
}

func TestCSRF002_AllWriteEntrypointsRejectBeforeMutation(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("matrix_user")
	b.login("matrix_user", testPassword)
	routes := []struct{ method, path, body string }{
		{"POST", "/api/auth/register", `{"username":"new_user","password":"Correct horse battery 123!"}`},
		{"POST", "/api/auth/login", `{"username":"matrix_user","password":"Correct horse battery 123!"}`},
		{"POST", "/api/auth/logout", ""}, {"PUT", "/api/me/password", `{"current_password":"Correct horse battery 123!","new_password":"Another password here 456!"}`},
		{"PUT", "/api/me/preferences", fullPreferences}, {"PATCH", "/api/admin/users/1", `{"role":"admin"}`}, {"POST", "/api/admin/users/1/reset-password", `{"temporary_password":"Another password here 456!"}`},
	}
	before := f.svc.Passwords.Metrics()
	for _, route := range routes {
		w := b.request(route.method, route.path, route.body, map[string]string{"X-CSRF-Token": ""})
		status(t, w, 403)
		code(t, w, "CSRF_INVALID")
	}
	if f.svc.Passwords.Metrics() != before {
		t.Fatal("CSRF rejection reached password operations")
	}
	var users, sessions int
	if err := f.st.DB.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB.QueryRow("SELECT count(*) FROM sessions").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if users != 1 || sessions != 1 {
		t.Fatalf("CSRF request mutated users/sessions: %d/%d", users, sessions)
	}
	// A matching cookie/header with a tampered signature must still fail.
	b.csrf = b.csrf + "x"
	b.cookies["freshplate_csrf"] = &http.Cookie{Name: "freshplate_csrf", Value: b.csrf}
	status(t, b.request("POST", "/api/auth/logout", "", nil), 403)
}

func TestSES003_DEP002_SecureCookieAndSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	cfg := appRuntime.Config{Address: "127.0.0.1:8080", Database: "test.db", KeyFile: "test.key", Origin: "https://freshplate.example", Secure: true}
	handler, err := New(f.svc, cfg, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.handler = handler
	b := f.browser()
	b.token()
	csrf := b.cookies["__Host-freshplate_csrf"]
	if csrf == nil || !csrf.Secure || !csrf.HttpOnly || csrf.Domain != "" || csrf.Path != "/" {
		t.Fatalf("secure CSRF cookie: %+v", csrf)
	}
	w := b.request("POST", "/api/auth/register", fmt.Sprintf(`{"username":"secure_user","password":%q}`, testPassword), map[string]string{"Origin": cfg.Origin})
	status(t, w, 201)
	w = b.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":"secure_user","password":%q}`, testPassword), map[string]string{"Origin": cfg.Origin})
	status(t, w, 200)
	session := b.cookies["__Host-freshplate_session"]
	if session == nil || !session.Secure || !session.HttpOnly || session.Domain != "" || session.Path != "/" || session.MaxAge != 604800 {
		t.Fatalf("secure session cookie: %+v", session)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("security headers: %v", w.Header())
	}
}

func TestSEC003_InternalFailuresStayPrivate(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("failure_user")
	b.login("failure_user", testPassword)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	w := b.request("GET", "/api/auth/csrf", "", nil)
	status(t, w, 500)
	code(t, w, "INTERNAL_ERROR")
	for _, private := range []string{"sql:", "database", "test.db", "goroutine"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("private error content: %s", w.Body.String())
		}
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("session lookup failure must not replace security context")
	}
}

func TestSEC003_SES004_BodyLimitAndLogoutPreserveSession(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("body_limit_user")
	b.login("body_limit_user", testPassword)
	oversized := strings.Repeat("x", (64<<10)+1)
	// The global length check runs even for routes that do not decode JSON or require authentication.
	for _, route := range []struct{ method, path string }{{"GET", "/api/auth/csrf"}, {"GET", "/api/me"}, {"GET", "/api/admin/users"}, {"POST", "/api/auth/logout"}} {
		w := b.request(route.method, route.path, oversized, map[string]string{"X-CSRF-Token": ""})
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	for _, body := range []string{"{}", " ", oversized} {
		r := httptest.NewRequest("POST", "/api/auth/logout", strings.NewReader(body))
		r.ContentLength = -1
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-CSRF-Token", b.csrf)
		for _, c := range b.cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
		if w.Header().Get("Cache-Control") != "no-store" || len(w.Result().Cookies()) != 0 {
			t.Fatalf("rejected logout altered cookies or cache policy: %v", w.Header())
		}
		status(t, b.request("GET", "/api/me", "", nil), 200)
	}
	w := b.request("POST", "/api/auth/logout", "", map[string]string{"Content-Type": ""})
	status(t, w, 204)
	if w.Body.Len() != 0 {
		t.Fatal("logout returned a response body")
	}
	status(t, b.request("GET", "/api/me", "", nil), 401)
}

func TestLIMIT002_PasswordIPThreshold(t *testing.T) {
	f := newFixture(t)
	browsers := make([]*browser, 5)
	for i := range browsers {
		name := fmt.Sprintf("password_ip_%d", i)
		if _, err := f.svc.Register(context.Background(), name, testPassword); err != nil {
			t.Fatal(err)
		}
		browsers[i] = f.browser()
		browsers[i].login(name, testPassword)
	}
	body := `{"current_password":"short","new_password":"Different password here 456!"}`
	for _, b := range browsers[:4] {
		for range 5 {
			w := b.request("PUT", "/api/me/password", body, nil)
			status(t, w, 400)
			code(t, w, "INVALID_CURRENT_PASSWORD")
		}
		status(t, b.request("GET", "/api/me", "", nil), 200)
	}
	before := f.svc.Passwords.Metrics()
	w := browsers[4].request("PUT", "/api/me/password", body, nil)
	status(t, w, 429)
	code(t, w, "RATE_LIMITED")
	retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || retry <= 0 {
		t.Fatalf("invalid Retry-After: %q", w.Header().Get("Retry-After"))
	}
	if f.svc.Passwords.Metrics() != before {
		t.Fatal("IP-limited password request performed password work")
	}
	status(t, browsers[4].request("GET", "/api/me", "", nil), 200)
}

func TestSEC004_TrustedProxyControlsClientIP(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted_%t", trusted), func(t *testing.T) {
			f := newFixture(t)
			cfg := appRuntime.Config{Address: "127.0.0.1:8080", Database: "test.db", KeyFile: "test.key", Origin: testOrigin}
			if trusted {
				cfg.TrustedProxies = []string{"192.0.2.1"}
			}
			var err error
			f.handler, err = New(f.svc, cfg, make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			b := f.browser()
			b.token()
			body := `{"username":"x","password":"short"}`
			for range 10 {
				status(t, b.request("POST", "/api/auth/register", body, map[string]string{"X-Forwarded-For": "198.51.100.1"}), 400)
			}
			status(t, b.request("POST", "/api/auth/register", body, map[string]string{"X-Forwarded-For": "198.51.100.1"}), 429)
			want := 429
			if trusted {
				want = 400
			}
			status(t, b.request("POST", "/api/auth/register", body, map[string]string{"X-Forwarded-For": "198.51.100.2"}), want)
		})
	}
}
