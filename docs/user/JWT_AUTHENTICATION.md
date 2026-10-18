# JWT 认证与升级

本文是当前认证实现的契约；早期技术设计、架构图及历史验证记录中的 Session Cookie 描述仅代表迁移前版本。业务接口索引和调用示例见 [API.md](API.md)。

## 请求与会话

登录返回 `user`、`access_token`、`token_type: "Bearer"`、`expires_in`、`csrf_token`。access JWT 使用独立 32 字节密钥以 HS256 签名，包含 `sub`、`sid`、`iss`、`aud`、`iat`、`exp`，最长 15 分钟且不超过会话到期。受保护 API 只接受 Authorization Bearer，旧 Session Cookie 不再认证。

每次认证查询 `sid` 对应会话及当前用户，检查会话有效期、撤销状态、用户状态和认证版本，角色读取数据库最新值；写事务再次复核。注销撤销当前会话，改密、管理员重置密码、恢复密码及禁用账号撤销该用户全部会话，旧 access JWT 立即失效。受保护写操作仍需当前表单的 `X-Expected-User-ID`，防止账号切换后误提交。

refresh token 为随机不透明令牌，只存 HttpOnly、SameSite=Lax、Path=/ Cookie；HTTP 名称 `freshplate_refresh`，HTTPS Secure 模式为 `__Host-freshplate_refresh`。服务端只保存 SHA-256 哈希。会话从登录起固定有效 7 天，刷新不延长。

`POST /api/auth/refresh` 不要求有效 access JWT，每次在 SQLite 即时事务内消费旧令牌、插入新令牌并设置新 Cookie。消费记录保留至会话到期；再次使用旧令牌返回 `401 REFRESH_TOKEN_REUSED`，并提交撤销整个所属会话。未知令牌不能撤销其他会话。严格轮换意味着刷新响应丢失时可能需要重新登录；客户端禁止自动重试网络失败的刷新。

## 浏览器与 CSRF

access JWT 只存内存。刷新页面时通过 refresh Cookie 恢复，普通 API 请求自动添加 Bearer。每个标签页共享一个刷新 Promise，跨标签页由 Web Locks 排他锁串行化，通过 BroadcastChannel 广播 access JWT 和登录状态。localStorage 仅保存随机的 `freshplate-auth-revision` 顺序标记，不保存令牌、密码或用户资料；sessionStorage 不保存凭据。目标浏览器必须支持 Web Locks 和 BroadcastChannel。

只有 `ACCESS_TOKEN_EXPIRED` 会触发刷新，并在原身份一致时最多重试原请求一次；身份变化、网络错误及业务错误不自动重放写操作。旧请求不能覆盖新账号的状态。改密在锁外预检身份、锁内提交；为避免重入刷新锁，若 access 恰在预检后到期，改密返回到期错误并要求再次提交，下次预检会先刷新。

登录、刷新、注销要求精确 Origin、CSRF Cookie 及 `X-CSRF-Token`；普通业务写接口和注册不要求 CSRF。匿名或已登录状态都可通过 `GET /api/auth/csrf` 取得令牌，登录后使用响应的新值。CSRF 与稳定 `sid` 绑定，不随 refresh token 轮换；access JWT 不替代 Cookie 接口的 CSRF。

## 升级和密钥

新增 `--jwt-key` / `FRESHPLATE_JWT_KEY`，在 `server/` 运行时默认 `../data/jwt.key`。JWT 与 CSRF 使用不同文件，均须为 32 字节、权限 `0600` 的普通文件。首次新部署自动生成；已有数据库缺失密钥时拒绝启动，不会静默更换。

升级步骤：

1. 停止服务，使用 SQLite 一致性备份保留数据库和原 CSRF 密钥；不要只复制可能遗漏 WAL 的主文件。
2. 在受限的 `data/` 目录中预置独立 JWT 密钥；自定义路径时在服务和管理员 CLI 使用相同 `--jwt-key`。

   ```sh
   # 从仓库根目录执行；仅在尚无 JWT 密钥时创建，存在时拒绝覆盖。
   python3 - <<'PY'
   import os
   from pathlib import Path
   path = Path('data/jwt.key')
   fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
   with os.fdopen(fd, 'wb') as file:
       file.write(os.urandom(32))
   PY
   ```

3. 启动新服务，让新增迁移建立 JWT 会话及 refresh 表。旧登录会话失效，用户、偏好和审计数据保留；所有用户需要重新登录。
4. 验证登录、刷新、注销、强制改密及旧凭据失效，再开放流量。后续备份必须配套保存数据库、CSRF/JWT 密钥和程序版本。

JWT 密钥轮换会使已签发 access JWT 验签失败；保留数据库会话时 refresh 仍可换取新 access。无密钥备份时按人工轮换操作处理，禁止让启动过程自动补密钥。旧程序不支持新增数据库版本，不提供自动降级。

## 验证

后端覆盖签名与声明验证、即时撤销、事务内复核、轮换并发及重放撤销；前端覆盖刷新合并与身份代次。Playwright 使用隔离数据库验证真实 Bearer 请求、Cookie 属性、刷新轮换与重放、多标签页恢复和注销；`scripts/verify_cli.py` 验证进程互斥、重启持久化、恢复撤销和独立密钥。历史日志不覆盖，本次结果另行记录。

不提供旧认证兼容期、刷新宽限窗口或滑动续期。真实 HTTPS/反向代理、Safari、Firefox 和实体移动设备需各部署环境另行验证。
