package logic

import (
	"math"
	"strings"
	"testing"

	"freshplate/server/internal/store"
)

// validFood 返回一份完整合法输入（营养/换算/过敏原组齐全），供各用例局部覆盖。
func validFood() store.FoodWrite {
	energy, protein, fat, carb := 121.0, 12.5, 3.3, 68.6
	src := "中国食物成分表 第6版"
	ref := "standard-table-01"
	density, piece := 1.03, 50.0
	conv := "包装营养标注"
	asrc := "厂商标签"
	return store.FoodWrite{
		Name: "燕麦片", Status: "active",
		EnergyKcal100g: &energy, ProteinG100g: &protein, FatG100g: &fat, CarbohydrateG100g: &carb,
		NutritionSource: &src, NutritionReference: &ref,
		DensityGml: &density, PieceWeightG: &piece, ConversionSource: &conv,
		Allergens: []string{"egg"}, AllergenComplete: true, AllergenSource: &asrc,
	}
}

// blankNutrition 把营养组六字段全部置为显式未知（null）。
func blankNutrition(in store.FoodWrite) store.FoodWrite {
	in.EnergyKcal100g, in.ProteinG100g, in.FatG100g, in.CarbohydrateG100g = nil, nil, nil, nil
	in.NutritionSource, in.NutritionReference = nil, nil
	return in
}

func fptr(v float64) *float64 { return &v }
func sptr(s string) *string   { return &s }
func nan() float64            { return math.NaN() }
func inf(sign int) float64    { return math.Inf(sign) }

// FOOD-LOGIC-001/002：营养组全空可保存（保持未知，不补零）；完整组可保存。
func TestFoodLOGIC001_002_NutritionGroupEmptyAndComplete(t *testing.T) {
	blank := blankNutrition(validFood())
	blank.DensityGml, blank.PieceWeightG, blank.ConversionSource = nil, nil, nil
	blank.Allergens, blank.AllergenComplete, blank.AllergenSource = []string{}, false, nil
	out, err := ValidateFoodInput(blank)
	if err != nil {
		t.Fatalf("全空营养组应通过: %v", err)
	}
	if out.EnergyKcal100g != nil || out.ProteinG100g != nil || out.FatG100g != nil || out.CarbohydrateG100g != nil || out.NutritionSource != nil || out.NutritionReference != nil {
		t.Fatal("未知营养信息被改写")
	}
	full := validFood()
	if _, err := ValidateFoodInput(full); err != nil {
		t.Fatalf("完整营养组应通过: %v", err)
	}
}

// FOOD-LOGIC-003：营养组六字段逐一置 null、其余保持完整，共 6 组均拒绝。
func TestFoodLOGIC003_PartialNutritionGroupRejected(t *testing.T) {
	for i, mutate := range []func(*store.FoodWrite){
		func(in *store.FoodWrite) { in.EnergyKcal100g = nil },
		func(in *store.FoodWrite) { in.ProteinG100g = nil },
		func(in *store.FoodWrite) { in.FatG100g = nil },
		func(in *store.FoodWrite) { in.CarbohydrateG100g = nil },
		func(in *store.FoodWrite) { in.NutritionSource = nil },
		func(in *store.FoodWrite) { in.NutritionReference = nil },
	} {
		in := validFood()
		mutate(&in)
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("组合 %d: 期望 ErrInvalidArgument，得到 %v", i, err)
		}
	}
}

// FOOD-LOGIC-004：零与未知语义区分——0 有来源证明被保留；全 null 保持未知。
func TestFoodLOGIC004_ZeroVersusUnknown(t *testing.T) {
	zero := validFood()
	zero.EnergyKcal100g = fptr(0)
	zero.ProteinG100g = fptr(0)
	out, err := ValidateFoodInput(zero)
	if err != nil {
		t.Fatalf("零值组应通过: %v", err)
	}
	if out.EnergyKcal100g == nil || *out.EnergyKcal100g != 0 || out.ProteinG100g == nil || *out.ProteinG100g != 0 {
		t.Fatal("零值被改写为未知")
	}
	unknown := blankNutrition(validFood())
	out, err = ValidateFoodInput(unknown)
	if err != nil {
		t.Fatalf("未知组应通过: %v", err)
	}
	if out.EnergyKcal100g != nil {
		t.Fatal("未知能量被补零")
	}
}

