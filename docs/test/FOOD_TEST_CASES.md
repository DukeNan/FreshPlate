# 食材模块测试用例

- 依据方案：`docs/food/FOOD_MODULE_TECHNICAL_DESIGN.md`（以 §8「测试与验收」为基线，另承担跨模块公共用例：迁移器顺序执行器、兼容性红线、`resource_audit_logs` 公共审计表）
- 编写日期：2026-10-19
- 状态：用例基线 v1。功能代码尚未实现，全部预期结果以方案契约为准；实现完成后按本文件逐条验证并回填实际验证记录。
- 用例 ID 约定：`FOOD-<层次缩写>-<三位序号>`；层次缩写 LOGIC / STORE / MIG / API / SEC / COMPAT / WEB / E2E。

## 用例统计

| 层次 | ID 前缀 | 数量 | 对应方案 §8 行 |
| --- | --- | --- | --- |
| logic 校验 | FOOD-LOGIC | 19 | 营养组、零与未知、来源、来源长度边界、边界与非有限数、名称、编码、过敏原、缺来源系数 |
| store 与数据库约束 | FOOD-STORE | 13 | CHECK 拒绝、重名并发、版本冲突、审计回滚、停用保留忌口、事务串行 |
| 迁移（跨模块公共） | FOOD-MIG | 12 | 空库、v2 升级、重复启动、原数据保留、校验和/未来版本/损坏结构拒绝、CLI |
| API 契约 | FOOD-API | 18 | 未登录、权限、分页与错误映射、请求体限制、审计分页、并发繁忙与写路径错误码 |
| 安全 | FOOD-SEC | 9 | 越权、伪造字段、事务内降权/禁用/身份变化、丢 body |
| 兼容性红线 | FOOD-COMPAT | 5 | 原 `/foods` 不变、设置页、账号审计、停用忌口、Swagger 计数 |
| 前端单元 | FOOD-WEB | 7 | 视图交互、校验、刷新重试策略、无障碍 |
| E2E | FOOD-E2E | 6 | 管理员新增→检索→停用→忌口保留、未知提示、并发冲突 |
| 合计 | — | 89 | — |

`resource_audit_logs` 公共用例的归属：索引存在 → FOOD-MIG-012；按 id DESC 分页与 details_json 编码 → FOOD-STORE-011/013；筛选参数与分页契约 → FOOD-API-015/016；普通用户 403 → FOOD-SEC-003。

---

## 1. logic 层校验（FOOD-LOGIC）

