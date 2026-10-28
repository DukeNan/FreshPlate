package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	migfs "freshplate/server/migrations"
)

// 001 自带的 12 条基础食材；迁移后必须逐条保留 ID、名称与状态。
var legacyFoodNames = []string{"鸡蛋", "牛奶", "大米", "小麦", "花生", "大豆", "虾", "鱼", "牛肉", "鸡肉", "番茄", "西兰花"}

// embeddedChecksum 计算嵌入迁移文件的 SHA-256，与迁移器登记的校验和同源。
func embeddedChecksum(t *testing.T, file string) string {
	t.Helper()
	script, err := migfs.Files.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(script)
	return hex.EncodeToString(sum[:])
}

// legacyDB 打开绕过迁移器的裸连接（不启用外键），仅用于构造夹具或检查失败后的库内容。
func legacyDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func legacyExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// runLegacyMigrations 按旧迁移器语义直接执行 001/002 的嵌入 SQL。
func runLegacyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, file := range []string{"001_users.sql", "002_refresh_tokens.sql"} {
		script, err := migfs.Files.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		legacyExec(t, db, string(script))
	}
}

// v2Fixture 构造 v2 库：执行 001/002、写入 v1/v2 校验和行，并预置用户、
// 会话、刷新令牌与偏好；12 条基础食材由 001 自带。
func v2Fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v2.db")
	db := legacyDB(t, path)
	runLegacyMigrations(t, db)
	legacyExec(t, db, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, completed_at INTEGER NOT NULL)")
	legacyExec(t, db, "INSERT INTO schema_migrations VALUES(1,?,?)", embeddedChecksum(t, "001_users.sql"), time.Now().Unix())
	legacyExec(t, db, "INSERT INTO schema_migrations VALUES(2,?,?)", embeddedChecksum(t, "002_refresh_tokens.sql"), time.Now().Unix())
	legacyExec(t, db, "INSERT INTO users(username,password_hash,role,status,must_change_password,auth_version,created_at,updated_at) VALUES('alice','hash-v2','user','active',0,7,111,222)")
	sessionHash := strings.Repeat("11", 32)
	legacyExec(t, db, "INSERT INTO sessions VALUES(X'"+sessionHash+"',1,7,333,444)")
	legacyExec(t, db, "INSERT INTO refresh_tokens VALUES(X'"+strings.Repeat("22", 32)+"',X'"+sessionHash+"',0)")
	legacyExec(t, db, "INSERT INTO user_preferences(user_id,allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,updated_at) VALUES(1,'[\"milk\"]','[1]','[\"vegan\"]',2000,555)")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// historyFixture 执行 001/002 后写入任意版本行，用于构造跳版/缺版历史。
