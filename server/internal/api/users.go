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
	success(c, 200, gin.H{"items": items, "total": total})
}
func (a *API) updateUser(c *gin.Context) {
	id, ok := a.id(c)
	if !ok {
		return
	}
	var p struct {
		Role   *string `json:"role"`
		Status *string `json:"status"`
	}
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
func (a *API) resetPassword(c *gin.Context) {
	id, ok := a.id(c)
	if !ok {
		return
	}
	var p struct {
		Password string `json:"temporary_password"`
	}
	if !a.decode(c, &p) {
		return
	}
	if err := a.service.ResetPassword(c.Request.Context(), *session(c), id, p.Password); err != nil {
		a.fail(c, err)
		return
	}
	success(c, 204, nil)
}
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
	success(c, 200, gin.H{"items": items, "total": total})
}
