package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// 食谱模块 store 层测试（REC-STORE-001~020）。

// seedRecipeFoods 预置三类测试食材：active 完整、active 不完整、disabled
//（绕过应用层直接插入，标注字段显式可控），返回对应 ID。
func seedRecipeFoods(t *testing.T, s *Store) (complete, incomplete, disabled int64) {
	t.Helper()
	r, err := s.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,allergen_source,created_at,updated_at) VALUES('完整食材','active','[\"milk\"]',1,'包装标注',?,?)", testNow.Unix(), testNow.Unix())
	if err != nil {
		t.Fatal(err)
	}
	complete, _ = r.LastInsertId()
	r, err = s.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,created_at,updated_at) VALUES('不完整食材','active','[]',0,?,?)", testNow.Unix(), testNow.Unix())
	if err != nil {
		t.Fatal(err)
	}
	incomplete, _ = r.LastInsertId()
	r, err = s.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,allergen_source,created_at,updated_at) VALUES('停用食材','disabled','[]',1,'历史标注',?,?)", testNow.Unix(), testNow.Unix())
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ = r.LastInsertId()
	return
}

// validRecipeWrite 返回一份可发布的合法输入：一个步骤、每食材一条原料。
func validRecipeWrite(name string, foodIDs ...int64) RecipeWrite {
	src := "核对依据：包装标签"
	in := RecipeWrite{
		Name: name, Servings: 2, DurationMinutes: 15,
		Steps: []string{"第一步。"}, Source: "个人记录",
		AllergenComplete: true, AllergenSource: &src,
		AdditionalAllergens: []string{},
	}
	for _, id := range foodIDs {
		in.Ingredients = append(in.Ingredients, RecipeIngredientInput{FoodID: id, QuantityMilli: 250000, Unit: "g"})
	}
	return in
}

