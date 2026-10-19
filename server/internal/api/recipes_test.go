package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// 食谱模块 API 测试（REC-API-001~024、REC-SEC-001~016 中 API 部分）。

// seedAPIFoods 预置测试食材：完整（可发布引用）、不完整、停用。
func seedAPIFoods(t *testing.T, f *fixture) (complete, incomplete, disabled int64) {
	t.Helper()
	r, err := f.st.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,allergen_source,created_at,updated_at) VALUES('API完整食材','active','[\"milk\"]',1,'包装标注',?,?)", f.now.Unix(), f.now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	complete, _ = r.LastInsertId()
	r, err = f.st.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,created_at,updated_at) VALUES('API不完整食材','active','[]',0,?,?)", f.now.Unix(), f.now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	incomplete, _ = r.LastInsertId()
	r, err = f.st.DB.Exec("INSERT INTO foods(name,status,allergens_json,allergen_complete,allergen_source,created_at,updated_at) VALUES('API停用食材','disabled','[]',1,'历史标注',?,?)", f.now.Unix(), f.now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ = r.LastInsertId()
	return
}

// recipePayload 构造完整合法食谱 JSON。
func recipePayload(name string, foodID int64) string {
	return fmt.Sprintf(`{"name":%q,"servings":2,"duration_minutes":15,"steps":["第一步。"],"source":"个人记录","ingredients":[{"food_id":%d,"quantity":"250","unit":"g"}],"additional_allergens":[],"allergen_complete":true,"allergen_source":"包装标注"}`, name, foodID)
}

// putPayload 构造带状态与版本的更新 JSON。
func putPayload(name string, foodID int64, status string, version int64) string {
	return fmt.Sprintf(`{"name":%q,"servings":2,"duration_minutes":15,"steps":["第一步。"],"source":"个人记录","ingredients":[{"food_id":%d,"quantity":"250","unit":"g"}],"additional_allergens":[],"allergen_complete":true,"allergen_source":"包装标注","status":%q,"version":%d}`, name, foodID, status, version)
}

// adminFixture 构造管理员与普通用户两个已登录浏览器，返回种子食材。
func adminFixture(t *testing.T) (*fixture, *browser, *browser, int64, int64, int64) {
	f := newFixture(t)
	if _, err := f.svc.InitAdmin(context.Background(), "recipe_admin", testPassword); err != nil {
		t.Fatal(err)
	}
	admin, user := f.browser(), f.browser()
	admin.login("recipe_admin", testPassword)
	user.register("recipe_user")
	user.login("recipe_user", testPassword)
	complete, incomplete, disabled := seedAPIFoods(t, f)
	return f, admin, user, complete, incomplete, disabled
}

// publishRecipe 通过 API 创建并发布一个公共食谱，返回 id。
func publishRecipe(t *testing.T, admin *browser, name string, foodID int64) int64 {
	t.Helper()
	w := admin.request("POST", "/api/admin/recipes", recipePayload(name, foodID), nil)
	status(t, w, 201)
	id := pathIDOf(t, w)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", id), putPayload(name, foodID, "published", 1), nil)
	status(t, w, 200)
	return id
}

// pathIDOf 从创建/更新响应中读取 id。
func pathIDOf(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	var p struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Data.ID <= 0 {
		t.Fatalf("响应缺 id: %s", w.Body.String())
	}
	return p.Data.ID
}

func decodeBody(w *httptest.ResponseRecorder, v any) error {
	return json.Unmarshal(w.Body.Bytes(), v)
}

