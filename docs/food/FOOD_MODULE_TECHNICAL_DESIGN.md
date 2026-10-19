# FreshPlate 食材基础数据模块技术方案

状态：待实现设计。编写日期：2026-10-19。本文不代表接口或数据迁移已经交付。

## 1. 目标、现状与范围

为库存、食谱和后续营养计算提供同一套公共食材 ID、单位换算、营养来源及过敏原标注。

当前 `foods` 只有 `id/name/status` 和 12 条名称数据；`GET /api/foods` 返回 active 食材数组，个人设置用它选择忌口。已有名称没有营养或过敏原依据，升级不能自动标记为“无过敏原”或填写推测的营养值。

首版交付：登录用户检索、查看食材；管理员创建、编辑、启停食材；可选营养和换算信息；可区分未知与已确认的过敏原数据；管理操作审计。暂不做图片、别名库、分类树、条码、批量导入、第三方实时同步、食材合并和物理删除。

依赖现有账号模块；后续由[库存模块](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md)及[食谱模块](../recipe/RECIPE_MODULE_TECHNICAL_DESIGN.md)引用。数据来源的授权和首批数据由维护者确认；未确认的数据可保持未知，不阻止维护库存。

## 2. 共用技术与安全约定

- 沿用 Go 1.26.4、Gin、SQLite、Vue 3；API → logic → store，不增加 ORM、状态管理库或外部服务。
- 认证以[当前 JWT 契约](../user/JWT_AUTHENTICATION.md)为准，不沿用旧设计中的 Session Cookie 认证。业务写操作提交 Bearer 和表单加载时的 `X-Expected-User-ID`；普通业务写接口不另加 CSRF，登录、刷新、注销规则保持现状。
- 所有新增业务入口要求有效登录、active 账号和已完成强制改密。管理员权限由后端决定，写事务内重新检查会话、账号状态和角色。
- 复用 `api.decode` 的 JSON、有效 UTF-8、拒绝未知字段及尾随 JSON 规则。请求体默认上限 64 KiB；仅食谱创建/更新的四类路由（`POST /api/recipes`、`PUT /api/recipes/:id` 及对应 `/api/admin/recipes` 路由）放宽至 256 KiB。按已匹配路由和方法确定限制，不按 URL 前缀泛化；`context` 中间件的 Content-Length 预检、流式 `MaxBytesReader` 及 `decode` 必须使用同一限制来源。不能仅放宽解码器，因为中间件已包裹请求体。超限统一返回 `400 INVALID_ARGUMENT`，无 Content-Length 的流式请求同样受限。ID 必须为正整数。时间存 UTC Unix 秒，输出 RFC 3339 UTC。
- 成功响应为 `{"data":...}`，分页对象为 `{"items":[],"total":0}`；失败使用现有 `error.code/message`。列表默认每页 20、最大 100，按 ID 倒序，名称使用参数化的字面子串查询，不把输入解释成 SQL 通配符。
- 复用 `WithImmediate`；写入、版本校验、关联检查和审计在同一连接、同一事务内完成。禁止事务内再次从单连接池取连接。读取列表与 total 使用同一只读事务。
- 复用 `api.js`、`auth`、`verifyForm` 和身份代次检查。仅现有的 access 过期且身份不变场景允许刷新后重试；网络错误、503、版本冲突不得自动重放写入。

以上规则也适用于另外两个方案。

## 3. 数据模型

在原 `foods` 表增列，保留原 ID、名称、状态与唯一约束。

