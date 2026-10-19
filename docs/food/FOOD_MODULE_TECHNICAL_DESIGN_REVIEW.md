# FreshPlate 食材基础数据模块技术方案评审

状态：评审完成。评审日期：2026-10-19。评审对象：[FOOD_MODULE_TECHNICAL_DESIGN.md](FOOD_MODULE_TECHNICAL_DESIGN.md) 当前版本，评审时仓库 HEAD 为 `3b0e83a`。同批评审：[库存](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN_REVIEW.md)、[食谱](../recipe/RECIPE_MODULE_TECHNICAL_DESIGN_REVIEW.md)。

修订说明：2026-10-19 按确认清单修订方案及本评审。下文发现描述原评审问题，并补充最终取舍；“已处理”仅指设计文档，业务代码尚未实现，现有请求体限制仍为 64 KiB。

## 1. 总体结论

本方案是三模块共用契约（认证与安全、分页、响应、审计、数量契约引用）的源头，迁移器改造前提识别准确、旧契约完整保留、审计窄表与账号审计职责分离。原共享 64 KiB 上限与食谱字段限制的冲突已在方案层面修正；营养 CHECK 和首版取舍已明确，可按修订方案进入实现与验证。

## 2. 发现

### 2.1 已处理：明确营养完整组取舍及 CHECK

- 位置：设计文档 §3、§3.2。
- 来源缺项时，整组规则不能保留部分营养值，但允许整组未知，不会要求造假；原评审“会逼管理员造假”的说法不成立。
- 措辞歧义："两个营养来源字段全部为空，或四项全部有值且来源非空"——complete 分支下 `nutrition_reference` 是否必须非空未说清。
- 最终取舍：保留全有或全无，不增加“仅能量”子组。方案 §3 已写明完整布尔表达式，两个来源都须非 NULL 且去空格后非空；应用另拒绝 Unicode 纯空白来源。显式 IS NOT NULL 避免 SQLite CHECK 的 NULL 通过问题。

### 2.2 已处理（共享规则源头）：食谱写路由采用一致的 256 KiB 限制

- 位置：设计文档 §2"复用 `api.decode` 的 JSON、64 KiB……"并声明"以上规则也适用于另外两个方案"。
- 当前上限同时存在于 `server/internal/api/security.go:86,90` 和 `api.go:111`。30 步骤×1000 个中文码点仅正文即可达 90000 字节，超过 64 KiB；这是可复现示例，并非所有字符和 JSON 编码下的最大载荷。
- 两处现有超限路径均返回 `400 INVALID_ARGUMENT`，原“400/413”表述已纠正。修订方案仅将食谱创建/更新路由设为 256 KiB，其他接口保持 64 KiB；中间件 Content-Length 预检、流式 MaxBytesReader 和 decode 必须使用同一限制来源，只改 decode 不能生效。

### 2.3 低优先级

- **精度分工已明确**：数量用整数千分单位保证精确增减，营养/换算系数保留 REAL 作为估算，不统一整数存储；方案 §3.2 已补充说明。
- **数量契约位置保留**：规范继续位于库存方案；food 本身不解析数量、不依赖 helper，只有库存与食谱共享实现。文档引用不构成反向实现依赖。

## 3. 跨文档一致性

| 核对项 | 结果 |
| --- | --- |
| 迁移链 003→004→005 与现有 001/002 衔接 | ✅ 一致 |
| 认证、分页、响应、错误码契约被库存/食谱声明复用 | ✅ 一致 |
| 数量契约（字符串 + 千分位 + 三位小数）被下游引用 | ✅ 一致（规范位置见 2.3） |
| 窄表 `resource_audit_logs` 被食谱模块复用，不污染 `target_user_id NOT NULL` 的账号审计 | ✅ 一致 |
| 保留 `GET /foods` 旧契约、不因新接口改变旧行为 | ✅ 明确 |

## 4. 代码现状核对

设计文档声称的现状逐条属实（三模块合并核对共 10 项，全表存于本文件）：

| # | 声称 | 结果 | 证据 |
| --- | --- | --- | --- |
| 1 | 迁移器硬编码只支持 v1/v2，仅加 SQL 文件不会执行 | 属实 | `server/internal/store/db.go:150`（`:116/:160` 直接 ReadFile 001/002） |
| 2 | JSON 解码 64 KiB 上限 | 属实 | `server/internal/api/api.go:111`（`64<<10`；`security.go:86,90` 同值） |
| 3 | `audit_logs.target_user_id` NOT NULL，不能伪装目标用户 | 属实 | `server/migrations/001_users.sql:36` |
| 4 | `GET /api/foods` 返回 active 数组，仅 id/name/status，无分页 | 属实 | `server/internal/store/preferences.go:114`；列校验 `db.go:200` |
| 5 | JWT 契约：Bearer + `X-Expected-User-ID`；业务写不加 CSRF | 属实 | `docs/user/JWT_AUTHENTICATION.md:7,9,21`；`security.go:150-163`；`swagger.go:15` |
| 6 | `EnforceRestrictions` 输入含过敏原与完整性标志，incomplete fail-closed | 属实 | `server/internal/logic/preferences.go:74,79` |
| 7 | 过敏原恰 10 种编码 | 属实 | `server/internal/logic/preferences.go:9` |
| 8 | 分页默认 20、上限 100 | 属实 | `server/internal/api/users.go:24,47,167` |
| 9 | `WithImmediate` 使用 BEGIN IMMEDIATE，单连接事务 | 属实 | `server/internal/store/db.go:85,91` |
| 10 | 无同主题冲突旧方案（旧 Session Cookie 文档已被声明取代） | 属实 | `JWT_AUTHENTICATION.md:3`；本方案 §2 声明 |

## 5. 方案亮点

- 迁移器改造前置条件识别准确（硬编码 v1/v2 → 顺序迁移框架）。
- 审计窄表与账号审计职责分离，不破坏既有表约束。
- 完整保留 `GET /foods` 旧契约，新能力走新端点，设置页零回归。

## 6. 修订追踪

| 评审项 | 状态 |
| --- | --- |
| 2.1 营养 CHECK 取舍与歧义 | 已处理：完整组不变，两个来源必填，明确 SQL 分支 |
| 2.2 请求体限制 | 已处理：方案约定食谱写路由 256 KiB，两层一致，超限 400；代码待实现 |
| 2.3 REAL 精度说明 / 契约规范位置 | 已明确：保留 REAL 分工与库存契约位置 |
