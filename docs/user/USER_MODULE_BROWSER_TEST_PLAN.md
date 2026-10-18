# 用户模块真实浏览器测试实施方案

制定时间：2026-10-19。功能初版与 16 项前端状态/API 单元测试已经完成。本方案先于本轮浏览器测试执行编写。

## 环境与隔离

使用项目开发依赖 `@playwright/test` 和独立 headless Chromium，作为可重复运行的项目端到端测试；不访问用户 Chrome、现有浏览器标签页或真实用户数据库。内置浏览器工具当前返回 `No browser is available`，本轮不据此声称任何浏览器验收成功。

启动器在 `os.tmpdir()` 创建独立目录，SQLite、CSRF 密钥和 Go 可执行文件均在其中，测试结束停止服务后删除。实际 Go 服务监听 `127.0.0.1:18080`，Origin 为 `http://127.0.0.1:18080`，提供实际 `web/dist`；端口已占用则拒绝执行，不复用未知服务。构建默认使用 PATH 中的 `go`（本机如需指定 `/Users/dukenan/.goenv/versions/1.26.0/bin/go`，可通过 `FRESHPLATE_E2E_GO` 显式覆盖），删除继承的 `GOROOT`，设置 `GOTOOLCHAIN=go1.26.4`、`GOPROXY=https://goproxy.cn`。注册账号仅为本次测试创建，密码仅为隔离测试夹具。

## 命令与产物

1. `npm install --prefix web`：安装锁定的项目依赖。
2. `cd web && npx playwright install chromium`：安装独立 Chromium。下载或启动失败如实记录为阻塞，不能改称通过。
3. `npm run test:e2e --prefix web`：先构建 Vue 生产产物，再启动实际 Go 服务并运行 Playwright。
4. `npm test --prefix web`：回归现有状态/API 测试。

Playwright 单 worker 顺序执行，失败自动保留 trace 和截图到 `web/test-results/`；结果汇总写 `web/playwright-report/results.json`，控制台给出真实用例计数。报告和证据不记录实际用户信息。成功或失败均执行服务终止和临时数据清理。

## 用例与断言

| 测试 | 对应用例 | 真实步骤与断言 |
| --- | --- | --- |
| 注册、设置与会话 | REG-001、FE-001/002、SES-001/004/005 | UI 注册 A、登录、选择过敏原和公共食材忌口、设置营养目标、保存并刷新恢复；修改密码后回登录页；旧密码失败、新密码登录成功；退出后受保护路由不能访问。 |
| 多标签身份切换 | FE-003/004、ID-001 | 同一 BrowserContext 中 A 标签保留未保存表单；B 标签退出 A 并注册/登录 B；等待 BroadcastChannel 触发身份复核；A 标签旧表单被清除，显示 B 的默认设置，不能把 A 的修改写给 B。 |
| 手机与键盘 | FE-002 | 390×844 viewport，通过标签定位填写登录、Tab/Enter 提交；设置页面主内容无横向溢出，标签及保存按钮可见，键盘可以操作表单。 |
| 最终页面安全 | SEC-004、FE-005 | 检查真实 HTML 的 CSP、nosniff、X-Frame-Options 和 no-store；浏览器记录 CSP violation；DOM 无内联 script/style/事件处理属性；私有 API 响应 no-store；尝试 HTML 用户名被拒绝且不执行脚本；不以此替代服务端安全测试。 |

所有请求都访问真实服务，不使用路由 mock、伪造管理用户或虚假设置响应。管理员页面如没有真实管理员初始化夹具，本轮明确不进行 UI 验收，已有真实 HTTP 集成测试另记；库存、推荐等尚不存在的业务不计通过。多标签测试对广播传播设置合理等待，但不自动重放任何写请求。

## 执行结果

待按上述方案执行后，根据 Playwright 实际结果追加；此前单元测试与构建结果不替代浏览器结果。

执行前环境修订：最新版 Playwright 1.64.1 的 `playwright install chromium` 返回 `Playwright does not support chromium on mac12`。测试依赖固定为仍支持本机系统的 Playwright 1.48.2，安装独立 Chromium 130.0.6723.31 (build 1140) 成功；只运行本地隔离应用，不浏览不可信网站。Vitest 同步升级至 4.1.12 以修复原开发依赖审计问题，17 项状态/API 测试已通过。旧 Playwright 的开发依赖安全限制会在最终结果明确披露，不能把该旧浏览器结果扩展为当前浏览器兼容性验收。

审查补充：退出也必须先成功核对 `/me`，身份检查失败不发退出请求，按钮在检查失败时禁用；增加真实浏览器离线检查及单元断言。

首轮真实执行：4 个浏览器用例中 3 通过、1 失败。多标签页退出后另一标签停留在登录路由，虽然身份已成功更新为 B 且 A 表单已销毁，但未自动转入 B 设置页面。根因是 App 的身份 watch 缺少“公开页面上恢复已登录身份”跳转；已补一条路由替换规则，并保留原失败报告与 trace 到 `web/playwright-report/initial-results.json`、`initial-multitab-failure-trace.zip`。修复后将完整回归，并追加离线身份检查暂停退出/保存、恢复网络可重试的真实浏览器用例。

修复后的第二轮：5 个真实浏览器用例全部通过。为进一步消除旧 Playwright 下载器的 `GHSA-7mvr-c777-76hp` 高危开发依赖，下一轮升级项目 Playwright 至最新版，并允许 `FRESHPLATE_E2E_CHROMIUM` 环境变量指定本机已经安装的测试专用 Chromium 可执行文件；代码不硬编码用户路径。支持新系统的环境继续使用最新版 Playwright 默认浏览器。本机下一轮命令为 `FRESHPLATE_E2E_CHROMIUM="/Users/dukenan/Library/Caches/ms-playwright/chromium-1140/chrome-mac/Chromium.app/Contents/MacOS/Chromium" npm run test:e2e --prefix web`。是否可由新版驱动控制旧 Chromium，以该轮真实结果为准，不能仅凭版本号认定兼容。