节首共用前置条件：`store.Open(临时 SQLite 文件)` 成功后以 `logic.New` 构造服务（参照 `server/internal/logic/preferences_test.go` 的纯函数表驱动写法）。全部用例不经过 HTTP。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-LOGIC-001 | 营养组全空可保存 | 服务已初始化 | 创建输入：四项营养值与两个营养来源字段全部显式 null | 校验通过，输入原样下传 store | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-002 | 营养组完整可保存 | 同上 | 四项营养值取合法值（如 121/12.5/3.3/68.6），nutrition_source 与 nutrition_reference 均为非空白字符串 | 校验通过 | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-003 | 营养组部分缺失拒绝 | 同上 | 表驱动：六字段（四值+两来源）逐一置 null、其余保持 FOOD-LOGIC-002 输入，共 6 组 | 每组均返回 400 INVALID_ARGUMENT，错误信息指明完整组要求 | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-004 | 零与未知语义区分 | 同上 | 组 A：energy=0 且组完整；组 B：四值全 null | 组 A 通过且 0 被保留（不得改写为 null）；组 B 通过且保持未知，不补零 | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-005 | 营养来源为空字符串拒绝 | 同上 | nutrition_source="" 或 nutrition_reference=""（其余组完整） | 400 INVALID_ARGUMENT；空字符串不等于 NULL | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-006 | 营养来源仅含 Unicode 空白拒绝 | 同上 | nutrition_source 取 "　"（U+3000）、"\t"、" \n" 等 TrimSpace 后为空的值 | 400 INVALID_ARGUMENT（应用层 TrimSpace，不依赖 SQLite trim） | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-007 | 能量边界 0～1000 | 同上 | 合法：0、1000；拒绝：-0.1、1000.01、1001 | 合法值通过；越界值 400 INVALID_ARGUMENT | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-008 | 三项营养素边界 0～100 | 同上 | protein/fat/carbohydrate 分别取 0、100（合法）、-1、100.01（拒绝），不校验三项相加或与能量的公式关系 | 合法值通过；越界值 400 INVALID_ARGUMENT | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-009 | 非有限数拒绝 | 同上 | 任一营养值或换算系数取 NaN、+Inf、-Inf（JSON 侧由解析层拦截，logic 侧直接以非有限浮点调用） | 400 INVALID_ARGUMENT | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-010 | 名称规范 | 同上 | 合法："  燕麦 "（裁剪为「燕麦」）、恰好 100 码点（含 emoji/汉字组合按码点计）；拒绝：""、"   "、101 码点、含 \n 或 \u0000 等控制字符 | 合法值 TrimSpace 后通过；非法值 400 INVALID_ARGUMENT | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-011 | 非法过敏原编码拒绝 | 同上 | allergens 含不在现有 10 种支持编码内的值（如 "gluten-unknown"、"milk " 带尾空格） | 400 INVALID_ARGUMENT，不复用 preferences 的宽松解析 | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-012 | 过敏原去重与上限 | 同上 | 合法：恰好 10 个不同编码、含重复编码（去重后 ≤10）；拒绝：11 个不同编码 | 合法输入去重后通过；超上限 400 INVALID_ARGUMENT | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-013 | 过敏原三态语义 | 同上 | A：[]+false=未知；B：[]+true=支持范围内已核对无标注项；C：非空数组+false=部分已知 | 三种组合均校验通过，语义标记按输入保留 | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-014 | 撤销完整性保留已知编码 | 已存在 allergens=["milk"], allergen_complete=true 的食材 | 更新为 allergen_complete=false 且保留 allergens=["milk"] | 校验通过，编码保留不被清空 | P2 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-015 | 缺换算依据的系数拒绝 | 同上 | density_g_ml=1.03 或 piece_weight_g=150，但 conversion_source 为 null / "" / 仅空白 | 400 INVALID_ARGUMENT（三种来源形态均拒绝） | P0 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-016 | 换算系数边界与精度 | 同上 | 合法：density=100、piece=1000000、六位小数（0.123456）；拒绝：0、负数、100.0001（density 超限）、1000000.1（piece 超限）、七位小数、NaN/Inf | 合法值通过；非法值 400 INVALID_ARGUMENT | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-017 | 完整性缺标注依据拒绝 | 同上 | allergen_complete=true 但 allergen_source 为 null / "" / 仅空白 | 400 INVALID_ARGUMENT | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-018 | 列表查询参数校验 | 同上 | q：TrimSpace 后 ≤100 码点合法、>100 拒绝；status：仅 active/disabled/空合法；page：≥1；page_size：1～100 | 合法值通过；非法值 400 INVALID_ARGUMENT | P1 | server/internal/logic/foods_test.go |
| FOOD-LOGIC-019 | 来源字段长度边界 500/1000 码点 | 同上 | 表驱动四个来源字段：nutrition_source/conversion_source/allergen_source 各取恰好 500 码点（含汉字与 emoji 组合、按码点计）与 501 码点；nutrition_reference 取恰好 1000 码点与 1001 码点；其余组输入保持合法（参照 FOOD-LOGIC-002） | 恰好上限（500/1000 码点）的值校验通过并原样保留；501/1001 码点均 400 INVALID_ARGUMENT（按码点而非字节计，不依赖 SQLite 长度判断） | P1 | server/internal/logic/foods_test.go |

---

## 2. store 层与数据库约束（FOOD-STORE）

