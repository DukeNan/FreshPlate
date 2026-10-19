-- 004：食材库存模块。个人库存按批次保存，不自动合并；
-- 数量以千分单位整数存储保证精确增减；原单位与食材引用创建后不可修改。
CREATE TABLE inventory_items (
 id INTEGER PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 food_id INTEGER NOT NULL REFERENCES foods(id) ON DELETE RESTRICT,
 quantity_milli INTEGER NOT NULL CHECK(quantity_milli>=0 AND quantity_milli<=1000000000),
 unit TEXT NOT NULL CHECK(unit IN ('g','ml','piece')),
 expires_on TEXT,
 note TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 deleted_at INTEGER
);
CREATE INDEX inventory_user ON inventory_items(user_id,id);
CREATE INDEX inventory_user_expiry ON inventory_items(user_id,expires_on,id);
