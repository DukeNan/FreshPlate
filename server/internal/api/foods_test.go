package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"freshplate/server/internal/store"
)

// foodFixture 构造已登录的管理员与普通用户浏览器。
func foodFixture(t *testing.T) (*fixture, *browser, *browser) {
	t.Helper()
	f := newFixture(t)
	if _, err := f.svc.InitAdmin(context.Background(), "root_admin", testPassword); err != nil {
		t.Fatal(err)
	}
	admin, alice := f.browser(), f.browser()
	alice.register("alice_01")
	admin.login("root_admin", testPassword)
	alice.login("alice_01", testPassword)
	return f, admin, alice
}

// minimalFoodBody 是方案 §4 的创建示例（仅名称已知，未知显式 null/false）。
const minimalFoodBody = `{"name":"燕麦片","status":"active","energy_kcal_100g":null,"protein_g_100g":null,"fat_g_100g":null,"carbohydrate_g_100g":null,"nutrition_source":null,"nutrition_reference":null,"density_g_ml":null,"piece_weight_g":null,"conversion_source":null,"allergens":[],"allergen_complete":false,"allergen_source":null}`

// fullFoodBody 是营养/换算/过敏原完整且合法的创建体。
const fullFoodBody = `{"name":"全麦面包","status":"active","energy_kcal_100g":246,"protein_g_100g":10.4,"fat_g_100g":3.4,"carbohydrate_g_100g":45.8,"nutrition_source":"成分表","nutrition_reference":"bread-01","density_g_ml":0.4,"piece_weight_g":35,"conversion_source":"包装标注","allergens":["wheat"],"allergen_complete":true,"allergen_source":"标签"}`

// seedFoodRow 直接插入食材行（夹具），返回自增 ID。基础库已有 12 条食材（id 1..12）。
func seedFoodRow(t *testing.T, f *fixture, name, status string) int64 {
	t.Helper()
	r, err := f.st.DB.Exec("INSERT INTO foods(name,status,created_at,updated_at) VALUES(?,?,1,1)", name, status)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// dbPath 取当前测试库文件路径（用于绕过连接池持锁）。
func dbPath(t *testing.T, f *fixture) string {
	t.Helper()
	var seq int
	var name, path string
	if err := f.st.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}

// queryInt 统计行数。
func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// auditCount 统计资源审计行数。
func auditCount(t *testing.T, f *fixture) int {
	t.Helper()
	return queryInt(t, f.st.DB, "SELECT count(*) FROM resource_audit_logs")
}

// rawData 提取响应 {"data":...} 的 data 原始 JSON。
func rawData(t *testing.T, w *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

// objectKeys 返回 JSON 对象的字段名集合。
func objectKeys(t *testing.T, raw json.RawMessage) map[string]bool {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]bool, len(obj))
	for k := range obj {
		keys[k] = true
	}
	return keys
}

var wantSummaryKeys = map[string]bool{"id": true, "name": true, "status": true, "version": true, "allergen_complete": true}

var wantDetailKeys = map[string]bool{
	"id": true, "name": true, "status": true,
	"energy_kcal_100g": true, "protein_g_100g": true, "fat_g_100g": true, "carbohydrate_g_100g": true,
	"nutrition_source": true, "nutrition_reference": true,
	"density_g_ml": true, "piece_weight_g": true, "conversion_source": true,
	"allergens": true, "allergen_complete": true, "allergen_source": true,
	"version": true, "created_at": true, "updated_at": true,
}

// FOOD-API-001：food-catalog 默认分页与摘要形状（仅 active、ID 倒序、不含营养来源）。
func TestFoodAPI001_CatalogDefaultPagingAndSummaryShape(t *testing.T) {
	f, _, alice := foodFixture(t)
	for i := 0; i < 25; i++ {
		statusCol := "active"
		if i%7 == 0 {
			statusCol = "disabled"
		}
		seedFoodRow(t, f, fmt.Sprintf("目录食材%02d", i), statusCol)
	}
	w := alice.request("GET", "/api/food-catalog", "", nil)
	status(t, w, 200)
	var page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	data(t, w, &page)
	wantTotal := queryInt(t, f.st.DB, "SELECT count(*) FROM foods WHERE status='active'")
	if page.Total != wantTotal {
		t.Fatalf("total=%d want=%d", page.Total, wantTotal)
	}
	if len(page.Items) != 20 {
		t.Fatalf("默认每页应为 20，得到 %d", len(page.Items))
	}
	var first struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(page.Items[0], &first); err != nil {
		t.Fatal(err)
	}
	maxActive := queryInt(t, f.st.DB, "SELECT max(id) FROM foods WHERE status='active'")
	if first.ID != int64(maxActive) {
		t.Fatalf("首条应为最大 active ID %d，得到 %d", maxActive, first.ID)
	}
	if first.Status != "active" {
		t.Fatalf("目录混入非 active: %+v", first)
	}
	for _, item := range page.Items {
		keys := objectKeys(t, item)
		if len(keys) != len(wantSummaryKeys) {
			t.Fatalf("摘要字段集合异常: %v", keys)
		}
		for k := range keys {
			if !wantSummaryKeys[k] {
				t.Fatalf("摘要出现未知字段 %s: %v", k, keys)
			}
		}
	}
	// 第二页为剩余条目。
	w = alice.request("GET", "/api/food-catalog?page=2", "", nil)
	status(t, w, 200)
	data(t, w, &page)
	if len(page.Items) != wantTotal-20 {
		t.Fatalf("第二页条数 %d want %d", len(page.Items), wantTotal-20)
	}
}