节首共用前置条件：`store.Open(临时 SQLite 文件)` 建库（参照 `server/internal/store/store_test.go`），需要时经 `logic` 预置账号与食材。写路径断言遵循现有模式：`WithImmediate`（单连接 BEGIN IMMEDIATE）→ 事务内复核 → 读记录比版本 → 条件 `UPDATE ... WHERE id=? AND version=?` 校验影响行数=1 → 失败整体回滚。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-STORE-001 | DB CHECK 拒绝部分营养组 | v3 库已建 | 绕过应用层直接 SQL 插入：四项营养值缺一但来源有值（表驱动 6 组组合） | INSERT 触发 CHECK 失败报错，行未写入 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-002 | DB CHECK 拒绝缺失/空营养来源 | 同上 | 直接 SQL：四值有值但 nutrition_source 为 NULL、"" 或纯空白（分支二要求 IS NOT NULL 且 length(trim())>0） | 三种形态均被 CHECK 拒绝 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-003 | DB CHECK 拒绝缺来源的系数与标注 | 同上 | 直接 SQL：A. density_g_ml 有值但 conversion_source NULL；B. allergen_complete=1 但 allergen_source NULL | A、B 均被 CHECK 拒绝 | P1 | server/internal/store/foods_test.go |
| FOOD-STORE-004 | CHECK 允许 NULL 通过无陷阱 | 同上 | 直接 SQL：全空分支（全 NULL）与完整分支（全 NOT NULL）各插入一行 | 两行均成功，验证 CHECK 显式 IS NOT NULL 写法未误伤 NULL 分支 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-005 | 重名创建与并发 | 已有食材「燕麦片」 | A. 顺序再建同名 → 失败；B. 两个 goroutine 各自 WithImmediate 创建同名 | A 返回 409 FOOD_NAME_TAKEN；B 恰一个成功、另一个 409 FOOD_NAME_TAKEN（唯一约束按现有二进制比较，无 Unicode 归一化） | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-006 | 版本冲突整体回滚 | 已存在 version=3 的食材 | 以 version=2 的条件 UPDATE 修改名称 | 条件 UPDATE 影响行数=0 → 返回 409 VERSION_CONFLICT；名称、version、审计均无任何变化 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-007 | 审计失败整体回滚 | 已存在食材与至少一条审计 | 注入审计写入失败（参照 server/internal/store/transaction_fault_test.go 的故障注入方式） | 食材更新被回滚、version 不变、无半提交数据；错误上抛 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-008 | 停用保留用户忌口 | 某用户 user_preferences.excluded_food_ids_json 含食材 X | 停用食材 X（status→disabled） | 偏好行逐字节不变；不物理删除食材、不级联清理 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-009 | 重新启用保留 ID 与版本 | 已停用的食材 X（version=N） | 重新启用 X | ID 不变、status=active、version=N+1；不生成新 ID | P1 | server/internal/store/foods_test.go |
| FOOD-STORE-010 | 状态复核、更新与审计同事务串行 | 已存在食材 | 调用更新写路径，并在审计步骤注入失败 | 状态复核、条件更新、审计写入在同一连接同一事务内完成（无从连接池二次取连接）；任一步失败全部回滚 | P0 | server/internal/store/foods_test.go |
| FOOD-STORE-011 | 列表与 total 同一只读事务 | 预置 ≥25 条混合 active/disabled 食材并含名称匹配项 | 列表查询（ReadOnly BeginTx + pageBounds + count(*)） | 返回 (items, total, err)；total 与同筛选条件一致；items 为当前页；倒序按 ID | P1 | server/internal/store/foods_test.go |
| FOOD-STORE-012 | q 按字面子串匹配不解释通配符 | 预置名称含 "100%" 与 "a_b" 的食材 | q="%"、q="_"、q="100%" | 仅命中字面包含该子串的行，% 与 _ 不作为 SQL 通配符（参数化 LIKE 转义） | P1 | server/internal/store/foods_test.go |
| FOOD-STORE-013 | 审计表写入与 id DESC 分页 | 预置同一食材 ≥3 条审计事件 | 查询 resource_audit_logs 分页 | details_json 由 store 编码（含变更字段数组、前后状态与版本）；按 id DESC 返回；total 正确 | P0 | server/internal/store/resource_audit_test.go |

备注（前向契约，本阶段以代码评审确认、食谱模块落地时扩展为用例）：食材更新不得覆盖食谱自身 version，但必须使下一次食谱读取/校验拿到新的营养与过敏原结果，不信任食谱缓存的旧安全标记（方案 §5）。

---

## 3. 迁移（FOOD-MIG，跨模块公共用例）

