# 用户模块 API 文档

[自动生成的 Swagger 2.0 规范](../../server/docs/swagger.yaml)覆盖当前实现的 14 个接口，包含请求字段、响应模型、参数限制及错误响应。可将文件导入 Swagger Editor/UI、Apifox 或 Postman 查看；规范由 Go handler 注释和实际请求/响应类型生成，技术设计保留方案背景。

## 查看与调试

启动 Go 服务后打开 [Swagger UI](http://127.0.0.1:8080/swagger/index.html)。页面由 `swaggo/gin-swagger` + `swaggo/files` 提供，规范从同源 `/swagger/doc.json` 加载，不依赖前端构建或外部 CDN。开发时也直接访问后端 8080 端口的该链接。

内置页面使用同源 `/api`。先执行 `/auth/csrf`，将返回 token 填入登录接口的 `X-CSRF-Token`；登录后在 Authorize 中填写 `Bearer <access_token>`。刷新和退出使用登录后的 `csrf_token`，浏览器自动携带 refresh Cookie。普通业务写接口只需 Bearer 和当前用户的 `X-Expected-User-ID`。Origin 必须与服务配置匹配，浏览器实际发送的值由浏览器控制。

可将 [JSON 规范](http://127.0.0.1:8080/swagger/doc.json) 或 `server/docs/swagger.yaml` 导入 API 工具，设置服务主机地址；`basePath` 为 `/api`。`swag` 当前生成 Swagger 2.0，Bearer 认证以 `Authorization` 请求头表示，refresh Cookie 由浏览器自动维护。外部 Swagger UI 的跨站页面受 Origin/CORS 限制，不能直接调试本服务。不要将真实密码、Cookie 或用户数据提交到公共文档服务。

## 接口索引

下表路径均带 `/api` 前缀。

| 方法 | 路径 | 权限与用途 |
| --- | --- | --- |
| GET | `/auth/csrf` | 匿名或登录：取得 CSRF 令牌 |
| POST | `/auth/register` | 匿名可用：注册普通用户，不自动登录 |
| POST | `/auth/login` | 匿名可用：登录，返回 access JWT 并设置 refresh Cookie |
| POST | `/auth/refresh` | refresh Cookie：轮换 refresh token，取得新 access JWT |
| POST | `/auth/logout` | 匿名或登录：退出当前会话，无请求体 |
| GET | `/me` | 登录：读取当前身份 |
| PUT | `/me/password` | 登录：修改本人密码并撤销本人全部会话 |
| GET | `/me/preferences` | 登录：读取个人饮食设置 |
| PUT | `/me/preferences` | 登录：完整替换个人饮食设置 |
| GET | `/foods` | 登录：查询启用食材，无分页 |
| GET | `/admin/users` | 管理员：筛选并分页查询用户 |
| PATCH | `/admin/users/{id}` | 管理员：修改目标角色或状态 |
| POST | `/admin/users/{id}/reset-password` | 管理员：重置目标密码，强制下次登录改密 |
| GET | `/admin/audit-logs` | 管理员：筛选并分页查询审计日志 |

## 认证与写请求约定

- access JWT 通过 `Authorization: Bearer <token>` 传递，有效期最多 15 分钟；只保存在前端内存，不写入 localStorage/sessionStorage。JWT 的 `sid` 对应服务端会话，每次请求复核会话和当前用户，注销、改密、重置或禁用后立即失效。
- refresh token 是随机不透明令牌，存于 `freshplate_refresh` Cookie；HTTPS Secure 模式为 `__Host-freshplate_refresh`。Cookie 为 HttpOnly、SameSite=Lax、Path=/，会话从登录起固定 7 天，刷新不延长。
- 登录、刷新和退出必须传精确匹配的 `Origin`、`X-CSRF-Token` 及 CSRF Cookie；普通业务写请求和注册不要求 CSRF。refresh 请求不要求 access JWT，不接受请求体。
- 每次刷新原子消费旧 refresh token 并设置新 Cookie。重放旧 token 会撤销整个所属会话；网络错误时不能自动重试刷新，可能需要重新登录。
- 受保护写请求另需 `X-Expected-User-ID`，值为表单加载时的当前用户 ID，不能使用目标用户 ID。缺失或格式错误返回 400，身份不一致返回 409。登录、注册、刷新、退出不需要此 Header。
- 登录后使用响应中的新 `csrf_token`。退出或本人改密后重新调用 `/auth/csrf` 取得匿名令牌；不能继续使用旧令牌。
- `must_change_password=true` 时仅允许访问 `/me`、`/auth/csrf`、`/auth/refresh`、`/auth/logout`、`/me/password`，其他接口返回 403 `PASSWORD_CHANGE_REQUIRED`。
- JSON 请求须为 `application/json`，最多 64 KiB，拒绝未知字段和尾随 JSON。退出接口不接受请求体，不能传 `{}`。

## 响应与错误处理

成功响应包在 `data` 内；注册返回 201，退出、改密和重置密码返回 204 且无响应体。失败格式：

```json
{"error":{"code":"AUTH_REQUIRED","message":"请先登录"}}
```

| 状态码 | 常见错误码 | 客户端处理 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT`、`INVALID_CURRENT_PASSWORD` | 修正输入；新密码与原密码相同也返回 `INVALID_ARGUMENT` |
| 401 | `AUTH_REQUIRED`、`INVALID_CREDENTIALS`、`ACCESS_TOKEN_EXPIRED`、`REFRESH_TOKEN_REUSED` | 仅 access 到期可刷新后重试一次；其他认证失败重新登录 |
| 403 | `CSRF_INVALID`、`FORBIDDEN`、`PASSWORD_CHANGE_REQUIRED` | 核对身份并刷新令牌、检查权限或先改密 |
| 404 | `NOT_FOUND` | 核对目标用户或接口路径 |
| 409 | `USERNAME_TAKEN`、`IDENTITY_CHANGED`、`SELF_ADMIN_CHANGE_FORBIDDEN`、`LAST_ADMIN_REQUIRED` | 刷新表单或修正操作；不能修改本人管理属性或重置本人密码 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | 使用 `application/json` |
| 429 | `RATE_LIMITED` | 按 `Retry-After` 秒数等待后重试 |
| 500 | `INTERNAL_ERROR` | 提示服务异常，不展示内部细节 |
| 503 | `DATABASE_BUSY` | 本次写入未保存，稍后手动重提 |

所有 API 响应均为 `Cache-Control: no-store`。仅 `ACCESS_TOKEN_EXPIRED` 可在刷新并核对原身份后自动重试一次；身份、CSRF、网络或其他业务失败不自动重放写请求。改密在跨标签页锁内不自动刷新；若恰在身份预检后过期，用户再次提交时预检会先刷新。限流包含注册/登录/改密频率及密码运算并发，具体阈值见 OpenAPI 各接口说明。

## curl 调用示例

以下从仓库根目录执行，需要已启动默认服务、已有普通用户，以及 Python 3。示例密码仅为占位值，请换成本地测试账号；不要把真实凭据保存在脚本或提交记录中。Cookie 和响应保存在临时目录中。

```sh
api_base=http://127.0.0.1:8080/api
api_origin=http://127.0.0.1:8080
api_tmp=$(mktemp -d)

# 1. 取得匿名 CSRF，并保存 Cookie。
curl --fail-with-body -sS -c "$api_tmp/cookies" \
  "$api_base/auth/csrf" -o "$api_tmp/csrf.json"
csrf=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["token"])' "$api_tmp/csrf.json")

# 2. 登录并保存新的 Cookie 和 CSRF。
curl --fail-with-body -sS -b "$api_tmp/cookies" -c "$api_tmp/cookies" \
  -H "Origin: $api_origin" -H "X-CSRF-Token: $csrf" \
  -H 'Content-Type: application/json' \
  --data '{"username":"alice_01","password":"Example-pass-2026"}' \
  "$api_base/auth/login" -o "$api_tmp/login.json"
csrf=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["csrf_token"])' "$api_tmp/login.json")
access=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["access_token"])' "$api_tmp/login.json")
user_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["user"]["id"])' "$api_tmp/login.json")

