# 食谱管理模块测试用例

| 项 | 内容 |
| --- | --- |
| 依据方案 | [docs/recipe/RECIPE_MODULE_TECHNICAL_DESIGN.md](../recipe/RECIPE_MODULE_TECHNICAL_DESIGN.md) |
| 编写日期 | 2026-10-19 |
| 状态 | 用例已定义，待功能实现后执行 |
| 用例总数 | 103（STORE 20 / LOGIC 19 / MIG 6 / COMPAT 3 / API 24 / SEC 16 / WEB 10 / E2E 5） |

## 1. 范围与约定

- 本文档覆盖食谱管理（recipes/recipe_ingredients/recipe_allergens）私有与公共两条链路，不含推荐、AI 生成、库存扣减与营养汇总；验收以「食谱数据管理与可见性闭环」为准。
- 用例 ID 规则：`REC-<层次缩写>-<三位序号>`，层次缩写为 LOGIC / STORE / MIG / API / SEC / COMPAT / WEB / E2E。
- 优先级：P0（发布阻断项，必须全绿）＞ P1（核心行为回归）＞ P2（边界与体验加固）。
- 「建议落点」遵循现有代码模式：后端测试与被测代码同目录；`api` 层用 `api_test.go` 的 `fixture`/`browser`（httptest 起服务、`/api/auth/csrf` + JWT 登录辅助、`status/code/data` 断言）；`store` 层用 `store_test.go` 的临时 SQLite 文件建库与 schema 篡改结构校验、`transaction_fault_test.go` 的故障注入；`logic` 层为纯函数表驱动。
- 错误码速查（方案 §5/§6 出现的全部错误码，均有对应用例）：

| HTTP | code | 触发场景 | 对应用例 |
| --- | --- | --- | --- |
| 400 | INVALID_ARGUMENT | 非法输入、请求体超限（64/256 KiB）、解码失败 | LOGIC-002/009/010/011/012/014/015/019、API-010/011/012/014/016/022/023、SEC-008 |
| 400 | FOOD_UNAVAILABLE | 新增引用 disabled/不存在食材 | LOGIC-013 |
| 401 | UNAUTHENTICATED | 未登录、旧 access token | API-020 |
| 403 | FORBIDDEN | 普通用户访问 `/api/admin/recipes` 系列；事务内角色/状态复核失败 | SEC-006/007 |
| 404 | NOT_FOUND | 他人私有、未发布/已撤回公共、管理员访问私有、资源不存在 | API-003/005/007、SEC-001/002/003/004 |
| 409 | RECIPE_NOT_PUBLISHABLE | 不满足发布条件 | LOGIC-004/005/006 |
| 409 | INVALID_RECIPE_TRANSITION | 非法状态转换（archived→published 等） | LOGIC-007/008 |
| 409 | VERSION_CONFLICT | 父版本不符 | STORE-012、API-019 |
| 409 | IDENTITY_CHANGED | `X-Expected-User-ID` 与会话身份不一致 | API-020、SEC-009 |
| 503 | DATABASE_BUSY | 库忙重试耗尽（沿用现有契约） | STORE-013、API-015 |

## 2. store 层（含数据库约束）

