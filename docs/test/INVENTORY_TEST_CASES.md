# 库存模块测试用例

| 项 | 内容 |
| --- | --- |
| 依据方案 | [../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md)（§8 验收与测试表 + 正文全部错误码与边界值） |
| 编写日期 | 2026-10-19 |
| 状态 | 用例已定义；实现完成后按「运行矩阵」执行并回填实际结果 |
| 用例 ID 规则 | `INV-<层次缩写>-<三位序号>`；层次缩写：LOGIC / STORE / MIG / API / SEC / COMPAT / WEB / E2E |
| 优先级 | P0 主流程与安全底线；P1 错误分支与边界；P2 体验与审计类 |
| 落点约定 | 后端测试与被测代码同目录（`server/internal/api/api_test.go` 的 fixture/httptest 模式、`server/internal/store/store_test.go` 的临时 SQLite 模式、`server/internal/logic/*_test.go` 的纯函数表驱动模式）；前端 Vitest 与组件同目录；E2E 用 Playwright 隔离服务端口 18080（`web/e2e/server.mjs` 启动，夹具 `web/e2e/fixtures.json`，种子 `web/e2e/seed-admin.py`） |

说明：`logic/quantity.go` 的数量解析/格式化 helper 由库存与食谱模块共享，其单元用例全部落在本文件（recipe 模块文档不复述）。

---

## 1. 逻辑层（LOGIC）

### 1.1 数量契约：ParseQuantityMilli

落点：`server/internal/logic/quantity_test.go`（表驱动，不经过 float64）。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-LOGIC-001 | 合法整数解析 | 无 | `ParseQuantityMilli("250")` | 返回 `250000, nil` | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-002 | 合法三位小数解析 | 无 | `ParseQuantityMilli("250.125")` | 返回 `250125, nil` | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-003 | 零值可解析 | 无 | `ParseQuantityMilli("0")` | 返回 `0, nil`（编辑期允许 0 表示用完，创建期 >0 由库存业务层另行拒绝，见 INV-LOGIC-036） | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-004 | 零开头带小数合法 | 无 | `ParseQuantityMilli("0.5")`、`ParseQuantityMilli("0.125")` | 分别返回 `500`、`125`，nil（`0` 仅允许本身或 `0.xxx` 小数形式） | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-005 | 最小精度 0.001 | 无 | `ParseQuantityMilli("0.001")` | 返回 `1, nil` | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-006 | 最大数量 1000000 | 无 | `ParseQuantityMilli("1000000")` | 返回 `1000000000, nil`（= CHECK 上限，合法） | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-007 | 超过最大数量 | 无 | `ParseQuantityMilli("1000000.001")` | 拒绝，返回 INVALID_ARGUMENT 类错误；在相乘前完成范围检查，不发生整型溢出 | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-008 | 拒绝四位小数（不四舍五入截断） | 无 | `ParseQuantityMilli("0.0001")`、`ParseQuantityMilli("1.0001")`、`ParseQuantityMilli("250.1239")` | 全部拒绝；明确拒绝“四舍五入/截断到三位”的语义 | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-009 | 三位小数边界值 | 无 | `ParseQuantityMilli("0.999")`、`ParseQuantityMilli("999.999")` | 分别返回 `999`、`999999`，nil | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-010 | 拒绝正负号 | 无 | `"+5"`、`"-1"`、`"-0.5"`、`"+0"` | 全部拒绝；负库存永远不允许 | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-011 | 拒绝指数形式 | 无 | `"1e3"`、`"1E3"`、`"1e-3"`、`"0e0"` | 全部拒绝 | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-012 | 拒绝空白 | 无 | `" 5"`、`"5 "`、`"5 .5"`、`"\t250"`、全角空格前缀 | 全部拒绝（只接受 ASCII 十进制） | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-013 | 拒绝前导零 | 无 | `"007"`、`"01"`、`"00.5"`、`"01.5"` | 全部拒绝（`"0"`、`"0.5"` 例外，见 INV-LOGIC-003/004） | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-014 | 拒绝残缺形式 | 无 | `""`、`"."`、`"5."`、`".5"`、`"5.1.2"` | 全部拒绝 | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-015 | 拒绝非 ASCII 数字 | 无 | 全角 `"２５０"`、中文数字 `"五"` | 全部拒绝 | P2 | server/internal/logic/quantity_test.go |
| INV-LOGIC-016 | 超长整数位不溢出 | 无 | 21 个 `9` 组成的字符串 | 拒绝并返回错误，不 panic、不回绕 | P1 | server/internal/logic/quantity_test.go |
| INV-LOGIC-017 | FormatQuantity 规范输出 | 无 | `FormatQuantity(1250)`、`(1000)`、`(1)`、`(0)`、`(1000000000)`、`(1500)`、`(1005000)` | 分别输出 `"1.25"`、`"1"`、`"0.001"`、`"0"`、`"1000000"`、`"1.5"`、`"1005"`（无多余前导零、去小数末尾零） | P0 | server/internal/logic/quantity_test.go |
| INV-LOGIC-018 | 解析/格式化往返不丢精度 | 无 | 枚举 `["0","0.001","0.01","0.125","0.5","1","1.25","2.5","6","250","250.125","999.999","1000000"]` | 对每个值 `FormatQuantity(ParseQuantityMilli(s)) == s`；边界值（0.001、1000000）必须包含 | P0 | server/internal/logic/quantity_test.go |