func historyFixture(t *testing.T, versions []int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	db := legacyDB(t, path)
	runLegacyMigrations(t, db)
	legacyExec(t, db, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, completed_at INTEGER NOT NULL)")
	for _, version := range versions {
		legacyExec(t, db, "INSERT INTO schema_migrations VALUES(?,'forged-checksum',?)", version, time.Now().Unix())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// refuseOpen 断言打开失败、错误包含指定片段（失败场景不得继续启动）。
func refuseOpen(t *testing.T, path, fragment string) {
	t.Helper()
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("open expected failure")
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error %q missing fragment %q", err, fragment)
	}
}

var wantTables = []string{"users", "sessions", "refresh_tokens", "user_preferences", "foods", "audit_logs", "resource_audit_logs", "inventory_items", "recipes", "recipe_ingredients", "recipe_allergens", "meal_logs", "meal_log_items"}

var wantIndexes = []string{"sessions_user", "sessions_expiry", "refresh_session", "audit_created", "audit_target", "resource_audit_resource", "inventory_user", "inventory_user_expiry", "recipes_owner", "recipes_visibility", "recipe_ingredients_food", "meals_user"}

func assertTablesAndIndexes(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range wantTables {
		if got := queryInt(t, db, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table); got != 1 {
			t.Fatalf("table %s missing (count=%d)", table, got)
		}
	}
	for _, index := range wantIndexes {
		if got := queryInt(t, db, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index); got != 1 {
			t.Fatalf("index %s missing (count=%d)", index, got)
		}
	}
}

func columnNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func indexColumns(t *testing.T, db *sql.DB, index string) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// FOOD-MIG-001：空库一次 Open 顺序执行 001→005，版本行、校验和与完成时间一一对应。
func TestFoodMIG001FreshDatabaseAppliesFullChain(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "fresh.db"))
	rows, err := s.DB.Query("SELECT version,checksum,completed_at FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var version, completedAt int64
		var checksum string
		if err := rows.Scan(&version, &checksum, &completedAt); err != nil {
			t.Fatal(err)
		}
		if count >= len(migrationChain) {
			t.Fatalf("unexpected extra migration row version=%d", version)
		}
		want := migrationChain[count]
		if int(version) != want.version {
			t.Fatalf("row %d: version=%d want %d", count, version, want.version)
		}
		if checksum != embeddedChecksum(t, want.file) {
			t.Fatalf("version %d checksum mismatch: %s", version, checksum)
		}
		if completedAt <= 0 {
			t.Fatalf("version %d missing completed_at", version)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(migrationChain) {
		t.Fatalf("schema_migrations has %d rows, want %d", count, len(migrationChain))
	}
	// 001/002/003/004/005 结构齐备。
	assertTablesAndIndexes(t, s.DB)
}

// FOOD-MIG-002：v2 库升级成功，用户、会话、刷新令牌与偏好逐字段保留；
// 不清空用户、不撤销正常会话。
func TestFoodMIG002V2UpgradePreservesExistingData(t *testing.T) {
	path := v2Fixture(t)
	s := mustOpen(t, path)
	if got := queryInt(t, s.DB, "SELECT count(*) FROM schema_migrations"); got != len(migrationChain) {
		t.Fatalf("schema_migrations rows: %d", got)
	}
	var username, passwordHash, role, status string
	var mustChange, authVersion, createdAt, updatedAt int64
	if err := s.DB.QueryRow("SELECT username,password_hash,role,status,must_change_password,auth_version,created_at,updated_at FROM users WHERE id=1").Scan(&username, &passwordHash, &role, &status, &mustChange, &authVersion, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if username != "alice" || passwordHash != "hash-v2" || role != "user" || status != "active" || mustChange != 0 || authVersion != 7 || createdAt != 111 || updatedAt != 222 {
		t.Fatalf("user mutated: %s %s %s %s %d %d %d %d", username, passwordHash, role, status, mustChange, authVersion, createdAt, updatedAt)
	}
	if got := queryInt(t, s.DB, "SELECT count(*) FROM sessions"); got != 1 {
		t.Fatalf("sessions lost: %d", got)
	}
	if got := queryInt(t, s.DB, "SELECT count(*) FROM refresh_tokens"); got != 1 {
		t.Fatalf("refresh tokens lost: %d", got)
	}
	var allergens, excluded, dietary string
	var energy float64
	var prefUpdatedAt int64
	if err := s.DB.QueryRow("SELECT allergens_json,excluded_food_ids_json,dietary_preferences_json,daily_energy_kcal,updated_at FROM user_preferences WHERE user_id=1").Scan(&allergens, &excluded, &dietary, &energy, &prefUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if allergens != `["milk"]` || excluded != "[1]" || dietary != `["vegan"]` || energy != 2000 || prefUpdatedAt != 555 {
		t.Fatalf("preferences mutated: %s %s %s %v %d", allergens, excluded, dietary, energy, prefUpdatedAt)
	}
	if got := queryInt(t, s.DB, "SELECT count(*) FROM foods"); got != 12 {
		t.Fatalf("foods rows: %d", got)
	}
}

// FOOD-MIG-003：重复启动幂等，版本行、结构、业务数据都不变。
func TestFoodMIG003RepeatedOpenIsIdempotent(t *testing.T) {
	path := v2Fixture(t)
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if got := queryInt(t, s.DB, "SELECT count(*) FROM schema_migrations"); got != len(migrationChain) {
			t.Fatalf("open %d: schema_migrations rows: %d", i, got)
		}
		assertTablesAndIndexes(t, s.DB)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db := legacyDB(t, path)
	// 无重复列：foods 恰为新结构列数。
	if got := queryInt(t, db, "SELECT count(*) FROM pragma_table_info('foods')"); got != len(strings.Split(foodColumns, ",")) {
		t.Fatalf("foods column count: %d", got)
	}
	// 无重复索引：显式命名索引恰出现一次。
	if got := queryInt(t, db, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_autoindex%'"); got != len(wantIndexes) {
		t.Fatalf("explicit index count: %d", got)
	}
	for _, index := range wantIndexes {
		if got := queryInt(t, db, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index); got != 1 {
			t.Fatalf("index %s count: %d", index, got)
		}
	}
	for _, check := range []struct {
		query string
		want  int
	}{{"SELECT count(*) FROM users", 1}, {"SELECT count(*) FROM sessions", 1}, {"SELECT count(*) FROM refresh_tokens", 1}, {"SELECT count(*) FROM user_preferences", 1}, {"SELECT count(*) FROM foods", 12}} {
		if got := queryInt(t, db, check.query); got != check.want {
			t.Fatalf("%s: %d want %d", check.query, got, check.want)
		}
	}
}

// FOOD-MIG-004：旧 12 条食材 ID/名称/状态保留，新列取默认值，
// created_at/updated_at 为迁移时刻而非真实创建时间；原 id/name/status 查询形状不变。
func TestFoodMIG004LegacyFoodsKeepIdentityAndDefaults(t *testing.T) {
	path := v2Fixture(t)
	before := time.Now().Unix()
	s := mustOpen(t, path)
	after := time.Now().Unix()
	rows, err := s.DB.Query("SELECT id,name,status,energy_kcal_100g,protein_g_100g,fat_g_100g,carbohydrate_g_100g,nutrition_source,nutrition_reference,density_g_ml,piece_weight_g,conversion_source,allergens_json,allergen_complete,allergen_source,version,created_at,updated_at FROM foods ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, complete, version, createdAt, updatedAt int64
		var name, status, allergensJSON string
		var energy, protein, fat, carb, density, piece sql.NullFloat64
		var nutritionSource, nutritionReference, conversionSource, allergenSource sql.NullString
		if err := rows.Scan(&id, &name, &status, &energy, &protein, &fat, &carb, &nutritionSource, &nutritionReference, &density, &piece, &conversionSource, &allergensJSON, &complete, &allergenSource, &version, &createdAt, &updatedAt); err != nil {
			t.Fatal(err)
		}
		if id != int64(n+1) || name != legacyFoodNames[n] || status != "active" {
			t.Fatalf("food %d identity changed: id=%d name=%s status=%s", n, id, name, status)
		}
		for _, v := range []sql.NullFloat64{energy, protein, fat, carb, density, piece} {
			if v.Valid {
				t.Fatalf("food %d: nutrition/conversion value not NULL: %v", id, v)
			}
		}
		for _, v := range []sql.NullString{nutritionSource, nutritionReference, conversionSource, allergenSource} {
			if v.Valid {
				t.Fatalf("food %d: source column not NULL: %v", id, v)
			}
		}
		if allergensJSON != "[]" || complete != 0 || version != 1 {
			t.Fatalf("food %d defaults changed: %s %d %d", id, allergensJSON, complete, version)
		}
		if createdAt != updatedAt || createdAt < before || createdAt > after+1 {
			t.Fatalf("food %d timestamps not migration time: %d (window %d..%d)", id, createdAt, before, after+1)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Fatalf("foods rows: %d", n)
	}
	rows.Close()
	// 原查询形状（仅 id/name/status）继续可用。
	legacy, err := s.DB.Query("SELECT id,name,status FROM foods WHERE status='active' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	count := 0
	for legacy.Next() {
		var id int64
		var name, status string
		if err := legacy.Scan(&id, &name, &status); err != nil {
			t.Fatal(err)
		}
		if id != int64(count+1) || name != legacyFoodNames[count] || status != "active" {
			t.Fatalf("legacy shape row %d: id=%d name=%s status=%s", count, id, name, status)
		}
		count++
	}
	if err := legacy.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("legacy shape rows: %d", count)
	}
}

// FOOD-MIG-005：已应用版本的校验和被篡改时拒绝启动，且无任何写入。
func TestFoodMIG005ChecksumMismatchRefused(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tampered.db")
			s := mustOpen(t, path)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			db := legacyDB(t, path)
			legacyExec(t, db, "UPDATE schema_migrations SET checksum='changed' WHERE version=?", version)
			db.Close()
			refuseOpen(t, path, "migration checksum mismatch")
			// 库内容无任何变更：版本行仍为完整链条。
			db = legacyDB(t, path)
			if got := queryInt(t, db, "SELECT count(*) FROM schema_migrations"); got != len(migrationChain) {
				t.Fatalf("refused open mutated history: %d rows", got)
			}
		})
	}
}

// FOOD-MIG-006：跳版（{1,3} 缺 2）与缺首版（{2} 缺 1）都拒绝启动。
func TestFoodMIG006IncompleteHistoryRefused(t *testing.T) {
	for _, versions := range [][]int{{1, 3}, {2}} {
		t.Run(fmt.Sprint(versions), func(t *testing.T) {
			path := historyFixture(t, versions)
			refuseOpen(t, path, "unsupported or incomplete migration history")
		})
	}
}

// FOOD-MIG-007：手工插入超出迁移链的未来版本行时拒绝启动且不改动数据。
func TestFoodMIG007FutureVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s := mustOpen(t, path)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db := legacyDB(t, path)
	legacyExec(t, db, "INSERT INTO schema_migrations VALUES(?,'forged',?)", len(migrationChain)+1, time.Now().Unix())
	db.Close()
	refuseOpen(t, path, "newer version")
	db = legacyDB(t, path)
	if got := queryInt(t, db, "SELECT count(*) FROM schema_migrations"); got != len(migrationChain)+1 {
		t.Fatalf("refused open changed history: %d rows", got)
	}
	if got := queryInt(t, db, "SELECT max(version) FROM schema_migrations"); got != len(migrationChain)+1 {
		t.Fatalf("refused open appended rows: max=%d", got)
	}
}