// createPrivateRecipe 以普通用户会话创建一份私有草稿。
func createPrivateRecipe(t *testing.T, s *Store, owner Session, name string, foodIDs ...int64) RecipeDetail {
	t.Helper()
	d, err := s.CreateRecipe(context.Background(), owner, false, validRecipeWrite(name, foodIDs...), testNow)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// createPublicRecipe 以管理员会话创建公共草稿。
func createPublicRecipe(t *testing.T, s *Store, admin Session, name string, foodIDs ...int64) RecipeDetail {
	t.Helper()
	d, err := s.CreateRecipe(context.Background(), admin, true, validRecipeWrite(name, foodIDs...), testNow)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// recipeCheckDirectSQL 绕过应用层执行 INSERT，断言数据库约束拒绝。
func recipeCheckDirectSQL(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err == nil {
		t.Fatalf("约束未拒绝: %s", query)
	}
}

// REC-STORE-001/002：所有权联合 CHECK 与枚举约束。
func TestRecipeSTORE001_002_OwnershipAndEnumChecks(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "recipe_owner", "user")
	// 合法组合必须成功。
	if _, err := s.DB.Exec("INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('private',?,'draft','合法私有',1,1,'[]',0,1,0,0)", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('public',NULL,'draft','合法公共',1,1,'[]',0,1,0,0)"); err != nil {
		t.Fatal(err)
	}
	recipeCheckDirectSQL(t, s, "INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('private',NULL,'draft','私有无主',1,1,'[]',0,1,0,0)")
	recipeCheckDirectSQL(t, s, "INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('public',?,'draft','公共有主',1,1,'[]',0,1,0,0)", u.ID)
	recipeCheckDirectSQL(t, s, "INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('internal',NULL,'draft','坏可见性',1,1,'[]',0,1,0,0)")
	recipeCheckDirectSQL(t, s, "INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('public',NULL,'deleted','坏状态',1,1,'[]',0,1,0,0)")
}

// REC-STORE-003：父子外键 RESTRICT。
func TestRecipeSTORE003_ForeignKeyRestrict(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "fk_user", "user")
	food, _, _ := seedRecipeFoods(t, s)
	r, err := s.DB.Exec("INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('private',?,'draft','外键食谱',1,1,'[]',0,1,0,0)", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipeID, _ := r.LastInsertId()
	recipeCheckDirectSQL(t, s, "INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(99999,1,?,1,'g')", food)
	recipeCheckDirectSQL(t, s, "INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,1,99999,1,'g')", recipeID)
	if _, err := s.DB.Exec("INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,1,?,1,'g')", recipeID, food); err != nil {
		t.Fatal(err)
	}
	// 被引用的 recipe 与 food 删除均受 RESTRICT 限制。
	recipeCheckDirectSQL(t, s, "DELETE FROM recipes WHERE id=?", recipeID)
	recipeCheckDirectSQL(t, s, "DELETE FROM foods WHERE id=?", food)
}

// REC-STORE-004/005/006/007/008：子表唯一、用量范围、单位、position、
// 过敏原编码约束。
func TestRecipeSTORE004_008_IngredientAndAllergenChecks(t *testing.T) {
	s := testStore(t)
	u := user(t, s, "constraint_user", "user")
	food, _, disabled := seedRecipeFoods(t, s)
	r, err := s.DB.Exec("INSERT INTO recipes(visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES('private',?,'draft','约束食谱',1,1,'[]',0,1,0,0)", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipeID, _ := r.LastInsertId()
	insert := "INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,?,?,?,?)"
	if _, err := s.DB.Exec(insert, recipeID, 1, food, 250000, "g"); err != nil {
		t.Fatal(err)
	}
	// 004：同食谱重复食材（position 不同）触发 UNIQUE(recipe_id,food_id)。
	recipeCheckDirectSQL(t, s, insert, recipeID, 2, food, 100000, "g")
	// 005：用量范围 0/-1/1000000001 拒绝；1 与 1000000000 通过。
	for _, bad := range []int64{0, -1, 1000000001} {
		recipeCheckDirectSQL(t, s, insert, recipeID, 3, food, bad, "g")
	}
	for _, good := range []int64{1, 1000000000} {
		if _, err := s.DB.Exec("INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,?,?,?,?)", recipeID, 100, disabled, good, "g"); err != nil {
			t.Fatalf("合法用量 %d 被拒绝: %v", good, err)
		}
		if _, err := s.DB.Exec("DELETE FROM recipe_ingredients WHERE recipe_id=? AND position=100", recipeID); err != nil {
			t.Fatal(err)
		}
	}
	// 006：单位仅 g/ml/piece。
	for _, badUnit := range []string{"kg", "升", ""} {
		recipeCheckDirectSQL(t, s, insert, recipeID, 4, food, 1, badUnit)
	}
	// 007：position 0/101/重复主键拒绝。
	recipeCheckDirectSQL(t, s, insert, recipeID, 0, food, 1, "g")
	recipeCheckDirectSQL(t, s, insert, recipeID, 101, food, 1, "g")
	recipeCheckDirectSQL(t, s, insert, recipeID, 1, food, 1, "ml")
	// 008：附加过敏原编码 CHECK。
	for _, bad := range []string{"gluten-x", ""} {
		recipeCheckDirectSQL(t, s, "INSERT INTO recipe_allergens(recipe_id,allergen_code) VALUES(?,?)", recipeID, bad)
	}
	if _, err := s.DB.Exec("INSERT INTO recipe_allergens(recipe_id,allergen_code) VALUES(?,'peanut')", recipeID); err != nil {
		t.Fatal(err)
	}
}

// REC-STORE-009/011：子项重插失败整体回滚——通过 store 层传入重复
// food_id 制造 UNIQUE 冲突，父行与旧子项必须原样保留。
func TestRecipeSTORE009_011_ChildReplaceFailureRollsBack(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "回滚食谱", food)
	old := validRecipeWrite("回滚食谱", food)
	// 构造重复 food_id 的非法输入绕过 logic（store 层为最后防线）。
	bad := old
	bad.Ingredients = append(bad.Ingredients, RecipeIngredientInput{FoodID: food, QuantityMilli: 1, Unit: "g"})
	bad.Name = "改名后"
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", bad, testNow); err == nil {
		t.Fatal("重复 food_id 重插应失败")
	}
	// 父行 version 仍为 1、内容不变，旧 ingredients/allergens 完整保留。
	var version int64
	var name string
	if err := s.DB.QueryRow("SELECT version,name FROM recipes WHERE id=?", created.ID).Scan(&version, &name); err != nil {
		t.Fatal(err)
	}
	if version != 1 || name != "回滚食谱" {
		t.Fatalf("回滚不完整: version=%d name=%s", version, name)
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM recipe_ingredients WHERE recipe_id=?", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("旧子项未保留: count=%d err=%v", count, err)
	}
	var audits int
	if err := s.DB.QueryRow("SELECT count(*) FROM resource_audit_logs WHERE resource_type='recipe'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("失败更新不应追加审计: %d %v", audits, err)
	}
}