节首共用前置条件：临时 SQLite 文件路径（参照 `server/internal/store/store_test.go`）；v2 库用「旧迁移器语义夹具」构造——直接执行 001/002 的嵌入 SQL 并向 schema_migrations 写入 v1/v2 校验和行，再以新版 `store.Open` 打开。所有失败场景断言：`Open` 返回错误、进程不得继续启动、库内容无任何变更。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-MIG-001 | 空库顺序执行 001→003 | 不存在的库文件 | `store.Open` 一次 | 成功；schema_migrations 恰 3 行（1/2/3），校验和与嵌入文件 SHA-256 一致；001/002/003 结构齐备 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-002 | v2 库升级到 v3 | 夹具构造的 v2 库，含用户、会话、refresh_tokens、偏好、12 条食材 | 以新版 `store.Open` 打开 | 成功追加 v3 行；既有用户/会话/偏好数据逐字段保留；不清空用户、不撤销正常会话 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-003 | 重复启动幂等 | FOOD-MIG-001 完成后的 v3 库 | 连续 `store.Open`/`Close` 三次 | 全部成功；schema_migrations 仍 3 行；无重复列/索引；业务数据不变 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-004 | 旧 12 条食材 ID/名称/状态/version 保留 | v2 库预置现有 12 条 (id,name,status) 食材 | 升级到 v3 后逐条读取 | 12 条 ID/名称/状态逐一相等；新列默认：营养与换算全 NULL、allergens_json='[]'、allergen_complete=0、version=1；created_at/updated_at=迁移时间且明确非真实创建时间 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-005 | 已应用版本校验和不匹配拒绝 | v3 库，篡改 schema_migrations 中 v1 或 v2 的 checksum | `store.Open` | 报 migration checksum mismatch，拒绝启动，无任何写入 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-006 | 跳版/缺失中间版本拒绝 | 夹具构造 schema_migrations={1,3}（缺 2）、以及 {2}（缺 1）两个库 | 分别 `store.Open` | 均报 unsupported or incomplete migration history，拒绝启动 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-007 | 未知未来版本拒绝 | v3 库后手工插入 version=4 行 | `store.Open` | 顺序执行器只认识 001→003，拒绝启动且不改动数据 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-008 | 损坏结构拒绝（缺表/缺列/缺索引） | v3 库派生三个损坏副本：DROP TABLE foods；构造缺列副本；DROP INDEX audit_created | 分别 `store.Open` | 均报 incomplete database schema（指明表/列/索引），拒绝启动 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-009 | 非空库缺 schema_migrations 拒绝 | 手工建含无关表的库（无 schema_migrations） | `store.Open` | 报 unrecognized database schema: explicit legacy migration required（保持既有行为） | P1 | server/internal/store/db_test.go |
| FOOD-MIG-010 | 事务中途失败回滚 | 注入执行器在 003 中途失败（如新列 ALTER 之后、resource_audit_logs 建表之前） | `store.Open` | 失败上抛；schema_migrations 无 v3 行、foods 无新列、无新表；修复注入后同一库可重新 Open 成功 | P0 | server/internal/store/db_test.go |
| FOOD-MIG-011 | CLI 能打开 v3 新库与已升级库 | 编译 server/cmd/admin | 分别对空库与 v3 库执行维护命令 | CLI 经 `store.Open` 成功打开并完成操作；库保持 v3；`python3 scripts/verify_cli.py --go go` 通过 | P0 | server/cmd/admin（验证脚本 scripts/verify_cli.py） |
| FOOD-MIG-012 | v3 启动结构校验更新 | FOOD-MIG-001 的 v3 库 | PRAGMA table_info(foods) 与 sqlite_master 检查 | foods 含全部新列；resource_audit_logs 表存在；索引 (resource_type,resource_id,id) 存在 | P0 | server/internal/store/db_test.go |

---

## 4. API 契约（FOOD-API）

