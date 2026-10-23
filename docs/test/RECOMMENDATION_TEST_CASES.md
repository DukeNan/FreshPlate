# 推荐模块验收用例与执行计划

依据：[技术设计 §8](../recommendation/RECOMMENDATION_MODULE_TECHNICAL_DESIGN.md)。详细独立验证记录见 [validation](../recommendation/tasks/validation.md)，最终代码审查见 [review](../recommendation/tasks/review.md)。测试数据库均为临时隔离库。

| 用例组 | 验收要求 | 实际测试入口 |
| --- | --- | --- |
| RCM-SAFE | 公共及本人 published、非空原料；元数据→限制→停用的互斥计数；未知编码、非法偏好、账号隔离、下一请求偏好生效 | `server/internal/logic/recommendations*_test.go`、`store/recommendations*_test.go`、`api/recommendations*_test.go` |
| RCM-QTY | 六方向整数换算、系数缺失、同单位恒可换算、逐条取整、中间精度、cap 单批/聚合饱和与顺序不变量 | `server/internal/logic/recommendations*_test.go` |
| RCM-MATCH | ok/short/missing/unknown、need/have/lack、1 milli 边界、日期/停用/删除/零批次、can_cook 与 missing_count 不变量 | 同上 |
| RCM-LIST | 四键排序、临期 today 至 today+3、字面 q、availability、筛选后 total 与分页、stats 不受筛选影响 | logic/API recommendations tests |
| RCM-STORE | 常数批量查询、单只读事务快照、无偏好/库存/候选、无写入与审计、零迁移 | store recommendations tests、既有 migrate tests |
| RCM-API | 外层 data 完整契约、分页默认/上限、400/401/403/503、管理员仅本人视角、Swagger | API recommendations tests、Swagger 回归 |
| RCM-WEB | 三种优先空态及 q/availability 交叉；库存横幅独立显示；缺料摘要；搜索重置页；身份代次；快照 version 不一致全部丢弃 | `web/src/recommendations*.test.js`、推荐 E2E |
| RCM-E2E | A 库存→推荐排序→忌口过滤→B 隔离；无库存仍显示 missing；有效库存增减横幅；停用后无有效库存；组合空态；分页；详情失配 | `web/e2e/recommendation-module.spec.js` |
| RCM-REG | 既有 Go/Vitest/E2E/CLI 全量回归，迁移保持 001–005 | 下列命令 |

从仓库根目录执行：

```sh
(cd server && go test ./... && go vet ./... && go test -race ./... && go build ./cmd/...)
npm test --prefix web
npm run build --prefix web
(cd web && npm run test:e2e)
python3 scripts/verify_cli.py --go go
```

运行记录：本机首轮完整 `go test -race ./...` 在默认 10 分钟时限触发超时，栈位于 API 测试的 Argon2 哈希计算。独立验证 Agent 使用 `go test -race -timeout 20m ./...` 完整复验通过（API 665.856 秒）；不修改业务密码参数来缩短测试。

执行策略：实现 Agent 添加领域回归测试；独立验证 Agent 按设计检查遗漏并追加自己的用例。先编译与单测，再完整浏览器回归（避免与 CLI 重负载并行造成时序抖动）；失败须记录并由实现者修复、重新执行受影响检查。执行结果只记录实际完成的命令，未执行/环境阻塞单列，不计通过。

本模块没有迁移，不新增 `schema_migrations` 记录。AI、烹饪确认和库存扣减未实现，不属于本次测试通过声明。

## 可追溯用例编号

编号保持稳定；Go 以测试函数前注释标记，Vitest/E2E 以测试名称标记。参数化子用例继承定义处编号，具体边界见每个用例的数据行。