| 字段 | 存储类型 / 约束 | 含义 |
| --- | --- | --- |
| `id/name/status` | 保留现有定义 | status 仍为 active/disabled |
| `energy_kcal_100g` | REAL NULL | 每 100 克可食部能量 |
| `protein_g_100g/fat_g_100g/carbohydrate_g_100g` | REAL NULL | 每 100 克可食部宏量营养素 |
| `nutrition_source` | TEXT NULL | 数据集名称、版本或来源说明，最多 500 码点 |
| `nutrition_reference` | TEXT NULL | 可核对的条目编号或来源 URL，最多 1000 码点 |
| `density_g_ml` | REAL NULL，> 0 | 每毫升对应的克数 |
| `piece_weight_g` | REAL NULL，> 0 | 每个对应的可食部克数 |
| `conversion_source` | TEXT NULL | 换算依据，最多 500 码点 |
| `allergens_json` | TEXT NOT NULL DEFAULT '[]' | 已知过敏原编码，复用现有 10 种编码 |
| `allergen_complete` | INTEGER NOT NULL DEFAULT 0，CHECK IN (0,1) | 是否已针对支持的编码完成核对 |
| `allergen_source` | TEXT NULL | 标注依据，最多 500 码点 |
| `version` | INTEGER NOT NULL DEFAULT 1，> 0 | 乐观并发版本 |
| `created_at/updated_at` | INTEGER NOT NULL | 迁移旧记录时设为迁移时间，并说明非真实创建时间 |

数据库 CHECK 同步限制非负营养值和完整组：四项营养值及两个营养来源字段全部为空，或四项全部有值且来源非空。换算系数存在时必须有换算依据；`allergen_complete=1` 时必须有标注依据。应用额外拒绝 NaN/Inf、无效编码和超长字符串。

营养完整组约束使用以下明确的两个分支；单项范围约束另行保留。SQLite 的 CHECK 允许 NULL 结果通过，因此完整分支必须显式检查每项 IS NOT NULL，不能只写比较或非空字符串判断：

```sql
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
)
```

两个来源字段均须填写，空字符串不等于 NULL；应用还须用 TrimSpace 拒绝仅含 Unicode 空白的来源，不能只依赖 SQLite trim。

### 3.1 名称与身份

名称 TrimSpace 后为 1～100 个 Unicode 码点，禁止控制字符，按 SQLite 现有二进制唯一性判断重复，不引入 Unicode 归一化或模糊合并。名称冲突映射为 `409 FOOD_NAME_TAKEN`。

同一个 ID 必须始终指代同一种食材及加工形态。名称可纠正错字，不能把“鸡蛋”改成“鸭蛋”，或将生食材改成熟食材；这些情况创建新记录并停用旧记录。界面明确说明这一维护约束。首版不自动识别语义变更；这是需要管理员遵守的限制。

### 3.2 营养与换算

- 营养信息整组填写或整组为 null；null 表示未知，0 表示有来源证明为零。首版有意不收录部分营养组，来源缺项时整组保持未知，不补零或推测数值，也不增加“仅能量”特例。能量范围 0～1000 kcal/100g；三项营养素分别 0～100 g/100g，不额外强制三项相加或能量公式相等。
- 单位固定为 `g`、`ml`、`piece`，显示为克、毫升、个。数量契约见库存方案：JSON 十进制字符串，最多三位小数，内部以千分单位整数保存；食材模块本身不解析库存数量，不依赖该 helper，后续库存和食谱实现时共享。
- `g` 可直接计算质量；`ml` 仅在有 density 时换算；`piece` 仅在有 piece_weight 时换算。两个系数分别限制在 0 < 值 ≤ 100、0 < 值 ≤ 1000000，最多六位小数，拒绝非有限值。
- 无换算依据时仍可按原单位保存库存或食谱；后续展示“无法换算/营养信息不完整”，不能默认 1ml=1g，也不能把未知营养当成零。
- 换算表示估算的可食部质量，不推算去皮、烹饪损耗或熟重。不同加工形态使用不同食材 ID。
- 库存原始数量用整数保证精确增减；营养和换算系数保留 REAL，属于估算数据，不承诺十进制位级精确或用作库存扣减依据，无须统一为整数存储。

### 3.3 过敏原

数组去重，最多 10 个已支持编码；`[] + false` 为未知，`[] + true` 为在支持范围内已核对无标注项；非空数组且 complete=false 表示部分已知。撤销完整性时可以保留已知编码。

仅核对支持的 10 类，不能宣称覆盖交叉污染或特殊过敏。未来推荐和烹饪必须把完整性传给 `EnforceRestrictions`，未知信息应拒绝作为通过限制检查的依据。营养不完整与过敏原不完整分别处理。

