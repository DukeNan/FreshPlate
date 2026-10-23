What this change does: This adds deterministic recommendations from the current user's inventory and dietary restrictions. It provides safe filtering, exact quantity matching, stable ordering, server-side search and paging, and an in-memory detail snapshot checked against the recipe version.

Looks good. Ship.

## 审查范围与结论

独立 Review Agent 未参与业务实现、测试编写或缺陷修复；本次仅写本报告。审查包括最终未提交 tracked diff 和全部 untracked 推荐业务/测试文件，并追踪 API → Service → Store、鉴权 middleware、既有数量/偏好 helper、食谱更新 version、前端 auth/API/router/详情调用链。依据技术方案 §2–§8、最终设计评审及 ponytail-review 技能执行；按单机数十到数百候选、每食谱最多 100 食材的设计规模判断。

未发现需修复的 Must fix / Should fix / Nice to have 问题，故无严重程度编号项或修复后复核项。此结论是代码 Review 结果，不把尚未完成的测试记为通过；最终交付应以独立验证记录为准。

## 核对结果

- 候选 SQL 仅取 public published 与本人 private published；非空食材在计入 candidates 前检查。他人私有、草稿、归档不会进入输出，管理员库存仍仅本人。
- 请求级先校验偏好；逐候选依次处理元数据不完整、EnforceRestrictions、停用引用；未知过敏原编码归 restrictions，首次命中即终止，stats 与展示筛选无关。未加入 dietary_preferences 猜测过滤。
- 六方向换算逐批在目的单位取整；系数先 Round 后检查整数范围。Mul64 承载分子及 cap×分母，除法前比较阈值；未饱和商小于 cap，满足 Div64 前提。累加使用 min(batch, cap-confirmed)，不存在先 int64 溢出再限幅。饱和后仍逐批处理临期与不可换算标志。
- 四态优先级为 ok → unknown → short → missing；足量确认库存不被不可换算批次推翻；有效库存统一排除零、删除、过期及停用批次；ExpiryState 与一次注入 UTC today 共用，today 当天有效。
- Snapshot 为五次批量 SELECT、单只读事务，所有结果集均关闭并检查扫描/迭代错误；末尾提交，失败由 defer 回滚。无 N+1、写入、审计、缓存、新迁移或新依赖。候选/食材/附加过敏原均在同一快照读取。
- q 为 TrimSpace 后最多 100 码点、参数化 instr 字面检索；顺序为安全过滤 → q → 匹配 → availability → 四键排序 → total → 分页，复用 a.page。鉴权、强制改密、数据库 busy 映射沿用既有链路。
- API 摘要字段、food 三字段、nullable have/lack、data 包装及 Swagger generated JSON 对应设计；详情 API 未加入匹配数据。实际检查生成 Swagger 中 /recommendations 及五种 DTO 定义。
- 前端列表请求有序列和身份代次保护；App 在 generation 变化时重新挂载路由视图。匹配只在内存，generation 同步变更即清空；进入详情一次性取出对应食谱，version 不同则整个匹配 region/徽标/缺料信息均不渲染；刷新或无快照直接访问无提示。库存/偏好变化仍沿用设计允许的加载时快照，并明确标注其时效。
- 三类互斥空态使用 stats/total，库存横幅独立；组合筛选完整归因；分页使用服务端 total；缺料和临期有文字，不依赖颜色，无 v-html 或烹饪死入口。

## 测试有效性与证据

逐份阅读 implementation 与 independent 的 logic/store/API 测试、recommendations.test.js、recommendation-module.spec.js，以及 validation.md。独立 math/big.Rat oracle 与生产位运算不同源，覆盖 2700 个固定随机组合；极值、9224 批次、防溢出、逐批取整、四态/排序/日期、安全优先级、五次查询及并发修改快照均有有效断言。真实 API 验证包装、权限/强制改密、busy 后恢复、本人库存与偏好即时生效；E2E 验证真实浏览器推荐/详情版本失配及不渲染旧匹配 region。

独立验证 Agent 已记录普通 Go 全套、vet、build、76 项 Vitest、37 项 Playwright、CLI、最终推荐专项 race 通过。Review 本轮额外执行 git diff --check 并解析生成 Swagger JSON，均通过；未重复运行完整测试套件。

最终复核已直接读取 validation.md 与 logs/race-extended.log：go test -race -timeout 20m ./... 全通过，exit_code=0；API 665.856 秒、logic 5.787 秒、runtime 2.544 秒、store 16.618 秒。首轮默认 10 分钟在既有 Argon2 夹具处超时仍作为历史失败记录保留，未改报为通过。审查后无生产代码变更；最终文档、日志与测试状态更新未改变审查范围或结论，当前无未关闭 Review 项。

Not checked: 未做超出设计规模的压测、跨午夜长请求故障实验；AI、记餐、库存扣减及营养不属于本次范围。完整测试由独立验证 Agent 执行，本 Review 核对代码与最终运行证据，未重复跑全套。
