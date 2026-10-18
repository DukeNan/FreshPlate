# 用户模块测试实施方案

> 认证已迁移到 JWT；本文保留早期设计和验证背景。当前认证契约、迁移与测试范围以 [JWT 认证说明](JWT_AUTHENTICATION.md) 和 [API 文档](API.md) 为准。

编写时间：2026-10-19。架构已由用户明确确认；功能初版（数据库、logic、HTTP、CLI、Vue）已完成。此前各 Agent 的开发期检查用于定位错误，本方案规定整体验证与回归，执行结果及证据见 [本轮总结](USER_MODULE_IMPLEMENTATION_SUMMARY.md)。基础用例见 [测试用例](USER_MODULE_TEST_CASES.md)，浏览器具体步骤见 [浏览器实施方案](USER_MODULE_BROWSER_TEST_PLAN.md)。

## 1. 环境、数据与隔离

- 本机 macOS、Intel amd64；Go 1.26.4（模块及 .go-version 固定），Node 20.20.0，Vue/Vite/npm 依赖锁及 Go go.sum 固定。
- 系统继承 GOROOT 指向 1.25.3，验证时显式删除 GOROOT。自动工具链下载通过 goproxy.cn 并保留 Go 校验；不更改用户全局 go env。正常开发应安装并选择 1.26.4。
- Go 测试使用 t.TempDir 下真实 SQLite 文件；每个 fixture 自有账号、会话、时钟。HTTP 使用 httptest 和真实生产 Argon2；logic 常规测试降低计算成本，保留独立生产参数测试和 benchmark。
- 不连接真实数据库、不开放公网、不访问既有用户浏览器。浏览器测试单独启动真实服务、生产前端构建和临时数据库；内置浏览器不可用时使用项目独立 headless Chromium；具体支持版本和兼容失败写入结果。
- API 用例每次只改变待测条件，合法 CSRF/Origin/页面身份作为其余前提；拒绝请求还核查数据库未发生未经允许的写入。合法的失效 Cookie 清理不视为业务副作用。

## 2. 实际测试入口与验收断言