// FOOD-LOGIC-005/006：营养来源为空字符串或仅含 Unicode 空白均拒绝。
func TestFoodLOGIC005_006_BlankNutritionSourceRejected(t *testing.T) {
	for _, raw := range []string{"", "　", "\t", " \n", " \t "} {
		in := validFood()
		in.NutritionSource = sptr(raw)
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("nutrition_source=%q 应拒绝，得到 %v", raw, err)
		}
		in = validFood()
		in.NutritionReference = sptr(raw)
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("nutrition_reference=%q 应拒绝，得到 %v", raw, err)
		}
	}
}

// FOOD-LOGIC-007：能量边界 0～1000。
func TestFoodLOGIC007_EnergyBounds(t *testing.T) {
	for _, v := range []float64{0, 1000} {
		in := validFood()
		in.EnergyKcal100g = fptr(v)
		if _, err := ValidateFoodInput(in); err != nil {
			t.Fatalf("energy=%v 应通过: %v", v, err)
		}
	}
	for _, v := range []float64{-0.1, 1000.01, 1001} {
		in := validFood()
		in.EnergyKcal100g = fptr(v)
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("energy=%v 应拒绝，得到 %v", v, err)
		}
	}
}

// FOOD-LOGIC-008：三项营养素边界 0～100；不校验相加或与能量的公式关系。
func TestFoodLOGIC008_MacroBounds(t *testing.T) {
	for _, name := range []string{"protein", "fat", "carb"} {
		for _, v := range []float64{0, 100} {
			in := validFood()
			switch name {
			case "protein":
				in.ProteinG100g = fptr(v)
			case "fat":
				in.FatG100g = fptr(v)
			default:
				in.CarbohydrateG100g = fptr(v)
			}
			if _, err := ValidateFoodInput(in); err != nil {
				t.Fatalf("%s=%v 应通过: %v", name, v, err)
			}
		}
		for _, v := range []float64{-1, 100.01} {
			in := validFood()
			switch name {
			case "protein":
				in.ProteinG100g = fptr(v)
			case "fat":
				in.FatG100g = fptr(v)
			default:
				in.CarbohydrateG100g = fptr(v)
			}
			if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
				t.Fatalf("%s=%v 应拒绝，得到 %v", name, v, err)
			}
		}
	}
	// 三项相加大于能量也不拒绝：不做公式一致性校验。
	in := validFood()
	in.EnergyKcal100g = fptr(10)
	in.ProteinG100g, in.FatG100g, in.CarbohydrateG100g = fptr(100), fptr(100), fptr(100)
	if _, err := ValidateFoodInput(in); err != nil {
		t.Fatalf("不校验营养素相加公式: %v", err)
	}
}

// FOOD-LOGIC-009：非有限数拒绝（营养值与换算系数）。
func TestFoodLOGIC009_NonFiniteRejected(t *testing.T) {
	nonFinite := []float64{nan(), inf(1), inf(-1)}
	for _, v := range nonFinite {
		for _, mutate := range []func(*store.FoodWrite, float64){
			func(in *store.FoodWrite, x float64) { in.EnergyKcal100g = fptr(x) },
			func(in *store.FoodWrite, x float64) { in.ProteinG100g = fptr(x) },
			func(in *store.FoodWrite, x float64) { in.FatG100g = fptr(x) },
			func(in *store.FoodWrite, x float64) { in.CarbohydrateG100g = fptr(x) },
			func(in *store.FoodWrite, x float64) { in.DensityGml = fptr(x) },
			func(in *store.FoodWrite, x float64) { in.PieceWeightG = fptr(x) },
		} {
			in := validFood()
			mutate(&in, v)
			if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
				t.Fatalf("非有限数 %v 应拒绝，得到 %v", v, err)
			}
		}
	}
}

