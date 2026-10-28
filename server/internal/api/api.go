package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"freshplate/server/internal/logic"
	appRuntime "freshplate/server/internal/runtime"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

type API struct {
	service *logic.Service
	config  appRuntime.Config
	key     []byte
	limiter *logic.Limiter
}

func New(s *logic.Service, cfg appRuntime.Config, key []byte) (*gin.Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.JWTKey) != 32 {
		return nil, errors.New("JWT key must contain 32 bytes")
	}
	if len(key) != 32 {
		return nil, errors.New("CSRF key must contain 32 bytes")
	}
	a := &API{s, cfg, key, logic.NewLimiter(10000)}
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}, gin.Recovery())
	mountSwagger(r, cfg.Secure)
	api := r.Group("/api")
	api.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() }, a.context)
	api.GET("/auth/csrf", a.csrf)
	api.POST("/auth/register", a.register)
	api.POST("/auth/login", a.login)
	api.POST("/auth/logout", a.logout)
	api.POST("/auth/refresh", a.refresh)
	me := api.Group("", a.protected(false))
	me.GET("/me", a.me)
	me.PUT("/me/password", a.password)
	me.GET("/me/preferences", a.getPreferences)
	me.PUT("/me/preferences", a.putPreferences)
	me.GET("/foods", a.foods)
	me.GET("/recommendations", a.recommendations)
	me.GET("/nutrition", a.nutrition)
	me.GET("/meals", a.listMeals)
	me.POST("/meals", a.createMeal)
	admin := api.Group("/admin", a.protected(true))
	admin.GET("/users", a.users)
	admin.PATCH("/users/:id", a.updateUser)
	admin.POST("/users/:id/reset-password", a.resetPassword)
	admin.GET("/audit-logs", a.audit)
	a.registerFoodRoutes(me, admin)
	a.registerInventoryRoutes(me)
	a.registerRecipeRoutes(me, admin)
	r.NoRoute(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		a.fail(c, store.E(404, "NOT_FOUND", "接口不存在"))
	})
	return r, nil
}
func success(c *gin.Context, status int, data any) {
	if status == 204 {
		c.Status(status)
		return
	}
	c.JSON(status, response{Data: data})
}
func (a *API) fail(c *gin.Context, err error) {
	var app *store.AppError
	if !errors.As(err, &app) {
		switch {
		case errors.Is(err, logic.ErrInvalidArgument):
			app = &store.AppError{Status: 400, Code: "INVALID_ARGUMENT", Message: "输入不符合要求"}
		case errors.Is(err, logic.ErrInvalidCredentials):
			app = &store.AppError{Status: 401, Code: "INVALID_CREDENTIALS", Message: "用户名或密码错误"}
		case errors.Is(err, logic.ErrInvalidCurrentPassword):
			app = &store.AppError{Status: 400, Code: "INVALID_CURRENT_PASSWORD", Message: "当前密码错误"}
		case errors.Is(err, logic.ErrSamePassword):
			app = &store.AppError{Status: 400, Code: "INVALID_ARGUMENT", Message: "新密码不能与当前密码相同"}
		case errors.Is(err, logic.ErrRateLimited):
			app = &store.AppError{Status: 429, Code: "RATE_LIMITED", Message: "请求过于频繁，请稍后重试"}
		}
	}
	if app == nil { // Only known application errors cross the HTTP boundary.
		log.Printf("request failed (%T)", err)
		app = &store.AppError{Status: 500, Code: "INTERNAL_ERROR", Message: "服务暂时不可用，请重试"}
	}
	if app.Status == 429 && c.Writer.Header().Get("Retry-After") == "" {
		c.Header("Retry-After", "1")
	}
	c.AbortWithStatusJSON(app.Status, errorResponse{Error: errorDetail{Code: app.Code, Message: app.Message}})
}
// bodyLimit 按已匹配路由（FullPath）与请求方法返回请求体上限（食材方案
// §2、食谱方案 §4）：仅食谱创建/更新四条写路由放宽至 256 KiB，其余全部
// 接口保持 64 KiB；不按 URL 前缀泛化（未注册的子路径不受放宽）。入参为
// gin 路由模板（c.FullPath() 返回 ":id"）或等价的正整数 ID 实际路径。
// 它是 Content-Length 预检、流式 MaxBytesReader 与 decode 的唯一限制
// 来源，保证三种通道同一限制。
func bodyLimit(fullPath, method string) int64 {
	if method != "POST" && method != "PUT" {
		return 64 << 10
	}
	switch fullPath {
	case "/api/recipes", "/api/admin/recipes", "/api/recipes/:id", "/api/admin/recipes/:id":
		return 256 << 10
	}
	for _, prefix := range []string{"/api/recipes/", "/api/admin/recipes/"} {
		if rest, ok := strings.CutPrefix(fullPath, prefix); ok && isPositiveDigits(rest) {
			return 256 << 10
		}
	}
	return 64 << 10
}

