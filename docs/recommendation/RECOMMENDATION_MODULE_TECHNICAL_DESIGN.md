# FreshPlate 推荐模块技术方案

状态：待实现设计。编写日期：2026-10-21；2026-10-23 由 Codex 按双方互评共识修订，最终评审见 [评审结论](RECOMMENDATION_MODULE_FINAL_REVIEW.md)。本文覆盖确定性推荐（安全过滤、库存匹配、排序与缺料说明）；AI 步骤生成只定义接入约束，不选型。

## 1. 目标、依赖与边界

为登录用户基于本人当前有效库存推荐"能做、接近能做"的食谱：以程序强制过滤过敏原与忌口，按缺料数、临期食材和耗时排序，并逐条说明缺什么、缺多少。推荐是纯只读计算，不新增任何业务表，不持久化推荐结果，不写任何审计。

依赖全部就绪：用户认证与强制改密；[食材基础数据](../food/FOOD_MODULE_TECHNICAL_DESIGN.md) v3 的过敏原编码、完整性标注与换算字段（`density_g_ml`、`piece_weight_g`）；[库存方案](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md) v4 的有效批次语义与 `expiry_state` 判定；[食谱方案](../recipe/RECIPE_MODULE_TECHNICAL_DESIGN.md) v5 的候选范围、有效过敏原派生与派生字段；`user_preferences` 的忌口与过敏原。复用 `logic/preferences.go` 的 `EnforceRestrictions`（其注释已声明供推荐复用；元数据不完整 fail-closed）、`logic/quantity.go` 的数量解析/格式化 helper、分页契约与服务端 `today` 注入。

首版不做：AI 生成（见 §5 约束）、烹饪确认与库存扣减（阶段 5 记餐模块）、营养估算与汇总、按 `dietary_preferences` 过滤（食谱无标签数据，且食谱方案禁止由名称猜"素食/低盐"）、收藏/评分/点击反馈、多食谱组合凑库存优化、购物清单、推荐结果缓存或后台任务、通知推送。推荐结果不做个性化学习——排序规则固定且可解释。

## 2. 候选范围与安全过滤

候选集合 = `visibility='public' AND status='published'` 的公共食谱 ∪ `visibility='private' AND owner_user_id=? AND status='published'` 的本人私有食谱。draft/archived 不进入推荐：draft 可能没有食材或步骤，无法匹配；published 经发布校验保证至少一个步骤和一个食材；私有 published 在界面上本就表示"已完成"。他人私有食谱在任何路径下都不出现。

候选还须至少存在一条 `recipe_ingredients` 行；该前提在计入 `stats.candidates` 之前检查。published 发布校验已保证食材非空，推荐读取仍保留此防御性检查；无食材记录不计入候选或过滤计数，不把“全部食材停用”当作无食材。

请求级先调用 `ValidatePreferences` 校验偏好，失败走错误路径，不计入食谱过滤。有效候选按以下固定顺序经过三项安全判定，首次命中即排除并终止，每个候选最多计入一个过滤计数：

| 顺序 | 条件 | 计数与依据 |
| --- | --- | --- |
| 1 | `restriction_metadata_complete=false` | `filtered_incomplete_metadata`；元数据不完整 fail-closed，未知按不安全 |
| 2 | 元数据完整时调用 `EnforceRestrictions` 失败：有效过敏原（关联 food 的过敏原 ∪ 食谱附加过敏原）与用户过敏原相交，或任一食材 ∈ `excluded_food_ids`，或过敏原编码未知 | `filtered_by_restrictions`；复用 `logic/preferences.go`，保留函数签名、不解析错误文案；未知编码归本项而非元数据不完整项 |
| 3 | `has_disabled_foods=true`：引用了停用食材 | `filtered_disabled_reference`；不做“停用食材匹配停用库存批次”的例外语义 |

