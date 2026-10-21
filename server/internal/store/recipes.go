package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 食谱模块存储层（方案 §3/§5/§6）。父表保存原始输入，原料与附加过敏原
// 为子表并在写事务内整体替换；visibility 由创建路径决定且永不修改：
// private 必须 owner_user_id=当前用户，public 归属为 NULL 且仅管理员路径。
// 归档取代物理删除，不提供 DELETE。派生字段（has_disabled_foods、
// restriction_metadata_complete、effective_allergens）永不持久化，读取时
// 在同一只读事务内按当前 foods 数据重新计算，禁止逐条/逐原料查询。

// recipeColumns 是 recipes 表全部列；owner_user_id 仅用于所有权过滤，
// 任何响应 DTO 都不得携带他人身份。
const recipeColumns = "id,visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,source,allergen_complete,allergen_source,version,created_at,updated_at"

// RecipeIngredientInput 是一条原料的持久化输入；quantity_milli 已由 logic
// 层解析校验，position 由数组顺序在写入时生成，不接受外部输入。
type RecipeIngredientInput struct {
	FoodID        int64
	QuantityMilli int64
	Unit          string
}

// RecipeWrite 是创建/更新共用的可编辑字段集合；visibility/owner/status/
// version/时间戳由服务端决定，不在其中。创建时 status 恒为 draft。
type RecipeWrite struct {
	Name                string
	Servings            int
	DurationMinutes     int
	Steps               []string
	Source              string
	AllergenComplete    bool
	AllergenSource      *string
	AdditionalAllergens []string
	Ingredients         []RecipeIngredientInput
}

// RecipeIngredientRow 是详情中的原料行，附食物名称与当前状态（含停用
// 标记，用于前端警示）；QuantityMilli 为内部千分单位，由 api 层格式化。
type RecipeIngredientRow struct {
	Position      int64
	FoodID        int64
	FoodName      string
	FoodStatus    string
	QuantityMilli int64
	Unit          string
}

