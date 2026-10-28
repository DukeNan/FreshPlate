# FreshPlate 鲜食记

FreshPlate（鲜食记）是饮食管理项目，已实现用户、食材基础数据、食材库存、食谱管理与确定性推荐，可在 macOS/Linux 本地运行。AI 步骤生成、记餐与营养业务尚未实现，后续规划见 [实现计划](IMPLEMENTATION_PLAN.md)，三模块实施结果见 [实施总结](docs/test/IMPLEMENTATION_SUMMARY.md)。

库存到期、推荐和营养统计按中国标准时间（UTC+8）计算日历日；记餐时间按中国标准时间输入和显示，时间点仍以 UTC 保存。

## 当前功能

- 注册、登录、退出；access JWT 有效期 15 分钟，HttpOnly refresh Cookie 固定有效期 7 天，支持轮换、重放检测及即时撤销。密码使用 Argon2id 哈希。
- 个人饮食设置及忌口食材选择，内置 12 种基础食材名称。
- 食谱推荐：按本人有效库存匹配公共及本人已完成食谱，强制过滤过敏原、忌口及不完整数据，按缺料数、临期食材和耗时排序，展示缺料与单位换算未知提示。推荐与详情的匹配提示为加载时快照，库存或偏好变化后须刷新。
- 推荐模块不新增数据库表或迁移，schema 仍为 v5；设计与验证见 [推荐方案](docs/recommendation/RECOMMENDATION_MODULE_TECHNICAL_DESIGN.md) 和 [验收记录](docs/recommendation/tasks/validation.md)。
- 普通用户与管理员权限；管理员可查询用户、调整角色和状态、重置密码及查询审计日志。
- 本地管理员初始化和密码恢复；重置或恢复密码后要求用户修改密码。
- Origin/CSRF 校验、写入时身份核对、请求限流与安全响应头。

## 技术架构与目录

前端使用 Vue 3、Vue Router 和 Vite，后端使用 Go、Gin、GORM 和 SQLite。浏览器通过 `/api` 访问接口，API 层负责请求与安全校验，logic 层处理业务规则，store 层负责数据库读写。本地管理员 CLI 复用业务与存储层。业务 CRUD 通过 GORM 执行，复用 `modernc.org/sqlite` 的纯 Go 连接；保留原 SQL 迁移及 `BEGIN IMMEDIATE` 事务清理，不使用 `AutoMigrate`，已有 v5 数据库无需转换。迁移验证见 [GORM 替换验证记录](docs/test/GORM_MIGRATION_VERIFICATION.md)。完整架构图与说明见 [用户模块架构](docs/user/USER_MODULE_ARCHITECTURE.md)。

```text
FreshPlate/
├── server/
│   ├── cmd/server/       # HTTP 服务入口
│   ├── cmd/admin/        # 管理员初始化、恢复入口
│   └── internal/
│       ├── api/          # 路由、请求校验与安全中间件
│       ├── logic/        # 认证、密码与业务规则
│       ├── store/        # SQLite、迁移与事务
│       └── runtime/      # 配置、密钥、进程锁与静态文件
├── web/                  # Vue 页面、单元测试与浏览器测试
├── scripts/              # CLI 与进程验证脚本
├── docs/                 # 按模块组织文档
│   └── user/             # 用户模块设计、测试方案、总结与验证记录
├── data/                 # 运行时数据库和密钥，自动创建且不入库
└── .env.example          # 环境变量示例
```

## 本地运行

需要 Go 1.26.4、Node.js 20.19+ 和 npm；浏览器端到端测试与 CLI 验证脚本还需要 Python 3。依赖版本记录在 `go.mod`、`go.sum` 和 `package-lock.json`。

以下命令从仓库根目录开始执行。使用 goenv 时先安装 Go 1.26.4；若环境强制设置旧 `GOROOT`，先执行 `unset GOROOT`，避免编译器与标准库不匹配。

```sh
npm ci --prefix web
npm run build --prefix web
cd server
go mod download
go run ./cmd/admin init --username admin
go run ./cmd/server
```

打开 <http://127.0.0.1:8080>，使用刚创建的管理员账号登录，或注册普通账号后登录。

API 文档页面：<http://127.0.0.1:8080/swagger/index.html>，使用方式见 [API 文档](docs/user/API.md)。

`init` 需要交互式终端，隐藏读取密码及确认密码。密码要求为 12～128 个 Unicode 码点且 UTF-8 编码不超过 512 字节，不裁剪或规范化。仅在没有管理员时允许初始化；公开注册始终创建普通用户，注册后需要登录。默认服务仅监听本机环回地址，并提供构建后的 Vue 页面及安全响应头。

开发时后端与Vite分别运行：

终端 1，从仓库根目录启动后端：

```sh
cd server
go run ./cmd/server --origin http://127.0.0.1:5173
```