// isPositiveDigits 判断是否为非空十进制数字串（:id 的等价实际形式）。
func isPositiveDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (a *API) decode(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(415, "UNSUPPORTED_MEDIA_TYPE", "请求必须使用 application/json"))
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit(c.FullPath(), c.Request.Method)))
	if err != nil {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求体过大或无法读取"))
		return false
	}
	if !utf8.Valid(body) {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求必须是有效 UTF-8"))
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求字段或格式错误"))
		return false
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求只能包含一个 JSON 对象"))
		return false
	}
	return true
}

// csrf godoc
// @Summary 取得 CSRF 令牌
// @Description 匿名或登录均可调用；令牌有效期 24 小时并绑定当前会话/匿名状态。服务端同时维护 HttpOnly CSRF Cookie。
// @Tags 认证
// @Produce json
// @Success 200 {object} response{data=csrfResult}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Router /auth/csrf [get]
func (a *API) csrf(c *gin.Context) {
	token, err := c.Cookie(a.csrfName())
	if err != nil || !a.validCSRF(c, token) {
		token, err = a.issueCSRF(c)
	}
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, csrfResult{Token: token})
}

// register godoc
// @Summary 注册普通用户
// @Description 始终创建 active 普通用户；注册不会自动登录。用户名重复返回 USERNAME_TAKEN。每 IP 每小时最多 10 次。
// @Tags 认证
// @Produce json
// @Accept json
// @Param body body credentials true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 201 {object} response{data=store.User}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "USERNAME_TAKEN、IDENTITY_CHANGED、SELF_ADMIN_CHANGE_FORBIDDEN 或 LAST_ADMIN_REQUIRED；详见各接口说明。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 429 {object} errorResponse "RATE_LIMITED：请求频率或密码运算并发超限；Retry-After 给出建议等待秒数。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Header 429 {integer} Retry-After "建议等待的秒数"
// @Router /auth/register [post]
func (a *API) register(c *gin.Context) {
	if !a.limit(c, "register:ip:"+c.ClientIP(), 10, time.Hour) {
		return
	}
	var p credentials
	if !a.decode(c, &p) {
		return
	}
	u, err := a.service.Register(c.Request.Context(), p.Username, p.Password)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 201, u)
}

