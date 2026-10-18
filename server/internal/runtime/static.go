package runtime

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const CSP = "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func Pages(c Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", CSP)
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if c.Secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		for _, part := range strings.Split(clean, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		file := filepath.Join(c.WebDir, filepath.FromSlash(clean))
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			if path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			file = filepath.Join(c.WebDir, "index.html")
			if _, err = os.Stat(file); err != nil {
				http.Error(w, "Build the frontend with npm run build first", 503)
				return
			}
		}
		http.ServeFile(w, r, file)
	})
}
