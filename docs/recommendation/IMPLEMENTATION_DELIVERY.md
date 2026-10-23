# 推荐模块实施交付

本次依据 [技术设计](RECOMMENDATION_MODULE_TECHNICAL_DESIGN.md) 实现纯确定性推荐，不包含 AI、烹饪确认、库存扣减或营养估算。任务分工、共享接口和阶段状态见 [任务索引](tasks/README.md)。

## 设计与实现对应

| 要求 | 实现位置 | 行为 |
| --- | --- | --- |
| §2 候选范围、安全过滤与互斥计数 | `store/recommendations.go`、`logic/recommendations.go` | 只读公共及本人 published、排除无原料；先完整性、再偏好限制、再停用引用；偏好先校验，stats 与展示筛选无关 |
| §3.1 有效库存、四态匹配 | `logic/recommendations.go` | 本人非删除正数量批次，排除过期及停用食材；复用 ExpiryState；unknown 不推翻已确认充足的 ok |
| §3.1 数量精度与饱和 | `convertRecommendationQuantity`、安全累加 | 六方向整数换算，math/bits 128 位中间乘积及阈值比较；最终单位逐批向下取整；cap=10^15 milli 向下饱和 |
| §3.2–3.3 筛选排序 | `BuildRecommendations`、SQL instr 命中旗 | 安全过滤后应用名称命中，再匹配及 availability，四键排序后计算 total、分页；没有评分或猜测换算 |
| §4 API、鉴权与错误 | `api/recommendations.go`、`api/api.go`、Swagger | 复用登录/强制改密中间件与 page()；data 包装；不增加写接口或身份断言头 |
| §5 快照与查询 | `RecommendationSnapshot` | 单只读事务五条批量 SELECT，不随食谱/原料数量增长；偏好、库存、配方及食材元数据来自一致快照 |
| §6 页面、空态与横幅 | `RecommendationsView.vue`、`recommendations.js` | 服务端筛选分页；基于 stats/total 的互斥空态；无库存横幅独立显示；缺料/临期使用文字提示 |
| §6 详情一致性及身份隔离 | `RecipeDetailView.vue`、内存快照 helper | 点击来源的单次内存快照；版本不符整体丢弃；身份代次变化同步清空；直接打开或刷新详情不显示匹配 |
| §7 文档和零迁移 | README、AGENTS、IMPLEMENTATION_PLAN、测试索引 | schema 保持 v5，无新表、迁移、审计、缓存、后台任务或 npm 依赖；不新增 schema_migrations 记录 |

路径前缀为 `server/internal/` 或 `web/src/`，函数名可直接检索。

## 验证与 Review

普通 Go 全量测试、vet/build、前端 76 项单测、37 项浏览器回归、CLI、最终推荐专项 race 及完整 `go test -race -timeout 20m ./...` 均通过。完整 race 首轮默认 10 分钟超时，延长时限后 API 包在 665.856 秒完成；未修改业务密码强度。独立 Review 未发现需修复项。完整命令、失败与复测记录由未参与实现的 Agent 分别维护：

- [独立验证记录](tasks/validation.md)
- [独立代码 Review](tasks/review.md)
- [用例与执行计划](../test/RECOMMENDATION_TEST_CASES.md)

## 运行边界

匹配提示是推荐加载时的库存与偏好快照。配方 version 校验只保证配方一致，不保证库存、偏好或食材元数据仍未变化；这些数据变更后应刷新推荐。当前使用全量候选内存计算，适用于设计规定的单机数十至数百食谱规模；未进行超出该规模的性能验收。

## Claude Review 后续修复

搜索校验、测试追溯和单位标签复用已调整；第3/5项驳回经原 Claude Agent 复审接受，未修改推荐查询策略或共享安全校验。新增改动和后续边界修复见 [处理记录](tasks/claude-review-followup.md)，独立检查见 [本轮验证](tasks/followup-validation.md) 与 [本轮 Review](tasks/followup-review.md)。前述76项单测/37项完整浏览器回归是首轮历史记录；本轮执行范围和最新数量以本轮验证记录为准。
