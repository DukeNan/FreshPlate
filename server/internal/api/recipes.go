package api

import (
	"mime"
	"strconv"
	"time"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// 食谱模块 API（方案 §5）：/recipes 为本人私有链路，/admin/recipes 为
// 公共食谱管理链路；visibility/owner_user_id/status(POST)/version 由
// 服务端决定，DTO 中不存在，出现即被 decode 拒绝。四条写路由请求体上限
// 为 256 KiB（api.bodyLimit），其余接口保持 64 KiB。

// recipeIngredientInput 是一条原料输入；数量只接受 ASCII 十进制字符串，
// position 不接受输入（由数组顺序派生），出现即 400。
type recipeIngredientInput struct {
	FoodID   int64  `json:"food_id" binding:"required" minimum:"1" example:"11"` // 引用的标准食材；同一食谱不可重复。
	Quantity string `json:"quantity" binding:"required" example:"250"`           // ASCII 十进制字符串（可带 1~3 位小数），必须 >0；不接受数字类型。
	Unit     string `json:"unit" binding:"required" enums:"g,ml,piece"`          // 原始单位。
}

// recipeInput 是食谱创建输入；POST 全部字段必须显式提交（空步骤/空原料
// 合法，键不可缺失），未知信息按方案语义显式传 null。id/visibility/
// owner_user_id/status/version/created_at/updated_at 等服务端字段不在
// DTO 中，出现即被 decode 拒绝。
type recipeInput struct {
	Name                string                   `json:"name" binding:"required" example:"番茄炒蛋"` // 去除首尾空白后 1～100 码点，不含控制字符。
	Servings            *int                     `json:"servings" binding:"required" minimum:"1" maximum:"100" example:"2"`          // 整份食谱对应的份数。
	DurationMinutes     *int                     `json:"duration_minutes" binding:"required" minimum:"1" maximum:"1440" example:"15"` // 预计总耗时（分钟）。
	Steps               *[]string                `json:"steps" binding:"required" maxItems:"30"`                                     // 纯文本步骤数组；每步去除首尾空白后 1～1000 码点，允许换行。
	Source              *string                  `json:"source" extensions:"x-nullable" maxLength:"1000" example:"个人记录"`             // 来源说明或 URL；键必须存在，空串表示未填写。
	Ingredients         *[]recipeIngredientInput `json:"ingredients" binding:"required" maxItems:"100"`                              // 0～100 条；position 由数组顺序生成。
	AdditionalAllergens *[]string                `json:"additional_allergens" binding:"required" maxItems:"10" enums:"milk,egg,fish,crustacean,mollusc,peanut,soy,wheat,tree_nut,sesame"` // 额外标注；去重后至多 10 个，[] 表示无。
	AllergenComplete    *bool                    `json:"allergen_complete" binding:"required"`                                       // true=已核对食谱层面额外风险；必须提供核对依据。
	AllergenSource      *string                  `json:"allergen_source" extensions:"x-nullable" maxLength:"500"`                    // 核对依据；complete=true 时必填。
}

// recipeUpdateInput 是更新输入：与创建相同，并要求提交目标 status 与当前
// 乐观并发版本；PUT 归档也提交完整内容。
type recipeUpdateInput struct {
	recipeInput
	Status  *string `json:"status" binding:"required" enums:"draft,published,archived"` // 目标状态；非法转换返回 409。
	Version *int64  `json:"version" binding:"required" minimum:"1"`                     // 服务端按 id+version 条件更新，不一致返回 409。
}

// recipeFoodOutput 是原料内嵌的食物摘要；停用食材保留名称与停用标记。
type recipeFoodOutput struct {
	ID     int64  `json:"id"`
	Name   string `json:"name" example:"番茄"`
	Status string `json:"status" enums:"active,disabled"`
}

// recipeIngredientOutput 是详情原料行：position 由数组顺序派生，quantity
// 为规范数量字符串；不返回内部千分单位 quantity_milli。
type recipeIngredientOutput struct {
	Position int64             `json:"position"`
	Food     recipeFoodOutput  `json:"food"`
	Quantity string            `json:"quantity" example:"250"`
	Unit     string            `json:"unit" enums:"g,ml,piece"`
}

// recipeOutput 是详情响应 DTO：全部输入字段 + 服务端字段与派生字段。
// effective_allergens 为 food 当前标注与附加标注的合并去重排序；
// restriction_metadata_complete/has_disabled_foods 读取时派生。
type recipeOutput struct {
	ID                          int64                    `json:"id"`
	Visibility                  string                   `json:"visibility" enums:"private,public"`
	Status                      string                   `json:"status" enums:"draft,published,archived"`
	Name                        string                   `json:"name"`
	Servings                    int                      `json:"servings"`
	DurationMinutes             int                      `json:"duration_minutes"`
	Steps                       []string                 `json:"steps"`
	Source                      string                   `json:"source"`
	Ingredients                 []recipeIngredientOutput `json:"ingredients"`
	AdditionalAllergens         []string                 `json:"additional_allergens"`
	AllergenComplete            bool                     `json:"allergen_complete"`
	AllergenSource              *string                  `json:"allergen_source"`
	EffectiveAllergens          []string                 `json:"effective_allergens"`
	RestrictionMetadataComplete bool                     `json:"restriction_metadata_complete"`
	HasDisabledFoods            bool                     `json:"has_disabled_foods"`
	Version                     int64                    `json:"version"`
	CreatedAt                   time.Time                `json:"created_at"`
	UpdatedAt                   time.Time                `json:"updated_at"`
}

// recipeSummaryOutput 是列表摘要：不含步骤全量内容，携带派生警示字段。
type recipeSummaryOutput struct {
	ID                          int64     `json:"id"`
	Visibility                  string    `json:"visibility" enums:"private,public"`
	Status                      string    `json:"status" enums:"draft,published,archived"`
	Name                        string    `json:"name"`
	Servings                    int       `json:"servings"`
	DurationMinutes             int       `json:"duration_minutes"`
	Version                     int64     `json:"version"`
	UpdatedAt                   time.Time `json:"updated_at"`
	RestrictionMetadataComplete bool      `json:"restriction_metadata_complete"`
	HasDisabledFoods            bool      `json:"has_disabled_foods"`
}

// recipePage 是列表分页响应；无结果时 items=[]。
type recipePage struct {
	Items []recipeSummaryOutput `json:"items"`
	Total int                   `json:"total"`
}

// registerRecipeRoutes 注册食谱模块全部路由：me 为登录用户组（私有链路），
// admin 为管理员组（公共链路）。不提供 DELETE；归档取代物理删除。
func (a *API) registerRecipeRoutes(me, admin gin.IRouter) {
	me.GET("/recipes", a.recipes)
	me.GET("/recipes/:id", a.recipeDetail)
	me.POST("/recipes", a.createRecipe)
	me.PUT("/recipes/:id", a.updateRecipe)
	admin.GET("/recipes", a.adminRecipes)
	admin.GET("/recipes/:id", a.adminRecipeDetail)
	admin.POST("/recipes", a.adminCreateRecipe)
	admin.PUT("/recipes/:id", a.adminUpdateRecipe)
}

// recipeID 解析路径中的食谱 ID；必须为正整数，否则返回 400。
func (a *API) recipeID(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的食谱编号"))
		return 0, false
	}
	return id, true
}

