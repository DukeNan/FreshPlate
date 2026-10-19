# FreshPlate 食谱管理模块技术方案评审

状态：评审完成。评审日期：2026-10-19。评审对象：[RECIPE_MODULE_TECHNICAL_DESIGN.md](RECIPE_MODULE_TECHNICAL_DESIGN.md) 当前版本，评审时仓库 HEAD 为 `3b0e83a`。同批评审：[食材](../food/FOOD_MODULE_TECHNICAL_DESIGN_REVIEW.md)、[库存](../inventory/INVENTORY_MODULE_TECHNICAL_DESIGN_REVIEW.md)。

修订说明：2026-10-19 按确认清单修订方案及本评审。下文记录原问题和处理结果；“已处理”仅指设计文档，业务代码尚未实现，现有请求体限制仍为 64 KiB。

## 1. 总体结论

方案整体可实施：发布时校验与读取时派生警示避免依赖陈旧标记，visibility/owner 联合 CHECK 与父子同事务设计正确。原载荷上限冲突、越权错误码和列表查询约束均已在方案层面处理，可按修订方案进入实现与验证。

## 2. 必须修正

### 2.1 最大合法载荷超过共享 64 KiB 请求体上限

- 冲突双方：本方案 §4 允许 30 个步骤 × 每步 1000 码点；[食材方案 §2](../food/FOOD_MODULE_TECHNICAL_DESIGN.md) 将 64 KiB 解码上限定为三模块共用规则。
- 计算：按中文 UTF-8 每码点 3 字节，仅步骤即达 30×1000×3=90000 字节，已超过 65536 字节；含其他字段的示例可达约 95 KB。这不是所有字符/编码的绝对上界，但足以证明字段限制内的请求可能被原限制拒绝。
- 现有 `security.go:86` 的 Content-Length 预检、`:90` 的流式 MaxBytesReader 及 `api.go:111` 的 decode 共同限制请求体；当前超限均返回 `400 INVALID_ARGUMENT`，原“400/413”已更正。中间件包裹后只放宽 decode 无法解除原限制。
- 处理结果：方案 §4 和食材方案 §2 约定食谱创建/更新路由 256 KiB，其余接口仍为 64 KiB；预检、中间件流式限制及 decode 共用同一限制来源，不新增通用配置项。字段上限和编码后的总字节上限分别验证，并覆盖有/无 Content-Length 的测试。

## 3. 建议修改

### 3.1 越权 PUT 错误码未闭环

- 位置：设计文档 §5"资源不存在、无权读取/编辑……在**普通详情接口**统一 404"只覆盖 GET。
- PUT 他人私有食谱、普通接口 PUT 公共食谱应同样统一 404（而非 403），与库存方案防枚举口径对齐。补一句明确化即可。
- 处理结果：方案 §5 已明确 GET/PUT 资源级拒绝均为 404；普通用户访问管理员路由为 403；管理员不能经管理接口访问私有食谱，该情况仍为 404。

### 3.2 列表派生字段的查询成本

- 位置：设计文档 §5，列表 items 含派生的 `restriction_metadata_complete`、`has_disabled_foods`。
- 默认每页 20、最大每页 100，每谱最多 100 原料，派生字段查询不应逐食谱/逐原料往返数据库。个人规模不要求特定优化框架，只须避免查询次数随条目数逐条增长。
- 处理结果：方案 §5 已加入约束，允许 EXISTS、聚合、批量 JOIN 或 IN，不限定必须使用某一种 SQL。

## 4. 跨文档一致性

| 核对项 | 结果 |
| --- | --- |
| 005 → v5 在食材 v3、库存 v4 之后（数量 helper 先行落地） | ✅ 一致 |
| 数量契约（字符串 + 千分位 + 三位小数）复用库存 helper | ✅ 一致 |
| 有效过敏原 = 关联 food 当前 ∪ 食谱附加；完整性取 AND | ✅ 与食材方案及 `EnforceRestrictions` fail-closed 语义一致 |
| 公共食谱审计复用 `resource_audit_logs`，action 集合与食材互补 | ✅ 一致 |
| 错误码纳入共用体系 | ✅ 越权 GET/PUT 为 404、管理员路由角色不足为 403，已按 3.1 闭环 |
| 删除语义：库存 `deleted_at` vs 食谱 archived | ✅ 不同但有明确理由（记餐引用 vs 用户可见状态），可接受 |

## 5. 代码现状核对

本模块直接相关项均属实（三模块合并核对共 10 项，全表见[食材评审 §4](../food/FOOD_MODULE_TECHNICAL_DESIGN_REVIEW.md)）：

| # | 声称 | 结果 | 证据 |
| --- | --- | --- | --- |
| 2 | 64 KiB 解码上限适用于本模块（与本方案字段上限冲突，见 2.1） | 属实 | `server/internal/api/api.go:111` |
| 6 | `EnforceRestrictions` 语义可承接完整性传递，incomplete fail-closed | 属实 | `server/internal/logic/preferences.go:74,79` |
| 7 | 过敏原恰 10 种编码，`recipe_allergens` CHECK 可直接复用 | 属实 | `server/internal/logic/preferences.go:9` |
| 8 | 分页默认 20、上限 100 | 属实 | `server/internal/api/users.go:47` |
| 9 | `WithImmediate` 单连接 BEGIN IMMEDIATE，父子表同事务可行 | 属实 | `server/internal/store/db.go:85,91` |

## 6. 方案亮点

- 发布时校验 + 读取时重新派生警示，食材后续停用/改标注不会读取到陈旧安全结论。
- visibility/owner 联合 CHECK + 创建路径决定 visibility，客户端无法自由提交归属。
- 父子表同事务整体替换，重复请求不会产生父内容与部分子项混合。
- 纯文本步骤 + 禁 `v-html`/Markdown 解析，XSS 防线明确；未提供烹饪按钮，避免形成未实现流程。

## 7. 修订追踪

| 评审项 | 状态 |
| --- | --- |
| 2.1 载荷上限冲突 | 已处理：方案约定食谱写路由 256 KiB，两层一致，超限 400；代码待实现 |
| 3.1 越权 PUT 统一 404 | 已处理：资源级 404、路由级角色 403 |
| 3.2 列表派生字段查询 | 已处理：禁止逐条 N+1，不限定 IN |
