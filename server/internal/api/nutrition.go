package api

import (
	"time"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// nutrition godoc
// @Summary 七日营养估算（UTC）
// @Tags 营养
// @Produce json
// @Param end query string false "结束日期 YYYY-MM-DD，默认 UTC 今天"
// @Success 200 {object} response
// @Security BearerAuth
// @Router /nutrition [get]
func (a *API) nutrition(c *gin.Context) {
	end := a.service.Now().UTC()
	if raw := c.Query("end"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil || parsed.Format("2006-01-02") != raw || parsed.Year() < 1970 {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "日期格式必须为 YYYY-MM-DD"))
			return
		}
		end = parsed
	}
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	items, err := a.service.Store.NutritionItems(c.Request.Context(), session(c).User.ID, end.AddDate(0, 0, -6), end.AddDate(0, 0, 1))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, gin.H{"days": logic.NutritionDays(end, items)})
}