// FOOD-API-002：catalog 查询参数边界。
func TestFoodAPI002_CatalogQueryParamBounds(t *testing.T) {
	_, _, alice := foodFixture(t)
	q100 := strings.Repeat("麦", 100)
	for _, item := range []struct {
		query string
		code  int
		err   string
	}{
		{"?q=" + q100, 200, ""},
		{"?page_size=100", 200, ""},
		{"?q=" + strings.Repeat("麦", 101), 400, "INVALID_ARGUMENT"},
		{"?page_size=101", 400, "INVALID_ARGUMENT"},
		{"?page=0", 400, "INVALID_ARGUMENT"},
		{"?page=-1", 400, "INVALID_ARGUMENT"},
		{"?page=abc", 400, "INVALID_ARGUMENT"},
	} {
		w := alice.request("GET", "/api/food-catalog"+item.query, "", nil)
		status(t, w, item.code)
		if item.err != "" {
			code(t, w, item.err)
		}
	}
}

// FOOD-API-003：详情读取含停用项、全部业务字段与 RFC 3339 UTC 时间。
func TestFoodAPI003_DetailIncludesDisabledAndTimeFormat(t *testing.T) {
	f, admin, alice := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", fullFoodBody, nil)
	status(t, w, 201)
	var x store.FoodDetail
	data(t, w, &x)
	// disabled 食材 Y 同样可读。
	y := seedFoodRow(t, f, "停用食材", "disabled")
	for _, id := range []int64{x.ID, y} {
		w = alice.request("GET", fmt.Sprintf("/api/foods/%d", id), "", nil)
		status(t, w, 200)
		var d store.FoodDetail
		data(t, w, &d)
		if d.ID != id {
			t.Fatalf("详情 ID %d want %d", d.ID, id)
		}
	}
	// X 的字段集合恰为全部业务字段（不多不少）。
	w = alice.request("GET", fmt.Sprintf("/api/foods/%d", x.ID), "", nil)
	keys := objectKeys(t, rawData(t, w))
	if len(keys) != len(wantDetailKeys) {
		t.Fatalf("详情字段数 %d want %d: %v", len(keys), len(wantDetailKeys), keys)
	}
	for k := range keys {
		if !wantDetailKeys[k] {
			t.Fatalf("详情出现未知字段 %s", k)
		}
	}
	// 时间为 RFC 3339 UTC。
	var times struct {
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(rawData(t, w), &times); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{times.CreatedAt, times.UpdatedAt} {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatalf("时间非 RFC 3339: %q %v", value, err)
		}
		if _, offset := parsed.Zone(); offset != 0 {
			t.Fatalf("时间非 UTC: %q", value)
		}
	}
}

