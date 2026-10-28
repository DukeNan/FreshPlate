package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"freshplate/server/internal/logic"
	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type mealItemInput struct {
	InventoryID int64  `json:"inventory_id"`
	Version     int64  `json:"version"`
	Quantity    string `json:"quantity"`
}
type mealCreateInput struct {
	RequestKey     string          `json:"request_key"`
	RecipeID       int64           `json:"recipe_id"`
	RecipeVersion  int64           `json:"recipe_version"`
	CookedServings string          `json:"cooked_servings"`
	EatenServings  string          `json:"eaten_servings"`
	AteAt          string          `json:"ate_at"`
	Items          []mealItemInput `json:"items"`
}
type mealOutput struct {
	ID             int64     `json:"id"`
	RecipeID       int64     `json:"recipe_id"`
	RecipeName     string    `json:"recipe_name"`
	RecipeVersion  int64     `json:"recipe_version"`
	CookedServings string    `json:"cooked_servings"`
	EatenServings  string    `json:"eaten_servings"`
	AteAt          time.Time `json:"ate_at"`
	CreatedAt      time.Time `json:"created_at"`
}

func mealResponse(m store.Meal) mealOutput {
	return mealOutput{m.ID, m.RecipeID, m.RecipeName, m.RecipeVersion, logic.FormatQuantity(m.CookedServingsMilli), logic.FormatQuantity(m.EatenServingsMilli), m.AteAt, m.CreatedAt}
}

// createMeal godoc
// @Summary 确认烹饪并记录饮食
// @Description 同一请求编号仅扣减一次；必须提交食谱各食材的本人有效库存批次及实际用量。版本或库存变化时拒绝保存。
// @Tags 饮食
// @Accept json
// @Produce json
// @Param X-Expected-User-ID header string true "表单加载时的用户 ID"
// @Param body body mealCreateInput true "烹饪确认"
// @Success 201 {object} response{data=mealOutput}
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 404 {object} errorResponse
// @Failure 409 {object} errorResponse
// @Failure 503 {object} errorResponse
// @Security BearerAuth
// @Router /meals [post]
func (a *API) createMeal(c *gin.Context) {
	var p mealCreateInput
	if !a.decodeInventory(c, &p) {
		return
	}
	invalid := func() {
		a.fail(c, store.E(400, "INVALID_ARGUMENT", "请检查食谱、份量、时间和库存批次"))
	}
	key, e := uuid.Parse(p.RequestKey)
	if e != nil || key.String() != p.RequestKey || p.RecipeID <= 0 || p.RecipeVersion <= 0 || len(p.Items) < 1 || len(p.Items) > 100 {
		invalid()
		return
	}
	cooked, e := logic.ParseQuantityMilli(p.CookedServings)
	if e != nil || cooked == 0 {
		invalid()
		return
	}
	eaten, e := logic.ParseQuantityMilli(p.EatenServings)
	if e != nil || eaten == 0 || eaten > cooked {
		invalid()
		return
	}
	ate, e := time.Parse(time.RFC3339, p.AteAt)
	if e != nil || ate.Year() < 1970 || ate.Year() > 9999 || ate.Nanosecond() != 0 {
		invalid()
		return
	}
	in := store.MealInput{RequestKey: p.RequestKey, RecipeID: p.RecipeID, RecipeVersion: p.RecipeVersion, CookedServingsMilli: cooked, EatenServingsMilli: eaten, AteAt: ate.UTC(), Items: make([]store.MealItemInput, 0, len(p.Items))}
	seen := map[int64]bool{}
	for _, v := range p.Items {
		q, e := logic.ParseQuantityMilli(v.Quantity)
		if e != nil || q == 0 || v.InventoryID <= 0 || v.Version <= 0 || seen[v.InventoryID] {
			invalid()
			return
		}
		seen[v.InventoryID] = true
		in.Items = append(in.Items, store.MealItemInput{InventoryID: v.InventoryID, Version: v.Version, QuantityMilli: q})
	}
	b, e := json.Marshal(in)
	if e != nil {
		a.fail(c, e)
		return
	}
	hash := sha256.Sum256(b)
	in.RequestHash = hex.EncodeToString(hash[:])
	m, e := a.service.CreateMeal(c.Request.Context(), *session(c), in)
	if e != nil {
		a.fail(c, e)
		return
	}
	success(c, 201, mealResponse(m))
}

// listMeals godoc
// @Summary 分页查询本人饮食记录
// @Tags 饮食
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response
// @Failure 401 {object} errorResponse
// @Security BearerAuth
// @Router /meals [get]
func (a *API) listMeals(c *gin.Context) {
	page, size, ok := a.page(c)
	if !ok {
		return
	}
	items, total, e := a.service.Store.ListMeals(c.Request.Context(), session(c).User.ID, page, size)
	if e != nil {
		a.fail(c, e)
		return
	}
	out := make([]mealOutput, 0, len(items))
	for _, m := range items {
		out = append(out, mealResponse(m))
	}
	success(c, 200, struct {
		Items []mealOutput `json:"items"`
		Total int          `json:"total"`
	}{out, total})
}