落点：`server/internal/store/recipes_test.go`（临时 SQLite 建库，参照 `store_test.go`）；回滚类用 `transaction_fault_test.go` 的注入方式。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-STORE-001 | 所有权联合 CHECK | 库已迁移到 v5 | 直接 INSERT：`visibility='private' AND owner_user_id IS NULL`；`visibility='public' AND owner_user_id=1` | 两类插入均因 CHECK 失败；合法组合（private+owner、public+NULL）成功 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-002 | visibility/status 枚举约束 | 同上 | INSERT visibility=`'internal'`、status=`'deleted'` | 数据库 CHECK 拒绝，仅 draft/published/archived、private/public 可写入 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-003 | 父子外键 RESTRICT | 存在 v5 库与食材目录 | 插入 ingredients 引用不存在的 recipe_id / food_id；尝试删除被引用的 recipe 与 food | 插入失败；被引用的 recipe、food 删除受 RESTRICT 限制失败 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-004 | 同食谱重复食材唯一约束 | 存在私有草稿及 1 个 active food | 同一 recipe_id 两次插入相同 food_id（position 不同） | 第二次插入触发 UNIQUE(recipe_id,food_id) 失败 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-005 | 用量毫秒值范围约束 | 同上 | INSERT quantity_milli=0、-1、1000000001 | 全部被 CHECK（1～1000000000）拒绝；1 与 1000000000 通过 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-006 | 单位 CHECK | 同上 | INSERT unit=`'kg'`、`'升'`、空串 | 拒绝；仅 `g`/`ml`/`piece` 可写入 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-007 | position 主键与范围 | 存在含 3 条原料的食谱 | 插入 position=0、101、以及与既有行重复的 (recipe_id,position) | 全部失败；position 1～100 且主键不冲突才成功 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-008 | 附加过敏原编码 CHECK | 同上 | INSERT allergen_code=`'gluten-x'`、空串 | 数据库 CHECK 拒绝；仅 10 类受支持编码可写入 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-009 | 子项重插失败整体回滚 | 存在公共草稿（父 v1）及其 2 条原料 | 在 WithImmediate 事务内：删旧子项→重插时制造失败（如重复 food_id） | 事务回滚：父行 version 仍为 1、内容不变，旧 ingredients/allergens 完整保留 | P0 | server/internal/store/recipes_test.go（注入参照 transaction_fault_test.go） |
| REC-STORE-010 | 审计写入失败回滚 | 存在公共草稿，审计表写入被注入失败 | 执行公共食谱更新（含发布） | 父行、子项全部回滚，resource_audit_logs 无记录，接口失败不产生半成品 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-011 | 部分写入不落库 | 同上，在子项重插中途注入故障（部分原料已重插） | 提交前注入 panic/error | 库中不存在「新父内容 + 旧子项」或部分子项的混合状态；重复请求可安全重试 | P0 | server/internal/store/transaction_fault_test.go 扩展 |
| REC-STORE-012 | 版本竞争：条件更新影响行数≠1 | 两个会话读到同一父 version=1 | 会话 A 更新成功 version→2；会话 B 携 version=1 进入事务 | B 的 `UPDATE ... WHERE id=? AND version=?` 影响 0 行→整体回滚，返回 409 VERSION_CONFLICT，子项未动 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-013 | 食材并发停用/改标注 | 已发布食谱引用 food X（active） | 在更新事务内重读 X 时，X 已被另一事务停用或改为过敏原不完整 | 事务按重读结果判定：新增引用被停→400 FOOD_UNAVAILABLE；保持 published 编辑不满足条件→409 RECIPE_NOT_PUBLISHABLE；库忙时按现有契约重试后 503 DATABASE_BUSY | P1 | server/internal/store/recipes_test.go |
| REC-STORE-014 | 无变化 PUT 不加版本不写审计 | 存在公共食谱 v3 | 用与当前完全相同的载荷执行 PUT | 返回原详情；version 仍为 3、updated_at 不变；resource_audit_logs 无新记录 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-015 | 列表派生批量查询无逐条 N+1 | 预置 >20 个食谱、合计 >100 条原料（食材标注混合：部分 disabled、部分过敏原不完整） | 分别以 page_size=20（默认）与 100 调用列表查询，用 SQL 钩子/计数器统计查询次数 | `restriction_metadata_complete`、`has_disabled_foods` 数值正确，且查询次数为常数级（不随页内食谱数/原料数逐条增长）；两种页大小均覆盖 | P0 | server/internal/store/recipes_test.go |
| REC-STORE-016 | 详情单只读事务一致派生 | 已发布食谱引用 food X | 在同一 BeginTx(ReadOnly) 内读详情，期间并发更新 X 的标注 | 详情内 ingredients 与 effective_allergens/派生字段来自同一快照，前后一致 | P1 | server/internal/store/recipes_test.go |
| REC-STORE-017 | 列表只读事务与排序 | 预置 25 个公共食谱 | 列表查询 page=2&page_size=10 | 只读事务内 `count(*)` total=25，items 为 id DESC 的第 11～20 条；返回 (items,total,err) | P1 | server/internal/store/recipes_test.go（参照 lists.go 的 pageBounds 模式） |
| REC-STORE-018 | 父子共享版本 | 创建含原料与附加过敏原的食谱 | 读取父行与子行 | 父 version 初值 1 且 >0；任一子项变化均体现为父 version+1，子表无独立版本 | P2 | server/internal/store/recipes_test.go |
| REC-STORE-019 | v5 索引与结构校验 | 迁移到 v5 | 校验 `(owner_user_id,status,id)`、`(visibility,status,id)`、`(food_id,recipe_id)` 索引存在；篡改 schema/checksum 后重开库 | 索引齐全；结构校验失败（参照 store_test.go 篡改用例：改 checksum、删索引等均被拒绝） | P2 | server/internal/store/store_test.go 扩展 |
| REC-STORE-020 | 归档保留食谱 ID | 存在已发布私有食谱 | 状态置 archived 后再次读取 | 行仍存在且 id 不变（供后续记餐引用），无物理删除 | P2 | server/internal/store/recipes_test.go |
| REC-REG-001 | 仅修改步骤的 PUT 必须落库（回归） | draft 含 2 步，version=1 | PUT 仅变更 steps，其余字段与 version 不变 | 200、响应 steps 为新值、version=2；重读确认落库 | P0 | server/internal/store/recipes_test.go `TestRecipeREG001_StepsOnlyPut` |
| REC-REG-002 | 更新成功后响应为事务内最新详情（回归） | 已有食谱 | PUT 同时修改 name/steps/servings/duration/source/allergen_source | 响应各字段全部等于请求值（非旧值）且 version+1 | P0 | server/internal/store/recipes_test.go `TestRecipeREG002_UpdateReturnsFreshDetail` |
| REC-REG-003 | 完全相同 PUT 保持无变化语义（回归） | 已有食谱 | 相同内容 PUT 再次提交 | 仍走无变化路径：version 不变、200 返回当前详情、不写审计 | P0 | server/internal/store/recipes_test.go `TestRecipeSTORE014_NoChangePut` |

## 3. logic 层