节首共用前置条件：httptest 起服务（参照 `server/internal/api/api_test.go` 的 fixture/browser 模式，含 JWT 登录辅助）；已注册并登录管理员 admin 与普通用户 alice；写入请求携带 Bearer 与 `X-Expected-User-ID`。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-API-001 | food-catalog 默认分页与摘要形状 | 预置 >20 条混合状态食材 | GET /api/food-catalog 不带参数 | 200 {"items","total"}；默认 page=1、page_size=20；仅 active；按 ID 倒序；摘要仅 id/name/status/version/过敏原完整标记，不含营养来源 | P0 | server/internal/api/foods_test.go |
| FOOD-API-002 | catalog 查询参数边界 | 同上 | q=恰好 100 码点（合法）、101 码点（400）；page_size=100（合法）、101（400）；page=0、-1、非数字（400） | 合法值生效；非法值均 400 INVALID_ARGUMENT | P0 | server/internal/api/foods_test.go |
| FOOD-API-003 | 详情读取含停用项与时间格式 | 已创建含完整营养/换算/过敏原的食材 X，并停用其中 Y | GET /api/foods/X 与 /api/foods/Y | 均 200；返回全部业务字段；disabled 亦可读；created_at/updated_at 输出 RFC 3339 UTC | P0 | server/internal/api/foods_test.go |
| FOOD-API-004 | 详情 404 与非法 ID | 已登录普通用户 | GET /api/foods/999999（不存在）；/api/foods/0、/-1、/abc | 999999 → 404 NOT_FOUND；其余 → 400 INVALID_ARGUMENT（ID 必须正整数） | P0 | server/internal/api/foods_test.go |
| FOOD-API-005 | 管理员列表状态筛选 | 预置 active 与 disabled 食材 | GET /api/admin/foods：不带 status、status=active、status=disabled、status=other | 前三种分别全量/过滤/过滤；other → 400 INVALID_ARGUMENT | P0 | server/internal/api/foods_test.go |
| FOOD-API-006 | 创建成功与创建审计 | admin 登录 | POST /api/admin/foods 按方案示例 body（仅名称已知，未知显式 null/false） | 201 返回详情 version=1；resource_audit_logs 新增一条 action=food_created，details_json 含字段名数组与前后版本，不含完整请求体 | P0 | server/internal/api/foods_test.go |
| FOOD-API-007 | 全量更新与版本递增 | 已存在 version=1 食材 | PUT /api/admin/foods/:id 提交完整输入 + 当前 version=1 | 200 返回详情 version=2；审计 action=food_updated；响应不返回服务端输入字段 | P0 | server/internal/api/foods_test.go |
| FOOD-API-008 | 无变化更新不升版本不写审计 | 已存在食材 X（version=N） | PUT 提交与当前完全一致的完整输入 | 200 返回原详情；version 仍 N；resource_audit_logs 无新增行 | P0 | server/internal/api/foods_test.go |
| FOOD-API-009 | 启停动作审计编码 | 已存在 active 食材 X | A. PUT 将 X 置 disabled；B. 再置回 active | A 记 food_disabled；B 记 food_enabled；version 各 +1 | P0 | server/internal/api/foods_test.go |
| FOOD-API-010 | 重名 409 | 已存在「燕麦片」 | A. POST 创建同名；B. PUT 把其他食材改为同名 | A、B 均 409 FOOD_NAME_TAKEN | P0 | server/internal/api/foods_test.go |
| FOOD-API-011 | 请求体解码规则 | admin 登录 | 分别发送：含未知字段、尾随 JSON、Content-Type 非 application/json、无效 UTF-8、空 body | 五种均 400 INVALID_ARGUMENT，错误体 {"error":{"code","message"}} | P0 | server/internal/api/foods_test.go |
| FOOD-API-012 | 伪造服务端字段拒绝 | admin 登录 | POST body 带 id/version/created_at/updated_at；PUT body 带 id/created_at/updated_at（PUT 的 version 是必需输入，不得拒绝）；PUT 的 version 另取 0、-1、"abc" 三个非法变体（违反 version > 0 的正整数约束） | 全部 400 INVALID_ARGUMENT；PUT 缺 version 及 version=0/-1/"abc" 亦 400 | P0 | server/internal/api/foods_test.go |
| FOOD-API-013 | 食物路由 64 KiB 双通道限制 | admin 登录 | A. Content-Length=64KiB+1 的 POST；B. 无 Content-Length 的分块流式 body 超 64KiB | A、B 均 400 INVALID_ARGUMENT；Content-Length 预检、MaxBytesReader 与 decode 三处同一限制来源 | P0 | server/internal/api/foods_test.go |
| FOOD-API-014 | 按已匹配路由的限制来源 | 服务已注册路由表 | 单测限制来源函数：食物路由 64 KiB、预留食谱四路由 256 KiB；验证中间件按已匹配路由+方法取值，不按 URL 前缀泛化 | 取值正确；食谱路由落地后在其测试中补 256 KiB 双通道实测（含超限 400 INVALID_ARGUMENT） | P1 | server/internal/api/foods_test.go |
| FOOD-API-015 | 资源审计分页契约 | 预置 ≥25 条审计事件 | GET /api/admin/resource-audit-logs 不带参数 | 200 {"items","total"}；按 id DESC；默认 page=1、page_size=20；分页对象形状与现有接口一致 | P0 | server/internal/api/foods_test.go |
| FOOD-API-016 | 资源审计筛选参数 | 同上 | resource_type=food/recipe（合法）、=user（400）；resource_id 非正整数（400）、正整数（过滤）；action=food_disabled（过滤） | 合法筛选生效；resource_type 仅接受 food/recipe；非法参数 400 INVALID_ARGUMENT | P0 | server/internal/api/foods_test.go |
| FOOD-API-017 | 统一响应与错误映射 | 任意已登录会话 | 抽样触发 400/403/404/409/503 各一类（如 API-004/010/012 与 API-018 busy 场景） | 错误体统一 {"error":{"code","message"}}；成功 {"data":...}；分页 {"items","total"}；无 Content-Length 流式超限与带长度超限错误一致 | P1 | server/internal/api/foods_test.go |
| FOOD-API-018 | 并发持锁写入返回 503 DATABASE_BUSY | admin 登录；另建一条绕过连接池的独立连接，对库执行 BEGIN IMMEDIATE 并保持不提交（占住写锁） | 持锁期间发送 POST /api/admin/foods（合法完整输入），使写路径 BEGIN IMMEDIATE 在 busy_timeout 内取不到写锁；随后释放该连接的锁，重复同一请求作对照 | 持锁请求 503 DATABASE_BUSY（错误体 {"error":{"code","message"}}），无食材行、无审计行、无半提交数据；释放锁后同一请求 201 成功 | P1 | server/internal/api/foods_test.go |
| FOOD-API-019 | PUT 提交过期版本 409 VERSION_CONFLICT | 已存在 version=3 的食材，admin 登录 | PUT /api/admin/foods/:id 提交完整合法输入但 version=2 | 409 VERSION_CONFLICT；食材名称、version 与审计均无任何变化；错误体 {"error":{"code","message"}} | P0 | server/internal/api/foods_test.go |
| FOOD-API-020 | 写路径目标不存在 404 NOT_FOUND | admin 登录 | A. PUT /api/admin/foods/999999 提交完整输入与合法 version（更新形态）；B. 对 999999 提交 status 置 disabled 的完整输入（启停形态） | A、B 均 404 NOT_FOUND（999999 为正整数，不落入 400 非法 ID 分支）；无食材行、无审计行 | P1 | server/internal/api/foods_test.go |
| FOOD-API-021 | PUT 省略字段返回 400 且不清空数据（回归） | 已有含营养与换算的食材（能量 246、密度 0.4） | PUT 完整体但省略 9 个营养/换算键（version 合法） | 400 INVALID_ARGUMENT 且消息逐一列全缺失字段；GET 详情确认能量/密度/来源原样、版本未升 | P0 | server/internal/api/foods_test.go `TestFoodAPI021` |
| FOOD-API-022 | 显式 null 保持契约语义（回归） | 同上场景 | PUT 完整 14 键 + version，9 个营养/换算键显式 null | 200；九字段清为 null、版本 +1；过敏原组原样保留（显式清空 ≠ 字段缺失） | P0 | server/internal/api/foods_test.go `TestFoodAPI022` |
| FOOD-API-023 | allergens 显式 null 被拒且无 panic（回归） | 已有食材 | PUT 与 POST 提交 `"allergens":null`（键存在） | 均 400 INVALID_ARGUMENT（requireFoodInput 兜底）；服务不 panic、原数据不变 | P0 | server/internal/api/foods_test.go `TestFoodAPI023` |
| FOOD-API-024 | Swagger required 与后端校验一致（回归） | 读取 server/docs/swagger.json | 断言 api.foodInput.required 覆盖 foodRequiredKeys 全部 14 键且均存在于 properties；foodUpdateInput.required 含 version | schema 与后端必需键永不漂移；10 个可空字段保留 x-nullable | P1 | server/internal/api/foods_test.go `TestFoodAPI024` |