// FOOD-LOGIC-010：名称规范——裁剪、码点上限、控制字符。
func TestFoodLOGIC010_NameRules(t *testing.T) {
	// 合法：裁剪后保留；恰好 100 码点（汉字+emoji 组合按码点计）。
	out, err := ValidateFoodInput(validFoodWithName(t, "  燕麦 "))
	if err != nil || out.Name != "燕麦" {
		t.Fatalf("名称应裁剪为「燕麦」: %q %v", out.Name, err)
	}
	name100 := strings.Repeat("麦", 75) + strings.Repeat("🌾", 25)
	if got := len([]rune(name100)); got != 100 {
		t.Fatalf("夹具码点数错误: %d", got)
	}
	if _, err := ValidateFoodInput(validFoodWithName(t, name100)); err != nil {
		t.Fatalf("恰好 100 码点应通过: %v", err)
	}
	// 非法：空、纯空白、101 码点、内嵌控制字符。
	for _, name := range []string{"", "   ", strings.Repeat("麦", 76) + strings.Repeat("🌾", 25), "燕\n麦", "燕\x00麦", "燕\t麦"} {
		if _, err := ValidateFoodInput(validFoodWithName(t, name)); err != ErrInvalidArgument {
			t.Fatalf("名称 %q 应拒绝，得到 %v", name, err)
		}
	}
}

func validFoodWithName(t *testing.T, name string) store.FoodWrite {
	t.Helper()
	in := validFood()
	in.Name = name
	return in
}

// FOOD-LOGIC-011：非法过敏原编码拒绝，不复用 preferences 的宽松解析。
func TestFoodLOGIC011_InvalidAllergenCodeRejected(t *testing.T) {
	for _, code := range []string{"gluten-unknown", "milk ", "Milk", "sesame1"} {
		in := validFood()
		in.Allergens = []string{code}
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("编码 %q 应拒绝，得到 %v", code, err)
		}
	}
	in := validFood()
	in.Allergens = []string{"milk", "sesame"}
	if _, err := ValidateFoodInput(in); err != nil {
		t.Fatalf("合法编码应通过: %v", err)
	}
}

// FOOD-LOGIC-012：过敏原去重与上限——去重后 ≤10 通过，超过上限拒绝。
// 支持编码恰为 10 种，故「11 个不同且合法编码」无法构造；上限语义等价于
// 去重后数量 >10 或出现支持集合之外的编码，两者分别覆盖。
func TestFoodLOGIC012_AllergenDedupAndLimit(t *testing.T) {
	all := []string{"milk", "egg", "fish", "crustacean", "mollusc", "peanut", "soy", "wheat", "tree_nut", "sesame"}
	// 恰好 10 个不同编码。
	in := validFood()
	in.Allergens = all
	out, err := ValidateFoodInput(in)
	if err != nil {
		t.Fatalf("恰好 10 个编码应通过: %v", err)
	}
	if len(out.Allergens) != 10 {
		t.Fatalf("输出编码数 %d", len(out.Allergens))
	}
	// 含重复编码、去重后 ≤10：保序去重通过。
	in.Allergens = append(append([]string{}, all...), "milk", "egg", "fish")
	out, err = ValidateFoodInput(in)
	if err != nil {
		t.Fatalf("含重复编码（去重后 10）应通过: %v", err)
	}
	if len(out.Allergens) != 10 {
		t.Fatalf("去重后应为 10，得到 %d", len(out.Allergens))
	}
	for i, v := range out.Allergens {
		if v != all[i] {
			t.Fatalf("去重保序失败: %v", out.Allergens)
		}
	}
	// 11 个不同输入项中第 11 个超出支持集合：超上限等价场景，拒绝。
	in.Allergens = append(append([]string{}, all...), "gluten-unknown")
	if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
		t.Fatalf("第 11 项为非法编码应拒绝，得到 %v", err)
	}
}