# 3. 提交完整设置；如账号要求先改密，应先调用改密接口并重新登录。
curl --fail-with-body -sS -X PUT -b "$api_tmp/cookies" \
  -H "Authorization: Bearer $access" \
  -H "X-Expected-User-ID: $user_id" -H 'Content-Type: application/json' \
  --data '{"allergens":[],"excluded_food_ids":[],"dietary_preferences":[],"daily_energy_kcal":2000,"daily_protein_g":null,"daily_fat_g":null,"daily_carbohydrate_g":null}' \
  "$api_base/me/preferences"

# 4. 刷新（无请求体），必须保存轮换后的 Cookie，access JWT 从响应重新读取。
curl --fail-with-body -sS -X POST -b "$api_tmp/cookies" -c "$api_tmp/cookies" \
  -H "Origin: $api_origin" -H "X-CSRF-Token: $csrf" \
  "$api_base/auth/refresh" -o "$api_tmp/refresh.json"

# 5. 退出，无请求体。
curl --fail-with-body -sS -X POST -b "$api_tmp/cookies" -c "$api_tmp/cookies" \
  -H "Origin: $api_origin" -H "X-CSRF-Token: $csrf" "$api_base/auth/logout"
```

调试后删除 `$api_tmp` 临时目录，其中包含登录 Cookie。管理员分页示例：`GET /api/admin/users?page=1&page_size=20&role=user&status=active`，响应为 `{"data":{"items":[],"total":0}}`；列表按 ID 降序。全部字段限制、食材/过敏原枚举和营养目标上限见 [生成的 Swagger definitions](../../server/docs/swagger.yaml)。

## 维护规则

修改 Go handler 注释、实际请求/响应类型，并同步本说明中的行为与示例。从仓库根目录执行：

```sh
(cd server && go generate ./internal/api)
```

生成器通过 `go.mod` 的 Go tool 声明固定为 `swag v1.16.6`；输出 `server/docs/docs.go`、`swagger.json`、`swagger.yaml`，与代码一起提交，不手工编辑生成文件。Go 服务通过生成的注册包提供 `/swagger/doc.json`；修改后需重新构建并重启 Go 服务，前端无需参与。后端回归测试会核对文档和实际 API 路由覆盖。本文件不将库存、推荐或营养计算等尚未实现的业务列为已提供接口。