终端 2，从仓库根目录启动前端：

```sh
npm run dev --prefix web
```

开发时打开 <http://127.0.0.1:5173>。Vite 将 `/api` 代理到 `127.0.0.1:8080`；浏览器地址与配置 Origin 必须逐字匹配，`localhost` 与 `127.0.0.1` 不同。生产不启用 CORS；前端代理与 HMR 配置仅用于开发。

## 配置与维护

配置示例见 [.env.example](.env.example)。服务读取导出的环境变量或命令行参数，命令行参数优先，**不自动加载 `.env`**。在server目录运行时，默认数据库 `../data/freshplate.db`、CSRF密钥 `../data/csrf.key`、JWT密钥 `../data/jwt.key`、静态目录 `../web/dist`。路径相对当前工作目录解析；自定义部署应使用绝对路径。

| Flag | 环境变量 | 默认值 |
| --- | --- | --- |
| --addr | FRESHPLATE_ADDR | 127.0.0.1:8080 |
| --db | FRESHPLATE_DB | ../data/freshplate.db |
| --key | FRESHPLATE_KEY | ../data/csrf.key |
| --jwt-key | FRESHPLATE_JWT_KEY | ../data/jwt.key |
| --origin | FRESHPLATE_ORIGIN | http://127.0.0.1:8080 |
| --secure | FRESHPLATE_SECURE | false |
| --web | FRESHPLATE_WEB | ../web/dist |
| --trusted-proxies | 无，显式flag | 不信任代理 |

`FRESHPLATE_SECURE` 只接受 `true` 或 `false`，Secure Cookie 模式必须与 Origin 的 HTTP/HTTPS 协议一致。`--trusted-proxies` 接受逗号分隔的 IP/CIDR。

同数据库服务与维护CLI共用操作系统独占锁；只运行一个服务进程。数据库必须为普通文件，不支持文件符号链接或硬链接别名，避免绕过互斥或混淆WAL。停止服务后才可初始化、恢复、迁移、备份或回退。SQLite启用WAL、每连接外键及5000ms忙等待，单连接池；503表示本次写入未保存，请稍后手动重提。内存限流重启清空；密码任务最多同时两个，占满立即429，不设等待队列。

恢复已有active管理员：

```sh
# 停止服务，在 server/ 执行；密码从终端隐藏输入
go run ./cmd/admin recover-password --username admin
```

恢复撤销该账号全部Session并要求登录后改密。不能用该命令启用disabled账号或提升普通用户。普通账号由网页管理员重置，临时密码通过独立渠道交付；系统不自动发消息。管理员能通过重置接管账号，首版信任管理员，审计只能追责。

JWT 升级及密钥预置步骤见 [JWT 认证说明](docs/user/JWT_AUTHENTICATION.md)。数据库、密钥和本地环境文件已忽略；密钥仅首次新部署生成32字节、0600，后续复用。已有数据库缺密钥时拒绝启动，先恢复原密钥备份；不能直接重启让系统静默换密钥。确实无备份时须由运维明确决定轮换：停服、完成一致性数据库备份、在受限目录生成新的32字节0600密钥并替换，再启动并验证登录及CSRF；原CSRF全部失效，Session本身不因密钥轮换失效。该恢复是人工运维操作，不提供HTTP旁路。

迁移仅支持当前新库结构，启动监听前完成迁移及校验；未知旧库、版本超前、脚本校验不符或结构不完整均拒绝启动。没有旧单人数据库格式，当前不提供猜测式导入。不要自动降级删除用户列。

备份须先停服，使用SQLite一致性备份（例如sqlite3的`.backup`）并同时备份CSRF与JWT密钥，运行integrity_check和foreign_key_check；不能只复制主库遗漏WAL。恢复时配套恢复数据库、密钥及对应程序版本，确认权限0600、外键与行数再启动。旧备份不能自动保留备份后新增数据；不要把该说明当作已演练的旧库迁移。

HTTPS部署需显式 `--origin https://你的域名 --secure`，仅信任明确列出的反向代理IP/CIDR。最终HTML应由当前服务或设置同等CSP/禁止嵌入/nosniff/Referrer-Policy的代理提供，HTTPS入口设置HSTS `max-age=31536000`；真实代理、TLS和公网开放需另做部署验收。不要同时暴露绕过HTTPS的明文入口。

## 接口约定

当前认证契约见 [JWT 认证说明](docs/user/JWT_AUTHENTICATION.md)，原始业务接口设计见 [技术设计](docs/user/USER_MODULE_TECHNICAL_DESIGN.md)。补充登录后只读 `GET /api/foods`，响应 `{"data":[{"id":1,"name":"鸡蛋","status":"active"}]}`，用于忌口选择；已有停用忌口在设置的excluded_foods中继续返回。迁移内置12种基础食材名称，不含营养或过敏原判定数据，不可用于营养计算。