## 4. API 契约与兼容性

所有路径下表均相对 `/api`。

| 方法与路径 | 权限 | 行为 |
| --- | --- | --- |
| `GET /foods` | 登录用户 | 完全保留当前契约：active、ID 升序、无分页、仅 id/name/status 数组 |
| `GET /food-catalog?q=&page=1&page_size=20` | 登录用户 | 新增分页检索，只返回 active 食材摘要 |
| `GET /foods/:id` | 登录用户 | 食材完整详情；disabled 也可读以解释既有引用 |
| `GET /admin/foods?q=&status=&page=1&page_size=20` | 管理员 | 查询全部状态；status 留空不过滤 |
| `POST /admin/foods` | 管理员 | 创建，201 返回详情，version=1 |
| `PUT /admin/foods/:id` | 管理员 | 全量替换可编辑字段及状态，200 返回详情 |
| `GET /admin/resource-audit-logs?resource_type=&resource_id=&action=&page=1&page_size=20` | 管理员 | 食材与后续公共食谱管理审计，资源类型仅 food/recipe |

`food-catalog` 和管理员列表摘要返回 id/name/status、version 和过敏原完整标记，不携带全部营养来源；详情返回全部业务字段。查询字符串 q TrimSpace 后最多 100 码点；无效状态、页码、ID 返回 400。分页复用现有上限，不因增加新接口改变旧接口行为。

POST 必须提交 name、status 及本方案所有营养、换算、过敏原输入字段，未知信息显式传 null 或 false；PUT 同样提交完整输入并增加当前 version。id、时间等服务端字段不能传入。字符串来源空白不等于有效来源。成功修改时 version+1；无变化返回原详情，不加版本、不写审计。

创建示例（仅名称已知）：

```json
{
  "name": "燕麦片", "status": "active",
  "energy_kcal_100g": null, "protein_g_100g": null,
  "fat_g_100g": null, "carbohydrate_g_100g": null,
  "nutrition_source": null, "nutrition_reference": null,
  "density_g_ml": null, "piece_weight_g": null, "conversion_source": null,
  "allergens": [], "allergen_complete": false, "allergen_source": null
}
```

错误：无权限 403 FORBIDDEN；不存在 404 NOT_FOUND；重名 409 FOOD_NAME_TAKEN；版本不符 409 VERSION_CONFLICT；输入错误 400 INVALID_ARGUMENT；事务繁忙沿用 503 DATABASE_BUSY。认证、身份变化及强制改密沿用现有错误。

## 5. 停用、并发与审计

停用不物理删除，不清除用户忌口，也不级联删除库存、食谱。新增库存及新增食谱引用只能选择 active 食材；既有引用的编辑例外在对应方案中定义。重新启用保留 ID。

写事务先复核管理员，再读取当前版本并比较。版本不一致返回 409；实际更新 SQL 同时限制 `id/version`，影响行数必须为 1。业务变更与审计任何一步失败都回滚。

现有 `audit_logs.target_user_id` 非空，专用于账号操作，不能将操作者伪装为目标用户。新增一张窄用途的 `resource_audit_logs`，供本模块及公共食谱复用，保留原账号审计接口及结构：

| 字段 | 定义 |
| --- | --- |
| `id` | INTEGER PRIMARY KEY |
| `actor_user_id` | 非空 users 外键，ON DELETE RESTRICT |
| `resource_type/resource_id` | food 或 recipe、正整数资源 ID；资源不物理删除 |
| `action` | food_created / food_updated / food_disabled / food_enabled；食谱编码见食谱方案 |
| `details_json` | 变更字段名称数组、前后状态及前后版本，不保存完整请求或用户饮食数据 |
| `created_at` | UTC Unix 秒 |

索引 `(resource_type,resource_id,id)`；按 id DESC 分页。JSON 由 store 编码，资源类型和 action 由业务代码固定生成；不建通用审计框架、不提供写入或删除 API。