// login godoc
// @Summary 登录
// @Description 返回有效 15 分钟的 access JWT，设置固定有效期 7 天的 refresh Cookie；登录、刷新与注销使用 csrf_token。每 IP 每 15 分钟 30 次，每规范化用户名每 15 分钟 10 次。must_change_password=true 时仅可访问 /me、/auth/csrf、/auth/logout、/me/password。
// @Tags 认证
// @Produce json
// @Accept json
// @Param Origin header string true "必须逐字匹配服务配置 Origin；浏览器自动设置，不允许跨站请求。"
// @Param X-CSRF-Token header string true "GET /auth/csrf 返回的 token；登录后改用登录响应中的 csrf_token，须与 CSRF Cookie 一致。"
// @Param body body credentials true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 200 {object} response{data=loginResult}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 429 {object} errorResponse "RATE_LIMITED：请求频率或密码运算并发超限；Retry-After 给出建议等待秒数。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Header 429 {integer} Retry-After "建议等待的秒数"
// @Router /auth/login [post]
func (a *API) login(c *gin.Context) {
	if !a.limit(c, "login:ip:"+c.ClientIP(), 30, 15*time.Minute) {
		return
	}
	var p credentials
	if !a.decode(c, &p) {
		return
	}
	name, _ := logic.NormalizeUsername(p.Username)
	if !a.limit(c, "login:user:"+name, 10, 15*time.Minute) {
		return
	}
	u, raw, err := a.service.Login(c.Request.Context(), p.Username, p.Password)
	if err != nil {
		a.fail(c, err)
		return
	}
	hash, err := logic.HashToken(raw)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.Set("session", &store.Session{User: u, TokenHash: hash, ExpiresAt: a.service.Now().Add(7 * 24 * time.Hour)})
	token, err := a.issueCSRF(c)
	if err != nil {
		_ = a.service.Store.DeleteSession(c.Request.Context(), hash)
		a.fail(c, err)
		return
	}
	a.cookie(c, a.sessionName(), raw, 604800)
	access, expires, err := a.access(session(c))
	if err != nil {
		_ = a.service.Store.DeleteSession(c.Request.Context(), hash)
		a.fail(c, err)
		return
	}
	success(c, 200, loginResult{User: u, CSRFToken: token, AccessToken: access, TokenType: "Bearer", ExpiresIn: expires})
}

// logout godoc
// @Summary 退出当前会话
// @Description 不接受任何请求体；匿名也可调用。撤销当前会话、清除会话 Cookie 并设置匿名 CSRF Cookie，随后调用 GET /auth/csrf 获取匿名令牌。无需 X-Expected-User-ID。
// @Tags 认证
// @Produce json
// @Param Origin header string true "必须逐字匹配服务配置 Origin；浏览器自动设置，不允许跨站请求。"
// @Param X-CSRF-Token header string true "GET /auth/csrf 返回的 token；登录后改用登录响应中的 csrf_token，须与 CSRF Cookie 一致。"
// @Success 204 "成功，无响应体"
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Router /auth/logout [post]
func (a *API) logout(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) != 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "退出请求不应包含请求体"))
		return
	}
	if s := session(c); s != nil {
		if err := a.service.Store.DeleteSession(c.Request.Context(), s.TokenHash); err != nil {
			a.fail(c, err)
			return
		}
	}
	c.Set("session", (*store.Session)(nil))
	a.cookie(c, a.sessionName(), "", -1)
	if _, err := a.issueCSRF(c); err != nil {
		a.fail(c, err)
		return
	}
	success(c, 204, nil)
}

// password godoc
// @Summary 修改本人密码
// @Description 仅登录用户可用。新密码不能等于当前密码（400 INVALID_ARGUMENT）。允许强制改密用户访问。成功撤销本人全部会话并要求重新登录；再 GET /auth/csrf 获取匿名令牌。每用户每 15 分钟 5 次、每 IP 每 15 分钟 20 次。
// @Tags 个人账户
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围），不是目标用户 ID；不作为认证凭据。身份不一致返回 409。"
// @Param body body passwordChange true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 204 "成功，无响应体"
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "USERNAME_TAKEN、IDENTITY_CHANGED、SELF_ADMIN_CHANGE_FORBIDDEN 或 LAST_ADMIN_REQUIRED；详见各接口说明。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 429 {object} errorResponse "RATE_LIMITED：请求频率或密码运算并发超限；Retry-After 给出建议等待秒数。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Header 429 {integer} Retry-After "建议等待的秒数"
// @Security BearerAuth
// @Router /me/password [put]
func (a *API) password(c *gin.Context) {
	s := session(c)
	if !a.limit(c, "password:user:"+strconv.FormatInt(s.User.ID, 10), 5, 15*time.Minute) || !a.limit(c, "password:ip:"+c.ClientIP(), 20, 15*time.Minute) {
		return
	}
	var p passwordChange
	if !a.decode(c, &p) {
		return
	}
	if err := a.service.ChangePassword(c.Request.Context(), *s, p.Current, p.New); err != nil {
		a.fail(c, err)
		return
	}
	c.Set("session", (*store.Session)(nil))
	a.cookie(c, a.sessionName(), "", -1)
	if _, err := a.issueCSRF(c); err != nil {
		a.fail(c, err)
		return
	}
	success(c, 204, nil)
}