### 1.2 日期解析与 expiry_state 派生

落点：`server/internal/logic/inventory_test.go`；时钟经服务端注入（参照 `api_test.go` 中 `svc.Now = func() time.Time {...}` 的替换方式）。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-LOGIC-019 | 合法日期解析 | 无 | `"2026-10-25"` 经 `time.Parse("2006-01-02", …)` 后严格比对格式 | 解析成功，格式完全一致 | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-020 | 拒绝非零填充月/日 | 无 | `"2026-2-3"`、`"2026-10-5"` | 拒绝，错误码 INVALID_ARGUMENT | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-021 | 拒绝无分隔符日期 | 无 | `"20261301"` | 拒绝 | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-022 | 拒绝带时间后缀 | 无 | `"2026-10-25T00:00:00"`、`"2026-10-25 10:00"` | 拒绝（解析后须严格等于 YYYY-MM-DD） | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-023 | 闰日判定 | 无 | `"2028-02-29"` 与 `"2026-02-29"` | 2028（闰年）合法；2026 拒绝 | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-024 | 日期范围边界 | 无 | `"1970-01-01"`、`"9999-12-31"`、`"1969-12-31"`、`"9999-12-32"` | 前两者合法；越界拒绝 | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-025 | 无日期为未知 | 无 | expires_on 缺省/null | 存储 NULL，派生 `unknown`（数量 >0 时） | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-026 | today 每请求只取一次 | 注入时钟 | 同一次列表请求内变更注入时钟 | 筛选条件与响应 expiry_state 共用第一次取得的 today，不受中途变化影响 | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-027 | expired 派生 | 注入 today=2026-10-19 | date=2026-10-18，数量 >0 | `expired` | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-028 | 今日到期仍为 expiring | 注入 today=2026-10-19 | date=2026-10-19，数量 >0 | `expiring`（到期日当天标记“今日到期”，次日起过期） | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-029 | 3 天/4 天边界 | 注入 today=2026-10-19 | date=2026-10-22 与 date=2026-10-23 | today+3 → `expiring`；today+4 → `fresh` | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-030 | fresh 派生 | 注入 today | date > today+3 | `fresh` | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-031 | 数量 0 优先 depleted | 注入 today | quantity_milli=0 且 date=昨天（已过期到期日） | `depleted`，不再显示临期/过期警告 | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-032 | UTC 日历日跨日 | 注入时钟分别为 UTC 2026-10-19T23:59 与 2026-10-20T00:00 | 同一 date=2026-10-19 | 前者 `expiring`（今日到期），后者 `expired`；不依赖服务器本地时区 | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-033 | 停用食材只能减量（业务规则） | food.status=disabled 的库存批次 | 比较：新数量 > 旧数量 / 新数量 ≤ 旧数量、设 0、仅改备注或到期日 | 增量拒绝（映射 409 FOOD_DISABLED）；减量、设零、改备注、改到期日、删除均允许 | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-034 | 创建数量必须 >0 | 无 | quantity 解析为 0 的创建请求 | 拒绝，INVALID_ARGUMENT（helper 允许 0，创建业务规则拒绝） | P0 | server/internal/logic/inventory_test.go |
| INV-LOGIC-035 | note 码点上限校验 | 无 | 500 码点（含多码点 emoji）与 501 码点 | 500 通过；501 拒绝（按 Unicode 码点计数而非字节） | P1 | server/internal/logic/inventory_test.go |
| INV-LOGIC-036 | q 子串码点上限校验 | 无 | 100 码点与 101 码点的 `q` 参数 | 100 通过；101 拒绝 | P1 | server/internal/logic/inventory_test.go |

---

## 2. 存储层（STORE，含数据库约束）

