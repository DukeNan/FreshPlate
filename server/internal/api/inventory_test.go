package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 库存模块 API/安全测试（INV-API-001～037、INV-SEC-001～014）。
// 复用 api_test.go 的 fixture/browser/status/data/code 辅助与 JWT 登录流程；
// 夹具日期基准为 2026-10-19（UTC）。

// inventoryFixture 构造两个已登录普通用户（alice、bobby）。
func inventoryFixture(t *testing.T) (*fixture, *browser, *browser) {
	t.Helper()
	f := newFixture(t)
	alice, bobby := f.browser(), f.browser()
	alice.register("alice_01")
	bobby.register("bobby_02")
	alice.login("alice_01", testPassword)
	bobby.login("bobby_02", testPassword)
	return f, alice, bobby
}

// seedInventoryRow 直接插入库存批次（夹具）；expiresOn 空串表示 NULL，
// 返回自增 ID。
func seedInventoryRow(t *testing.T, f *fixture, userID, foodID, milli int64, unit, expiresOn, note string, version int64) int64 {
	t.Helper()
	var eo any
	if expiresOn != "" {
		eo = expiresOn
	}
	r, err := f.st.DB.Exec("INSERT INTO inventory_items(user_id,food_id,quantity_milli,unit,expires_on,note,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", userID, foodID, milli, unit, eo, note, version, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// invPage 是列表响应 data 的解码目标。
type invPage struct {
	Items []inventoryItemOutput `json:"items"`
	Total int                   `json:"total"`
}

// invBody 按 JSON 序组装创建请求体（不经过浮点，数量恒为字符串）。
func invBody(foodID int64, quantity, unit, expiresOn, note string) string {
	eo := "null"
	if expiresOn != "" {
		eo = fmt.Sprintf("%q", expiresOn)
	}
	return fmt.Sprintf(`{"food_id":%d,"quantity":%q,"unit":%q,"expires_on":%s,"note":%q}`, foodID, quantity, unit, eo, note)
}

// INV-API-001：创建成功 201 与响应形状。
func TestInvAPI001_CreateReturnsDetail(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	w := alice.request("POST", "/api/inventory-items", invBody(foodID, "6", "piece", "2026-10-25", "第一批"), nil)
	status(t, w, 201)
	var got inventoryItemOutput
	data(t, w, &got)
	if got.ID <= 0 || got.Food.ID != foodID || got.Food.Name != "鸡胸肉" || got.Food.Status != "active" {
		t.Fatalf("food 关联错误: %+v", got)
	}
	if got.Quantity != "6" || got.Unit != "piece" || got.ExpiresOn == nil || *got.ExpiresOn != "2026-10-25" || got.Note != "第一批" {
		t.Fatalf("字段回显错误: %+v", got)
	}
	if got.Version != 1 || got.ExpiryState != "fresh" || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("版本/派生状态/时间错误: %+v", got)
	}
}

// INV-API-002/003/009：默认分页、空列表与分页边界。
func TestInvAPI002_003_009_ListPagingEmptyAndBounds(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	// 空列表是正常状态。
	w := alice.request("GET", "/api/inventory-items", "", nil)
	status(t, w, 200)
	var p invPage
	data(t, w, &p)
	if p.Items == nil || len(p.Items) != 0 || p.Total != 0 {
		t.Fatalf("空列表应为 items=[]: %+v", p)
	}
	// 25 个批次：默认 page=1、page_size=20，id DESC。
	for i := 0; i < 25; i++ {
		seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", fmt.Sprintf("批次%d", i), 1)
	}
	w = alice.request("GET", "/api/inventory-items", "", nil)
	status(t, w, 200)
	data(t, w, &p)
	if p.Total != 25 || len(p.Items) != 20 {
		t.Fatalf("默认分页错误: total=%d len=%d", p.Total, len(p.Items))
	}
	if p.Items[0].ID <= p.Items[1].ID {
		t.Fatal("应按 id DESC 排序")
	}
	// 分页边界。
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"page=0", "?page=0", 400},
		{"page=-1", "?page=-1", 400},
		{"page=abc", "?page=abc", 400},
		{"page_size=0", "?page_size=0", 400},
		{"page_size=101", "?page_size=101", 400},
		{"page_size=100", "?page_size=100", 200},
	} {
		w := alice.request("GET", "/api/inventory-items"+tc.query, "", nil)
		status(t, w, tc.want)
		if tc.want == 400 {
			code(t, w, "INVALID_ARGUMENT")
		}
	}
}

