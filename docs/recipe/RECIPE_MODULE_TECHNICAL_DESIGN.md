# FreshPlate 食谱管理模块技术方案

状态：待实现设计。编写日期：2026-10-19。本文覆盖食谱管理，不包含推荐或 AI 生成。

## 1. 目标、依赖与边界

支持用户维护自己的私有食谱，管理员独立创建和发布公共食谱，登录用户查看已发布公共食谱。结构化保存份数、耗时、步骤、原料和附加过敏原，为后续推荐与烹饪提供输入。

依赖[食材基础数据](../food/FOOD_MODULE_TECHNICAL_DESIGN.md)及其资源审计；复用[库存方案](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md)的数量字符串/千分单位 helper，按实施顺序在 v4 后部署 v5。共用食材方案的认证、安全、分页、时间和响应契约。

首版不做推荐排序、库存匹配、AI、图片上传、收藏、评论、用户投稿、私有转公共、复制公共食谱、食谱版本历史、烹饪扣减或营养汇总。用户可以手动新建私有食谱；公共食谱只由管理员从独立表单创建。

## 2. 权限与状态

| 操作 | 普通用户 | 管理员 |
| --- | --- | --- |
| 查看 published 公共食谱 | 允许 | 允许 |
| 查看公共 draft/archived | 禁止 | 仅管理员接口允许 |
| 创建、查看、编辑、归档私有食谱 | 仅本人 | 仅本人 |
| 管理公共食谱 | 禁止 | 允许 |
| 查看他人的私有食谱 | 禁止 | 禁止 |

visibility 固定为 private/public，创建路径决定，之后不可修改；private 的 owner_user_id 必须非空，public 必须为空。管理员创建的公共食谱不属于其个人账号，降权或禁用该管理员不改变公共资源归属。

状态统一为 draft/published/archived：新建始终 draft；draft→published、published→draft、draft/published→archived、archived→draft 均允许，archived→published 必须先回到 draft。私有 published 在界面显示“已完成”，仍只本人可见；公共 published 显示“已发布”。

归档取代物理删除，保留食谱 ID 供后续记餐引用；不提供 DELETE。published 公共食谱编辑成功后立即生效，不做另存草稿/多版本发布机制；管理员可先撤回 draft 再编辑。

## 3. 数据模型

### 3.1 `recipes`

| 字段 | 类型 / 约束 | 含义 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | 食谱 ID |
| `visibility` | TEXT NOT NULL，private/public | 不可修改 |
| `owner_user_id` | INTEGER NULL，users 外键 RESTRICT | 与 visibility 联合 CHECK |
| `status` | TEXT NOT NULL，draft/published/archived | 默认 draft |
| `name` | TEXT NOT NULL | TrimSpace 后 1～100 码点；不唯一 |
| `servings` | INTEGER NOT NULL，1～100 | 整份食谱对应的份数 |
| `duration_minutes` | INTEGER NOT NULL，1～1440 | 预计总耗时 |
| `steps_json` | TEXT NOT NULL | 按顺序保存纯文本步骤数组 |
| `source` | TEXT NOT NULL DEFAULT '' | 来源说明或 URL，最多 1000 码点 |
| `allergen_complete` | INTEGER NOT NULL，0/1 | 作者是否核对食谱层面的额外风险 |
| `allergen_source` | TEXT NULL | 核对依据，最多 500 码点；complete=1 时必填 |
| `version` | INTEGER NOT NULL DEFAULT 1，>0 | 父级及所有子项共同版本 |
| `created_at/updated_at` | INTEGER NOT NULL | UTC Unix 秒 |

联合 CHECK：`(visibility='private' AND owner_user_id IS NOT NULL) OR (visibility='public' AND owner_user_id IS NULL)`。索引 `(owner_user_id,status,id)`、`(visibility,status,id)`。

### 3.2 `recipe_ingredients`

| 字段 | 类型 / 约束 | 含义 |
| --- | --- | --- |
| `recipe_id` | INTEGER NOT NULL，recipes 外键 RESTRICT | 父食谱 |
| `position` | INTEGER NOT NULL，1～100 | 顺序，与 recipe_id 构成主键 |
| `food_id` | INTEGER NOT NULL，foods 外键 RESTRICT | 食材 |
| `quantity_milli` | INTEGER NOT NULL，1～1000000000 | 整份食谱用量 ×1000 |
| `unit` | TEXT NOT NULL，g/ml/piece | 原始单位 |

