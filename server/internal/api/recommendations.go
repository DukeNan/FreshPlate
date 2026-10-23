package api

import "github.com/gin-gonic/gin"

// recommendations godoc
// @Summary 基于本人库存与饮食限制推荐食谱
// @Description 仅本人视角；只读快照，安全过滤后按缺料数、临期、耗时、ID 排序。stats 不受检索与 availability 影响。
// @Tags 推荐
// @Produce json
// @Param q query string false "食谱名称字面子串，TrimSpace 后最多 100 码点"
// @Param availability query string false "可做筛选" Enums(all,can_cook) default(all)
// @Param page query int false "页码" default(1) minimum(1) maximum(1000000)
// @Param page_size query int false "每页条数" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=logic.RecommendationPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR"
// @Failure 503 {object} errorResponse "DATABASE_BUSY"
// @Security BearerAuth
// @Router /recommendations [get]
func (a *API) recommendations(c *gin.Context) {
	p, n, ok := a.page(c)
	if !ok {
		return
	}
	result, err := a.service.Recommendations(c.Request.Context(), *session(c), c.Query("q"), c.Query("availability"), p, n)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, result)
}