// INV-API-004/005/006/008：q 字面子串与码点、food_id、expiry_state 校验。
func TestInvAPI004_005_006_008_ListFilters(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	chicken := seedFoodRow(t, f, "鸡胸肉", "active")
	leg := seedFoodRow(t, f, "鸡腿肉", "active")
	a1 := seedInventoryRow(t, f, alice.id, chicken, 1000, "g", "2026-10-25", "", 1)
	seedInventoryRow(t, f, alice.id, leg, 1000, "g", "2026-10-25", "", 1)
	// q 名称字面子串。
	w := alice.request("GET", "/api/inventory-items?q=%E9%B8%A1%E8%83%B8", "", nil)
	status(t, w, 200)
	var p invPage
	data(t, w, &p)
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].ID != a1 {
		t.Fatalf("q 应只命中鸡胸肉批次: %+v", p)
	}
	// q 码点边界：100 码点通过；101 码点拒绝；101 字节 100 码点内通过。
	w = alice.request("GET", "/api/inventory-items?q="+strings.Repeat("%E5%A5%BD", 100), "", nil)
	status(t, w, 200)
	w = alice.request("GET", "/api/inventory-items?q="+strings.Repeat("%E5%A5%BD", 101), "", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	w = alice.request("GET", "/api/inventory-items?q="+strings.Repeat("%F0%9F%8D%96", 25)+"aaaa", "", nil) // 25 emoji+4a=104 字节 29 码点
	status(t, w, 200)
	// food_id 筛选：存在→200；无批次→200 items=[]；非法→400。
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items?food_id=%d", chicken), "", nil)
	status(t, w, 200)
	data(t, w, &p)
	if p.Total != 1 || p.Items[0].ID != a1 {
		t.Fatalf("food_id 应命中对应批次: %+v", p)
	}
	w = alice.request("GET", "/api/inventory-items?food_id=999999", "", nil)
	status(t, w, 200)
	data(t, w, &p)
	if p.Total != 0 || p.Items == nil || len(p.Items) != 0 {
		t.Fatalf("无批次食材应返回空列表: %+v", p)
	}
	for _, bad := range []string{"0", "-1", "abc"} {
		w = alice.request("GET", "/api/inventory-items?food_id="+bad, "", nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// expiry_state 非法值。
	w = alice.request("GET", "/api/inventory-items?expiry_state=stale", "", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
}

// INV-API-007：五状态筛选，数量 0 只匹配 depleted。
func TestInvAPI007_ExpiryStateFilters(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	ids := map[string]int64{}
	ids["expired"] = seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-18", "", 1)
	ids["expiring"] = seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-19", "", 1)
	ids["fresh"] = seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-23", "", 1)
	ids["unknown"] = seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", "", 1)
	ids["depleted"] = seedInventoryRow(t, f, alice.id, foodID, 0, "g", "2026-10-18", "", 1)
	for _, state := range []string{"depleted", "expired", "expiring", "fresh", "unknown"} {
		w := alice.request("GET", "/api/inventory-items?expiry_state="+state, "", nil)
		status(t, w, 200)
		var p invPage
		data(t, w, &p)
		if p.Total != 1 || len(p.Items) != 1 || p.Items[0].ID != ids[state] {
			t.Fatalf("状态 %s 应只命中对应批次: %+v", state, p)
		}
		if p.Items[0].ExpiryState != state {
			t.Fatalf("派生状态应与筛选一致: %s", p.Items[0].ExpiryState)
		}
	}
	// 3 天/4 天边界：today+3 属 expiring，today+4 属 fresh。
	plus3 := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-22", "", 1)
	plus4 := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-23", "", 1)
	w := alice.request("GET", "/api/inventory-items?expiry_state=expiring", "", nil)
	status(t, w, 200)
	var p invPage
	data(t, w, &p)
	if p.Total != 2 {
		t.Fatalf("expiring 应含今日与+3: %+v", p)
	}
	found := map[int64]bool{}
	for _, it := range p.Items {
		found[it.ID] = true
	}
	if !found[ids["expiring"]] || !found[plus3] || found[plus4] {
		t.Fatalf("expiring 边界错误: %v", found)
	}
}

// INV-API-010/023/032/033/037：详情、404 不可区分、停用可读、
// 响应形状不泄露内部字段、数量规范字符串、no-store。
func TestInvAPI010_023_032_033_DetailShapeAndNotFound(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	live := seedInventoryRow(t, f, alice.id, foodID, 1250, "g", "", "", 3)
	deleted := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-18", "", 2)
	if _, err := f.st.DB.Exec("UPDATE inventory_items SET deleted_at=1 WHERE id=?", deleted); err != nil {
		t.Fatal(err)
	}
	// 详情：数量 1250 → "1.25"，expires_on 显式 null，无内部字段。
	w := alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", live), "", nil)
	status(t, w, 200)
	var got inventoryItemOutput
	data(t, w, &got)
	if got.Quantity != "1.25" || got.Version != 3 || got.ExpiresOn != nil || got.ExpiryState != "unknown" {
		t.Fatalf("详情错误: %+v", got)
	}
	raw := rawData(t, w)
	keys := objectKeys(t, raw)
	wantKeys := map[string]bool{"id": true, "food": true, "quantity": true, "unit": true, "expires_on": true, "note": true, "version": true, "expiry_state": true, "created_at": true, "updated_at": true}
	if len(keys) != len(wantKeys) {
		t.Fatalf("DTO 字段集合错误: %v", keys)
	}
	for k := range wantKeys {
		if !keys[k] {
			t.Fatalf("缺少字段 %s: %v", k, keys)
		}
	}
	for _, leak := range []string{"user_id", "quantity_milli", "deleted_at"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Fatalf("泄露内部字段 %s: %s", leak, w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), `"expires_on":null`) {
		t.Fatal("expires_on=null 应显式输出")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("应包含 Cache-Control: no-store")
	}
	// 已删除与不存在均 404。
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", deleted), "", nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	w = alice.request("GET", "/api/inventory-items/999999", "", nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	// 未提供的聚合路径同样 404（INV-API-036）。
	w = alice.request("GET", "/api/inventory-items/summary", "", nil)
	status(t, w, 404)
}

// INV-API-011/012/013/014/015/016：PUT 全量替换契约。
func TestInvAPI011_016_UpdateContract(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 6000, "piece", "2026-10-25", "第一批", 1)
	// 全量替换成功，version 递增。
	w := alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"4.5","expires_on":"2026-10-25","note":"已用掉一部分","version":1}`, nil)
	status(t, w, 200)
	var got inventoryItemOutput
	data(t, w, &got)
	if got.Version != 2 || got.Quantity != "4.5" || got.Note != "已用掉一部分" {
		t.Fatalf("替换结果错误: %+v", got)
	}
	// 缺任一可变字段或 version 均 400。
	for _, body := range []string{
		`{"expires_on":null,"note":"","version":2}`,
		`{"quantity":"1","note":"","version":2}`,
		`{"quantity":"1","expires_on":null,"version":2}`,
		`{"quantity":"1","expires_on":null,"note":""}`,
	} {
		w := alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), body, nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// 携带 food_id/unit 拒绝（不可修改）。
	for _, extra := range []string{`,"food_id":2`, `,"unit":"g"`} {
		w := alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), fmt.Sprintf(`{"quantity":"1","expires_on":null,"note":"","version":2%s}`, extra), nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// 版本冲突：以 version=1 提交（当前已是 2）。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"9","expires_on":null,"note":"","version":1}`, nil)
	status(t, w, 409)
	code(t, w, "VERSION_CONFLICT")
	// 数量为 0 表示用完：depleted。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"0","expires_on":null,"note":"","version":2}`, nil)
	status(t, w, 200)
	data(t, w, &got)
	if got.Quantity != "0" || got.ExpiryState != "depleted" {
		t.Fatalf("设零应 depleted: %+v", got)
	}
	// 不存在的 id → 404。
	w = alice.request("PUT", "/api/inventory-items/999999", `{"quantity":"1","expires_on":null,"note":"","version":1}`, nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
}

// INV-API-017/018/019/020：DELETE 契约。
func TestInvAPI017_020_DeleteContract(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-25", "", 2)
	// 首次删除成功 204 空体，随后 GET 404。
	w := alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), `{"version":2}`, nil)
	status(t, w, 204)
	if w.Body.Len() != 0 {
		t.Fatal("204 应无响应体")
	}
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 404)
	// 二次删除 404（不承诺在旧 version 下返回成功）。
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), `{"version":3}`, nil)
	status(t, w, 404)
	// 版本冲突：记录未删除。
	id2 := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-25", "", 3)
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id2), `{"version":2}`, nil)
	status(t, w, 409)
	code(t, w, "VERSION_CONFLICT")
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", id2), "", nil)
	status(t, w, 200)
	// version 非法值全部 400。
	for _, body := range []string{`{}`, `{"version":null}`, `{"version":0}`, `{"version":-1}`, `{"version":1.5}`, `{"version":"2"}`} {
		w := alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id2), body, nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
}