---

## 5. 安全（FOOD-SEC）

节首共用前置条件：同第 4 节（httptest 服务 + admin/alice）。「事务内复核」断言要点：复核发生在写事务内读取最新数据（先 SQL 改状态再发请求即可验证读到最新值），且复核失败时无任何写入与审计残留。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-SEC-001 | 未登录写操作 401 | 无 Bearer 的浏览器会话 | POST /api/admin/foods、PUT /api/admin/foods/:id | 401（沿用现有认证错误码与结构） | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-002 | 普通用户写操作 403 | alice（active、已完成强制改密）已登录 | alice 发 POST /api/admin/foods、PUT /api/admin/foods/:id | 均 403 FORBIDDEN；无食材行、无审计行 | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-003 | 普通用户读管理接口 403 | alice 已登录 | GET /api/admin/foods；GET /api/admin/resource-audit-logs | 均 403 FORBIDDEN（含公共审计表的越权红线） | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-004 | 事务内降权复核 | 先将 admin 的角色 SQL 直接改为普通用户 | admin 会话发 PUT /api/admin/foods/:id | 403 FORBIDDEN；写事务内复核读到最新角色；食材、version、审计均无变化 | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-005 | 事务内禁用复核 | 先将 admin 账号 SQL 置 disabled | admin 会话发 POST /api/admin/foods | 403 FORBIDDEN；无写入残留 | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-006 | 身份代次与头不一致 | alice 已登录 | A. X-Expected-User-ID 填 admin 的 ID；B. 会话 auth_version 在服务端递增后用旧 access 重放写入 | A → 409 IDENTITY_CHANGED；B → 401 AUTH_REQUIRED（Session 已随 auth_version 递增被撤销，旧 access 与 refresh 均失效、不可刷新重试，见 docs/user/API.md 401 错误码表） | P0 | server/internal/api/foods_test.go |
| FOOD-SEC-007 | 强制改密未完成沿用现有错误 | 新注册、未完成强制改密的账号登录 | 访问 GET /api/food-catalog 与 POST /api/admin/foods | 均 403 PASSWORD_CHANGE_REQUIRED（沿用现有账号模块错误码，见 docs/user/API.md；与 /api/me 写接口白名单行为一致），不得放行 | P1 | server/internal/api/foods_test.go |
| FOOD-SEC-008 | access 过期刷新重试边界 | alice 的 access 即将过期 | A. 仅 access 过期且身份不变 → 前端刷新后重试一次；B. 对 503 与 409 VERSION_CONFLICT 的写请求观察重放行为 | A 成功且仅重试一次；B 不自动重放写入（网络错误、503、版本冲突不得自动重试，方案 §2） | P0 | web/src/api.test.js |
| FOOD-SEC-009 | 停用账号访问新接口 | 将 alice 置 disabled 后携旧 access 访问 | GET /api/food-catalog、GET /api/foods/:id | 403（沿用现有中间件行为，新接口不绕过） | P2 | server/internal/api/foods_test.go |

