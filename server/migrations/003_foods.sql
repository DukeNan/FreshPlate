-- 003：食材基础数据模块。SQLite 的 ALTER ADD COLUMN 无法添加跨列 CHECK，
-- 因此用「重建表」方式为 foods 增加营养、换算与过敏原列；随后新增公共资源审计表。
-- 旧 12 条记录保留原 id/name/status 与唯一约束；新列取 NULL/默认值，
-- created_at/updated_at 记录迁移时刻而非真实创建时间。
CREATE TABLE foods_new (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 energy_kcal_100g REAL CHECK(energy_kcal_100g IS NULL OR (energy_kcal_100g>=0 AND energy_kcal_100g<=1000)),
 protein_g_100g REAL CHECK(protein_g_100g IS NULL OR (protein_g_100g>=0 AND protein_g_100g<=100)),
 fat_g_100g REAL CHECK(fat_g_100g IS NULL OR (fat_g_100g>=0 AND fat_g_100g<=100)),
 carbohydrate_g_100g REAL CHECK(carbohydrate_g_100g IS NULL OR (carbohydrate_g_100g>=0 AND carbohydrate_g_100g<=100)),
 nutrition_source TEXT,
 nutrition_reference TEXT,
 density_g_ml REAL CHECK(density_g_ml IS NULL OR (density_g_ml>0 AND density_g_ml<=100)),
 piece_weight_g REAL CHECK(piece_weight_g IS NULL OR (piece_weight_g>0 AND piece_weight_g<=1000000)),
 conversion_source TEXT,
 allergens_json TEXT NOT NULL DEFAULT '[]',
 allergen_complete INTEGER NOT NULL DEFAULT 0 CHECK(allergen_complete IN (0,1)),
 allergen_source TEXT,
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 -- 营养完整组：六字段全空（未知），或四项营养值与两个来源全部非空且来源非空白。
 -- CHECK 允许 NULL 通过，完整分支必须逐项显式 IS NOT NULL，不能只写比较。
 CHECK (
   (
     energy_kcal_100g IS NULL AND protein_g_100g IS NULL
     AND fat_g_100g IS NULL AND carbohydrate_g_100g IS NULL
     AND nutrition_source IS NULL AND nutrition_reference IS NULL
   ) OR (
     energy_kcal_100g IS NOT NULL AND protein_g_100g IS NOT NULL
     AND fat_g_100g IS NOT NULL AND carbohydrate_g_100g IS NOT NULL
     AND nutrition_source IS NOT NULL AND length(trim(nutrition_source)) > 0
     AND nutrition_reference IS NOT NULL AND length(trim(nutrition_reference)) > 0
   )
 ),
 -- 换算系数存在时必须有换算依据；空字符串不等于 NULL。
 CHECK (density_g_ml IS NULL OR (conversion_source IS NOT NULL AND length(trim(conversion_source)) > 0)),
 CHECK (piece_weight_g IS NULL OR (conversion_source IS NOT NULL AND length(trim(conversion_source)) > 0)),
 -- 声称过敏原已核对时必须有标注依据。
 CHECK (allergen_complete=0 OR (allergen_source IS NOT NULL AND length(trim(allergen_source)) > 0))
);
INSERT INTO foods_new(id,name,status,version,created_at,updated_at)
 SELECT id,name,status,1,CAST(strftime('%s','now') AS INTEGER),CAST(strftime('%s','now') AS INTEGER) FROM foods;
DROP TABLE foods;
ALTER TABLE foods_new RENAME TO foods;
-- 窄用途的资源管理审计表：food/recipe 公共数据的管理动作；账号审计仍走 audit_logs。
CREATE TABLE resource_audit_logs (
 id INTEGER PRIMARY KEY,
 actor_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 resource_type TEXT NOT NULL CHECK(resource_type IN ('food','recipe')),
 resource_id INTEGER NOT NULL CHECK(resource_id>0),
 action TEXT NOT NULL CHECK(action IN ('food_created','food_updated','food_disabled','food_enabled','recipe_created','recipe_updated','recipe_published','recipe_unpublished','recipe_archived','recipe_restored')),
 details_json TEXT NOT NULL DEFAULT '{}',
 created_at INTEGER NOT NULL
);
CREATE INDEX resource_audit_resource ON resource_audit_logs(resource_type,resource_id,id);