// INV-API-021/022/023/034：食材关联与已过期日期可录入。
func TestInvAPI021_022_034_FoodAssociation(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	disabledFood := seedFoodRow(t, f, "停用食材", "disabled")
	// 引用不存在或 disabled 食材 → 400 FOOD_UNAVAILABLE。
	w := alice.request("POST", "/api/inventory-items", invBody(999999, "1", "g", "", ""), nil)
	status(t, w, 400)
	code(t, w, "FOOD_UNAVAILABLE")
	w = alice.request("POST", "/api/inventory-items", invBody(disabledFood, "1", "g", "", ""), nil)
	status(t, w, 400)
	code(t, w, "FOOD_UNAVAILABLE")
	// 已过期日期可录入。
	w = alice.request("POST", "/api/inventory-items", invBody(foodID, "2", "g", "2026-10-01", "过期批次"), nil)
	status(t, w, 201)
	var got inventoryItemOutput
	data(t, w, &got)
	if got.ExpiryState != "expired" {
		t.Fatalf("过去日期应 expired: %+v", got)
	}
	// 停用食材的既有批次：减量可、增量拒、读取保留。
	id := seedInventoryRow(t, f, alice.id, disabledFood, 1000, "g", "2026-10-25", "", 1)
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"2","expires_on":"2026-10-25","note":"","version":1}`, nil)
	status(t, w, 409)
	code(t, w, "FOOD_DISABLED")
	// 减量。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"0.5","expires_on":"2026-10-25","note":"","version":1}`, nil)
	status(t, w, 200)
	data(t, w, &got)
	if got.Version != 2 || got.Quantity != "0.5" {
		t.Fatalf("减量应成功: %+v", got)
	}
	// 设零。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"0","expires_on":"2026-10-25","note":"","version":2}`, nil)
	status(t, w, 200)
	// 仅改备注。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"0","expires_on":"2026-10-25","note":"只用备注","version":3}`, nil)
	status(t, w, 200)
	// 仅改到期日。
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"0","expires_on":"2026-11-01","note":"只用备注","version":4}`, nil)
	status(t, w, 200)
	// 详情与列表保留停用标记。
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 200)
	data(t, w, &got)
	if got.Food.Name != "停用食材" || got.Food.Status != "disabled" {
		t.Fatalf("停用标记丢失: %+v", got)
	}
	w = alice.request("GET", "/api/inventory-items", "", nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status":"disabled"`) {
		t.Fatal("列表应保留停用标记")
	}
}