落点：`server/internal/logic/recipes_test.go`（纯函数表驱动，参照 `preferences_test.go`）；发布/校验规则来源为方案 §4。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-LOGIC-001 | 空草稿可创建 | 已登录普通用户 | 仅 name/servings/duration 合法，steps=[]、ingredients=[]、additional_allergens=[]、allergen_complete=false | 校验通过，创建为 private/draft（201） | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-002 | draft 必填字段仍有效 | 同上 | name=`""`/全空白/101 码点；servings=0、101；duration_minutes=0、1441；缺任一字段；另构造缺 steps/ingredients/additional_allergens/allergen_complete 任一键的载荷（键必须存在，值可为空数组/false，对照 LOGIC-001） | 全部 400 INVALID_ARGUMENT；name、servings、duration 在 draft 也不可缺；steps/ingredients/additional_allergens/allergen_complete 键缺失同样 400（方案 §4「请求所有字段完整」、§5「其他输入字段全部必填」） | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-003 | name 码点边界 | 同上 | 100 码点（含中文/组合字符）通过；101 拒绝；`"  标题  "` TrimSpace 后判定 | 100 码点通过；101 拒绝 400；两端空白被裁剪后计数 | P1 | server/internal/logic/recipes_test.go |
| REC-LOGIC-004 | published 至少一步骤一食材 | 存在无步骤或无食材的草稿 | PUT 提交 status=published | 409 RECIPE_NOT_PUBLISHABLE，错误信息指出缺失项 | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-005 | 发布完整性四条件 | 草稿含 1 步骤 1 食材 | 分别构造：引用 disabled food / food 过敏原不完整 / allergen_complete=false / complete=true 但 allergen_source 空 | 四种均 409 RECIPE_NOT_PUBLISHABLE；全部满足才允许 published | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-006 | 保持 published 的编辑需仍满足发布条件 | 已发布公共食谱 | PUT 仅改 servings，但某食材已被停用 | 409 RECIPE_NOT_PUBLISHABLE；不能以「只是小改」绕过校验 | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-007 | archived→published 禁止 | 存在 archived 食谱 | PUT status=published | 409 INVALID_RECIPE_TRANSITION；必须先 archived→draft | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-008 | 状态转换矩阵 | 覆盖 draft/published/archived 三态食谱 | 表驱动遍历：draft→published ✓、published→draft ✓、draft→archived ✓、published→archived ✓、archived→draft ✓、archived→published ✗、同名自转 ✓（无变化路径） | 允许表通过，archived→published 返回 409 INVALID_RECIPE_TRANSITION | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-009 | 数量字符串解析边界 | — | 接受：`"250"`、`"0.001"`、`"1000000"`；拒绝：`250`（数字）、`"1e3"`、`"-1"`、`"0"`、`"0.0001"`（4 位小数）、`"1000000.001"`、`""`、`"abc"` | 合法值换算 quantity_milli 正确；非法值返回 400 INVALID_ARGUMENT（沿用库存数量 helper） | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-010 | steps 边界 | — | 条目数 0、30 通过，31 拒绝；单步 1000 码点通过、1001 拒绝；空串/全空白步骤拒绝（含全空白数组，如 ["  ",""]，剔除后为 0 条时同样拒绝）；含换行步骤通过 | 越界与空白步骤一律 400 INVALID_ARGUMENT：每条目须 TrimSpace 后 1～1000 码点，不采用「剔除后为 0 步放行」的解释，draft 也不允许 | P1 | server/internal/logic/recipes_test.go |
| REC-LOGIC-011 | ingredients 条目边界 | — | 0、100 条通过；101 条拒绝；同一 food_id 出现两次拒绝 | 越界与重复 food_id 均 400 | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-012 | source 与 allergen_source 边界 | — | source 1000 码点通过、1001 拒绝；allergen_source 500 码点通过、501 拒绝；allergen_complete=true 且 allergen_source=null 拒绝；complete=false 时 source 可为 null | 越界/缺依据 400 | P1 | server/internal/logic/recipes_test.go |
| REC-LOGIC-013 | 新增引用必须 active，保留例外仅限草稿 | 草稿已引用 food X（后遭停用）、目录含 disabled food Y | 新增 Y→拒绝；保留 X 并仅改其用量→允许；把 X 当有效食材去发布→拒绝 | 新增 disabled/不存在食材 400 FOOD_UNAVAILABLE；draft/archived 编辑保留原 disabled 引用并改用量成功；published 校验拒绝 | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-014 | 有效过敏原合并与编码校验 | food X 标注「乳、蛋」，食谱附加「花生」 | 计算 effective_allergens；再附加编码 `unsupported` | 合并结果为并集且不改动 food 表；非法编码 400；食谱修改不能把 X 已有过敏原移除 | P1 | server/internal/logic/recipes_test.go |
| REC-LOGIC-015 | 控制字符与无效 UTF-8 | — | name/steps/source 注入 `\x00`、`\x1b` 及无效 UTF-8 字节序列；步骤内 `\n` | 控制字符与无效编码 400；换行允许（仅步骤字段） | P1 | server/internal/logic/recipes_test.go |
| REC-LOGIC-016 | 公共审计动作选择规则 | 公共食谱各状态 | 表驱动：创建→recipe_created；draft→published→recipe_published；published→draft→recipe_unpublished；→archived→recipe_archived；archived→draft→recipe_restored；内容变化无状态变化→recipe_updated；无变化→不写 | 同次请求优先状态动作，一笔变更仅一条审计；无变化无审计；details 只含变更字段与前后版本/状态 | P0 | server/internal/logic/recipes_test.go |
| REC-LOGIC-017 | servings/duration 合法端点 | — | servings=1、100；duration_minutes=1、1440 | 全部通过（与 LOGIC-002 的越界值成对） | P2 | server/internal/logic/recipes_test.go |
| REC-LOGIC-018 | q 检索语义 | 预置名称含「番茄」的食谱 | q=`番茄`（子串命中）、q 超 100 码点 | 子串命中；超长 400 INVALID_ARGUMENT；无通配语义 | P2 | server/internal/logic/recipes_test.go |
| REC-LOGIC-019 | 未知单位被应用层拒绝 | — | 合法数量字符串（如 `"250"`）分别配 unit=`'kg'`、`'升'`、空串 | 一律 400 INVALID_ARGUMENT（方案 §4「拒绝……未知单位」、§3.2），由应用层拦截，不得依赖数据库 CHECK 兜底后以非 400 方式失败（对照 STORE-006） | P1 | server/internal/logic/recipes_test.go |

