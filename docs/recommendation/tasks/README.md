# 推荐模块实施任务索引

依据：`../RECOMMENDATION_MODULE_TECHNICAL_DESIGN.md`（2026-10-23 版）。本次交付确定性推荐闭环；AI、记餐、库存扣减、营养估算不在范围。起始工作区干净。

## 任务边界与依赖

| 任务 / 负责 Agent | 允许修改范围 | 输入与输出 | 依赖与完成条件 |
| --- | --- | --- | --- |
| 后端 / backend | `server/internal/{logic,store,api}/recommendations*.go`、API 路由与必要 DTO、Swagger 生成文件 | 当前偏好、本人库存、公共和本人 published 食谱、一次中国标准时间 today → `GET /api/recommendations` | 复用现有偏好/数量/到期规则；完成设计 §2–5、§8 后端验收及实现单测 |
| 前端 / frontend | `web/src/` 的推荐视图、路由导航、详情和内存快照及单测 | 设计 §4 响应 → 列表、过滤分页、详情快照 | 可与后端依约并行；完成 §6 空态/身份隔离/version 校验，Vitest/build 通过 |
| 独立验证 / validation | `web/e2e/recommendation-module.spec.js`、独立测试文件、`validation.md` | 设计与最终变更 → 独立清单、检查结果、缺陷 | 未参与业务实现；准备与实现并行，完整回归依赖集成；失败交实现者修复后复测 |
| 集成 / root | 本任务索引、实施计划、测试文档；必要跨模块协调 | 各任务产物 → 集成结果及交付映射 | 解决接口问题、记录决策与文档证据，不做无关重构 |
| 独立 Review / review（集成后启动） | Review 报告及只读源码检查 | 最终 diff、设计和验证结果 → 分级发现及复核 | 未参与业务实现；集成和验证后执行，阻塞发现关闭后交付 |

各 Agent 独占自己的详细日志：`backend.md`、`frontend.md`、`validation.md`、`review.md`。共享文件变更先由主 Agent 协调。

## 共享接口

- API：`GET /api/recommendations?q=&availability=all&page=1&page_size=20`；登录且非强制改密；外层 `data` 包装，内层 `{items,total,stats}` 严格按设计 §4。
- 匹配量为规范字符串；unknown 的 have/lack 为 null；摘要只含设计规定字段并透传 recipe.version。
- 排序：缺料数升序、临期优先、耗时升序、ID 降序；展示筛选后计 total 再分页，stats 不受展示筛选影响。
- 前端匹配快照仅在内存，按身份代次隔离；version 不一致时全部丢弃匹配提示。
- 不新增表、不新增迁移：迁移链保持 001–005，不新增 schema_migrations 记录。

## 需求与验收映射

| 设计要求 | 主要产物 | 验收证据 |
| --- | --- | --- |
| §2 候选及三层安全过滤 | logic/store recommendations | 安全优先级、偏好即时生效、本人范围、无食材排除测试 |
| §3.1 四态与整数换算 | logic recommendations | 六方向、逐批向下取整、128 位中间量与饱和边界测试 |
| §3.2–3.3 排序/检索/分页 | logic/store/API | 四排序键、字面检索、availability、total/stats 不变量测试 |
| §4 响应与鉴权错误 | API recommendations 与 Swagger | data 包装、400/401/403/503、分页检查 |
| §5 常数查询与一致快照 | store recommendations | 单事务、常数查询、并发修改快照验证 |
| §6 页面与内存快照 | RecommendationsView、详情、路由与 helper | 空态交叉、库存横幅、身份隔离、version 丢弃的单测/E2E |
| §7 零迁移与文档 | 实施计划、测试记录 | 迁移链/CLI 回归、实施文档 |

## 初始核对与决策

- 设计所依赖的 `EnforceRestrictions`、`ValidatePreferences`、`FormatQuantity`、`calendar.Today`、`ExpiryState` 均存在；维持 API → logic → store 边界。
- 仓库 AGENTS 与实施计划仍描述推荐待实现，这是本次要更新的状态文字；无需需求变更。
- 名称检索使用参数化 SQL `instr(name,?)>0` 命中旗；保留全部候选以计算 stats，仅对安全幸存且名称命中的候选做匹配，不引入 N+1。
- 常规实现细节由负责 Agent 决策；发现需求冲突或重大行为变更由主 Agent向用户确认。

## 阶段状态

- 仓库检查与拆分：完成。
- 后端与前端实现：完成；详细记录见 `backend.md`、`frontend.md`。
- 独立验证：普通 Go 全量检查、37 项浏览器回归、76 项前端单测、CLI、推荐专项 race 均通过；最终完整 `go test -race -timeout 20m ./...` 全通过（API 665.856 秒）。
- 独立 Review：完成，未发现需修复项；报告见 `review.md`。
- 完整 race 首轮默认 10 分钟超时于既有食谱测试的 Argon2 哈希调用；未报告竞态。延长至 20 分钟时限后完整复验通过；首轮失败如实保留，不计通过。
- 主 Agent 已检查成功截图，三张卡片、临期/可做提示、缺料数量与分页显示正常。

## Claude Review 后续

用户要求接受项落地、驳回项交原 Claude Agent 复审；处理与证据详见 [后续处理](claude-review-followup.md)。独立验证/Review分别维护 `followup-validation.md` / `followup-review.md`，本轮新修改不冒用首轮回归结果。