// getPreferences godoc
// @Summary 读取个人饮食设置
// @Description 仅登录用户可用。返回完整设置，包括仍被引用的停用忌口食材。
// @Tags 个人账户
// @Produce json
// @Success 200 {object} response{data=store.Preferences}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /me/preferences [get]
func (a *API) getPreferences(c *gin.Context) {
	p, err := a.service.Store.Preferences(c.Request.Context(), session(c).User.ID)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, p)
}

// putPreferences godoc
// @Summary 完整替换个人饮食设置
// @Description 仅登录用户可用。必须提交全部七个输入字段；数组不可为 null，营养目标允许 null。不接受响应中的 excluded_foods、updated_at。
// @Tags 个人账户
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围），不是目标用户 ID；不作为认证凭据。身份不一致返回 409。"
// @Param body body preferencesInput true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 200 {object} response{data=store.Preferences}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "USERNAME_TAKEN、IDENTITY_CHANGED、SELF_ADMIN_CHANGE_FORBIDDEN 或 LAST_ADMIN_REQUIRED；详见各接口说明。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /me/preferences [put]
func (a *API) putPreferences(c *gin.Context) {
	var p preferencesInput
	if !a.decode(c, &p) {
		return
	}
	if p.Allergens == nil || p.ExcludedFoodIDs == nil || p.DietaryPreferences == nil || len(p.Energy) == 0 || len(p.Protein) == 0 || len(p.Fat) == 0 || len(p.Carbohydrate) == 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部设置字段，数组不能为空值"))
		return
	}
	pref := store.Preferences{Allergens: *p.Allergens, ExcludedFoodIDs: *p.ExcludedFoodIDs, DietaryPreferences: *p.DietaryPreferences}
	for _, item := range []struct {
		raw  json.RawMessage
		dest **float64
	}{{p.Energy, &pref.DailyEnergyKcal}, {p.Protein, &pref.DailyProteinG}, {p.Fat, &pref.DailyFatG}, {p.Carbohydrate, &pref.DailyCarbohydrateG}} {
		if err := json.Unmarshal(item.raw, item.dest); err != nil {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "营养目标必须是数值或 null"))
			return
		}
	}
	saved, err := a.service.SavePreferences(c.Request.Context(), *session(c), pref)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, saved)
}

// foods godoc
// @Summary 列出可选食材
// @Description 仅登录用户可用。仅返回 active 食材，按 ID 升序，无分页。仅基础名称，不提供营养或过敏原判定数据。
// @Tags 个人账户
// @Produce json
// @Success 200 {object} response{data=[]store.Food}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /foods [get]
func (a *API) foods(c *gin.Context) {
	items, err := a.service.Store.Foods(c.Request.Context())
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, items)
}

// me godoc
// @Summary 获取当前用户
// @Description 仅登录用户可用。允许处于强制改密状态的用户访问。
// @Tags 个人账户
// @Produce json
// @Success 200 {object} response{data=store.User}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /me [get]
func (a *API) me(c *gin.Context) { success(c, 200, session(c).User) }
