package api

import (
	"encoding/json"
	"mime"
	"strconv"
	"time"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
)

// 库存模块 API（方案 §4）：/inventory-items CRUD 全部挂在登录用户组，
// 管理员也只能维护自己的库存。DTO 与 store 响应类型分开，只输出白名单
// 字段：不返回 user_id、内部千分单位 quantity_milli 与 deleted_at；
// quantity 为规范字符串，expiry_state 由服务端按同一 today 派生。

// inventoryCreateInput 是库存批次创建输入；user_id/deleted_at/version/
// created_at/updated_at/expiry_state 等服务端字段不在 DTO 中，出现即被
// decode 拒绝。数量只接受 ASCII 十进制字符串。
type inventoryCreateInput struct {
	FoodID    int64   `json:"food_id" binding:"required" minimum:"1" example:"1"`                                                                                             // 引用的标准食材；创建后不可修改。
	Quantity  string  `json:"quantity" binding:"required" example:"6"`                                                                                                        // ASCII 十进制字符串（0 或非零整数开头，可带 1~3 位小数），创建必须 >0；不接受数字类型。
	Unit      string  `json:"unit" binding:"required" enums:"g,ml,piece"`                                                                                                     // 原始单位；创建后不可修改。
	ExpiresOn *string `json:"expires_on" extensions:"x-nullable" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" example:"2026-10-25"`                                                 // 严格 YYYY-MM-DD；null/省略表示未知。
	Note      string  `json:"note" maxLength:"500" example:"第一批"`                                                                                                            // TrimSpace 后至多 500 码点。
}

// inventoryUpdateInput 是编辑输入：必须提交全部三个可变字段与当前
// version；food_id/unit 出现即 400（食材引用与原单位不可修改）。
// expires_on/note 用指针区分"缺失"（400）与显式 null/空串。
type inventoryUpdateInput struct {
	Quantity  string          `json:"quantity" binding:"required" example:"4.5"` // ASCII 十进制字符串；允许 "0" 表示用完。
	ExpiresOn json.RawMessage `json:"expires_on" binding:"required" swaggertype:"string" extensions:"x-nullable" example:"2026-10-25"` // 必填；null 表示未知到期日。
	Note      *string         `json:"note" binding:"required" maxLength:"500" example:"已用掉一部分"`                        // 必填（全量替换）；空串合法。
	Version   *int64          `json:"version" binding:"required" minimum:"1" example:"1"`                              // 期望旧版本；不一致返回 409。
}

// inventoryDeleteInput 是删除输入；version 必填正整数，缺失/null/非法
// 在查询资源存在性之前返回 400，永不退化为无版本删除。
type inventoryDeleteInput struct {
	Version *int64 `json:"version" binding:"required" minimum:"1" example:"2"`
}

// inventoryFoodOutput 是库存响应内嵌的食材摘要；停用食材保留名称与
// 停用标记。
type inventoryFoodOutput struct {
	ID     int64  `json:"id"`
	Name   string `json:"name" example:"燕麦片"`
	Status string `json:"status" enums:"active,disabled"`
}