// decodeRecipe 在 decode 之上将非 application/json 也归入 400
// INVALID_ARGUMENT：食谱写路径契约只暴露 INVALID_ARGUMENT 等错误码。
func (a *API) decodeRecipe(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求必须使用 application/json"))
		return false
	}
	return a.decode(c, dst)
}

// requireRecipeInput 校验 POST/PUT 全字段必填：数组与布尔必须显式提交
//（空数组/false 合法，键不可缺失），servings/duration/source 键必须存在。
func requireRecipeInput(p recipeInput) bool {
	return p.Servings != nil && p.DurationMinutes != nil && p.Steps != nil &&
		p.Source != nil && p.Ingredients != nil && p.AdditionalAllergens != nil && p.AllergenComplete != nil
}

// recipeFormFromInput 把请求 DTO 转为 logic 表单；调用前必须先通过
// requireRecipeInput。
func recipeFormFromInput(p recipeInput) logic.RecipeForm {
	form := logic.RecipeForm{
		Name:                p.Name,
		Servings:            *p.Servings,
		DurationMinutes:     *p.DurationMinutes,
		Steps:               *p.Steps,
		Source:              *p.Source,
		AllergenComplete:    *p.AllergenComplete,
		AllergenSource:      p.AllergenSource,
		AdditionalAllergens: *p.AdditionalAllergens,
		// Ingredients 必须初始化为非 nil 空切片：请求里的空数组是合法的
		// 「无原料」（REC-LOGIC-001 空草稿可创建），nil 会被
		// ValidateRecipeForm 视为键缺失而拒绝。Steps/AdditionalAllergens
		// 因直接解引用输入指针天然非 nil，这里必须显式给空切片。
		Ingredients: []logic.RecipeIngredientForm{},
	}
	for _, item := range *p.Ingredients {
		form.Ingredients = append(form.Ingredients, logic.RecipeIngredientForm{FoodID: item.FoodID, Quantity: item.Quantity, Unit: item.Unit})
	}
	return form
}