落点：`server/internal/store/inventory_test.go`（参照 `store_test.go`：临时 SQLite 文件建库）；写路径断言遵循 `WithImmediate(单连接 BEGIN IMMEDIATE) → checkSession(事务内复核) → 读记录比版本 → 条件 UPDATE … WHERE id=? AND version=? 校验影响行数=1 → 失败整体回滚`。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-STORE-001 | 创建批次默认值 | 已登录用户、active food | 创建 unit='g'、quantity=250 批次 | `version=1`、`note=''`、created_at/updated_at 为 UTC Unix 秒、quantity_milli=250000 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-002 | 多批次不合并 | 已有同 food 同 unit 批次 | 再次创建完全相同内容的批次 | 两行独立保存（POST 不因相同内容合并） | P0 | server/internal/store/inventory_test.go |
| INV-STORE-003 | quantity_milli 负数 CHECK | 已建 v4 库 | 直接 SQL 插入 quantity_milli=-1 | SQLite CHECK 约束拒绝 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-004 | quantity_milli 超上限 CHECK | 已建 v4 库 | 直接 SQL 插入 quantity_milli=1000000001 | CHECK 拒绝（上限 1000000000） | P0 | server/internal/store/inventory_test.go |
| INV-STORE-005 | unit CHECK 枚举 | 已建 v4 库 | 插入 unit='kg' | CHECK 拒绝；'g'/'ml'/'piece' 均可插入 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-006 | version CHECK >0 | 已建 v4 库 | 插入 version=0 | CHECK 拒绝 | P2 | server/internal/store/inventory_test.go |
| INV-STORE-007 | food_id 外键 RESTRICT | 已建 v4 库 | 插入引用不存在 food_id 的批次 | 外键拒绝 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-008 | 用户外键不能删除 | 用户有库存批次 | 删除该 users 行 | RESTRICT 拒绝 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-009 | foods 外键 RESTRICT | food 被库存引用 | 直接 SQL 删除该 foods 行 | RESTRICT 拒绝（下线走停用，不走删除） | P1 | server/internal/store/inventory_test.go |
| INV-STORE-010 | 逻辑删除不返回 | 已删除一批次 | 列表查询、详情读取 | 均不可见（`deleted_at IS NULL` 过滤参与所有查询） | P0 | server/internal/store/inventory_test.go |
| INV-STORE-011 | 删除设 deleted_at 且 version+1 | 存在 version=2 批次 | 执行逻辑删除 | 行保留、deleted_at 置值、version=3 | P1 | server/internal/store/inventory_test.go |
| INV-STORE-012 | 条件更新版本不匹配 | 批次 version=2 | 以 version=1 执行条件 UPDATE | 影响行数=0，判定 VERSION_CONFLICT，事务回滚，记录未变 | P0 | server/internal/store/inventory_test.go |
| INV-STORE-013 | 并发编辑仅一次成功 | 批次 version=1，两个连接 | 两连接各自读 version=1 后先后提交编辑 | 先者成功 version=2；后者影响行数=0 → 409 VERSION_CONFLICT；不得把旧数量覆盖到新数量 | P0 | server/internal/store/inventory_test.go（配合 go test -race） |
| INV-STORE-014 | 事务内撤销身份有效 | 写事务已开启 | 事务执行中撤销该会话/触发强制改密 | 事务内 checkSession 复核失败，整体回滚，无部分写入 | P0 | server/internal/store/inventory_test.go（参照 transaction_fault_test.go 注入方式） |
| INV-STORE-015 | 业务失败整体回滚 | 存在批次 | 编辑时注入失败（如日期校验失败/food 状态变化） | 数量、updated_at、version 均未改变 | P1 | server/internal/store/inventory_test.go |
| INV-STORE-016 | 相同内容 PUT 不加版本 | 批次 version=3 | 提交与当前值完全相同的 PUT | 返回原详情，version 仍为 3，updated_at 不推进 | P1 | server/internal/store/inventory_test.go |
| INV-STORE-017 | 列表只读事务与 total 一致 | 25 个批次 | 列表 page=1&page_size=20 | 只读事务（BeginTx ReadOnly）内 pageBounds(LIMIT/OFFSET) 取 items、同事务 count(*) 取 total=25；返回 (items, total, nil) | P1 | server/internal/store/inventory_test.go |
| INV-STORE-018 | 列表排序与零数量项 | 混合数量批次（含 0） | 默认列表 | 按 id DESC 排序；数量为零项包含在默认结果中 | P1 | server/internal/store/inventory_test.go |
| INV-STORE-019 | 所有查询限定归属与未删除 | 用户 A、B 各有批次，A 有已删除批次 | 以 user_id=A 执行各筛选分支（五状态 + 无筛选） | 结果恒满足 user_id=A 且 deleted_at IS NULL；depleted 分支 `quantity_milli=0`，其余分支均含 `quantity_milli>0`（用完批次不进入日期状态） | P0 | server/internal/store/inventory_test.go |
| INV-STORE-020 | 编辑不触碰 food_id/unit | 批次 food_id=7、unit='ml' | 任意成功编辑后重读 | food_id 与 unit 保持不变（原单位不可更改） | P0 | server/internal/store/inventory_test.go |
| INV-STORE-021 | 并发停用阻止新增 | active food；两个事务交错 | 事务 1 停用 food 与事务 2 创建该 food 库存并发提交 | 二者串行化：创建要么在停用前提交成功，要么因事务内 food active 复核失败回滚（FOOD_UNAVAILABLE）；不存在“停用后仍创建成功” | P0 | server/internal/store/inventory_test.go（配合 go test -race） |
| INV-STORE-022 | 未知换算不阻止保存 | food 无任何换算/营养信息 | 分别以 unit='g'/'ml'/'piece' 创建批次 | 全部创建成功（原单位与 quantity_milli 是事实数据，不因换算未知而拒绝） | P1 | server/internal/store/inventory_test.go |
| INV-STORE-023 | 索引存在 | 已建 v4 库 | 查 sqlite_master | `(user_id,id)` 与 `(user_id,expires_on,id)` 索引存在 | P2 | server/internal/store/inventory_test.go |

---

## 3. 迁移（MIG）

