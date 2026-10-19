-- 005：食谱管理模块。父表保存原始输入，原料与附加过敏原为子表，
-- 随父表在同一事务内整体替换；归档取代物理删除，不提供物理删除。
-- visibility 与归属联合约束：private 必须有 owner，public 必须为空。
CREATE TABLE recipes (
 id INTEGER PRIMARY KEY,
 visibility TEXT NOT NULL CHECK(visibility IN ('private','public')),
 owner_user_id INTEGER REFERENCES users(id) ON DELETE RESTRICT,
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','archived')),
 name TEXT NOT NULL,
 servings INTEGER NOT NULL CHECK(servings>=1 AND servings<=100),
 duration_minutes INTEGER NOT NULL CHECK(duration_minutes>=1 AND duration_minutes<=1440),
 steps_json TEXT NOT NULL,
 source TEXT NOT NULL DEFAULT '',
 allergen_complete INTEGER NOT NULL CHECK(allergen_complete IN (0,1)),
 allergen_source TEXT,
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 CHECK((visibility='private' AND owner_user_id IS NOT NULL) OR (visibility='public' AND owner_user_id IS NULL)),
 -- 声称已完成核对时必须有核对依据。
 CHECK(allergen_complete=0 OR (allergen_source IS NOT NULL AND length(trim(allergen_source)) > 0))
);
CREATE INDEX recipes_owner ON recipes(owner_user_id,status,id);
CREATE INDEX recipes_visibility ON recipes(visibility,status,id);
-- 原料按 (recipe_id,position) 定序；同一食谱同一食材只允许出现一次。
CREATE TABLE recipe_ingredients (
 recipe_id INTEGER NOT NULL REFERENCES recipes(id) ON DELETE RESTRICT,
 position INTEGER NOT NULL CHECK(position>=1 AND position<=100),
 food_id INTEGER NOT NULL REFERENCES foods(id) ON DELETE RESTRICT,
 quantity_milli INTEGER NOT NULL CHECK(quantity_milli>=1 AND quantity_milli<=1000000000),
 unit TEXT NOT NULL CHECK(unit IN ('g','ml','piece')),
 PRIMARY KEY(recipe_id,position),
 UNIQUE(recipe_id,food_id)
);
CREATE INDEX recipe_ingredients_food ON recipe_ingredients(food_id,recipe_id);
-- 附加过敏原只保存作者额外标注，不复制 foods 中已有标注；
-- 编码与 logic 层共用同一组 10 种支持值。
CREATE TABLE recipe_allergens (
 recipe_id INTEGER NOT NULL REFERENCES recipes(id) ON DELETE RESTRICT,
 allergen_code TEXT NOT NULL CHECK(allergen_code IN ('milk','egg','fish','crustacean','mollusc','peanut','soy','wheat','tree_nut','sesame')),
 PRIMARY KEY(recipe_id,allergen_code)
);