// FOOD-MIG-008：缺表、缺列、缺索引的损坏结构都拒绝启动，错误指明缺失对象。
func TestFoodMIG008CorruptStructureRefused(t *testing.T) {
	for _, tc := range []struct {
		mutation, fragment string
	}{
		{"DROP TABLE foods", "incomplete database schema: foods"},
		{"ALTER TABLE users DROP COLUMN updated_at", "incomplete database schema: users"},
		{"DROP INDEX audit_created", "missing schema index: audit_created"},
	} {
		t.Run(tc.mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corrupt.db")
			s := mustOpen(t, path)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			db := legacyDB(t, path)
			legacyExec(t, db, tc.mutation)
			db.Close()
			refuseOpen(t, path, tc.fragment)
		})
	}
}

// FOOD-MIG-009：非空库缺少 schema_migrations 时保持既有拒绝行为。
func TestFoodMIG009LegacyDatabaseWithoutMigrationsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := legacyDB(t, path)
	legacyExec(t, db, "CREATE TABLE old_preferences(id INTEGER)")
	db.Close()
	refuseOpen(t, path, "unrecognized database schema: explicit legacy migration required")
}

// FOOD-MIG-010：003 中途失败（foods 重建之后、resource_audit_logs 建表时）
// 整体回滚：无 v3 版本行、foods 无新列、无后续表；修复注入后同一库可重新打开。
func TestFoodMIG010MidMigrationFailureRollsBack(t *testing.T) {
	path := v2Fixture(t)
	// 注入占位表，使 003 的 CREATE TABLE resource_audit_logs 失败。
	db := legacyDB(t, path)
	legacyExec(t, db, "CREATE TABLE resource_audit_logs(id INTEGER)")
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("injected mid-migration failure not triggered")
	}
	db = legacyDB(t, path)
	if got := queryInt(t, db, "SELECT count(*) FROM schema_migrations"); got != 2 {
		t.Fatalf("history after failure: %d rows", got)
	}
	if got := queryInt(t, db, "SELECT count(*) FROM schema_migrations WHERE version>=3"); got != 0 {
		t.Fatalf("v3+ rows survived rollback: %d", got)
	}
	// foods 重建被回滚：仍是旧 3 列结构，12 条数据完好。
	if got := queryInt(t, db, "SELECT count(*) FROM pragma_table_info('foods') WHERE name='version'"); got != 0 {
		t.Fatalf("foods rebuild survived rollback: %d new columns", got)
	}
	if got := queryInt(t, db, "SELECT count(*) FROM foods"); got != 12 {
		t.Fatalf("foods rows after failure: %d", got)
	}
	// 后续迁移的表未创建。
	for _, table := range []string{"inventory_items", "recipes", "recipe_ingredients", "recipe_allergens", "meal_logs", "meal_log_items"} {
		if got := queryInt(t, db, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table); got != 0 {
			t.Fatalf("table %s exists after rollback", table)
		}
	}
	// 修复注入后同一库可重新 Open 成功。
	legacyExec(t, db, "DROP TABLE resource_audit_logs")
	db.Close()
	s := mustOpen(t, path)
	if got := queryInt(t, s.DB, "SELECT count(*) FROM schema_migrations"); got != len(migrationChain) {
		t.Fatalf("retry history: %d rows", got)
	}
	assertTablesAndIndexes(t, s.DB)
}