落点：`server/migrations/migrations_test.go`（新建；建库方式参照 `server/internal/store/store_test.go` 的临时 SQLite 文件）。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-MIG-001 | 空库直接建到 v4 | 无数据库文件 | 打开新建库 | 迁移 001→004 全部执行，schema_version=4，inventory_items 表就绪，空列表为正常状态 | P0 | server/migrations/migrations_test.go |
| INV-MIG-002 | 已有 foods v3 库升级 v4 | 先建含 foods/用户/忌口数据的 v3 库 | 打开并迁移 | 升级到 v4；既有 foods、users、preferences 数据逐行保留不变 | P0 | server/migrations/migrations_test.go |
| INV-MIG-003 | v2 升级路径 | 构造 v2 库 | 连续迁移到 v4 | v2→v3→v4 顺序执行成功，数据保留 | P1 | server/migrations/migrations_test.go |
| INV-MIG-004 | 重复启动幂等 | 已迁移到 v4 的库 | 再次启动服务/再次迁移 | 不重复执行 004，版本保持 4，无错误 | P0 | server/migrations/migrations_test.go |
| INV-MIG-005 | 校验和登记 | 已迁移库 | 篡改已执行迁移 SQL 或重读登记 | 迁移器校验和校验生效：不匹配时报错拒绝启动；未篡改时结构检查通过 | P1 | server/migrations/migrations_test.go |
| INV-MIG-006 | 管理员 CLI 打开 v4 | 已迁移 v4 库 | 运行 `cmd/admin` 对应维护命令（`python3 scripts/verify_cli.py --go go` 覆盖） | CLI 正常打开 v4 库完成维护动作，无迁移报错 | P1 | server/migrations/migrations_test.go + scripts/verify_cli.py |
| INV-MIG-007 | 表结构完整 | 已迁移库 | 检查 inventory_items 列、CHECK、外键（users/foods RESTRICT）、两个索引 | 与方案 §2 定义完全一致 | P0 | server/migrations/migrations_test.go |
| INV-MIG-008 | 不更改既有表 | v3 库升级前后 | 对比 foods、user_preferences、认证表结构 | 迁移前后结构完全一致 | P1 | server/migrations/migrations_test.go |
| INV-MIG-009 | 无破坏性降级脚本 | 代码审查 | 检索 migrations 目录 | 不存在 v4→v3 降级 SQL；`004_inventory.sql` 只升不降 | P2 | server/migrations/migrations_test.go |

---

## 4. API 层（API）

