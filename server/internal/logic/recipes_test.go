package logic

import (
	"strings"
	"testing"

	"freshplate/server/internal/store"
)

// 食谱模块 logic 层纯函数测试（REC-LOGIC-001~019）；发布条件与状态机
// 纯函数实现于 store 包（事务内复用），此处直接对导出函数表驱动覆盖。

// validRecipeForm 返回一份合法输入，供各用例局部覆盖。
func validRecipeForm() RecipeForm {
	return RecipeForm{
		Name: "番茄炒蛋", Servings: 2, DurationMinutes: 15,
		Steps: []string{"洗净番茄并切块。", "鸡蛋炒熟后加入番茄继续翻炒。"},
		Source: "个人记录",
		Ingredients: []RecipeIngredientForm{
			{FoodID: 11, Quantity: "250", Unit: "g"},
			{FoodID: 1, Quantity: "2", Unit: "piece"},
		},
		AdditionalAllergens: []string{}, AllergenComplete: false, AllergenSource: nil,
	}
}

func strptr(s string) *string { return &s }

// REC-LOGIC-001/002：空草稿可创建；必填字段缺失/越界拒绝（键必须存在）。
func TestRecipeLOGIC001_002_EmptyDraftAndRequiredFields(t *testing.T) {
	empty := validRecipeForm()
	empty.Steps = []string{}
	empty.Ingredients = []RecipeIngredientForm{}
	out, err := ValidateRecipeForm(empty)
	if err != nil {
		t.Fatalf("空草稿应通过: %v", err)
	}
	if out.Name != "番茄炒蛋" || len(out.Steps) != 0 || len(out.Ingredients) != 0 {
		t.Fatalf("空草稿被改写: %+v", out)
	}
	cases := []struct {
		name string
		mut  func(*RecipeForm)
	}{
		{"name空", func(f *RecipeForm) { f.Name = "" }},
		{"name全空白", func(f *RecipeForm) { f.Name = "   " }},
		{"name101码点", func(f *RecipeForm) { f.Name = strings.Repeat("菜", 101) }},
		{"servings0", func(f *RecipeForm) { f.Servings = 0 }},
		{"servings101", func(f *RecipeForm) { f.Servings = 101 }},
		{"duration0", func(f *RecipeForm) { f.DurationMinutes = 0 }},
		{"duration1441", func(f *RecipeForm) { f.DurationMinutes = 1441 }},
		{"steps缺失", func(f *RecipeForm) { f.Steps = nil }},
		{"ingredients缺失", func(f *RecipeForm) { f.Ingredients = nil }},
		{"allergens缺失", func(f *RecipeForm) { f.AdditionalAllergens = nil }},
	}
	for _, tc := range cases {
		f := validRecipeForm()
		tc.mut(&f)
		if _, err := ValidateRecipeForm(f); err == nil {
			t.Fatalf("%s 应拒绝", tc.name)
		}
	}
}

// REC-LOGIC-003：name 码点边界与 TrimSpace。
func TestRecipeLOGIC003_NameRuneBounds(t *testing.T) {
	f := validRecipeForm()
	f.Name = "  " + strings.Repeat("菜", 98) + "  "
	out, err := ValidateRecipeForm(f)
	if err != nil || out.Name != strings.Repeat("菜", 98) {
		t.Fatalf("TrimSpace 后 98 码点应通过: %v %q", err, out.Name)
	}
	f.Name = strings.Repeat("菜", 100)
	if _, err := ValidateRecipeForm(f); err != nil {
		t.Fatalf("100 码点应通过: %v", err)
	}
	f.Name = strings.Repeat("菜", 101)
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("101 码点应拒绝")
	}
}