// FOOD-MIG-012：v5 启动结构校验覆盖 foods 新列全集、资源审计表及全部新增索引。
func TestFoodMIG012StartupStructureChecks(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "v5.db"))
	for table, want := range map[string]string{
		"foods":               foodColumns,
		"resource_audit_logs": "id,actor_user_id,resource_type,resource_id,action,details_json,created_at",
		"inventory_items":     "id,user_id,food_id,quantity_milli,unit,expires_on,note,version,created_at,updated_at,deleted_at",
		"recipes":             "id,visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,source,allergen_complete,allergen_source,version,created_at,updated_at",
		"recipe_ingredients":  "recipe_id,position,food_id,quantity_milli,unit",
		"recipe_allergens":    "recipe_id,allergen_code",
	} {
		got := strings.Join(columnNames(t, s.DB, table), ",")
		if got != want {
			t.Fatalf("%s columns: got %s want %s", table, got, want)
		}
	}
	for index, want := range map[string][]string{
		"resource_audit_resource": {"resource_type", "resource_id", "id"},
		"inventory_user":          {"user_id", "id"},
		"inventory_user_expiry":   {"user_id", "expires_on", "id"},
		"recipes_owner":           {"owner_user_id", "status", "id"},
		"recipes_visibility":      {"visibility", "status", "id"},
		"recipe_ingredients_food": {"food_id", "recipe_id"},
	} {
		got := indexColumns(t, s.DB, index)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("index %s columns: %v want %v", index, got, want)
		}
	}
}
