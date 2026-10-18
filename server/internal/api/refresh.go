package api

import (
	"crypto/rand"
	"encoding/base64"
	"io"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// refresh godoc
// @Summary 轮换刷新令牌并取得 access JWT
// @Description refresh Cookie 固定有效 7 天；旧令牌重放会撤销整个会话。需要匹配 Origin 和 CSRF。
// @Tags 认证
// @Produce json
// @Param Origin header string true "精确匹配服务 Origin"
// @Param X-CSRF-Token header string true "当前 CSRF 令牌"
// @Success 200 {object} response{data=loginResult}
// @Failure 401 {object} errorResponse "AUTH_REQUIRED 或 REFRESH_TOKEN_REUSED"
// @Failure 403 {object} errorResponse "CSRF_INVALID"
// @Router /auth/refresh [post]
func (a *API) refresh(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) != 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "刷新请求不应包含请求体"))
		return
	}
	raw, err := c.Cookie(a.sessionName())
	if err != nil {
		a.fail(c, store.E(401, "AUTH_REQUIRED", "请重新登录"))
		return
	}
	old, err := logic.HashToken(raw)
	if err != nil {
		a.fail(c, store.E(401, "AUTH_REQUIRED", "请重新登录"))
		return
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		a.fail(c, err)
		return
	}
	next := base64.RawURLEncoding.EncodeToString(random)
	hash, err := logic.HashToken(next)
	if err != nil {
		a.fail(c, err)
		return
	}
	s, err := a.service.Store.RotateRefresh(c.Request.Context(), old, hash, a.service.Now())
	if err != nil {
		a.fail(c, err)
		return
	}
	c.Set("session", s)
	csrf, err := c.Cookie(a.csrfName())
	if err != nil || !a.validCSRF(c, csrf) {
		csrf, err = a.issueCSRF(c)
	}
	if err != nil {
		a.fail(c, err)
		return
	}
	access, expires, err := a.access(s)
	if err != nil {
		a.fail(c, err)
		return
	}
	a.cookie(c, a.sessionName(), next, int(s.ExpiresAt.Unix()-a.service.Now().Unix()))
	success(c, 200, loginResult{User: s.User, CSRFToken: csrf, AccessToken: access, TokenType: "Bearer", ExpiresIn: expires})
}