落点：`server/internal/api/inventory_test.go`（新建；复用 `api_test.go` 的 fixture/browser/status/data/code 辅助与 JWT 登录流程）。所有路径相对 `/api`，均要求登录及完成强制改密。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-API-001 | 创建成功 201 | 登录、active food | `POST /inventory-items`，body `{"food_id":1,"quantity":"6","unit":"piece","expires_on":"2026-10-25","note":"第一批"}` | 201；data 含 id、food{id,name,status}、quantity="6"、unit、expires_on、note、version=1、expiry_state、created_at、updated_at | P0 | server/internal/api/inventory_test.go |
| INV-API-002 | 默认分页列表 | 登录、>20 批次 | `GET /inventory-items`（无参数） | 200；page=1、page_size=20 默认生效；`{"items":[…],"total":N}`；id DESC | P0 | server/internal/api/inventory_test.go |
| INV-API-003 | 空列表 | 登录、无批次 | `GET /inventory-items` | 200 且 items=[]、total=0（空列表为正常状态） | P0 | server/internal/api/inventory_test.go |
| INV-API-004 | q 名称字面子串筛选 | 登录、有“鸡胸肉”“鸡腿肉”批次 | `GET /inventory-items?q=鸡胸` | 仅返回名称含该字面子串的批次 | P1 | server/internal/api/inventory_test.go |
| INV-API-005 | q 码点边界 | 登录 | q 为 100 码点 / 101 码点 / 101 字节 100 码点内的 emoji 串 | 100 码点通过；101 码点 400 INVALID_ARGUMENT（按码点计） | P1 | server/internal/api/inventory_test.go |
| INV-API-006 | food_id 筛选校验 | 登录 | food_id=1（存在）/ 999999（无批次）/ 0 / -1 / "abc" | 前两者 200（后者 items=[]）；后三者 400 INVALID_ARGUMENT | P1 | server/internal/api/inventory_test.go |
| INV-API-007 | expiry_state 五值筛选 | 登录，预置五状态各一批次 | 依次 `expiry_state=depleted/expired/expiring/fresh/unknown` | 每次仅返回 §3.3 表对应条件命中的批次；数量 0 只匹配 depleted | P0 | server/internal/api/inventory_test.go |
| INV-API-008 | expiry_state 非法值 | 登录 | `expiry_state=stale` | 400 INVALID_ARGUMENT | P1 | server/internal/api/inventory_test.go |
| INV-API-009 | 分页边界 | 登录 | page=0 / page=-1 / page=abc / page_size=0 / page_size=101 / page_size=100 | 前 5 个 400 INVALID_ARGUMENT；page_size=100 通过（上限 100） | P1 | server/internal/api/inventory_test.go |
| INV-API-010 | 详情与 404 | 登录；存在批次 1、已删除批次 2 | `GET /inventory-items/1`、`/2`、`/999999` | 1 → 200 详情；2、999999 均 404 NOT_FOUND（已删除与不存在不可区分） | P0 | server/internal/api/inventory_test.go |
| INV-API-011 | PUT 全量替换 | 登录、批次 version=1 | `PUT /inventory-items/:id`，body `{"quantity":"4.5","expires_on":"2026-10-25","note":"已用掉一部分","version":1}` | 200 返回详情，version=2、数量更新 | P0 | server/internal/api/inventory_test.go |
| INV-API-012 | PUT 缺可变字段 | 登录 | 依次缺 quantity / expires_on / note / version 各发一次 | 每次均 400 INVALID_ARGUMENT（三个可变字段 + version 必填） | P0 | server/internal/api/inventory_test.go |
| INV-API-013 | PUT 携带 food_id/unit | 登录 | PUT body 附加 `"food_id":2` 或 `"unit":"g"` | 400 INVALID_ARGUMENT（DisallowUnknownFields；不接受 food_id/unit） | P0 | server/internal/api/inventory_test.go |
| INV-API-014 | PUT 版本冲突 | 批次当前 version=2 | 以 version=1 提交 PUT | 409 VERSION_CONFLICT，记录未变 | P0 | server/internal/api/inventory_test.go |
| INV-API-015 | PUT 数量为 0 表示用完 | 登录 | PUT quantity="0" | 200；expiry_state=depleted | P0 | server/internal/api/inventory_test.go |
| INV-API-016 | PUT 不存在的 id | 登录 | `PUT /inventory-items/999999` | 404 NOT_FOUND | P1 | server/internal/api/inventory_test.go |
| INV-API-017 | DELETE 成功 204 | 登录、批次 version=2 | `DELETE /inventory-items/:id`，body `{"version":2}` | 204 空体；随后 GET 404 | P0 | server/internal/api/inventory_test.go |
| INV-API-018 | DELETE 二次删除 | 批次已逻辑删除 | 再次 DELETE（任意 version） | 404 NOT_FOUND（不承诺在旧 version 下返回成功） | P0 | server/internal/api/inventory_test.go |
| INV-API-019 | DELETE 版本冲突 | 批次当前 version=3 | body `{"version":2}` | 409 VERSION_CONFLICT 且记录未删除 | P0 | server/internal/api/inventory_test.go |
| INV-API-020 | DELETE version 非法值 | 登录、批次存在 | body 分别为 `{}`、`{"version":null}`、`{"version":0}`、`{"version":-1}`、`{"version":1.5}`、`{"version":"2"}` | 全部 400 INVALID_ARGUMENT | P0 | server/internal/api/inventory_test.go |
| INV-API-021 | POST 引用不可用食材 | 登录 | food_id 不存在 / food.status=disabled 各一次 | 均 400 FOOD_UNAVAILABLE | P0 | server/internal/api/inventory_test.go |
| INV-API-022 | 停用食材减量可、增量拒 | 存在 disabled food 的库存批次 | PUT 增量一次；PUT 减量、设 0、改备注、改到期日各一次 | 增量 409 FOOD_DISABLED；其余 200 | P0 | server/internal/api/inventory_test.go |
| INV-API-023 | 停用项读取保留 | disabled food 的库存批次 | `GET /inventory-items/:id` 与列表 | food.name 保留且 food.status="disabled"（显示名称和停用标记）；停用不删除库存 | P1 | server/internal/api/inventory_test.go |
| INV-API-024 | quantity 数字 JSON 拒绝 | 登录 | POST/PUT 中 `"quantity":250`（数值）或 null | 400 INVALID_ARGUMENT（只接受字符串） | P0 | server/internal/api/inventory_test.go |
| INV-API-025 | Content-Type 校验 | 登录 | POST 不带 application/json | 400 INVALID_ARGUMENT（沿用既有媒体类型检查） | P1 | server/internal/api/inventory_test.go |
| INV-API-026 | 64KiB body 上限 | 登录 | POST body >64KiB（超长 note） | 400 INVALID_ARGUMENT（MaxBytesReader） | P1 | server/internal/api/inventory_test.go |
| INV-API-027 | 非 UTF-8 body | 登录 | POST 携带非法 UTF-8 字节 | 400 INVALID_ARGUMENT | P1 | server/internal/api/inventory_test.go |
| INV-API-028 | 未知字段拒绝 | 登录 | POST body 附加 `"foo":1` | 400 INVALID_ARGUMENT（DisallowUnknownFields） | P1 | server/internal/api/inventory_test.go |
| INV-API-029 | 尾随 JSON 拒绝 | 登录 | body 为合法 JSON 后再拼接 `{}` | 400 INVALID_ARGUMENT | P1 | server/internal/api/inventory_test.go |
| INV-API-030 | 未登录与未改密 | 无会话 / 处于强制改密状态 | 访问任一 /inventory-items 端点 | 未登录 401；强制改密未完成按既有错误约定拒绝（沿用现有中间件行为） | P0 | server/internal/api/inventory_test.go |
| INV-API-031 | 身份断言不一致 | 登录用户 A | 请求头 X-Expected-User-ID=B 的 id | 409 IDENTITY_CHANGED（沿用 protected/admin 中间件既有行为） | P0 | server/internal/api/inventory_test.go |
| INV-API-032 | 响应不泄露内部字段 | 登录 | 读取详情与列表 JSON | 不出现 user_id、quantity_milli（内部千分单位）、deleted_at；expires_on=null 显式输出 | P0 | server/internal/api/inventory_test.go |
| INV-API-033 | 数量输出为规范字符串 | 已存 quantity_milli=1250 的批次 | 读取详情 | quantity="1.25"（字符串、去尾零），非数字类型 | P1 | server/internal/api/inventory_test.go |
| INV-API-034 | 已过期日期可录入 | 登录 | POST expires_on=过去日期；PUT 同 | 200/201 成功，expiry_state=expired（允许记录已过期批次，界面明确提示） | P1 | server/internal/api/inventory_test.go |
| INV-API-035 | 列表与筛选同一 today | 注入时钟跨越 UTC 日界 | 同一请求内含日期边界批次与 expiry_state 筛选 | SQL 筛选与响应派生共用同一服务端 today，结果自洽 | P1 | server/internal/api/inventory_test.go |
| INV-API-036 | 不提供聚合端点 | 登录 | `GET /inventory-items/summary` 或任何总量/总质量路径 | 404（不额外提供总质量/总数量，不同单位不能相加） | P2 | server/internal/api/inventory_test.go |
| INV-API-037 | 成功响应 no-store | 登录 | 任意 inventory 请求 | 响应含 Cache-Control: no-store（fixture 已全局断言） | P2 | server/internal/api/inventory_test.go |

