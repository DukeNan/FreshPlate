# 推荐后端实现记录

负责范围：`server/internal/{logic,store,api}/recommendations*.go`、API 路由、Swagger 注解生成配置及生成产物；无业务表、迁移或其他业务重构。

## 设计对应

- §2：store 限定 published 公共 ∪ 本人私有；纯函数先排无原料记录，再按元数据 → EnforceRestrictions → 停用引用三项互斥计数。请求级 ValidatePreferences，原有函数签名保持不变。
- §3.1：math/bits Mul64、阈值 128 位比较、Div64 实现最终单位唯一一次向下取整；六位系数 math.Round 整数化并复核范围；单批及累加 cap=10^15；同单位恒已知；四态和缺量规范字符串。
- §3.2：calendar.Today 单次 Service.Now 注入，ExpiryState 共用三天阈值；按缺料、临期、耗时、id 排序；availability、total、分页顺序符合方案。
- §3.3：候选 SQL SELECT 使用绑定的 instr(name,?) 名称匹配标记；安全过滤 stats 仍来自全量候选，不随 q 减少。
- §4：受保护 GET /api/recommendations；logic DTO 精确返回 data 内 items/total/stats，recipe 不包含 owner/steps/status/审计；have/lack 无 omitempty 确保 null。
- §5：固定五条 SELECT（偏好、库存+food、候选、原料+food、附加过敏原），全部在单 ReadOnly 事务内完成。到期/active 过滤在内存。查询、扫描及提交错误统一 mapError。
- §6：生成器包含 ../logic 目录以识别纯函数 DTO；沿用 response{data=logic.RecommendationPage} 注解。
- §7：不新增 schema_migrations 记录，迁移链维持 001–005。

## 证据与决策

现有 `store/readPreferences` 逐 excluded_food_ids 查询展示名称，不满足推荐固定查询数；本模块直接单批读取偏好 JSON 与目标数值（不读取无用 ExcludedFoods），不修改原有偏好接口。现有库存与食谱 scanner、Food DTO、Quantity 格式化、calendar.Today/ExpiryState、分页及认证 helper 均复用。

纯计算接口：BuildRecommendations(store.RecommendationInput, today, availability, page, size)。输入仅为通过 store 候选可见性筛选的快照，NameMatches 来自参数化 instr；输出 RecommendationPage。Service 负责参数及页码校验、一次时钟注入与 IO 调用。

## 实现测试结果

已通过：

- `cd server && go test ./internal/logic ./internal/store ./internal/api`（初版；API 完整包约 70 秒）。
- `cd server && go generate ./internal/api`（Swagger 三份产物成功生成）。
- `cd server && go test ./internal/logic ./internal/store -run Recommendation`。
- `cd server && go test ./internal/api -run Recommendation -v`。

实现单测包括六向换算、128 位中间值未超 cap 的精确结果、复合饱和、毫单位余数、四态/不变量/不可换算单调性、三项过滤优先级、未知过敏原、无原料候选、stats 独立、日期 today/+3/+4、四键排序、availability 后分页、cap−1 加 1/2 和反序。

Store 测试：40 公共食谱+他人私有，精确断言查询数为 5；q=% 不作为通配符。独立写连接在候选查询后真实提交停用及元数据变更，当前原料读取仍维持旧值，下一请求立即反映新值，验证事务快照而非仅断言调用结构。

API 测试：外层 data 完整结构、21 候选默认 20 条、q 缩小 total 不缩 stats、字面百分号、can_cook、非法参数、401、本人库存、管理员仅本人、偏好修改下请求即时生效。

独立验证 Agent 另负责全量 Go test/vet/race、缺失异常路径及独立设计极值验证。本实现 Agent 未执行 CLI 或浏览器检查。
