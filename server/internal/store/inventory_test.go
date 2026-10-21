package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
)

// 库存模块存储层测试（INV-STORE-001～023）：默认值、不合并、CHECK、
// 外键、逻辑删除、版本竞争、会话撤销、列表只读事务、归属与五状态筛选、
// 停用只减与并发停用。写路径全部经 WithImmediate → checkSession →
// 条件更新 → 影响行数校验。

// isCode 判断错误是否为指定 AppError 错误码。
func isCode(err error, want string) bool {
	var e *AppError
	return errors.As(err, &e) && e.Code == want
}

// sptrStr 返回字符串指针（到期日夹具）。
func sptrStr(s string) *string { return &s }

// invFood 插入一条食材行（夹具），返回自增 ID。
func invFood(t *testing.T, s *Store, name, status string) int64 {
	t.Helper()
	r, err := s.DB.Exec("INSERT INTO foods(name,status,created_at,updated_at) VALUES(?,?,1,1)", name, status)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// invSeed 直接插入库存批次（夹具）；expiresOn 空串表示 NULL，返回自增 ID。
func invSeed(t *testing.T, s *Store, userID, foodID, milli int64, unit, expiresOn, note string, version int64) int64 {
	t.Helper()
	var eo any
	if expiresOn != "" {
		eo = expiresOn
	}
	r, err := s.DB.Exec("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,expires_on,note,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", userID, foodID, milli, unit, eo, note, version, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// invRow 读取批次的 (quantity_milli, version, updated_at, deleted_at)。
func invRow(t *testing.T, s *Store, id int64) (int64, int64, int64, *int64) {
	t.Helper()
	var milli, version, updated int64
	var deleted *int64
	if err := s.DB.QueryRow("SELECT quantity_milli,version,updated_at,deleted_at FROM inventory_items WHERE id=?", id).Scan(&milli, &version, &updated, &deleted); err != nil {
		t.Fatal(err)
	}
	return milli, version, updated, deleted
}

// INV-STORE-001/002：创建默认值与多批次不合并。
func TestINVSTORE001_002_CreateDefaultsAndNoMerge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 250000, "g", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 || out.Note != "" || out.QuantityMilli != 250000 || out.Unit != "g" {
		t.Fatalf("默认值错误: %+v", out)
	}
	if out.FoodID != foodID || out.FoodName != "鸡胸肉" || out.FoodStatus != "active" || out.UserID != u.ID {
		t.Fatalf("关联列错误: %+v", out)
	}
	if !out.CreatedAt.Equal(testNow) || !out.UpdatedAt.Equal(testNow) {
		t.Fatalf("时间应为 UTC Unix 秒: %v %v", out.CreatedAt, out.UpdatedAt)
	}
	if out.ExpiresOn != nil {
		t.Fatalf("无日期应为 NULL: %v", out.ExpiresOn)
	}
	// 完全相同内容再次创建：两行独立保存，不合并（POST 不因相同内容合并）。
	second, err := s.CreateInventory(ctx, ss, foodID, 250000, "g", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == out.ID {
		t.Fatal("相同内容不应合并")
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM inventory_items WHERE user_id=? AND food_id=? AND deleted_at IS NULL", u.ID, foodID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("应有两行独立批次: %d %v", n, err)
	}
}

// INV-STORE-003/004/005/006：CHECK 约束（数量范围、单位枚举、版本）。
func TestINVSTORE003_004_005_006_CheckConstraints(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	foodID := invFood(t, s, "鸡胸肉", "active")
	mustFail := []struct {
		name string
		q    string
	}{
		{"负数量", fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,%d,-1,'g','',1,1,1)", u.ID, foodID)},
		{"超上限", fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,%d,1000000001,'g','',1,1,1)", u.ID, foodID)},
		{"非法单位", fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,%d,1,'kg','',1,1,1)", u.ID, foodID)},
		{"零版本", fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,%d,1,'g','',0,1,1)", u.ID, foodID)},
	}
	for _, tc := range mustFail {
		if _, err := s.DB.ExecContext(ctx, tc.q); err == nil {
			t.Fatalf("%s 应被 CHECK 拒绝", tc.name)
		}
	}
	for _, unit := range []string{"g", "ml", "piece"} {
		if _, err := s.DB.ExecContext(ctx, fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,%d,1,'%s','',1,1,1)", u.ID, foodID, unit)); err != nil {
			t.Fatalf("单位 %s 应可插入: %v", unit, err)
		}
	}
}

// INV-STORE-007/008/009：外键 RESTRICT。
func TestINVSTORE007_008_009_ForeignKeyRestrict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	foodID := invFood(t, s, "鸡胸肉", "active")
	// 引用不存在的食材。
	if _, err := s.DB.ExecContext(ctx, fmt.Sprintf("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(%d,999999,1,'g','',1,1,1)", u.ID)); err == nil {
		t.Fatal("悬空 food_id 应被外键拒绝")
	}
	ss := session(t, s, u)
	if _, err := s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow); err != nil {
		t.Fatal(err)
	}
	// 有库存的用户不能删除。
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM users WHERE id=?", u.ID); err == nil {
		t.Fatal("users 外键 RESTRICT 应拒绝删除")
	}
	// 被库存引用的食材不能删除。
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM foods WHERE id=?", foodID); err == nil {
		t.Fatal("foods 外键 RESTRICT 应拒绝删除")
	}
}