// REC-STORE-010：审计写入失败回滚——用 TRIGGER 注入审计表写入失败。
func TestRecipeSTORE010_AuditFailureRollsBackWholeUpdate(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "审计回滚食谱", food)
	if _, err := s.DB.Exec("CREATE TRIGGER fail_recipe_audit BEFORE INSERT ON resource_audit_logs BEGIN SELECT RAISE(ABORT,'injected audit failure'); END"); err != nil {
		t.Fatal(err)
	}
	next := validRecipeWrite("审计回滚食谱", food)
	next.Name = "改名触发审计"
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", next, testNow); err == nil {
		t.Fatal("审计失败应导致整体失败")
	}
	if _, err := s.DB.Exec("DROP TRIGGER fail_recipe_audit"); err != nil {
		t.Fatal(err)
	}
	var version int64
	var name string
	if err := s.DB.QueryRow("SELECT version,name FROM recipes WHERE id=?", created.ID).Scan(&version, &name); err != nil {
		t.Fatal(err)
	}
	if version != 1 || name != "审计回滚食谱" {
		t.Fatalf("审计失败未整体回滚: version=%d name=%s", version, name)
	}
	var ingredients, allergens int
	if err := s.DB.QueryRow("SELECT count(*) FROM recipe_ingredients WHERE recipe_id=?", created.ID).Scan(&ingredients); err != nil || ingredients != 1 {
		t.Fatalf("子项未保留: %d %v", ingredients, err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM recipe_allergens WHERE recipe_id=?", created.ID).Scan(&allergens); err != nil || allergens != 0 {
		t.Fatalf("allergens: %d %v", allergens, err)
	}
}

// REC-STORE-012：版本竞争——条件更新影响行数≠1 时整体回滚返回 409。
func TestRecipeSTORE012_VersionConflict(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "版本食谱", food)
	a := validRecipeWrite("版本A", food)
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", a, testNow); err != nil {
		t.Fatal(err)
	}
	b := validRecipeWrite("版本B", food)
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", b, testNow); err == nil {
		t.Fatal("旧版本更新应失败")
	} else {
		code(t, err, "VERSION_CONFLICT")
	}
	var name string
	var version int64
	if err := s.DB.QueryRow("SELECT name,version FROM recipes WHERE id=?", created.ID).Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != "版本A" || version != 2 {
		t.Fatalf("竞争失败后内容被改动: %s v%d", name, version)
	}
}

// REC-STORE-013：事务内重读食材——停用后保持 published 编辑被拒，
// 新增停用引用被拒。
func TestRecipeSTORE013_FoodConcurrentDisable(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, disabled := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "并发食谱", food)
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "published", validRecipeWrite("并发食谱", food), testNow); err != nil {
		t.Fatal(err)
	}
	// 模拟并发停用：另一连接直接停用食材。
	if _, err := s.DB.Exec("UPDATE foods SET status='disabled' WHERE id=?", food); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, 2, "published", validRecipeWrite("并发食谱", food), testNow)
	code(t, err, "RECIPE_NOT_PUBLISHABLE")
	// 新增停用引用：FOOD_UNAVAILABLE。
	if _, err := s.DB.Exec("UPDATE foods SET status='active' WHERE id=?", food); err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateRecipe(context.Background(), admin, true, created.ID, 2, "draft", validRecipeWrite("并发食谱", food, disabled), testNow)
	code(t, err, "FOOD_UNAVAILABLE")
}

