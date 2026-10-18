CREATE TABLE users (
 id INTEGER PRIMARY KEY,
 username TEXT NOT NULL UNIQUE CHECK(length(username) BETWEEN 3 AND 32 AND username NOT GLOB '*[^a-z0-9_]*'),
 password_hash TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('user','admin')),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 must_change_password INTEGER NOT NULL DEFAULT 0 CHECK(must_change_password IN (0,1)),
 auth_version INTEGER NOT NULL DEFAULT 1 CHECK(auth_version>0),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE sessions (
 token_hash BLOB NOT NULL PRIMARY KEY CHECK(length(token_hash)=32),
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 auth_version INTEGER NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE user_preferences (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
 allergens_json TEXT NOT NULL DEFAULT '[]', excluded_food_ids_json TEXT NOT NULL DEFAULT '[]',
 dietary_preferences_json TEXT NOT NULL DEFAULT '[]',
 daily_energy_kcal REAL CHECK(daily_energy_kcal>0 AND daily_energy_kcal<=20000),
 daily_protein_g REAL CHECK(daily_protein_g>0 AND daily_protein_g<=2000),
 daily_fat_g REAL CHECK(daily_fat_g>0 AND daily_fat_g<=2000),
 daily_carbohydrate_g REAL CHECK(daily_carbohydrate_g>0 AND daily_carbohydrate_g<=2000),
 updated_at INTEGER NOT NULL
);
CREATE TABLE foods (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')));
-- 基础中文食材名称目录；不含营养或过敏原标注，不能用作营养计算依据。
INSERT INTO foods(name) VALUES ('鸡蛋'),('牛奶'),('大米'),('小麦'),('花生'),('大豆'),('虾'),('鱼'),('牛肉'),('鸡肉'),('番茄'),('西兰花');
CREATE TABLE audit_logs (
 id INTEGER PRIMARY KEY,
 actor_user_id INTEGER REFERENCES users(id) ON DELETE RESTRICT,
 actor_source TEXT NOT NULL CHECK(actor_source IN ('web','cli')),
 action TEXT NOT NULL,
 target_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 details_json TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL
);
CREATE INDEX audit_created ON audit_logs(created_at);
CREATE INDEX audit_target ON audit_logs(target_user_id,id);
