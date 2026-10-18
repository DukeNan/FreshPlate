package api

import (
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAccessJWTClaimsExpiryAndRevocation(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("jwt_user")
	b.login("jwt_user", testPassword)
	original := b.access
	b.access = ""
	status(t, b.request("GET", "/api/me", "", nil), 401)
	b.access = original
	if len(strings.Split(original, ".")) != 3 {
		t.Fatal("login did not return JWT")
	}
	f.now = f.now.Add(15 * time.Minute)
	w := b.request("GET", "/api/me", "", nil)
	status(t, w, 401)
	code(t, w, "ACCESS_TOKEN_EXPIRED")
	claims := new(accessClaims)
	if _, err := jwt.ParseWithClaims(original, claims, func(*jwt.Token) (any, error) { return make([]byte, 32), nil }, jwt.WithoutClaimsValidation()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*accessClaims){
		func(c *accessClaims) { c.Issuer = "evil" }, func(c *accessClaims) { c.Audience = jwt.ClaimStrings{"evil"} },
		func(c *accessClaims) { c.Subject = "0" }, func(c *accessClaims) { c.IssuedAt = nil },
		func(c *accessClaims) { c.SID = "bad" },
		func(c *accessClaims) { c.ExpiresAt = nil },
		func(c *accessClaims) { c.IssuedAt = jwt.NewNumericDate(f.now.Add(time.Minute)) },
	} {
		changed := *claims
		mutate(&changed)
		bad, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &changed).SignedString(make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		w = b.request("GET", "/api/me", "", map[string]string{"Authorization": "Bearer " + bad})
		status(t, w, 401)
		code(t, w, "AUTH_REQUIRED")
	}
	wrongAlgorithm, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	w = b.request("GET", "/api/me", "", map[string]string{"Authorization": "Bearer " + wrongAlgorithm})
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
	bad, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("wrong key wrong key wrong key wrong"))
	if err != nil {
		t.Fatal(err)
	}
	w = b.request("GET", "/api/me", "", map[string]string{"Authorization": "Bearer " + bad})
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
	sid, err := hex.DecodeString(claims.SID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.st.DeleteSession(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	w = b.request("GET", "/api/me", "", nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
}
func TestRefreshRotationAndReplay(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("refresh_api")
	b.login("refresh_api", testPassword)
	old := *b.sessionCookie()
	csrf := b.csrf
	f.now = f.now.Add(time.Minute)
	w := b.request("POST", "/api/auth/refresh", "", nil)
	status(t, w, 200)
	var result loginResult
	data(t, w, &result)
	if result.CSRFToken != csrf || result.ExpiresIn != 900 || result.TokenType != "Bearer" || b.sessionCookie().Value == old.Value {
		t.Fatal("refresh did not rotate with stable CSRF")
	}
	b.access = result.AccessToken
	status(t, b.request("GET", "/api/me", "", nil), 200)
	b.cookies[old.Name] = &old
	w = b.request("POST", "/api/auth/refresh", "", nil)
	status(t, w, 401)
	code(t, w, "REFRESH_TOKEN_REUSED")
	w = b.request("GET", "/api/me", "", nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
}
func TestBearerWritesDoNotRequireCSRFOrCookie(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	b.register("bearer_user")
	b.login("bearer_user", testPassword)
	b.cookies = map[string]*http.Cookie{}
	w := b.request("PUT", "/api/me/preferences", fullPreferences, map[string]string{"Origin": "", "X-CSRF-Token": ""})
	status(t, w, 200)
	b.access = ""
	status(t, b.request("GET", "/api/me", "", nil), 401)
}