第三轮：升级后的 Playwright 1.64.1 驱动已安装 Chromium 130，5/5 通过（18.9 秒），状态/API 测试 17/17 通过；全依赖 `npm audit` 为 0 vulnerabilities。随后清理核验发现 Playwright 默认强制终止测试服务会留下临时目录，已改用 `gracefulShutdown: SIGTERM` 并修正子进程退出等待；仅删除前三轮明确生成的测试临时目录后，再完整回归并核对无残留。此问题不影响业务数据，因为每轮均使用新建隔离库。

最终回归结果（2026-10-19 02:05:53–02:06:34 Asia/Shanghai）：

- `FRESHPLATE_E2E_CHROMIUM=… npm run test:e2e --prefix web`：5/5 通过，0 skipped、0 flaky，40.9 秒；实际 Go 服务、实际 SQLite 和 Vue 生产构建。
- `npm test --prefix web`：3 文件、17/17 通过，Vitest 4.1.12。
- `npm audit --prefix web --audit-level=high`：0 vulnerabilities，包含开发依赖。
- 最终 `web/playwright-report/results.json` 记录 expected=5、unexpected=0；首轮失败报告及 trace 仍保留在相邻 `initial-*` 文件，报告目录由 Git 忽略。
- 测试后 `os.tmpdir()` 无 `freshplate-e2e-*` 残留，18080 无监听进程。
- 全部用例使用 Playwright 1.64.1 驱动测试专用 Chromium 130；当前系统无法安装最新版官方 Chromium，因此不宣称当前 Chrome、Safari、Firefox 跨浏览器兼容性通过。
- 未进行管理员 UI 端到端验收（没有初始化管理员夹具）；管理员实际 HTTP 集成测试由后端验证记录负责，不能据此将管理员 UI 标成已通过。

本轮修复：公开页面收到另一标签的新登录身份后自动转个人设置；身份核对失败时退出和保存均暂停，退出提交也先核对 `/me`；测试服务收到退出信号后完成隔离数据清理。

## 管理员页面补充实施方案（执行前）

为补齐本轮实际交付的管理员 UI，在同一个隔离测试启动器中构建真正 `cmd/admin`，使用 Python PTY 复用 `scripts/verify_cli.py` 的 `hidden_command` 调用 `admin init`。测试夹具用户名及密码放在 `web/e2e/fixtures.json`；密码通过隐藏终端输入，不放命令参数，不新增 HTTP 初始化旁路。初始化在 Go 服务启动前完成，沿用同一个临时 SQLite 与密钥。

新增真实 Playwright 用例：

1. UI 注册目标用户，管理员 UI 登录并进入用户管理，查询目标用户名；切换角色/状态筛选并验证列表。
2. 当前管理员所在行不展示角色、启停、重置操作。
3. 对目标角色变更弹窗选择取消，监听实际请求确认没有 PATCH，行内角色保持用户；确认后升为管理员，再降回用户。
4. 确认停用并验证状态，再确认启用并验证恢复。
5. UI 输入临时密码并确认重置；目标密码状态变为待改密。
6. 管理员审计列表按目标用户 ID 查询，验证角色、状态、重置事件；按 action 进一步筛选。
7. 使用目标账号临时密码登录，必须进入改密页且没有业务/管理入口；直接访问 settings 仍回改密页；用新密码完成改密并重新登录，恢复设置页面。

以上仅访问真实页面、真实 API、真实 CLI 和隔离数据，不 mock 响应。追加后执行完整六个浏览器场景、17 项前端单元测试、构建及依赖审计，并再次检查服务和临时目录清理。

管理员补充执行结果（2026-10-19 02:09:04–02:09:34 Asia/Shanghai）：真实 CLI 隐藏输入成功初始化隔离管理员；全部 **6/6 浏览器用例通过**（29.5 秒，0 skipped、0 unexpected、0 flaky）。新增管理员场景通过了用户列表及用户名/角色/状态筛选、当前管理员无自操作按钮、取消确认不发 PATCH、确认角色升降/停用启用、临时密码重置、按目标与 action 查询审计，以及目标用户临时密码登录后的改密门禁和最终改密恢复。该结果替代前文“未进行管理员 UI 验收”的阶段性限制；六类实际页面均已进入真实浏览器流程。

补充后的回归：17/17 单元测试通过；Vue 生产构建通过；全依赖审计仍为 0 vulnerabilities；18080 无监听，测试临时目录无残留。最新报告仍为 `web/playwright-report/results.json`。跨浏览器及新版 Chromium 兼容性限制保持不变。

可移植性修订：测试启动器默认使用 PATH 中的 `go`，继续移除 `GOROOT` 并固定 `GOTOOLCHAIN=go1.26.4`；仅在需要时通过 `FRESHPLATE_E2E_GO` 指定本机路径。以下将使用默认 `go` 完整执行既有六个 E2E 用例，验证启动入口，不新增用例。

默认 `go` 入口回归结果（2026-10-19 02:10:56–02:11:28 Asia/Shanghai）：生产构建成功，既有 6/6 浏览器用例全部通过（31.3 秒，0 skipped、0 unexpected、0 flaky），未设置 `FRESHPLATE_E2E_GO`。最新报告为 `web/playwright-report/results.json`；临时测试目录无残留，18080 无监听。此项验证替代前述硬编码 Go 路径的启动方式。
