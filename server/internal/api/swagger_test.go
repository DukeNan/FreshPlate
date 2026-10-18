package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSwaggerMatchesRegisteredAPI(t *testing.T) {
	f := newFixture(t)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest("GET", "/swagger/doc.json", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Swagger unavailable without frontend assets: %d %s", w.Code, w.Body.String())
	}
	var spec struct {
		Swagger     string
		BasePath    string
		Paths       map[string]map[string]json.RawMessage
		Definitions map[string]struct{ Properties map[string]json.RawMessage }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Swagger != "2.0" || spec.BasePath != "/api" {
		t.Fatalf("unexpected Swagger metadata: %s %s", spec.Swagger, spec.BasePath)
	}
	operations := map[string]bool{}
	for path, methods := range spec.Paths {
		for method := range methods {
			operations[strings.ToUpper(method)+" /api"+strings.ReplaceAll(path, "{id}", ":id")] = true
		}
	}
	for _, route := range f.handler.(*gin.Engine).Routes() {
		if strings.HasPrefix(route.Path, "/api/") {
			key := route.Method + " " + route.Path
			if !operations[key] {
				t.Errorf("undocumented route: %s", key)
			}
			delete(operations, key)
		}
	}
	if len(operations) != 0 {
		t.Fatalf("documented but unregistered operations: %v", operations)
	}
	for _, private := range []string{"password_hash", "auth_version", "PasswordHash", "AuthVersion"} {
		if _, exists := spec.Definitions["store.User"].Properties[private]; exists {
			t.Errorf("private user field in documentation: %s", private)
		}
	}
	for _, path := range []string{"/swagger/index.html", "/swagger/swagger-ui-bundle.js", "/swagger/swagger-ui.css"} {
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("Swagger resource/header failure: %s %d %v", path, w.Code, w.Header())
		}
	}
}