## 4. 迁移

落点：`server/internal/store/store_test.go`（迁移与结构校验扩展）及 `scripts/verify_cli.py`（CLI 部分）。`005_recipes.sql` 按方案 §8 登记进迁移器。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-MIG-001 | 空库顺序执行 001→005 | 空文件库 | 顺序执行 001～005 全部迁移 | 三张表、外键、CHECK、索引创建成功；结构校验通过；schema_migrations 记录到 005 | P0 | server/internal/store/store_test.go |
| REC-MIG-002 | v4→v5 升级 | 构造已迁移到 v4 的库并写入用户/偏好/食材/库存数据 | 执行 005 | 旧表数据逐行不变（可 dump 对比）；recipes 三表出现；校验通过 | P0 | server/internal/store/store_test.go |
| REC-MIG-003 | 重复启动幂等 | 已迁移到 v5 的库 | 再次启动服务/迁移器 | 不重复执行、不报错，schema 无变化 | P1 | server/internal/store/store_test.go |
| REC-MIG-004 | CLI 打开 v5 | v5 库 | `python3 scripts/verify_cli.py --go go`（扩展覆盖 recipes CLI 路径），并用管理员 CLI 打开 v5 库 | CLI 正常打开并输出 schema 版本 005；不提供自动降级 | P1 | scripts/verify_cli.py（扩展） |
| REC-MIG-005 | v2/v3 库补齐缺失迁移 | 构造 v2、v3 库 | 启动迁移器至 v5 | 缺失迁移按序补齐，结果与全新库结构一致 | P1 | server/internal/store/store_test.go |
| REC-MIG-006 | 重启后 schema 稳定 | 已运行 v5 的库 | 停止后重启两次，比对 schema | 无自动降级、无新增变更，checksum 校验持续通过 | P2 | server/internal/store/store_test.go |

## 5. 兼容性与升级回归

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-COMPAT-001 | v5 升级后旧数据完整可用 | REC-MIG-002 升级前的 v4 库 | 升级后以旧账号登录，读取/写入偏好、食材、库存 | 账号、偏好、食材、库存、会话、审计全部保留且功能正常 | P0 | server/internal/store/store_test.go + server/internal/api/api_test.go 回归 |
| REC-COMPAT-002 | 既有接口行为回归 | v5 库 | 跑既有用户/偏好/食材/库存模块的 Go 测试与既有 e2e（user-module.spec.js、swagger.spec.js） | 全部通过，行为与 v4 一致 | P1 | 既有测试套件全量回归 |
| REC-COMPAT-003 | Swagger 文档随新接口更新 | 食谱接口已实现 | 打开 Swagger UI，核对 8 条 recipes 路由；运行 swagger.spec.js | 文档包含全部新路由；swagger.spec.js 的 opblock 总数断言随新接口同步更新（实现任务，注意同步修改） | P1 | web/e2e/swagger.spec.js、server/internal/api/swagger.go |

## 6. API 层

