package logic

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"freshplate/server/internal/store"
)

// 食谱模块业务规则（方案 §4）：纯文本字段按码点限长并拒绝控制字符
//（步骤允许 \n）；数量字符串沿用 quantity.go 的 ParseQuantityMilli 且
// 必须 >0；visibility/owner/status(POST)/version 由服务端决定，输入中
// 不存在；发布条件与状态机的事务内校验在 store 层完成，本层提供纯输入
// 校验与查询参数校验。

const (
	recipeNameMaxRunes     = 100
	recipeStepMaxRunes     = 1000
	recipeSourceMaxRunes   = 1000
	recipeAllergSrcMaxRunes = 500
	recipeStepsMaxCount    = 30
	recipeIngredientsMaxCount = 100
	recipeQueryMaxRunes    = 100
)

// recipeUnits 与数据库 CHECK 同一组原始单位；应用层先行拦截（REC-LOGIC-019）。
var recipeUnits = map[string]bool{"g": true, "ml": true, "piece": true}

// recipeStatuses 是列表 status 筛选允许的取值。
var recipeStatuses = map[string]bool{"draft": true, "published": true, "archived": true}

// RecipeIngredientForm 是一条原料的原始输入；Quantity 为十进制字符串，
// position 由数组顺序派生，输入结构中不存在该字段。
type RecipeIngredientForm struct {
	FoodID   int64
	Quantity string
	Unit     string
}

// RecipeForm 是创建/更新共用的原始输入表单；字段全部必填由 api 层判空。
type RecipeForm struct {
	Name                string
	Servings            int
	DurationMinutes     int
	Steps               []string
	Source              string
	AllergenComplete    bool
	AllergenSource      *string
	AdditionalAllergens []string
	Ingredients         []RecipeIngredientForm
}

// rejectBodyControlChars 拒绝无效 UTF-8 与不适合正文的控制字符；allowNL
// 仅对步骤文本为 true（\n 合法，\r/\t 等仍拒绝）。
func rejectBodyControlChars(s string, allowNL bool) error {
	if !utf8.ValidString(s) {
		return ErrInvalidArgument
	}
	for _, r := range s {
		if !unicode.IsControl(r) {
			continue
		}
		if allowNL && r == '\n' {
			continue
		}
		return ErrInvalidArgument
	}
	return nil
}