// REC-LOGIC-004/005/006：发布完整性条件（store 导出纯函数）。
func TestRecipeLOGIC004_005_006_PublishProblems(t *testing.T) {
	complete := store.RecipeFoodMeta{Status: "active", AllergenComplete: true}
	disabled := store.RecipeFoodMeta{Status: "disabled", AllergenComplete: true}
	incomplete := store.RecipeFoodMeta{Status: "active", AllergenComplete: false}
	src := "依据"
	if p := store.RecipePublishProblems(true, &src, 1, 1, []store.RecipeFoodMeta{complete}); len(p) != 0 {
		t.Fatalf("全部满足应可发布: %v", p)
	}
	cases := []struct {
		name       string
		complete   bool
		source     *string
		steps      int
		ingredient int
		foods      []store.RecipeFoodMeta
	}{
		{"无步骤", true, &src, 0, 1, []store.RecipeFoodMeta{complete}},
		{"无食材", true, &src, 1, 0, []store.RecipeFoodMeta{}},
		{"停用食材", true, &src, 1, 1, []store.RecipeFoodMeta{disabled}},
		{"食材不完整", true, &src, 1, 1, []store.RecipeFoodMeta{incomplete}},
		{"自身未核对", false, &src, 1, 1, []store.RecipeFoodMeta{complete}},
		{"缺依据", true, nil, 1, 1, []store.RecipeFoodMeta{complete}},
		{"依据纯空白", true, strptr("   "), 1, 1, []store.RecipeFoodMeta{complete}},
	}
	for _, tc := range cases {
		p := store.RecipePublishProblems(tc.complete, tc.source, tc.steps, tc.ingredient, tc.foods)
		if len(p) == 0 {
			t.Fatalf("%s 应不满足发布条件", tc.name)
		}
	}
}

// REC-LOGIC-007/008：状态机全转换矩阵。
func TestRecipeLOGIC007_008_TransitionMatrix(t *testing.T) {
	allowed := [][2]string{
		{"draft", "published"}, {"published", "draft"},
		{"draft", "archived"}, {"published", "archived"}, {"archived", "draft"},
		{"draft", "draft"}, {"published", "published"}, {"archived", "archived"},
	}
	for _, tr := range allowed {
		if err := store.ValidateRecipeTransition(tr[0], tr[1]); err != nil {
			t.Fatalf("%s→%s 应允许: %v", tr[0], tr[1], err)
		}
	}
	if err := store.ValidateRecipeTransition("archived", "published"); err == nil {
		t.Fatal("archived→published 应拒绝")
	}
}

// REC-LOGIC-009：数量字符串解析边界（沿用库存 helper）。
func TestRecipeLOGIC009_QuantityBounds(t *testing.T) {
	accept := map[string]int64{"250": 250000, "0.001": 1, "1000000": 1000000000, "2": 2000, "0.5": 500}
	for raw, want := range accept {
		f := validRecipeForm()
		f.Ingredients = []RecipeIngredientForm{{FoodID: 1, Quantity: raw, Unit: "g"}}
		out, err := ValidateRecipeForm(f)
		if err != nil || out.Ingredients[0].QuantityMilli != want {
			t.Fatalf("%q 解析错误: %v %d", raw, err, out.Ingredients[0].QuantityMilli)
		}
	}
	for _, raw := range []string{"1e3", "-1", "0", "0.0001", "1000000.001", "", "abc", "5.", ".5"} {
		f := validRecipeForm()
		f.Ingredients = []RecipeIngredientForm{{FoodID: 1, Quantity: raw, Unit: "g"}}
		if _, err := ValidateRecipeForm(f); err == nil {
			t.Fatalf("%q 应拒绝", raw)
		}
	}
	// 数字类型无法进入字符串字段，等价的非法形式（前导零）同样拒绝。
	f := validRecipeForm()
	f.Ingredients = []RecipeIngredientForm{{FoodID: 1, Quantity: "007", Unit: "g"}}
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("前导零应拒绝")
	}
}