// INV-STORE-010/011：逻辑删除不返回，删除设 deleted_at 且 version+1。
func TestINVSTORE010_011_DeletedHiddenAndTombstone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteInventory(ctx, ss, out.ID, 1, testNow); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListInventory(ctx, u.ID, "", 0, "", 1, 20, "2026-10-19")
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("已删除项不应出现在列表: %d %v", total, err)
	}
	if _, err = s.InventoryByID(ctx, ss, out.ID, testNow); !isCode(err, "NOT_FOUND") {
		t.Fatalf("已删除项详情应 404: %v", err)
	}
	milli, version, _, deleted := invRow(t, s, out.ID)
	if milli != 1000 || version != 2 || deleted == nil {
		t.Fatalf("删除后应保留行且 version+1: milli=%d version=%d deleted=%v", milli, version, deleted)
	}
}

// INV-STORE-012/015/016：条件更新版本冲突回滚、业务失败不落库、
// 相同内容 PUT 不加版本。
func TestINVSTORE012_015_016_VersionConflictRollbackAndNoop(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 2000, "g", sptrStr("2026-10-25"), "原始", testNow)
	if err != nil {
		t.Fatal(err)
	}
	// 版本不匹配：影响行数=0 → VERSION_CONFLICT，记录未变。
	if _, err = s.UpdateInventory(ctx, ss, out.ID, 99, 3000, nil, "改", testNow); !isCode(err, "VERSION_CONFLICT") {
		t.Fatalf("应版本冲突: %v", err)
	}
	if milli, version, _, _ := invRow(t, s, out.ID); milli != 2000 || version != 1 {
		t.Fatalf("冲突编辑不应落库: milli=%d version=%d", milli, version)
	}
	// 相同内容 PUT：返回原详情，不加版本、不推进 updated_at。
	same, err := s.UpdateInventory(ctx, ss, out.ID, 1, 2000, sptrStr("2026-10-25"), "原始", testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != 1 || !same.UpdatedAt.Equal(testNow) {
		t.Fatalf("无变化 PUT 不应加版本: %+v", same)
	}
	// 停用后增量：业务失败整体回滚（INV-STORE-015）。
	if _, err = s.DB.Exec("UPDATE foods SET status='disabled' WHERE id=?", foodID); err != nil {
		t.Fatal(err)
	}
	_, _, beforeUpdated, _ := invRow(t, s, out.ID)
	if _, err = s.UpdateInventory(ctx, ss, out.ID, 1, 9999, sptrStr("2026-10-25"), "原始", testNow.Add(2*time.Hour)); !isCode(err, "FOOD_DISABLED") {
		t.Fatalf("停用增量应 FOOD_DISABLED: %v", err)
	}
	milli, version, updated, _ := invRow(t, s, out.ID)
	if milli != 2000 || version != 1 || updated != beforeUpdated {
		t.Fatalf("业务失败不应改变数量/版本/时间: %d %d %d", milli, version, updated)
	}
}

// INV-STORE-013：并发编辑仅一次成功，不得把旧数量覆盖到新数量。
func TestINVSTORE013_ConcurrentEditSingleWinner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, e := s.UpdateInventory(ctx, ss, out.ID, 1, int64(2000+i), nil, fmt.Sprintf("并发%d", i), testNow)
			errs <- e
		}(i)
	}
	winners := 0
	for i := 0; i < 2; i++ {
		if e := <-errs; e == nil {
			winners++
		} else if !isCode(e, "VERSION_CONFLICT") {
			t.Fatalf("竞争失败应版本冲突: %v", e)
		}
	}
	if winners != 1 {
		t.Fatalf("应恰有一次成功: %d", winners)
	}
	milli, version, _, _ := invRow(t, s, out.ID)
	if version != 2 || (milli != 2000 && milli != 2001) {
		t.Fatalf("最终状态错误: milli=%d version=%d", milli, version)
	}
}