// FOOD-LOGIC-013：过敏原三态语义——[]+false=未知、[]+true=已核对无、非空+false=部分已知。
func TestFoodLOGIC013_AllergenTriState(t *testing.T) {
	cases := []struct {
		name     string
		allergen func(*store.FoodWrite)
	}{
		{"未知", func(in *store.FoodWrite) { in.Allergens, in.AllergenComplete, in.AllergenSource = []string{}, false, nil }},
		{"已核对无", func(in *store.FoodWrite) { in.Allergens, in.AllergenComplete, in.AllergenSource = []string{}, true, sptr("检测报告") }},
		{"部分已知", func(in *store.FoodWrite) { in.Allergens, in.AllergenComplete, in.AllergenSource = []string{"milk"}, false, nil }},
	}
	for _, tc := range cases {
		in := blankNutrition(validFood())
		in.DensityGml, in.PieceWeightG, in.ConversionSource = nil, nil, nil
		tc.allergen(&in)
		out, err := ValidateFoodInput(in)
		if err != nil {
			t.Fatalf("%s 组合应通过: %v", tc.name, err)
		}
		switch tc.name {
		case "未知":
			if len(out.Allergens) != 0 || out.AllergenComplete {
				t.Fatal("未知语义被改写")
			}
		case "已核对无":
			if len(out.Allergens) != 0 || !out.AllergenComplete || out.AllergenSource == nil {
				t.Fatal("已核对无语义被改写")
			}
		case "部分已知":
			if len(out.Allergens) != 1 || out.AllergenComplete {
				t.Fatal("部分已知语义被改写")
			}
		}
	}
}

// FOOD-LOGIC-014：撤销完整性时保留已知编码。
func TestFoodLOGIC014_RevokeCompletenessKeepsCodes(t *testing.T) {
	in := validFood()
	in.Allergens, in.AllergenComplete, in.AllergenSource = []string{"milk"}, false, nil
	out, err := ValidateFoodInput(in)
	if err != nil {
		t.Fatalf("撤销完整性应通过: %v", err)
	}
	if len(out.Allergens) != 1 || out.Allergens[0] != "milk" || out.AllergenComplete {
		t.Fatalf("已知编码应保留不被清空: %v", out.Allergens)
	}
}

// FOOD-LOGIC-015：缺换算依据的系数拒绝（null/空串/纯空白三种形态 × 两个系数）。
func TestFoodLOGIC015_CoefficientWithoutSourceRejected(t *testing.T) {
	for _, source := range []*string{nil, sptr(""), sptr("   ")} {
		in := validFood()
		in.ConversionSource = source
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("density 有值但换算依据=%v 应拒绝，得到 %v", source, err)
		}
		in = validFood()
		in.ConversionSource = source
		in.DensityGml = nil
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("piece 有值但换算依据=%v 应拒绝，得到 %v", source, err)
		}
	}
	// 两个系数都为空时换算依据可有可无。
	in := validFood()
	in.DensityGml, in.PieceWeightG = nil, nil
	if _, err := ValidateFoodInput(in); err != nil {
		t.Fatalf("无系数时不应要求换算依据: %v", err)
	}
	in.ConversionSource = nil
	if _, err := ValidateFoodInput(in); err != nil {
		t.Fatalf("无系数无依据应通过: %v", err)
	}
}

// FOOD-LOGIC-016：换算系数边界与精度。
func TestFoodLOGIC016_CoefficientBoundsAndPrecision(t *testing.T) {
	for _, item := range []struct {
		name              string
		density, piece    *float64
		wantErr           bool
	}{
		{"density=100 上限", fptr(100), fptr(50), false},
		{"piece=1000000 上限", fptr(1.03), fptr(1000000), false},
		{"六位小数", fptr(0.123456), fptr(0.123456), false},
		{"density=0", fptr(0), fptr(50), true},
		{"density 负数", fptr(-1), fptr(50), true},
		{"density=100.0001 超限", fptr(100.0001), fptr(50), true},
		{"piece=1000000.1 超限", fptr(1.03), fptr(1000000.1), true},
		{"七位小数", fptr(0.1234567), fptr(50), true},
		{"piece 七位小数", fptr(1.03), fptr(0.1234567), true},
		{"density NaN", fptr(nan()), fptr(50), true},
		{"piece +Inf", fptr(1.03), fptr(inf(1)), true},
	} {
		in := validFood()
		in.DensityGml, in.PieceWeightG = item.density, item.piece
		_, err := ValidateFoodInput(in)
		if item.wantErr && err != ErrInvalidArgument {
			t.Fatalf("%s: 期望拒绝，得到 %v", item.name, err)
		}
		if !item.wantErr && err != nil {
			t.Fatalf("%s: 期望通过，得到 %v", item.name, err)
		}
	}
}

