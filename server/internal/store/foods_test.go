package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// seedFood 直接插入一行食材（绕过应用层校验），返回自增 ID。
// 默认插入 12 条 001 迁移自带的基础食材之后。
func seedFood(t *testing.T, s *Store, name, status string) int64 {
	t.Helper()
	r, err := s.DB.Exec("INSERT INTO foods(name,status,created_at,updated_at) VALUES(?,?,?,?)", name, status, testNow.Unix(), testNow.Unix())
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// adminSession 用 InitAdmin 构造一个管理员会话（每个测试独享新库）。
func adminSession(t *testing.T, s *Store) Session {
	t.Helper()
	a, err := s.InitAdmin(context.Background(), "food_admin", "hash", testNow)
	if err != nil {
		t.Fatal(err)
	}
	return session(t, s, a)
}

// plainFood 返回一份无营养/无换算/过敏原未知的合法输入。
func plainFood(name string) FoodWrite {
	return FoodWrite{Name: name, Status: "active", Allergens: []string{}}
}

// fullFood 返回营养/换算/过敏原完整的输入。
func fullFood(name string) FoodWrite {
	return FoodWrite{
		Name: name, Status: "active",
		EnergyKcal100g: f64ptr(121), ProteinG100g: f64ptr(12.5), FatG100g: f64ptr(3.3), CarbohydrateG100g: f64ptr(68.6),
		NutritionSource: strptr("成分表"), NutritionReference: strptr("ref-01"),
		DensityGml: f64ptr(1.03), PieceWeightG: f64ptr(50), ConversionSource: strptr("包装标注"),
		Allergens: []string{"egg"}, AllergenComplete: true, AllergenSource: strptr("标签"),
	}
}

func f64ptr(v float64) *float64 { return &v }
func strptr(s string) *string   { return &s }

// foodCheckDirectSQL 绕过应用层执行 INSERT，断言 CHECK 拒绝。
func foodCheckDirectSQL(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err == nil {
		t.Fatalf("CHECK 未拒绝: %s", query)
	}
}

// FOOD-STORE-001：DB CHECK 拒绝部分营养组（六字段逐一置 NULL 的 6 组组合）。
func TestFoodSTORE001_CheckRejectsPartialNutritionGroup(t *testing.T) {
	s := testStore(t)
	base := "INSERT INTO foods(name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,created_at,updated_at) VALUES('部分组','active',?,?,?,?,?,?,1,1)"
	for i := 0; i < 6; i++ {
		var energy, protein, fat, carb, src, ref any
		energy, protein, fat, carb = 121.0, 12.5, 3.3, 68.6
		src, ref = "成分表", "ref-01"
		// 第 i 组：第 i 个字段置 NULL，其余保持有值。
		switch i {
		case 0:
			energy = nil
		case 1:
			protein = nil
		case 2:
			fat = nil
		case 3:
			carb = nil
		case 4:
			src = nil
		case 5:
			ref = nil
		}
		foodCheckDirectSQL(t, s, base, energy, protein, fat, carb, src, ref)
	}
}

// FOOD-STORE-002：DB CHECK 拒绝缺失/空营养来源（四值有值但来源 NULL/""/纯空白）。
func TestFoodSTORE002_CheckRejectsMissingOrBlankSource(t *testing.T) {
	s := testStore(t)
	for i, src := range []any{nil, "", "   "} {
		foodCheckDirectSQL(t, s, "INSERT INTO foods(name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,created_at,updated_at) VALUES(?,?,121,12.5,3.3,68.6,?, 'ref-01',1,1)", fmt.Sprintf("来源缺失%d", i), "active", src)
		foodCheckDirectSQL(t, s, "INSERT INTO foods(name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,created_at,updated_at) VALUES(?,?,121,12.5,3.3,68.6,'成分表',?,1,1)", fmt.Sprintf("依据缺失%d", i), "active", src)
	}
}

// FOOD-STORE-003：DB CHECK 拒绝缺来源的系数与标注。
func TestFoodSTORE003_CheckRejectsCoefficientOrCompleteWithoutSource(t *testing.T) {
	s := testStore(t)
	// A. density 有值但 conversion_source NULL。
	foodCheckDirectSQL(t, s, "INSERT INTO foods(name,status,density_g_ml,conversion_source,allergens_json,created_at,updated_at) VALUES('系数A','active',1.03,NULL,'[]',1,1)")
	// B. allergen_complete=1 但 allergen_source NULL。
	foodCheckDirectSQL(t, s, "INSERT INTO foods(name,status,allergen_complete,allergen_source,allergens_json,created_at,updated_at) VALUES('标注B','active',1,NULL,'[]',1,1)")
}

// FOOD-STORE-004：CHECK 允许全空分支与完整分支通过，无误伤 NULL。
func TestFoodSTORE004_CheckAllowsNullAndCompleteBranches(t *testing.T) {
	s := testStore(t)
	// 全空分支：六字段全 NULL。
	if _, err := s.DB.Exec("INSERT INTO foods(name,status,allergens_json,created_at,updated_at) VALUES('全空','active','[]',1,1)"); err != nil {
		t.Fatalf("全空分支被误伤: %v", err)
	}
	// 完整分支：全 NOT NULL 且来源非空白。
	if _, err := s.DB.Exec("INSERT INTO foods(name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,allergens_json,created_at,updated_at) VALUES('完整','active',121,12.5,3.3,68.6,'成分表','ref-01','[]',1,1)"); err != nil {
		t.Fatalf("完整分支被误伤: %v", err)
	}
	if n := queryInt(t, s.DB, "SELECT count(*) FROM foods WHERE name IN ('全空','完整')"); n != 2 {
		t.Fatalf("插入行数 %d", n)
	}
}

// FOOD-STORE-005：重名创建与并发——顺序与并发各恰一胜，其余 409。
func TestFoodSTORE005_DuplicateNameSequentialAndConcurrent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := adminSession(t, s)
	if _, err := s.CreateFood(ctx, admin, plainFood("燕麦片"), testNow); err != nil {
		t.Fatal(err)
	}
	// A. 顺序再建同名。
	_, err := s.CreateFood(ctx, admin, plainFood("燕麦片"), testNow)
	code(t, err, "FOOD_NAME_TAKEN")
	var appErr *AppError
	if errors.As(err, &appErr) && appErr.Status != 409 {
		t.Fatalf("status=%d", appErr.Status)
	}
	// B. 两个 goroutine 并发创建同名：单连接池串行化，恰一个成功。
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := s.CreateFood(ctx, admin, plainFood("并发燕麦"), testNow); errs <- e }()
	}
	ok := 0
	for i := 0; i < 2; i++ {
		if e := <-errs; e == nil {
			ok++
		} else {
			code(t, e, "FOOD_NAME_TAKEN")
		}
	}
	if ok != 1 {
		t.Fatalf("并发创建成功数 %d", ok)
	}
}

