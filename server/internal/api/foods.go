package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// registerFoodRoutes 注册食材模块全部路由：me 为登录用户组，admin 为
// 管理员组。原 GET /foods（a.foods）保持不变，仅供忌口目录使用。
func (a *API) registerFoodRoutes(me, admin gin.IRouter) {
	me.GET("/food-catalog", a.foodCatalog)
	me.GET("/foods/:id", a.foodDetail)
	admin.GET("/foods", a.adminFoods)
	admin.POST("/foods", a.createFood)
	admin.PUT("/foods/:id", a.updateFood)
	admin.GET("/resource-audit-logs", a.resourceAudit)
}

// bodyLimitBytes 按已匹配路由与方法返回请求体上限（方案 §2）：仅食谱
// 创建/更新四类路由放宽至 256 KiB，其余一律 64 KiB；不按 URL 前缀泛化。
// 食谱路由已落地，context 中间件的 Content-Length 预检、流式
// MaxBytesReader 与 decode 统一使用 api.bodyLimit 作为同一限制来源；
// 本函数保留为兼容委托。
func bodyLimitBytes(method, path string) int64 { return bodyLimit(path, method) }

// foodID 解析路径中的食材 ID；必须为正整数，否则返回 400。
func (a *API) foodID(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的食材编号"))
		return 0, false
	}
	return id, true
}

// decodeFood 在 decode 之上将非 application/json 也归入 400 INVALID_ARGUMENT：
// 食材写路径契约只暴露 INVALID_ARGUMENT/DATABASE_BUSY 等错误码（方案 §4）。
func (a *API) decodeFood(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求必须使用 application/json"))
		return false
	}
	return a.decode(c, dst)
}

// foodListParams 解析并校验食材列表查询参数；filterStatus 控制是否接受
// status 筛选（目录不接受）。
func (a *API) foodListParams(c *gin.Context, filterStatus bool) (q, status string, page, size int, ok bool) {
	page, size, ok = a.page(c)
	if !ok {
		return "", "", 0, 0, false
	}
	status = ""
	if filterStatus {
		status = c.Query("status")
	}
	q, err := logic.ValidateFoodQuery(c.Query("q"), status, page, size)
	if err != nil {
		a.fail(c, err)
		return "", "", 0, 0, false
	}
	return q, status, page, size, true
}

// requireFoodInput 校验 POST/PUT 全字段必填：过敏原与完整性必须显式提交
//（未知信息显式传 []/false）；name/status 缺失由校验层拒绝。
// decodeFoodInput 保证过敏原两键存在后，此处仅拦截「键存在但显式 null」
// 的请求——指针为 nil 会被 foodWriteFromInput 解引用 panic，必须先拦下。
func requireFoodInput(p foodInput) bool {
	return p.Allergens != nil && p.AllergenComplete != nil
}

// foodRequiredKeys 是 POST/PUT 必须提交的键全集（方案 §4）：JSON 键缺失
// 与显式 null 在指针字段上解码后无法区分，必须对照原始对象检查键存在性。
var foodRequiredKeys = []string{
	"name", "status",
	"energy_kcal_100g", "protein_g_100g", "fat_g_100g", "carbohydrate_g_100g",
	"nutrition_source", "nutrition_reference",
	"density_g_ml", "piece_weight_g", "conversion_source",
	"allergens", "allergen_complete", "allergen_source",
}

// decodeFoodInput 在 decodeFood 的媒体类型/解码规则之上强制全字段必填：
// 键缺失（区别于键存在但值为 null 的「显式未知」）返回 400 INVALID_ARGUMENT
// 并列出缺失字段，防止 PUT 省略键被当作显式 null 静默清空已有数据。
// dst 为 foodInput 或内嵌它的 foodUpdateInput；version 的存在性由 PUT
// handler 单独检查。
func (a *API) decodeFoodInput(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求必须使用 application/json"))
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
	var present map[string]json.RawMessage
	if err = json.Unmarshal(body, &present); err != nil || present == nil {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求字段或格式错误"))
		return false
	}
	var missing []string
	for _, key := range foodRequiredKeys {
		if _, ok := present[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "缺少必填字段："+strings.Join(missing, "、")+"（可显式传 null 表示未知，但不能省略字段）"))
		return false
	}
	return true
}

// foodWriteFromInput 把请求 DTO 转为 store 输入结构；调用前必须先通过
// requireFoodInput。
func foodWriteFromInput(p foodInput) store.FoodWrite {
	return store.FoodWrite{
		Name:               p.Name,
		Status:             p.Status,
		EnergyKcal100g:     p.EnergyKcal100g,
		ProteinG100g:       p.ProteinG100g,
		FatG100g:           p.FatG100g,
		CarbohydrateG100g:  p.CarbohydrateG100g,
		NutritionSource:    p.NutritionSource,
		NutritionReference: p.NutritionReference,
		DensityGml:         p.DensityGml,
		PieceWeightG:       p.PieceWeightG,
		ConversionSource:   p.ConversionSource,
		Allergens:          *p.Allergens,
		AllergenComplete:   *p.AllergenComplete,
		AllergenSource:     p.AllergenSource,
	}
}

// foodCatalog godoc
// @Summary 分页检索食材目录
// @Description 仅登录用户可用。只返回 active 食材摘要，按 ID 倒序；摘要仅含 id/name/status/version/allergen_complete，不携带营养或来源详情。q 为去除首尾空白后的名称字面子串匹配，不解释 SQL 通配符。
// @Tags 食材
// @Produce json
// @Param q query string false "名称字面子串；处理后至多 100 码点。"
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=foodPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页参数或 q 超长。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /food-catalog [get]
func (a *API) foodCatalog(c *gin.Context) {
	q, _, page, size, ok := a.foodListParams(c, false)
	if !ok {
		return
	}
	items, total, err := a.service.FoodCatalog(c.Request.Context(), q, page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, foodPage{Items: items, Total: total})
}