---

## 5. 安全（SEC，单独成组）

落点：`server/internal/api/inventory_test.go`（用户 A/B 双账号 + 管理员账号，参照 `api_test.go` 多 browser 实例）。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-SEC-001 | A 不能读 B 的批次 | A、B 各有批次 | A `GET /inventory-items/<B 的 id>` | 404 NOT_FOUND | P0 | server/internal/api/inventory_test.go |
| INV-SEC-002 | A 不能改 B 的批次 | 同上 | A `PUT /inventory-items/<B 的 id>`（携带合法 body 与 version） | 404 NOT_FOUND；B 的记录未变 | P0 | server/internal/api/inventory_test.go |
| INV-SEC-003 | A 不能删 B 的批次 | 同上 | A `DELETE /inventory-items/<B 的 id>`（version 正确） | 404 NOT_FOUND；B 的批次未被删除 | P0 | server/internal/api/inventory_test.go |
| INV-SEC-004 | 管理员也不能访问他人批次 | 管理员与普通用户各有批次 | 管理员对普通用户批次执行 GET/PUT/DELETE | 全部 404 NOT_FOUND（管理员只维护自己的库存，无查询他人库存入口） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-005 | 列表只含本人数据 | A、B 各有批次 | A 遍历所有筛选组合与分页 | 任何响应都不出现 B 的批次 ID | P0 | server/internal/api/inventory_test.go |
| INV-SEC-006 | 伪造 user_id 字段失败 | 登录 | POST/PUT body 附加 `"user_id":<他人 id>` | 400 INVALID_ARGUMENT（不从请求接收 user_id） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-007 | 伪造 food_id/unit 修改失败 | 登录、存在批次 | PUT body 附加 `"food_id":…`、`"unit":"…"` | 400 INVALID_ARGUMENT，且绝不生效（无静默忽略） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-008 | 伪造服务端字段失败 | 登录 | POST body 附加 `"deleted_at"`、`"version"`、`"created_at"`、`"updated_at"`、`"expiry_state"` | 400 INVALID_ARGUMENT（version 新值由服务端设 1；时间/删除标记不可注入） | P1 | server/internal/api/inventory_test.go |
| INV-SEC-009 | 事务内撤销身份有效 | 已登录并开启写事务 | 事务进行中撤销会话/置强制改密 | WithImmediate 内 checkSession 复核失败，写操作整体回滚（对应 INV-STORE-014 的 API 级验证） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-010 | DELETE 丢 body 只能拒绝 | 登录、本人批次存在 | DELETE 不带 body（模拟代理丢弃） | 400 INVALID_ARGUMENT；不执行删除；批次仍可查（永不退化为无版本删除） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-011 | version 校验先于存在性检查 | 登录 | DELETE 不存在 id 且 body 缺失/null/非法 version | 返回 400 INVALID_ARGUMENT 而非 404（顺序：解析并验证 version 必填正整数 → 再查资源存在性） | P0 | server/internal/api/inventory_test.go |
| INV-SEC-012 | 不提供替代版本通道 | 登录、批次存在 | `DELETE /inventory-items/:id?version=2` 或带 If-Match 头、无 body | 仍 400 INVALID_ARGUMENT（不增加查询参数或 If-Match 替代通道） | P1 | server/internal/api/inventory_test.go |
| INV-SEC-013 | 错误不可区分存在性 | A、B 各有批次 | 对比：A 读 B 的批次 / A 读不存在 id / 读已删除 id | 三者状态码与错误体（code/message）完全一致，均为 404 NOT_FOUND | P0 | server/internal/api/inventory_test.go |
| INV-SEC-014 | 隐私不进审计与日志 | 普通用户操作库存 | 检查管理员资源审计记录与服务日志 | 本人库存不写入管理员资源审计；服务日志不记录完整请求体（私人备注不泄露） | P2 | server/internal/api/inventory_test.go（日志断言/代码审查） |

---