// INV-STORE-014：事务内撤销身份有效——写事务开启后撤销会话，
// checkSession 复核失败，整体回滚无部分写入。
func TestINVSTORE014_SessionRevokedMidTransaction(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	err = s.WithImmediate(ctx, func(c *gorm.DB) error {
		previous := s.DB.Stats().WaitCount
		go func() {
			_, e := s.UpdateInventory(ctx, ss, out.ID, 1, 5000, nil, "撤销期间编辑", testNow)
			result <- e
		}()
		if err := waitForStoreWaiter(ctx, s, previous); err != nil {
			return err
		}
		// 外层事务内撤销身份：auth_version+1 使会话失效。
		e := c.WithContext(ctx).Exec("UPDATE users SET auth_version=auth_version+1 WHERE id=?", u.ID).Error
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := <-result; !isCode(e, "AUTH_REQUIRED") {
		t.Fatalf("事务内撤销应 AUTH_REQUIRED: %v", e)
	}
	milli, version, _, _ := invRow(t, s, out.ID)
	if milli != 1000 || version != 1 {
		t.Fatalf("撤销后编辑不得落库: milli=%d version=%d", milli, version)
	}
}

// INV-STORE-017/018：列表只读事务与 total 一致、id DESC、零数量项包含。
func TestINVSTORE017_018_ListPagingOrderAndZeroQuantity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	foodID := invFood(t, s, "鸡胸肉", "active")
	for i := 0; i < 25; i++ {
		milli := int64(1000)
		if i == 24 {
			milli = 0 // 零数量批次（id 最大）也必须出现在默认结果首行。
		}
		invSeed(t, s, u.ID, foodID, milli, "g", "", fmt.Sprintf("批次%d", i), 1)
	}
	items, total, err := s.ListInventory(ctx, u.ID, "", 0, "", 1, 20, "2026-10-19")
	if err != nil || total != 25 || len(items) != 20 {
		t.Fatalf("分页错误: total=%d len=%d %v", total, len(items), err)
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].ID <= items[i].ID {
			t.Fatal("应按 id DESC 排序")
		}
	}
	if items[0].QuantityMilli != 0 {
		t.Fatal("零数量批次应包含在默认结果首行（id 最大）")
	}
	second, total2, err := s.ListInventory(ctx, u.ID, "", 0, "", 2, 20, "2026-10-19")
	if err != nil || total2 != 25 || len(second) != 5 {
		t.Fatalf("第二页错误: total=%d len=%d %v", total2, len(second), err)
	}
}

// INV-STORE-019：所有查询限定归属与未删除；五状态分支互斥且
// 用完批次只进 depleted。
func TestINVSTORE019_OwnerScopeAndFiveStateFilters(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := user(t, s, "alice", "user")
	b := user(t, s, "bobby", "user")
	foodID := invFood(t, s, "鸡胸肉", "active")
	sb := session(t, s, b)
	// B 的批次：任何 A 的查询都不得出现。
	if _, err := s.CreateInventory(ctx, sb, foodID, 1000, "g", nil, "", testNow); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	ids["expired"] = invSeed(t, s, a.ID, foodID, 1000, "g", "2026-10-18", "", 1)
	ids["expiring_today"] = invSeed(t, s, a.ID, foodID, 1000, "g", "2026-10-19", "", 1)
	ids["expiring_plus3"] = invSeed(t, s, a.ID, foodID, 1000, "g", "2026-10-22", "", 1)
	ids["fresh"] = invSeed(t, s, a.ID, foodID, 1000, "g", "2026-10-23", "", 1)
	ids["unknown"] = invSeed(t, s, a.ID, foodID, 1000, "g", "", "", 1)
	ids["depleted"] = invSeed(t, s, a.ID, foodID, 0, "g", "2026-10-18", "", 1)
	// 已删除批次：任何状态与默认列表都不得命中。
	tombstone := invSeed(t, s, a.ID, foodID, 1000, "g", "2026-10-18", "已删除", 1)
	sa := session(t, s, a)
	if err := s.DeleteInventory(ctx, sa, tombstone, 1, testNow); err != nil {
		t.Fatal(err)
	}
	list := func(state string) map[int64]bool {
		t.Helper()
		items, _, err := s.ListInventory(ctx, a.ID, "", 0, state, 1, 100, "2026-10-19")
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]bool{}
		for _, it := range items {
			if it.UserID != a.ID {
				t.Fatalf("出现他人批次: %+v", it)
			}
			got[it.ID] = true
		}
		return got
	}
	if got := list(""); len(got) != 6 {
		t.Fatalf("默认应含 6 个未删除批次: %v", got)
	}
	if got := list("depleted"); len(got) != 1 || !got[ids["depleted"]] {
		t.Fatalf("depleted 只应命中数量零: %v", got)
	}
	if got := list("expired"); len(got) != 1 || !got[ids["expired"]] {
		t.Fatalf("expired 应只命中未删除过期行: %v", got)
	}
	if got := list("expiring"); len(got) != 2 || !got[ids["expiring_today"]] || !got[ids["expiring_plus3"]] {
		t.Fatalf("expiring 应含今日与+3: %v", got)
	}
	if got := list("fresh"); len(got) != 1 || !got[ids["fresh"]] {
		t.Fatalf("fresh 应只含 +4: %v", got)
	}
	if got := list("unknown"); len(got) != 1 || !got[ids["unknown"]] {
		t.Fatalf("unknown 应只含无日期: %v", got)
	}
	// 用完批次即使有到期日也只匹配 depleted（其余日期分支均要求 >0）。
	invSeed(t, s, a.ID, foodID, 0, "g", "2026-10-01", "零数量过期", 1)
	if got := list("expired"); len(got) != 1 {
		t.Fatalf("零数量批次不得进入 expired: %v", got)
	}
	if got := list("depleted"); len(got) != 2 {
		t.Fatalf("depleted 应含两个零数量批次: %v", got)
	}
}