被排除的食谱不进入列表，也不逐条返回排除明细；排除计数进入响应 `stats`，前端可显示“另有 N 个食谱因过敏原/忌口、信息不完整或停用引用未列入”。食谱详情接口不受过滤影响（详情展示 ≠ 推荐适合），详情响应已有的派生警示字段足以让前端对含忌口/过敏原或元数据不完整的食谱显示警示。停用食材的库存批次不属于 §3.1 有效批次集合，不参与匹配——引用该食材的食谱不可能通过全部安全判定。

忌口与过敏原变更在下一次请求立即生效：推荐不缓存任何用户的过滤结果。管理员没有额外视角——管理员登录后看到的推荐同样只基于其本人库存与偏好，公共食谱的管理能力与推荐无关。

## 3. 匹配与排序

### 3.1 食材可用量与状态

对候选食谱的每个食材，取本人有效库存批次（`user_id=? AND deleted_at IS NULL AND quantity_milli>0`，且满足到期有效条件 `expires_on IS NULL OR expires_on >= today`，且关联食材 `status='active'`）按 `food_id` 聚合；该集合（下称"有效批次集合"）是本节匹配、§3.2 `uses_expiring` 与 §4 `has_usable_inventory` 共同引用的唯一集合定义。追加 `status='active'` 条件不改变任何候选的匹配结果：§2 第 3 项已排除引用停用食材的候选，候选食材必为 active，该条件仅使 `has_usable_inventory` 语义唯一。`today` 为 §3.2/§5 共用的同一次注入服务端 UTC 日期，边界与库存模块 `ExpiryState` 一致：`expires_on < today`（库存语义 expired）不计入聚合；到期日当天（expiring）计入；`expires_on IS NULL`（unknown）视为可用。该到期过滤按 §5 在内存完成（批量取出已含 `expires_on`），不给 store 查询引入 today 绑定。食材仅存在过期批次时按无可用库存处理，缺料说明不区分"从未购买"与"仅有已过期批次"，不新增第五态。

每条批次相对食谱申报单位的可换算性单独判定：批次单位与申报单位相同则恒可换算（系数 1，与食材换算字段是否齐全无关）；跨单位时按换算表判定——`g`↔`ml` 需 `density_g_ml`，`piece`↔`g` 需 `piece_weight_g`，`piece`↔`ml` 经克中转、两个系数缺一不可；缺任一所需系数即该条批次不可换算。

确认量 `confirmed` = 全部可换算批次换算到申报单位后逐条向下取整到 milli，再求和（整数毫单位运算，不经过 float64）。不可换算批次不参与 `confirmed`，既不被当作零库存，也不按猜测系数换算。`need` 用食谱原值，不参与取整。

数量契约区分两层：单批输入契约不变——库存批次与食谱用量均 ≤ 10^9 milli，由既有解析与 CHECK 保证；换算与聚合适用输出契约。记 `q` 为批次的整数 milli 数量，`pw_int = round(piece_weight_g × 10^6)`、`den_int = round(density_g_ml × 10^6)`。既有校验保证系数至多六位小数；整数化先四舍五入到最近整数，再复核正值与范围，不截断 float64 余量。合法整数范围为 `1 ≤ pw_int ≤ 10^12`、`1 ≤ den_int ≤ 10^8`。

同单位直接取 `q`。跨单位的完整整数公式如下，除法均向下取整，复合换算不在克中间单位取整；每条批次仅在最终申报单位向下取整一次：

| 批次→申报单位 | 最终 milli 数量公式 | 单批未饱和理论最大值 |
| --- | --- | --- |
| piece→g | `q × pw_int / 10^6` | 10^15 |
| g→ml | `q × 10^6 / den_int` | 10^15 |
| piece→ml | `q × pw_int / den_int` | 10^21 |
| g→piece | `q × 10^6 / pw_int` | 10^15 |
| ml→g | `q × den_int / 10^6` | 10^11 |
| ml→piece | `q × den_int / pw_int` | 10^17 |

乘除以 128 位中间精度精确承载（如 Go `math/bits` 的 Mul64/Div64）；合法输入域内最大分子乘积为 10^21，中间量不溢出、也不触发饱和。不得先除后乘丢失余数：例如 `q=1000` milli-g、`pw_int=50000000` 时，g→piece 正确结果为 20 milli-piece，先算 `q/pw_int` 会错误归零。