// REC-STORE-014：无变化 PUT 不加版本不写审计。
//（REC-REG-003 回归等价用例：steps 纳入 recipeWriteEqual 比较后，
// 完全相同的 PUT 仍须走此无变化路径——本测试即为其回归保障，不另重复。）
func TestRecipeSTORE014_NoChangePut(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "无变化食谱", food)
	same := validRecipeWrite("无变化食谱", food)
	out, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", same, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != created.Version {
		t.Fatalf("无变化 PUT 升版本: %d", out.Version)
	}
	var audits int
	if err := s.DB.QueryRow("SELECT count(*) FROM resource_audit_logs WHERE resource_type='recipe'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	// 仅创建时的一条 recipe_created。
	if audits != 1 {
		t.Fatalf("无变化 PUT 写了审计: %d", audits)
	}
	var updatedAt int64
	if err := s.DB.QueryRow("SELECT updated_at FROM recipes WHERE id=?", created.ID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt != created.UpdatedAt.Unix() {
		t.Fatal("无变化 PUT 推进了 updated_at")
	}
}

// REC-REG-001（Bug 2 回归）：仅修改 steps 的 PUT 必须落库并升版本，
// 不得被 recipeWriteEqual 误判为无变化。
func TestRecipeREG001_StepsOnlyPut(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "仅改步骤", food)
	in := validRecipeWrite("仅改步骤", food)
	in.Steps = []string{"新第一步。", "新第二步。"}
	out, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, 1, "draft", in, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 2 {
		t.Fatalf("仅改步骤未升版本: %d", out.Version)
	}
	if strings.Join(out.Steps, "|") != "新第一步。|新第二步。" {
		t.Fatalf("响应 steps 为旧值: %v", out.Steps)
	}
	got, err := s.RecipeDetail(context.Background(), created.ID, RecipeDetailReadScope{Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Steps, "|") != "新第一步。|新第二步。" {
		t.Fatalf("落库 steps 为旧值: %v", got.Steps)
	}
	if got.Version != 2 {
		t.Fatalf("落库版本未推进: %d", got.Version)
	}
}

// REC-REG-002（Bug 1 回归）：更新成功后的响应必须是重新读取的新数据，
// 而非更新前的旧 row。
func TestRecipeREG002_UpdateReturnsFreshDetail(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "旧名字", food)
	src := "新核对依据：官网"
	in := validRecipeWrite("新名字", food)
	in.Servings = 4
	in.DurationMinutes = 30
	in.Steps = []string{"新第一步。", "新第二步。", "新第三步。"}
	in.Source = "新个人记录"
	in.AllergenSource = &src
	out, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", in, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != created.Version+1 {
		t.Fatalf("版本未 +1: %d", out.Version)
	}
	if out.Name != "新名字" {
		t.Fatalf("响应 name 为旧值: %q", out.Name)
	}
	if out.Servings != 4 || out.DurationMinutes != 30 {
		t.Fatalf("响应 servings/duration 为旧值: %d/%d", out.Servings, out.DurationMinutes)
	}
	if strings.Join(out.Steps, "|") != "新第一步。|新第二步。|新第三步。" {
		t.Fatalf("响应 steps 为旧值: %v", out.Steps)
	}
	if out.Source != "新个人记录" {
		t.Fatalf("响应 source 为旧值: %q", out.Source)
	}
	if !out.AllergenComplete || out.AllergenSource == nil || *out.AllergenSource != src {
		t.Fatalf("响应 allergen 字段为旧值: complete=%v source=%v", out.AllergenComplete, out.AllergenSource)
	}
}

// countDriver 包装驱动统计查询次数（REC-STORE-015 的 N+1 断言）。
type countDriver struct {
	base    driver.Driver
	mu      sync.Mutex
	queries int
}

func (d *countDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &countConn{Conn: c, d: d}, nil
}

type countConn struct {
	driver.Conn
	d *countDriver
}