落点：`server/internal/api/recipes_test.go`（httptest + `api_test.go` 的 `fixture`/`browser` 模式）。请求体上限路由表：`POST /api/recipes`、`PUT /api/recipes/:id`、`POST /api/admin/recipes`、`PUT /api/admin/recipes/:id` 共 4 条为 256 KiB，其余全部接口（含 GET 详情、admin 列表）仍 64 KiB。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-API-001 | 公共列表默认 scope 与分页 | 管理员已发布 ≥25 个公共食谱 | `GET /api/recipes`（不带参数）；`?page=2&page_size=100` | 默认 scope=public 仅含 published 公共；默认 page=1/page_size=20；page_size>100 拒绝 400；响应 `{items,total}` | P0 | server/internal/api/recipes_test.go |
| REC-API-002 | scope=mine 与 status 筛选 | 普通用户持有 draft/published/archived 私有食谱各 1 | `GET /api/recipes?scope=mine`；`?scope=mine&status=draft`；`?status=draft`（public scope 带 status） | mine 返回本人全部状态；status 过滤生效；public scope 携 status 值拒绝 400 INVALID_ARGUMENT | P0 | server/internal/api/recipes_test.go |
| REC-API-003 | 详情可见性三元判定 | 公共 published/draft、他人私有、不存在 ID 各 1 | 依次 `GET /api/recipes/:id` | published 公共与本人私有 200；其余（公共 draft、他人私有、不存在）一律 404 NOT_FOUND，不区分原因 | P0 | server/internal/api/recipes_test.go |
| REC-API-004 | 创建私有食谱与拒收保留字段 | 已登录普通用户 | `POST /api/recipes` 合法载荷；载荷内追加 `visibility`/`owner_user_id`/`status`/`version` | 合法载荷 201 且 visibility=private、status=draft、version=1；保留字段因 DisallowUnknownFields 400 INVALID_ARGUMENT | P0 | server/internal/api/recipes_test.go |
| REC-API-005 | 更新本人私有/不可改公共 | 普通用户私有食谱 + 任一公共食谱 | `PUT /api/recipes/:id`（自己的）合法载荷；对自己可见的公共食谱同操作 | 私有更新 200、version+1；公共食谱 PUT 404 NOT_FOUND | P0 | server/internal/api/recipes_test.go |
| REC-API-006 | 管理员列表范围与角色门槛 | 公共 draft/published/archived 各 1、他人私有 1；普通用户 1 名 | 管理员 `GET /api/admin/recipes`；普通用户同请求 | 管理员可见全部状态公共食谱（不含任何私有）；普通用户 403 FORBIDDEN | P0 | server/internal/api/recipes_test.go |
| REC-API-007 | 管理员详情 | 同上 | 管理员 `GET /api/admin/recipes/:id` 分别取公共 draft 与他人私有 ID | 公共 draft/archived 200；私有 404 NOT_FOUND | P0 | server/internal/api/recipes_test.go |
| REC-API-008 | 管理员创建公共食谱 | 已登录管理员 | `POST /api/admin/recipes` 合法载荷（含尝尝追加 `visibility:'private', owner_user_id:自ID`） | 合法载荷 201、visibility=public、owner_user_id 为 NULL；保留字段 400 | P0 | server/internal/api/recipes_test.go |
| REC-API-009 | 管理员编辑/发布/撤回/归档 | 公共草稿 1 个 | `PUT /api/admin/recipes/:id` 依次：改内容、status=published、published→draft、draft→archived、archived→draft | 全部 200，状态与 version 按转换矩阵推进；archived→published 走 409（见 LOGIC-007） | P0 | server/internal/api/recipes_test.go |
| REC-API-010 | 请求体上限路由表 | 4 条食谱写路由与至少 2 条其他接口可用；已登录用户与管理员 | 对 `POST/PUT /api/recipes`、`POST/PUT /api/admin/recipes` 各发送一次最大合法载荷（约 90～105 KiB，构造方式见 API-013）；对同 4 路由再各发送 256 KiB+1 字节 | 最大合法载荷 4 条路由均不因体长被拒（201/200 正常受理）；256 KiB+1 一律 400 INVALID_ARGUMENT，预期用错误信息区分「请求体超限」与「字段校验失败」两类 400——受 name≤100、steps≤30×1000、source≤1000、allergen_source≤500 码点、ingredients≤100 条等字段上限约束，合法载荷约 105 KiB 封顶，无法构造 256 KiB 的合法内容，故不以「256 KiB 整应被接受」为断言；其他接口（GET `/api/recipes/:id`、GET `/api/admin/recipes` 等）上限仍 64 KiB，见 API-014 | P0 | server/internal/api/recipes_test.go |
| REC-API-011 | 256 KiB 超限（带 Content-Length） | 已登录管理员 | `POST /api/admin/recipes`，body=262145 字节且带准确 Content-Length | 中间件预检拦截：400 INVALID_ARGUMENT，不进入解码 | P0 | server/internal/api/recipes_test.go |
| REC-API-012 | 256 KiB 超限（无 Content-Length 流式） | 同上 | chunked 方式流式发送 >256 KiB，无 Content-Length | 流式读取路径同样 400 INVALID_ARGUMENT（中间件与解码器同一限制） | P0 | server/internal/api/recipes_test.go |
| REC-API-013 | 64 KiB～256 KiB 合法写入成功（30 条×1000 码点步骤） | 已登录管理员；预置 30 条步骤、每步约 1000 中文码点（30×1000×3B≈90 KiB 正文，满足方案 §4 steps 0～30 条、每步 1～1000 码点上限，不再使用 ≥100 条步骤） | `POST /api/admin/recipes` body 约 90 KiB 合法载荷 | 201 创建成功（证明不能沿用 64 KiB 旧限） | P0 | server/internal/api/recipes_test.go |
| REC-API-014 | 非食谱写路由仍限 64 KiB | 已登录用户 | 向 `GET /api/recipes/:id`（构造长查询）、`GET /api/admin/recipes` 及既有接口发送 >64 KiB 请求体/查询 | 一律 400 INVALID_ARGUMENT，确认放宽仅限 4 条写路由 | P1 | server/internal/api/recipes_test.go |
| REC-API-015 | 响应契约 | 各类成功/失败场景就绪 | 抽查错误、详情、列表三类响应 | 错误体 `{"error":{"code","message"}}`、详情 `{"data":...}`、列表 `{"items","total"}`；所有响应含 `Cache-Control: no-store`；库忙路径 503 DATABASE_BUSY | P1 | server/internal/api/recipes_test.go |
| REC-API-016 | 解码器拒绝矩阵 | 已登录用户 | 依次发送：非 application/json、含未知字段、合法 JSON 后带尾随 JSON、无效 UTF-8 字节 | 全部 400 INVALID_ARGUMENT（decode 路径：MaxBytesReader+UTF-8+DisallowUnknownFields+拒尾随） | P1 | server/internal/api/recipes_test.go |
| REC-API-017 | 详情响应字段契约 | 含 2 原料、附加过敏原的食谱 | `GET /api/recipes/:id` | ingredients 含 position（按数组顺序派生）与 food 的 id/name/status；含 effective_allergens、restriction_metadata_complete、has_disabled_foods；不含 quantity_milli、不含他人身份字段 | P1 | server/internal/api/recipes_test.go |
| REC-API-018 | 列表字段契约 | 已发布食谱若干 | `GET /api/recipes`、`GET /api/admin/recipes` | items 仅含 id/name/visibility/status/servings/duration_minutes/version/updated_at 与两个派生字段；不含全量步骤；按 id DESC | P1 | server/internal/api/recipes_test.go |
| REC-API-019 | 父版本冲突端到端 | 已发布公共食谱 v2 | 两个管理员会话各读 v2；A 提交 PUT 成功；B 以 version=2 提交 | B 收到 409 VERSION_CONFLICT，内容未变 | P0 | server/internal/api/recipes_test.go |
| REC-API-020 | 未登录与身份切换 | 已登录用户 A | 不带 token 访问 /api/recipes；带 A 的 token 但 `X-Expected-User-ID`=B | 无 token 401；身份不一致 409 IDENTITY_CHANGED | P0 | server/internal/api/recipes_test.go |
| REC-API-021 | 无 DELETE 路由 | 已登录用户 | `DELETE /api/recipes/:id`、`DELETE /api/admin/recipes/:id` | 路由不存在（404/405，随路由器行为），归档取代物理删除 | P2 | server/internal/api/recipes_test.go |
| REC-API-022 | 查询参数校验 | 已登录用户 | page=0、page=-1、page_size=101、page_size=`abc`、q 超 100 码点 | 全部 400 INVALID_ARGUMENT | P2 | server/internal/api/recipes_test.go |
| REC-API-023 | ingredients 条目携带 position 被拒绝 | 已登录用户与管理员 | `POST /api/recipes` 合法载荷的 ingredients 数组条目内追加 `position:1`；`POST /api/admin/recipes` 同样注入 | 均 400 INVALID_ARGUMENT（DisallowUnknownFields 同样作用于条目级未知字段；position 仅由数组顺序派生，与 API-004/005 覆盖的顶层保留字段 visibility/owner_user_id/status/version 互补，方案 §4「输入不接受 position」） | P1 | server/internal/api/recipes_test.go |
| REC-API-024 | 同名食谱可共存、不自动合并 | 已登录普通用户与管理员各 1；已存在至少一条公共食谱 | `POST /api/recipes` 连续创建两条完全同名（含 100 码点重名）的私有食谱；`POST /api/admin/recipes` 创建一条与既有公共食谱完全同名的公共食谱；分别读取详情与列表 | 每次均 201 且各自独立成行（id 不同、version 各自从 1 起），内容互不影响、不自动合并（方案 §3.1 name 不唯一、§6 不自动合并重名食谱）；私有与公共两条路径均覆盖 | P1 | server/internal/api/recipes_test.go |