// REC-API-001/002：列表默认 scope、分页、mine 与 status 筛选。
func TestRecipeAPI001_002_ListScopesAndPaging(t *testing.T) {
	_, admin, user, complete, _, _ := adminFixture(t)
	for i := 0; i < 3; i++ {
		publishRecipe(t, admin, fmt.Sprintf("公共食谱%02d", i), complete)
	}
	// 管理员另建一个公共草稿：默认 scope 不可见。
	admin.request("POST", "/api/admin/recipes", recipePayload("公共草稿", complete), nil)
	// 普通用户私有三态各一。
	for _, st := range []string{"draft", "published", "archived"} {
		w := user.request("POST", "/api/recipes", recipePayload("私有"+st, complete), nil)
		status(t, w, 201)
		id := pathIDOf(t, w)
		if st != "draft" {
			w = user.request("PUT", fmt.Sprintf("/api/recipes/%d", id), putPayload("私有"+st, complete, st, 1), nil)
			status(t, w, 200)
		}
	}
	w := user.request("GET", "/api/recipes", "", nil)
	status(t, w, 200)
	var page struct {
		Items []struct {
			ID       int64  `json:"id"`
			Status   string `json:"status"`
			Visib    string `json:"visibility"`
		} `json:"items"`
		Total int `json:"total"`
	}
	data(t, w, &page)
	if page.Total != 3 {
		t.Fatalf("默认 scope 应只见 3 个已发布公共: %d %s", page.Total, w.Body.String())
	}
	for _, it := range page.Items {
		if it.Status != "published" || it.Visib != "public" {
			t.Fatalf("默认 scope 混入非 published: %+v", it)
		}
	}
	// page_size>100 拒绝；public scope 带 status 拒绝。
	status(t, user.request("GET", "/api/recipes?page_size=101", "", nil), 400)
	status(t, user.request("GET", "/api/recipes?status=draft", "", nil), 400)
	// mine 返回本人全部状态，status 过滤生效。
	w = user.request("GET", "/api/recipes?scope=mine", "", nil)
	status(t, w, 200)
	data(t, w, &page)
	if page.Total != 3 {
		t.Fatalf("mine 应 3 条: %d", page.Total)
	}
	w = user.request("GET", "/api/recipes?scope=mine&status=draft", "", nil)
	status(t, w, 200)
	data(t, w, &page)
	if page.Total != 1 || page.Items[0].Status != "draft" {
		t.Fatalf("mine+draft 应 1 条: %s", w.Body.String())
	}
	// 管理员列表：全部状态公共，不含私有。
	w = admin.request("GET", "/api/admin/recipes", "", nil)
	status(t, w, 200)
	data(t, w, &page)
	if page.Total != 4 {
		t.Fatalf("admin 应 4 条公共: %d", page.Total)
	}
}

// REC-API-003：详情可见性三元判定。
func TestRecipeAPI003_DetailVisibility(t *testing.T) {
	f, admin, user, complete, _, _ := adminFixture(t)
	published := publishRecipe(t, admin, "已发布", complete)
	admin.request("POST", "/api/admin/recipes", recipePayload("未发布", complete), nil)
	w := user.request("POST", "/api/recipes", recipePayload("他人私有", complete), nil)
	privateOfUser := pathIDOf(t, w)
	for _, tc := range []struct {
		path string
		want int
	}{
		{fmt.Sprintf("/api/recipes/%d", published), 200},
		{"/api/recipes/999999", 404},
	} {
		w := user.request("GET", tc.path, "", nil)
		status(t, w, tc.want)
		if tc.want == 404 {
			code(t, w, "NOT_FOUND")
		}
	}
	// 公共 draft：普通用户 404；本人私有他人 404。
	draftID := int64(0)
	w = admin.request("GET", "/api/admin/recipes?status=draft", "", nil)
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	data(t, w, &list)
	draftID = list.Items[0].ID
	status(t, user.request("GET", fmt.Sprintf("/api/recipes/%d", draftID), "", nil), 404)
	// A 无法查看 B 私有；管理员同样不能（SEC-001/003）。
	other := f.browser()
	other.register("other_user")
	other.login("other_user", testPassword)
	status(t, other.request("GET", fmt.Sprintf("/api/recipes/%d", privateOfUser), "", nil), 404)
	status(t, admin.request("GET", fmt.Sprintf("/api/recipes/%d", privateOfUser), "", nil), 404)
	status(t, admin.request("GET", fmt.Sprintf("/api/admin/recipes/%d", privateOfUser), "", nil), 404)
	// 管理员详情公共 draft 200（SEC-004 上半部分）。
	status(t, admin.request("GET", fmt.Sprintf("/api/admin/recipes/%d", draftID), "", nil), 200)
}