// REC-LOGIC-010：steps 边界——条目数、单步码点、空白步骤、换行。
func TestRecipeLOGIC010_StepBounds(t *testing.T) {
	mk := func(steps []string) RecipeForm {
		f := validRecipeForm()
		f.Steps = steps
		return f
	}
	if _, err := ValidateRecipeForm(mk([]string{})); err != nil {
		t.Fatalf("0 步应通过: %v", err)
	}
	steps30 := make([]string, 30)
	for i := range steps30 {
		steps30[i] = "第"
		steps30[i] += "一步骤内容"
	}
	if _, err := ValidateRecipeForm(mk(steps30)); err != nil {
		t.Fatalf("30 步应通过: %v", err)
	}
	if _, err := ValidateRecipeForm(mk(append(append([]string{}, steps30...), "第31步"))); err == nil {
		t.Fatal("31 步应拒绝")
	}
	if _, err := ValidateRecipeForm(mk([]string{strings.Repeat("步", 1000)})); err != nil {
		t.Fatalf("1000 码点应通过: %v", err)
	}
	if _, err := ValidateRecipeForm(mk([]string{strings.Repeat("步", 1001)})); err == nil {
		t.Fatal("1001 码点应拒绝")
	}
	for _, bad := range [][]string{{""}, {"   "}, {"", "  "}} {
		if _, err := ValidateRecipeForm(mk(bad)); err == nil {
			t.Fatalf("空白步骤 %v 应拒绝", bad)
		}
	}
	if _, err := ValidateRecipeForm(mk([]string{"第一行\n第二行"})); err != nil {
		t.Fatalf("换行应允许: %v", err)
	}
}

// REC-LOGIC-011：ingredients 条目边界与重复 food_id。
func TestRecipeLOGIC011_IngredientBounds(t *testing.T) {
	mk := func(n int) RecipeForm {
		f := validRecipeForm()
		f.Ingredients = []RecipeIngredientForm{}
		for i := 0; i < n; i++ {
			f.Ingredients = append(f.Ingredients, RecipeIngredientForm{FoodID: int64(i + 1), Quantity: "1", Unit: "g"})
		}
		return f
	}
	if _, err := ValidateRecipeForm(mk(0)); err != nil {
		t.Fatalf("0 条应通过: %v", err)
	}
	if _, err := ValidateRecipeForm(mk(100)); err != nil {
		t.Fatalf("100 条应通过: %v", err)
	}
	if _, err := ValidateRecipeForm(mk(101)); err == nil {
		t.Fatal("101 条应拒绝")
	}
	f := validRecipeForm()
	f.Ingredients = append(f.Ingredients, RecipeIngredientForm{FoodID: 11, Quantity: "1", Unit: "g"})
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("重复 food_id 应拒绝")
	}
}

// REC-LOGIC-012：source 与 allergen_source 边界。
func TestRecipeLOGIC012_SourceBounds(t *testing.T) {
	f := validRecipeForm()
	f.Source = strings.Repeat("源", 1000)
	if _, err := ValidateRecipeForm(f); err != nil {
		t.Fatalf("source 1000 码点应通过: %v", err)
	}
	f.Source = strings.Repeat("源", 1001)
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("source 1001 码点应拒绝")
	}
	f = validRecipeForm()
	f.AllergenComplete = true
	f.AllergenSource = strptr(strings.Repeat("据", 500))
	if _, err := ValidateRecipeForm(f); err != nil {
		t.Fatalf("allergen_source 500 码点应通过: %v", err)
	}
	f.AllergenSource = strptr(strings.Repeat("据", 501))
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("allergen_source 501 码点应拒绝")
	}
	f.AllergenSource = nil
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("complete=true 且 source=null 应拒绝")
	}
	f.AllergenComplete = false
	if _, err := ValidateRecipeForm(f); err != nil {
		t.Fatalf("complete=false 时 source 可为 null: %v", err)
	}
}

// REC-LOGIC-013（基础输入部分）/REC-LOGIC-019：未知单位由应用层拒绝；
// 非法 food_id 拒绝。
func TestRecipeLOGIC013_019_UnitAndFoodID(t *testing.T) {
	for _, unit := range []string{"kg", "升", ""} {
		f := validRecipeForm()
		f.Ingredients = []RecipeIngredientForm{{FoodID: 1, Quantity: "250", Unit: unit}}
		if _, err := ValidateRecipeForm(f); err == nil {
			t.Fatalf("单位 %q 应拒绝", unit)
		}
	}
	for _, bad := range []int64{0, -1} {
		f := validRecipeForm()
		f.Ingredients = []RecipeIngredientForm{{FoodID: bad, Quantity: "250", Unit: "g"}}
		if _, err := ValidateRecipeForm(f); err == nil {
			t.Fatalf("food_id %d 应拒绝", bad)
		}
	}
}