// foodDetail godoc
// @Summary 读取食材完整详情
// @Description 仅登录用户可用。返回全部业务字段；停用食材同样可读，用于解释既有引用。未知信息以 null 表示，不补零。
// @Tags 食材
// @Produce json
// @Param id path int64 true "食材 ID。" minimum(1)
// @Success 200 {object} response{data=store.FoodDetail}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：食材 ID 不是正整数。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食材不存在。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /foods/{id} [get]
func (a *API) foodDetail(c *gin.Context) {
	id, ok := a.foodID(c)
	if !ok {
		return
	}
	f, err := a.service.Food(c.Request.Context(), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, f)
}

// adminFoods godoc
// @Summary 分页查询全部食材（管理员）
// @Description 仅管理员可用。返回全部状态食材摘要，按 ID 倒序；status 省略或为空表示不过滤。
// @Tags 管理
// @Produce json
// @Param q query string false "名称字面子串；处理后至多 100 码点。"
// @Param status query string false "状态筛选；省略或空字符串表示全部。" Enums(active,disabled)
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=foodPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页参数、q 超长或状态无效。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /admin/foods [get]
func (a *API) adminFoods(c *gin.Context) {
	q, status, page, size, ok := a.foodListParams(c, true)
	if !ok {
		return
	}
	items, total, err := a.service.AdminFoodList(c.Request.Context(), q, status, page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, foodPage{Items: items, Total: total})
}

// createFood godoc
// @Summary 创建食材（管理员）
// @Description 仅管理员可用。必须提交全部输入字段，未知信息显式传 null 或 false；id/version/created_at/updated_at 等服务端字段不允许出现。成功返回 201 与 version=1 详情，并写 food_created 审计；名称重复返回 409 FOOD_NAME_TAKEN。写事务内复核会话、账号状态与管理员角色。
// @Tags 管理
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param body body foodInput true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 201 {object} response{data=store.FoodDetail}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、格式错误、请求体超限或 Content-Type 非 application/json。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "FOOD_NAME_TAKEN：名称已存在；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /admin/foods [post]
func (a *API) createFood(c *gin.Context) {
	var p foodInput
	if !a.decodeFoodInput(c, &p) {
		return
	}
	if !requireFoodInput(p) {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "allergens 与 allergen_complete 不能为 null（未知请传 [] 与 false）"))
		return
	}
	f, err := a.service.CreateFood(c.Request.Context(), *session(c), foodWriteFromInput(p))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 201, f)
}

// updateFood godoc
// @Summary 全量更新食材（管理员）
// @Description 仅管理员可用。必须提交完整输入与当前 version；服务端按 id+version 条件更新，版本不一致返回 409 VERSION_CONFLICT。无变化时返回原详情，不升版本、不写审计；状态变化优先记 food_disabled/food_enabled，其余记 food_updated。同一 ID 必须始终指代同一种食材及加工形态，语义变更应创建新记录并停用旧记录。
// @Tags 管理
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param id path int64 true "食材 ID。" minimum(1)
// @Param body body foodUpdateInput true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 200 {object} response{data=store.FoodDetail}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、version 非法、请求体超限或 Content-Type 非 application/json。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食材不存在。"
// @Failure 409 {object} errorResponse "FOOD_NAME_TAKEN：名称已存在；VERSION_CONFLICT：版本已被他人修改；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /admin/foods/{id} [put]
func (a *API) updateFood(c *gin.Context) {
	id, ok := a.foodID(c)
	if !ok {
		return
	}
	var p foodUpdateInput
	if !a.decodeFoodInput(c, &p) {
		return
	}
	if !requireFoodInput(p.foodInput) {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "allergens 与 allergen_complete 不能为 null（未知请传 [] 与 false）"))
		return
	}
	if p.Version == nil || *p.Version <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部食材字段与有效的当前版本"))
		return
	}
	f, err := a.service.UpdateFood(c.Request.Context(), *session(c), id, *p.Version, foodWriteFromInput(p.foodInput))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, f)
}

// resourceAudit godoc
// @Summary 分页查询资源管理审计（管理员）
// @Description 仅管理员可用。返回 food/recipe 公共资源的管理审计事件，按 ID 倒序；resource_type 仅接受 food/recipe，resource_id 为正整数，action 精确匹配（如 food_created/food_updated/food_disabled/food_enabled）。账号审计仍走 GET /admin/audit-logs，两者互不混用。
// @Tags 管理
// @Produce json
// @Param resource_type query string false "资源类型筛选；省略或空字符串表示全部。" Enums(food,recipe)
// @Param resource_id query int64 false "按资源 ID 筛选；正整数。" minimum(1)
// @Param action query string false "精确匹配操作类型（实现限制为 64 字节）；省略或空字符串表示全部。" maxLength(64)
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=resourceAuditPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页参数或筛选条件无效。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /admin/resource-audit-logs [get]
func (a *API) resourceAudit(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	resourceType := c.Query("resource_type")
	if resourceType != "" && resourceType != "food" && resourceType != "recipe" {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的资源类型"))
		return
	}
	var resourceID int64
	if v, exists := c.GetQuery("resource_id"); exists {
		var e error
		resourceID, e = strconv.ParseInt(v, 10, 64)
		if e != nil || resourceID <= 0 {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的资源编号"))
			return
		}
	}
	action := c.Query("action")
	if len(action) > 64 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的操作类型"))
		return
	}
	items, total, err := a.service.Store.ListResourceAudit(c.Request.Context(), resourceType, resourceID, action, page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, resourceAuditPage{Items: items, Total: total})
}