// recipeOutputFromDetail 把 store 详情转为响应 DTO；quantity 由千分单位
// 格式化为规范字符串。
func recipeOutputFromDetail(d store.RecipeDetail) recipeOutput {
	out := recipeOutput{
		ID: d.ID, Visibility: d.Visibility, Status: d.Status, Name: d.Name,
		Servings: d.Servings, DurationMinutes: d.DurationMinutes,
		Steps: d.Steps, Source: d.Source,
		Ingredients:             []recipeIngredientOutput{},
		AdditionalAllergens:     d.AdditionalAllergens,
		AllergenComplete:        d.AllergenComplete,
		AllergenSource:          d.AllergenSource,
		EffectiveAllergens:      d.EffectiveAllergens,
		RestrictionMetadataComplete: d.RestrictionMetadataComplete,
		HasDisabledFoods:        d.HasDisabledFoods,
		Version:                 d.Version,
		CreatedAt:               d.CreatedAt,
		UpdatedAt:               d.UpdatedAt,
	}
	if out.Steps == nil {
		out.Steps = []string{}
	}
	for _, item := range d.Ingredients {
		out.Ingredients = append(out.Ingredients, recipeIngredientOutput{
			Position: item.Position,
			Food:     recipeFoodOutput{ID: item.FoodID, Name: item.FoodName, Status: item.FoodStatus},
			Quantity: logic.FormatQuantity(item.QuantityMilli),
			Unit:     item.Unit,
		})
	}
	return out
}

// recipePageFromSummaries 把列表摘要转为响应 DTO。
func recipePageFromSummaries(items []store.RecipeSummary, total int) recipePage {
	out := make([]recipeSummaryOutput, 0, len(items))
	for _, it := range items {
		out = append(out, recipeSummaryOutput{
			ID: it.ID, Visibility: it.Visibility, Status: it.Status, Name: it.Name,
			Servings: it.Servings, DurationMinutes: it.DurationMinutes,
			Version: it.Version, UpdatedAt: it.UpdatedAt,
			RestrictionMetadataComplete: it.RestrictionMetadataComplete,
			HasDisabledFoods:            it.HasDisabledFoods,
		})
	}
	return recipePage{Items: out, Total: total}
}

// recipes godoc
// @Summary 食谱列表（公共/我的）
// @Description 仅登录用户可用。scope=public（默认）仅返回已发布公共食谱，不接受 status 筛选；scope=mine 返回本人私有食谱的全部状态，可按 status 筛选。q 为去除首尾空白后至多 100 码点的名称字面子串；按 ID 倒序分页。items 携带读取时派生的警示字段，不含步骤全量内容。
// @Tags 食谱
// @Produce json
// @Param scope query string false "列表范围。" Enums(public,mine) default(public)
// @Param q query string false "名称字面子串；处理后至多 100 码点。"
// @Param status query string false "仅 scope=mine 可用；省略或空表示全部状态。" Enums(draft,published,archived)
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=recipePage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页参数、q 超长、scope 或 status 无效。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /recipes [get]
func (a *API) recipes(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	items, total, err := a.service.RecipeList(c.Request.Context(), *session(c), c.Query("scope"), c.Query("q"), c.Query("status"), page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipePageFromSummaries(items, total))
}