UNIQUE(recipe_id,food_id)，同一食谱同一食材只出现一次；不同阶段用量合计填写，在步骤中说明分配，不引入分组/可选原料。建议加 `(food_id,recipe_id)` 索引用于引用核对。

### 3.3 `recipe_allergens`

`recipe_id` 为 recipes 外键 RESTRICT，`allergen_code` 为已支持的 10 类之一，联合主键 `(recipe_id,allergen_code)`。这张表只保存作者额外标注，不复制 foods 中已有标注；代码及数据库 CHECK 均限制编码。

有效过敏原为“所有关联 food 的当前 allergens ∪ 食谱附加 allergens”。有效完整性为“食谱 allergen_complete=true 且所有关联 food 的 allergen_complete=true”。食谱修改不能把食材已有过敏原移除。

食谱仅持久化原始输入，不保存营养总计、可烹饪标记或静态安全结论，防止食材变更后读取旧结果。

## 4. 校验与发布规则

- 请求所有字段完整；steps 为 0～30 个文本步骤，每步 TrimSpace 后 1～1000 码点；ingredients 为 0～100 个条目。draft 可暂时没有步骤或食材，但 name、servings、duration 仍必须有效；published 至少一个步骤和一个食材。
- 创建/更新请求体上限为 256 KiB，其他接口保持 64 KiB，具体路由范围见食材方案 §2。中文步骤 30×1000 码点仅正文即可达 90000 字节，不能套用原 64 KiB。中间件预检、流式读取和解码必须使用同一限制，超限统一返回 `400 INVALID_ARGUMENT`；字段上限和编码后请求体总字节上限分别校验。
- 食材用量 >0，沿用库存的 JSON 十进制字符串，最多三位小数；拒绝数字、指数、负数、重复 food_id、未知单位。响应 position 由数组顺序生成，输入不接受 position。
- source、步骤、名称均为纯文本，拒绝无效 UTF-8 和不适合正文的控制字符；步骤允许换行。Vue 用文本插值，不用 v-html、不自动解析 Markdown；来源首版以文本显示，避免链接协议风险。
- 新增引用必须为 active food；draft/archived 编辑可保留原有 disabled food_id 及修改其用量，界面警示；不能新增 disabled 引用，也不能把它当成 published 的有效食材。
- 新发布、或保持 published 的任何编辑，必须所有食材 active、食材过敏原完整、食谱自身核对完整且有依据。营养或换算不完整不阻止发布，但响应要明确显示不可完整计算。
- 发布验证只检查基础数据和完整性，不套用发布者个人忌口；公共食谱是公共资料。详情展示不等于推荐适合食用，未来推荐/烹饪仍需当前用户的 `EnforceRestrictions`。
- 食材发布后被停用或变为过敏原不完整，不自动改写食谱状态；读取详情时重新派生 `restriction_metadata_complete`、`has_disabled_foods`。后续推荐/烹饪不得把它当作可用候选。食谱列表显示相应警示，避免“已发布=始终安全”。

本模块不推断“低盐/素食”标签：现有数据不足以可靠证明，不能仅由食谱名称猜测。未列出的特殊过敏风险仍不在支持范围内。

## 5. API