---

## 6. 兼容性红线（FOOD-COMPAT）

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-COMPAT-001 | GET /api/foods 响应逐字节不变 | v2 库（升级前）与 v3 库（升级后）各起一次服务 | 分别 GET /api/foods 并比对响应 | 仅 active、按 ID 升序、无数组外分页包裹、每个元素仅 id/name/status 三字段；两次响应逐字节一致 | P0 | server/internal/api/foods_test.go |
| FOOD-COMPAT-002 | 原账号审计接口不回归 | 升级后的 v3 库，账号审计已有历史数据 | GET /api/admin/audit-logs 及其既有参数组合 | 响应形状、筛选、分页与升级前一致；既有用例全部通过；account 审计未混入 resource 事件 | P0 | server/internal/api/api_test.go（既有用例回归） |
| FOOD-COMPAT-003 | PreferencesView 设置页保存不回归 | 前端构建与隔离 e2e 服务就绪 | 运行既有设置页单测与 user-module.spec.js 中的保存/回显用例 | 忌口目录仍来自 GET /foods 原契约；保存、reload 回显行为不变 | P0 | web/e2e/user-module.spec.js（既有用例回归）+ web/src/views/PreferencesView 相关单测 |
| FOOD-COMPAT-004 | 停用忌口保留（user_preferences） | 用户偏好已勾选食材 X | 管理员停用 X → 用户 reload 设置页 | excluded_food_ids_json 仍含 X；设置页 X 仍勾选；不物理删除 | P0 | server/internal/store/foods_test.go + web/e2e/food-module.spec.js |
| FOOD-COMPAT-005 | Swagger opblock 计数随新接口更新 | 新路由注册完成 | 运行 web/e2e/swagger.spec.js | 该 spec 的 opblock 总数断言按新增 7 个路由更新——此为实现任务（非兼容红线），实现时同步修改断言并记录 | P1 | web/e2e/swagger.spec.js |

---

## 7. 前端单元（FOOD-WEB）

节首共用前置条件：Vitest node 环境（`web/src/*.test.js` 及视图同目录测试），mock `web/src/api.js` 的请求模块。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-WEB-001 | FoodCatalogView 三态与分页 | mock API | 依次 mock 加载中、失败、空列表、多页数据 | 三态文案齐备；失败含 role=alert；翻页触发正确 page/page_size/q 参数 | P1 | web/src/views/FoodCatalogView.test.js |
| FOOD-WEB-002 | FoodsView 表单分组与默认值 | mock API | 渲染新建表单 | 基本/营养来源/换算/过敏原四分组齐备；完整性默认未确认（不预勾选「已核对」）；停用提示含「影响新增引用」文案 | P1 | web/src/views/FoodsView.test.js |
| FOOD-WEB-003 | FoodsView 客户端校验 | mock API | 输入：营养组只填两项、来源填纯空白、系数填 0 或超上限、PUT 缺 version | 提交被阻止并显示对应提示；不发出请求 | P1 | web/src/views/FoodsView.test.js |
| FOOD-WEB-004 | 并发冲突提示重新加载 | mock API 返回 409 VERSION_CONFLICT | 编辑保存 | 界面提示冲突并引导重新加载；不自动重放；不覆盖他人修改 | P1 | web/src/views/FoodsView.test.js |
| FOOD-WEB-005 | api.js 刷新重试策略 | mock fetch/定时 | A. 401 且身份不变 → 刷新后重试一次；B. 503、409、网络错误 | A 重试一次成功；B 直接失败上抛，不重放写入 | P0 | web/src/api.test.js（扩展现有文件） |
| FOOD-WEB-006 | ResourceAuditView 只读渲染 | mock API | 渲染列表与筛选 | 只读展示、无写操作入口；筛选参数透传；账号审计页面（AuditView）入口与内容不受影响 | P2 | web/src/views/ResourceAuditView.test.js |
| FOOD-WEB-007 | 维护约束文案与无障碍 | 渲染 FoodsView | 检查静态结构 | 界面含「同一 ID 必须始终指代同一种食材及加工形态」的维护说明；错误 role=alert；输入均有 label；启停确认可键盘完成 | P2 | web/src/views/FoodsView.test.js |

