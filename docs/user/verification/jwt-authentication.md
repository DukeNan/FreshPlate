# JWT 认证迁移验证记录

验证日期：2026-10-19（Asia/Shanghai）。本文新增记录，不替换已有 Session 版本的历史日志。

## 环境

- macOS / Darwin 21.6.0，amd64。
- Go 1.26.4（`/Users/dukenan/.goenv/versions/1.26.4/bin/go`）。
- Node.js 20.20.0，Python 3.11.8，Playwright 1.64.1。
- Google Chrome 150.0.7871.125，通过 `FRESHPLATE_E2E_CHROMIUM` 指定系统可执行文件。
- 浏览器与 CLI 均使用独立临时数据库、CSRF/JWT 密钥及服务端口，不读取日常数据库。

## 实际执行

| 检查 | 命令与结果 |
| --- | --- |
| CLI 与真实进程 | `python3 scripts/verify_cli.py --go /Users/dukenan/.goenv/versions/1.26.4/bin/go`：通过 |
| 前端完整浏览器回归 | `FRESHPLATE_E2E_GO=/Users/dukenan/.goenv/versions/1.26.4/bin/go FRESHPLATE_E2E_CHROMIUM='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' npm run test:e2e --prefix web`：9/9 通过，39.9 秒 |
| 独立 JWT 浏览器验收 | 同上命令追加 `-- --grep JWT`：3/3 通过，23.4 秒，包含新增 access 到期刷新重试用例 |
| 独立后端测试与静态检查 | `GOTOOLCHAIN=go1.26.4 go test ./... && GOTOOLCHAIN=go1.26.4 go vet ./...`：通过 |
| 独立前端单元与构建 | Vitest 31/31 通过；生产构建通过（35 modules） |
| 后端竞态与构建 | Go 1.26.4 执行 `go test -race ./...`、`go build ./cmd/...`：通过；最终补充测试再次执行 API/store 定向 race：通过 |

CLI 实际覆盖：隐藏密码输入、规范化和重复初始化、用户默认数据与审计原子性、服务/CLI 独占锁、数据库/WAL/SHM/两类密钥 `0600`、服务重启后 JWT 会话有效、refresh 轮换、管理员恢复后旧 access 立即失效、强制改密及新密码登录、已有数据库缺失 JWT/CSRF 密钥时拒绝启动且不重新生成。

浏览器完整回归覆盖：注册、登录、设置保存与重载、改密和旧 JWT 撤销、注销、跨标签页切换账号及清理旧表单、移动视口键盘操作、CSP/no-store、离线身份检查阻止提交、管理员角色/状态/重置与审计、强制改密、Swagger 14 接口、Bearer-only 业务写入、refresh Cookie 属性、轮换和重放检测撤销整个会话，以及两个标签页并发重载后继续登录和跨标签页注销。断言持久存储只允许非敏感 revision 标记，令牌和用户信息不入 localStorage/sessionStorage。

首次完整运行 8/9 通过，唯一失败为运行时尚未重新生成 Swagger，UI 仍显示 13 接口；生成新规范并重新构建服务后上述完整回归全部通过。

独立验收 Agent 未参与实现，只读检查和复测后判定通过；上述完整浏览器回归与独立 JWT 子集共覆盖当前 10 个浏览器用例。

## 限制

- access 过期的浏览器用例通过拦截单次 API 响应模拟 `ACCESS_TOKEN_EXPIRED`，随后真实刷新并重试；JWT 时间验证由后端测试覆盖，不等待 15 分钟。
- 未验证真实 HTTPS、反向代理、Safari、Firefox 或实体移动设备；本次浏览器结果仅代表上述 Chrome 版本。
- 严格 refresh 轮换无宽限期，刷新响应丢失可能要求重新登录；改密预检后恰好到期时需再次提交。