// REC-API-004/005：创建私有、保留字段拒收、不可改公共。
func TestRecipeAPI004_005_CreatePrivateAndUpdate(t *testing.T) {
	_, admin, user, complete, _, _ := adminFixture(t)
	w := user.request("POST", "/api/recipes", recipePayload("我的食谱", complete), nil)
	status(t, w, 201)
	var detail struct {
		ID         int64  `json:"id"`
		Visibility string `json:"visibility"`
		Status     string `json:"status"`
		Version    int64  `json:"version"`
	}
	data(t, w, &detail)
	if detail.Visibility != "private" || detail.Status != "draft" || detail.Version != 1 {
		t.Fatalf("创建结果错误: %+v", detail)
	}
	// 保留字段出现即 400（DisallowUnknownFields）。
	status(t, user.request("POST", "/api/recipes", strings.TrimSuffix(recipePayload("保留字段", complete), "}")+`,"visibility":"public","owner_user_id":1,"status":"published","version":9}`, nil), 400)
	// 更新本人私有 200、version+1（内容有实际变化）。
	id := detail.ID
	w = user.request("PUT", fmt.Sprintf("/api/recipes/%d", id), putPayload("我的食谱改名", complete, "draft", 1), nil)
	status(t, w, 200)
	data(t, w, &detail)
	if detail.Version != 2 {
		t.Fatalf("更新后版本=%d", detail.Version)
	}
	// 对公共食谱 PUT 404。
	pub := publishRecipe(t, admin, "公共不可改", complete)
	status(t, user.request("PUT", fmt.Sprintf("/api/recipes/%d", pub), putPayload("公共不可改", complete, "draft", 1), nil), 404)
}

// REC-API-006/008/009：管理员角色门槛、创建公共、生命周期矩阵。
func TestRecipeAPI006_008_009_AdminLifecycle(t *testing.T) {
	_, admin, user, complete, _, _ := adminFixture(t)
	// REC-SEC-006：普通用户访问 4 条 admin 路由全部 403。
	id := 1
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/admin/recipes"}, {"POST", "/api/admin/recipes"},
		{"GET", fmt.Sprintf("/api/admin/recipes/%d", id)}, {"PUT", fmt.Sprintf("/api/admin/recipes/%d", id)},
	} {
		w := user.request(route.method, route.path, recipePayload("越权", complete), nil)
		status(t, w, 403)
		code(t, w, "FORBIDDEN")
	}
	// 管理员创建：public、owner NULL、draft。
	w := admin.request("POST", "/api/admin/recipes", recipePayload("公共生命周期", complete), nil)
	status(t, w, 201)
	var detail struct {
		ID         int64  `json:"id"`
		Visibility string `json:"visibility"`
		Status     string `json:"status"`
		Version    int64  `json:"version"`
	}
	data(t, w, &detail)
	if detail.Visibility != "public" || detail.Status != "draft" {
		t.Fatalf("公共创建错误: %+v", detail)
	}
	if strings.Contains(w.Body.String(), "owner_user_id") {
		t.Fatalf("响应泄露 owner_user_id: %s", w.Body.String())
	}
	rid := detail.ID
	put := func(status string, version int64) *httptest.ResponseRecorder {
		return admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", rid), putPayload("公共生命周期", complete, status, version), nil)
	}
	// 转换矩阵：draft→published→draft→archived→draft 全部 200；
	// archived→published 409。
	for _, step := range []struct {
		target string
		from   int64
	}{
		{"published", 1}, {"draft", 2}, {"archived", 3}, {"draft", 4},
	} {
		w := put(step.target, step.from)
		status(t, w, 200)
	}
	// 再次归档后尝试 archived→published：409。
	w = put("archived", 5)
	status(t, w, 200)
	w = put("published", 6)
	status(t, w, 409)
	code(t, w, "INVALID_RECIPE_TRANSITION")
	// 保留字段注入 visibility='private' → 400（SEC-005）。
	bad := strings.TrimSuffix(putPayload("注入", complete, "draft", 5), "}") + `,"visibility":"private"}`
	status(t, admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", rid), bad, nil), 400)
	_ = user
}