---

## 8. E2E（FOOD-E2E）

节首共用前置条件：`web/e2e/server.mjs` 启动隔离服务于 127.0.0.1:18080，夹具 `web/e2e/fixtures.json`，种子管理员脚本 `web/e2e/seed-admin.py`；新用例写入 `web/e2e/food-module.spec.js`。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| FOOD-E2E-001 | 主链路：新增→检索→停用→忌口保留 | 种子管理员与普通用户就绪 | 管理员新增含营养/过敏原食材 → 普通用户在 /foods 检索到 → 管理员停用 → 普通用户刷新目录与设置页 | 检索可见 → 停用后目录不再显示 → 设置页旧忌口仍勾选且可保存 | P0 | web/e2e/food-module.spec.js |
| FOOD-E2E-002 | 未知信息明确显示 | 存在无营养/无换算/过敏原未核对的食材 | 普通用户查看目录与详情 | 显示「未知/不完整」类提示；不把未知营养显示为 0；不默认 1ml=1g | P0 | web/e2e/food-module.spec.js |
| FOOD-E2E-003 | 并发修改提示重新加载 | 两个管理员会话打开同一食材 | A 编辑保存 → B 编辑后保存 | B 收到冲突提示并可重新加载最新数据；B 的修改未静默覆盖 A | P1 | web/e2e/food-module.spec.js |
| FOOD-E2E-004 | 编辑与启停确认可键盘操作 | 种子管理员登录 /admin/foods | 编辑保存；启停走确认对话框；全程键盘操作 | 保存成功；确认对话框可 Enter/Tab 完成；焦点可见 | P1 | web/e2e/food-module.spec.js |
| FOOD-E2E-005 | 资源审计页面联动 | 已发生创建/更新/停用/启用事件 | 管理员打开 /admin/resource-audit 并按类型筛选 | 按时间倒序（id DESC）看到四类事件；筛选生效；账号审计页面内容不变 | P1 | web/e2e/food-module.spec.js |
| FOOD-E2E-006 | 移动端视口可用 | 390×844 视口（参照 user-module FE-002 模式） | 普通用户浏览 /foods、管理员浏览 /admin/foods | 无横向滚动；列表、详情、分页可操作 | P2 | web/e2e/food-module.spec.js |
| FOOD-E2E-007 | 编辑对象切换的失败态与保存目标固定（回归） | 管理员登录；食材 A、B 就绪 | 正常编辑 A → route 拦截 B 的详情 GET 注入 500 → 切换编辑 B → 断言无 A 残留数据且保存禁用 → 恢复后重新编辑 B → 数据正确并保存成功（消息含 B） | 失败态不残留上一目标数据、禁止保存；详情成功后表单与目标一致；保存目标与内容来自同一快照 | P0 | web/e2e/food-module.spec.js |

---

## 9. 运行矩阵

实现完成后按顺序执行；Go 命令必须使用 `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go` 形式（shell 的 GOROOT 指向旧版本，直接 `go` 会编译失败）。命令均在仓库根目录执行。

| 命令 | 验证的用例组 |
| --- | --- |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./...` | FOOD-LOGIC 全部、FOOD-STORE 全部、FOOD-MIG 全部、FOOD-API 全部、FOOD-SEC 全部、FOOD-COMPAT-001/002/004 后端部分 |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go vet ./...` | 静态检查（非用例；实现代码卫生门槛） |
| `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test -race ./...` | 并发相关用例：FOOD-STORE-005/007/010/011、FOOD-API 写路径与 FOOD-SEC-004/005 |
| `npm test --prefix web` | FOOD-WEB 全部、FOOD-SEC-008、FOOD-COMPAT-003 单测部分 |
| `npm run build --prefix web` | 前端构建通过（FOOD-WEB 各落点文件可编译、路由与导航同步无遗漏） |
| `(cd web && npx playwright install chromium && npm run test:e2e)` | FOOD-E2E 全部、FOOD-COMPAT-003/005（含既有 user-module、swagger 回归） |
| `python3 scripts/verify_cli.py --go go` | FOOD-MIG-011（CLI 打开 v3 库）及 CLI 恢复/进程锁回归 |

补充说明：
- 数据是否有合法来源须人工核对，自动测试不能代替授权核查（方案 §8）。
- FOOD-API-014 中食谱 256 KiB 的全量实测在食谱模块路由落地后补齐；本阶段以限制来源函数单测覆盖。
- 本模块承担的迁移器重写用例（FOOD-MIG 全组）同时是库存 v4、食谱 v5 接入同一迁移器的公共基线，后续模块文档不再重复。