// ValidateRecipeForm 校验并规范化食谱输入（REC-LOGIC-001~015）：
//   - name TrimSpace 后 1~100 码点，拒绝控制字符与无效 UTF-8；
//   - servings 1~100；duration_minutes 1~1440；
//   - steps 0~30 个，每步 TrimSpace 后 1~1000 码点，允许 \n，空步骤拒绝；
//   - source TrimSpace 后至多 1000 码点，控制字符拒绝；
//   - ingredients 0~100 条，food_id 为正整数且不重复，数量字符串解析后
//     必须 >0（≤1000000000 milli），unit 仅 g/ml/piece；
//   - additional_allergens 仅接受支持编码，去重后至多 10 个；
//   - allergen_complete=true 时 allergen_source 必须 TrimSpace 非空。
func ValidateRecipeForm(f RecipeForm) (store.RecipeWrite, error) {
	f.Name = strings.TrimSpace(f.Name)
	if n := utf8.RuneCountInString(f.Name); n < 1 || n > recipeNameMaxRunes {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	if err := rejectBodyControlChars(f.Name, false); err != nil {
		return store.RecipeWrite{}, err
	}
	if f.Servings < 1 || f.Servings > 100 {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	if f.DurationMinutes < 1 || f.DurationMinutes > 1440 {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	// 数组/布尔键必须显式存在：nil 表示键缺失（api 层 requireRecipeInput
	// 同源约束），空数组才是合法的"无内容"。
	if f.Steps == nil || f.Ingredients == nil || f.AdditionalAllergens == nil {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	if len(f.Steps) > recipeStepsMaxCount {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	steps := make([]string, 0, len(f.Steps))
	for _, step := range f.Steps {
		step = strings.TrimSpace(step)
		n := utf8.RuneCountInString(step)
		if n < 1 || n > recipeStepMaxRunes {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
		if err := rejectBodyControlChars(step, true); err != nil {
			return store.RecipeWrite{}, err
		}
		steps = append(steps, step)
	}
	source := strings.TrimSpace(f.Source)
	if utf8.RuneCountInString(source) > recipeSourceMaxRunes {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	if err := rejectBodyControlChars(source, false); err != nil {
		return store.RecipeWrite{}, err
	}
	if f.AllergenSource != nil {
		if utf8.RuneCountInString(*f.AllergenSource) > recipeAllergSrcMaxRunes {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
	}
	if f.AllergenComplete && (f.AllergenSource == nil || strings.TrimSpace(*f.AllergenSource) == "") {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	if len(f.Ingredients) > recipeIngredientsMaxCount {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	seen := map[int64]bool{}
	ingredients := make([]store.RecipeIngredientInput, 0, len(f.Ingredients))
	for _, item := range f.Ingredients {
		if item.FoodID <= 0 || seen[item.FoodID] {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
		seen[item.FoodID] = true
		if !recipeUnits[item.Unit] {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
		milli, err := ParseQuantityMilli(item.Quantity)
		if err != nil || milli <= 0 {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
		ingredients = append(ingredients, store.RecipeIngredientInput{FoodID: item.FoodID, QuantityMilli: milli, Unit: item.Unit})
	}
	seenAllergen := map[string]bool{}
	allergens := make([]string, 0, len(f.AdditionalAllergens))
	for _, code := range f.AdditionalAllergens {
		if !allergenCodes[code] {
			return store.RecipeWrite{}, ErrInvalidArgument
		}
		if !seenAllergen[code] {
			seenAllergen[code] = true
			allergens = append(allergens, code)
		}
	}
	if len(allergens) > 10 {
		return store.RecipeWrite{}, ErrInvalidArgument
	}
	return store.RecipeWrite{
		Name: f.Name, Servings: f.Servings, DurationMinutes: f.DurationMinutes,
		Steps: steps, Source: source,
		AllergenComplete: f.AllergenComplete, AllergenSource: f.AllergenSource,
		AdditionalAllergens: allergens, Ingredients: ingredients,
	}, nil
}

// ValidateRecipeQuery 校验个人食谱列表参数（方案 §5）：scope 默认 public，
// 仅 published 公共且不接受 status；mine 返回本人私有全状态，可按
// draft/published/archived 筛选；q TrimSpace 后至多 100 码点。
func ValidateRecipeQuery(scope, q, status string, page, size int) (string, string, string, error) {
	if scope == "" {
		scope = "public"
	}
	if scope != "public" && scope != "mine" {
		return "", "", "", ErrInvalidArgument
	}
	if scope == "public" && status != "" {
		return "", "", "", ErrInvalidArgument
	}
	if status != "" && !recipeStatuses[status] {
		return "", "", "", ErrInvalidArgument
	}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > recipeQueryMaxRunes {
		return "", "", "", ErrInvalidArgument
	}
	if page < 1 || page > 1000000 {
		return "", "", "", ErrInvalidArgument
	}
	if size < 1 || size > 100 {
		return "", "", "", ErrInvalidArgument
	}
	return scope, q, status, nil
}

// ValidateRecipeAdminQuery 校验管理员公共列表参数：status 可筛选全部状态。
func ValidateRecipeAdminQuery(q, status string, page, size int) (string, error) {
	if status != "" && !recipeStatuses[status] {
		return "", ErrInvalidArgument
	}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > recipeQueryMaxRunes {
		return "", ErrInvalidArgument
	}
	if page < 1 || page > 1000000 {
		return "", ErrInvalidArgument
	}
	if size < 1 || size > 100 {
		return "", ErrInvalidArgument
	}
	return q, nil
}

// RecipeList 返回个人可见列表：scope=public 仅已发布公共食谱，scope=mine
// 返回本人私有全状态（status 可筛选）。
func (s *Service) RecipeList(ctx context.Context, actor store.Session, scope, q, status string, page, size int) ([]store.RecipeSummary, int, error) {
	scope, q, status, err := ValidateRecipeQuery(scope, q, status, page, size)
	if err != nil {
		return nil, 0, err
	}
	query := store.RecipeListQuery{Q: q, Page: page, Size: size}
	if scope == "public" {
		query.Visibility = "public"
		query.Status = "published"
	} else {
		query.Visibility = "private"
		query.OwnerID = actor.User.ID
		query.Status = status
	}
	return s.Store.ListRecipes(ctx, query)
}

// AdminRecipeList 返回全部状态公共食谱分页；不含任何人的私有食谱。
func (s *Service) AdminRecipeList(ctx context.Context, q, status string, page, size int) ([]store.RecipeSummary, int, error) {
	q, err := ValidateRecipeAdminQuery(q, status, page, size)
	if err != nil {
		return nil, 0, err
	}
	return s.Store.ListRecipes(ctx, store.RecipeListQuery{Visibility: "public", Status: status, Q: q, Page: page, Size: size})
}

// Recipe 读取普通详情：已发布公共或本人私有，其余 404。
func (s *Service) Recipe(ctx context.Context, actor store.Session, id int64) (store.RecipeDetail, error) {
	if id <= 0 {
		return store.RecipeDetail{}, ErrInvalidArgument
	}
	return s.Store.RecipeDetail(ctx, id, store.RecipeDetailReadScope{ViewerID: actor.User.ID})
}

// AdminRecipe 读取管理员详情：仅公共食谱任意状态，私有一律 404。
func (s *Service) AdminRecipe(ctx context.Context, id int64) (store.RecipeDetail, error) {
	if id <= 0 {
		return store.RecipeDetail{}, ErrInvalidArgument
	}
	return s.Store.RecipeDetail(ctx, id, store.RecipeDetailReadScope{Admin: true})
}

// CreateRecipe 校验后创建食谱；public=true 为管理员公共路径（owner NULL），
// 否则为本人私有路径。POST 恒为 draft，status 输入不存在。
func (s *Service) CreateRecipe(ctx context.Context, actor store.Session, public bool, form RecipeForm) (store.RecipeDetail, error) {
	in, err := ValidateRecipeForm(form)
	if err != nil {
		return store.RecipeDetail{}, err
	}
	return s.Store.CreateRecipe(ctx, actor, public, in, s.Now())
}

// UpdateRecipe 校验后全量更新食谱内容与状态；version 为期望旧版本。
// 状态机、发布条件、保留停用引用例外与审计在 store 写事务内复核。
func (s *Service) UpdateRecipe(ctx context.Context, actor store.Session, public bool, id, version int64, status string, form RecipeForm) (store.RecipeDetail, error) {
	if id <= 0 || version <= 0 {
		return store.RecipeDetail{}, ErrInvalidArgument
	}
	if status != "draft" && status != "published" && status != "archived" {
		return store.RecipeDetail{}, ErrInvalidArgument
	}
	in, err := ValidateRecipeForm(form)
	if err != nil {
		return store.RecipeDetail{}, err
	}
	return s.Store.UpdateRecipe(ctx, actor, public, id, version, status, in, s.Now())
}