## 7. 安全（访问控制与安全语义）

落点：`server/internal/api/recipes_test.go` 与 `server/internal/store/recipes_test.go`；本组为方案 §9「访问控制」「安全语义」两行 + 任务要求的安全专项，不得省略。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-SEC-001 | 用户 A 无法查看 B 的私有食谱 | A、B 各有私有食谱 | A `GET /api/recipes/{B 的 id}`；A 列表 scope=mine 查 B 的 | 404 NOT_FOUND；列表不含 B 的任何记录 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-002 | 用户 A 无法编辑 B 的私有食谱 | 同上 | A `PUT /api/recipes/{B 的 id}` 携带合法载荷与 B 当前 version | 404 NOT_FOUND（不泄露资源存在性）；B 的食谱 version 不变 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-003 | 管理员同样不能查看/编辑他人私有食谱 | 管理员 + B 的私有食谱 | 管理员 `GET/PUT /api/recipes/{B 的 id}` 与 `GET /api/admin/recipes/{B 的 id}` | 全部 404 NOT_FOUND；管理员权限不突破私有边界 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-004 | 公共 draft/archived 不可被普通详情读取 | 公共 draft 与 archived 各 1 | 普通用户 `GET /api/recipes/:id` | 均 404 NOT_FOUND；列表默认 scope 也不含 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-005 | 伪造 owner_user_id/visibility 拒绝 | 已登录普通用户与管理员 | POST/PUT 载荷注入 `owner_user_id:{他人ID}`、`visibility:'public'`（个人接口）或 `visibility:'private'`（管理接口） | 未知字段 400 INVALID_ARGUMENT；visibility/owner 由路径与角色决定，客户端不可提交 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-006 | 管理员路由角色 403 | 普通用户 token | 依次访问 GET/POST `/api/admin/recipes`、GET/PUT `/api/admin/recipes/:id` 共 4 条 | 全部 403 FORBIDDEN（进入不到资源查询） | P0 | server/internal/api/recipes_test.go |
| REC-SEC-007 | 事务内降权/禁用复核 | 已登录管理员 | 请求到达后、WithImmediate 事务复核前，将该管理员降权/禁用/强制改密 | 事务内 checkSession 复核失败，写路径拒绝（按现有契约 401/403），不产生部分写入 | P1 | server/internal/api/recipes_test.go（参照既有事务内复核用例） |
| REC-SEC-008 | 丢 body/空 body 请求 | 已登录用户 | POST/PUT 无 body、空字符串 body、Content-Length: 0 | 400 INVALID_ARGUMENT；不影响既有数据 | P1 | server/internal/api/recipes_test.go |
| REC-SEC-009 | 身份代次切换防护 | 用户 A 已登录 | 会话期间 A 在别处重新登录/登出后，旧会话携 `X-Expected-User-ID` 不匹配值调用写接口 | 409 IDENTITY_CHANGED，表单按前端约定清理 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-010 | 管理者忌口/降权不改变公共配方 | 管理员 M 发布公共食谱后，M 设置忌口或被降权 | M（或继任管理员）再次读取/编辑该公共食谱；普通用户读取 | 保存与校验不套用发布者个人忌口；降权/禁用 M 不改变公共食谱归属与内容 | P0 | server/internal/api/recipes_test.go |
| REC-SEC-011 | food 过敏原不可被食谱移除 | food X 标注「乳」，食谱引用 X 且附加 allergens=[] | 编辑食谱把附加过敏原清空并保存；读取详情 | effective_allergens 仍含「乳」；食谱编辑不写 food 表 | P0 | server/internal/logic/recipes_test.go + server/internal/api/recipes_test.go |
| REC-SEC-012 | 发布后食材变更读取时重派生 | 已发布食谱引用 active 且标注完整的 food X | 发布后将 X 停用、再改为过敏原不完整，逐次读详情与列表 | 食谱 status 不被自动改写；restriction_metadata_complete/has_disabled_foods 在读取时更新并显示警示；列表含警示标记 | P0 | server/internal/store/recipes_test.go + server/internal/api/recipes_test.go |
| REC-SEC-013 | 未知营养不当作零 | 食谱引用缺少营养数据的食材 | 读取详情 | 响应明确标注营养不可完整计算，不显示 0 值总计 | P1 | server/internal/api/recipes_test.go |
| REC-SEC-014 | 私有食谱不写管理员审计 | 普通用户创建/编辑私有食谱 | 查询 resource_audit_logs 与管理端审计视图 | 无 recipe_* 审计记录；审计动作枚举新增 recipe_created/updated/published/unpublished/archived/restored 且仅公共资源写入 | P1 | server/internal/store/recipes_test.go |
| REC-SEC-015 | 纯文本脚本不执行 | 创建步骤含 `<script>window.__xss=true</script>`、来源含 `javascript:` 的食谱 | API 存取后在前端详情页打开；检查 DOM 与 CSP 违规上报 | 存储为纯文本原样返回；页面以文本插值渲染，脚本不执行、无 v-html、无 CSP violation | P0 | server/internal/api/recipes_test.go + web/e2e/recipe-module.spec.js |
| REC-SEC-016 | 列表不接受可枚举他人的查询条件 | 用户 A、B 各持有私有与公共食谱若干 | A 以 `GET /api/recipes?owner_user_id={B 的 id}` 调用（可再叠加 scope/page/q 等组合），与不带该参数的基准响应逐条对比 | 结果集必须与基准完全一致：注入 owner_user_id 不得扩大或缩小可见范围、B 的任何记录不得出现（方案 §5「不得提供可枚举他人 owner_user_id 的查询条件」）；若实现为查询参数校验直接拒绝未知参数，则返回 400 INVALID_ARGUMENT 亦视为通过 | P0 | server/internal/api/recipes_test.go |