受保护请求使用 `Authorization: Bearer <access_token>`，写请求另传表单加载时的 `X-Expected-User-ID`。登录、刷新、注销保留 Origin/CSRF；普通业务写接口无需 CSRF。access JWT 仅保存在内存，过期时通过 refresh Cookie 刷新，原身份未变化时最多重试一次；身份变化丢弃旧表单。Web Locks 和 BroadcastChannel 协调多标签页刷新。登录返回新 csrf_token；退出/改密后取得匿名令牌。所有API响应no-store；管理列表按id DESC、offset分页。PATCH同时变更role/status只写一条user_updated审计，单字段使用role_changed/status_changed；未改变值成功返回且不写审计。

## 测试验证

后端测试与静态检查，从仓库根目录开始：

```sh
cd server
go test ./...
go vet ./...
go test -race ./...
```

前端单元测试、构建与依赖检查，从仓库根目录执行：

```sh
npm test --prefix web
npm run build --prefix web
npm audit --prefix web
```

浏览器端到端测试，从仓库根目录开始：

```sh
cd web
npx playwright install chromium
npm run test:e2e
```

端到端测试会构建前端并启动真实 Go 服务，使用临时数据库、Python 3 终端夹具初始化管理员，结束后清理服务与临时数据。需保持 `127.0.0.1:18080` 空闲；测试不会复用已有服务。Go 默认从 PATH 查找，也可通过 `FRESHPLATE_E2E_GO` 指定可执行文件。旧系统若无法运行 Playwright 默认浏览器，可用 `FRESHPLATE_E2E_CHROMIUM` 指定兼容的 Chromium 完整路径，其结果仅代表该浏览器版本。

结果写入 `web/playwright-report/results.json`，失败截图与 trace 位于 `web/test-results/`。详细步骤见 [浏览器测试方案](docs/user/USER_MODULE_BROWSER_TEST_PLAN.md)。

CLI 和进程行为验证，从仓库根目录执行：

```sh
python3 scripts/verify_cli.py --go go
```

该脚本使用隔离目录验证初始化、恢复、进程锁和会话行为，不读取日常运行数据。

## 文档导航与交付边界

| 文档 | 内容 |
| --- | --- |
| [用户模块 API 使用说明](docs/user/API.md) | 认证、错误处理、接口索引与 curl 示例 |
| [食材基础数据技术方案](docs/food/FOOD_MODULE_TECHNICAL_DESIGN.md) | 已实现：食材目录、营养来源、换算与过敏原标注 |
| [食材库存技术方案](docs/inventory/INVENTORY_MODULE_TECHNICAL_DESIGN.md) | 已实现：个人批次库存与到期提醒 |
| [食谱管理技术方案](docs/recipe/RECIPE_MODULE_TECHNICAL_DESIGN.md) | 已实现：私有食谱与公共食谱发布 |
| [推荐模块技术方案](docs/recommendation/RECOMMENDATION_MODULE_TECHNICAL_DESIGN.md) | 待实现：安全过滤、库存匹配、排序与缺料说明 |
| [推荐模块最终评审](docs/recommendation/RECOMMENDATION_MODULE_FINAL_REVIEW.md) | Codex 与 Claude 互评共识及修订核对 |
| [三模块测试用例与实施总结](docs/test/IMPLEMENTATION_SUMMARY.md) | 测试用例基线、用例→测试映射、实测结果与遗留事项 |
| [自动生成的 Swagger 规范](server/docs/swagger.yaml) | Gin handler 注释生成的 33 个接口，可导入 API 工具 |
| [用户模块技术设计](docs/user/USER_MODULE_TECHNICAL_DESIGN.md) | 需求、数据模型与接口契约 |
| [用户模块架构](docs/user/USER_MODULE_ARCHITECTURE.md) | 已确认的架构图、职责与实现范围 |
| [测试用例](docs/user/USER_MODULE_TEST_CASES.md) | 按需求组织的验证场景 |
| [测试实施方案](docs/user/USER_MODULE_TEST_EXECUTION_PLAN.md) | 测试环境、实施步骤及验收条件 |
| [浏览器测试方案](docs/user/USER_MODULE_BROWSER_TEST_PLAN.md) | 页面交互与端到端验证 |
| [实现与测试总结](docs/user/USER_MODULE_IMPLEMENTATION_SUMMARY.md) | 实现结果、验证记录及已知限制 |
| [验证记录](docs/user/verification/) | 测试日志与结果文件 |

当前交付范围为用户模块。历史测试结果和执行环境以总结文档为准；未实现的业务、旧库迁移、真实 HTTPS/代理部署仍需后续验收。浏览器验证使用兼容的 Chromium，尚未覆盖 Safari、Firefox 和实体移动设备。