// RecipeDetail 是食谱完整详情；steps 为解析后的数组，AdditionalAllergens
// 是附加过敏原编码，EffectiveAllergens 为 food 过敏原与附加的合并去重
// 排序结果。派生字段随当前 foods 数据读取时计算。刻意不带 JSON 标签：
// 响应 DTO 在 api 层单独定义，不暴露 owner_user_id 与 quantity_milli。
type RecipeDetail struct {
	ID                          int64
	Visibility                  string
	Status                      string
	Name                        string
	Servings                    int
	DurationMinutes             int
	Steps                       []string
	Source                      string
	AllergenComplete            bool
	AllergenSource              *string
	AdditionalAllergens         []string
	Ingredients                 []RecipeIngredientRow
	EffectiveAllergens          []string
	RestrictionMetadataComplete bool
	HasDisabledFoods            bool
	Version                     int64
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

// RecipeSummary 是列表摘要行；派生警示字段由批量聚合得出。
type RecipeSummary struct {
	ID                          int64
	Visibility                  string
	Status                      string
	Name                        string
	Servings                    int
	DurationMinutes             int
	Version                     int64
	UpdatedAt                   time.Time
	RestrictionMetadataComplete bool
	HasDisabledFoods            bool
}

// recipeNotFound 统一表示不存在/越权/状态不可见三类情形；调用方不得据
// 错误区分差异（方案 §5：一律 404 不泄露资源存在性）。
var recipeNotFound = E(404, "NOT_FOUND", "食谱不存在")

// recipeRow 是 recipes 父行的原始扫描结果（含 owner_user_id 供所有权过滤）。
type recipeRow struct {
	ID               int64
	Visibility       string
	OwnerID          sql.NullInt64 `gorm:"column:owner_user_id"`
	Status           string
	Name             string
	Servings         int
	DurationMinutes  int
	StepsJSON        string
	Source           string
	AllergenComplete bool
	AllergenSource   *string
	Version          int64
	CreatedAt        int64 `gorm:"autoCreateTime:false"`
	UpdatedAt        int64 `gorm:"autoUpdateTime:false"`
}

func scanRecipeRow(r scanner) (recipeRow, error) {
	var row recipeRow
	if err := r.Scan(&row.ID, &row.Visibility, &row.OwnerID, &row.Status, &row.Name, &row.Servings, &row.DurationMinutes, &row.StepsJSON, &row.Source, &row.AllergenComplete, &row.AllergenSource, &row.Version, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return recipeRow{}, err
	}
	return row, nil
}

// ValidateRecipeTransition 校验状态机（方案 §2）：draft→published、
// published→draft、draft/published→archived、archived→draft 允许；
// archived→published 拒绝。相同状态不构成转换（内容更新路径），直接放行。
func ValidateRecipeTransition(from, to string) error {
	if from == to {
		return nil
	}
	allowed := (from == "draft" && to == "published") ||
		(from == "published" && to == "draft") ||
		(from == "draft" && to == "archived") ||
		(from == "published" && to == "archived") ||
		(from == "archived" && to == "draft")
	if !allowed {
		return E(409, "INVALID_RECIPE_TRANSITION", "归档食谱必须先恢复为草稿才能发布")
	}
	return nil
}

// RecipeFoodMeta 是发布校验所需的食材元数据快照（事务内重读结果）。
type RecipeFoodMeta struct {
	Status           string
	AllergenComplete bool
}

// RecipePublishProblems 返回不满足发布条件的原因列表（供 409 附带），
// 空切片表示可以处于 published。触发条件：目标状态为 published，或保持
// published 的任何编辑。校验只看基础数据与完整性，不套用发布者个人忌口。
func RecipePublishProblems(selfComplete bool, allergenSource *string, stepCount, ingredientCount int, foods []RecipeFoodMeta) []string {
	problems := []string{}
	if stepCount < 1 {
		problems = append(problems, "发布前至少需要一个步骤")
	}
	if ingredientCount < 1 {
		problems = append(problems, "发布前至少需要一种食材")
	}
	for _, f := range foods {
		if f.Status != "active" {
			problems = append(problems, "存在已停用食材，发布前必须移除或替换")
			break
		}
	}
	for _, f := range foods {
		if !f.AllergenComplete {
			problems = append(problems, "存在过敏原标注不完整的食材")
			break
		}
	}
	if !selfComplete {
		problems = append(problems, "食谱层面的过敏原核对尚未标记完成")
	}
	if allergenSource == nil || strings.TrimSpace(*allergenSource) == "" {
		problems = append(problems, "缺少过敏原核对依据")
	}
	return problems
}

// recipeAuditAction 选择公共食谱更新审计动作（REC-LOGIC-016）：状态动作
// 优先，无状态变化记 updated；一笔变更只写一条审计。
func recipeAuditAction(before, after string) string {
	if before == after {
		return "recipe_updated"
	}
	switch after {
	case "published":
		return "recipe_published"
	case "archived":
		return "recipe_archived"
	case "draft":
		if before == "archived" {
			return "recipe_restored"
		}
		return "recipe_unpublished"
	}
	return "recipe_updated"
}

// recipeWriteSteps 序列化步骤数组为 steps_json；logic 层保证非 nil。
func recipeWriteSteps(steps []string) (string, error) {
	if steps == nil {
		steps = []string{}
	}
	b, err := json.Marshal(steps)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// recipeStepsEqual 比较 json 数组文本与期望步骤（顺序敏感）。
func recipeStepsEqual(storedJSON string, steps []string) bool {
	var stored []string
	if err := json.Unmarshal([]byte(storedJSON), &stored); err != nil {
		return false
	}
	return slices.Equal(stored, steps)
}

// recipeWriteEqual 判断请求内容（含目标状态）与当前详情是否完全一致；
// 一致则无变化 PUT：不升版本、不写审计、返回原详情。
func recipeWriteEqual(old RecipeDetail, status string, in RecipeWrite) bool {
	if old.Status != status || old.Name != in.Name || old.Servings != in.Servings ||
		old.DurationMinutes != in.DurationMinutes || old.Source != in.Source ||
		old.AllergenComplete != in.AllergenComplete {
		return false
	}
	if (old.AllergenSource == nil) != (in.AllergenSource == nil) {
		return false
	}
	if in.AllergenSource != nil && *old.AllergenSource != *in.AllergenSource {
		return false
	}
	// steps 与其余字段一致参与比较：仅改步骤（编辑/排序/增删）不能走无变化分支。
	if !recipeStepsEqual(stepsJSONOf(old), in.Steps) {
		return false
	}
	oldExtra := append([]string(nil), old.AdditionalAllergens...)
	newExtra := append([]string(nil), in.AdditionalAllergens...)
	slices.Sort(oldExtra)
	slices.Sort(newExtra)
	if !slices.Equal(oldExtra, newExtra) {
		return false
	}
	if len(old.Ingredients) != len(in.Ingredients) {
		return false
	}
	for i, row := range old.Ingredients {
		w := in.Ingredients[i]
		if row.FoodID != w.FoodID || row.QuantityMilli != w.QuantityMilli || row.Unit != w.Unit {
			return false
		}
	}
	return true
}

// recipeChangedFields 返回发生变化的字段名列表（审计 details 用），
// 顺序固定；子项变化聚合记 ingredients/additional_allergens。
func recipeChangedFields(old RecipeDetail, status string, in RecipeWrite) []string {
	fields := []string{}
	add := func(name string, changed bool) {
		if changed {
			fields = append(fields, name)
		}
	}
	add("name", old.Name != in.Name)
	add("servings", old.Servings != in.Servings)
	add("duration_minutes", old.DurationMinutes != in.DurationMinutes)
	add("steps", !recipeStepsEqual(stepsJSONOf(old), in.Steps))
	add("source", old.Source != in.Source)
	add("allergen_complete", old.AllergenComplete != in.AllergenComplete)
	oldSrc, newSrc := old.AllergenSource, in.AllergenSource
	srcChanged := (oldSrc == nil) != (newSrc == nil) || (oldSrc != nil && newSrc != nil && *oldSrc != *newSrc)
	add("allergen_source", srcChanged)
	add("additional_allergens", !recipeAllergenSetsEqual(old.AdditionalAllergens, in.AdditionalAllergens))
	add("ingredients", !recipeIngredientsEqual(old.Ingredients, in.Ingredients))
	add("status", old.Status != status)
	return fields
}

func stepsJSONOf(d RecipeDetail) string {
	b, err := json.Marshal(d.Steps)
	if err != nil {
		return ""
	}
	return string(b)
}

func recipeAllergenSetsEqual(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func recipeIngredientsEqual(rows []RecipeIngredientRow, in []RecipeIngredientInput) bool {
	if len(rows) != len(in) {
		return false
	}
	for i, row := range rows {
		if row.FoodID != in[i].FoodID || row.QuantityMilli != in[i].QuantityMilli || row.Unit != in[i].Unit {
			return false
		}
	}
	return true
}

// recipeVisibilityFilter 返回写/读路径的可见性条件与参数：
// admin 路径仅 public（任意状态），普通路径仅本人 private。
func recipeVisibilityFilter(public bool, viewerID int64) (string, []any) {
	if public {
		return "visibility='public'", []any{}
	}
	return "visibility='private' AND owner_user_id=?", []any{viewerID}
}

// replaceRecipeChildren 删除旧子项并按请求重插；position 由数组顺序生成
// 1..n。任一步失败由调用方整体回滚。
func replaceRecipeChildren(ctx context.Context, c *gorm.DB, recipeID int64, in RecipeWrite) error {
	if err := c.WithContext(ctx).Table("recipe_ingredients").Where("recipe_id=?", recipeID).Delete(&struct{}{}).Error; err != nil {
		return err
	}
	if err := c.WithContext(ctx).Table("recipe_allergens").Where("recipe_id=?", recipeID).Delete(&struct{}{}).Error; err != nil {
		return err
	}
	for i, item := range in.Ingredients {
		if err := c.WithContext(ctx).Table("recipe_ingredients").Create(map[string]any{"recipe_id": recipeID, "position": i + 1, "food_id": item.FoodID, "quantity_milli": item.QuantityMilli, "unit": item.Unit}).Error; err != nil {
			return err
		}
	}
	for _, code := range in.AdditionalAllergens {
		if err := c.WithContext(ctx).Table("recipe_allergens").Create(map[string]any{"recipe_id": recipeID, "allergen_code": code}).Error; err != nil {
			return err
		}
	}
	return nil
}

// recipeFoodMetas 在事务内批量读取给定 food_id 集合的状态与完整性；
// 返回按输入顺序排列的元数据。任何 id 不存在都返回错误（调用方在读取
// 前已确保 id 来自请求与既有引用，缺行按 FOOD_UNAVAILABLE 处理）。
func recipeFoodMetas(ctx context.Context, c *gorm.DB, ids []int64) (map[int64]RecipeFoodMeta, error) {
	metas := map[int64]RecipeFoodMeta{}
	for i := 0; i < len(ids); i += 200 {
		end := i + 200
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		rows, err := c.WithContext(ctx).Table("foods").Select("id,status,allergen_complete").Where("id IN ?", batch).Rows()
		if err != nil {
			return nil, mapError(err)
		}
		for rows.Next() {
			var meta RecipeFoodMeta
			var id int64
			if err := rows.Scan(&id, &meta.Status, &meta.AllergenComplete); err != nil {
				rows.Close()
				return nil, err
			}
			metas[id] = meta
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return metas, nil
}

// recipeExistingFoodIDs 读取食谱当前引用的 food_id 集合（保留停用引用
// 例外的判定基准）。
func recipeExistingFoodIDs(ctx context.Context, c *gorm.DB, recipeID int64) (map[int64]bool, error) {
	rows, err := c.WithContext(ctx).Table("recipe_ingredients").Select("food_id").Where("recipe_id=?", recipeID).Rows()
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// checkRecipeFoods 执行事务内食材校验（方案 §4）：
//   - 请求涉及的全部 food_id 必须存在；
//   - 新增引用（不在旧集合中）必须 active，否则 400 FOOD_UNAVAILABLE；
//   - draft/archived 编辑可保留旧 disabled 引用并改用量，但不能新增。
func checkRecipeFoods(existing map[int64]bool, requested []int64, metas map[int64]RecipeFoodMeta) error {
	for _, id := range requested {
		meta, ok := metas[id]
		if !ok {
			return E(400, "FOOD_UNAVAILABLE", "食材不存在或已停用")
		}
		if !existing[id] && meta.Status != "active" {
			return E(400, "FOOD_UNAVAILABLE", "食材不存在或已停用")
		}
	}
	return nil
}

// recipeDetailFromTx 在给定查询入口上读取完整详情：父行、原料（JOIN foods
// 取名称与状态）、附加过敏原、有效过敏原合并与派生警示；全部查询处于
// 调用方既有的同一事务内，满足 REC-STORE-016 的一致性要求。
func recipeDetailFromTx(ctx context.Context, q *gorm.DB, row recipeRow) (RecipeDetail, error) {
	d := RecipeDetail{
		ID: row.ID, Visibility: row.Visibility, Status: row.Status,
		Name: row.Name, Servings: row.Servings, DurationMinutes: row.DurationMinutes,
		Source: row.Source, AllergenComplete: row.AllergenComplete,
		AllergenSource: row.AllergenSource, Version: row.Version,
		CreatedAt: time.Unix(row.CreatedAt, 0).UTC(), UpdatedAt: time.Unix(row.UpdatedAt, 0).UTC(),
	}
	if err := json.Unmarshal([]byte(row.StepsJSON), &d.Steps); err != nil {
		return RecipeDetail{}, err
	}
	if d.Steps == nil {
		d.Steps = []string{}
	}
	// 原料行：JOIN foods 保留名称与当前状态（含停用标记）。
	rows, err := q.WithContext(ctx).Table("recipe_ingredients ri").Select("ri.position,ri.food_id,f.name,f.status,ri.quantity_milli,ri.unit").Joins("JOIN foods f ON f.id=ri.food_id").Where("ri.recipe_id=?", row.ID).Order("ri.position").Rows()
	if err != nil {
		return RecipeDetail{}, mapError(err)
	}
	d.Ingredients = []RecipeIngredientRow{}
	for rows.Next() {
		var item RecipeIngredientRow
		if err := rows.Scan(&item.Position, &item.FoodID, &item.FoodName, &item.FoodStatus, &item.QuantityMilli, &item.Unit); err != nil {
			rows.Close()
			return RecipeDetail{}, err
		}
		d.Ingredients = append(d.Ingredients, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return RecipeDetail{}, err
	}
	rows.Close()
	// 附加过敏原：固定按编码排序输出。
	rows, err = q.WithContext(ctx).Table("recipe_allergens").Select("allergen_code").Where("recipe_id=?", row.ID).Order("allergen_code").Rows()
	if err != nil {
		return RecipeDetail{}, mapError(err)
	}
	d.AdditionalAllergens = []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return RecipeDetail{}, err
		}
		d.AdditionalAllergens = append(d.AdditionalAllergens, code)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return RecipeDetail{}, err
	}
	rows.Close()
	// 派生：单条 JOIN 聚合得出停用/完整性；无食材时 COALESCE 给出中性值。
	var disabled, minComplete int
	if err = q.WithContext(ctx).Table("recipe_ingredients ri").Select("COALESCE(MAX(f.status<>'active'),0),COALESCE(MIN(f.allergen_complete),1)").Joins("JOIN foods f ON f.id=ri.food_id").Where("ri.recipe_id=?", row.ID).Row().Scan(&disabled, &minComplete); err != nil {
		return RecipeDetail{}, mapError(err)
	}
	d.HasDisabledFoods = disabled == 1
	d.RestrictionMetadataComplete = row.AllergenComplete && minComplete == 1
	// 有效过敏原：food 当前标注 ∪ 附加标注，SQL 内去重排序（方案 §3.3）。
	rows, err = q.WithContext(ctx).Raw("SELECT DISTINCT a FROM (SELECT je.value AS a FROM recipe_ingredients ri JOIN foods f ON f.id=ri.food_id, json_each(f.allergens_json) je WHERE ri.recipe_id=? UNION SELECT allergen_code FROM recipe_allergens WHERE recipe_id=?) ORDER BY a", row.ID, row.ID).Rows()
	if err != nil {
		return RecipeDetail{}, mapError(err)
	}
	d.EffectiveAllergens = []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return RecipeDetail{}, err
		}
		d.EffectiveAllergens = append(d.EffectiveAllergens, code)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RecipeDetail{}, err
	}
	return d, nil
}

// loadRecipeForWrite 在写事务内按可见性条件读取父行（REC-STORE 校验基准）。
func loadRecipeForWrite(ctx context.Context, c *gorm.DB, id int64, public bool, viewerID int64) (recipeRow, error) {
	vis, args := recipeVisibilityFilter(public, viewerID)
	row, err := scanRecipeRow(c.WithContext(ctx).Table("recipes").Select(recipeColumns).Where("id=?", id).Where(vis, args...).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return recipeRow{}, recipeNotFound
	}
	return row, mapError(err)
}

// RecipeDetailReadScope 是普通详情读取的可见性范围：admin 路径读任意状态
// 公共食谱；普通路径读已发布公共或本人私有。
type RecipeDetailReadScope struct {
	Admin    bool
	ViewerID int64
}

// RecipeDetail 读取食谱详情（含全部派生字段）。可见性三元判定：admin 仅
// public 任意状态；普通用户 public+published 或 private+本人；其余（未发布
// 公共、他人私有、不存在）一律 404。读取在单个只读事务内完成。
func (s *Store) RecipeDetail(ctx context.Context, id int64, scope RecipeDetailReadScope) (out RecipeDetail, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return RecipeDetail{}, mapError(err)
	}
	defer tx.Rollback()
	query := tx.WithContext(ctx).Table("recipes").Select(recipeColumns).Where("id=?", id)
	if scope.Admin {
		query = query.Where("visibility='public'")
	} else {
		query = query.Where("(visibility='public' AND status='published') OR (visibility='private' AND owner_user_id=?)", scope.ViewerID)
	}
	row, err := scanRecipeRow(query.Row())
	if errors.Is(err, sql.ErrNoRows) {
		return RecipeDetail{}, recipeNotFound
	}
	if err != nil {
		return RecipeDetail{}, mapError(err)
	}
	out, err = recipeDetailFromTx(ctx, tx, row)
	if err != nil {
		return RecipeDetail{}, err
	}
	return out, mapError(tx.Commit().Error)
}

// RecipeListQuery 是列表查询条件；Visibility 恒为 public/private，OwnerID
// 仅 private scope 使用（本人），Admin 列表等价于 Visibility=public 且无
// 状态限制，由调用方组装。
type RecipeListQuery struct {
	Visibility string
	OwnerID    int64
	Status     string
	Q          string
	Page, Size int
}

// ListRecipes 返回食谱摘要分页（id DESC）；total 与当前页在同一只读事务
// 内读取，派生警示字段对当页 recipe_id 集合做一条 JOIN 聚合批量完成
// （REC-STORE-015：查询次数不随页内食谱数或原料数增长）。
func (s *Store) ListRecipes(ctx context.Context, q RecipeListQuery) (items []RecipeSummary, total int, err error) {
	tx := s.orm.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	err = tx.Error
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer tx.Rollback()
	where := "visibility=?"
	args := []any{q.Visibility}
	if q.Visibility == "private" {
		where += " AND owner_user_id=?"
		args = append(args, q.OwnerID)
	}
	if q.Status != "" {
		where += " AND status=?"
		args = append(args, q.Status)
	}
	if q.Q != "" {
		where += " AND instr(name,?)>0"
		args = append(args, q.Q)
	}
	if err = tx.WithContext(ctx).Table("recipes").Select("count(*)").Where(where, args...).Row().Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	limit, offset := pageBounds(q.Page, q.Size)
	rows, err := tx.WithContext(ctx).Table("recipes").Select("id,visibility,status,name,servings,duration_minutes,allergen_complete,version,updated_at").Where(where, args...).Order("id DESC").Limit(limit).Offset(offset).Rows()
	if err != nil {
		return nil, 0, mapError(err)
	}
	items = []RecipeSummary{}
	ids := []int64{}
	selfComplete := map[int64]bool{}
	for rows.Next() {
		var it RecipeSummary
		var complete bool
		var updated int64
		if e := rows.Scan(&it.ID, &it.Visibility, &it.Status, &it.Name, &it.Servings, &it.DurationMinutes, &complete, &it.Version, &updated); e != nil {
			rows.Close()
			return nil, 0, e
		}
		it.UpdatedAt = time.Unix(updated, 0).UTC()
		selfComplete[it.ID] = complete
		items = append(items, it)
		ids = append(ids, it.ID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	// 批量派生：当页全部 recipe_id 一条 JOIN 聚合（REC-STORE-015）；
	// 无食材的食谱不在聚合结果中，默认 has_disabled=false、foods 完整。
	if len(ids) > 0 {
		drows, e := tx.WithContext(ctx).Table("recipe_ingredients ri").Select("ri.recipe_id,MAX(f.status<>'active'),MIN(f.allergen_complete)").Joins("JOIN foods f ON f.id=ri.food_id").Where("ri.recipe_id IN ?", ids).Group("ri.recipe_id").Rows()
		if e != nil {
			return nil, 0, mapError(e)
		}
		derived := map[int64][2]int{}
		for drows.Next() {
			var id, disabled, minComplete int64
			if e := drows.Scan(&id, &disabled, &minComplete); e != nil {
				drows.Close()
				return nil, 0, e
			}
			derived[id] = [2]int{int(disabled), int(minComplete)}
		}
		if e = drows.Err(); e != nil {
			drows.Close()
			return nil, 0, e
		}
		drows.Close()
		for i := range items {
			complete := selfComplete[items[i].ID]
			if d, ok := derived[items[i].ID]; ok {
				items[i].HasDisabledFoods = d[0] == 1
				complete = complete && d[1] == 1
			}
			items[i].RestrictionMetadataComplete = complete
		}
	}
	return items, total, mapError(tx.Commit().Error)
}

// CreateRecipe 在写事务内复核会话后创建食谱：POST 恒为 draft，visibility
// 由路径决定（public=true 管理员公共、false 本人私有）；创建时全部引用
// 都是新增，必须 active。公共食谱写 recipe_created 审计，私有不写。
func (s *Store) CreateRecipe(ctx context.Context, actor Session, public bool, in RecipeWrite, now time.Time) (out RecipeDetail, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		a, e := checkSession(ctx, c, actor, now, public)
		if e != nil {
			return e
		}
		// 创建路径全部引用皆为新增：逐条要求 active。
		ids := make([]int64, len(in.Ingredients))
		for i, item := range in.Ingredients {
			ids[i] = item.FoodID
		}
		metas, e := recipeFoodMetas(ctx, c, ids)
		if e != nil {
			return e
		}
		if e = checkRecipeFoods(map[int64]bool{}, ids, metas); e != nil {
			return e
		}
		steps, e := recipeWriteSteps(in.Steps)
		if e != nil {
			return e
		}
		var owner sql.NullInt64
		visibility := "private"
		if public {
			visibility = "public"
		} else {
			owner = sql.NullInt64{Int64: actor.User.ID, Valid: true}
		}
		record := recipeRow{
			Visibility:       visibility,
			OwnerID:          owner,
			Status:           "draft",
			Name:             in.Name,
			Servings:         in.Servings,
			DurationMinutes:  in.DurationMinutes,
			StepsJSON:        steps,
			Source:           in.Source,
			AllergenComplete: in.AllergenComplete,
			AllergenSource:   in.AllergenSource,
			Version:          1,
			CreatedAt:        now.Unix(),
			UpdatedAt:        now.Unix(),
		}
		e = c.WithContext(ctx).Table("recipes").Create(&record).Error
		if e != nil {
			return e
		}
		id := record.ID
		if e = replaceRecipeChildren(ctx, c, id, in); e != nil {
			return e
		}
		if public {
			details := map[string]any{
				"fields":         append([]string{}, "name", "servings", "duration_minutes", "steps", "source", "allergen_complete", "allergen_source", "additional_allergens", "ingredients"),
				"status_before":  nil,
				"status_after":   "draft",
				"version_before": nil,
				"version_after":  int64(1),
			}
			if e = insertResourceAudit(ctx, c, a.ID, "recipe", id, "recipe_created", details, now); e != nil {
				return e
			}
		}
		row, e := scanRecipeRow(c.WithContext(ctx).Table("recipes").Select(recipeColumns).Where("id=?", id).Row())
		if e != nil {
			return e
		}
		out, e = recipeDetailFromTx(ctx, c, row)
		return e
	})
	return
}

// UpdateRecipe 在写事务内完成（方案 §6）：复核会话 → 按路径可见性读父行
// → 版本与状态机校验 → 事务内重读全部请求食材 → 新增引用必须 active、
// 保留 disabled 引用仅限既有 → 发布条件校验 → 无变化直接返回原详情 →
// 条件更新父行 version+1 → 子项整体替换 → 公共写审计；任何失败整体回滚。
func (s *Store) UpdateRecipe(ctx context.Context, actor Session, public bool, id, version int64, status string, in RecipeWrite, now time.Time) (out RecipeDetail, err error) {
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		a, e := checkSession(ctx, c, actor, now, public)
		if e != nil {
			return e
		}
		row, e := loadRecipeForWrite(ctx, c, id, public, actor.User.ID)
		if e != nil {
			return e
		}
		if row.Version != version {
			return E(409, "VERSION_CONFLICT", "食谱已被他人修改，请刷新后重试")
		}
		if e = ValidateRecipeTransition(row.Status, status); e != nil {
			return e
		}
		old, e := recipeDetailFromTx(ctx, c, row)
		if e != nil {
			return e
		}
		ids := make([]int64, len(in.Ingredients))
		for i, item := range in.Ingredients {
			ids[i] = item.FoodID
		}
		existing, e := recipeExistingFoodIDs(ctx, c, id)
		if e != nil {
			return e
		}
		metas, e := recipeFoodMetas(ctx, c, ids)
		if e != nil {
			return e
		}
		if e = checkRecipeFoods(existing, ids, metas); e != nil {
			return e
		}
		// 发布条件：目标为 published，或保持 published 的任何编辑。
		if status == "published" {
			foodList := make([]RecipeFoodMeta, len(in.Ingredients))
			for i, item := range in.Ingredients {
				foodList[i] = metas[item.FoodID]
			}
			problems := RecipePublishProblems(in.AllergenComplete, in.AllergenSource, len(in.Steps), len(in.Ingredients), foodList)
			if len(problems) > 0 {
				return &AppError{Status: 409, Code: "RECIPE_NOT_PUBLISHABLE", Message: strings.Join(problems, "；")}
			}
		}
		if recipeWriteEqual(old, status, in) {
			out = old
			return nil
		}
		steps, e := recipeWriteSteps(in.Steps)
		if e != nil {
			return e
		}
		newVersion := row.Version + 1
		r := c.WithContext(ctx).Table("recipes").Where("id=? AND version=?", id, row.Version).Updates(map[string]any{
			"name":              in.Name,
			"servings":          in.Servings,
			"duration_minutes":  in.DurationMinutes,
			"steps_json":        steps,
			"source":            in.Source,
			"allergen_complete": boolInt(in.AllergenComplete),
			"allergen_source":   in.AllergenSource,
			"status":            status,
			"version":           newVersion,
			"updated_at":        now.Unix(),
		})
		e = r.Error
		if e != nil {
			return e
		}
		if n := r.RowsAffected; n != 1 {
			return E(409, "VERSION_CONFLICT", "食谱已被他人修改，请刷新后重试")
		}
		if e = replaceRecipeChildren(ctx, c, id, in); e != nil {
			return e
		}
		if row.Visibility == "public" {
			details := map[string]any{"fields": recipeChangedFields(old, status, in), "status_before": row.Status, "status_after": status, "version_before": row.Version, "version_after": newVersion}
			if e = insertResourceAudit(ctx, c, a.ID, "recipe", id, recipeAuditAction(row.Status, status), details, now); e != nil {
				return e
			}
		}
		// 修复 Bug：旧 row 是更新前读出的数据，直接构造响应会返回旧值；
		// 与 CreateRecipe 一致，UPDATE 与子项替换成功后在同一事务内重读父行。
		fresh, e := scanRecipeRow(c.WithContext(ctx).Table("recipes").Select(recipeColumns).Where("id=?", id).Row())
		if e != nil {
			return e
		}
		out, e = recipeDetailFromTx(ctx, c, fresh)
		return e
	})
	return
}
