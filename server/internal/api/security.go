package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"

	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) sessionName() string {
	if a.config.Secure {
		return "__Host-freshplate_refresh"
	}
	return "freshplate_refresh"
}
func (a *API) csrfName() string {
	if a.config.Secure {
		return "__Host-freshplate_csrf"
	}
	return "freshplate_csrf"
}
func (a *API) cookie(c *gin.Context, name, value string, age int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, Secure: a.config.Secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
func session(c *gin.Context) *store.Session {
	v, _ := c.Get("session")
	s, _ := v.(*store.Session)
	return s
}
func (a *API) binding(c *gin.Context) string {
	if s := session(c); s != nil {
		return "session:" + fmt.Sprintf("%x", s.TokenHash)
	}
	return "anonymous"
}
func (a *API) signature(payload, binding string) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(binding + "\x00" + payload))
	return m.Sum(nil)
}
func (a *API) validCSRF(c *gin.Context, token string) bool {
	if len(token) > 256 {
		return false
	}
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return false
	}
	random, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil || len(random) != 32 {
		return false
	}
	ts, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil {
		return false
	}
	now := a.service.Now().Unix()
	if ts > now || now-ts >= 86400 {
		return false
	}
	sig, e := base64.RawURLEncoding.DecodeString(p[2])
	return e == nil && hmac.Equal(sig, a.signature(p[0]+"."+p[1], a.binding(c)))
}
func (a *API) issueCSRF(c *gin.Context) (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(random) + "." + strconv.FormatInt(a.service.Now().Unix(), 10)
	token := payload + "." + base64.RawURLEncoding.EncodeToString(a.signature(payload, a.binding(c)))
	a.cookie(c, a.csrfName(), token, 86400)
	return token, nil
}
func (a *API) context(c *gin.Context) {
	limit := bodyLimit(c.FullPath(), c.Request.Method)
	if c.Request.ContentLength > limit {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求体过大或无法读取"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	path := c.Request.URL.Path
	cookieAuth := path == "/api/auth/csrf" || path == "/api/auth/login" || path == "/api/auth/refresh" || path == "/api/auth/logout"
	if cookieAuth {
		if raw, e := c.Cookie(a.sessionName()); e == nil {
			hash, e := logic.HashToken(raw)
			if e == nil {
				s, e := a.service.Store.ResolveRefresh(c.Request.Context(), hash, a.service.Now())
				if e != nil {
					a.fail(c, e)
					return
				}
				if s != nil {
					c.Set("session", s)
				}
			}
		}
	} else if auth := c.GetHeader("Authorization"); auth != "" {
		if len(c.Request.Header.Values("Authorization")) != 1 || !strings.HasPrefix(auth, "Bearer ") || strings.ContainsAny(strings.TrimPrefix(auth, "Bearer "), " \t\r\n") {
			a.fail(c, store.E(401, "AUTH_REQUIRED", "请重新登录"))
			return
		}
		if e := a.authenticate(c, strings.TrimPrefix(auth, "Bearer ")); e != nil {
			a.fail(c, e)
			return
		}
	}
	if s := session(c); s != nil && s.User.MustChangePassword {
		p := c.Request.URL.Path
		if p != "/api/me" && p != "/api/auth/csrf" && p != "/api/auth/logout" && p != "/api/auth/refresh" && p != "/api/auth/login" && p != "/api/me/password" {
			a.fail(c, store.E(403, "PASSWORD_CHANGE_REQUIRED", "请先修改密码"))
			return
		}
	}
	if c.Request.Method == "POST" && (path == "/api/auth/login" || path == "/api/auth/refresh" || path == "/api/auth/logout") {
		values := c.Request.Header.Values("Origin")
		origin := ""
		if len(values) == 1 {
			origin = values[0]
		}
		token := c.GetHeader("X-CSRF-Token")
		cookie, e := c.Cookie(a.csrfName())
		if origin != a.config.Origin || e != nil || token == "" || len(c.Request.Header.Values("X-CSRF-Token")) != 1 || subtle.ConstantTimeCompare([]byte(token), []byte(cookie)) != 1 || !a.validCSRF(c, token) {
			a.fail(c, store.E(403, "CSRF_INVALID", "页面安全令牌已失效，请重新提交"))
			return
		}
	}
	c.Next()
}
func (a *API) protected(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := session(c)
		if s == nil {
			a.fail(c, store.E(401, "AUTH_REQUIRED", "请先登录"))
			return
		}
		if admin && s.User.Role != "admin" {
			a.fail(c, store.E(403, "FORBIDDEN", "无权执行此操作"))
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			values := c.Request.Header.Values("X-Expected-User-ID")
			value := ""
			if len(values) == 1 {
				value = values[0]
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
				a.fail(c, store.E(400, "INVALID_ARGUMENT", "缺少有效的页面身份断言"))
				return
			}
			if id != s.User.ID {
				a.fail(c, store.E(409, "IDENTITY_CHANGED", "登录身份已变化，请重新加载页面"))
				return
			}
		}
		c.Next()
	}
}
func (a *API) limit(c *gin.Context, key string, count int, window time.Duration) bool {
	ok, retry := a.limiter.Allow(key, count, window, a.service.Now())
	if !ok {
		c.Header("Retry-After", fmt.Sprint(retry))
		a.fail(c, store.E(429, "RATE_LIMITED", "请求过于频繁，请稍后重试"))
	}
	return ok
}