// INV-API-024/025/026/027/028/029：请求体与媒体类型校验。
func TestInvAPI024_029_RequestValidation(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	// quantity 数字 JSON / null 拒绝。
	w := alice.request("POST", "/api/inventory-items", `{"food_id":1,"quantity":250,"unit":"g","note":""}`, nil)
	status(t, w, 400)
	w = alice.request("POST", "/api/inventory-items", fmt.Sprintf(`{"food_id":%d,"quantity":null,"unit":"g","note":""}`, foodID), nil)
	status(t, w, 400)
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", "", 1)), `{"quantity":null,"expires_on":null,"note":"","version":1}`, nil)
	status(t, w, 400)
	// Content-Type 校验。
	w = alice.request("POST", "/api/inventory-items", invBody(foodID, "1", "g", "", ""), map[string]string{"Content-Type": "text/plain"})
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 64KiB 上限（超长 note）。
	w = alice.request("POST", "/api/inventory-items", invBody(foodID, "1", "g", "", strings.Repeat("a", 70000)), nil)
	status(t, w, 400)
	// 非 UTF-8 body。
	w = alice.request("POST", "/api/inventory-items", `{"food_id":1,"quantity":"1","unit":"g","note":"`+string([]byte{255})+`"}`, nil)
	status(t, w, 400)
	// 未知字段拒绝。
	w = alice.request("POST", "/api/inventory-items", fmt.Sprintf(`{"food_id":%d,"quantity":"1","unit":"g","note":"","foo":1}`, foodID), nil)
	status(t, w, 400)
	// 尾随 JSON 拒绝。
	w = alice.request("POST", "/api/inventory-items", invBody(foodID, "1", "g", "", "")+" {}", nil)
	status(t, w, 400)
}

