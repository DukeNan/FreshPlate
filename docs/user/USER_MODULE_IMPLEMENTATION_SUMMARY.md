# 用户模块实现与测试总结

> 认证已迁移到 JWT；本文保留早期设计和验证背景。当前认证契约、迁移与测试范围以 [JWT 认证说明](JWT_AUTHENTICATION.md) 和 [API 文档](API.md) 为准。

日期：2026-10-19。用户确认架构后完成本轮用户模块实现，并按测试实施方案验证。库存、推荐、记餐和营养等后续业务不属于本轮已交付功能。

## 1. 过程与交付

本次使用多个子 Agent 分别完成架构复核、用例编写、架构图、数据库、认证逻辑、Vue、HTTP测试和独立安全审查，由主 Agent 集成、修复并执行正式验证。执行顺序为：架构与用例 → 用户确认 → 功能实现及伴随开发检查 → 具体测试实施方案 → 正式验证与回归 → 本总结。

| 交付 | 位置 |
| --- | --- |
| 架构说明与范围 | [USER_MODULE_ARCHITECTURE.md](USER_MODULE_ARCHITECTURE.md) |
| 架构图 | [交互HTML](../../.archify/architecture-user-module-20261019-014414/user-module.html)（方案阶段图，不能作为功能验证证据） |
| 用例与追踪 | [USER_MODULE_TEST_CASES.md](USER_MODULE_TEST_CASES.md) |
| 后端与CLI/部署测试方案 | [USER_MODULE_TEST_EXECUTION_PLAN.md](USER_MODULE_TEST_EXECUTION_PLAN.md) |
| 真实浏览器测试方案与过程 | [USER_MODULE_BROWSER_TEST_PLAN.md](USER_MODULE_BROWSER_TEST_PLAN.md) |
| 启动、配置与运维 | [README.md](../../README.md)、[配置示例](../../.env.example) |
| 后端与迁移 | [server/](../../server/) |
| Vue与前端测试 | [web/](../../web/) |
| 可重复CLI/进程演练 | [verify_cli.py](../../scripts/verify_cli.py) |
| 脱敏验证证据 | [verification/](verification/) |
| 最终源码摘要 | [source-manifest.json](verification/source-manifest.json)（未提交工作区快照，不伪称HEAD已包含本轮实现） |

## 2. 实现结果

后端沿用 Gin API → logic → store → SQLite；管理员CLI直接复用logic/store。未增加ORM、JWT、Redis、权限引擎或状态管理库。

- 注册与用户名规范化，注册原子创建普通账号和默认设置；Argon2id密码、等成本虚拟哈希、双槽有界密码计算、内存限流。
- 固定七天服务端Session、摘要存储、重启持久化、改密/禁用/重置撤销全部会话；当前角色每次从数据库读取，强制改密白名单。
- 匿名与有效Session区分的CSRF、精确Origin、严格JSON与UTF-8、请求体64KiB限制、Cookie属性、私有响应no-store、统一脱敏错误。
- 完整个人设置、输入保护、停用忌口保留、非法食材拒绝、缺失设置报错；最小只读公共食材入口和12种基础名称，不含营养或过敏原推断。
- 用户列表/筛选/分页、角色与启停、密码重置及审计；禁止自管理、至少一个active管理员、写事务内复核操作者与目标、审计失败原子回滚。
- SQLite单连接、逐连接外键和busy_timeout、WAL、专用连接BEGIN IMMEDIATE、取消/panic独立上下文回滚、回滚失败丢弃底层连接。
- 空库迁移与版本/checksum/表列索引检查，未知旧库拒绝启动；管理员隐藏终端初始化/恢复、持久0600密钥、数据库/WAL/SHM0600、服务与维护互斥锁。
- Vue登录、注册、设置、改密、用户管理、审计六类页面；身份恢复、表单身份快照、广播及focus/visibility复核、旧请求代次丢弃、错误恢复不重放写请求、手机和键盘操作。
- 实际静态页面CSP、禁止嵌入、nosniff、Referrer-Policy；Secure模式和HTTPS配置校验。真实HTTPS反向代理仍需部署验收。

`GET /api/foods` 为设置依赖补充，返回active食材；已有disabled引用在设置响应中保留。PATCH同时改变role/status只写一条user_updated审计；单字段分别role_changed/status_changed；无变化成功返回，不更新时间或新增审计。数据库不接受文件符号链接/硬链接别名，防止绕过进程锁。

