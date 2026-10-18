package api

import (
	_ "freshplate/server/docs"
	appRuntime "freshplate/server/internal/runtime"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

//go:generate go tool swag init -g swagger.go -d .,../store -o ../../docs

// @title FreshPlate 用户模块 API
// @version 0.1.0
// @description 使用 HttpOnly Cookie 会话，不使用 Bearer Token。会话固定有效期 7 天，CSRF 有效期 24 小时。写请求须有匹配的 Origin、CSRF Cookie 与 X-CSRF-Token；受保护写请求另需表单加载时的 X-Expected-User-ID。登录后改用响应中的 csrf_token。强制改密期间只允许 /me、/auth/csrf、/auth/logout、/me/password。成功返回 data（204 无响应体），失败返回 error.code/message。JSON 最多 64 KiB，拒绝未知字段。所有 API 响应 no-store。不启用 CORS。密码任务最多同时两个，超限 429。Swagger 2.0 不支持 cookie 类型的 security scheme，Cookie 由同源浏览器自动维护。
// @BasePath /api
func swaggerHeaders(c *gin.Context) {
	// Swagger UI's embedded SVG uses a style attribute; scripts and stylesheets remain same-origin.
	c.Header("Content-Security-Policy", appRuntime.CSP+"; style-src-attr 'unsafe-inline'")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func mountSwagger(r *gin.Engine, secure bool) {
	r.GET("/swagger/*any", swaggerHeaders, func(c *gin.Context) {
		if secure {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}, ginSwagger.WrapHandler(swaggerFiles.Handler))
}