// REC-API-010/011/012/013：请求体上限路由表与双通道超限。
func TestRecipeAPI010_013_BodyLimits(t *testing.T) {
	f, admin, user, complete, _, _ := adminFixture(t)
	// 约 90KiB 的合法载荷：30 步 × 1000 中文码点。
	longStep := strings.Repeat("步", 1000)
	steps := make([]string, 30)
	for i := range steps {
		steps[i] = longStep
	}
	stepsJSON := make([]string, len(steps))
	for i, s := range steps {
		stepsJSON[i] = strconv.Quote(s)
	}
	big := fmt.Sprintf(`{"name":"大食谱","servings":2,"duration_minutes":15,"steps":[%s],"source":"","ingredients":[{"food_id":%d,"quantity":"1","unit":"g"}],"additional_allergens":[],"allergen_complete":true,"allergen_source":"标注"}`, strings.Join(stepsJSON, ","), complete)
	if len(big) <= 64<<10 || len(big) > 256<<10 {
		t.Fatalf("载荷应处于 (64KiB,256KiB]: %d", len(big))
	}
	w := admin.request("POST", "/api/admin/recipes", big, nil)
	status(t, w, 201)
	adminID := pathIDOf(t, w)
	// 普通 POST 路由同样放宽。
	w = user.request("POST", "/api/recipes", big, nil)
	status(t, w, 201)
	// 两条 PUT 路由放宽（载荷需带 status/version）。
	bigPut := strings.TrimSuffix(big, "}") + `,"status":"draft","version":1}`
	w = admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", adminID), bigPut, nil)
	status(t, w, 200)
	// 256KiB+1 带 Content-Length：中间件预检拦截。
	oversize := "x" + strings.Repeat("a", 256<<10)
	w = admin.request("POST", "/api/admin/recipes", oversize, nil)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 无 Content-Length 流式超限：decode MaxBytesReader 拦截。
	manual := func(method, path string, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-CSRF-Token", admin.csrf)
		r.Header.Set("Authorization", "Bearer "+admin.access)
		r.Header.Set("X-Expected-User-ID", strconv.FormatInt(admin.id, 10))
		for _, c := range admin.cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	w = manual("POST", "/api/admin/recipes", oversize)
	status(t, w, 400)
	code(t, w, "INVALID_ARGUMENT")
	// 其他接口仍 64KiB：GET 详情带大 body、偏好更新 64~256KiB。
	status(t, admin.request("GET", fmt.Sprintf("/api/admin/recipes/%d", adminID), oversize, nil), 400)
	status(t, admin.request("GET", "/api/admin/recipes", oversize, nil), 400)
	status(t, user.request("PUT", "/api/me/preferences", `{"allergens":[],"excluded_food_ids":[],"dietary_preferences":[],"daily_energy_kcal":null,"daily_protein_g":null,"daily_fat_g":null,"daily_carbohydrate_g":null}`+strings.Repeat(" ", 70<<10), nil), 400)
}

// REC-API-016/SEC-008：解码器拒绝矩阵与空 body。
func TestRecipeAPI016_008_DecoderMatrix(t *testing.T) {
	_, admin, user, complete, _, _ := adminFixture(t)
	base := recipePayload("解码矩阵", complete)
	cases := []struct {
		name, body, media string
		want              int
	}{
		{"未知字段", strings.TrimSuffix(base, "}") + `,"owner_user_id":1}`, "application/json", 400},
		{"尾随JSON", base + ` {}`, "application/json", 400},
		{"无效UTF8", strings.Replace(base, "第一步。", "第一步"+string([]byte{255}), 1), "application/json", 400},
		{"空body", "", "application/json", 400},
		{"非JSON", base, "text/plain", 400},
	}
	for _, tc := range cases {
		w := user.request("POST", "/api/recipes", tc.body, map[string]string{"Content-Type": tc.media})
		status(t, w, tc.want)
		code(t, w, "INVALID_ARGUMENT")
	}
	// 管理员路由同一行为。
	w := admin.request("POST", "/api/admin/recipes", base, map[string]string{"Content-Type": "text/plain"})
	status(t, w, 400)
	// Content-Length: 0 的 POST。
	status(t, user.request("POST", "/api/recipes", "", nil), 400)
}

// REC-API-017/018：详情与列表字段契约。
func TestRecipeAPI017_018_FieldContracts(t *testing.T) {
	_, admin, _, complete, _, _ := adminFixture(t)
	w := admin.request("POST", "/api/admin/recipes", `{"name":"契约食谱","servings":2,"duration_minutes":15,"steps":["步骤一"],"source":"来源","ingredients":[{"food_id":` + strconv.FormatInt(complete, 10) + `,"quantity":"1.25","unit":"g"}],"additional_allergens":["peanut"],"allergen_complete":true,"allergen_source":"标注"}`, nil)
	status(t, w, 201)
	id := pathIDOf(t, w)
	w = admin.request("GET", fmt.Sprintf("/api/recipes/%d", id), "", nil)
	status(t, w, 404) // 草稿公共对普通路由不可见
	w = admin.request("GET", fmt.Sprintf("/api/admin/recipes/%d", id), "", nil)
	status(t, w, 200)
	body := w.Body.String()
	for _, want := range []string{`"position":1`, `"quantity":"1.25"`, `"unit":"g"`, `"food":{`, `"effective_allergens"`, `"restriction_metadata_complete":true`, `"has_disabled_foods":false`, `"additional_allergens":["peanut"]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("详情缺字段 %s: %s", want, body)
		}
	}
	for _, banned := range []string{"quantity_milli", "owner_user_id", "steps_json"} {
		if strings.Contains(body, banned) {
			t.Fatalf("详情泄露 %s: %s", banned, body)
		}
	}
	// 发布后列表字段契约：不含步骤。
	publish := strings.Replace(strings.Replace(body, `"status":"draft"`, `"status":"draft"`, 1), "", "", 1)
	_ = publish
	admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", id), putPayload("契约食谱", complete, "published", 1), nil)
	w = admin.request("GET", "/api/admin/recipes", "", nil)
	status(t, w, 200)
	body = w.Body.String()
	for _, want := range []string{`"restriction_metadata_complete"`, `"has_disabled_foods"`, `"visibility":"public"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表缺字段 %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"steps"`) {
		t.Fatalf("列表携带步骤: %s", body)
	}
}

// REC-API-019：父版本冲突端到端。
func TestRecipeAPI019_VersionConflict(t *testing.T) {
	_, a, user, complete, _, _ := adminFixture(t)
	// 经 PATCH 提升第二管理员（InitAdmin 仅允许首个管理员）；既有会话
	// 即时生效，无需重新登录。
	w0 := a.request("PATCH", fmt.Sprintf("/api/admin/users/%d", user.id), `{"role":"admin"}`, nil)
	status(t, w0, 200)
	w := a.request("POST", "/api/admin/recipes", recipePayload("冲突食谱", complete), nil)
	status(t, w, 201)
	id := pathIDOf(t, w)
	// 两个会话各读 v1（隐含于创建响应），A 先改。
	w = a.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", id), putPayload("冲突A", complete, "draft", 1), nil)
	status(t, w, 200)
	w = user.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", id), putPayload("冲突B", complete, "draft", 1), nil)
	status(t, w, 409)
	code(t, w, "VERSION_CONFLICT")
	w = a.request("GET", fmt.Sprintf("/api/admin/recipes/%d", id), "", nil)
	if !strings.Contains(w.Body.String(), `"name":"冲突A"`) {
		t.Fatalf("冲突后内容被覆盖: %s", w.Body.String())
	}
}

// REC-API-020：未登录与身份切换。
func TestRecipeAPI020_UnauthedAndIdentity(t *testing.T) {
	f, _, user, complete, _, _ := adminFixture(t)
	anonymous := f.browser()
	anonymous.token()
	status(t, anonymous.request("GET", "/api/recipes", "", nil), 401)
	status(t, anonymous.request("POST", "/api/recipes", recipePayload("匿名", complete), nil), 401)
	// X-Expected-User-ID 指向他人 → 409。
	w := user.request("POST", "/api/recipes", recipePayload("身份", complete), map[string]string{"X-Expected-User-ID": "999999"})
	status(t, w, 409)
	code(t, w, "IDENTITY_CHANGED")
}

// REC-API-021/022/023/024：无 DELETE、查询参数、原料 position、重名。
func TestRecipeAPI021_024_MiscContracts(t *testing.T) {
	_, admin, user, complete, _, _ := adminFixture(t)
	// 021：无 DELETE 路由。
	status(t, user.request("DELETE", "/api/recipes/1", "", nil), 404)
	status(t, admin.request("DELETE", "/api/admin/recipes/1", "", nil), 404)
	// 022：查询参数校验。
	for _, bad := range []string{"?page=0", "?page=-1", "?page_size=101", "?page_size=abc", "?q=" + strings.Repeat("超", 101)} {
		status(t, user.request("GET", "/api/recipes"+bad, "", nil), 400)
	}
	// 023：原料条目携带 position 拒绝。
	badIngredient := fmt.Sprintf(`{"name":"位置注入","servings":2,"duration_minutes":15,"steps":[],"source":"","ingredients":[{"food_id":%d,"quantity":"1","unit":"g","position":1}],"additional_allergens":[],"allergen_complete":false,"allergen_source":null}`, complete)
	status(t, user.request("POST", "/api/recipes", badIngredient, nil), 400)
	status(t, admin.request("POST", "/api/admin/recipes", badIngredient, nil), 400)
	// 024：同名食谱可共存、不自动合并。
	w1 := user.request("POST", "/api/recipes", recipePayload("同名食谱", complete), nil)
	w2 := user.request("POST", "/api/recipes", recipePayload("同名食谱", complete), nil)
	status(t, w1, 201)
	status(t, w2, 201)
	id1, id2 := pathIDOf(t, w1), pathIDOf(t, w2)
	if id1 == id2 {
		t.Fatal("同名食谱未独立成行")
	}
	w3 := admin.request("POST", "/api/admin/recipes", recipePayload("同名食谱", complete), nil)
	status(t, w3, 201)
	if pathIDOf(t, w3) == id1 || pathIDOf(t, w3) == id2 {
		t.Fatal("公共同名与私有混行")
	}
	// REC-SEC-016：owner_user_id 查询参数被忽略，结果与基准一致。
	base := user.request("GET", "/api/recipes", "", nil).Body.String()
	injected := user.request("GET", fmt.Sprintf("/api/recipes?owner_user_id=%d&page=1&page_size=20", admin.id), "", nil)
	status(t, injected, 200)
	if injected.Body.String() != base {
		t.Fatalf("owner_user_id 参数改变了可见范围:\nbase=%s\ninjected=%s", base, injected.Body.String())
	}
}

// REC-SEC-001/002/003/004/005：越权矩阵。
func TestRecipeSEC001_005_PrivacyMatrix(t *testing.T) {
	f, admin, user, complete, _, _ := adminFixture(t)
	// B 的私有食谱。
	b := f.browser()
	b.register("privacy_owner")
	b.login("privacy_owner", testPassword)
	w := b.request("POST", "/api/recipes", recipePayload("B的私有", complete), nil)
	status(t, w, 201)
	bID := pathIDOf(t, w)
	// SEC-001：A 列表 scope=mine 不含 B。
	w = user.request("GET", "/api/recipes?scope=mine", "", nil)
	if strings.Contains(w.Body.String(), "B的私有") {
		t.Fatal("他人私有泄漏进 mine 列表")
	}
	// SEC-002：A PUT B 私有 404，B 版本不变。
	status(t, user.request("PUT", fmt.Sprintf("/api/recipes/%d", bID), putPayload("A篡改", complete, "draft", 1), nil), 404)
	w = admin.request("GET", "/api/recipes", "", nil)
	_ = w
	// SEC-003：管理员 GET/PUT 他人私有（普通与 admin 路由）全部 404。
	status(t, admin.request("GET", fmt.Sprintf("/api/recipes/%d", bID), "", nil), 404)
	status(t, admin.request("PUT", fmt.Sprintf("/api/recipes/%d", bID), putPayload("管理员篡改", complete, "draft", 1), nil), 404)
	status(t, admin.request("GET", fmt.Sprintf("/api/admin/recipes/%d", bID), "", nil), 404)
	var version int64
	if err := f.st.DB.QueryRow("SELECT version FROM recipes WHERE id=?", bID).Scan(&version); err != nil || version != 1 {
		t.Fatalf("越权修改生效: v%d %v", version, err)
	}
	// SEC-004：公共 draft/archived 普通详情 404，默认列表也不含。
	w = admin.request("POST", "/api/admin/recipes", recipePayload("公共草稿SEC", complete), nil)
	status(t, w, 201)
	draftID := pathIDOf(t, w)
	status(t, user.request("GET", fmt.Sprintf("/api/recipes/%d", draftID), "", nil), 404)
	w = admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", draftID), putPayload("公共草稿SEC", complete, "archived", 1), nil)
	status(t, w, 200)
	status(t, user.request("GET", fmt.Sprintf("/api/recipes/%d", draftID), "", nil), 404)
	w = user.request("GET", "/api/recipes", "", nil)
	if strings.Contains(w.Body.String(), "公共草稿SEC") {
		t.Fatal("归档公共泄漏进默认列表")
	}
	// SEC-005：个人接口注入 visibility='public' 400。
	status(t, user.request("POST", "/api/recipes", strings.TrimSuffix(recipePayload("注入可见性", complete), "}")+`,"visibility":"public"}`, nil), 400)
}

// REC-SEC-008：丢 body/空 body（覆盖管理员路由）。
func TestRecipeSEC008_EmptyBodyAdmin(t *testing.T) {
	_, admin, _, complete, _, _ := adminFixture(t)
	status(t, admin.request("POST", "/api/admin/recipes", "", nil), 400)
	status(t, admin.request("PUT", fmt.Sprintf("/api/admin/recipes/%d", complete), "", nil), 400)
}

// REC-SEC-014：私有食谱不写管理员审计；公共写。
func TestRecipeSEC014_PrivateNoAudit(t *testing.T) {
	f, admin, user, complete, _, _ := adminFixture(t)
	w := user.request("POST", "/api/recipes", recipePayload("私有无审计", complete), nil)
	status(t, w, 201)
	id := pathIDOf(t, w)
	status(t, user.request("PUT", fmt.Sprintf("/api/recipes/%d", id), putPayload("私有无审计", complete, "published", 1), nil), 200)
	var n int
	if err := f.st.DB.QueryRow("SELECT count(*) FROM resource_audit_logs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("私有食谱写了 %d 条审计", n)
	}
	publishRecipe(t, admin, "公共有审计", complete)
	if err := f.st.DB.QueryRow("SELECT count(*) FROM resource_audit_logs WHERE resource_type='recipe'").Scan(&n); err != nil || n < 2 {
		t.Fatalf("公共食谱审计缺失: %d %v", n, err)
	}
}

// REC-LOGIC-013 API 侧：新增引用停用/不存在食材 → FOOD_UNAVAILABLE。
func TestRecipeAPIFoodUnavailable(t *testing.T) {
	_, _, user, complete, _, disabled := adminFixture(t)
	status(t, user.request("POST", "/api/recipes", recipePayload("引用停用", disabled), nil), 400)
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := decodeBody(user.request("POST", "/api/recipes", recipePayload("引用停用", disabled), nil), &e); err != nil {
		t.Fatal(err)
	}
	if e.Error.Code != "FOOD_UNAVAILABLE" {
		t.Fatalf("错误码=%s", e.Error.Code)
	}
	status(t, user.request("POST", "/api/recipes", recipePayload("引用不存在", 999999), nil), 400)
	_ = complete
}
