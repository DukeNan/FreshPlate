CREATE TABLE meal_logs (
 id INTEGER PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 request_key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 recipe_id INTEGER NOT NULL REFERENCES recipes(id) ON DELETE RESTRICT,
 recipe_name TEXT NOT NULL,
 recipe_version INTEGER NOT NULL,
 cooked_servings_milli INTEGER NOT NULL CHECK(cooked_servings_milli>0),
 eaten_servings_milli INTEGER NOT NULL CHECK(eaten_servings_milli>0 AND eaten_servings_milli<=cooked_servings_milli),
 ate_at INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 UNIQUE(user_id,request_key)
);
CREATE INDEX meals_user ON meal_logs(user_id,id);
CREATE TABLE meal_log_items (
 meal_id INTEGER NOT NULL REFERENCES meal_logs(id) ON DELETE RESTRICT,
 inventory_id INTEGER NOT NULL REFERENCES inventory_items(id) ON DELETE RESTRICT,
 food_id INTEGER NOT NULL REFERENCES foods(id) ON DELETE RESTRICT,
 food_name TEXT NOT NULL,
 quantity_milli INTEGER NOT NULL CHECK(quantity_milli>0),
 unit TEXT NOT NULL CHECK(unit IN ('g','ml','piece')),
 energy_kcal_100g REAL,
 protein_g_100g REAL,
 fat_g_100g REAL,
 carbohydrate_g_100g REAL,
 density_g_ml REAL,
 piece_weight_g REAL,
 PRIMARY KEY(meal_id,inventory_id)
);
