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
// @description 使用 Authorization: Bearer access JWT（15 分钟），每次查会话确保即时撤销。refresh HttpOnly Cookie 固定有效 7 天，轮换并检测重放。登录、刷新和注销要求精确 Origin 与 CSRF；其他写接口无需 CSRF，受保护写接口仍要求 X-Expected-User-ID。JWT 仅保存前端内存。强制改密期间允许 /me、/auth/csrf、/auth/refresh、/auth/logout、/me/password。成功返回 data，失败返回 error.code/message。所有 API no-store，不启用 CORS。
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
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