## 3. 正式验证结果与证据

环境：macOS 12、Intel i7-7820HQ、Go 1.26.4、Node 20.20.0。Go环境显式删除旧GOROOT；依赖锁已整理并校验，未修改用户全局go配置。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 全项目Go测试 | 通过；完整基线44个顶层测试，含子项81个PASS，0失败 | [原始JSON事件](verification/go-test.log)、[命令/退出码](verification/go-summary.json) |
| 全项目race | 同一完整基线通过，0失败/数据竞争 | [race事件](verification/go-race.log) |
| vet与可执行程序构建 | 通过 | [vet](verification/go-vet.log)、[build](verification/go-build.log)、[汇总](verification/go-summary.json) |
| 后续定向补充 | 设置事务顺序/完整替换、原哈希独立复核、状态审计回滚、IP改密阈值、可信代理、Cookie覆盖恢复均通过；对应race也通过 | [Store测试](verification/store-additional-test.log)、[Store race](verification/store-additional-race.log)、[API测试](verification/api-additional-test.log)、[API race](verification/api-additional-race.log)、[CSRF恢复](verification/csrf-cookie-race.log) |
| 用户名与哈希损坏回归 | 原始非ASCII用户名拒绝；合法大小写正常；损坏哈希安全失败并记录脱敏内部事件 | [用户名测试](verification/logic-additional-test.log)、[用户名race](verification/logic-additional-race.log)、[调用方回归](verification/normalization-integration-race.log)、[损坏哈希race](verification/hash-corruption-race.log) |
| 模块文件校验 | 通过 | [go mod verify](verification/go-mod-verify.log) |
| 前端单元/状态/请求测试 | 17项通过，0失败 | [Vitest结果](verification/web-unit.log) |
| 前端生产构建 | 通过 | [构建日志](verification/web-build.log) |
| npm全依赖审计 | 0漏洞（含开发依赖） | [审计JSON](verification/web-audit.log) |
| 真实浏览器 | 6/6通过，0跳过/失败/不稳定 | [Playwright结果](verification/browser-results.json)、[方案与实际步骤](USER_MODULE_BROWSER_TEST_PLAN.md) |
| 隐藏输入、真实进程与恢复 | 通过：初始化、重复拒绝、服务/CLI互斥、文件权限、重启会话、恢复撤销与强制改密、密钥丢失拒启 | [演练日志](verification/cli-process.log) |

全量基线之后新增的测试只定向或按受影响包执行，不把重复执行相加为“新用例”。最终源码的顶层Go测试总数为51；72组设计用例是参数化需求组，不能与测试函数/子项计数直接比较。

浏览器为项目独立headless Chromium 130、Playwright 1.64.1，访问真实Go服务/生产Vue构建/临时SQLite；管理员由真实CLI经PTY初始化，无HTTP测试旁路，无API路由mock。覆盖注册、登录、设置、刷新恢复、改密、退出、多标签身份切换、离线暂停/重试、手机390×844/键盘、最终CSP与HTML/API响应头，以及管理员筛选、自操作隐藏、取消确认、升降角色、启停、重置、审计和强制改密。测试结束停止服务并删除临时数据库，不修改真实用户数据。

生产密码参数基线：Argon2id m=64MiB、t=3、p=1；本机一次benchmark约262.11ms，67,114,360B/op，51次分配。两个槽约128MiB算法内存，另有运行时开销。这是本机容量参考，不是生产并发SLA；部署机仍需复测。

## 4. 用例覆盖与明确边界

原用例共72组：57组U（当前模块）、11组B（后续业务）、4组C（条件验收）。本轮验证记录按模块组合自动化、真实进程/浏览器和源码审查证据；未将72组全部标为通过，也未用空业务入口凑数。