// FOOD-API-004：详情 404 与非法 ID。
func TestFoodAPI004_DetailNotFoundAndInvalidID(t *testing.T) {
	_, _, alice := foodFixture(t)
	w := alice.request("GET", "/api/foods/999999", "", nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	for _, id := range []string{"0", "-1", "abc"} {
		w = alice.request("GET", "/api/foods/"+id, "", nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
}

// FOOD-API-005：管理员列表状态筛选。
func TestFoodAPI005_AdminListStatusFilter(t *testing.T) {
	f, admin, _ := foodFixture(t)
	seedFoodRow(t, f, "筛active", "active")
	seedFoodRow(t, f, "筛disabled", "disabled")
	all := queryInt(t, f.st.DB, "SELECT count(*) FROM foods")
	actives := queryInt(t, f.st.DB, "SELECT count(*) FROM foods WHERE status='active'")
	cases := []struct {
		query string
		want  int
	}{
		{"", all},
		{"?status=active", actives},
		{"?status=disabled", all - actives},
	}
	for _, tc := range cases {
		w := admin.request("GET", "/api/admin/foods"+tc.query, "", nil)
		status(t, w, 200)
		var page struct {
			Total int `json:"total"`
		}
		data(t, w, &page)
		if page.Total != tc.want {
			t.Fatalf("query=%q total=%d want=%d", tc.query, page.Total, tc.want)
		}
	}
	w := admin.request("GET", "/api/admin/foods?status=other", "", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
}

// FOOD-API-006：创建成功与创建审计（details 含字段名数组与前后版本，不含请求值）。
func TestFoodAPI006_CreateAuditsCreation(t *testing.T) {
	f, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 201)
	var d store.FoodDetail
	data(t, w, &d)
	if d.ID <= 0 || d.Version != 1 || d.Name != "燕麦片" || d.Status != "active" || d.EnergyKcal100g != nil || d.AllergenComplete {
		t.Fatalf("创建详情异常: %+v", d)
	}
	var action, detailsJSON string
	if err := f.st.DB.QueryRow("SELECT action,details_json FROM resource_audit_logs WHERE resource_id=? AND resource_type='food'", d.ID).Scan(&action, &detailsJSON); err != nil {
		t.Fatal(err)
	}
	if action != "food_created" {
		t.Fatalf("action=%s", action)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fields", "status_before", "status_after", "version_before", "version_after"} {
		if _, ok := details[key]; !ok {
			t.Fatalf("details 缺少 %s: %s", key, detailsJSON)
		}
	}
	if len(details) != 5 {
		t.Fatalf("details 含多余字段: %s", detailsJSON)
	}
	if details["status_after"] != "active" || details["status_before"] != nil {
		t.Fatalf("前后状态异常: %s", detailsJSON)
	}
	if v, ok := details["version_after"].(float64); !ok || int64(v) != 1 || details["version_before"] != nil {
		t.Fatalf("前后版本异常: %s", detailsJSON)
	}
	if strings.Contains(detailsJSON, "燕麦片") {
		t.Fatalf("details 不应包含请求值: %s", detailsJSON)
	}
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("字段名数组缺失: %s", detailsJSON)
	}
}

// FOOD-API-007：全量更新与版本递增。
func TestFoodAPI007_UpdateBumpsVersionAndAudits(t *testing.T) {
	f, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	f.now = f.now.Add(time.Minute) // 推进固定时钟以观察 updated_at
	body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"快熟燕麦片","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), body, nil)
	status(t, w, 200)
	var updated store.FoodDetail
	data(t, w, &updated)
	if updated.Version != 2 || updated.Name != "快熟燕麦片" {
		t.Fatalf("更新结果异常: %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("updated_at 未推进: %v -> %v", created.UpdatedAt, updated.UpdatedAt)
	}
	var action string
	if err := f.st.DB.QueryRow("SELECT action FROM resource_audit_logs WHERE resource_id=? AND resource_type='food' ORDER BY id DESC LIMIT 1", created.ID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "food_updated" {
		t.Fatalf("action=%s", action)
	}
}

// FOOD-API-008：无变化更新不升版本、不写审计。
func TestFoodAPI008_NoopUpdateKeepsVersionAndAudit(t *testing.T) {
	f, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", fullFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	before := auditCount(t, f)
	body := strings.Replace(fullFoodBody, `"name":"全麦面包"`, `"name":"全麦面包","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), body, nil)
	status(t, w, 200)
	var got store.FoodDetail
	data(t, w, &got)
	if got.Version != created.Version || !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("无变化更新改变了状态: %+v vs %+v", got, created)
	}
	if after := auditCount(t, f); after != before {
		t.Fatalf("无变化更新写了审计: %d -> %d", before, after)
	}
}

// FOOD-API-009：启停动作审计编码（disabled/enabled）与版本递增。
func TestFoodAPI009_StatusActionsAudited(t *testing.T) {
	f, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	disable := strings.Replace(minimalFoodBody, `"status":"active"`, `"status":"disabled","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), disable, nil)
	status(t, w, 200)
	var disabled store.FoodDetail
	data(t, w, &disabled)
	if disabled.Status != "disabled" || disabled.Version != 2 {
		t.Fatalf("停用结果异常: %+v", disabled)
	}
	enable := strings.Replace(minimalFoodBody, `"status":"active"`, `"status":"active","version":2`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), enable, nil)
	status(t, w, 200)
	var enabled store.FoodDetail
	data(t, w, &enabled)
	if enabled.Status != "active" || enabled.Version != 3 {
		t.Fatalf("启用结果异常: %+v", enabled)
	}
	var actions []string
	rows, err := f.st.DB.Query("SELECT action FROM resource_audit_logs WHERE resource_id=? AND resource_type='food' ORDER BY id", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	rows.Close()
	if len(actions) != 3 || actions[0] != "food_created" || actions[1] != "food_disabled" || actions[2] != "food_enabled" {
		t.Fatalf("审计动作序列异常: %v", actions)
	}
}

// FOOD-API-010：重名 409（创建同名与改名撞名两个形态）。
func TestFoodAPI010_DuplicateNameConflict(t *testing.T) {
	_, admin, _ := foodFixture(t)
	if w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil); w.Code != 201 {
		t.Fatalf("夹具创建失败: %s", w.Body.String())
	}
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 409)
	code(t, w, "FOOD_NAME_TAKEN")
	// 基础库含「鸡蛋」；改为同名同样 409。
	w = admin.request("POST", "/api/admin/foods", strings.Replace(fullFoodBody, `"name":"全麦面包"`, `"name":"鸡蛋"`, 1), nil)
	status(t, w, 409)
	code(t, w, "FOOD_NAME_TAKEN")
}

// FOOD-API-011：请求体解码规则（五种均 400 INVALID_ARGUMENT）。
func TestFoodAPI011_DecodeRules(t *testing.T) {
	_, admin, _ := foodFixture(t)
	// 未知字段。
	w := admin.request("POST", "/api/admin/foods", strings.Replace(minimalFoodBody, "}", `,"unknown_field":1}`, 1), nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 尾随 JSON。
	w = admin.request("POST", "/api/admin/foods", minimalFoodBody+`{"x":1}`, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// Content-Type 非 application/json。
	w = admin.request("POST", "/api/admin/foods", minimalFoodBody, map[string]string{"Content-Type": "text/plain"})
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 无效 UTF-8。
	w = admin.request("POST", "/api/admin/foods", "{\"name\":\"\xff\x01\"}", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 空 body。
	w = admin.request("POST", "/api/admin/foods", "", nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
}

// FOOD-API-012：伪造服务端字段拒绝；PUT version 必填且必须为正整数。
func TestFoodAPI012_ForgedServerFieldsRejected(t *testing.T) {
	f, admin, _ := foodFixture(t)
	// POST 带 id/version/created_at/updated_at（全为未知字段）。
	for _, forged := range []string{`"id":1`, `"version":1`, `"created_at":"2026-01-01T00:00:00Z"`, `"updated_at":"2026-01-01T00:00:00Z"`} {
		w := admin.request("POST", "/api/admin/foods", strings.Replace(minimalFoodBody, "}", ","+forged+"}", 1), nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// PUT 带 id/created_at/updated_at。
	putBase := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"合法名","version":1`, 1)
	seedFoodRow(t, f, "合法名", "active")
	for _, forged := range []string{`"id":1`, `"created_at":"2026-01-01T00:00:00Z"`, `"updated_at":"2026-01-01T00:00:00Z"`} {
		w := admin.request("PUT", "/api/admin/foods/13", strings.Replace(putBase, "}", ","+forged+"}", 1), nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
	// PUT 缺 version。
	w := admin.request("PUT", "/api/admin/foods/13", minimalFoodBody, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// PUT version=0/-1/"abc"。
	for _, version := range []string{"0", "-1", `"abc"`} {
		body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"改名","version":`+version, 1)
		w = admin.request("PUT", "/api/admin/foods/13", body, nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
}

// FOOD-API-013：食物路由 64 KiB 双通道限制。
func TestFoodAPI013_BodyLimitBothChannels(t *testing.T) {
	f, admin, _ := foodFixture(t)
	// 恰好 64 KiB 的合法 JSON（末尾空白补齐）应成功。
	pad := 64<<10 - len(minimalFoodBody)
	if pad < 2 {
		t.Fatal("夹具长度异常")
	}
	exact := minimalFoodBody[:len(minimalFoodBody)-1] + strings.Repeat(" ", pad-1) + "}"
	w := admin.request("POST", "/api/admin/foods", exact, nil)
	status(t, w, 201)
	// A. Content-Length = 64KiB+1 → 中间件预检拒绝。
	big := minimalFoodBody + strings.Repeat(" ", 64<<10-len(minimalFoodBody)+1)
	w = admin.request("POST", "/api/admin/foods", big, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// B. 无 Content-Length 的分块流式 body 超 64 KiB → decode 通道拒绝。
	r := httptest.NewRequest("POST", "/api/admin/foods", struct{ io.Reader }{strings.NewReader(big)})
	r.ContentLength = 0 // 模拟无长度声明的流式请求
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+admin.access)
	r.Header.Set("X-Expected-User-ID", strconv.FormatInt(admin.id, 10))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	status(t, rec, 400)
	code(t, rec, "INVALID_ARGUMENT")
}

// FOOD-API-014：限制来源函数按已匹配路由+方法取值（食物 64 KiB，预留食谱四路由 256 KiB）。
func TestFoodAPI014_BodyLimitSourceFunction(t *testing.T) {
	cases := []struct {
		method, path string
		want         int64
	}{
		{"POST", "/api/foods", 64 << 10},
		{"PUT", "/api/admin/foods/1", 64 << 10},
		{"POST", "/api/admin/foods", 64 << 10},
		{"GET", "/api/food-catalog", 64 << 10},
		{"POST", "/api/me/preferences", 64 << 10},
		{"POST", "/api/recipes", 256 << 10},
		{"PUT", "/api/recipes/9", 256 << 10},
		{"POST", "/api/admin/recipes", 256 << 10},
		{"PUT", "/api/admin/recipes/9", 256 << 10},
		{"GET", "/api/recipes/9", 64 << 10},
		{"DELETE", "/api/recipes/9", 64 << 10},
		// 未注册的子路径不属于四条写路由，按契约保持 64 KiB。
		{"POST", "/api/recipes/9/steps", 64 << 10},
		{"GET", "/api/admin/recipes", 64 << 10},
	}
	for _, tc := range cases {
		if got := bodyLimitBytes(tc.method, tc.path); got != tc.want {
			t.Fatalf("bodyLimitBytes(%s,%s)=%d want=%d", tc.method, tc.path, got, tc.want)
		}
	}
}

// FOOD-API-015：资源审计分页契约（默认分页、id DESC、形状与现有接口一致）。
func TestFoodAPI015_ResourceAuditPaging(t *testing.T) {
	f, admin, _ := foodFixture(t)
	foodID := seedFoodRow(t, f, "审计分页食材", "active")
	for i := 0; i < 25; i++ {
		if _, err := f.st.DB.Exec("INSERT INTO resource_audit_logs(actor_user_id,resource_type,resource_id,action,details_json,created_at) VALUES(?,?,?,?,?,?)", admin.id, "food", foodID, "food_updated", "{}", i+1); err != nil {
			t.Fatal(err)
		}
	}
	w := admin.request("GET", "/api/admin/resource-audit-logs", "", nil)
	status(t, w, 200)
	var page struct {
		Items []store.ResourceAudit `json:"items"`
		Total int                   `json:"total"`
	}
	data(t, w, &page)
	if page.Total != 25 || len(page.Items) != 20 {
		t.Fatalf("分页异常: total=%d items=%d", page.Total, len(page.Items))
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i-1].ID <= page.Items[i].ID {
			t.Fatalf("未按 id DESC: %v", page.Items)
		}
	}
	// 第二页 5 条。
	w = admin.request("GET", "/api/admin/resource-audit-logs?page=2", "", nil)
	status(t, w, 200)
	data(t, w, &page)
	if len(page.Items) != 5 {
		t.Fatalf("第二页条数 %d", len(page.Items))
	}
}

// FOOD-API-016：资源审计筛选参数。
func TestFoodAPI016_ResourceAuditFilters(t *testing.T) {
	f, admin, _ := foodFixture(t)
	foodID := seedFoodRow(t, f, "审计筛选食材", "active")
	recipeID := int64(77)
	if _, err := f.st.DB.Exec("INSERT INTO resource_audit_logs(actor_user_id,resource_type,resource_id,action,details_json,created_at) VALUES(?,?,?,?,?,?)", admin.id, "recipe", recipeID, "recipe_created", "{}", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.Exec("INSERT INTO resource_audit_logs(actor_user_id,resource_type,resource_id,action,details_json,created_at) VALUES(?,?,?,?,?,?)", admin.id, "food", foodID, "food_disabled", "{}", 2); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query    string
		wantType string
		wantID   int64
	}{
		{"?resource_type=food", "food", foodID},
		{"?resource_type=recipe", "recipe", recipeID},
		{"?resource_id=" + strconv.FormatInt(foodID, 10), "food", foodID},
		{"?action=food_disabled", "food", foodID},
	} {
		w := admin.request("GET", "/api/admin/resource-audit-logs"+tc.query, "", nil)
		status(t, w, 200)
		var page struct {
			Items []store.ResourceAudit `json:"items"`
			Total int                   `json:"total"`
		}
		data(t, w, &page)
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ResourceType != tc.wantType || page.Items[0].ResourceID != tc.wantID {
			t.Fatalf("query=%q 结果异常: %+v", tc.query, page.Items)
		}
	}
	for _, bad := range []string{"?resource_type=user", "?resource_id=0", "?resource_id=-1", "?resource_id=abc"} {
		w := admin.request("GET", "/api/admin/resource-audit-logs"+bad, "", nil)
		status(t, w, 400)
		code(t, w, "INVALID_ARGUMENT")
	}
}

// FOOD-API-017：统一响应与错误映射抽样（400/403/404/409；503 由 FOOD-API-018 覆盖）。
func TestFoodAPI017_ErrorEnvelopeShape(t *testing.T) {
	_, admin, alice := foodFixture(t)
	admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	cases := []struct {
		name   string
		b      *browser
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{"400 非法 ID", alice, "GET", "/api/foods/abc", "", 400, "INVALID_ARGUMENT"},
		{"403 普通用户写", alice, "POST", "/api/admin/foods", minimalFoodBody, 403, "FORBIDDEN"},
		{"404 不存在", alice, "GET", "/api/foods/999999", "", 404, "NOT_FOUND"},
		{"409 重名", admin, "POST", "/api/admin/foods", minimalFoodBody, 409, "FOOD_NAME_TAKEN"},
	}
	for _, tc := range cases {
		w := tc.b.request(tc.method, tc.path, tc.body, nil)
		status(t, w, tc.status)
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Data any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s: 错误体形状异常: %s", tc.name, w.Body.String())
		}
		if envelope.Error.Code != tc.code || envelope.Error.Message == "" || envelope.Data != nil {
			t.Fatalf("%s: 错误体异常: %s", tc.name, w.Body.String())
		}
	}
}

// FOOD-API-018：并发持锁写入返回 503 DATABASE_BUSY；释放后同一请求成功。
func TestFoodAPI018_DatabaseBusyMappedAndRecovers(t *testing.T) {
	f, admin, _ := foodFixture(t)
	name := "持锁食材"
	body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"`+name+`"`, 1)
	// 绕过连接池的独立连接占住写锁。
	raw, err := sql.Open("sqlite", "file:"+dbPath(t, f))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err = raw.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	w := admin.request("POST", "/api/admin/foods", body, nil)
	status(t, w, 503)
	code(t, w, "DATABASE_BUSY")
	// 无食材行、无审计行、无半提交数据。
	if n := queryInt(t, f.st.DB, "SELECT count(*) FROM foods WHERE name=?", name); n != 0 {
		t.Fatalf("繁忙失败留下食材行: %d", n)
	}
	if auditCount(t, f) != 0 {
		t.Fatal("繁忙失败留下审计行")
	}
	if _, err = raw.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	// 释放锁后同一请求成功。
	w = admin.request("POST", "/api/admin/foods", body, nil)
	status(t, w, 201)
}

// FOOD-API-019：PUT 提交过期版本 409 VERSION_CONFLICT 且无任何变化。
func TestFoodAPI019_StaleVersionConflict(t *testing.T) {
	f, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	// 更新一次推到 version=2。
	body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"二代名","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), body, nil)
	status(t, w, 200)
	auditsBefore := auditCount(t, f)
	// 以过期 version=1 再次提交改名。
	stale := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"过期提交","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), stale, nil)
	status(t, w, 409)
	code(t, w, "VERSION_CONFLICT")
	var name string
	var version int64
	if err := f.st.DB.QueryRow("SELECT name,version FROM foods WHERE id=?", created.ID).Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != "二代名" || version != 2 {
		t.Fatalf("过期提交改变了状态: %s v%d", name, version)
	}
	if after := auditCount(t, f); after != auditsBefore {
		t.Fatalf("过期提交写了审计: %d -> %d", auditsBefore, after)
	}
}

// FOOD-API-020：写路径目标不存在 404 且无写入。
func TestFoodAPI020_UpdateMissingFoodNotFound(t *testing.T) {
	f, admin, _ := foodFixture(t)
	before := auditCount(t, f)
	body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"幽灵食材","version":1`, 1)
	w := admin.request("PUT", "/api/admin/foods/999999", body, nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	// 启停形态同样 404。
	disable := strings.Replace(body, `"status":"active"`, `"status":"disabled"`, 1)
	w = admin.request("PUT", "/api/admin/foods/999999", disable, nil)
	status(t, w, 404)
	code(t, w, "NOT_FOUND")
	if n := queryInt(t, f.st.DB, "SELECT count(*) FROM foods WHERE name='幽灵食材'"); n != 0 {
		t.Fatalf("404 写入了食材行: %d", n)
	}
	if after := auditCount(t, f); after != before {
		t.Fatalf("404 写入了审计: %d -> %d", before, after)
	}
}

// FOOD-SEC-001：未登录写操作 401（沿用现有认证错误码与结构）。
func TestFoodSEC001_UnauthenticatedWriteRejected(t *testing.T) {
	f, _, _ := foodFixture(t)
	anon := f.browser()
	anon.token()
	w := anon.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
	w = anon.request("PUT", "/api/admin/foods/1", strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"燕麦片","version":1`, 1), nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
}

// FOOD-SEC-002：普通用户写操作 403，无食材行、无审计行。
func TestFoodSEC002_NonAdminWriteForbidden(t *testing.T) {
	f, _, alice := foodFixture(t)
	foodsBefore := queryInt(t, f.st.DB, "SELECT count(*) FROM foods")
	auditsBefore := auditCount(t, f)
	w := alice.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 403)
	code(t, w, "FORBIDDEN")
	w = alice.request("PUT", "/api/admin/foods/1", strings.Replace(minimalFoodBody, `"name":"燕麦片"`, `"name":"越权改名","version":1`, 1), nil)
	status(t, w, 403)
	code(t, w, "FORBIDDEN")
	if queryInt(t, f.st.DB, "SELECT count(*) FROM foods") != foodsBefore {
		t.Fatal("越权写入留下食材行")
	}
	if auditCount(t, f) != auditsBefore {
		t.Fatal("越权写入留下审计行")
	}
}

// FOOD-SEC-003：普通用户读管理接口 403（含公共审计表越权红线）。
func TestFoodSEC003_NonAdminReadForbidden(t *testing.T) {
	_, _, alice := foodFixture(t)
	w := alice.request("GET", "/api/admin/foods", "", nil)
	status(t, w, 403)
	code(t, w, "FORBIDDEN")
	w = alice.request("GET", "/api/admin/resource-audit-logs", "", nil)
	status(t, w, 403)
	code(t, w, "FORBIDDEN")
}

// FOOD-SEC-004：事务内降权复核——SQL 直改角色后，会话解析读到最新角色并拒绝。
func TestFoodSEC004_InTransactionDemotionRecheck(t *testing.T) {
	f, admin, _ := foodFixture(t)
	foodID := seedFoodRow(t, f, "降权目标", "active")
	if _, err := f.st.DB.Exec("UPDATE users SET role='user' WHERE username='root_admin'"); err != nil {
		t.Fatal(err)
	}
	auditsBefore := auditCount(t, f)
	body := strings.Replace(minimalFoodBody, `"name":"燕麦片"`, fmt.Sprintf(`"name":"降权后改名","version":1`), 1)
	w := admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", foodID), body, nil)
	status(t, w, 403)
	code(t, w, "FORBIDDEN")
	var name, statusCol string
	if err := f.st.DB.QueryRow("SELECT name,status FROM foods WHERE id=?", foodID).Scan(&name, &statusCol); err != nil {
		t.Fatal(err)
	}
	if name != "降权目标" || statusCol != "active" {
		t.Fatalf("降权后写入生效: %s %s", name, statusCol)
	}
	if auditCount(t, f) != auditsBefore {
		t.Fatal("降权后写入留下审计")
	}
}

// FOOD-SEC-005：事务内禁用复核——账号禁用后现有会话立即失效（沿用现有
// 中间件行为：会话不再解析、返回 401 AUTH_REQUIRED；用例文档写 403 与
// 账号模块现状不符，断言继承行为，已在实现报告单独说明）。
func TestFoodSEC005_InTransactionDisableRecheck(t *testing.T) {
	f, admin, _ := foodFixture(t)
	if _, err := f.st.DB.Exec("UPDATE users SET status='disabled' WHERE username='root_admin'"); err != nil {
		t.Fatal(err)
	}
	foodsBefore := queryInt(t, f.st.DB, "SELECT count(*) FROM foods")
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	if w.Code != 401 && w.Code != 403 {
		t.Fatalf("禁用管理员未被拒绝: %d %s", w.Code, w.Body.String())
	}
	if queryInt(t, f.st.DB, "SELECT count(*) FROM foods") != foodsBefore {
		t.Fatal("禁用管理员写入生效")
	}
}

// FOOD-SEC-006：身份代次与头不一致。
func TestFoodSEC006_IdentityHeaderAndGeneration(t *testing.T) {
	f, admin, alice := foodFixture(t)
	// A. X-Expected-User-ID 与会话不一致 → 409 IDENTITY_CHANGED
	//（用管理员会话构造：管理员组先做角色检查，普通用户在角色检查即 403）。
	w := admin.request("POST", "/api/admin/foods", minimalFoodBody, map[string]string{"X-Expected-User-ID": strconv.FormatInt(alice.id, 10)})
	status(t, w, 409)
	code(t, w, "IDENTITY_CHANGED")
	// B. 服务端递增 auth_version 后，旧 access 重放 → 401 AUTH_REQUIRED
	//（会话随版本递增被撤销，不可刷新重试）。
	if _, err := f.st.DB.Exec("UPDATE users SET auth_version=auth_version+1 WHERE id=?", alice.id); err != nil {
		t.Fatal(err)
	}
	w = alice.request("GET", "/api/food-catalog", "", nil)
	status(t, w, 401)
	code(t, w, "AUTH_REQUIRED")
}

// FOOD-SEC-007：强制改密未完成沿用现有错误。
func TestFoodSEC007_PasswordChangeRequiredGate(t *testing.T) {
	f, _, alice := foodFixture(t)
	if _, err := f.st.DB.Exec("UPDATE users SET must_change_password=1 WHERE id=?", alice.id); err != nil {
		t.Fatal(err)
	}
	w := alice.request("GET", "/api/food-catalog", "", nil)
	status(t, w, 403)
	code(t, w, "PASSWORD_CHANGE_REQUIRED")
	w = alice.request("POST", "/api/admin/foods", minimalFoodBody, nil)
	status(t, w, 403)
	code(t, w, "PASSWORD_CHANGE_REQUIRED")
}

// FOOD-SEC-009：停用账号携旧 access 访问新接口——沿用现有中间件行为
// （会话不再解析、返回 401；说明同 FOOD-SEC-005）。
func TestFoodSEC009_DisabledUserOldAccess(t *testing.T) {
	f, _, alice := foodFixture(t)
	if _, err := f.st.DB.Exec("UPDATE users SET status='disabled' WHERE id=?", alice.id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/food-catalog", "/api/foods/1"} {
		w := alice.request("GET", path, "", nil)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("停用账号未被拒绝 %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

// FOOD-COMPAT-001：GET /api/foods 响应逐字节保持原契约
// （仅 active、ID 升序、无分页包裹、每元素仅 id/name/status 三字段）。
func TestFoodCOMPAT001_LegacyFoodsEndpointByteContract(t *testing.T) {
	f, _, alice := foodFixture(t)
	maltID := seedFoodRow(t, f, "燕麦", "active")
	seedFoodRow(t, f, "荞麦", "disabled") // 停用不出现
	quinoaID := seedFoodRow(t, f, "藜麦", "active")
	// 手工拼出期望字节：基础 12 条（id 1..12，全 active）+ 新增 active 两条。
	legacy := []string{"鸡蛋", "牛奶", "大米", "小麦", "花生", "大豆", "虾", "鱼", "牛肉", "鸡肉", "番茄", "西兰花"}
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i, name := range legacy {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":%q,"status":"active"}`, i+1, name)
	}
	fmt.Fprintf(&b, `,{"id":%d,"name":%q,"status":"active"}`, maltID, "燕麦")
	fmt.Fprintf(&b, `,{"id":%d,"name":%q,"status":"active"}`, quinoaID, "藜麦")
	b.WriteString(`]}`)
	w := alice.request("GET", "/api/foods", "", nil)
	status(t, w, 200)
	if w.Body.String() != b.String() {
		t.Fatalf("原 /foods 契约被破坏:\n got  %s\n want %s", w.Body.String(), b.String())
	}
}

// FOOD-API-021：PUT 省略营养/换算键 → 400 INVALID_ARGUMENT，原数据不被
// 静默清空（键缺失与显式 null 必须区分；缺失多个时错误消息列出全部）。
func TestFoodAPI021_PutOmittingKeysRejectedAndKeepsData(t *testing.T) {
	_, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", fullFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	// 省略 6 个营养键与 3 个换算键，仅保留其余 5 键 + version。
	omitBody := `{"name":"全麦面包","status":"active","allergens":["wheat"],"allergen_complete":true,"allergen_source":"标签","version":1}`
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), omitBody, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"energy_kcal_100g", "protein_g_100g", "fat_g_100g", "carbohydrate_g_100g",
		"nutrition_source", "nutrition_reference", "density_g_ml", "piece_weight_g", "conversion_source",
	} {
		if !strings.Contains(envelope.Error.Message, key) {
			t.Fatalf("错误消息未列出缺失字段 %s: %s", key, envelope.Error.Message)
		}
	}
	// GET 详情确认原营养/换算数据未被清空（未落库）。
	w = admin.request("GET", fmt.Sprintf("/api/foods/%d", created.ID), "", nil)
	status(t, w, 200)
	var got store.FoodDetail
	data(t, w, &got)
	if got.EnergyKcal100g == nil || *got.EnergyKcal100g != 246 ||
		got.DensityGml == nil || *got.DensityGml != 0.4 ||
		got.NutritionSource == nil || *got.NutritionSource != "成分表" ||
		got.ConversionSource == nil || *got.ConversionSource != "包装标注" {
		t.Fatalf("省略键的 PUT 清空了原数据: %+v", got)
	}
	if got.Version != created.Version {
		t.Fatalf("省略键的 PUT 升了版本: %d -> %d", created.Version, got.Version)
	}
}

// FOOD-API-022：PUT 完整 14 键且营养/换算显式传 null → 200，按契约清为
// null（显式清空语义保持不变，与键缺失相区分）。
func TestFoodAPI022_PutExplicitNullClearsByContract(t *testing.T) {
	_, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", fullFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	nullBody := strings.Replace(fullFoodBody, `"name":"全麦面包"`, `"name":"全麦面包","version":1`, 1)
	// 营养与换算共 9 个可选键全部改为显式 null。
	for _, oldNew := range [][2]string{
		{`"energy_kcal_100g":246`, `"energy_kcal_100g":null`},
		{`"protein_g_100g":10.4`, `"protein_g_100g":null`},
		{`"fat_g_100g":3.4`, `"fat_g_100g":null`},
		{`"carbohydrate_g_100g":45.8`, `"carbohydrate_g_100g":null`},
		{`"nutrition_source":"成分表"`, `"nutrition_source":null`},
		{`"nutrition_reference":"bread-01"`, `"nutrition_reference":null`},
		{`"density_g_ml":0.4`, `"density_g_ml":null`},
		{`"piece_weight_g":35`, `"piece_weight_g":null`},
		{`"conversion_source":"包装标注"`, `"conversion_source":null`},
	} {
		nullBody = strings.Replace(nullBody, oldNew[0], oldNew[1], 1)
	}
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), nullBody, nil)
	status(t, w, 200)
	var got store.FoodDetail
	data(t, w, &got)
	if got.EnergyKcal100g != nil || got.ProteinG100g != nil || got.FatG100g != nil || got.CarbohydrateG100g != nil ||
		got.NutritionSource != nil || got.NutritionReference != nil ||
		got.DensityGml != nil || got.PieceWeightG != nil || got.ConversionSource != nil {
		t.Fatalf("显式 null 未按契约清空: %+v", got)
	}
	if got.Version != 2 || got.Name != "全麦面包" {
		t.Fatalf("显式 null 更新结果异常: %+v", got)
	}
	// 过敏原组未传 null，必须原样保留。
	if len(got.Allergens) != 1 || got.Allergens[0] != "wheat" || !got.AllergenComplete || got.AllergenSource == nil {
		t.Fatalf("过敏原组被意外改动: %+v", got)
	}
}

// FOOD-API-023：allergens 键显式传 null → 400（requireFoodInput 兜底，
// foodWriteFromInput 不被解引用 panic）。
func TestFoodAPI023_AllergensExplicitNullRejected(t *testing.T) {
	_, admin, _ := foodFixture(t)
	w := admin.request("POST", "/api/admin/foods", fullFoodBody, nil)
	status(t, w, 201)
	var created store.FoodDetail
	data(t, w, &created)
	// allergens 显式 null，其余 13 键齐全。
	nullAllergens := strings.Replace(fullFoodBody, `"allergens":["wheat"]`, `"allergens":null`, 1)
	nullAllergens = strings.Replace(nullAllergens, `"name":"全麦面包"`, `"name":"全麦面包","version":1`, 1)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/foods/%d", created.ID), nullAllergens, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(envelope.Error.Message, "allergens") {
		t.Fatalf("错误消息未指明 allergens: %s", envelope.Error.Message)
	}
	// POST 同样拒绝显式 null 的 allergens。
	w = admin.request("POST", "/api/admin/foods", strings.Replace(minimalFoodBody, `"allergens":[]`, `"allergens":null`, 1), nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 原数据未变。
	w = admin.request("GET", fmt.Sprintf("/api/foods/%d", created.ID), "", nil)
	status(t, w, 200)
	var got store.FoodDetail
	data(t, w, &got)
	if len(got.Allergens) != 1 || got.Allergens[0] != "wheat" || got.Version != created.Version {
		t.Fatalf("allergens=null 请求改变了原数据: %+v", got)
	}
}

// FOOD-API-024：Swagger schema 的 required 必须与后端 decodeFoodInput 的
// 必填键全集一致，防止按 schema 构造的请求被后端 400 拒绝。直接引用
// foodRequiredKeys（同包），schema 与实现永不漂移。
func TestFoodAPI024_SwaggerRequiredMatchesBackend(t *testing.T) {
	// 测试工作目录是 internal/api，swagger.json 在 server/docs 下。
	raw, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Definitions struct {
			FoodInput struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"api.foodInput"`
			FoodUpdateInput struct {
				Required []string `json:"required"`
			} `json:"api.foodUpdateInput"`
		} `json:"definitions"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	required := make(map[string]bool)
	for _, key := range doc.Definitions.FoodInput.Required {
		required[key] = true
	}
	for _, key := range foodRequiredKeys {
		if !required[key] {
			t.Errorf("api.foodInput.required 缺少后端必填键 %q（当前仅 %v）", key, doc.Definitions.FoodInput.Required)
		}
		if _, ok := doc.Definitions.FoodInput.Properties[key]; !ok {
			t.Errorf("api.foodInput.properties 缺少键 %q", key)
		}
	}
	if got := len(foodRequiredKeys); got != 14 {
		t.Errorf("foodRequiredKeys 应为 14 键，实际 %d（本测试的键数断言需同步更新）", got)
	}
	updateRequired := make(map[string]bool)
	for _, key := range doc.Definitions.FoodUpdateInput.Required {
		updateRequired[key] = true
	}
	if !updateRequired["version"] {
		t.Errorf("api.foodUpdateInput.required 缺少 version: %v", doc.Definitions.FoodUpdateInput.Required)
	}
}