// INV-API-030/031：未登录、强制改密与身份断言。
func TestInvAPI030_031_AuthAndIdentity(t *testing.T) {
	f, alice, bobby := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	// 未登录 401。
	anon := f.browser()
	anon.token()
	w := anon.request("GET", "/api/inventory-items", "", nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
	// 强制改密用户 403 PASSWORD_CHANGE_REQUIRED（沿用既有中间件行为）。
	if _, err := f.svc.InitAdmin(context.Background(), "root_admin", testPassword); err != nil {
		t.Fatal(err)
	}
	admin := f.browser()
	admin.login("root_admin", testPassword)
	target := f.browser()
	target.register("temp_user")
	target.login("temp_user", testPassword) // register 不返回 id，先登录取得
	w = admin.request("POST", fmt.Sprintf("/api/admin/users/%d/reset-password", target.id), `{"temporary_password":"Temp-Pass-2026!"}`, nil)
	status(t, w, 204)
	target.login("temp_user", "Temp-Pass-2026!")
	w = target.request("GET", "/api/inventory-items", "", nil)
	status(t, w, 403)
	code(t, w, "PASSWORD_CHANGE_REQUIRED")
	// 身份断言不一致：X-Expected-User-ID=B 的 id → 409（写路径）。
	w = alice.request("POST", "/api/inventory-items", invBody(foodID, "1", "g", "", ""), map[string]string{"X-Expected-User-ID": fmt.Sprint(bobby.id)})
	status(t, w, 409)
	code(t, w, "IDENTITY_CHANGED")
}

// INV-API-035：列表筛选与响应派生共用同一服务端 today。
func TestInvAPI035_ListAndDerivationShareToday(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-19", "", 1)
	// UTC 日界前：今日到期 → expiring。
	f.now = time.Date(2026, 10, 19, 23, 59, 0, 0, time.UTC)
	alice.login("alice_01", testPassword) // 时钟前移超过 15 分钟，重新登录刷新 JWT
	w := alice.request("GET", "/api/inventory-items?expiry_state=expiring", "", nil)
	status(t, w, 200)
	var p invPage
	data(t, w, &p)
	if p.Total != 1 || p.Items[0].ID != id || p.Items[0].ExpiryState != "expiring" {
		t.Fatalf("日界前应 expiring: %+v", p)
	}
	// UTC 日界后：同一批次变 expired，且不再命中 expiring 筛选。
	f.now = time.Date(2026, 10, 20, 0, 1, 0, 0, time.UTC)
	alice.login("alice_01", testPassword) // 再次刷新 JWT
	w = alice.request("GET", "/api/inventory-items?expiry_state=expiring", "", nil)
	status(t, w, 200)
	data(t, w, &p)
	if p.Total != 0 {
		t.Fatalf("日界后不应命中 expiring: %+v", p)
	}
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 200)
	var got inventoryItemOutput
	data(t, w, &got)
	if got.ExpiryState != "expired" {
		t.Fatalf("日界后详情应 expired: %+v", got)
	}
}