// INV-STORE-020：编辑不触碰 food_id/unit。
func TestINVSTORE020_EditKeepsFoodAndUnit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	out, err := s.CreateInventory(ctx, ss, foodID, 1000, "ml", nil, "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.UpdateInventory(ctx, ss, out.ID, 1, 500, sptrStr("2026-10-25"), "改", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if edited.FoodID != foodID || edited.Unit != "ml" {
		t.Fatalf("编辑不得修改 food_id/unit: %+v", edited)
	}
	var dbFood int64
	var dbUnit string
	if err = s.DB.QueryRow("SELECT food_id,unit FROM inventory_items WHERE id=?", out.ID).Scan(&dbFood, &dbUnit); err != nil || dbFood != foodID || dbUnit != "ml" {
		t.Fatalf("库中 food_id/unit 不变: %d %s %v", dbFood, dbUnit, err)
	}
}

// INV-STORE-021：并发停用阻止新增——创建要么在停用前提交成功，
// 要么因事务内 food active 复核失败回滚。
func TestINVSTORE021_ConcurrentDisableBlocksCreate(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	foodID := invFood(t, s, "鸡胸肉", "active")
	// 场景一：外层事务先停用，随后执行的创建必须复核失败。
	result := make(chan error, 1)
	err := s.WithImmediate(ctx, func(c *gorm.DB) error {
		previous := s.DB.Stats().WaitCount
		go func() {
			_, e := s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow)
			result <- e
		}()
		if err := waitForStoreWaiter(ctx, s, previous); err != nil {
			return err
		}
		e := c.WithContext(ctx).Exec("UPDATE foods SET status='disabled' WHERE id=?", foodID).Error
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := <-result; !isCode(e, "FOOD_UNAVAILABLE") {
		t.Fatalf("停用后创建应 FOOD_UNAVAILABLE: %v", e)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM inventory_items WHERE food_id=?", foodID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("失败创建不得残留: %d %v", n, err)
	}
	// 场景二：恢复 active 后创建成功。
	if _, err = s.DB.Exec("UPDATE foods SET status='active' WHERE id=?", foodID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateInventory(ctx, ss, foodID, 1000, "g", nil, "", testNow); err != nil {
		t.Fatalf("恢复后创建应成功: %v", err)
	}
}

// INV-STORE-022：未知换算不阻止保存（原单位与数量是事实数据）。
func TestINVSTORE022_UnknownConversionUnitsAccepted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := user(t, s, "alice", "user")
	ss := session(t, s, u)
	// 夹具食材没有任何换算/营养信息。
	foodID := invFood(t, s, "神秘食材", "active")
	for _, unit := range []string{"g", "ml", "piece"} {
		if _, err := s.CreateInventory(ctx, ss, foodID, 1500, unit, nil, "", testNow); err != nil {
			t.Fatalf("单位 %s 应可保存: %v", unit, err)
		}
	}
}

// INV-STORE-023：两个索引存在。
func TestINVSTORE023_IndexesPresent(t *testing.T) {
	s := testStore(t)
	for _, index := range []string{"inventory_user", "inventory_user_expiry"} {
		var n int
		if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&n); err != nil || n != 1 {
			t.Fatalf("索引 %s 缺失: %d %v", index, n, err)
		}
	}
}