// FOOD-STORE-006：版本冲突整体回滚——名称、version、审计均无任何变化。
func TestFoodSTORE006_VersionConflictRollsBackEverything(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := adminSession(t, s)
	created, err := s.CreateFood(ctx, admin, plainFood("冲突食材"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	// 推到 version=3。
	for i := int64(2); i <= 3; i++ {
		in := plainFood(fmt.Sprintf("冲突食材-v%d", i))
		if _, err = s.UpdateFood(ctx, admin, created.ID, i-1, in, testNow); err != nil {
			t.Fatal(err)
		}
	}
	auditsBefore := queryInt(t, s.DB, "SELECT count(*) FROM resource_audit_logs")
	// 以过期 version=2 修改名称。
	_, err = s.UpdateFood(ctx, admin, created.ID, 2, plainFood("被篡改的名称"), testNow)
	code(t, err, "VERSION_CONFLICT")
	got, err := s.FoodByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "冲突食材-v3" || got.Version != 3 {
		t.Fatalf("版本冲突后状态被修改: %+v", got)
	}
	if audits := queryInt(t, s.DB, "SELECT count(*) FROM resource_audit_logs"); audits != auditsBefore {
		t.Fatalf("版本冲突写入了审计: %d -> %d", auditsBefore, audits)
	}
}

// FOOD-STORE-007/010：审计写入失败时，食材更新（含状态变化）整体回滚；
// 状态复核、条件更新、审计写入在同一连接同一事务内完成，无半提交数据。
func TestFoodSTORE007_010_AuditFailureRollsBackWholeUpdate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := adminSession(t, s)
	created, err := s.CreateFood(ctx, admin, plainFood("回滚食材"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if audits := queryInt(t, s.DB, "SELECT count(*) FROM resource_audit_logs"); audits != 1 {
		t.Fatalf("创建审计缺失: %d", audits)
	}
	if _, err = s.DB.ExecContext(ctx, "CREATE TRIGGER fail_food_audit BEFORE INSERT ON resource_audit_logs BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	// 更新形态：改名（food_updated）。
	in := plainFood("改名后")
	if _, err = s.UpdateFood(ctx, admin, created.ID, created.Version, in, testNow); err == nil {
		t.Fatal("审计失败未上抛")
	}
	// 启停形态：状态变化（food_disabled）。
	disabled := fullFood("改名后")
	disabled.Status = "disabled"
	if _, err = s.UpdateFood(ctx, admin, created.ID, created.Version, disabled, testNow); err == nil {
		t.Fatal("审计失败未上抛")
	}
	got, err := s.FoodByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "回滚食材" || got.Status != "active" || got.Version != created.Version || !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("半提交数据残留: %+v", got)
	}
	if audits := queryInt(t, s.DB, "SELECT count(*) FROM resource_audit_logs"); audits != 1 {
		t.Fatalf("失败事务留下审计: %d", audits)
	}
	if _, err = s.DB.ExecContext(ctx, "DROP TRIGGER fail_food_audit"); err != nil {
		t.Fatal(err)
	}
	// 修复注入后同一更新成功。
	updated, err := s.UpdateFood(ctx, admin, created.ID, created.Version, in, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "改名后" || updated.Version != created.Version+1 {
		t.Fatalf("修复后更新失败: %+v", updated)
	}
}

// FOOD-STORE-008：停用保留用户忌口——偏好行逐字段不变，不物理删除。
func TestFoodSTORE008_DisableKeepsUserPreferences(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	su := session(t, s, u)
	admin := adminSession(t, s)
	x, err := s.CreateFood(ctx, admin, plainFood("忌口食材"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Preferences(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.ExcludedFoodIDs = []int64{x.ID}
	if _, err = s.SavePreferences(ctx, su, p, testNow); err != nil {
		t.Fatal(err)
	}
	type prefRow struct {
		allergens, excluded, dietary string
		energy, protein, fat, carb   sql.NullFloat64
		updated                      int64
	}
	snapshot := func() prefRow {
		t.Helper()
		var row prefRow
		if err := s.DB.QueryRow("SELECT allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,daily_protein_g,daily_fat_g,daily_carbohydrate_g,updated_at FROM user_preferences WHERE user_id=?", u.ID).Scan(&row.allergens, &row.excluded, &row.dietary, &row.energy, &row.protein, &row.fat, &row.carb, &row.updated); err != nil {
			t.Fatal(err)
		}
		return row
	}
	before := snapshot()
	in := plainFood("忌口食材")
	in.Status = "disabled"
	if _, err = s.UpdateFood(ctx, admin, x.ID, x.Version, in, testNow); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("偏好行被修改: %+v -> %+v", before, after)
	}
	// 食材未物理删除，仅停用。
	got, err := s.FoodByID(ctx, x.ID)
	if err != nil || got.Status != "disabled" {
		t.Fatalf("停用食材异常: %+v %v", got, err)
	}
	if got2, err := s.Preferences(ctx, u.ID); err != nil || len(got2.ExcludedFoodIDs) != 1 || got2.ExcludedFoodIDs[0] != x.ID {
		t.Fatalf("忌口引用丢失: %+v %v", got2, err)
	}
}

// FOOD-STORE-009：重新启用保留 ID 与版本递增。
func TestFoodSTORE009_ReenableKeepsIDAndBumpsVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := adminSession(t, s)
	created, err := s.CreateFood(ctx, admin, plainFood("启停食材"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	disabled := plainFood("启停食材")
	disabled.Status = "disabled"
	d, err := s.UpdateFood(ctx, admin, created.ID, created.Version, disabled, testNow)
	if err != nil {
		t.Fatal(err)
	}
	enabled := plainFood("启停食材")
	e, err := s.UpdateFood(ctx, admin, created.ID, d.Version, enabled, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != created.ID || e.Status != "active" || e.Version != d.Version+1 {
		t.Fatalf("重新启用破坏身份/版本: %+v", e)
	}
	if n := queryInt(t, s.DB, "SELECT count(*) FROM foods WHERE name='启停食材'"); n != 1 {
		t.Fatalf("重新启用生成了新行: %d", n)
	}
}

// FOOD-STORE-011：列表与 total 同一只读事务；混合状态、名称匹配、ID 倒序。
func TestFoodSTORE011_ListTotalSameReadOnlyTransaction(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		status := "active"
		if i%3 == 0 {
			status = "disabled"
		}
		seedFood(t, s, fmt.Sprintf("列表食材%02d", i), status)
	}
	items, total, err := s.FoodAdminList(ctx, "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := queryInt(t, s.DB, "SELECT count(*) FROM foods") // 12 条基础 + 25 条新插入
	if total != wantTotal {
		t.Fatalf("total=%d want=%d", total, wantTotal)
	}
	if len(items) != 20 {
		t.Fatalf("第一页条数 %d", len(items))
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].ID <= items[i].ID {
			t.Fatalf("未按 ID 倒序: %v", items)
		}
	}
	// 名称过滤与 total 一致。
	matched, total, err := s.FoodAdminList(ctx, "列表食材", "", 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(matched) != 5 {
		t.Fatalf("第二页: items=%d total=%d", len(matched), total)
	}
	// active 过滤。
	actives := queryInt(t, s.DB, "SELECT count(*) FROM foods WHERE status='active'")
	_, total, err = s.FoodCatalog(ctx, "", 1, 100)
	if err != nil || total != actives {
		t.Fatalf("catalog total=%d want=%d err=%v", total, actives, err)
	}
}

// FOOD-STORE-012：q 按字面子串匹配，% 与 _ 不解释为 SQL 通配符。
func TestFoodSTORE012_LiteralSubstringNoWildcards(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedFood(t, s, "100%", "active")
	seedFood(t, s, "a_b", "active")
	cases := []struct {
		q    string
		want string
	}{
		{"%", "100%"},
		{"_", "a_b"},
		{"100%", "100%"},
	}
	for _, tc := range cases {
		items, total, err := s.FoodCatalog(ctx, tc.q, 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		// 12 条基础食材不含 % 或 _；结果应只含字面命中的行。
		if total != 1 || len(items) != 1 || items[0].Name != tc.want {
			t.Fatalf("q=%q: items=%v total=%d", tc.q, items, total)
		}
	}
}

// FOOD-STORE-013：审计表写入与 id DESC 分页；details_json 由 store 编码。
func TestFoodSTORE013_ResourceAuditPagingAndDetails(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin := adminSession(t, s)
	created, err := s.CreateFood(ctx, admin, fullFood("审计食材"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	// 再产生两条更新事件（一条普通更新、一条停用）。
	renamed := fullFood("审计食材2")
	if _, err = s.UpdateFood(ctx, admin, created.ID, created.Version, renamed, testNow); err != nil {
		t.Fatal(err)
	}
	renamed.Status = "disabled"
	if _, err = s.UpdateFood(ctx, admin, created.ID, created.Version+1, renamed, testNow); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListResourceAudit(ctx, "food", created.ID, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("审计条数 items=%d total=%d", len(items), total)
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].ID <= items[i].ID {
			t.Fatalf("未按 id DESC: %v", items)
		}
	}
	// 最新一条为停用：action 与 details 编码由 store 完成。
	latest := items[0]
	if latest.Action != "food_disabled" || latest.ResourceType != "food" || latest.ResourceID != created.ID || latest.ActorUserID != admin.User.ID {
		t.Fatalf("最新审计字段异常: %+v", latest)
	}
	if latest.Details["status_before"] != "active" || latest.Details["status_after"] != "disabled" {
		t.Fatalf("前后状态缺失: %+v", latest.Details)
	}
	if v, ok := latest.Details["version_before"].(float64); !ok || int64(v) != created.Version+1 {
		t.Fatalf("version_before 缺失: %+v", latest.Details["version_before"])
	}
	if v, ok := latest.Details["version_after"].(float64); !ok || int64(v) != created.Version+2 {
		t.Fatalf("version_after 缺失: %+v", latest.Details["version_after"])
	}
	fields, ok := latest.Details["fields"].([]any)
	if !ok || len(fields) == 0 || fields[0] != "status" {
		t.Fatalf("变更字段数组缺失: %+v", latest.Details["fields"])
	}
	// action 筛选。
	disabledEvents, total, err := s.ListResourceAudit(ctx, "food", created.ID, "food_disabled", 1, 20)
	if err != nil || total != 1 || len(disabledEvents) != 1 || disabledEvents[0].Action != "food_disabled" {
		t.Fatalf("action 筛选异常: %d %v", total, err)
	}
	// created_at 为 UTC。
	if !latest.CreatedAt.Equal(testNow) || latest.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at 非 UTC: %v", latest.CreatedAt)
	}
}