## 6. 兼容性（COMPAT）

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-COMPAT-001 | DELETE body 经同源 fetch 透传 | e2e 隔离服务（18080） | 页面内通过本地同源 fetch 发送带 body 的 DELETE | 后端收到 version 并成功 204（首版兼容性限制成立） | P1 | web/e2e/inventory-module.spec.js |
| INV-COMPAT-002 | 代理丢 body 仅导致拒绝 | API 测试环境 | 模拟上游丢弃 body 的 DELETE | 400 INVALID_ARGUMENT，不执行删除（与 INV-SEC-010 同源，端到端复验） | P1 | server/internal/api/inventory_test.go |
| INV-COMPAT-003 | 既有用户与忌口测试仍通过 | 实现完成 | 全量运行 Go 测试与既有 e2e（user-module.spec.js、swagger.spec.js） | 全部通过；迁移不破坏 users/preferences/认证行为 | P0 | 运行矩阵（go test 全量 + Playwright） |
| INV-COMPAT-004 | Swagger opblock 计数随新接口更新 | 新增 5 个 inventory 端点 | 运行 swagger.spec.js | 该用例的 opblock 总数断言按新端点数更新（实现任务，用例文档仅记录此项） | P1 | web/e2e/swagger.spec.js |
| INV-COMPAT-005 | 接口文档与 Swagger 更新 | 实现完成 | 检查 Swagger UI 与接口文档 | 含 5 个 inventory 端点、请求/响应示例与错误码说明 | P2 | docs 更新 + web/e2e/swagger.spec.js |

---

## 7. 前端单元（WEB）

落点：`web/src/views/InventoryView.test.js`（Vitest，node 环境，与组件同目录）；API helper 相关断言参照 `web/src/api.test.js`。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-WEB-001 | 字符串数量不被浮点转换 | 组件已挂载 | 输入 "0.1" 并提交，截获请求 body | quantity 为字符串 "0.1"；不得先经 Number() 再提交（避免 0.1 浮点表示进入 payload） | P0 | web/src/views/InventoryView.test.js |
| INV-WEB-002 | 数量输入形态 | 渲染表单 | 检查数量输入元素 | type 为文本且 inputmode="decimal" | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-003 | 400 错误信息展示 | mock API 返回 400 INVALID_ARGUMENT | 提交非法表单 | 页面展示后端 message，不静默失败 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-004 | 409 冲突不覆盖草稿 | mock PUT 返回 409 VERSION_CONFLICT | 提交已过期的编辑 | 表单内容保留供核对；提示重新加载当前记录；不自动覆盖 | P0 | web/src/views/InventoryView.test.js |
| INV-WEB-005 | 网络失败不自动新建 | mock POST 网络错误 | 提交创建请求 | 不自动重试；提示保存状态不明、先刷新核对（无幂等键，禁止盲目重试） | P0 | web/src/views/InventoryView.test.js |
| INV-WEB-006 | 503 处理 | mock API 返回 503 DATABASE_BUSY | 提交任意写请求 | 展示忙碌提示，可重试，无未定义行为 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-007 | 提交中防连点 | 组件已挂载 | 连续快速点击保存 | 提交期间按钮禁用，只发出一次请求 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-008 | loading 与空状态 | mock 慢响应 / 空列表 | 打开库存页 | 先展示 loading；空数据显示空状态文案与新建入口 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-009 | 删除确认内容 | 列表有批次 | 打开删除确认 | 确认框显示食材名称和剩余数量后才可删除 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-010 | 删除末项回退有效页 | mock 第 2 页仅 1 项 | 删除该页唯一批次 | 自动回退到有效页码并刷新列表 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-011 | 账号变化丢弃草稿 | 表单有未保存内容 | 模拟身份切换（沿用现有身份处理） | 丢弃旧账号草稿，表单清空/复位 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-012 | 状态标签不只靠颜色 | 渲染五状态行 | 断言标签 DOM | 每个状态（fresh/expiring/expired/unknown/depleted）有文字标签 + 颜色，不只颜色 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-013 | 编辑锁定食材与单位 | 打开编辑表单 | 检查字段 | food 与 unit 只读；food 为停用状态时显示“只允许减少数量”提示 | P0 | web/src/views/InventoryView.test.js |
| INV-WEB-014 | 日期不经时区转换 | 渲染列表 | 检查 YYYY-MM-DD 展示路径 | 字符串原样展示；页面注明“按 UTC 日期判断”；不调用 Date 把日期转浏览器时区 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-015 | 新建表单选食材 | mock GET /food-catalog | 打开新建表单 | 分页检索 active 食材，显示名称及明确单位；不自动推断个重 | P1 | web/src/views/InventoryView.test.js |
| INV-WEB-016 | 键盘可用性 | 渲染表单 | Tab 遍历、Enter 提交 | 焦点顺序合理，可纯键盘完成新建/编辑/删除流程 | P2 | web/src/views/InventoryView.test.js |
| INV-WEB-017 | 导航与路由 | 渲染 App | 检查导航与路由表 | App.vue 导航含库存入口；router.js 注册 /inventory（InventoryView.vue） | P1 | web/src/views/InventoryView.test.js + web/src/api.test.js |
| INV-WEB-018 | 数量字符串→千分单位共享换算 | 导入 web/src/quantity.js | 表驱动：`"250"→"250000"`、`"250.5"→"250500"`、`"0.5"→"500"`、`"999999.999"→"999999999"`（不误拒）、`"1000000"→"1000000000"`、`"1000000.001"→"1000000001"`（调用方上限拒绝）、非法输入（abc/1.2345/-5/007/1e3/空串）返回空串；compareQuantity 与后端规范串（无小数点）比较正确 | 全部与手算千分值一致；库存/食谱两视图共用同一实现 | P0 | web/src/quantity.test.js |