## 8. 前端单元（Vitest）

落点：`web/src/views/*.test.js`（node 环境，与现有 `web/src/*.test.js` 同风格）；共享数量校验若沉淀进 `web/src/validation.js` 则同步扩展 `web/src/validation.test.js`。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-WEB-001 | 列表视图切换/筛选/分页 | mock 列表接口 | RecipesView：公共/我的 tab、status 筛选、翻页、警示徽标渲染（restriction_metadata_complete=false、has_disabled_foods=true） | 请求参数正确拼装；派生警示徽标按数据显示；空态/加载/失败态齐全 | P1 | web/src/views/RecipesView.test.js |
| REC-WEB-002 | 数量字符串输入校验 | 编辑页挂载 | 输入 `0.5`、`250`、`1e3`、`-1`、`0.0001` | 前端即时拦截非法值，提示三位小数十进制字符串要求，不发请求 | P1 | web/src/views/RecipeEditView.test.js（或 validation.test.js） |
| REC-WEB-003 | 步骤/原料上移下移 | 编辑页含 ≥3 步骤/原料 | 点击上移/下移、增删按钮 | 顺序数组正确重排、position 由顺序派生；无拖拽依赖 | P2 | web/src/views/RecipeEditView.test.js |
| REC-WEB-004 | 草稿默认与发布按钮行为 | 新建草稿表单 | 检查 allergen_complete 初始 false；触发发布校验失败；连点保存 | 草稿完整性默认 false；发布按钮先展示后端校验失败原因；防连点生效 | P1 | web/src/views/RecipeEditView.test.js |
| REC-WEB-005 | 版本冲突保留草稿 | mock 409 VERSION_CONFLICT 响应 | 提交编辑 | 弹出比较/重新加载提示，本地草稿保留不静默覆盖 | P1 | web/src/views/RecipeEditView.test.js |
| REC-WEB-006 | 账号变化清理表单 | 两账号切换（身份代次） | 模拟 409 IDENTITY_CHANGED/账号切换事件 | 表单清空、回到登录/重新加载，无跨账号残留数据 | P1 | web/src/views/RecipeEditView.test.js |
| REC-WEB-007 | 步骤与来源文本插值 | 步骤/来源含 HTML 片段 | 挂载详情组件断言 DOM | 以文本节点渲染，无 v-html、不解析 Markdown；来源按纯文本显示 | P0 | web/src/views/RecipeDetailView.test.js |
| REC-WEB-008 | 管理/个人模式同一组件 | props/meta 两种模式 | 挂载编辑组件对比 | 管理模式走 /api/admin 路径，客户端不可提交 visibility；个人模式走 /api/recipes | P2 | web/src/views/RecipeEditView.test.js |
| REC-WEB-009 | 状态与无障碍 | mock 加载中/失败/空数据 | 断言角色与标签 | 加载/失败/空状态齐全；按钮/输入有无障碍标签（aria/label） | P2 | web/src/views/RecipesView.test.js |
| REC-WEB-010 | 编辑入口权限显示 | 本人私有 / 他人私有 / 公共食谱 | 挂载详情组件 | 仅本人私有食谱显示编辑入口；公共食谱不显示 | P1 | web/src/views/RecipeDetailView.test.js |
| REC-WEB-011 | 数量字符串→千分单位共享换算 | 导入 web/src/quantity.js | 表驱动：`"250"→"250000"`、`"250.5"→"250500"`、`"0.5"→"500"`、`"999999.999"→"999999999"`（不误拒）、`"1000000"→"1000000000"`、`"1000000.001"→"1000000001"`（调用方上限拒绝）、非法输入（abc/1.2345/-5/007/1e3/空串）返回空串；compareQuantity 长度/字典序 | 全部与手算千分值一致；库存/食谱两视图共用同一实现 | P0 | web/src/quantity.test.js |