取 `cap = 10^15` milli（10^12 原单位）。单批最终值及聚合值真实结果达到 cap 时向下饱和到 cap，低于 cap 时精确返回。cap 是输出限幅值，不是六方向共同的数学上界。除法前以 128 位比较分子与 `cap × 分母`，达到阈值直接返回 cap，不执行可能使 64 位商溢出的 Div64；阈值比较乘积至多 10^27，仍在 128 位范围内。中间乘积超过 int64、最终值未达 cap 时必须精确返回，不得饱和。换算后不足 1 milli 的批次对 confirmed 贡献为 0。

聚合从 `confirmed=0` 开始；每条可换算批次得到已限幅的非负整数 `batch` 后，执行 `confirmed += min(batch, cap-confirmed)`（或先比较 `batch >= cap-confirmed` 再决定饱和或相加），保持 `0 ≤ confirmed ≤ cap`，不得先无保护求和再限幅。到 cap 后无需继续增加 confirmed，但临期标记等其他判定仍须完整计算。该累加结果为 `min(逐条取整后的真实总量, cap)`，与批次数及遍历顺序无关，不需 128 位聚合。

饱和只向下夹取：`need ≤ 10^9 milli < cap`，饱和值 ≤ 真实值，饱和与真实值对 ok/short 的比较结果恒一致；数值超界仍是信息完备的可判定情形，不视为 unknown，unknown 仅保留给所需换算系数缺失或不完整的情形。`lack = need − confirmed` 仅在 short 时输出，此时 `0 < lack ≤ 10^9 milli`，无溢出可能。

匹配批次集合为空时，候选所有食材一律判 `missing`，先于任何换算与 unknown 判定。其余状态按序判定、互斥单选：

| state | 条件 |
| --- | --- |
| `ok` | `confirmed ≥ need`，与是否存在不可换算批次无关；`have` 返回 `confirmed`（已确认量下限） |
| `unknown` | `confirmed < need` 且存在不可换算批次，`have/lack` 返回 null |
| `short` | `confirmed < need`、全部批次可换算且 `confirmed > 0`，返回 `lack = need − confirmed` |
| `missing` | `confirmed < need`、全部批次可换算且 `confirmed = 0`（无有效批次，或逐条换算后不足 1 milli） |

该判定随库存增长单调：新增可换算批次只会改善状态；新增不可换算批次只会把 `confirmed < need` 的 `short`/`missing` 保守降级为 `unknown`（fail-closed，如实反映"缺多少无法确认"），永远不会推翻既有 `ok`。

`can_cook = 所有食材均为 ok`。`missing_count = short + missing + unknown` 的食材数（即非 ok 食材数）。

### 3.2 临期加权与排序

`uses_expiring = true` 当且仅当候选食谱的任一所需食材，在本人满足 §3.1 到期条件的库存批次中存在 `expiry_state='expiring'` 者。判定复用库存模块同一 `today`（服务端每次请求注入一次）与 3 天阈值，不得另设一套日期规则；该食材即使同时缺料也算（烹饪会消耗部分临期批次）。临期加权只看批次存在性，与该批次可否换算无关。

排序固定为四个键，不加权、不打分：

1. `missing_count` 升序（可立即做的排最前）；
2. `uses_expiring` 降序（能用掉临期批次的优先）；
3. `duration_minutes` 升序；
4. `id` 降序，保证稳定。

加权评分（如"匹配度 87%"）被明确拒绝：权重不可解释、给人伪精确感，且库存语义下缺料数已是最直接的可操作信号。此顺序是对全站"id DESC"列表约定的受控例外，推荐列表的价值在顺序本身；分页契约不变（默认 20、最大 100）。

推荐列表管线顺序固定为：候选前提检查与三项安全过滤（§2）→ 名称检索 `q` → 匹配计算（四态判定与 `can_cook`）→ `availability` 筛选 → 排序 → `total` 计数 → 分页。`total` 为 `q` 与 `availability` 全部生效后、分页前的条数，与 `items` 出自同一只读事务快照，任一页都不会出现因筛选产生的可点击空页；`stats` 为安全过滤阶段的计数，与 `q`、`availability` 及排序均无关。

