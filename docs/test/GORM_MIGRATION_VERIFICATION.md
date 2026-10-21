# GORM 数据访问替换验证

日期：2026-10-20。数据库结构仍为 v5，HTTP 接口与前端业务代码不变。

## 实现

- 业务 CRUD 使用 GORM v1.31.2、SQLite Dialector v1.6.0；复用原 `modernc.org/sqlite` v1.39.1 连接。官方 Dialector 间接引入 `go-sqlite3`，实际连接仍由 modernc 创建，不要求启用 CGO。
- 保留 `Store.DB *sql.DB` 供连接管理、迁移和测试使用；私有 ORM 句柄复用同一连接池。`WithImmediate` 回调现在接收绑定专用连接的 `*gorm.DB`。
- SQL 迁移脚本、校验和、结构检查、WAL、外键、5000ms 忙等待及 0600 权限保持原实现，不使用 `AutoMigrate`。
- 写事务保留 `BEGIN IMMEDIATE`、独立清理上下文和回滚失败后丢弃连接；GORM 默认写事务关闭。读事务继续保证计数、分页与派生值的一致性。
- 显式模型及字段 map 保留整数秒、JSON、NULL、零值、版本条件和影响行数检查；仅食谱有效过敏原 UNION 查询保留完整 SQL，通过 GORM `Raw` 执行。现有行扫描与 JSON 校验继续复用。
- SQL 日志关闭，避免密码哈希或令牌参数进入日志。

## 回归覆盖

新增 `server/internal/store/gorm_test.go`：

- 用裸 SQL 创建已有 v5 库，预置用户、会话、刷新令牌、偏好、库存和食谱；经 GORM 读取及刷新令牌轮换后，表结构和原迁移记录保持不变。
- 营养值更新为 0、布尔值更新为 false、可空字段清为 NULL、库存数量归零、备注清空、食谱子项清空，以及时间戳 0 不被自动替换。
- 未找到用户仍返回 `sql.ErrNoRows`；GORM 未找到错误映射保持兼容。

既有事务故障、取消、panic、忙等待恢复、身份撤销、版本竞争、审计原子性及食谱查询次数测试继续执行。故障注入和查询计数夹具均初始化同连接的 ORM。

浏览器首次回归暴露库存 INV-E2E-001 的等待时序问题：旧成功提示会让后续创建断言提前完成。测试现在等待当前表单关闭后再检查请求及成功提示，避免后续用例缺少批次数据。首次失败引起 worker 重启及额外登录，末项 JWT 测试触发登录限流；等待修正后，全量重跑 33/33 通过。

## 执行结果

所有验证使用临时数据库，未读取或修改日常数据库。

| 检查 | 结果 |
| --- | --- |
| 替换前 `go test ./...` | 通过 |
| 替换后 `go test ./...` | 通过，4 个测试包 |
| `go vet ./...`、`go build ./cmd/...` | 通过 |
| `CGO_ENABLED=0 go build ./cmd/...` | 通过 |
| `CGO_ENABLED=0 go test ./internal/store -run TestGORM` | 3 项新增回归通过 |
| `go test -race ./...` | 通过，4 个测试包；api 538.8s / logic 5.5s / runtime 2.0s / store 15.4s |
| `python3 scripts/verify_cli.py --go go` | 6 项验证通过 |
| `npm test --prefix web` | 59/59 通过 |
| `npm run test:e2e --prefix web`（含前端构建） | 33/33 通过（30.3s） |

环境：Go 1.26.4、Node.js 24.6.0、macOS arm64；浏览器使用 Playwright 下载的 Chromium。