// FOOD-LOGIC-017：完整性缺标注依据拒绝。
func TestFoodLOGIC017_CompleteWithoutSourceRejected(t *testing.T) {
	for _, source := range []*string{nil, sptr(""), sptr("  ")} {
		in := validFood()
		in.AllergenComplete, in.AllergenSource = true, source
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("allergen_source=%v 且 complete=true 应拒绝，得到 %v", source, err)
		}
	}
}

// FOOD-LOGIC-018：列表查询参数校验。
func TestFoodLOGIC018_ListQueryValidation(t *testing.T) {
	q100 := strings.Repeat("麦", 60) + strings.Repeat("🌾", 40)
	if got := len([]rune(q100)); got != 100 {
		t.Fatalf("夹具码点数错误: %d", got)
	}
	if out, err := ValidateFoodQuery("  "+q100+"  ", "", 1, 20); err != nil || len([]rune(out)) != 100 {
		t.Fatalf("100 码点 q 应通过: %q %v", out, err)
	}
	if _, err := ValidateFoodQuery(strings.Repeat("麦", 101), "", 1, 20); err != ErrInvalidArgument {
		t.Fatal("101 码点 q 应拒绝")
	}
	for _, status := range []string{"", "active", "disabled"} {
		if _, err := ValidateFoodQuery("", status, 1, 20); err != nil {
			t.Fatalf("status=%q 应通过: %v", status, err)
		}
	}
	if _, err := ValidateFoodQuery("", "other", 1, 20); err != ErrInvalidArgument {
		t.Fatal("非法 status 应拒绝")
	}
	if _, err := ValidateFoodQuery("", "", 1, 20); err != nil {
		t.Fatal("page=1 应通过")
	}
	if _, err := ValidateFoodQuery("", "", 0, 20); err != ErrInvalidArgument {
		t.Fatal("page=0 应拒绝")
	}
	for _, size := range []int{1, 100} {
		if _, err := ValidateFoodQuery("", "", 2, size); err != nil {
			t.Fatalf("page_size=%d 应通过: %v", size, err)
		}
	}
	for _, size := range []int{0, 101} {
		if _, err := ValidateFoodQuery("", "", 1, size); err != ErrInvalidArgument {
			t.Fatalf("page_size=%d 应拒绝", size)
		}
	}
}

// FOOD-LOGIC-019：来源字段长度边界 500/1000 码点（含汉字与 emoji 组合）。
func TestFoodLOGIC019_SourceLengthBounds(t *testing.T) {
	src500 := strings.Repeat("据", 300) + strings.Repeat("🌾", 200)
	ref1000 := strings.Repeat("参", 600) + strings.Repeat("🌾", 400)
	if len([]rune(src500)) != 500 || len([]rune(ref1000)) != 1000 {
		t.Fatal("夹具码点数错误")
	}
	// 边界值通过且原样保留。
	in := validFood()
	in.NutritionSource, in.NutritionReference = sptr(src500), sptr(ref1000)
	in.ConversionSource, in.AllergenSource = sptr(src500), sptr(src500)
	out, err := ValidateFoodInput(in)
	if err != nil {
		t.Fatalf("恰好上限应通过: %v", err)
	}
	if *out.NutritionSource != src500 || *out.NutritionReference != ref1000 || *out.ConversionSource != src500 || *out.AllergenSource != src500 {
		t.Fatal("边界值应原样保留")
	}
	// 超限拒绝。
	over := func(name string, set func(*store.FoodWrite)) {
		in := validFood()
		set(&in)
		if _, err := ValidateFoodInput(in); err != ErrInvalidArgument {
			t.Fatalf("%s 超限应拒绝，得到 %v", name, err)
		}
	}
	over("nutrition_source", func(in *store.FoodWrite) { in.NutritionSource = sptr(src500 + "麦") })
	over("nutrition_reference", func(in *store.FoodWrite) { in.NutritionReference = sptr(ref1000 + "麦") })
	over("conversion_source", func(in *store.FoodWrite) { in.ConversionSource = sptr(src500 + "🌾") })
	over("allergen_source", func(in *store.FoodWrite) { in.AllergenSource = sptr(src500 + "🌾") })
}