### 3.3 规模与检索

`q` 经 TrimSpace 后为最多 100 码点的名称字面子串，参数化 `instr(name, ?)>0`，在匹配计算前施加以减少无关候选；`total` 随 `q` 缩小，`stats` 计数不受 `q` 影响。全量候选内存计算后分页：单机试用规模下候选为数十到数百、每食谱食材 ≤100，一次请求为常数次批量查询加一次内存计算；该假设不成立时再讨论物化，首版不做。列表项内嵌全量匹配明细（含 `ok` 食材），最坏 20×100 项，换取不再逐条二次请求。

## 4. API

路径相对 `/api`，要求登录且非强制改密。只读，无写接口，无审计。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /recommendations?q=&availability=all&page=1&page_size=20` | 本人视角的推荐列表 |

HTTP 响应体遵循全站契约 `{"data":{"items":[…],"total":N,"stats":{…}}}`；本节的"顶层"、列表项结构与 `stats` 示例均描述 `data` 内层对象。

`availability` 取值 `all|can_cook`，默认 `all`：`all` 返回全部通过安全过滤与 `q` 的候选；`can_cook` 仅返回 `can_cook=true` 的条目。筛选在 §3.2 管线中匹配计算之后、`total` 计数与分页之前施加；非法取值返回 400 `INVALID_ARGUMENT`。

列表项结构示例：

```json
{
  "recipe": {"id": 81, "name": "番茄炒蛋", "visibility": "public",
             "servings": 2, "duration_minutes": 15, "version": 7},
  "can_cook": false,
  "missing_count": 1,
  "uses_expiring": true,
  "ingredients": [
    {"food": {"id": 11, "name": "番茄", "status": "active"},
     "state": "ok", "unit": "g", "need": "250", "have": "600", "lack": null},
    {"food": {"id": 1, "name": "鸡蛋", "status": "active"},
     "state": "short", "unit": "piece", "need": "2", "have": "1", "lack": "1"}
  ]
}
```

示例自洽性：`can_cook=false`、`missing_count=1`、仅鸡蛋一个非 ok 食材，三者互相一致；`uses_expiring=true` 与鸡蛋 short 并存合法——鸡蛋存在临期批次，依 §3.2"该食材即使同时缺料也算"。

`need/have/lack` 均为规范化数量字符串，单位为食谱申报单位；`have` 允许超过单批 10^6 原单位的输入上限（聚合与饱和规则见 §3.1）；`unknown` 时 `have/lack` 为 null。`recipe.version` 是匹配所用配方版本，食谱任何内容、状态或子项（ingredients/附加过敏原）变化均使服务端 version+1，推荐仅只读透传；携带它的唯一用途是 §6 的详情复用一致性校验，不代表推荐暴露管理字段，摘要仍不含步骤、来源、派生警示与审计字段。

`stats` 为 `{"candidates":12,"filtered_by_restrictions":3,"filtered_incomplete_metadata":1,"filtered_disabled_reference":1,"has_usable_inventory":true}`：`candidates` 是满足 §2 可见性、published 与非空食材前提后、安全过滤之前的总数；三项 `filtered_*` 按 §2 首次命中归属，互斥且总和不超过 `candidates`，两者之差恰为安全过滤幸存者数。这四项均与 `q`、`availability` 无关；`has_usable_inventory` 是 §3.1 有效批次集合（含到期条件与食材 active 条件）的非空判定，在同一只读事务内内存完成，不新增查询。

`GET /recipes/:id` 保持不变、不追加匹配信息——匹配只在推荐上下文提供，避免同一逻辑两处实现。

错误沿用既有契约：分页、`q` 或 `availability` 非法 400 `INVALID_ARGUMENT`；未登录 401、强制改密 403、数据库繁忙 503 `DATABASE_BUSY`。推荐不产生资源级 404/409，也没有身份断言头（无写操作）。

## 5. 事务、一致性与 AI 接入约束

单只读事务内完成全部读取，查询次数为常数、不随食谱数或食材数增长（与食谱列表派生字段同一约束，禁止逐食谱/逐食材 N+1）：用户偏好、本人有效库存批次（有效即 §3.1 有效批次集合，到期与食材 active 过滤在内存完成；取出含 `expires_on` 及关联食材 `status`，经与库存列表同式的 foods JOIN，不增加查询次数）、候选食谱及全部 `recipe_ingredients`、关联 foods 的过敏原/完整性/换算字段，全部批量取出后在内存完成过滤、匹配与饱和运算。WAL 读快照保证事务中途的食材停用、库存编辑或偏好修改不影响本次响应的一致性。

推荐每次请求全量重算，无缓存、无物化、无定时任务；临期判定与响应共用同一次注入的 `today`。

AI 约束（为阶段 4 后半留位，本文不选型，提供商与费用上限按实施方案 §9 待定）：

1. AI 只允许对已通过 §2 安全过滤的候选生成文本（易读步骤、替代食材建议）。
2. AI 输出中的任何食材引用与过敏原标注，必须重新经 `EnforceRestrictions` 与数据校验；不通过的结果不展示。
3. 模型 API 不可用时推荐列表必须完整可用，生成内容整体降级隐藏——AI 是增强，不是依赖。
4. AI 永不负责过敏原判定、`can_cook` 判定或缺料数量；这些只来自 §3 的确定性计算。
5. AI 未接入前，本模块按纯确定性推荐验收。

## 6. 前端与代码落点

- `RecommendationsView.vue`（`/recommendations`）加入 `App.vue` 导航：列表项显示名称、可做/缺 N 样徽标、临期徽标、耗时与缺料食材摘要（"缺 鸡蛋×1、酱油 未知"）；文字加颜色表达状态，不只靠颜色。
- "仅可立即做"筛选走服务端 `availability` 参数，翻页与 `total` 同源，不做页内二次过滤；检索或筛选变更时重置到第 1 页。展示 stats（"另有 N 个食谱因忌口、信息不完整或停用引用未列入"，N=三项过滤计数之和）。
- 空态判定（三类互斥，按序命中，一律以本次响应的 `stats` 与 `total` 为准、不依据 `items`——异常越界分页下 `items` 可为空而 `total>0`，以 `total` 判空避免误报）：一、`stats.candidates=0` → "暂无可推荐的食谱"，引导去食谱页创建或发布；二、`candidates>0` 且三项过滤计数之和等于 `candidates`（安全过滤后剩余为零）→ "你的 N 个食谱因过敏原/忌口、信息不完整或引用停用食材未列入"（N=`candidates`；此时关闭 `q`/`availability` 列表仍为空，归因于安全过滤即完整原因）；三、`total=0`（必有安全过滤幸存者，空态仅由展示筛选造成）按筛选组合提示：`q` 生效（TrimSpace 后非空）且 `availability=all` → "无匹配结果"，引导修改或清空搜索；`q` 为空且 `availability=can_cook` → "暂无可立即做的食谱"，引导关闭筛选查看缺料清单；两者同时生效 → "没有匹配「q」且可立即做的食谱"，不强行归因单一原因，引导调整任一条件。不新增任何 `q` 相关计数字段，`stats` 维持与筛选无关的定义。另设独立横幅，不参与上述互斥判定：`has_usable_inventory=false` 时列表顶部常驻"当前没有可用于推荐的库存"并链接 /inventory；横幅与一至三任一空态叠加显示、互不替代，横幅下方列表照常渲染（此时全部食材为 `missing`，缺料清单对采购仍有参考价值）。
- 从推荐进入详情时，先比较详情响应的 `version` 与列表快照记录的 `recipe.version`：一致则用列表已得的匹配数据渲染提示，不重复请求；不一致则整份丢弃匹配提示（can_cook、缺料摘要、食材明细全部不渲染），显示中性提示"该食谱在推荐加载后已被修改，请返回并刷新推荐"，不得部分渲染旧数据。无列表快照时（直接打开详情 URL 或刷新页面）不显示匹配提示，与"匹配只在推荐上下文提供"一致。
- 快照语义：匹配提示是上次推荐加载时点的快照。库存增减、忌口与过敏原修改、食材停用、食材过敏原或换算系数变化均不升食谱 version，也不会回写快照——version 校验只保证"匹配所对应的配方与详情页展示的配方一致"，不保证匹配对当前库存与偏好仍然准确，后者由快照标注与刷新重算兜底。快照仅存于前端内存，不写入浏览器存储、不上传服务端，与 §1"不持久化推荐结果"一致；刷新推荐即重新计算。
- 点击进入既有 `/recipes/:id` 详情；详情页不显示烹饪按钮（流程未实现，避免死入口）。
- 沿用 `UsersView.vue` 列表骨架：请求序列计数器、身份代次快照、防连点、loading/空/失败/503/网络异常处理；不使用 v-html；切换账号后不展示旧账号数据。
- 新增 `api/recommendations.go`、`logic/recommendations.go`、`store/recommendations.go` 及 DTO。logic 为纯函数：输入候选、库存、偏好与 `today`，输出过滤排序结果，不做任何 IO，便于表驱动单测；单位换算为 logic 内新纯函数 helper（含 §3.1 的 128 位中间精度与饱和规则），复用 `quantity.go` 与 `EnforceRestrictions`；store 只做批量只读查询。不新建推荐引擎抽象或策略框架。
- 新增 `logic.ValidateRecommendationQuery` 校验 `q`（TrimSpace 后至多 100 码点）与 `availability` 枚举；分页复用 API 的 `page()`，默认 20、每页最多 100、页码最多 10^6，非法参数统一 400 `INVALID_ARGUMENT`。
- 更新 `router.js`、Swagger 与文档；推荐 handler 的 Swagger 注解沿用既有 `@Success 200 {object} response{data=…}` 形式，保证生成结果与响应契约一致；沿用现有 api/auth helper，不加任何 npm 依赖。

## 7. 迁移与实施顺序

无新表、无迁移：schema 停留在 v5，迁移链、启动结构校验、`migrate_test.go`、管理员 CLI 均不变。这是首个零迁移模块，实施说明中应明确"不新增 `schema_migrations` 记录"，避免照搬前三模块的迁移步骤。

实施顺序：换算与匹配纯函数及单测 → store 批量查询与只读事务 → logic 过滤/排序 → API 与 Swagger → 页面 → 隔离端到端与文档更新（`IMPLEMENTATION_PLAN.md` 阶段 4 链接、`docs/test/` 测试用例与执行计划）。可完全独立于 AI 与记餐模块交付。

## 8. 验收与测试

| 层次 | 必须覆盖 |
| --- | --- |
| logic 过滤 | 过敏原交集、忌口命中、元数据不完整 fail-closed、停用引用排除、draft/他人私有绝不出现；同一候选同时命中元数据不完整、限制、停用时仅按固定优先级计一次；未知过敏原编码归限制计数；偏好校验失败走错误路径、不计过滤；无食材行不计入 candidates；stats 计数与候选遍历顺序无关，不受 `q`/`availability` 影响；三项过滤计数之和加幸存者数恒等于 candidates |
| logic 匹配 | 恰好相等、差 1 milli、多批次同单位聚合、g↔ml↔piece 全组合换算、系数缺失 unknown、零数量与逻辑删除批次不计入、逐条向下取整（含换算后不足 1 milli 判 missing 的边界）、`lack` 字符串往返；同单位批次聚合与换算字段是否齐全无关；新增不可换算批次不推翻既有 ok、仅把 short/missing 保守降级为 unknown；`expires_on=today` 计入 have 且该食材 `uses_expiring=true`；`expires_on IS NULL` 计入 have；仅过期批次→missing、过期+未过期混合→只聚合未过期、未过期充足→ok；无批次时全 missing 优先于 unknown；极值：单批 10^9 milli-piece × piece_weight 10^6 → 恰 10^15 milli 饱和边界；piece→ml 复合换算超限 → 饱和且判定不变；两批最大同单位聚合 → have 超单批上限、格式化正常；中间溢出但最终未超限必须精确返回——99,999,999 milli-piece × piece_weight 10^6 ÷ density 100，中间乘积 ≈10^20 超 int64、先除后乘保留余数仍溢出，但真实换算值恰为 999,999,990,000 milli-ml < 10^15，have 返回 "999999990" 不饱和（若按"任何一步溢出即饱和"将虚增约 1000 倍、违反 have 下限语义）；ml→piece 极值：q=10^9、den_int=10^8、pw_int=1，真实结果 10^17 → 饱和为 10^15；g→piece：q=1、pw_int=10^12，取整为 0 → missing；g→piece 先除后乘反例 q=1000、pw_int=50000000 → 精确 20 milli-piece；聚合累加真实值 ≥ 10^15 → 饱和且判定不变，覆盖 9224 条各已饱和为 cap 的批次及 cap−1 再加 1/2 的边界，确认不先溢出后限幅、顺序不影响结果；仅有停用食材批次（未删除、正数量、未过期）→ 全部食材 missing 且 `has_usable_inventory=false` |
| 输出一致性 | `can_cook ⟺ 所有食材 state 均为 ok`；`missing_count = 非 ok 食材数`（unknown 且 have=null 计入）；表驱动单测对每条用例断言该不变量，含恰好相等时 can_cook=true 且 missing_count=0 的边界 |
| logic 排序 | 四键逐级生效、全并列时 id DESC 稳定、`uses_expiring` 的 today/today+3 边界、可注入时钟；`availability` 筛选取子集保序 |
| store | 查询次数为常数（防 N+1）、单只读事务一致快照、空库存/空候选/无偏好行为、`today` 单次注入、空库存时 `has_usable_inventory=false`、仅停用食材批次时 `has_usable_inventory=false` |
| API | `{"data":{"items","total","stats"}}` 完整响应体契约（断言含外层 data 包装）、分页默认与上限、`total` 为 `q`/`availability` 生效后分页前条数（q 命中 1/100 → total=1 且 stats.candidates=100、无空页）、`q` 字面子串不解析通配符、`availability` 非法 400、401/403/503、无越权面确认 |
| 安全语义 | 偏好修改后下一请求立即生效；管理员视角仅本人数据；停用引用与元数据不完整食谱永不推荐；`unknown` 不当作 `ok` 或零 |
| 前端 | 三类互斥空态（以 `stats`/`total` 判定）与无库存横幅叠加、互不误判（含 candidates=0、全部被安全过滤、q 命中但 availability=can_cook 清空三者的区分，q/availability 交叉用例）；详情 version 不一致时整份丢弃匹配提示（推荐加载后编辑该食谱，返回详情断言不渲染旧 can_cook/lack）；缺料摘要与 stats 展示、筛选翻页、账号切换即失效、无 v-html |
| E2E | 用户 A 录入库存（含临期批次）→ 推荐顺序正确（可做+临期在前）→ 设置忌口后对应食谱消失且 stats 变化 → 用户 B 登录看不到 A 的匹配与库存；无库存用户断言 `has_usable_inventory=false`、横幅可见且全 missing 列表仍渲染，录入一条有效库存后横幅消失；q+can_cook 组合空态断言（q 零命中与命中但均 short 两种前置下均显示组合提示）；q 命中少量候选时翻页无空页；用户新增批次后管理员停用该食材 → 断言 `has_usable_inventory=false`、横幅可见、列表全 missing 三者一致（夹具保证至少一个候选通过安全过滤） |
| 回归 | 既有 Go/Vitest/E2E/CLI 全部保持通过；迁移链仍为 001–005 |

实施后运行 Go test/vet/race、前端单测/构建、隔离 Playwright 验证并记录结果。验收以确定性推荐闭环为准：推荐可做不等于烹饪安全确认、库存扣减或营养估算已实现——它们属于记餐模块。
