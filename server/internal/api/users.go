package api

import (
	"strconv"
	"strings"

	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) id(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的用户编号"))
		return 0, false
	}
	return id, true
}
func (a *API) page(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	for _, item := range []struct {
		name string
		dest *int
	}{{"page", &page}, {"page_size", &size}} {
		if v, exists := c.GetQuery(item.name); exists {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 1000000 {
				a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的分页参数"))
				return 0, 0, false
			}
			*item.dest = n
		}
	}
	if size > 100 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "每页最多 100 条"))
		return 0, 0, false
	}
	return page, size, true
}

// users godoc
// @Summary 分页查询用户
// @Description 仅管理员可用。仅管理员。按 ID 降序，使用 offset 分页；total 为筛选后的总数。
// @Tags 管理
// @Produce json
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Param username query string false "去除首尾空白并转小写后的子串匹配，处理后最多 32 字节。"
// @Param role query string false "角色筛选；省略或空字符串表示全部。" Enums(user,admin)
// @Param status query string false "状态筛选；省略或空字符串表示全部。" Enums(active,disabled)
// @Success 200 {object} response{data=userPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Router /admin/users [get]
func (a *API) users(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	role, status := c.Query("role"), c.Query("status")
	if (role != "" && role != "user" && role != "admin") || (status != "" && status != "active" && status != "disabled") {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的筛选条件"))
		return
	}
	username := strings.ToLower(strings.TrimSpace(c.Query("username")))
	if len(username) > 32 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "用户名筛选过长"))
		return
	}
	items, total, err := a.service.Store.ListUsers(c.Request.Context(), page, size, username, role, status)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, userPage{Items: items, Total: total})
}

// updateUser godoc
// @Summary 修改用户角色或状态
// @Description 仅管理员可用。仅管理员。不能修改本人（SELF_ADMIN_CHANGE_FORBIDDEN），不能移除最后一位启用管理员（LAST_ADMIN_REQUIRED）。状态变化撤销目标全部会话；角色变化在后续请求生效。不变值成功返回且不写审计。
// @Tags 管理
// @Produce json
// @Accept json
// @Param Origin header string true "必须逐字匹配服务配置 Origin；浏览器自动设置，不允许跨站请求。"
// @Param X-CSRF-Token header string true "GET /auth/csrf 返回的 token；登录后改用登录响应中的 csrf_token，须与 CSRF Cookie 一致。"
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围），不是目标用户 ID；不作为认证凭据。身份不一致返回 409。"
// @Param id path int64 true "目标用户 ID。" minimum(1)
// @Param body body userUpdate true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 200 {object} response{data=store.User}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：目标用户或接口不存在。"
// @Failure 409 {object} errorResponse "USERNAME_TAKEN、IDENTITY_CHANGED、SELF_ADMIN_CHANGE_FORBIDDEN 或 LAST_ADMIN_REQUIRED；详见各接口说明。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Router /admin/users/{id} [patch]
func (a *API) updateUser(c *gin.Context) {
	id, ok := a.id(c)
	if !ok {
		return
	}
	var p userUpdate
	if !a.decode(c, &p) {
		return
	}
	if (p.Role == nil && p.Status == nil) || (p.Role != nil && *p.Role != "user" && *p.Role != "admin") || (p.Status != nil && *p.Status != "active" && *p.Status != "disabled") {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交有效角色或账号状态"))
		return
	}
	u, err := a.service.Store.AdminUpdate(c.Request.Context(), *session(c), id, p.Role, p.Status, a.service.Now())
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, u)
}

// resetPassword godoc
// @Summary 重置用户密码
// @Description 仅管理员可用。仅管理员，不能重置本人。临时密码不能等于目标当前密码（400 INVALID_ARGUMENT）。撤销目标全部会话，要求下次登录改密；不启用禁用账号。目标并发变化返回 IDENTITY_CHANGED。临时密码由管理员自行通过独立渠道交付。
// @Tags 管理
// @Produce json
// @Accept json
// @Param Origin header string true "必须逐字匹配服务配置 Origin；浏览器自动设置，不允许跨站请求。"
// @Param X-CSRF-Token header string true "GET /auth/csrf 返回的 token；登录后改用登录响应中的 csrf_token，须与 CSRF Cookie 一致。"
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围），不是目标用户 ID；不作为认证凭据。身份不一致返回 409。"
// @Param id path int64 true "目标用户 ID。" minimum(1)
// @Param body body passwordReset true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 204 "成功，无响应体"
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：目标用户或接口不存在。"
// @Failure 409 {object} errorResponse "USERNAME_TAKEN、IDENTITY_CHANGED、SELF_ADMIN_CHANGE_FORBIDDEN 或 LAST_ADMIN_REQUIRED；详见各接口说明。"
// @Failure 415 {object} errorResponse "UNSUPPORTED_MEDIA_TYPE：请求体必须使用 application/json。"
// @Failure 429 {object} errorResponse "RATE_LIMITED：请求频率或密码运算并发超限；Retry-After 给出建议等待秒数。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Header 429 {integer} Retry-After "建议等待的秒数"
// @Router /admin/users/{id}/reset-password [post]
func (a *API) resetPassword(c *gin.Context) {
	id, ok := a.id(c)
	if !ok {
		return
	}
	var p passwordReset
	if !a.decode(c, &p) {
		return
	}
	if err := a.service.ResetPassword(c.Request.Context(), *session(c), id, p.Password); err != nil {
		a.fail(c, err)
		return
	}
	success(c, 204, nil)
}

// audit godoc
// @Summary 分页查询审计日志
// @Description 仅管理员可用。仅管理员。按 ID 降序，使用 offset 分页。action 示例：admin_init、admin_recover_password、password_reset、role_changed、status_changed、user_updated；未知 action 返回空列表。
// @Tags 管理
// @Produce json
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Param target_user_id query int64 false "按目标用户筛选。" minimum(1)
// @Param action query string false "精确匹配操作类型（实现限制为 64 字节）；省略或空字符串表示全部。" maxLength(64)
// @Success 200 {object} response{data=auditPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：参数、请求体或页面身份断言无效；INVALID_CURRENT_PASSWORD：当前密码错误。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效；INVALID_CREDENTIALS：用户名、密码错误或账号已禁用。"
// @Failure 403 {object} errorResponse "CSRF_INVALID：Origin/CSRF 无效；FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Router /admin/audit-logs [get]
func (a *API) audit(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	var target int64
	if value, exists := c.GetQuery("target_user_id"); exists {
		var e error
		target, e = strconv.ParseInt(value, 10, 64)
		if e != nil || target <= 0 {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效目标用户"))
			return
		}
	}
	action := c.Query("action")
	if len(action) > 64 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效操作类型"))
		return
	}
	items, total, err := a.service.Store.ListAudit(c.Request.Context(), page, size, target, action)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, auditPage{Items: items, Total: total})
}