路径均相对 `/api`。不得提供可枚举他人 owner_user_id 的查询条件。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /recipes?scope=public\|mine&q=&status=&page=1&page_size=20` | scope 默认 public，仅 published 公共；mine 返回本人私有，status 留空全部状态 |
| `GET /recipes/:id` | 已发布公共或本人私有；其他情况 404 |
| `POST /recipes` | 创建本人 private/draft，201 |
| `PUT /recipes/:id` | 更新本人私有内容与状态，200；不能修改公共或他人食谱 |
| `GET /admin/recipes?q=&status=&page=1&page_size=20` | 管理员查询全部状态公共食谱，不含任何人的私有食谱 |
| `GET /admin/recipes/:id` | 管理员读取任意状态公共食谱 |
| `POST /admin/recipes` | 创建 public/draft，201 |
| `PUT /admin/recipes/:id` | 管理员编辑公共内容、发布、撤回和归档，200 |

公共 scope 不接受 status 查询值；管理员看草稿走管理员接口。q 为最多 100 码点的名称字面子串；所有列表按 id DESC。列表 items 为 id/name/visibility/status/servings/duration_minutes/version/updated_at，以及派生的 restriction_metadata_complete、has_disabled_foods；不包含全量步骤。

派生字段随当前页在同一读事务内计算，查询次数不得随食谱数或原料数逐条增长。可用 SQL EXISTS、聚合、批量 JOIN 或批量 IN 查询完成，不指定唯一 SQL 方案，不逐食谱/逐原料查询 food；默认每页 20、最大 100 的范围都应覆盖。

POST 不接收 visibility、owner_user_id、status、version，其他输入字段全部必填；PUT 增加 status 和旧 version，其他字段同 POST。归档也提交完整 PUT，不增加单独发布/归档接口。

创建私有草稿示例，食材 ID 以实际目录为准：

```json
{
  "name": "番茄炒蛋", "servings": 2, "duration_minutes": 15,
  "steps": ["洗净番茄并切块。", "鸡蛋炒熟后加入番茄继续翻炒。"],
  "source": "个人记录",
  "ingredients": [
    {"food_id": 11, "quantity": "250", "unit": "g"},
    {"food_id": 1, "quantity": "2", "unit": "piece"}
  ],
  "additional_allergens": [], "allergen_complete": false, "allergen_source": null
}
```

详情返回全部输入字段、id/visibility/status/version/created_at/updated_at；ingredients 额外返回 position 和 food 的 id/name/status；追加 effective_allergens、restriction_metadata_complete、has_disabled_foods。不返回他人身份或内部 quantity_milli。详情及派生信息在同一只读事务内读取，避免食材更新导致前后不一致；不在前端拼出安全结论。

错误：非法输入 400 INVALID_ARGUMENT；无效新增食材 400 FOOD_UNAVAILABLE；不满足发布条件 409 RECIPE_NOT_PUBLISHABLE；非法状态转换 409 INVALID_RECIPE_TRANSITION；版本不符 409 VERSION_CONFLICT。普通用户访问 `/admin/recipes` 系列接口由角色中间件返回 `403 FORBIDDEN`；已登录用户通过普通 GET/PUT 访问他人私有食谱、通过普通 PUT 修改公共食谱，以及普通 GET 访问未发布/已撤回公共食谱，均返回 `404 NOT_FOUND`。管理员接口访问私有食谱同样为 404；资源不存在也为 404。其他身份和数据库错误沿用现有契约。

## 6. 事务、并发及审计

保存时在 `WithImmediate` 内完成：

1. 复核会话、账号状态、强制改密和路径要求的角色。
2. 按资源类型及所有权读取父食谱：私有路径必须 `visibility='private' AND owner_user_id=?`；管理员路径必须 public。校验旧 version 和状态转换。
3. 在同一连接读取当前原料集合、所有请求食材状态和完整性；校验保留停用引用的例外及发布约束。
4. 条件更新父行并 version+1；删除该食谱旧 ingredients/allergens 子项并按请求重新插入。任何子项、来源、状态变化均算同一父版本变化，不对外开放子项独立写入。
5. 公共资源变更写入食材方案的 resource_audit_logs；任何失败全部回滚。完全无变化的 PUT 返回原详情、不加版本、不写审计。

新建同样在单事务内写父子表；不在事务内做网络请求或占用第二个数据库连接。重复请求不能造成父食谱内容与部分子项混合。公共食谱更新/撤回与公共详情读取，以数据库事务保证一致视图。

公共审计 action 为 recipe_created、recipe_updated、recipe_published、recipe_unpublished、recipe_archived、recipe_restored。同次请求优先记录状态动作，无状态变化记录 updated；一笔变更一条审计。details 仅含变更字段、前后版本/状态，不含完整步骤或个人偏好。私有食谱不写管理员可见审计。

POST 网络失败后需先查询核对再决定重提；不自动合并重名食谱，也不承诺创建幂等。未来烹饪必须再次读取当前配方、食材和当前用户限制，不能仅凭食谱 version 判断食材元数据是否变化；营养与名称历史由记餐快照解决。

## 7. 页面与代码落点

- `RecipesView.vue`（`/recipes`）：公共/我的分页列表、检索和我的状态筛选；有创建私有食谱入口。
- `RecipeDetailView.vue`（`/recipes/:id`）：份数、耗时、原料、步骤、来源和数据不完整提示；只有本人的私有食谱显示编辑入口。未提供烹饪按钮，避免形成未实现流程。
- `RecipeEditView.vue`（`/recipes/new`、`/recipes/:id/edit`）：增删及上移/下移原料/步骤；不用拖拽依赖；搜索 active 食材、编辑字符串数量、核对额外过敏原及依据，分别“保存草稿”“标记完成/发布”“归档”。草稿完整性默认 false。
- 管理员 `/admin/recipes`、`/admin/recipes/new`、`/admin/recipes/:id/edit` 使用同一组食谱视图，路由 meta/props 指明公共管理模式，后端路径仍独立检查权限；不让客户端自由提交 visibility。
- 发布按钮先展示校验失败原因；冲突保留草稿供用户比较并重新加载，不静默覆盖。账号变化清理表单；全部组件沿用身份代次、防连点、加载/失败/空状态及无障碍规则。
- 新增 `api/recipes.go`、`logic/recipes.go`、`store/recipes.go` 及 DTO/响应类型；复用数量 helper、已有过敏原编码和 `EnforceRestrictions` 的完整性语义。管理保存不能调用发布者的个人限制过滤。
- 更新 router/App 导航、Swagger、资源审计动作显示和文档；不新建推荐引擎、通用 CRUD 或富文本编辑器。

## 8. 迁移与实施顺序

新增 `005_recipes.sql`，创建三张表、外键、CHECK 和索引，登记到食材模块扩展后的迁移器及结构校验。全新库顺序执行 001～005；v2/v3/v4 库补齐缺失迁移。原用户、偏好、食材、库存、会话和审计不改写。

管理员 CLI 支持打开 v5；仍要求停服备份和启动前迁移，不提供自动降级。测试夹具用明确的食材标注和合法来源说明构造，不把迁移内 12 种名称自动认定为可发布依据。正式公共食谱来源授权、配料完整性及首批食材标注需要维护者核对；没有就先保留草稿。

实施顺序：表与父子事务 → 所有权/发布校验 → 私有管理 API → 公共管理及审计 → 列表详情/编辑页面 → 端到端与文档。无需等待 AI、营养趋势或记餐模块。

## 9. 验收与测试

| 层次 | 必须覆盖 |
| --- | --- |
| 模型 | private/public 所有权 CHECK、父子外键、重复食材、非正用量、非法单位和状态 |
| logic | 空草稿、发布所需字段、完整性与来源、已知/未知过敏原合并、所有状态转换及数量边界 |
| store | 父子替换失败整体回滚、审计失败回滚、版本竞争、食材并发停用/改标注、无变化不写审计 |
| 访问控制 | A 无法查看或编辑 B 私有食谱；管理员同样不能；公共草稿/归档不可被普通详情读取；伪造 owner/visibility 拒绝 |
| 安全语义 | 管理者忌口不改变公共配方；food 过敏原不能被食谱移除；发布后食材变未知/停用会在读取时更新警示；未知营养不当作零 |
| API/前端 | 纯文本脚本不执行、分页/筛选、列表派生查询无逐条 N+1、未知字段、超过64 KiB但不超过256 KiB的合法食谱写入、中间件/解码器对有无Content-Length的256 KiB超限请求均返回400 INVALID_ARGUMENT、其他接口仍限64 KiB、资源级GET/PUT越权404与管理员路由角色403、父版本冲突、身份切换、草稿和发布按钮权限 |
| E2E | 管理员建公共草稿→普通用户不可见→补齐标注并发布→普通用户可读→撤回后不可读；双用户私有食谱隔离；归档与恢复 |
| 升级回归 | 空库及 v2/v3/v4 升级、重启、CLI、旧账号/偏好/食材/库存完整保留 |

实施后运行 Go test/vet/race、前端单测/构建、隔离 Playwright 与 CLI 验证。验收以食谱数据管理和可见性闭环为准；食谱能发布不代表推荐、烹饪安全检查、库存扣减或营养估算已经实现。