// INV-SEC-001/002/003：A 不能读取/修改/删除 B 的批次。
func TestInvSEC001_002_003_CrossUserDenied(t *testing.T) {
	f, alice, bobby := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-25", "", 2)
	// 读。
	w := bobby.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	// 改：合法 body 与 version 也 404，且记录未变。
	w = bobby.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"999","expires_on":null,"note":"越权","version":2}`, nil)
	status(t, w, 404)
	var milli int64
	var note string
	if err := f.st.DB.QueryRow("SELECT quantity_milli,note FROM inventory_items WHERE id=?", id).Scan(&milli, &note); err != nil || milli != 1000 || note != "" {
		t.Fatalf("越权编辑不得生效: %d %q %v", milli, note, err)
	}
	// 删：version 正确也 404，批次未删除。
	w = bobby.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), `{"version":2}`, nil)
	status(t, w, 404)
	var del *int64
	if err := f.st.DB.QueryRow("SELECT deleted_at FROM inventory_items WHERE id=?", id).Scan(&del); err != nil || del != nil {
		t.Fatalf("越权删除不得生效: %v %v", del, err)
	}
}

// INV-SEC-004：管理员也不能访问他人批次。
func TestInvSEC004_AdminIsolation(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	if _, err := f.svc.InitAdmin(context.Background(), "root_admin", testPassword); err != nil {
		t.Fatal(err)
	}
	admin := f.browser()
	admin.login("root_admin", testPassword)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-25", "", 1)
	w := admin.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 404)
	w = admin.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), `{"quantity":"999","expires_on":null,"note":"","version":1}`, nil)
	status(t, w, 404)
	w = admin.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), `{"version":1}`, nil)
	status(t, w, 404)
}

// INV-SEC-005：列表只含本人数据（遍历筛选与分页组合）。
func TestInvSEC005_ListOnlyOwnData(t *testing.T) {
	f, alice, bobby := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	a1 := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "2026-10-19", "", 1)
	a2 := seedInventoryRow(t, f, alice.id, foodID, 0, "g", "", "", 1)
	bobID := seedInventoryRow(t, f, bobby.id, foodID, 1000, "g", "2026-10-18", "", 1)
	own := map[int64]bool{a1: true, a2: true}
	for _, query := range []string{"", "?expiry_state=depleted", "?expiry_state=expired", "?expiry_state=expiring", "?expiry_state=fresh", "?expiry_state=unknown", "?page=1&page_size=100", fmt.Sprintf("?food_id=%d", foodID)} {
		w := alice.request("GET", "/api/inventory-items"+query, "", nil)
		status(t, w, 200)
		var p invPage
		data(t, w, &p)
		for _, it := range p.Items {
			if it.ID == bobID || !own[it.ID] {
				t.Fatalf("列表 %s 返回了非本人批次: %+v", query, it)
			}
		}
	}
}

// INV-SEC-006/007/008：伪造 user_id/food_id/unit/服务端字段一律 400。
func TestInvSEC006_007_008_ForgedFieldsRejected(t *testing.T) {
	f, alice, bobby := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", "", 1)
	base := invBody(foodID, "1", "g", "", "")
	// POST 伪造 user_id。
	w := alice.request("POST", "/api/inventory-items", base[:len(base)-1]+fmt.Sprintf(`,"user_id":%d}`, bobby.id), nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// POST 伪造服务端字段。
	for _, field := range []string{`"deleted_at":0`, `"version":5`, `"created_at":0`, `"updated_at":0`, `"expiry_state":"fresh"`} {
		w := alice.request("POST", "/api/inventory-items", base[:len(base)-1]+","+field+"}", nil)
		status(t, w, 400)
	}
	// PUT 伪造 food_id/unit 绝不生效。
	for _, field := range []string{fmt.Sprintf(`"food_id":%d`, foodID), `"unit":"kg"`} {
		w := alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", id), fmt.Sprintf(`{"quantity":"1","expires_on":null,"note":"","version":1,%s}`, field), nil)
		status(t, w, 400)
	}
	var food int64
	var unit string
	if err := f.st.DB.QueryRow("SELECT food_id,unit FROM inventory_items WHERE id=?", id).Scan(&food, &unit); err != nil || food != foodID || unit != "g" {
		t.Fatalf("伪造字段不得生效: %d %s %v", food, unit, err)
	}
}

// INV-SEC-010/011/012：DELETE 版本守卫——丢 body 只能拒绝、
// version 校验先于存在性检查、无替代通道。
func TestInvSEC010_011_012_DeleteVersionGuard(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	id := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", "", 1)
	// 丢 body（模拟代理丢弃）：400 且批次仍可查。
	w := alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	w = alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", id), "", nil)
	status(t, w, 200)
	// 不存在 id + 非法 version → 400 而非 404（先校验 version）。
	for _, body := range []string{`{}`, `{"version":null}`, `{"version":0}`, `{"version":-1}`, `{"version":1.5}`} {
		w := alice.request("DELETE", "/api/inventory-items/999999", body, nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// 查询参数与 If-Match 不构成替代通道。
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d?version=1", id), "", nil)
	status(t, w, 400)
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), "", map[string]string{"If-Match": "1"})
	status(t, w, 400)
	// 正确提交仍可删除，证明只是拒绝替代通道而非功能损坏。
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", id), `{"version":1}`, nil)
	status(t, w, 204)
}

// INV-SEC-013：404 不可区分存在性（他人/不存在/已删除错误体一致）。
func TestInvSEC013_Undistinguishable404(t *testing.T) {
	f, alice, bobby := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	bobBatch := seedInventoryRow(t, f, bobby.id, foodID, 1000, "g", "", "", 1)
	deleted := seedInventoryRow(t, f, alice.id, foodID, 1000, "g", "", "", 1)
	if _, err := f.st.DB.Exec("UPDATE inventory_items SET deleted_at=1 WHERE id=?", deleted); err != nil {
		t.Fatal(err)
	}
	w1 := alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", bobBatch), "", nil)
	w2 := alice.request("GET", "/api/inventory-items/999999", "", nil)
	w3 := alice.request("GET", fmt.Sprintf("/api/inventory-items/%d", deleted), "", nil)
	for i, w := range []*httptest.ResponseRecorder{w1, w2, w3} {
		if w.Code != 404 {
			t.Fatalf("响应 %d 状态应为 404: %d", i+1, w.Code)
		}
		if w.Body.String() != w1.Body.String() {
			t.Fatalf("响应 %d 错误体不一致: %s vs %s", i+1, w.Body.String(), w1.Body.String())
		}
	}
}

// INV-SEC-014：本人库存不写入管理员资源审计。
func TestInvSEC014_NoResourceAudit(t *testing.T) {
	f, alice, _ := inventoryFixture(t)
	foodID := seedFoodRow(t, f, "鸡胸肉", "active")
	w := alice.request("POST", "/api/inventory-items", invBody(foodID, "6", "piece", "2026-10-25", "私人备注"), nil)
	status(t, w, 201)
	var got inventoryItemOutput
	data(t, w, &got)
	w = alice.request("PUT", fmt.Sprintf("/api/inventory-items/%d", got.ID), `{"quantity":"4.5","expires_on":"2026-10-25","note":"改","version":1}`, nil)
	status(t, w, 200)
	w = alice.request("DELETE", fmt.Sprintf("/api/inventory-items/%d", got.ID), `{"version":2}`, nil)
	status(t, w, 204)
	if n := auditCount(t, f); n != 0 {
		t.Fatalf("库存操作不得写资源审计: %d", n)
	}
}