func (c *countConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	c.d.queries++
	c.d.mu.Unlock()
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func (c *countConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

// BeginTx 委托底层驱动的只读/只写事务开启，保持 BeginTx(ReadOnly) 可用。
func (c *countConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

// REC-STORE-015：列表派生批量查询无逐条 N+1；派生值正确。
func TestRecipeSTORE015_ListDerivedNoNPlusOne(t *testing.T) {
	counted := openCountedStore(t)
	s := counted.store
	admin := adminSession(t, s)
	food, incomplete, disabled := seedRecipeFoods(t, s)
	// 25 个公共食谱：交替引用完整/不完整/停用（历史遗留）食材。
	var created []RecipeDetail
	for i := 0; i < 25; i++ {
		var d RecipeDetail
		switch i % 3 {
		case 0:
			d = createPublicRecipe(t, s, admin, fmt.Sprintf("派生食谱%02d", i), food)
		case 1:
			d = createPublicRecipe(t, s, admin, fmt.Sprintf("派生食谱%02d", i), incomplete)
		default:
			d = createPublicRecipe(t, s, admin, fmt.Sprintf("派生食谱%02d", i), food)
			// 直接 SQL 附加一条停用引用，模拟历史遗留引用。
			if _, e := s.DB.Exec("INSERT INTO recipe_ingredients(recipe_id,position,food_id,quantity_milli,unit) VALUES(?,?,?,1,'g')", d.ID, 2, disabled); e != nil {
				t.Fatal(e)
			}
		}
		created = append(created, d)
	}
	for _, size := range []int{20, 100} {
		counted.reset()
		items, total, err := s.ListRecipes(context.Background(), RecipeListQuery{Visibility: "public", Status: "draft", Page: 1, Size: size})
		if err != nil {
			t.Fatal(err)
		}
		if total != 25 || len(items) != min(size, 25) {
			t.Fatalf("size=%d total=%d items=%d", size, total, len(items))
		}
		// 派生值正确性：按 id DESC 对齐 created 逆序。预期：0→完整；
		// 1→不完整（complete=false）；2→含停用（disabled=true，两引用
		// 标注均完整故 complete 仍为 true）。
		for idx, it := range items {
			i := 25 - 1 - idx
			wantDisabled := i%3 == 2
			wantComplete := i%3 != 1
			if it.HasDisabledFoods != wantDisabled || it.RestrictionMetadataComplete != wantComplete {
				t.Fatalf("size=%d idx=%d 派生错误: complete=%v(期望%v) disabled=%v(期望%v)", size, i, it.RestrictionMetadataComplete, wantComplete, it.HasDisabledFoods, wantDisabled)
			}
		}
		// 查询次数常数级：total 计数 + 当页行 + 一次批量聚合，不随页内
		// 食谱数/原料数逐条增长（阈值 10 远高于实现所需，远低于逐条）。
		if n := counted.count(); n > 10 {
			t.Fatalf("size=%d 查询次数 %d 疑似 N+1", size, n)
		}
	}
}

// openCountedStore 打开一个带查询计数驱动的全新 v5 库（同包可调用迁移器）。
func openCountedStore(t *testing.T) *countedStore {
	t.Helper()
	d := &countDriver{base: &sqlite.Driver{}}
	name := fmt.Sprintf("sqlite-count-%d", faultDriverSeq.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, "file:"+filepath.Join(t.TempDir(), "count.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db}
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &countedStore{store: s, driver: d}
}

// countedStore 组合计数驱动与 Store，提供查询计数断言。
type countedStore struct {
	store  *Store
	driver *countDriver
}

func (c *countedStore) reset() {
	c.driver.mu.Lock()
	c.driver.queries = 0
	c.driver.mu.Unlock()
}

func (c *countedStore) count() int {
	c.driver.mu.Lock()
	defer c.driver.mu.Unlock()
	return c.driver.queries
}

// REC-STORE-017：列表只读事务与排序——25 条公共食谱，page=2&size=10
// 返回 id DESC 的第 11～20 条。
func TestRecipeSTORE017_ListOrderAndPaging(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	var ids []int64
	for i := 0; i < 25; i++ {
		d := createPublicRecipe(t, s, admin, fmt.Sprintf("排序食谱%02d", i))
		ids = append(ids, d.ID)
	}
	items, total, err := s.ListRecipes(context.Background(), RecipeListQuery{Visibility: "public", Status: "draft", Page: 2, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(items) != 10 {
		t.Fatalf("total=%d items=%d", total, len(items))
	}
	// id DESC 的第 11～20 条。
	for i, it := range items {
		if it.ID != ids[25-1-(10+i)] {
			t.Fatalf("第 %d 行 id=%d 顺序错误", i, it.ID)
		}
	}
}

// REC-STORE-016：详情派生与原料来自同一读取上下文；停用食材后派生警示
// 在读取时更新，食谱状态不被自动改写（REC-SEC-012 store 侧）。
func TestRecipeSTORE016_012_DerivedOnRead(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "派生食谱", food)
	d, err := s.RecipeDetail(context.Background(), created.ID, RecipeDetailReadScope{Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	if !d.RestrictionMetadataComplete || d.HasDisabledFoods {
		t.Fatalf("初始派生错误: %+v", d)
	}
	if len(d.EffectiveAllergens) != 1 || d.EffectiveAllergens[0] != "milk" {
		t.Fatalf("有效过敏原错误: %v", d.EffectiveAllergens)
	}
	if d.Ingredients[0].Position != 1 || d.Ingredients[0].FoodName != "完整食材" || d.Ingredients[0].FoodStatus != "active" {
		t.Fatalf("原料行错误: %+v", d.Ingredients[0])
	}
	// 停用食材：状态不被改写，读取时派生警示更新（has_disabled_foods）。
	if _, err := s.DB.Exec("UPDATE foods SET status='disabled' WHERE id=?", food); err != nil {
		t.Fatal(err)
	}
	d, err = s.RecipeDetail(context.Background(), created.ID, RecipeDetailReadScope{Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "draft" {
		t.Fatalf("状态被自动改写: %s", d.Status)
	}
	if !d.HasDisabledFoods {
		t.Fatal("停用后 has_disabled_foods 未更新")
	}
	if d.Ingredients[0].FoodStatus != "disabled" {
		t.Fatalf("原料行未带停用标记: %+v", d.Ingredients[0])
	}
	// 食材过敏原标注改为不完整：restriction_metadata_complete 随读取更新。
	if _, err := s.DB.Exec("UPDATE foods SET allergen_complete=0 WHERE id=?", food); err != nil {
		t.Fatal(err)
	}
	d, err = s.RecipeDetail(context.Background(), created.ID, RecipeDetailReadScope{Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.RestrictionMetadataComplete {
		t.Fatal("食材标注不完整后 restriction_metadata_complete 仍为真")
	}
}

// REC-STORE-018：父子共享版本——任一子项变化体现为父 version+1，
// 子表无独立版本。
func TestRecipeSTORE018_SharedParentVersion(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "共享版本食谱", food)
	if created.Version != 1 {
		t.Fatalf("初版本=%d", created.Version)
	}
	next := validRecipeWrite("共享版本食谱", food)
	next.Ingredients[0].QuantityMilli = 500000
	out, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "draft", next, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 2 || out.Ingredients[0].QuantityMilli != 500000 {
		t.Fatalf("子项变化未推进父版本: v%d %+v", out.Version, out.Ingredients[0])
	}
}

// REC-STORE-020：归档保留食谱 ID，行仍存在，可恢复为草稿。
func TestRecipeSTORE020_ArchiveKeepsID(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	created := createPublicRecipe(t, s, admin, "归档食谱", food)
	if _, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, created.Version, "published", validRecipeWrite("归档食谱", food), testNow); err != nil {
		t.Fatal(err)
	}
	archived, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, 2, "archived", validRecipeWrite("归档食谱", food), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ID != created.ID || archived.Status != "archived" {
		t.Fatalf("归档改变 ID/状态: id=%d %s", archived.ID, archived.Status)
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM recipes WHERE id=?", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("归档后行丢失: %d %v", count, err)
	}
	restored, err := s.UpdateRecipe(context.Background(), admin, true, created.ID, archived.Version, "draft", validRecipeWrite("归档食谱", food), testNow)
	if err != nil {
		t.Fatal(err)
	}
	// 版本链：create v1 → published v2 → archived v3 → draft v4。
	if restored.Status != "draft" || restored.Version != 4 {
		t.Fatalf("恢复失败: %s v%d", restored.Status, restored.Version)
	}
}

// REC-LOGIC-016（store 侧）/REC-SEC-014：审计动作选择规则；私有食谱不写
// 管理员审计。
func TestRecipeSTORE016SEC014_AuditActionsAndPrivacy(t *testing.T) {
	s := testStore(t)
	admin := adminSession(t, s)
	food, _, _ := seedRecipeFoods(t, s)
	// 私有链路：创建/更新/发布全生命周期零审计。
	owner := session(t, s, user(t, s, "privacy_user", "user"))
	private := createPrivateRecipe(t, s, owner, "私有食谱", food)
	if _, err := s.UpdateRecipe(context.Background(), owner, false, private.ID, private.Version, "published", validRecipeWrite("私有食谱", food), testNow); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := s.DB.QueryRow("SELECT count(*) FROM resource_audit_logs WHERE resource_type='recipe'").Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("私有食谱写了审计: %d %v", audits, err)
	}
	// 公共链路：created → published → updated → unpublished → archived
	// → restored，每步恰好一条新审计，动作按状态优先级选择。
	pub := createPublicRecipe(t, s, admin, "审计食谱", food)
	lastAction := func() string {
		t.Helper()
		var action string
		if err := s.DB.QueryRow("SELECT action FROM resource_audit_logs WHERE resource_type='recipe' AND resource_id=? ORDER BY id DESC LIMIT 1", pub.ID).Scan(&action); err != nil {
			t.Fatal(err)
		}
		return action
	}
	count := func() int {
		t.Helper()
		var n int
		if err := s.DB.QueryRow("SELECT count(*) FROM resource_audit_logs WHERE resource_type='recipe' AND resource_id=?", pub.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if lastAction() != "recipe_created" || count() != 1 {
		t.Fatalf("创建审计错误: %s", lastAction())
	}
	update := func(version int64, status, name string) RecipeDetail {
		t.Helper()
		out, err := s.UpdateRecipe(context.Background(), admin, true, pub.ID, version, status, validRecipeWrite(name, food), testNow)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := update(1, "published", "审计食谱")
	if lastAction() != "recipe_published" || count() != 2 {
		t.Fatalf("发布审计错误: %s", lastAction())
	}
	d = update(d.Version, "published", "审计食谱改名")
	if lastAction() != "recipe_updated" || count() != 3 {
		t.Fatalf("保持 published 编辑审计错误: %s", lastAction())
	}
	d = update(d.Version, "draft", "审计食谱改名")
	if lastAction() != "recipe_unpublished" || count() != 4 {
		t.Fatalf("撤回审计错误: %s", lastAction())
	}
	d = update(d.Version, "archived", "审计食谱改名")
	if lastAction() != "recipe_archived" || count() != 5 {
		t.Fatalf("归档审计错误: %s", lastAction())
	}
	d = update(d.Version, "draft", "审计食谱改名")
	if lastAction() != "recipe_restored" || count() != 6 {
		t.Fatalf("恢复审计错误: %s", lastAction())
	}
	// details 仅含变更字段与前后版本/状态，不含完整步骤。
	var details string
	if err := s.DB.QueryRow("SELECT details_json FROM resource_audit_logs WHERE resource_type='recipe' AND resource_id=? ORDER BY id DESC LIMIT 1", pub.ID).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(details, "第一步") {
		t.Fatalf("审计泄漏步骤内容: %s", details)
	}
	for _, want := range []string{`"version_before"`, `"version_after"`, `"status_before"`, `"status_after"`, `"fields"`} {
		if !strings.Contains(details, want) {
			t.Fatalf("审计缺字段 %s: %s", want, details)
		}
	}
}