| 设计用例范围 | 本轮验证 |
| --- | --- |
| REG/PWD/AUTH/SES | HTTP、logic、真实SQLite、CLI进程及浏览器验证用户名/密码边界、会话生命周期与重启、撤销、Cookie与退出；常量时间比较和脱敏日志另作源码审查。 |
| CSRF/SEC/ID/LIMIT | API矩阵及受控时钟、Cookie覆盖恢复、密码metrics、并发槽/限流测试、真实前端恢复不重放和身份代次；可信代理与非可信对照均验证。 |
| ACL/ADM/CON/CLI | 每个管理入口普通用户拒绝、已有会话角色即时生效、审计事务失败/陈旧凭证拒绝、最后管理员并发保护；真实CLI与管理员UI验证恢复/重置/强制改密。 |
| PREF | 七字段、范围/编码/去重、本人设置隔离、停用保留/移除/新增拒绝；实际写锁与channel控制顺序，确认检查后写入无窗口、最终完整版本无混搭。 |
| DB/MIG-001/002 | 真实文件SQLite约束、连接重建PRAGMA、锁等待、取消/panic/提交失败/回滚失败连接丢弃；空库迁移及历史/结构篡改拒启。 |
| FE/DEP-001 | 17单元与6浏览器场景、真实页面安全头、CLI/服务互斥、密钥丢失和权限；屏幕模拟和键盘为自动化验证，未做实体手机或屏幕阅读器验收。 |
| B：ISO/IDEM/FOOD/REC/NUT/E2E完整产品链路 | 业务入口尚未实现，保留后续强制验收。A/B个人设置验证不等于库存/私有食谱隔离已验收；硬限制helper通过不等于推荐/AI/烹饪业务通过。 |
| C：MIG-003/004/005 | 缺实际旧库结构、数据和旧程序，未执行旧单人库导入与配套回退。运行文档给出一致性备份/WAL要求，不能据此宣称旧库迁移通过。 |
| C：DEP-002 | 应用最终页面/CSP、Secure Cookie、HSTS配置分支已在本地验证；未接真实HTTPS代理、证书或网络入口，因此生产部署验收仍为条件阻塞。 |

未穷举所有字符及状态组合，未做持续负载/渗透测试；验证针对用例中的边界、关键失败路径和事务/权限不变量。其他浏览器、最新版Chromium和实体设备兼容性未验收。内置浏览器工具不可用，测试使用隔离的项目Chromium，不访问用户浏览器状态。

## 5. 发现的问题与修复

| 问题/触发 | 修复 | 回归 |
| --- | --- | --- |
| 已有0755目录首次打开数据库时WAL/SHM可能0644 | SQLite连接前创建/收紧主库及遗留sidecar至0600 | 文件权限真实测试、进程演练 |
| 数据库文件符号链接/硬链接可使用不同.lock文件 | 启动锁入口拒绝文件别名，保留普通路径及目录解析行为 | runtime别名与独占锁测试 |
| /me检查失败时仍可退出写请求 | 退出前复核身份，失败禁用并暂停退出 | 单元与浏览器离线场景 |
| 另一标签登录B后旧标签可能停在登录页 | 身份与公开路由联动恢复至对应页面，清旧表单 | 首轮失败重现后多标签真实回归 |
| Unicode转小写把Kelvin符号映射为ASCII用户名 | 先验证原始ASCII字符，再转小写 | Kelvin拒绝、合法uppercase和调用方race回归 |
| 无请求体退出入口未显式限制正文 | 全局请求体上限，退出拒绝非空正文且不撤销会话 | API限体、未知长度正文及无体退出回归 |

测试工具适配：macOS12不支持最新版Playwright所带Chromium，改用最新版测试库显式指定已安装的独立Chromium130，未保留旧版易受影响的下载依赖；npm全量审计为0。PTY测试驱动对EOF及时退出，并等实际ECHO关闭后写入，避免提示符与终端状态切换竞态。首轮失败过程记录于浏览器方案，最终结果为实际重跑结果。

## 6. 运行与后续

按README构建前端，在server目录执行管理员init，再启动server，访问 http://127.0.0.1:8080 。开发Vite使用5173时后端Origin同步配置为 http://127.0.0.1:5173 。服务、CLI与迁移不得并行写库；数据库/密钥不提交。

本轮没有提交、推送或部署；保留了原有IMPLEMENTATION_PLAN及技术设计修改。后续按总方案实现库存、食谱/推荐和饮食/营养模块时，必须接入当前用户归属、硬限制及幂等契约并执行B类用例；真实旧库和HTTPS环境具备后再执行C类验收。首版信任拥有重置权限的管理员和服务器运维人员，审计不能阻止其接管账号。