---

## 8. 端到端（E2E）

落点：`web/e2e/inventory-module.spec.js`（新建）；隔离服务端口 18080，由 `web/e2e/server.mjs` 启动；夹具 `web/e2e/fixtures.json`，种子 `web/e2e/seed-admin.py`；登录/注册辅助照抄 `web/e2e/user-module.spec.js`。临期数据通过夹具或 UI 以相对服务端 today 的日期构造。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| INV-E2E-001 | 新增两批同食材 | 用户 A 已登录、catalog 有目标食材 | 通过 UI 新建同食材两批（不同数量/到期日） | 列表出现两行独立批次，数量/单位/到期日与录入一致，不被合并 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-002 | 筛选临期 | A 已有今日至 +3 天内到期批次与更远日期批次 | 状态筛选选“临期” | 仅显示 expiring 批次；标签为文字+颜色；翻页/刷新后仍成立 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-003 | 编辑批次 | A 存在批次 | 编辑数量（如 "6"→"4.5"）、备注、到期日并保存 | 提示成功，列表刷新显示新值与 version 递增；食材与单位不可改 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-004 | 用完设零 | A 存在批次 | 编辑数量为 "0" 保存 | 批次保留且标记 depleted，不再显示临期/过期警告 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-005 | 删除批次 | A 存在批次 | 点删除 → 确认框显示食材与剩余数量 → 确认 | 批次从列表消失；若为当前页最后一项回退有效页 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-006 | 用户 B 看不到 A 的批次 | A 已有批次；B 为另一账号 | B 登录打开库存页，并直接访问 A 的批次详情 URL | B 的列表不含 A 批次；直接访问得 404/对应错误页 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-007 | 管理员停用后按规则处理 | A 有食材 X 的批次；管理员账号可登录 | 管理员停用 X；A 刷新库存页：尝试减量保存、再尝试增量保存 | A 看到停用标记与“只允许减少数量”；减量成功；增量提交后展示 409 FOOD_DISABLED 错误，表单内容保留 | P0 | web/e2e/inventory-module.spec.js |
| INV-E2E-008 | 两标签页编辑冲突 | A 在两个标签页打开同一批次（均基于 version=N） | 标签 1 保存成功后，标签 2 原样保存 | 标签 2 收到 409 提示，草稿保留，要求重新加载；不自动覆盖 | P1 | web/e2e/inventory-module.spec.js |
| INV-E2E-009 | 移动端视口可用 | 已登录 | 390×844 视口遍历库存列表/新建/编辑 | 无横向溢出（scrollWidth ≤ innerWidth），键盘操作可完成流程 | P2 | web/e2e/inventory-module.spec.js |
| INV-E2E-010 | 删除版本冲突提示在列表刷新后保留（回归） | 用户 A 持有批次 v1；页面外 API 更新至 v2 | 页面持 v1 点删除并确认 → DELETE 返回 409 | 列表刷新为 v2 后仍可见错误提示（含「已被他人修改」）；批次仍存在且为最新值；无成功提示 | P1 | web/e2e/inventory-module.spec.js |

---

## 9. 运行矩阵

实现完成后按下表执行（Go 命令在本机必须使用 `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go`，shell 的 GOROOT 指向旧版本，直接 `go` 会编译失败）。

| 命令 | 工作目录 | 验证的用例组 | 备注 |
| --- | --- | --- | --- |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./...` | `server/` | 全部 LOGIC、STORE、MIG、API、SEC、COMPAT-002 | 覆盖方案 §8 数量/日期/数据库/安全/食材关联后端各行 |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go vet ./...` | `server/` | 全部后端用例的静态检查 | 无 vet 告警 |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test -race ./...` | `server/` | INV-STORE-013（版本竞争）、INV-STORE-021（并发停用）、INV-SEC-009（事务内撤权） | 数据竞争必须为零 |
| `npm test --prefix web` | 仓库根 | 全部 WEB 组（INV-WEB-001～017） | Vitest，node 环境 |
| `npm run build --prefix web` | 仓库根 | WEB 组构建可用性；COMPAT-005 | 生产构建成功 |
| `(cd web && npx playwright install chromium && npm run test:e2e)` | 仓库根 | 全部 E2E 组（INV-E2E-001～009）、INV-COMPAT-001、INV-COMPAT-004 | 隔离服务端口 18080；含既有 user-module 与 swagger 回归 |
| `python3 scripts/verify_cli.py --go go` | 仓库根 | INV-MIG-006（CLI 打开 v4）、INV-COMPAT-003 | CLI 恢复与进程锁验证；如脚本内部调用 `go`，按其参数注入 goenv 路径 |

执行完成后在本文档或实现总结中记录各命令实际结果（通过/失败与要点）。