// inventoryItemOutput 是详情/列表统一响应 DTO：id、food{id,name,status}、
// quantity（规范字符串）、unit、expires_on、note、version、expiry_state、
// created_at、updated_at。expiry_state 为服务端按 UTC 当日派生，不持久化。
type inventoryItemOutput struct {
	ID          int64               `json:"id"`
	Food        inventoryFoodOutput `json:"food"`
	Quantity    string              `json:"quantity" example:"1.25"`
	Unit        string              `json:"unit" enums:"g,ml,piece"`
	ExpiresOn   *string             `json:"expires_on" extensions:"x-nullable" example:"2026-10-25"` // null 表示未知到期日。
	Note        string              `json:"note" example:"第一批"`
	Version     int64               `json:"version"`
	ExpiryState string              `json:"expiry_state" enums:"fresh,expiring,expired,unknown,depleted"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

// inventoryPage 是库存列表分页响应；无结果时 items=[]。
type inventoryPage struct {
	Items []inventoryItemOutput `json:"items"`
	Total int                   `json:"total"`
}

// registerInventoryRoutes 注册库存模块全部路由；me 为登录用户组。
func (a *API) registerInventoryRoutes(me gin.IRouter) {
	me.GET("/inventory-items", a.inventoryList)
	me.GET("/inventory-items/:id", a.inventoryDetail)
	me.POST("/inventory-items", a.createInventory)
	me.PUT("/inventory-items/:id", a.updateInventory)
	me.DELETE("/inventory-items/:id", a.deleteInventory)
}

// inventoryID 解析路径中的批次 ID；非正整数一律按资源不存在返回 404，
// 不泄露编号格式信息，也使 /inventory-items/summary 等未提供路径统一 404。
func (a *API) inventoryID(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		a.fail(c, store.E(404, "NOT_FOUND", "接口不存在"))
		return 0, false
	}
	return id, true
}

// decodeInventory 在 decode 之上把非 application/json 也归入
// 400 INVALID_ARGUMENT，与食材写路径契约一致（方案 §4 错误码集合）。
func (a *API) decodeInventory(c *gin.Context, dst any) bool {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请求必须使用 application/json"))
		return false
	}
	return a.decode(c, dst)
}

// inventoryExpiresOn 解析 PUT 必填的 expires_on 字段：缺失返回 400，
// null 表示未知到期日，非字符串数值拒绝。
func (a *API) inventoryExpiresOn(c *gin.Context, raw json.RawMessage) (*string, bool) {
	if len(raw) == 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部可变字段"))
		return nil, false
	}
	if string(raw) == "null" {
		return nil, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "到期日必须是字符串或 null"))
		return nil, false
	}
	return &s, true
}

// inventoryOutput 把 store 行组装为统一 DTO：数量输出为规范字符串，
// expiry_state 用与筛选同源的 today 派生（同一次请求结果自洽）。
func inventoryOutput(item store.InventoryItem, today time.Time) inventoryItemOutput {
	return inventoryItemOutput{
		ID:          item.ID,
		Food:        inventoryFoodOutput{ID: item.FoodID, Name: item.FoodName, Status: item.FoodStatus},
		Quantity:    logic.FormatQuantity(item.QuantityMilli),
		Unit:        item.Unit,
		ExpiresOn:   item.ExpiresOn,
		Note:        item.Note,
		Version:     item.Version,
		ExpiryState: logic.ExpiryState(item.QuantityMilli, item.ExpiresOn, today),
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

// inventoryList godoc
// @Summary 分页查询本人库存
// @Description 仅登录用户可用。返回本人全部未删除批次（含数量为零项），按 ID 倒序；items 与 total 基于同一服务端 today 取得，筛选与响应 expiry_state 规则一致（depleted/expired/expiring/fresh/unknown）。q 为食材名称字面子串；不提供总质量/总数量聚合。日期统一按 UTC 判断。
// @Tags 库存
// @Produce json
// @Param q query string false "食材名称字面子串；处理后至多 100 码点。"
// @Param food_id query int64 false "按食材 ID 筛选；正整数。" minimum(1)
// @Param expiry_state query string false "到期状态筛选；省略或空字符串表示全部。" Enums(depleted,expired,expiring,fresh,unknown)
// @Param page query integer false "页码。" default(1) minimum(1) maximum(1000000)
// @Param page_size query integer false "每页条数。" default(20) minimum(1) maximum(100)
// @Success 200 {object} response{data=inventoryPage}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：分页、q、food_id 或 expiry_state 参数无效。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /inventory-items [get]
func (a *API) inventoryList(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	var foodID int64
	if v, exists := c.GetQuery("food_id"); exists {
		var e error
		foodID, e = strconv.ParseInt(v, 10, 64)
		if e != nil || foodID <= 0 {
			a.fail(c, store.E(400, "INVALID_ARGUMENT", "无效的食材编号"))
			return
		}
	}
	items, total, today, err := a.service.InventoryList(c.Request.Context(), *session(c), c.Query("q"), foodID, c.Query("expiry_state"), page, size)
	if err != nil {
		a.fail(c, err)
		return
	}
	outs := make([]inventoryItemOutput, 0, len(items))
	for _, item := range items {
		outs = append(outs, inventoryOutput(item, today))
	}
	success(c, 200, inventoryPage{Items: outs, Total: total})
}

// inventoryDetail godoc
// @Summary 读取本人库存批次详情
// @Description 仅登录用户可用。返回统一 DTO；停用食材的批次同样可读并保留名称与停用标记。不存在、已删除或他人批次一律 404，不可区分。
// @Tags 库存
// @Produce json
// @Param id path int64 true "库存批次 ID。" minimum(1)
// @Success 200 {object} response{data=inventoryItemOutput}
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：批次不存在、已删除或不属于本人。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，请稍后重试。"
// @Security BearerAuth
// @Router /inventory-items/{id} [get]
func (a *API) inventoryDetail(c *gin.Context) {
	id, ok := a.inventoryID(c)
	if !ok {
		return
	}
	item, today, err := a.service.Inventory(c.Request.Context(), *session(c), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, inventoryOutput(item, today))
}

// createInventory godoc
// @Summary 新建库存批次
// @Description 仅登录用户可用。数量必须为 >0 的 ASCII 十进制字符串（至多三位小数）；食材必须 active（否则 400 FOOD_UNAVAILABLE）；不因相同内容自动合并。user_id/version/deleted_at 等服务端字段不允许出现。成功返回 201 与 version=1 详情。
// @Tags 库存
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param body body inventoryCreateInput true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 201 {object} response{data=inventoryItemOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：字段缺失或格式错误、请求体超限或 Content-Type 非 application/json；FOOD_UNAVAILABLE：食材不存在或已停用。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 409 {object} errorResponse "IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /inventory-items [post]
func (a *API) createInventory(c *gin.Context) {
	var p inventoryCreateInput
	if !a.decodeInventory(c, &p) {
		return
	}
	item, today, err := a.service.CreateInventory(c.Request.Context(), *session(c), p.FoodID, p.Unit, p.Quantity, p.ExpiresOn, p.Note)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 201, inventoryOutput(item, today))
}

// updateInventory godoc
// @Summary 全量替换库存批次可变字段
// @Description 仅登录用户可用。必须提交 quantity/expires_on/note 与当前 version；不接受 food_id/unit（食材引用与原单位不可修改）。服务端按 id+user_id+version+未删除条件更新，版本不一致返回 409 VERSION_CONFLICT；关联食材停用时只允许减量、设零或改备注/到期日（增量返回 409 FOOD_DISABLED）；相同内容返回原详情不加版本。
// @Tags 库存
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param id path int64 true "库存批次 ID。" minimum(1)
// @Param body body inventoryUpdateInput true "请求参数；拒绝未知字段，最多 64 KiB"
// @Success 200 {object} response{data=inventoryItemOutput}
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：可变字段缺失、数量/日期非法、请求体超限或 Content-Type 非 application/json。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：批次不存在、已删除或不属于本人。"
// @Failure 409 {object} errorResponse "VERSION_CONFLICT：版本已被他人修改；FOOD_DISABLED：食材已停用只允许减量；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /inventory-items/{id} [put]
func (a *API) updateInventory(c *gin.Context) {
	id, ok := a.inventoryID(c)
	if !ok {
		return
	}
	var p inventoryUpdateInput
	if !a.decodeInventory(c, &p) {
		return
	}
	expiry, ok := a.inventoryExpiresOn(c, p.ExpiresOn)
	if !ok {
		return
	}
	if p.Note == nil {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交全部可变字段"))
		return
	}
	if p.Version == nil || *p.Version <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交有效的当前版本"))
		return
	}
	item, today, err := a.service.UpdateInventory(c.Request.Context(), *session(c), id, *p.Version, p.Quantity, expiry, *p.Note)
	if err != nil {
		a.fail(c, err)
		return
	}
	success(c, 200, inventoryOutput(item, today))
}

// deleteInventory godoc
// @Summary 删除库存批次
// @Description 仅登录用户可用。必须提交 JSON {"version":N}（必填正整数）：缺失、null 或非法值在查询资源存在性之前返回 400，代理丢弃请求体也只能导致拒绝，永不退化为无版本删除；不接受查询参数或 If-Match 替代通道。版本不一致返回 409；成功逻辑删除返回 204；已删除或他人批次一律 404。
// @Tags 库存
// @Produce json
// @Accept json
// @Param X-Expected-User-ID header string true "表单加载时登录用户 ID 的十进制规范字符串（int64 范围）；不一致返回 409。"
// @Param id path int64 true "库存批次 ID。" minimum(1)
// @Param body body inventoryDeleteInput true "删除请求；必须包含当前 version。"
// @Success 204 "成功，无响应体"
// @Failure 400 {object} errorResponse "INVALID_ARGUMENT：version 缺失/null/非法、请求体缺失或 Content-Type 非 application/json。"
// @Failure 401 {object} errorResponse "AUTH_REQUIRED：未登录或会话失效。"
// @Failure 403 {object} errorResponse "PASSWORD_CHANGE_REQUIRED：必须先修改密码。"
// @Failure 404 {object} errorResponse "NOT_FOUND：批次不存在、已删除或不属于本人。"
// @Failure 409 {object} errorResponse "VERSION_CONFLICT：版本已被他人修改；IDENTITY_CHANGED：页面身份不一致。"
// @Failure 500 {object} errorResponse "INTERNAL_ERROR：服务内部错误。"
// @Failure 503 {object} errorResponse "DATABASE_BUSY：数据库忙，本次写入未保存，请稍后手动重提。"
// @Security BearerAuth
// @Router /inventory-items/{id} [delete]
func (a *API) deleteInventory(c *gin.Context) {
	id, ok := a.inventoryID(c)
	if !ok {
		return
	}
	var p inventoryDeleteInput
	if !a.decodeInventory(c, &p) {
		return
	}
	// version 必填正整数校验先于资源存在性检查（INV-SEC-011）：
	// 对不存在的 id 提交非法 version 也返回 400 而非 404。
	if p.Version == nil || *p.Version <= 0 {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请提交有效的当前版本"))
		return
	}
	if err := a.service.DeleteInventory(c.Request.Context(), *session(c), id, *p.Version); err != nil {
		a.fail(c, err)
		return
	}
	success(c, 204, nil)
}