| 用例 ID | 实际文件 | 测试函数 / 名称 |
| --- | --- | --- |
| RECO-API-001 | `server/internal/api/recommendations_test.go` | `TestRecommendationAPIContractFilteringAndIsolation` |
| RECO-API-002 | `server/internal/api/recommendations_validation_test.go` | `TestValidationRecommendationPasswordGate` |
| RECO-API-003 | `server/internal/api/recommendations_validation_test.go` | `TestValidationRecommendationBusyReadAndRecovery` |
| RECO-LOGIC-001 | `server/internal/logic/recommendations_test.go` | `TestRecommendationConversions` |
| RECO-LOGIC-002 | `server/internal/logic/recommendations_test.go` | `TestRecommendationStatesAndInvariants` |
| RECO-LOGIC-003 | `server/internal/logic/recommendations_test.go` | `TestRecommendationSafetyPriorityAndStats` |
| RECO-LOGIC-004 | `server/internal/logic/recommendations_test.go` | `TestRecommendationDatesSortAndFilter` |
| RECO-LOGIC-005 | `server/internal/logic/recommendations_test.go` | `TestRecommendationAggregateCapBoundaryOrder` |
| RECO-LOGIC-006 | `server/internal/logic/recommendations_test.go` | `TestValidateRecommendationQuery` |
| RECO-LOGIC-007 | `server/internal/logic/recommendations_validation_test.go` | `TestValidationRecommendationConversionOracle` |
| RECO-LOGIC-008 | `server/internal/logic/recommendations_validation_test.go` | `TestValidationRecommendationSaturationAndInvariants` |
| RECO-STORE-001 | `server/internal/store/recommendations_test.go` | `TestRecommendationBatchQueriesAndVisibility` |
| RECO-STORE-002 | `server/internal/store/recommendations_test.go` | `TestRecommendationReadSnapshotDuringFoodChange` |
| RECO-STORE-003 | `server/internal/store/recommendations_validation_test.go` | `TestValidationRecommendationEmptyAndInactiveInventory` |
| RECO-WEB-001 | `web/src/recommendations.test.js` | `sums all three exclusive safety counts` |
| RECO-WEB-002 | `web/src/recommendations.test.js` | `prioritizes no candidates over display filters and inventory banner` |
| RECO-WEB-003 | `web/src/recommendations.test.js` | `prioritizes all safety filtered over display filters` |
| RECO-WEB-004 | `web/src/recommendations.test.js` | `does not report empty when total is positive, even on an empty page or without usable inventory` |
| RECO-WEB-005 | `web/src/recommendations.test.js` | `only transfers the clicked recipe once without writing browser storage` |
| RECO-WEB-006 | `web/src/recommendations.test.js` | `does not reuse a different recipe or a direct visit` |
| RECO-WEB-007 | `web/src/recommendations.test.js` | `clears immediately when identity generation changes, including switching away and back` |
| RECO-WEB-008 | `web/src/recommendations.test.js` | `drops the whole match on version mismatch` |
| RECO-WEB-009 | `web/src/recommendations.test.js` | `summarizes all non-ok ingredients including unknown without treating null as zero` |
| RECO-WEB-010 | `web/src/recommendations.test.js` | `uses combined display filters %j / %s` |
| RECO-WEB-011 | `web/src/recommendations.test.js` | `describes %s with text and exact quantities` |
| RECO-WEB-012 | `web/src/recommendations.test.js` | `validates trimmed Unicode code points at the 100-character boundary` |
| RECO-WEB-013 | `web/src/recommendations.test.js` | `matches Go whitespace normalization at NEL and BOM boundaries` |
| RECO-E2E-001 | `web/e2e/recommendation-module.spec.js` | 无库存/临期排序/偏好即时生效/账号隔离 |
| RECO-E2E-002 | `web/e2e/recommendation-module.spec.js` | 组合筛选空态与分页 |
| RECO-E2E-003 | `web/e2e/recommendation-module.spec.js` | 详情配方版本校验及刷新失效 |
| RECO-E2E-004 | `web/e2e/recommendation-module.spec.js` | 停用唯一库存后横幅与missing列表 |
| RECO-E2E-005 | `web/e2e/recommendation-module.spec.js` | 搜索码点边界、超长请求阻断、修正后恢复查询；NEL裁剪与FEFF保留契约 |
| RECO-E2E-006 | `web/e2e/recommendation-module.spec.js` | 无效查询翻页不变更页码/条目，修正后恢复翻页 |
| RECO-COMPAT-001 | `web/e2e/swagger.spec.js` | 推荐操作可见与Swagger兼容 |
