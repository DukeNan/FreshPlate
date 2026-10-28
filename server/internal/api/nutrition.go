package api

import (
	"time"

	"freshplate/server/internal/calendar"
	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// nutrition godoc
// @Summary 七日营养估算（中国标准时间）
// @Tags 营养
// @Produce json
// @Param end query string false "结束日期 YYYY-MM-DD，默认中国标准时间今天"
// @Success 200 {object} response
// @Security BearerAuth
// @Router /nutrition [get]
func (a *API) nutrition(c *gin.Context) {
	end := a.service.Now().In(calendar.China)
	if raw := c.Query("end"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, calendar.China)
		if err != nil || parsed.Format("2006-01-02") != raw || parsed.Year() < 1970 {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "日期格式必须为 YYYY-MM-DD"))
			return
		}
		end = parsed
	}
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, calendar.China)
	items, err := a.service.Store.NutritionItems(c.Request.Context(), session(c).User.ID, end.AddDate(0, 0, -6), end.AddDate(0, 0, 1))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, gin.H{"days": logic.NutritionDays(end, items)})
}