| 用例组 | 实际入口 | 执行与断言 |
| --- | --- | --- |
| REG、PWD、AUTH、SES | logic/*_test.go、api/api_test.go、store/store_test.go | 用户名及Unicode密码边界、未知字段/角色注入、并发重名、默认设置原子性、虚拟哈希、生产编码、固定过期/重启/撤销/退出。限流用 metrics 确认被拒请求未执行密码计算。 |
| CSRF、SEC、ID | api/api_test.go、runtime/runtime_test.go、web/src/api.test.js | 匿名/认证上下文与跨 Session 拒绝、Cookie标志/登录轮换、精确Origin、JSON类型/尾随/无效UTF8/大小限制、显式表单身份快照、DB故障不能降匿名、no-store；前端恢复后不重放。 |
| LIMIT | logic/passwords_test.go、api/api_test.go | 可控时钟检查每IP/用户名/改密阈值、过期清理、最大键数、两哈希槽占满立即拒绝、并发读写与恢复。 |
| ACL、ADM、CON、CLI | store/store_test.go、logic/auth_test.go、api/api_test.go | 普通用户每个管理入口均拒绝，角色即时生效、自管理拒绝、最后管理员并发保护、操作者/目标陈旧凭证拒绝、审计失败全部回滚、初始化/恢复及强制改密。CLI隐藏输入再由真实PTY演练验证。 |
| PREF | logic/preferences_test.go、store/store_test.go、api/api_test.go | 七字段完整替换、数值与编码、去重、停用引用保留/新增拒绝/显式移除、缺失设置错误、用户A/B本人设置互不可改。并发校验和保存边界检查实际事务。 |
| DB | store/store_test.go、store/transaction_fault_test.go | 每连接PRAGMA/WAL/单连接/约束、真实锁等待5000ms映射503、取消/panic独立上下文回滚；包装真实SQLite驱动故障注入COMMIT/ROLLBACK，核对坏连接关闭和下次新连接。 |
| MIG | store/store_test.go | 空库最终结构、重复迁移保留数据、版本超前/校验不符/缺表列索引拒绝启动。未提供的单人旧库迁移另列条件验收。 |
| FE | web/src/*.test.js、web/e2e/ | 状态恢复、检查失败暂停、广播与focus/visibility、请求代次、并发/me、旧表单快照、CSRF恢复不重放；实际页面完成注册登录设置改密退出、手机与键盘、生产CSP。 |
| DEP | runtime/runtime_test.go、真实进程/PTY演练、浏览器测试 | 环回默认、Origin/Secure配置校验、持久密钥0600、密钥丢失拒启、互斥维护锁、最终页面CSP/nosniff/Referrer/禁止嵌入；生产HTTPS仍需真实部署入口验收。 |

测试函数名包含稳定用例ID，跨层用例同时核对对应入口，不将某个同名测试覆盖到的子项扩张为整行全部完成。最终逐组记录自动化、审查和人工/进程演练的实际证据及局限。

## 3. 正式执行命令

在仓库根目录创建 `docs/user/verification/` 保存脱敏结果。以下 Go 命令在 `server/` 执行；若 goenv 已安装1.26.4，可使用正常 go，否则本机统一前缀：

```sh
env -u GOROOT GOPROXY=https://goproxy.cn GOTOOLCHAIN=go1.26.4 /Users/dukenan/.goenv/versions/1.26.0/bin/go version
```

正式命令使用同一环境前缀：

```sh
go test ./... -count=1 -timeout=240s -json
go vet ./...
go test -race ./... -count=1 -timeout=300s -json
go test ./internal/logic -run '^$' -bench BenchmarkPWD003ProductionHash -benchtime=1x -benchmem
go build ./cmd/server ./cmd/admin
```

前端在根目录执行：

```sh
npm test --prefix web
npm run build --prefix web
npm audit --prefix web --omit=dev --audit-level=high
npm run test:e2e --prefix web
```

单独运行失败包/用例只用于诊断；修复后重新执行受影响检查。完整race通过后不再机械重复运行同一套检查，除非代码发生变化。

## 4. 本地CLI与启动演练

在临时目录构建 server/admin 可执行文件。使用PTY隐藏输入测试账号密码两次执行 init，确认账号创建、默认设置、CLI审计；输出不能含密码；重复init拒绝。运行服务，第二服务和维护命令必须在写入前因锁占用失败。服务停后执行recover-password，再真实HTTP验证旧Session撤销、临时密码可登录且必须改密、修改成功后临时密码失效。检查数据库/key/WAL/SHM权限；删除测试密钥后重启应失败，不能自动换密钥。

上述演练只使用一次性夹具密码，证据中不保存密码或Cookie/令牌。删除密钥只针对本次临时测试目录。若PTY或本机进程环境阻碍演练，应注明具体阻塞，不能用logic测试替代终端回显验收。

## 5. 结果和退出标准

每个命令记录工具版本、时间、退出状态、通过/失败计数及日志路径。修复记录包含触发条件、变更和回归证据。用例执行记录区分：通过、失败、部分验证、未执行、条件阻塞；只有真实完成的断言可以计通过。

B类库存/食谱/推荐/AI/记餐/营养入口不存在，保持后续强制验收。限制复用helper的单元测试不能代替推荐链路。C类旧库转换/WAL配套回退和真实生产HTTPS尚缺场景，本轮不宣称通过。最终总结须包含这些边界及尚未完成的U类子项，不能用文档检查或构建成功替代功能验证。

实际CLI演练入口：仓库根目录执行 `python3 scripts/verify_cli.py --go go`。Go可执行文件可显式指定；脚本清除旧GOROOT并固定工具链，所有数据位于临时目录，密码不回显，验证后自动停止服务并清理。