// recipeDetail godoc
// @Summary 读取食谱详情
// @Description 仅登录用户可用。返回已发布公共食谱或本人私有食谱的完整详情，含原料（position 与食物 id/name/status）、有效过敏原合并结果与派生警示；其余（未发布/已撤回公共、他人私有、不存在）一律 404，不区分原因。
// @Tags 食谱
// @Produce json
// @Param id path int64 true "食谱 ID。" minimum(1)
// @Success 200 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：食谱 ID 不是正整数。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食谱不存在、未发布或不属于本人。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /recipes/{id} [get]
func (a *API) recipeDetail(c *gin.Context) {
	id, ok := a.recipeID(c)
	if !ok {
		return
	}
	d, err := a.service.Recipe(c.Request.Context(), *session(c), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipeOutputFromDetail(d))
}

// createRecipe godoc
// @Summary 创建私有食谱
// @Description 仅登录用户可用。始终创建本人 private/draft（version=1）；不接受 visibility/owner_user_id/status/version，出现即 400。请求全部输入字段必须显式提交（空步骤/空原料合法）；请求体上限 256 KiB。私有食谱不写管理审计。
// @Tags 食谱
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param body body recipeInput true "请求参数；拒绝未知字段，最多 256 KiB"
// @Success 201 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、数量/单位/过敏原编码非法、请求体超限或 Content-Type 非 application/json；FOOD_UNAVAILABLE：引用的食材不存在或已停用。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /recipes [post]
func (a *API) createRecipe(c *gin.Context) {
	var p recipeInput
	if !a.decodeRecipe(c, &p) {
		return
	}
	if !requireRecipeInput(p) {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部食谱字段，空内容显式传空数组或 false"))
		return
	}
	d, err := a.service.CreateRecipe(c.Request.Context(), *session(c), false, recipeFormFromInput(p))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 201, recipeOutputFromDetail(d))
}

// updateRecipe godoc
// @Summary 更新本人私有食谱
// @Description 仅登录用户可用。必须提交完整输入、目标 status 与当前 version；按 id+version 条件更新，版本不一致返回 409 VERSION_CONFLICT。仅能修改本人私有食谱：公共或他人食谱一律 404。归档也提交完整内容，不提供 DELETE。
// @Tags 食谱
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param id path int64 true "食谱 ID。" minimum(1)
// @Param body body recipeUpdateInput true "请求参数；拒绝未知字段，最多 256 KiB"
// @Success 200 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、version/status 非法、请求体超限或 Content-Type 非 application/json；FOOD_UNAVAILABLE：新增引用的食材不存在或已停用。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：权限不足；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食谱不存在或不属于本人。"
// @Failure 409 {object} errorResponse "RECIPE_NOT_PUBLISHABLE：不满足发布条件；INVALID_RECIPE_TRANSITION：非法状态转换；VERSION_CONFLICT：版本已被他人修改；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /recipes/{id} [put]
func (a *API) updateRecipe(c *gin.Context) {
	id, ok := a.recipeID(c)
	if !ok {
		return
	}
	var p recipeUpdateInput
	if !a.decodeRecipe(c, &p) {
		return
	}
	if !requireRecipeInput(p.recipeInput) || p.Status == nil || p.Version == nil || *p.Version <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部食谱字段、目标状态与有效的当前版本"))
		return
	}
	d, err := a.service.UpdateRecipe(c.Request.Context(), *session(c), false, id, *p.Version, *p.Status, recipeFormFromInput(p.recipeInput))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipeOutputFromDetail(d))
}