// REC-LOGIC-014：附加过敏原编码校验、去重与上限。
func TestRecipeLOGIC014_AllergenCodes(t *testing.T) {
	f := validRecipeForm()
	f.AdditionalAllergens = []string{"peanut", "peanut", "soy"}
	out, err := ValidateRecipeForm(f)
	if err != nil || len(out.AdditionalAllergens) != 2 {
		t.Fatalf("去重后应 2 个: %v %v", err, out.AdditionalAllergens)
	}
	f.AdditionalAllergens = []string{"unsupported"}
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("非法编码应拒绝")
	}
	// 支持集共 10 种：全部唯一编码（去重后恰 10 个）应通过；键缺失
	//（nil）应拒绝。
	f.AdditionalAllergens = []string{"milk", "egg", "fish", "crustacean", "mollusc", "peanut", "soy", "wheat", "tree_nut", "sesame"}
	if _, err := ValidateRecipeForm(f); err != nil {
		t.Fatalf("10 个唯一编码应通过: %v", err)
	}
	f.AdditionalAllergens = nil
	if _, err := ValidateRecipeForm(f); err == nil {
		t.Fatal("additional_allergens 缺失应拒绝")
	}
}

// REC-LOGIC-015：控制字符与无效 UTF-8；步骤内换行允许。
func TestRecipeLOGIC015_ControlCharsAndUTF8(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*RecipeForm)
	}{
		{"name含NUL", func(f *RecipeForm) { f.Name = "坏\x00名" }},
		{"name含ESC", func(f *RecipeForm) { f.Name = "坏\x1b名" }},
		{"name含换行", func(f *RecipeForm) { f.Name = "坏\n名" }},
		{"name无效UTF8", func(f *RecipeForm) { f.Name = "坏" + string([]byte{255}) }},
		{"source含控制", func(f *RecipeForm) { f.Source = "源\x07" }},
		{"step含控制", func(f *RecipeForm) { f.Steps = []string{"步\t骤"} }},
		{"step无效UTF8", func(f *RecipeForm) { f.Steps = []string{"步" + string([]byte{254})}}},
	}
	for _, tc := range cases {
		f := validRecipeForm()
		tc.mut(&f)
		if _, err := ValidateRecipeForm(f); err == nil {
			t.Fatalf("%s 应拒绝", tc.name)
		}
	}
}

// REC-LOGIC-017：servings/duration 合法端点。
func TestRecipeLOGIC017_ServingsDurationEndpoints(t *testing.T) {
	for _, tc := range [][2]int{{1, 1}, {100, 1440}} {
		f := validRecipeForm()
		f.Servings, f.DurationMinutes = tc[0], tc[1]
		if _, err := ValidateRecipeForm(f); err != nil {
			t.Fatalf("端点 %v 应通过: %v", tc, err)
		}
	}
}

// REC-LOGIC-018：列表查询校验——scope/status/q/分页。
func TestRecipeLOGIC018_QueryValidation(t *testing.T) {
	if _, _, _, err := ValidateRecipeQuery("", "番茄", "", 1, 20); err != nil {
		t.Fatalf("默认 scope 应通过: %v", err)
	}
	if _, _, _, err := ValidateRecipeQuery("mine", "番茄", "draft", 1, 20); err != nil {
		t.Fatalf("mine+draft 应通过: %v", err)
	}
	for _, bad := range []struct {
		scope, status string
		page, size    int
	}{
		{"others", "", 1, 20}, {"public", "draft", 1, 20}, {"mine", "bogus", 1, 20},
		{"public", "", 0, 20}, {"public", "", 1, 101}, {"mine", "", 1, 0},
		{"public", "", -1, 20},
	} {
		if _, _, _, err := ValidateRecipeQuery(bad.scope, "", bad.status, bad.page, bad.size); err == nil {
			t.Fatalf("%+v 应拒绝", bad)
		}
	}
	if _, _, _, err := ValidateRecipeQuery("public", strings.Repeat("茄", 101), "", 1, 20); err == nil {
		t.Fatal("q 超 100 码点应拒绝")
	}
	if _, err := ValidateRecipeAdminQuery("", "bogus", 1, 20); err == nil {
		t.Fatal("admin status 非法应拒绝")
	}
	if _, err := ValidateRecipeAdminQuery("番茄", "archived", 1, 100); err != nil {
		t.Fatalf("admin 合法查询应通过: %v", err)
	}
}
