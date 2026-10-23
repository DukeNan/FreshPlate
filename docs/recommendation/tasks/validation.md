# 独立验证记录

负责 Agent：validation（未参与业务实现）。依据：设计 §2–§8、AGENTS.md。

## 独立验收清单

- 安全：候选可见性/published/非空前提；过滤优先级、未知编码、无效偏好；修改偏好立即生效；管理员和跨用户隔离。
- 数量：四态、不变量、六方向换算、逐批取整、整数极值与饱和（含中间值超过 int64 而结果精确）、缺系数与已有 ok 共存、有效/过期/停用批次。
- 排序和查询：四键、today 至 today+3、字面 q、availability 后 total/分页、stats 不随筛选变化。
- 存储/API：单只读快照、常数批量查询、无写入审计与迁移、响应 envelope、400/401/403/503。
- 浏览器：无库存横幅与列表同时呈现，空态归因，搜索/筛选重置分页，缺料/临期文字，详情版本失配全部丢弃提示，账号身份失效。
- 全回归：Go test/vet/race/build、Vitest/build、Playwright 全套、CLI 隔离验证。

## 隔离和职责

Playwright 使用已有 server.mjs 临时库；新增推荐 spec 使用专用食材名称与现有 A/B/admin 独立上下文，不修改共享 fixture、日常数据库或业务实现。后端独立测试预留 recommendations_validation_test.go；实现者测试另行审查，避免仅复述实现断言。

## 执行状态

准备阶段已结束。执行环境：Go 1.26.4、Node v24.6.0；最终状态详见下表。

## 已执行结果（2026-10-23）

| 命令（根目录，注明子目录者除外） | 状态 | 结果 |
| --- | --- | --- |
| `cd server && go test ./...` | 通过 | 最终生产代码 API、logic、runtime、store 全通过；API 68.493 秒、logic 1.008 秒、store 6.042 秒。随后独立 store 新增用例亦通过 `go test ./internal/store`（6.866 秒） |
| `cd server && go vet ./...` | 通过 | 无诊断 |
| `cd server && go build ./cmd/...` | 通过 | 服务/CLI 编译成功 |
| `npm test --prefix web` | 通过 | 最终版本 5 文件、76 用例 |
| `npm run test:e2e --prefix web` | 通过 | 构建成功；37/37 Playwright，38.0 秒，包含新增推荐 4 例和 Swagger 34 操作 |
| `python3 scripts/verify_cli.py --go go` | 通过 | 初始化、锁、权限、服务重启、恢复撤销、JWT/key 缺失保护；临时数据库 |
| `cd server && go test -race ./...` | 失败（超时） | API 在 601.183 秒触发 Go 默认 10 分钟限制，栈在 Argon2 密码计算，未发现 race 报告；logic 6.030 秒、store 18.573 秒、runtime 通过 |
| `cd server && go test -race -timeout 20m ./...` | 通过 | 最终完整 race：API 665.856 秒、logic 5.787 秒、runtime 2.544 秒、store 16.618 秒；密码强度与业务代码不变 |

独立新增 `logic/recommendations_validation_test.go`：使用 `math/big.Rat` 作为独立数值 oracle，固定种子 2700 个组合覆盖九种单位方向、系数六位小数与极大中间量；9224 个 cap 批次聚合、逐批取整和输出不变量。生产逻辑的位运算实现未复制到 oracle。

独立新增 `web/e2e/recommendation-module.spec.js`：真实 API/浏览器闭环，覆盖无库存全 missing、today 临期、可做优先及临期第二排序键、short数量、偏好下一次请求即时生效、A/B 隔离、组合筛选空态、字面 `%`、非法 availability、total 与越界页、详情配方版本失配、唯一库存被停用。`swagger.spec.js` 更新操作数并明确推荐操作存在。

### 发现和处理

1. 首次集成前 `go test ./...` 发现 `api/recommendations.go:23 undefined: page`；已回报后端实现 Agent，其修复为 `a.page(c)` 后全套通过。
2. 其他已完成独立测试未发现业务缺陷。首次成功截图在列表重绘时抓取，已强化等待三张卡片/缺料文字后重抓；这是测试同步改善，不属于业务缺陷。

### 验证边界

- 不执行 AI、扣库存、记餐和营养流程，设计明确在首版范围外。
- 单只读事务、五次批量 SELECT 与零写入通过源码检查及 store 实现测试验证；未进行生产规模压测、外部故障注入或跨午夜长请求实验。
- 推荐专属数据库 busy/恢复与强制改密由独立 `api/recommendations_validation_test.go` 验证：真实重置密码后端点返回 403；独立连接持排他读锁，已认证推荐 handler 返回 503，释放后 200。空候选、缺偏好错误和零数量/逻辑删除库存由独立 `store/recommendations_validation_test.go` 验证。权限与五次查询/并发快照另有实现 Agent 的后端测试，独立 Agent 执行并检查覆盖。

增强后的推荐 E2E 单独重跑：4/4 通过（11.7 秒），增加同 version 详情快照复用、失配 region 整体不渲染、筛选后下一页禁用断言。成功截图：[recommendations.png](recommendations.png)，独立 Agent 已目视确认三张卡片、临期/可做文字、缺 99 克和分页均正确。

执行控制：曾额外启动第二条完整 race，因与正在运行的完整 race 重复而主动终止（退出 143），不计通过；遵照主 Agent 协调，只保留首条完整 race，并对最终生产代码/新增测试补跑 `go test -race ./internal/api ./internal/logic ./internal/store -run Recommendation -count=1`。迁移文件独立核查仍仅 001–005。

最终生产代码专项 race 结果：`go test -race ./internal/api ./internal/logic ./internal/store -run Recommendation -count=1` 全通过，API 28.601 秒、logic 1.356 秒、store 2.833 秒，包含独立 API/store/logic 验证文件。

命令纠正记录：编写独立 logic 用例后曾在仓库根目录执行 `go test ./internal/logic`，因 Go 模块位于 `server/` 返回 `cannot find main module`；改在 `server/` 执行后相关用例及全套均通过。该次调用未执行任何 Go 测试。

完整 race 首轮超时证据：`panic: test timed out after 10m0s`，当时运行 `TestRecipeAPI021_024_MiscContracts`，堆栈 `argon2.processBlocks` → `Passwords.Hash` → 测试夹具注册用户；不是测试断言失败或 race 检出。该次全套未完成，不能认定为通过；随后以20分钟超时重跑最终代码，完整通过（见下方）。

### 日志原始记录范围

已从工具会话返回输出保存：[最终检查输出](logs/final-checks.log)、[首轮 race 完整超时 stack](logs/race-default-timeout.log)。空白列对齐已归一化；保留测试输出，不含工具包装元数据。Playwright 37 例控制台和最终推荐 4 例控制台将另存，延时限 race 输出已在真实完成后保存。不为补日志重复耗时测试。

[Playwright 构建与全部用例结果](logs/playwright.log)：全部37例结果及最终增强4例重跑均保留，明确省略种子登录时间/Node颜色警告，未声称为逐字原始录屏。

## 最终结论

最终完整 `go test -race -timeout 20m ./...` 全通过（API 665.856 秒）；[完整输出](logs/race-extended.log)。Go test/vet/build、Vitest 76 例、全量 Playwright 37 例（最终增强推荐4例另复跑）及CLI验证均通过。首轮默认10分钟race超时和主动终止重复race保留为真实历史结果，未改报为通过。独立测试新增3个Go测试文件、推荐E2E文件与Swagger浏览器断言，未修改业务实现；尚无未解决业务缺陷或环境阻塞项。验证范围限制仍如上，不包含生产规模压测。