// adminRecipes godoc
// @Summary 管理员公共食谱列表
// @Description 仅管理员可用。返回全部状态的公共食谱，不含任何用户的私有食谱；status 省略或为空表示不过滤。按 ID 倒序分页，items 携带派生警示字段。
// @Tags 管理
// @Produce json
// @Param q query string false "名称字面子串；处理后至多 100 码点。"
// @Param status query string false "状态筛选；省略或空字符串表示全部。" Enums(draft,published,archived)
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=recipePage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页参数、q 超长或状态无效。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /admin/recipes [get]
func (a *API) adminRecipes(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	items, total, err := a.service.AdminRecipeList(c.Request.Context(), c.Query("q"), c.Query("status"), page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipePageFromSummaries(items, total))
}

// adminRecipeDetail godoc
// @Summary 管理员读取公共食谱详情
// @Description 仅管理员可用。返回任意状态公共食谱的完整详情；私有食谱一律 404，管理员权限不突破私有边界。
// @Tags 管理
// @Produce json
// @Param id path int64 true "食谱 ID。" minimum(1)
// @Success 200 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：食谱 ID 不是正整数。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食谱不存在或为私有食谱。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /admin/recipes/{id} [get]
func (a *API) adminRecipeDetail(c *gin.Context) {
	id, ok := a.recipeID(c)
	if !ok {
		return
	}
	d, err := a.service.AdminRecipe(c.Request.Context(), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipeOutputFromDetail(d))
}

// adminCreateRecipe godoc
// @Summary 创建公共食谱（管理员）
// @Description 仅管理员可用。始终创建 public/draft（version=1），公共食谱不属于管理员个人账号（owner 为空）；不接受 visibility/owner_user_id/status/version，出现即 400。请求体上限 256 KiB。创建写入 recipe_created 资源审计。
// @Tags 管理
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param body body recipeInput true "请求参数；拒绝未知字段，最多 256 KiB"
// @Success 201 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、数量/单位/过敏原编码非法、请求体超限或 Content-Type 非 application/json；FOOD_UNAVAILABLE：引用的食材不存在或已停用。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /admin/recipes [post]
func (a *API) adminCreateRecipe(c *gin.Context) {
	var p recipeInput
	if !a.decodeRecipe(c, &p) {
		return
	}
	if !requireRecipeInput(p) {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部食谱字段，空内容显式传空数组或 false"))
		return
	}
	d, err := a.service.CreateRecipe(c.Request.Context(), *session(c), true, recipeFormFromInput(p))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 201, recipeOutputFromDetail(d))
}

// adminUpdateRecipe godoc
// @Summary 管理员编辑公共食谱
// @Description 仅管理员可用。提交完整输入、目标 status 与当前 version；支持发布、撤回为草稿与归档，archived→published 必须先恢复草稿。发布或保持 published 的编辑要求全部食材 active 且过敏原完整、食谱自身核对完整且有依据，不满足返回 409 RECIPE_NOT_PUBLISHABLE 并附原因。无变化返回原详情，不升版本、不写审计；状态变化优先记录对应审计动作。校验不套用发布者个人忌口。
// @Tags 管理
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param id path int64 true "食谱 ID。" minimum(1)
// @Param body body recipeUpdateInput true "请求参数；拒绝未知字段，最多 256 KiB"
// @Success 200 {object} response{data=recipeOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失、version/status 非法、请求体超限或 Content-Type 非 application/json；FOOD_UNAVAILABLE：新增引用的食材不存在或已停用。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "FORBIDDEN：需要管理员权限；PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：食谱不存在或为私有食谱。"
// @Failure 409 {object} errorResponse "RECIPE_NOT_PUBLISHABLE：不满足发布条件；INVALID_RECIPE_TRANSITION：非法状态转换；VERSION_CONFLICT：版本已被他人修改；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /admin/recipes/{id} [put]
func (a *API) adminUpdateRecipe(c *gin.Context) {
	id, ok := a.recipeID(c)
	if !ok {
		return
	}
	var p recipeUpdateInput
	if !a.decodeRecipe(c, &p) {
		return
	}
	if !requireRecipeInput(p.recipeInput) || p.Status == nil || p.Version == nil || *p.Version <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部食谱字段、目标状态与有效的当前版本"))
		return
	}
	d, err := a.service.UpdateRecipe(c.Request.Context(), *session(c), true, id, *p.Version, *p.Status, recipeFormFromInput(p.recipeInput))
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, recipeOutputFromDetail(d))
}