食材更新不能覆盖食谱自身 version，但必须影响下一次食谱读取/校验时使用的营养和过敏原结果，不能信任食谱中缓存的旧安全标记。历史记餐快照由将来的记餐模块保存，本阶段不生成。

## 6. 前端及代码落点

- 新建 `FoodCatalogView.vue`（`/foods`）及管理员 `FoodsView.vue`（`/admin/foods`）：分页检索、详情、未知信息提示；管理员有新建、编辑和启停确认。
- 表单分为基本信息、营养来源、换算、过敏原。完整性默认未确认；不能预勾选“已核对”。停用提示会影响新增引用。
- 新建 `ResourceAuditView.vue`（`/admin/resource-audit`），复用现有列表交互模式，只读展示资源管理事件。账号审计页面继续原有职责。
- `PreferencesView.vue`、`Food` 摘要及 `GET /foods` 不改响应形状；新增详情类型，避免扩展现有摘要导致设置页依赖变化。现有全量忌口目录适合本地首批数据，目录明显增大后再替换为搜索交互。
- API：`api/foods.go`、输入 DTO、路由注册和 Swagger 注释；logic：`logic/foods.go` 做校验；store：`store/foods.go` 和资源审计查询。原 store `Foods` 方法保留以供忌口页面使用。
- 同步 `router.js`、`App.vue` 导航、必要样式及文档，不增加新依赖。列表具备加载/失败/空状态；错误用 role=alert，输入有 label，启停可键盘操作。

## 7. 迁移与发布

1. 新增 `003_foods.sql`，增列及资源审计表；不修改已应用的 001/002 内容与校验和。12 条旧记录的营养/换算为空、过敏原不完整、version=1，已有忌口 ID 保持原值。
2. `store/db.go` 当前硬编码只支持 1/2：扩展为顺序校验并执行已知迁移的实现，保留未知旧库拒绝、未来版本拒绝、历史校验和检查、事务回滚及结构检查。更新 foods 列与新增表/索引的启动校验。仅增加 SQL 文件不会被当前迁移器自动执行。
3. 服务和管理员 CLI 都经 `store.Open`，必须都能打开新版本。停服、一致性备份数据库及 JWT/CSRF 密钥后升级；失败不继续启动。
4. 空库运行 001→003，已有 v2 库升级到 v3；不清空用户、不撤销正常会话。后续库存 v4、食谱 v5 顺序接入同一迁移器。
5. 不提供自动 down migration；回退使用配套旧程序、完整数据库和密钥备份，会丢失备份后新增数据，须明确决定。

## 8. 测试与验收

| 层次 | 必须覆盖 |
| --- | --- |
| logic | 营养全空/完整/部分缺失、零与未知、两个来源缺失/空字符串/仅空白、边界和非有限数、名称规范、非法编码、未知与已核对无过敏原、缺来源的系数 |
| store | 营养 CHECK 拒绝部分组和缺失/空来源、重名并发、版本冲突、审计失败回滚、停用保留忌口、食材更新与关联写入串行检查 |
| 迁移 | 空库、v2 升级、重复启动、原 ID/偏好保留、校验和/未来版本/损坏结构拒绝、CLI 打开新库 |
| API | 未登录、普通用户写入、事务内降权/禁用/身份变化、未知字段/大请求、详情读取停用项、分页与错误映射；食谱写路由 256 KiB 与其他路由 64 KiB 同时覆盖 Content-Length 及无长度流式请求，超限均为 400 INVALID_ARGUMENT |
| 兼容性 | 原 `/foods` 数组与字段不变，设置页保存、停用忌口保留、原账号审计仍通过 |
| 前端/E2E | 管理员新增→普通用户检索→停用→旧忌口保留；未知信息明确显示；并发修改提示重新加载 |

实施后运行 Go test/vet/race、前端单测与构建、隔离 Playwright 和 CLI 验证；更新 Swagger、README、实施计划和实际验证记录。数据是否有合法来源须人工核对，自动测试不能代替授权核查。

验收条件：原用户模块无回归；旧食材 ID 完整保留；普通用户不能管理公共数据；未标注数据不会成为安全或营养计算的已知值。