## 9. 端到端（Playwright）

落点：`web/e2e/recipe-module.spec.js`（隔离服务 127.0.0.1:18080，由 `web/e2e/server.mjs` 启动；夹具 `web/e2e/fixtures.json`、种子 `web/e2e/seed-admin.py`；登录/登出辅助参照 `user-module.spec.js`）。

| 用例 ID | 标题 | 前置条件 | 步骤/输入 | 预期结果 | 优先级 | 建议落点 |
| --- | --- | --- | --- | --- | --- | --- |
| REC-E2E-001 | 公共食谱发布可见性闭环 | 管理员与普通用户各 1；食材目录就绪 | 管理员建公共草稿 → 普通用户列表/详情不可见 → 管理员补齐标注并发布 → 普通用户可读 → 管理员撤回 → 普通用户再次不可见（404/列表消失） | 各步骤可见性与方案 §2 权限表一致；发布按钮在标注不全时先展示失败原因 | P0 | web/e2e/recipe-module.spec.js |
| REC-E2E-002 | 双用户私有食谱隔离 | 浏览器双上下文 A、B | A 创建私有食谱；B 登录后直接访问该详情 URL；B 查看自己的列表 | B 详情 404；B 列表为空；A 全程可见可编辑 | P0 | web/e2e/recipe-module.spec.js |
| REC-E2E-003 | 私有食谱全生命周期 | 普通用户 1 名 | 新建私有草稿 → 编辑加步骤/原料 → 发布（界面显示「已完成」）→ 归档 → 恢复为草稿再发布 | 全流程成功；archived 后无烹饪/删除入口；恢复后再次发布通过 | P0 | web/e2e/recipe-module.spec.js |
| REC-E2E-004 | 发布失败原因与冲突比较 | 预置不完整草稿；制造版本冲突 | 点击发布（缺标注）；另开标签改同一食谱后回旧表单提交 | 失败原因逐条展示；冲突时提示比较并重新加载，草稿未被静默覆盖 | P1 | web/e2e/recipe-module.spec.js |
| REC-E2E-005 | 列表检索/分页与移动端可用性 | 预置 >20 个已发布公共食谱 | 搜索关键词、翻页；390×844 视口走查 | 检索/分页正确；无横向滚动；警示徽标在窄屏可见 | P2 | web/e2e/recipe-module.spec.js |
| REC-E2E-006 | 步骤编辑持久化与删除按钮（回归） | 管理 API 预置 2 步 2 原料公共草稿（种子食材先强制 active） | 编辑页仅修改步骤文本并保存 → 重进编辑页回读 → 点「删除第 N 步」与「删除原料」各一次 → 再次保存 | 新步骤文本持久化；行数减少且无 pageerror（删除按钮可用）；保存成功 | P0 | web/e2e/recipe-module.spec.js |

## 10. 运行矩阵

实现完成后按序执行（Go 命令必须用 `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go`，shell 的 GOROOT 指向旧版本，直接 `go` 会编译失败）：

| 命令（仓库根目录执行） | 验证的用例组 | 说明 |
| --- | --- | --- |
| `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./...)` | STORE-001～020、LOGIC-001～019、MIG-001/002/003/005/006、COMPAT-001/002、API-001～024、SEC-001～016 | 后端全量单测（含既有模块回归） |
| `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go vet ./...)` | 全部后端用例组（静态检查） | 随全量测试执行 |
| `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test -race ./...)` | STORE-009～013（事务/并发回滚）、SEC-007/009 | 竞态检测 |
| `npm test --prefix web` | WEB-001～010、COMPAT-002（既有前端单测回归） | Vitest |
| `npm run build --prefix web` | WEB-001～010（构建级验证）、COMPAT-003 | 生产构建通过 |
| `(cd web && npx playwright install chromium && npm run test:e2e)` | E2E-001～005、SEC-015（浏览器侧）、COMPAT-002/003（含 swagger.spec.js，opblock 计数断言需随新接口更新） | 隔离服务端口 18080 |
| `python3 scripts/verify_cli.py --go go` | MIG-004（CLI 打开 v5）、COMPAT-001/002 | CLI 恢复与进程锁 |

补充执行说明：

- REC-MIG-002/005 需要 v2/v3/v4 夹具库，由迁移测试自行构造（食材/库存模块落地后复用其夹具），不依赖线上库。
- REC-SEC-015 的 CSP 断言复用 `user-module.spec.js` 中 SEC-004 的 `securitypolicyviolation` 监听写法。
- 所有 P0 用例必须全绿才视为本模块验收通过；P1/P2 允许记录已知问题后由维护者裁决。
