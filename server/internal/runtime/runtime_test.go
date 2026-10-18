package runtime

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func config() Config {
	return Config{Address: "127.0.0.1:8080", Database: "db", KeyFile: "key", JWTKeyFile: "jwt.key", Origin: "http://127.0.0.1:8080"}
}
func TestDEP001Configuration(t *testing.T) {
	c := config()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"null", "https://127.0.0.1:8080", "http://127.0.0.1:8080/", "http://user@localhost", "http://localhost?x=1", "http://localhost#x", "//localhost"} {
		t.Run(origin, func(t *testing.T) {
			c := config()
			c.Origin = origin
			if c.Validate() == nil {
				t.Fatal("accepted invalid origin/mode")
			}
		})
	}
	c.Secure = true
	c.Origin = "https://example.com"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.TrustedProxies = []string{"*"}
	if c.Validate() == nil {
		t.Fatal("trusted arbitrary proxy")
	}
	t.Setenv("FRESHPLATE_SECURE", "not-a-bool")
	c = config()
	if c.Validate() == nil {
		t.Fatal("invalid bool accepted")
	}
}
func TestSEC004PersistentSecret(t *testing.T) {
	dir := t.TempDir()
	db, key := filepath.Join(dir, "db"), filepath.Join(dir, "key")
	first, e := Key(db, key)
	if e != nil {
		t.Fatal(e)
	}
	second, e := Key(db, key)
	if e != nil || string(first) != string(second) || len(first) != 32 {
		t.Fatal("secret not reused")
	}
	info, _ := os.Stat(key)
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
	if e = os.WriteFile(db, []byte{}, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Key(db, filepath.Join(dir, "missing")); e == nil {
		t.Fatal("recreated missing key for existing db")
	}
	if e = os.Chmod(key, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = Key(db, key); e == nil {
		t.Fatal("insecure key accepted")
	}
	if e = os.Chmod(key, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(key, []byte("bad"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Key(db, key); e == nil {
		t.Fatal("corrupt key accepted")
	}
}
func TestDEP001ExclusiveLock(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db")
	first, e := Lock(db)
	if e != nil {
		t.Fatal(e)
	}
	if f, e := Lock(db); e == nil {
		Unlock(f)
		t.Fatal("second process/descriptor accepted")
	}
	if e = Unlock(first); e != nil {
		t.Fatal(e)
	}
	third, e := Lock(db)
	if e != nil {
		t.Fatal(e)
	}
	defer Unlock(third)
}
func TestDEP001FlagDefaults(t *testing.T) {
	for _, key := range []string{"FRESHPLATE_ADDR", "FRESHPLATE_DB", "FRESHPLATE_KEY", "FRESHPLATE_JWT_KEY", "FRESHPLATE_ORIGIN", "FRESHPLATE_SECURE", "FRESHPLATE_WEB"} {
		t.Setenv(key, "")
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c := Flags(fs)
	if e := fs.Parse([]string{}); e != nil {
		t.Fatal(e)
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	if c.Address != "127.0.0.1:8080" || c.Secure || c.Origin != "http://127.0.0.1:8080" || c.JWTKeyFile != "../data/jwt.key" {
		t.Fatal("unsafe defaults")
	}
}

func TestDEP001RejectDatabaseAliases(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db")
	if err := os.WriteFile(db, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(db, alias); err != nil {
		t.Fatal(err)
	}
	if f, err := Lock(alias); err == nil {
		Unlock(f)
		t.Fatal("database symlink accepted")
	}
	hard := filepath.Join(dir, "hard")
	if err := os.Link(db, hard); err != nil {
		t.Fatal(err)
	}
	if f, err := Lock(hard); err == nil {
		Unlock(f)
		t.Fatal("database hard link accepted")
	}
}
func TestDEP002FinalPageHeaders(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><html><body>FreshPlate</body></html>"), 0600); e != nil {
		t.Fatal(e)
	}
	c := config()
	c.WebDir = dir
	c.Secure = true
	handler := Pages(c)
	for _, path := range []string{"/", "/settings", "/missing.js"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Header().Get("Content-Security-Policy") != CSP || r.Header().Get("X-Frame-Options") != "DENY" || r.Header().Get("X-Content-Type-Options") != "nosniff" || r.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" || r.Header().Get("Strict-Transport-Security") != "max-age=31536000" {
			t.Fatalf("missing header: %v", r.Header())
		}
		if path == "/settings" && !strings.Contains(r.Body.String(), "FreshPlate") {
			t.Fatal("SPA fallback missing")
		}
		if path == "/missing.js" && r.Code != 404 {
			t.Fatal("asset fallback leaked html")
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")))
	if r.Code != 405 {
		t.Fatal("write page accepted")
	}
}

func TestJWTKeyLifecycle(t *testing.T) {
	dir := t.TempDir()
	db, path := filepath.Join(dir, "db"), filepath.Join(dir, "jwt.key")
	key, err := JWTKey(db, path)
	if err != nil || len(key) != 32 {
		t.Fatalf("create JWT key: %v", err)
	}
	csrf, err := Key(db, filepath.Join(dir, "csrf.key"))
	if err != nil || string(key) == string(csrf) {
		t.Fatalf("keys must be independent: %v", err)
	}
	if err := os.WriteFile(db, nil, 0600); err != nil {
		t.Fatal(err)
	}
	again, err := JWTKey(db, path)
	if err != nil || string(again) != string(key) {
		t.Fatalf("reuse JWT key: %v", err)
	}
	if _, err := JWTKey(db, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("generated JWT key for existing database")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := JWTKey(db, path); err == nil {
		t.Fatal("accepted public JWT key")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := JWTKey(db, path); err == nil {
		t.Fatal("accepted short JWT key")
	}
	alias := filepath.Join(dir, "alias.key")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := JWTKey(db, alias); err == nil {
		t.Fatal("accepted symbolic JWT key")
	}
}

func TestJWTKeyConfiguration(t *testing.T) {
	dir := t.TempDir()
	c := config()
	c.Database, c.KeyFile, c.JWTKeyFile = filepath.Join(dir, "db"), filepath.Join(dir, "csrf.key"), filepath.Join(dir, "jwt.key")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", c.Database, c.KeyFile} {
		t.Run(path, func(t *testing.T) {
			copy := c
			copy.JWTKeyFile = path
			if copy.Validate() == nil {
				t.Fatal("accepted missing or shared JWT key path")
			}
		})
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	c.JWTKeyFile = filepath.Join(alias, "csrf.key")
	if c.Validate() == nil {
		t.Fatal("accepted key through aliased parent before key exists")
	}
	if err := os.WriteFile(c.KeyFile, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	c.JWTKeyFile = filepath.Join(dir, "hard.key")
	if err := os.Link(c.KeyFile, c.JWTKeyFile); err != nil {
		t.Fatal(err)
	}
	if c.Validate() == nil {
		t.Fatal("accepted hard linked keys")
	}
}

func TestJWTKeyFlagAndEnvironment(t *testing.T) {
	t.Setenv("FRESHPLATE_JWT_KEY", "environment.key")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c := Flags(fs)
	if c.JWTKeyFile != "environment.key" {
		t.Fatal("JWT key environment ignored")
	}
	if err := fs.Parse([]string{"--jwt-key", "flag.key"}); err != nil {
		t.Fatal(err)
	}
	if c.JWTKeyFile != "flag.key" {
		t.Fatal("JWT key flag ignored")
	}
}
