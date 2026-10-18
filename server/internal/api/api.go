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
	api := r.Group("/api")
	api.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() }, a.context)
	api.GET("/auth/csrf", a.csrf)
	api.POST("/auth/register", a.register)
	api.POST("/auth/login", a.login)
	api.POST("/auth/logout", a.logout)
	me := api.Group("", a.protected(false))
	me.GET("/me", func(c *gin.Context) { success(c, 200, session(c).User) })
	me.PUT("/me/password", a.password)
	me.GET("/me/preferences", a.getPreferences)
	me.PUT("/me/preferences", a.putPreferences)
	me.GET("/foods", a.foods)
	admin := api.Group("/admin", a.protected(true))
	admin.GET("/users", a.users)
	admin.PATCH("/users/:id", a.updateUser)
	admin.POST("/users/:id/reset-password", a.resetPassword)
	admin.GET("/audit-logs", a.audit)
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
	c.JSON(status, gin.H{"data": data})
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
	c.AbortWithStatusJSON(app.Status, gin.H{"error": gin.H{"code": app.Code, "message": app.Message}})
}
func (a *API) decode(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(415, "UNSUPPORTED_MEDIA_TYPE", "请求必须使用 application/json"))
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
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
func (a *API) csrf(c *gin.Context) {
	token, err := c.Cookie(a.csrfName())
	if err != nil || !a.validCSRF(c, token) {
		token, err = a.issueCSRF(c)
	}
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, gin.H{"token": token})
}
func (a *API) register(c *gin.Context) {
	if !a.limit(c, "register:ip:"+c.ClientIP(), 10, time.Hour) {
		return
	}
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
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
func (a *API) login(c *gin.Context) {
	if !a.limit(c, "login:ip:"+c.ClientIP(), 30, 15*time.Minute) {
		return
	}
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
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
	c.Set("session", &store.Session{User: u, TokenHash: hash})
	token, err := a.issueCSRF(c)
	if err != nil {
		_ = a.service.Store.DeleteSession(c.Request.Context(), hash)
		a.fail(c, err)
		return
	}
	a.cookie(c, a.sessionName(), raw, 604800)
	success(c, 200, gin.H{"user": u, "csrf_token": token})
}
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
func (a *API) password(c *gin.Context) {
	s := session(c)
	if !a.limit(c, "password:user:"+strconv.FormatInt(s.User.ID, 10), 5, 15*time.Minute) || !a.limit(c, "password:ip:"+c.ClientIP(), 20, 15*time.Minute) {
		return
	}
	var p struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
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
func (a *API) getPreferences(c *gin.Context) {
	p, err := a.service.Store.Preferences(c.Request.Context(), session(c).User.ID)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, p)
}
func (a *API) putPreferences(c *gin.Context) {
	var p struct {
		Allergens          *[]string       `json:"allergens"`
		ExcludedFoodIDs    *[]int64        `json:"excluded_food_ids"`
		DietaryPreferences *[]string       `json:"dietary_preferences"`
		Energy             json.RawMessage `json:"daily_energy_kcal"`
		Protein            json.RawMessage `json:"daily_protein_g"`
		Fat                json.RawMessage `json:"daily_fat_g"`
		Carbohydrate       json.RawMessage `json:"daily_carbohydrate_g"`
	}
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
func (a *API) foods(c *gin.Context) {
	items, err := a.service.Store.Foods(c.Request.Context())
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, items)
}
